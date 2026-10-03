// Package nativebackup transfers native data, never native programs or panel
// authentication. It uses the existing bounded encrypted envelope.
package nativebackup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

const Format = "xkeen-control-native-data"
const Version = 1

type Bundle struct {
	Format     string            `json:"format"`
	Version    int               `json:"version"`
	ExportedAt string            `json:"exportedAt"`
	Files      map[string][]byte `json:"files"`
	Registry   json.RawMessage   `json:"registry"`
}

type RegistrySource interface {
	NativeSnapshotUnderLease(context.Context) (nodes.Registry, string, error)
}
type Service struct {
	Editor      *xkeen.ConfigEditor
	Nodes       RegistrySource
	Lease       *authority.Lease
	Interfaces  func() ([]string, error)
	mu          sync.Mutex
	preview     *privatePreview
	previewBusy bool
}

// Every native config can contain private material. Plaintext export is not a
// separate "safe settings" format and is deliberately unavailable.
func (s *Service) Export(context.Context) ([]byte, error) { return nil, backup.ErrUnavailable }

func (s *Service) ExportSecret(ctx context.Context, passphrase string) ([]byte, error) {
	if err := backup.ValidatePassphrase(passphrase); err != nil {
		return nil, err
	}
	if s == nil || s.Editor == nil || s.Nodes == nil || s.Lease == nil {
		return nil, backup.ErrUnavailable
	}
	return backup.SealProduced(passphrase, func() ([]byte, error) { return s.exportPlaintext(ctx) })
}

func (s *Service) exportPlaintext(ctx context.Context) ([]byte, error) {
	release, err := s.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return nil, backup.ErrUnavailable
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	files, digest, err := s.Editor.ExportFilesUnderLease(ctx)
	if err != nil {
		return nil, backup.ErrUnavailable
	}
	defer func() {
		for _, data := range files {
			clear(data)
		}
	}()
	registry, registryDigest, err := s.Nodes.NativeSnapshotUnderLease(ctx)
	if err != nil {
		return nil, backup.ErrUnavailable
	}
	encoded, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return nil, backup.ErrUnavailable
	}
	defer clear(encoded)
	bundle := Bundle{Format: Format, Version: Version, ExportedAt: time.Now().UTC().Format(time.RFC3339Nano), Files: files, Registry: encoded}
	if validate(bundle) != nil {
		return nil, backup.ErrUnavailable
	}
	current, err := s.Editor.Snapshot(ctx)
	latest, latestDigest, registryErr := s.Nodes.NativeSnapshotUnderLease(ctx)
	if err != nil || registryErr != nil || current.Digest != digest || latestDigest != registryDigest || !reflect.DeepEqual(latest, registry) {
		return nil, backup.ErrUnavailable
	}
	release()
	release = nil
	plaintext, err := json.Marshal(bundle)
	if err != nil {
		return nil, backup.ErrUnavailable
	}
	return plaintext, nil
}

// Open authenticates and strictly validates a native-data archive. Opening an
// archive performs no writes and does not authorize restoration or Restart.
func Open(contents []byte, passphrase string) (Bundle, error) {
	var bundle Bundle
	err := backup.OpenProduced(contents, passphrase, func(plaintext []byte) error {
		var err error
		bundle, err = Parse(plaintext)
		return err
	})
	if err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func Parse(plaintext []byte) (Bundle, error) {
	if len(plaintext) == 0 || len(plaintext) > backup.MaxSecretPlaintext {
		return Bundle{}, backup.ErrInvalidBundle
	}
	var bundle Bundle
	d := json.NewDecoder(bytes.NewReader(plaintext))
	d.DisallowUnknownFields()
	if d.Decode(&bundle) != nil || d.Decode(&struct{}{}) != io.EOF || validate(bundle) != nil {
		return Bundle{}, backup.ErrInvalidBundle
	}
	return bundle, nil
}

func validate(bundle Bundle) error {
	if bundle.Format != Format || bundle.Version != Version || len(bundle.Files) == 0 || len(bundle.Files) > 8 {
		return backup.ErrInvalidBundle
	}
	if _, err := time.Parse(time.RFC3339Nano, bundle.ExportedAt); err != nil {
		return backup.ErrInvalidBundle
	}
	for name, data := range bundle.Files {
		switch name {
		case "01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json":
		default:
			return backup.ErrInvalidBundle
		}
		if len(data) > 2<<20 {
			return backup.ErrInvalidBundle
		}
		if _, err := configjson.DecodeObject(data); err != nil {
			return backup.ErrInvalidBundle
		}
	}
	registry, err := nodes.ParseCanonical(bundle.Registry)
	if err != nil {
		return backup.ErrInvalidBundle
	}
	outbounds := bundle.Files["04_outbounds.json"]
	if len(outbounds) == 0 {
		return backup.ErrInvalidBundle
	}
	rendered, err := nodes.RenderNative(outbounds, registry, registry)
	if err != nil {
		return backup.ErrInvalidBundle
	}
	left, leftErr := configjson.DecodeObject(outbounds)
	right, rightErr := configjson.DecodeObject(rendered)
	if leftErr != nil || rightErr != nil {
		return backup.ErrInvalidBundle
	}
	// Compare semantic content, preserving array order while accepting JSONC and
	// whitespace. Do not silently rebuild an externally changed managed profile.
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	var a, b any
	ld, rd := json.NewDecoder(bytes.NewReader(l)), json.NewDecoder(bytes.NewReader(r))
	ld.UseNumber()
	rd.UseNumber()
	if ld.Decode(&a) != nil || rd.Decode(&b) != nil || !reflect.DeepEqual(a, b) {
		return backup.ErrInvalidBundle
	}
	return nil
}
