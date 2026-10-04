package xkeen

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

// Internal fixed sidecar; never passed to Xray or exposed as a raw editor ID.
const registryConfigID = "nodes.registry"

var absentRegistry = []byte("null")

func generationConfig(name string) bool {
	return editableConfig(name) || name == "04_outbounds.json" || name == registryConfigID
}
func (e *ConfigEditor) configPath(name string) string {
	if name == registryConfigID {
		return e.RegistryPath
	}
	return filepath.Join(e.Dir, name)
}

func (e *ConfigEditor) registryBytes() ([]byte, error) {
	parent, err := os.Lstat(filepath.Dir(e.RegistryPath))
	if err != nil && !os.IsNotExist(err) || err == nil && (!parent.IsDir() || parent.Mode().Perm() != 0700) {
		return nil, ErrConfig
	}
	info, err := os.Lstat(e.RegistryPath)
	if os.IsNotExist(err) {
		return append([]byte(nil), absentRegistry...), nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, ErrConfig
	}
	data, state := (Discovery{Root: filepath.Dir(e.RegistryPath)}).read(filepath.Base(e.RegistryPath), 4<<20)
	if state != CapabilityAvailable {
		return nil, ErrConfig
	}
	if _, err := configjson.DecodeObject(data); err != nil {
		return nil, ErrConfig
	}
	return data, nil
}

func (e *ConfigEditor) prepareRegistryDirectory() error {
	parent := filepath.Dir(e.RegistryPath)
	if e.RegistryPath == "" || os.MkdirAll(parent, 0700) != nil {
		return ErrConfig
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrConfig
	}
	return nil
}

// StageTransferUnderLease saves typed, already registry-validated native data
// through the existing pending/discard/previous-generation owner. The caller
// holds the panel lease and validates managed outbounds against the registry.
// This does not Restart. Public raw editors still exclude outbounds/registry.
func (e *ConfigEditor) StageTransferUnderLease(ctx context.Context, baseline string, files map[string][]byte, registry []byte) (string, error) {
	changes, err := e.transferChanges(files, registry)
	if err != nil {
		return "", err
	}
	if pending, err := e.HasSavedChanges(); err != nil || pending {
		return "", ErrConfig
	}
	before, err := e.Snapshot(ctx)
	if err != nil || before.Digest != baseline {
		return "", ErrConfig
	}
	return e.saveCandidate(ctx, before, changes)
}

// PreviewTransferUnderLease validates a destination candidate without writing.
func (e *ConfigEditor) PreviewTransferUnderLease(ctx context.Context, files map[string][]byte, registry []byte) (string, error) {
	changes, err := e.transferChanges(files, registry)
	if err != nil {
		return "", err
	}
	if pending, err := e.HasSavedChanges(); err != nil || pending {
		return "", ErrConfig
	}
	before, err := e.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	after, err := candidateSet(before, changes)
	if err != nil {
		return "", err
	}
	if err := e.validateSet(ctx, after); err != nil {
		return "", err
	}
	current, err := e.Snapshot(ctx)
	if err != nil || current.Digest != before.Digest {
		return "", ErrConfig
	}
	return before.Digest, nil
}

func (e *ConfigEditor) transferChanges(files map[string][]byte, registry []byte) (map[string][]byte, error) {
	if e.RegistryPath == "" || len(files) == 0 || len(files) > 8 || len(registry) == 0 || len(registry) > 4<<20 || bytes.Equal(registry, absentRegistry) {
		return nil, ErrConfig
	}
	changes := make(map[string][]byte, len(files)+1)
	for name, data := range files {
		if !generationConfig(name) || name == registryConfigID || data == nil {
			return nil, ErrConfig
		}
		changes[name] = data
	}
	changes[registryConfigID] = registry
	return changes, nil
}
