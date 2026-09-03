package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

// FileAuthorityConfig names only the fixed D.1 authority and generated
// runtime artifacts. It is an internal adapter, not a raw file API.
type FileAuthorityConfig struct {
	Appliance *appliance.Service
	Nodes     *nodes.Manager

	AppliancePath       string
	NodesPath           string
	ConfigDir           string
	XkeenConfigPath     string
	ActiveOutboundsPath string
}

// FileAuthorityProvider proves the adopted D.1 authority and returns a
// hash-only generation token for stale transaction checks. The typed
// registry is retained only inside the transaction call; it is never logged
// or exposed by this adapter.
type FileAuthorityProvider struct {
	config FileAuthorityConfig
}

func NewFileAuthorityProvider(config FileAuthorityConfig) *FileAuthorityProvider {
	return &FileAuthorityProvider{config: config}
}

func (p *FileAuthorityProvider) SnapshotUnderLease(ctx context.Context) (XrayAuthoritySnapshot, error) {
	if p == nil || p.config.Appliance == nil || p.config.Nodes == nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.config.Appliance.Verify(ctx); err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	applianceValue, err := p.config.Appliance.SnapshotUnderLease()
	if err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	registry, err := p.config.Nodes.SnapshotUnderLease(ctx)
	if err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	strictContents, err := readAuthorityFile(p.config.NodesPath, nodes.MaxLegacyDocument, true)
	if err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	strictRegistry, err := nodes.ParseCanonical(strictContents)
	if err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	loadedCanonical, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	strictCanonical, err := nodes.MarshalCanonical(strictRegistry)
	if err != nil || !bytes.Equal(loadedCanonical, strictCanonical) {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}

	inputs := []authorityHashInput{
		{name: "appliance", path: p.config.AppliancePath, limit: appliance.MaxDocumentSize, private: true},
		{name: "nodes", path: p.config.NodesPath, limit: nodes.MaxLegacyDocument, private: true},
		{name: "xray/01_log.json", path: filepath.Join(p.config.ConfigDir, "01_log.json"), limit: appliance.MaxDocumentSize},
		{name: "xray/02_dns.json", path: filepath.Join(p.config.ConfigDir, "02_dns.json"), limit: appliance.MaxDocumentSize},
		{name: "xray/03_inbounds.json", path: filepath.Join(p.config.ConfigDir, "03_inbounds.json"), limit: appliance.MaxDocumentSize},
		{name: "xray/04_outbounds.json", path: p.config.ActiveOutboundsPath, limit: nodes.MaxLegacyDocument},
		{name: "xray/05_routing.json", path: filepath.Join(p.config.ConfigDir, "05_routing.json"), limit: appliance.MaxDocumentSize},
		{name: "xray/06_policy.json", path: filepath.Join(p.config.ConfigDir, "06_policy.json"), limit: appliance.MaxDocumentSize},
		{name: "xray/07_observatory.json", path: filepath.Join(p.config.ConfigDir, "07_observatory.json"), limit: appliance.MaxDocumentSize},
		{name: "xray/08_api.json", path: filepath.Join(p.config.ConfigDir, "08_api.json"), limit: appliance.MaxDocumentSize},
		{name: "xkeen/xkeen.json", path: p.config.XkeenConfigPath, limit: appliance.MaxDocumentSize},
	}
	digest, err := hashAuthorityInputs(inputs)
	if err != nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	return XrayAuthoritySnapshot{Appliance: applianceValue, Registry: strictRegistry, Generation: digest}, nil
}

type authorityHashInput struct {
	name    string
	path    string
	limit   int
	private bool
}

func hashAuthorityInputs(inputs []authorityHashInput) ([sha256.Size]byte, error) {
	hash := sha256.New()
	for _, input := range inputs {
		if input.name == "" || input.path == "" || input.limit <= 0 {
			return [sha256.Size]byte{}, errors.New("authority input is not configured")
		}
		contents, err := readAuthorityFile(input.path, input.limit, input.private)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if err := writeAuthorityHashPart(hash, input.name, contents); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func writeAuthorityHashPart(destination io.Writer, name string, contents []byte) error {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(name)))
	if _, err := destination.Write(length[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(destination, name); err != nil {
		return err
	}
	binary.BigEndian.PutUint64(length[:], uint64(len(contents)))
	if _, err := destination.Write(length[:]); err != nil {
		return err
	}
	_, err := destination.Write(contents)
	return err
}

func readAuthorityFile(path string, limit int, private bool) ([]byte, error) {
	if path == "" || limit <= 0 {
		return nil, errors.New("authority file is not configured")
	}
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > int64(limit) {
		return nil, errors.New("authority file is not a bounded regular file")
	}
	if private && runtime.GOOS != "windows" && before.Mode().Perm() != 0o600 {
		return nil, errors.New("authority file permissions are invalid")
	}
	if private && runtime.GOOS != "windows" {
		if err := checkPrivateComponentDirectory(filepath.Dir(path)); err != nil {
			return nil, errors.New("authority directory is not private")
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, errors.New("authority file changed while opening")
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(contents) > limit {
		return nil, errors.New("authority file exceeds bounded size")
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || after.Size() != int64(len(contents)) {
		return nil, errors.New("authority file changed while reading")
	}
	return contents, nil
}
