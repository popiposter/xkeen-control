package xkeen

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

var ErrAttachmentRecovery = errors.New("native attachment needs inspection; do not replay")

// AttachStopped is the initial operator-only connection step. The operator must
// hold exclusive maintenance against native CLI/cron. It never starts a service
// and is deliberately not exposed as a general HTTP config mutation.
func (d Discovery) AttachStopped(ctx context.Context, expected string) (AttachmentCheck, error) {
	return d.attachStopped(ctx, expected, d.validateAttachment)
}

func (d Discovery) attachStopped(ctx context.Context, expected string, validate func(context.Context, string) error) (result AttachmentCheck, err error) {
	return d.attachStoppedWithWriter(ctx, expected, validate, writeAttachmentFile)
}

func (d Discovery) attachStoppedWithWriter(ctx context.Context, expected string, validate func(context.Context, string) error, write func(string, []byte) error) (result AttachmentCheck, err error) {
	for _, name := range []string{"opt/etc", "opt/etc/xray", "opt/etc/xray/configs"} {
		info, err := os.Lstat(d.path(name))
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 || !attachmentDirectoryOwned(info) {
			return result, ErrAttachmentConflict
		}
	}
	digest, decodeErr := hex.DecodeString(expected)
	if decodeErr != nil || len(digest) != 32 {
		return result, ErrAttachmentConflict
	}
	facts := d.Inspect(ctx)
	if facts.Installation != CapabilityAvailable || facts.Lifecycle != CapabilityAvailable || facts.Core != "xray" || !d.stoppedForAttachment(ctx) || !facts.NeedsOnboarding {
		return result, ErrAttachmentConflict
	}
	result, err = d.checkAttachment(ctx, validate)
	if err != nil {
		return result, err
	}
	if result.SourceSHA256 != expected {
		return AttachmentCheck{}, ErrAttachmentConflict
	}
	files, current, err := d.attachmentSource(ctx)
	if err != nil || current != expected {
		return AttachmentCheck{}, ErrAttachmentConflict
	}
	changes, err := BuildAttachment(files)
	if err != nil {
		return AttachmentCheck{}, err
	}
	stateDir := d.path("opt/etc/xkeen-control/state/native-attachment")
	// A fresh exclusive directory is both the receipt boundary and snapshot.
	for _, name := range []string{"opt/etc/xkeen-control", "opt/etc/xkeen-control/state"} {
		path := d.path(name)
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return AttachmentCheck{}, ErrAttachmentRecovery
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || !attachmentDirectoryOwned(info) || syncAttachmentDirectory(filepath.Dir(path)) != nil {
			return AttachmentCheck{}, ErrAttachmentRecovery
		}
	}
	if err := os.Mkdir(stateDir, 0700); err != nil {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	if syncAttachmentDirectory(filepath.Dir(stateDir)) != nil {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	intent, _ := json.Marshal(struct{ State, SourceSHA256 string }{"prepared", expected})
	if err := write(filepath.Join(stateDir, "receipt.json"), intent); err != nil {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	for name, data := range files {
		if err := write(filepath.Join(stateDir, name), data); err != nil {
			return AttachmentCheck{}, ErrAttachmentRecovery
		}
	}
	_, current, err = d.attachmentSource(ctx)
	if err != nil || current != expected || !d.stoppedForAttachment(ctx) || syncAttachmentDirectory(stateDir) != nil {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	names := make([]string, 0, len(changes))
	for name := range changes {
		names = append(names, name)
	}
	sort.Strings(names)
	mutated := false
	candidateVerified := false
	defer func() {
		if err == nil || !mutated || candidateVerified {
			return
		}
		if !d.stoppedForAttachment(context.Background()) {
			err = ErrAttachmentRecovery
			return
		}
		// Xray was stopped throughout this exclusive initial operation. Restore
		// only bytes still owned by this candidate; never overwrite new drift.
		for _, name := range names {
			if !d.stoppedForAttachment(context.Background()) {
				err = ErrAttachmentRecovery
				return
			}
			path := d.path("opt/etc/xray/configs/" + name)
			active, state := d.read("opt/etc/xray/configs/"+name, maxNativeConfig)
			old, existed := files[name]
			if state == CapabilityMissing && !existed {
				continue
			}
			if state != CapabilityAvailable || (!bytes.Equal(active, changes[name]) && !bytes.Equal(active, old)) {
				err = ErrAttachmentRecovery
				return
			}
			if existed {
				if writeAttachmentFile(path, old) != nil {
					err = ErrAttachmentRecovery
					return
				}
			} else if os.Remove(path) != nil || syncAttachmentDirectory(filepath.Dir(path)) != nil {
				err = ErrAttachmentRecovery
				return
			}
		}
		// Receipt/snapshot remain for explicit inspection even after restoration.
		err = ErrAttachmentRecovery
	}()
	for _, name := range names {
		if ctx.Err() != nil || !d.stoppedForAttachment(ctx) {
			return AttachmentCheck{}, ErrAttachmentRecovery
		}
		mutated = true
		if write(d.path("opt/etc/xray/configs/"+name), changes[name]) != nil {
			return AttachmentCheck{}, ErrAttachmentRecovery
		}
	}
	activeFiles, _, readErr := d.attachmentSource(ctx)
	expectedFiles := make(map[string][]byte, len(files)+len(changes))
	for name, data := range files {
		expectedFiles[name] = data
	}
	for name, data := range changes {
		expectedFiles[name] = data
	}
	if readErr != nil || len(activeFiles) != len(expectedFiles) {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	for name, expectedData := range expectedFiles {
		if !bytes.Equal(activeFiles[name], expectedData) {
			return AttachmentCheck{}, ErrAttachmentRecovery
		}
	}
	if d.Inspect(ctx).PanelIntegration != CapabilityAvailable || !d.stoppedForAttachment(ctx) {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	// Receipt replacement may succeed before its directory sync reports failure.
	// Keep the verified candidate on any final receipt error; reverting here
	// could leave a committed-stopped receipt describing the wrong configuration.
	candidateVerified = true
	complete, _ := json.Marshal(struct {
		State string
		Check AttachmentCheck
	}{"committed-stopped", result})
	if write(filepath.Join(stateDir, "receipt.json"), complete) != nil {
		return AttachmentCheck{}, ErrAttachmentRecovery
	}
	return result, nil
}

func writeAttachmentFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".native-attachment-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncAttachmentDirectory(filepath.Dir(path))
}
