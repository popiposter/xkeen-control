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
	Expected string            `json:"expected"`
	Original map[string][]byte `json:"original"` // JSON base64 is lossless and bounded
}
type PendingConfiguration struct {
	Files []string `json:"files"`
	Drift bool     `json:"drift"`
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
	if e.PreviousDir == "" {
		return nil, ErrConfig
	}
	path := filepath.Join(e.PreviousDir, "pending.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	parent, parentErr := os.Lstat(e.PreviousDir)
	if err != nil || parentErr != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, ErrConfig
	}
	data, state := (Discovery{Root: e.PreviousDir}).read("pending.json", 16<<20)
	if state != CapabilityAvailable {
		return nil, ErrConfig
	}
	var pending pendingConfig
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&pending) != nil || d.Decode(&struct{}{}) != io.EOF || len(pending.Expected) != 64 || len(pending.Original) == 0 || len(pending.Original) > 7 {
		return nil, ErrConfig
	}
	if _, err := hex.DecodeString(pending.Expected); err != nil {
		return nil, ErrConfig
	}
	total := 0
	for name, text := range pending.Original {
		if !editableConfig(name) || len(text) > maxNativeConfig {
			return nil, ErrConfig
		}
		total += len(text)
	}
	if total > 8<<20 {
		return nil, ErrConfig
	}
	return &pending, nil
}
func (e *ConfigEditor) stagePending(before ConfigSnapshot, file string, candidate []byte) error {
	pending, err := e.readPending()
	if err != nil {
		return err
	}
	if pending == nil {
		pending = &pendingConfig{Original: map[string][]byte{}}
	} else if pending.Expected != before.Digest {
		return ErrConfig
	}
	if _, exists := pending.Original[file]; !exists {
		pending.Original[file] = before.files[file]
	}
	files := make(map[string][]byte, len(before.files))
	for name, data := range before.files {
		files[name] = data
	}
	files[file] = candidate
	pending.Expected = configDigest(files)
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
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), filepath.Join(e.PreviousDir, "pending.json")) != nil {
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
	status := &PendingConfiguration{Files: []string{}, Drift: pending.Expected != snapshot.Digest}
	for name, original := range pending.Original {
		if !bytes.Equal(snapshot.files[name], original) {
			status.Files = append(status.Files, name)
		}
	}
	sort.Strings(status.Files)
	return status, nil
}
