package xkeen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// One explicitly saved config set, not a native command journal. Original
// bytes survive repeated Save until the set is applied/discarded.
type pendingConfig struct {
	Expected        string            `json:"expected"`
	Original        map[string][]byte `json:"original"` // JSON base64 is lossless and bounded
	ApplyID         string            `json:"applyId,omitempty"`
	ApplyState      string            `json:"applyState,omitempty"`
	BeforeProcess   string            `json:"beforeProcess,omitempty"`
	RestartRequired bool              `json:"restartRequired,omitempty"`
}
type PendingConfiguration struct {
	Files           []string `json:"files"`
	Drift           bool     `json:"drift"`
	ApplyState      string   `json:"applyState,omitempty"`
	ApplyID         string   `json:"applyId,omitempty"`
	RestartRequired bool     `json:"restartRequired"`
}

func configDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(files[name])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (e *ConfigEditor) readPending() (*pendingConfig, error) {
	return e.readGeneration("pending.json")
}

// HasSavedChanges is a read-only check for other panel operations that would
// restart the core. The caller owns the shared lease; external CLI is separate.
func (e *ConfigEditor) HasSavedChanges() (bool, error) {
	pending, err := e.readPending()
	return pending != nil, err
}

// PendingDigest is non-secret bookkeeping for native Start/Restart cards.
// An unavailable editor history does not prevent ordinary native command use.
func (e *ConfigEditor) PendingDigest() (string, bool) {
	pending, err := e.readPending()
	if err != nil || pending == nil {
		return "", false
	}
	return pending.Expected, true
}

func (e *ConfigEditor) readGeneration(name string) (*pendingConfig, error) {
	if e.PreviousDir == "" {
		return nil, ErrConfig
	}
	path := filepath.Join(e.PreviousDir, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	parent, parentErr := os.Lstat(e.PreviousDir)
	if err != nil || parentErr != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, ErrConfig
	}
	data, state := (Discovery{Root: e.PreviousDir}).read(name, 16<<20)
	if state != CapabilityAvailable {
		return nil, ErrConfig
	}
	var pending pendingConfig
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&pending) != nil || d.Decode(&struct{}{}) != io.EOF || len(pending.Expected) != 64 || len(pending.Original) == 0 || len(pending.Original) > 9 {
		return nil, ErrConfig
	}
	if _, err := hex.DecodeString(pending.Expected); err != nil {
		return nil, ErrConfig
	}
	if pending.ApplyID != "" {
		id, err := hex.DecodeString(pending.ApplyID)
		if err != nil || len(id) != 16 {
			return nil, ErrConfig
		}
	}
	if pending.ApplyState != "" && pending.ApplyState != "running" && pending.ApplyState != "failed" && pending.ApplyState != "unknown" && pending.ApplyState != "restored" {
		return nil, ErrConfig
	}
	if len(pending.BeforeProcess) > 256 {
		return nil, ErrConfig
	}
	total := 0
	for name, text := range pending.Original {
		limit := maxNativeConfig
		if name == registryConfigID {
			limit = 4 << 20
		}
		if !generationConfig(name) || name == registryConfigID && e.RegistryPath == "" || len(text) > limit {
			return nil, ErrConfig
		}
		total += len(text)
	}
	if total > 8<<20 {
		return nil, ErrConfig
	}
	return &pending, nil
}
func (e *ConfigEditor) stagePendingSet(before ConfigSnapshot, changes map[string][]byte) error {
	pending, err := e.readPending()
	if err != nil {
		return err
	}
	if pending == nil {
		pending = &pendingConfig{Original: map[string][]byte{}}
	} else if pending.Expected != before.Digest {
		return ErrConfig
	}
	if pending.ApplyState == "running" || pending.ApplyState == "unknown" {
		return ErrConfig
	}
	for file, candidate := range changes {
		if bytes.Equal(candidate, before.files[file]) {
			continue
		}
		if _, exists := pending.Original[file]; !exists {
			pending.Original[file] = before.files[file]
		}
	}
	files := make(map[string][]byte, len(before.files))
	for name, data := range before.files {
		files[name] = data
	}
	for file, candidate := range changes {
		if candidate == nil && file != registryConfigID {
			delete(files, file)
		} else {
			files[file] = candidate
		}
	}
	pending.Expected = configDigest(files)
	return e.writeGeneration("pending.json", pending)
}
func (e *ConfigEditor) writeGeneration(name string, pending *pendingConfig) error {
	if os.MkdirAll(e.PreviousDir, 0700) != nil {
		return ErrConfig
	}
	info, err := os.Lstat(e.PreviousDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrConfig
	}
	data, err := json.Marshal(pending)
	total := 0
	for _, original := range pending.Original {
		total += len(original)
	}
	if err != nil || total > 8<<20 || len(data) > 16<<20 {
		return ErrConfig
	}
	f, err := os.CreateTemp(e.PreviousDir, ".pending-*")
	if err != nil {
		return ErrConfig
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), filepath.Join(e.PreviousDir, name)) != nil {
		return ErrConfig
	}
	dir, err := os.Open(e.PreviousDir)
	if err != nil {
		return ErrConfig
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return ErrConfig
	}
	return nil
}
func (e *ConfigEditor) pendingStatus(ctx context.Context, snapshot ConfigSnapshot) (*PendingConfiguration, error) {
	if ctx.Err() != nil {
		return nil, ErrConfig
	}
	pending, err := e.readPending()
	if err != nil || pending == nil {
		return nil, err
	}
	status := &PendingConfiguration{Files: []string{}, Drift: pending.Expected != snapshot.Digest, ApplyID: pending.ApplyID, ApplyState: pending.ApplyState, RestartRequired: pending.RestartRequired}
	for name, original := range pending.Original {
		if !bytes.Equal(snapshot.files[name], original) {
			status.Files = append(status.Files, name)
		}
	}
	sort.Strings(status.Files)
	status.RestartRequired = status.RestartRequired || len(status.Files) > 0
	return status, nil
}
