package xkeen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type AttachmentFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

type AttachmentCheck struct {
	Validated    bool             `json:"validated"`
	SourceSHA256 string           `json:"sourceSha256"`
	Changes      []AttachmentFile `json:"changes"`
}

// CheckAttachment validates a temporary complete config with the installed Xray.
// No native command, restart, registry or active configuration write occurs.
func (d Discovery) CheckAttachment(ctx context.Context) (AttachmentCheck, error) {
	return d.checkAttachment(ctx, func(ctx context.Context, candidate string) error {
		command := exec.CommandContext(ctx, d.path("opt/sbin/xray"), "run", "-test", "-confdir", candidate)
		command.Dir = candidate
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "XRAY_LOCATION_ASSET=") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, "XRAY_LOCATION_ASSET="+d.path("opt/etc/xray/dat"))
		command.Stdout, command.Stderr = io.Discard, io.Discard
		if command.Run() != nil {
			return errors.New("native attachment candidate failed Xray validation")
		}
		return nil
	})
}

func (d Discovery) checkAttachment(ctx context.Context, validate func(context.Context, string) error) (AttachmentCheck, error) {
	var result AttachmentCheck
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	files, digest, err := d.attachmentSource(ctx)
	if err != nil {
		return result, err
	}
	changes, err := BuildAttachment(files)
	if err != nil {
		return result, err
	}
	dir, err := os.MkdirTemp("", "xkeen-native-attachment-")
	if err != nil {
		return result, errors.New("unable to prepare native candidate")
	}
	defer os.RemoveAll(dir)
	for name, data := range changes {
		files[name] = data
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return result, errors.New("unable to prepare native candidate")
		}
	}
	if err := validate(ctx, dir); err != nil {
		return result, err
	}
	_, current, err := d.attachmentSource(ctx)
	if err != nil || current != digest {
		return result, errors.New("native configuration changed during attachment check")
	}
	result.Validated = true
	result.SourceSHA256 = digest
	for name, data := range changes {
		hash := sha256.Sum256(data)
		result.Changes = append(result.Changes, AttachmentFile{Name: name, SHA256: hex.EncodeToString(hash[:]), Size: len(data)})
	}
	sort.Slice(result.Changes, func(i, j int) bool { return result.Changes[i].Name < result.Changes[j].Name })
	return result, nil
}

func (d Discovery) attachmentSource(ctx context.Context) (map[string][]byte, string, error) {
	entries, err := d.entries("opt/etc/xray/configs", maxNativeConfigFiles)
	if err != nil {
		return nil, "", ErrAttachmentConflict
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	files := make(map[string][]byte)
	hash := sha256.New()
	total := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, state := d.read("opt/etc/xray/configs/"+name, maxNativeConfig)
		total += len(data)
		if state != CapabilityAvailable || total > 8<<20 {
			return nil, "", ErrAttachmentConflict
		}
		files[name] = data
		_, _ = hash.Write([]byte(name + "\x00"))
		digest := sha256.Sum256(data)
		_, _ = hash.Write(digest[:])
	}
	return files, hex.EncodeToString(hash.Sum(nil)), nil
}
