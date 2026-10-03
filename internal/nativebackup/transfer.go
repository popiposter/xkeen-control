package nativebackup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

var ErrPreview = errors.New("native transfer preview is unavailable or changed")

// Only one bounded private preview is retained, in RAM and for one session.
// Stage consumes it before starting any write; it never starts a native command.
type privatePreview struct {
	token, binding, baseline string
	bundle                   Bundle
	expires                  time.Time
	timer                    *time.Timer
}
type TransferPreview struct {
	Token           string               `json:"token"`
	Digest          string               `json:"digest"`
	Files           []string             `json:"files"`
	Nodes           int                  `json:"nodes"`
	Subscriptions   int                  `json:"subscriptions"`
	Interfaces      []string             `json:"interfaces"`
	ExpiresAt       string               `json:"expiresAt"`
	References      []InterfaceReference `json:"references"`
	MappingRequired bool                 `json:"mappingRequired"`
}

type InterfaceReference struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

func clearBundle(bundle *Bundle) {
	for name, data := range bundle.Files {
		clear(data)
		delete(bundle.Files, name)
	}
	clear(bundle.Registry)
	bundle.Registry = nil
}

func (s *Service) dropPreviewLocked() {
	if s.preview != nil {
		if s.preview.timer != nil {
			s.preview.timer.Stop()
		}
		clearBundle(&s.preview.bundle)
		s.preview = nil
	}
}

func (s *Service) Preview(ctx context.Context, binding string, archive []byte, passphrase string, mapping map[string]string) (TransferPreview, error) {
	if s == nil || s.Editor == nil || s.Lease == nil || binding == "" {
		return TransferPreview{}, backup.ErrUnavailable
	}
	s.mu.Lock()
	if s.preview != nil && time.Now().After(s.preview.expires) {
		s.dropPreviewLocked()
	}
	if s.previewBusy || s.preview != nil && s.preview.binding != binding {
		s.mu.Unlock()
		return TransferPreview{}, backup.ErrBusy
	}
	s.previewBusy = true
	s.dropPreviewLocked()
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.previewBusy = false; s.mu.Unlock() }()
	bundle, err := Open(archive, passphrase)
	if err != nil {
		return TransferPreview{}, err
	}
	retained := false
	defer func() {
		if !retained {
			clearBundle(&bundle)
		}
	}()
	interfaces, err := s.availableInterfaces()
	if err != nil {
		return TransferPreview{}, backup.ErrUnavailable
	}
	registry, err := nodes.ParseCanonical(bundle.Registry)
	if err != nil {
		return TransferPreview{}, backup.ErrInvalidBundle
	}
	files := make([]string, 0, len(bundle.Files))
	for name := range bundle.Files {
		files = append(files, name)
	}
	sort.Strings(files)
	value := TransferPreview{Files: files, Nodes: len(registry.Nodes), Subscriptions: len(registry.Subscriptions), Interfaces: interfaces, References: []InterfaceReference{}}
	refs, err := interfaceReferences(bundle.Files)
	if err != nil || len(mapping) > 64 {
		return TransferPreview{}, backup.ErrInvalidBundle
	}
	known, used := map[string]bool{}, map[string]bool{}
	for _, name := range interfaces {
		known[name] = true
	}
	for _, source := range refs {
		target := source
		if to, exists := mapping[source]; exists {
			target = to
			used[source] = true
		}
		if len(target) > 64 {
			return TransferPreview{}, backup.ErrInvalidBundle
		}
		if !known[target] {
			target = ""
			value.MappingRequired = true
		}
		value.References = append(value.References, InterfaceReference{Source: source, Destination: target})
	}
	for from := range mapping {
		if !used[from] {
			return TransferPreview{}, backup.ErrInvalidBundle
		}
	}
	if value.MappingRequired {
		return value, nil
	}
	if err := mapInterfaces(bundle.Files, mapping, interfaces); err != nil {
		return TransferPreview{}, err
	}
	if validate(bundle) != nil {
		return TransferPreview{}, backup.ErrInvalidBundle
	}
	release, err := s.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return TransferPreview{}, backup.ErrUnavailable
	}
	defer release()
	baseline, err := s.Editor.PreviewTransferUnderLease(ctx, bundle.Files, bundle.Registry)
	if err != nil {
		return TransferPreview{}, err
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return TransferPreview{}, backup.ErrRandomUnavailable
	}
	expires := time.Now().Add(5 * time.Minute)
	value.Token, value.Digest, value.ExpiresAt = hex.EncodeToString(token), baseline, expires.UTC().Format(time.RFC3339)
	s.mu.Lock()
	s.preview = &privatePreview{token: value.Token, binding: binding, baseline: baseline, bundle: bundle, expires: expires}
	s.preview.timer = time.AfterFunc(5*time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.preview != nil && s.preview.token == value.Token {
			s.dropPreviewLocked()
		}
	})
	s.mu.Unlock()
	retained = true
	return value, nil
}

// Acknowledgement covers the destination's native mode, client policy and
// schedules, which are not copied from sourced scripts or kernel state.
func (s *Service) Stage(ctx context.Context, binding, token string, nativeSettingsChecked bool) (string, error) {
	if s == nil || !nativeSettingsChecked {
		return "", ErrPreview
	}
	s.mu.Lock()
	preview := s.preview
	if s.previewBusy || preview == nil || preview.token != token || preview.binding != binding || time.Now().After(preview.expires) {
		if preview != nil && time.Now().After(preview.expires) {
			s.dropPreviewLocked()
		}
		s.mu.Unlock()
		return "", ErrPreview
	}
	s.preview = nil
	if preview.timer != nil {
		preview.timer.Stop()
	}
	s.mu.Unlock()
	defer clearBundle(&preview.bundle)
	release, err := s.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return "", err
	}
	defer release()
	return s.Editor.StageTransferUnderLease(ctx, preview.baseline, preview.bundle.Files, preview.bundle.Registry)
}

func (s *Service) Cancel(binding, token string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview != nil && s.preview.binding == binding && s.preview.token == token {
		s.dropPreviewLocked()
	}
}

func (s *Service) availableInterfaces() ([]string, error) {
	if s.Interfaces != nil {
		return s.Interfaces()
	}
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) > 256 {
		return nil, backup.ErrUnavailable
	}
	result := make([]string, 0, len(interfaces))
	for _, value := range interfaces {
		result = append(result, value.Name)
	}
	sort.Strings(result)
	return result, nil
}
