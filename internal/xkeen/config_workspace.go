package xkeen

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

// IDs are native data templates only. Generated outbounds use the node editor.
func editableConfig(id string) bool {
	switch id {
	case "01_log.json", "02_dns.json", "03_inbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json":
		return true
	}
	return false
}

type EditorDocument struct {
	Text  string  `json:"text,omitempty"`
	Draft *string `json:"draft,omitempty"`
}
type EditorWorkspace struct {
	Digest        string                    `json:"digest"`
	Documents     map[string]EditorDocument `json:"documents"`
	Pending       *PendingConfiguration     `json:"pending,omitempty"`
	HasPrevious   bool                      `json:"hasPrevious"`
	PreviousDrift bool                      `json:"previousDrift"`
	Targets       []ConfigTarget            `json:"targets,omitempty"`
}

// Explicit private editor metadata: tags only, never outbound credentials.
type ConfigTarget struct {
	Tag      string `json:"tag"`
	Kind     string `json:"kind"`
	Protocol string `json:"protocol,omitempty"`
}

// Workspace is PRIVATE and can contain native secrets. No status projection
// embeds it. Reads do not create drafts, backups or change native files.
func (e *ConfigEditor) Workspace(ctx context.Context) (EditorWorkspace, error) {
	snapshot, err := e.Snapshot(ctx)
	if err != nil {
		return EditorWorkspace{}, err
	}
	w := EditorWorkspace{Digest: snapshot.Digest, Documents: map[string]EditorDocument{}}
	for name, data := range snapshot.files {
		if name == registryConfigID {
			continue
		}
		object, err := configjson.DecodeObject(data)
		if err != nil {
			return EditorWorkspace{}, ErrConfig
		}
		var outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
		}
		if raw, ok := object["outbounds"]; ok && json.Unmarshal(raw, &outbounds) == nil {
			for _, outbound := range outbounds {
				if outbound.Tag != "" && len(outbound.Tag) <= 128 && len(outbound.Protocol) <= 32 && len(w.Targets) < 256 {
					w.Targets = append(w.Targets, ConfigTarget{outbound.Tag, "outbound", outbound.Protocol})
				}
			}
		}
	}
	sort.Slice(w.Targets, func(i, j int) bool { return w.Targets[i].Tag < w.Targets[j].Tag })
	w.Pending, err = e.pendingStatus(ctx, snapshot)
	if err != nil {
		return EditorWorkspace{}, err
	}
	previous, err := e.readGeneration("previous.json")
	if err != nil {
		return EditorWorkspace{}, err
	}
	w.HasPrevious = previous != nil
	w.PreviousDrift = previous != nil && previous.Expected != snapshot.Digest && w.Pending == nil
	if e.DraftDir != "" {
		info, err := os.Lstat(e.DraftDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) || err == nil && (!info.IsDir() || info.Mode().Perm() != 0700) {
			return EditorWorkspace{}, ErrConfig
		}
	}
	for id, data := range snapshot.files {
		if !editableConfig(id) {
			continue
		}
		doc := EditorDocument{Text: string(data)}
		if e.DraftDir != "" {
			info, err := os.Lstat(filepath.Join(e.DraftDir, id))
			if err != nil && !errors.Is(err, os.ErrNotExist) || err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
				return EditorWorkspace{}, ErrConfig
			}
			draft, state := (Discovery{Root: e.DraftDir}).read(id, maxNativeConfig)
			if state == CapabilityAvailable {
				text := string(draft)
				doc.Draft = &text
			} else if state != CapabilityMissing {
				return EditorWorkspace{}, ErrConfig
			}
		}
		w.Documents[id] = doc
	}
	return w, nil
}

// SaveDraft accepts unfinished text but never touches executable native config.
// Drafts are explicit bounded private router storage, not browser persistence.
func (e *ConfigEditor) SaveDraft(ctx context.Context, id, text string, discard bool) error {
	if !editableConfig(id) || len(text) > maxNativeConfig || e.DraftDir == "" {
		return ErrConfig
	}
	release, err := e.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return err
	}
	defer release()
	if os.MkdirAll(e.DraftDir, 0700) != nil {
		return ErrConfig
	}
	info, err := os.Lstat(e.DraftDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrConfig
	}
	path := filepath.Join(e.DraftDir, id)
	if discard {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrConfig
		}
	} else {
		f, err := os.CreateTemp(e.DraftDir, ".draft-*")
		if err != nil {
			return ErrConfig
		}
		defer os.Remove(f.Name())
		_, writeErr := f.WriteString(text)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), path) != nil {
			return ErrConfig
		}
	}
	dir, err := os.Open(e.DraftDir)
	if err != nil {
		return ErrConfig
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return ErrConfig
	}
	return nil
}

type ValidationError struct {
	File      string `json:"file"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

func (*ValidationError) Error() string { return "native candidate validation failed" }

// Captures the first 256 KiB, drains the remainder without blocking Xray, and
// explicitly reports truncation. Diagnostics never enter persistent receipts.
type validationOutput struct {
	data      []byte
	truncated bool
}

func (w *validationOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (256 << 10) - len(w.data)
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	w.data = append(w.data, p...)
	return n, nil
}
