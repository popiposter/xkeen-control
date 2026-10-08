package xkeen

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

func candidateSet(before ConfigSnapshot, changes map[string][]byte) (ConfigSnapshot, error) {
	after := ConfigSnapshot{files: make(map[string][]byte, len(before.files))}
	for name, data := range before.files {
		after.files[name] = data
	}
	for name, data := range changes {
		limit := maxNativeConfig
		if name == registryConfigID {
			limit = 4 << 20
		}
		if !generationConfig(name) || len(data) > limit {
			return ConfigSnapshot{}, ErrConfig
		}
		if data == nil && name != registryConfigID {
			delete(after.files, name)
			continue
		}
		if name == registryConfigID && bytes.Equal(data, absentRegistry) {
			after.files[name] = data
			continue
		}
		if _, err := configjson.DecodeObject(data); err != nil {
			return ConfigSnapshot{}, ErrConfig
		}
		after.files[name] = data
	}
	total := 0
	for _, data := range after.files {
		total += len(data)
	}
	if total > 8<<20 {
		return ConfigSnapshot{}, ErrConfig
	}
	after.Digest = configDigest(after.files)
	return after, nil
}

func (e *ConfigEditor) validateSet(ctx context.Context, candidate ConfigSnapshot) error {
	tmp, err := os.MkdirTemp("", "xkeen-native-config-*")
	if err != nil {
		return ErrConfig
	}
	defer os.RemoveAll(tmp)
	for name, data := range candidate.files {
		if name == registryConfigID {
			continue
		}
		if os.WriteFile(filepath.Join(tmp, name), data, 0600) != nil {
			return ErrConfig
		}
	}
	validateCtx, cancel := context.WithTimeout(ctx, e.validationTimeout())
	defer cancel()
	command := exec.CommandContext(validateCtx, e.XrayBinary, "run", "-test", "-confdir", tmp)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "XRAY_LOCATION_ASSET=") {
			command.Env = append(command.Env, entry)
		}
	}
	if e.AssetDir != "" {
		command.Env = append(command.Env, "XRAY_LOCATION_ASSET="+e.AssetDir)
	}
	output := &validationOutput{}
	command.Stdout, command.Stderr = output, output
	if command.Run() != nil {
		return &ValidationError{Output: string(output.data), Truncated: output.truncated}
	}
	if e.ValidateDerived != nil {
		if err := e.ValidateDerived(ctx, candidate.NativeDocuments()); err != nil {
			return err
		}
	}
	return nil
}

// Caller holds the ordinary panel lease. External CLI/cron are not serialized.
func (e *ConfigEditor) saveCandidate(ctx context.Context, before ConfigSnapshot, changes map[string][]byte) (string, error) {
	after, err := candidateSet(before, changes)
	if err != nil {
		return "", err
	}
	if after.Digest == before.Digest {
		return before.Digest, nil
	}
	if err := e.validateSet(ctx, after); err != nil {
		if detail, ok := err.(*ValidationError); ok && len(changes) == 1 {
			for name := range changes {
				detail.File = name
			}
		}
		return "", err
	}
	current, err := e.Snapshot(ctx)
	if err != nil || current.Digest != before.Digest {
		return "", ErrConfig
	}
	if e.stagePendingSet(before, changes) != nil {
		return "", ErrConfig
	}
	return e.promoteSet(ctx, before, after)
}

// All temporary files are prepared before promotion. Native files switch by
// atomic rename individually; the running core does not reload until Restart.
// Partial failures retain originals and are visible as pending drift, not success.
func (e *ConfigEditor) promoteSet(ctx context.Context, before, after ConfigSnapshot) (string, error) {
	temps := map[string]string{}
	defer func() {
		for _, path := range temps {
			_ = os.Remove(path)
		}
	}()
	names := []string{}
	all := make(map[string]bool, len(before.files)+len(after.files))
	for name := range before.files {
		all[name] = true
	}
	for name := range after.files {
		all[name] = true
	}
	for name := range all {
		data := after.files[name]
		if bytes.Equal(data, before.files[name]) {
			continue
		}
		path := e.configPath(name)
		if name == registryConfigID && e.prepareRegistryDirectory() != nil {
			return "", ErrConfig
		}
		if data == nil || name == registryConfigID && bytes.Equal(data, absentRegistry) {
			names = append(names, name)
			continue
		}
		f, err := os.CreateTemp(filepath.Dir(path), ".panel-config-*")
		if err != nil {
			return "", ErrConfig
		}
		temps[name] = f.Name()
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return "", ErrConfig
		}
		names = append(names, name)
	}
	sort.Strings(names)
	expected := before.Digest
	working := make(map[string][]byte, len(before.files))
	for name, data := range before.files {
		working[name] = data
	}
	for _, name := range names {
		current, err := e.Snapshot(ctx)
		if err != nil || current.Digest != expected {
			return "", ErrConfig
		}
		var promoteErr error
		if after.files[name] == nil || name == registryConfigID && bytes.Equal(after.files[name], absentRegistry) {
			promoteErr = os.Remove(e.configPath(name))
			if os.IsNotExist(promoteErr) {
				promoteErr = nil
			}
		} else {
			promoteErr = os.Rename(temps[name], e.configPath(name))
		}
		if promoteErr != nil {
			return "", ErrConfig
		}
		if after.files[name] == nil {
			delete(working, name)
		} else {
			working[name] = after.files[name]
		}
		expected = configDigest(working)
	}
	if syncConfigDirectory(e.Dir) != nil {
		return "", ErrConfig
	}
	if e.RegistryPath != "" && all[registryConfigID] && !bytes.Equal(before.files[registryConfigID], after.files[registryConfigID]) && syncConfigDirectory(filepath.Dir(e.RegistryPath)) != nil {
		return "", ErrConfig
	}
	readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	actual, err := e.Snapshot(readCtx)
	if err != nil || actual.Digest != after.Digest {
		return "", ErrConfig
	}
	return actual.Digest, nil
}

func syncConfigDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return ErrConfig
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return ErrConfig
	}
	return nil
}
