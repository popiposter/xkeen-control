package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/release"
)

const DefaultIntentPath = "/opt/etc/xkeen-control/state/panel-update-active.json"
const DefaultResultPath = "/opt/etc/xkeen-control/state/panel-update-result.json"

var ErrInspectionRequired = errors.New("panel update requires receipt inspection; no operation was replayed")

// Receipt is the existing updater's durable sanitized outcome, not permission
// to resume work. An active intent always fences mutations, even if a terminal
// result was written before a crash during intent removal.
type Receipt struct {
	Verified       *buildinfo.Info `json:"verified,omitempty"`
	Schema         int             `json:"schemaVersion"`
	ID             string          `json:"operationId"`
	Action         string          `json:"action"`
	ManifestDigest string          `json:"manifestDigest"`
	PreviousDigest string          `json:"previousDigest"`
	Architecture   string          `json:"architecture"`
	Previous       buildinfo.Info  `json:"previous"`
	Candidate      buildinfo.Info  `json:"candidate"`
	Phase          string          `json:"phase"`
	StartedAt      string          `json:"startedAt"`
	UpdatedAt      string          `json:"updatedAt"`
	Reason         string          `json:"reason,omitempty"`
}

var receiptHex = regexp.MustCompile(`^[0-9a-f]+$`)
var receiptReason = regexp.MustCompile(`^[a-z0-9-]{0,64}$`)

func (r Receipt) validate() error {
	if r.Schema != 1 || len(r.ID) != 32 || !receiptHex.MatchString(r.ID) || len(r.ManifestDigest) != 64 || !receiptHex.MatchString(r.ManifestDigest) || len(r.PreviousDigest) != 64 || !receiptHex.MatchString(r.PreviousDigest) || r.Previous.Validate() != nil || r.Candidate.Validate() != nil || !receiptReason.MatchString(r.Reason) {
		return ErrInspectionRequired
	}
	if r.Action != "install" && r.Action != "rollback" || r.Architecture != "arm64" && r.Architecture != "mipsle" {
		return ErrInspectionRequired
	}
	start, e1 := time.Parse(time.RFC3339, r.StartedAt)
	end, e2 := time.Parse(time.RFC3339, r.UpdatedAt)
	if e1 != nil || e2 != nil || end.Before(start) {
		return ErrInspectionRequired
	}
	switch r.Phase {
	case "prepared", "stopping", "committing", "starting", "marker-commit", "rollback", "rollback-starting", "inspection-required":
		if r.Verified != nil {
			return ErrInspectionRequired
		}
	case "installed-verified":
		if r.Action != "install" || r.Verified == nil || *r.Verified != r.Candidate {
			return ErrInspectionRequired
		}
	case "rolled-back-verified":
		want := r.Previous
		if r.Action == "rollback" && r.Reason == "explicit-rollback" {
			want = r.Candidate
		}
		if r.Verified == nil || *r.Verified != want {
			return ErrInspectionRequired
		}
	default:
		return ErrInspectionRequired
	}
	return nil
}

func receiptPaths(marker string) (string, string) {
	dir := filepath.Dir(marker)
	return filepath.Join(dir, "panel-update-active.json"), filepath.Join(dir, "panel-update-result.json")
}

func readReceipt(path string) (*Receipt, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if !protectedPath(path) {
		return nil, ErrInspectionRequired
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 8192 {
		return nil, ErrInspectionRequired
	}
	dir, e := os.Lstat(filepath.Dir(path))
	if e != nil || !dir.IsDir() || dir.Mode().Perm() != 0700 {
		return nil, ErrInspectionRequired
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrInspectionRequired
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrInspectionRequired
	}
	var r Receipt
	dec := json.NewDecoder(io.LimitReader(f, 8193))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil || dec.Decode(&struct{}{}) != io.EOF || r.validate() != nil {
		return nil, ErrInspectionRequired
	}
	if _, err := hex.DecodeString(r.ID); err != nil {
		return nil, ErrInspectionRequired
	}
	return &r, nil
}

func MutationReady() error {
	if _, err := os.Lstat(DefaultIntentPath); !os.IsNotExist(err) {
		return ErrInspectionRequired
	}
	r, err := readReceipt(DefaultResultPath)
	if err != nil || r != nil && (r.Phase != "installed-verified" && r.Phase != "rolled-back-verified" || r.Verified == nil || r.Verified.Validate() != nil) {
		return ErrInspectionRequired
	}
	return nil
}

func (m *Manager) Receipt() (*Receipt, error) {
	active, result := receiptPaths(m.paths.MarkerPath)
	r, err := readReceipt(active)
	if err != nil {
		return nil, err
	}
	terminal, e := readReceipt(result)
	if e != nil {
		return nil, e
	}
	if r != nil {
		if terminal != nil && terminal.ID == r.ID {
			return terminal, ErrInspectionRequired
		}
		return r, ErrInspectionRequired
	}
	if terminal != nil && (terminal.Phase != "installed-verified" && terminal.Phase != "rolled-back-verified" || terminal.Verified == nil || terminal.Verified.Validate() != nil) {
		return terminal, ErrInspectionRequired
	}
	return terminal, nil
}

func (m *Manager) reserve(candidate release.Candidate) error {
	manifest, err := candidate.Manifest.MarshalDeterministic()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(manifest)
	return m.reserveRecord(Receipt{Action: "install", ManifestDigest: hex.EncodeToString(digest[:]), Architecture: candidate.Manifest.Architecture, Candidate: buildinfo.Info{Product: release.Product, Version: candidate.Manifest.Version, SourceCommit: candidate.Manifest.SourceCommit, Channel: candidate.Manifest.Channel}})
}

func (m *Manager) reserveRollback() error {
	marker := filepath.Join(m.paths.PreviousDir, "installed-release.json")
	if !protectedPath(marker) {
		return ErrInspectionRequired
	}
	f, err := os.Open(marker)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 8193))
	var build buildinfo.Info
	if err != nil || len(b) > 8192 || json.Unmarshal(b, &build) != nil || build.Validate() != nil {
		return ErrInspectionRequired
	}
	digest, err := generationDigest(m.paths.PreviousDir, m.client.Architecture())
	if err != nil {
		return err
	}
	return m.reserveRecord(Receipt{Action: "rollback", ManifestDigest: digest, Architecture: m.client.Architecture(), Candidate: build})
}

func generationDigest(dir, arch string) (string, error) {
	h := sha256.New()
	for _, name := range []string{release.BinaryArtifact(arch), "S99xkeen-control", "xkeen-control-updater", "installed-release.json"} {
		if name == "" {
			return "", ErrInspectionRequired
		}
		sum, err := protectedHash(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		io.WriteString(h, sum+"  "+name+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) installedDigest() (string, error) {
	h := sha256.New()
	names := []string{release.BinaryArtifact(m.client.Architecture()), "S99xkeen-control", "xkeen-control-updater", "installed-release.json"}
	for i, path := range []string{m.paths.BinaryPath, m.paths.InitPath, m.paths.HelperPath, m.paths.MarkerPath} {
		sum, err := protectedHash(path)
		if err != nil {
			return "", err
		}
		io.WriteString(h, sum+"  "+names[i]+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) reserveRecord(r Receipt) error {
	active, result := receiptPaths(m.paths.MarkerPath)
	if _, err := os.Lstat(active); !os.IsNotExist(err) {
		return ErrInspectionRequired
	}
	prior, err := readReceipt(result)
	if err != nil || prior != nil && prior.Phase != "installed-verified" && prior.Phase != "rolled-back-verified" {
		return ErrInspectionRequired
	}
	if err := os.MkdirAll(filepath.Dir(active), 0700); err != nil {
		return err
	}
	d, err := os.Lstat(filepath.Dir(active))
	if err != nil || !d.IsDir() || d.Mode().Perm() != 0700 || !protectedDirectory(filepath.Dir(active)) {
		return ErrInspectionRequired
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	r.Schema = 1
	r.ID = hex.EncodeToString(id[:])
	r.Previous = m.current
	r.PreviousDigest, err = m.installedDigest()
	if err != nil {
		return err
	}
	r.Phase = "prepared"
	r.StartedAt = now
	r.UpdatedAt = now
	if r.validate() != nil {
		return ErrInspectionRequired
	}
	if err := m.durabilityReady(); err != nil {
		return err
	}
	f, err := os.OpenFile(active, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrInspectionRequired
	}
	// Once reserved, every failure retains evidence and refuses replay.
	err = json.NewEncoder(f).Encode(r)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	dir, err := os.Open(filepath.Dir(active))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (m *Manager) durabilityReady() error {
	syncPath, err := exec.LookPath("sync")
	if err != nil {
		return errors.New("panel update requires coreutils-sync with file/directory sync -f")
	}
	if info, e := os.Stat("/opt/bin/sync"); e == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
		syncPath = "/opt/bin/sync"
	}
	for _, path := range []string{m.paths.MarkerPath, filepath.Dir(m.paths.MarkerPath)} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = exec.CommandContext(ctx, syncPath, "-f", path).Run()
		cancel()
		if err != nil {
			return errors.New("panel update requires working file/directory sync -f; install coreutils-sync")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, "timeout", "-k", "1", "1", "true").Run() != nil {
		return errors.New("panel update requires timeout -k support; install coreutils-timeout")
	}
	return flockReady()
}

// This disposable RAM file probes the required descriptor operations; it is
// never an update lock. The actual owner remains the existing setup inode.
func flockReady() error {
	flock, err := exec.LookPath("flock")
	if err != nil {
		return errors.New("panel update requires compatible flock descriptor locking")
	}
	f, err := os.CreateTemp("/tmp", "xkeen-flock-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	g, err := os.Open(f.Name())
	if err != nil {
		return err
	}
	defer g.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "timeout", "-k", "1", "3", "/bin/sh", "-c", `"$1" -n -x 3 || exit 1
if "$1" -n -s 4; then exit 1; fi
"$1" -n -s 3 || exit 1
"$1" -n -s 4 || exit 1
if "$1" -n -x 4; then exit 1; fi`, "flock-capability", flock)
	cmd.ExtraFiles = []*os.File{f, g}
	if cmd.Run() != nil {
		return errors.New("panel update requires flock -n -x and shared descriptor semantics")
	}
	return nil
}
