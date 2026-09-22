// Package performancepolicy owns the bounded persisted performance-policy
// authority and its session-bound Preview/Apply broker. It deliberately has no
// generic settings, URL, schedule, transfer-budget or selection-write surface.
package performancepolicy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

const (
	DefaultPath        = "/opt/etc/xkeen-control/state/performance-policy.json"
	MaxPolicyBytes     = 4 << 10
	DefaultPreviewTTL  = 5 * time.Minute
	DefaultMaxPreviews = 4
)

var (
	ErrUnavailable    = errors.New("performance policy unavailable")
	ErrInvalidRequest = errors.New("performance policy request is invalid")
	ErrPreviewExpired = errors.New("performance policy preview is expired or invalid")
	ErrPreviewStale   = errors.New("performance policy preview is stale")
	ErrBusy           = errors.New("performance policy is busy")
	ErrSave           = errors.New("performance policy could not be saved")
)

type Source string

const (
	SourceDefault   Source = "default"
	SourcePersisted Source = "persisted"
)

type HardCeilings struct {
	MaxCandidates        int    `json:"maxCandidates"`
	CandidateDownloadMiB int    `json:"candidateDownloadMiB"`
	CandidateUploadMiB   int    `json:"candidateUploadMiB"`
	CandidateMaxSeconds  int    `json:"candidateMaxSeconds"`
	GenerationMaxMiB     int    `json:"generationMaxMiB"`
	GenerationMaxSeconds int    `json:"generationMaxSeconds"`
	TransportIdentity    string `json:"transportIdentity"`
	RTTGuard             string `json:"rttGuard"`
	Scoring              string `json:"scoring"`
}

type Projection struct {
	Policy       c1.PerformancePolicy         `json:"policy"`
	Source       Source                       `json:"source"`
	ReasonCode   string                       `json:"reasonCode,omitempty"`
	HardCeilings HardCeilings                 `json:"hardCeilings"`
	Adaptive     c1.AdaptivePerformanceStatus `json:"adaptive"`
}

type FieldChange struct {
	Field  string `json:"field"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

type Preview struct {
	Token              string               `json:"previewToken"`
	ExpiresAt          time.Time            `json:"expiresAt"`
	Before             c1.PerformancePolicy `json:"before"`
	After              c1.PerformancePolicy `json:"after"`
	Changes            []FieldChange        `json:"changes"`
	Noop               bool                 `json:"noop"`
	RestartRequired    bool                 `json:"restartRequired"`
	NextRunTimeChanges bool                 `json:"nextRunTimeChanges"`
}

type ApplyResult struct {
	Policy             c1.PerformancePolicy `json:"policy"`
	Source             Source               `json:"source"`
	Changes            []FieldChange        `json:"changes"`
	Noop               bool                 `json:"noop"`
	RestartRequired    bool                 `json:"restartRequired"`
	NextRunTimeChanged bool                 `json:"nextRunTimeChanged"`
}

type Runtime interface {
	InitializePerformancePolicy(c1.PerformancePolicy, time.Time) error
	CommitPerformancePolicy(context.Context, c1.PerformancePolicy, time.Time, bool, func() error) error
	AdaptiveSnapshot() c1.AdaptivePerformanceStatus
}

type Config struct {
	Path        string
	Runtime     Runtime
	PreviewTTL  time.Duration
	MaxPreviews int
	Now         func() time.Time
	Random      io.Reader
}

type Service struct {
	config   Config
	mu       sync.Mutex
	previews map[string]previewEntry
}

type previewEntry struct {
	Binding     string
	Candidate   c1.PerformancePolicy
	Fingerprint string
	Changes     []FieldChange
	Noop        bool
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type fileState struct {
	policy      c1.PerformancePolicy
	source      Source
	reasonCode  string
	valid       bool
	fingerprint string
}

func NewService(config Config) *Service {
	if config.Path == "" {
		config.Path = DefaultPath
	}
	if config.PreviewTTL <= 0 || config.PreviewTTL > DefaultPreviewTTL {
		config.PreviewTTL = DefaultPreviewTTL
	}
	if config.MaxPreviews <= 0 || config.MaxPreviews > DefaultMaxPreviews {
		config.MaxPreviews = DefaultMaxPreviews
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &Service{config: config, previews: make(map[string]previewEntry)}
}

// InitializeRuntime installs the fail-closed startup snapshot before the C.1
// loops start. It never writes or repairs the policy file.
func (s *Service) InitializeRuntime() error {
	if s == nil || s.config.Runtime == nil {
		return ErrUnavailable
	}
	state := readPolicy(s.config.Path)
	if err := s.config.Runtime.InitializePerformancePolicy(state.policy, s.config.Now().UTC()); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) Read(context.Context) (Projection, error) {
	if s == nil {
		return Projection{}, ErrUnavailable
	}
	state := readPolicy(s.config.Path)
	adaptive := c1.DefaultAdaptivePerformanceStatus()
	if s.config.Runtime != nil {
		adaptive = s.config.Runtime.AdaptiveSnapshot()
	}
	return Projection{Policy: state.policy, Source: state.source, ReasonCode: state.reasonCode, HardCeilings: hardCeilings(), Adaptive: adaptive}, nil
}

func (s *Service) Preview(_ context.Context, binding string, candidate c1.PerformancePolicy) (Preview, error) {
	if s == nil || binding == "" || c1.ValidatePerformancePolicy(candidate) != nil {
		return Preview{}, ErrInvalidRequest
	}
	current := readPolicy(s.config.Path)
	changes := semanticChanges(current.policy, candidate)
	// An invalid authority may always be explicitly replaced, including with
	// the exact fail-closed defaults. An absent/default authority is a no-op.
	noop := current.valid && len(changes) == 0
	created := s.config.Now().UTC()
	expires := created.Add(s.config.PreviewTTL)
	token, err := s.randomToken()
	if err != nil {
		return Preview{}, ErrUnavailable
	}
	entry := previewEntry{Binding: binding, Candidate: candidate, Fingerprint: current.fingerprint, Changes: changes, Noop: noop, CreatedAt: created, ExpiresAt: expires}
	s.mu.Lock()
	s.purgeExpiredLocked(created)
	for oldToken, old := range s.previews {
		if old.Binding == binding {
			delete(s.previews, oldToken)
		}
	}
	for len(s.previews) >= s.config.MaxPreviews {
		s.evictOldestLocked()
	}
	s.previews[token] = entry
	s.mu.Unlock()
	return Preview{Token: token, ExpiresAt: expires, Before: current.policy, After: candidate, Changes: changes, Noop: noop, RestartRequired: false, NextRunTimeChanges: !noop}, nil
}

func (s *Service) Apply(ctx context.Context, binding, token string) (ApplyResult, error) {
	if s == nil || s.config.Runtime == nil {
		return ApplyResult{}, ErrUnavailable
	}
	entry, ok := s.takePreview(binding, token)
	if !ok {
		return ApplyResult{}, ErrPreviewExpired
	}
	if readPolicy(s.config.Path).fingerprint != entry.Fingerprint {
		return ApplyResult{}, ErrPreviewStale
	}
	appliedAt := s.config.Now().UTC()
	persist := func() error {
		current := readPolicy(s.config.Path)
		if current.fingerprint != entry.Fingerprint {
			return ErrPreviewStale
		}
		if entry.Noop {
			return nil
		}
		if err := writePolicyAtomic(s.config.Path, entry.Candidate); err != nil {
			return ErrSave
		}
		updated := readPolicy(s.config.Path)
		if !updated.valid || updated.source != SourcePersisted || updated.policy != entry.Candidate {
			return ErrSave
		}
		return nil
	}
	if err := s.config.Runtime.CommitPerformancePolicy(ctx, entry.Candidate, appliedAt, !entry.Noop, persist); err != nil {
		switch {
		case errors.Is(err, c1.ErrBenchmarkBusy), errors.Is(err, c1.ErrLifecycleBusy):
			return ApplyResult{}, ErrBusy
		case errors.Is(err, ErrPreviewStale):
			return ApplyResult{}, ErrPreviewStale
		case errors.Is(err, ErrSave):
			return ApplyResult{}, ErrSave
		default:
			return ApplyResult{}, ErrUnavailable
		}
	}
	source := SourcePersisted
	if entry.Noop {
		source = readPolicy(s.config.Path).source
	}
	return ApplyResult{Policy: entry.Candidate, Source: source, Changes: cloneChanges(entry.Changes), Noop: entry.Noop, RestartRequired: false, NextRunTimeChanged: !entry.Noop}, nil
}

func (s *Service) Cancel(binding, token string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if entry, ok := s.previews[token]; ok && entry.Binding == binding {
		delete(s.previews, token)
	}
	s.mu.Unlock()
}

func (s *Service) Invalidate(binding string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	for token, entry := range s.previews {
		if entry.Binding == binding {
			delete(s.previews, token)
		}
	}
	s.mu.Unlock()
}

func (s *Service) InvalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.previews = make(map[string]previewEntry)
	s.mu.Unlock()
}

func hardCeilings() HardCeilings {
	return HardCeilings{
		MaxCandidates:        c1.AdaptiveMaxCandidates,
		CandidateDownloadMiB: int(c1.AdaptiveMaxDownloadBytes / c1.MiB),
		CandidateUploadMiB:   int(c1.AdaptiveMaxUploadBytes / c1.MiB),
		CandidateMaxSeconds:  int(c1.AdaptiveMaxWallTime / time.Second),
		GenerationMaxMiB:     int(c1.AdaptiveMaxGenerationBytes / c1.MiB),
		GenerationMaxSeconds: int(c1.AdaptiveMaxGenerationWallTime / time.Second),
		TransportIdentity:    "source-owned",
		RTTGuard:             "source-owned",
		Scoring:              "source-owned",
	}
}

func semanticChanges(before, after c1.PerformancePolicy) []FieldChange {
	values := []FieldChange{
		{Field: "probeIntervalSeconds", Before: before.ProbeIntervalSeconds, After: after.ProbeIntervalSeconds},
		{Field: "failureThreshold", Before: before.FailureThreshold, After: after.FailureThreshold},
		{Field: "adaptiveCadenceMinutes", Before: before.AdaptiveCadenceMinutes, After: after.AdaptiveCadenceMinutes},
		{Field: "adaptiveChallengerLimit", Before: before.AdaptiveChallengerLimit, After: after.AdaptiveChallengerLimit},
		{Field: "minimumDwellMinutes", Before: before.MinimumDwellMinutes, After: after.MinimumDwellMinutes},
		{Field: "qualityHysteresisPercent", Before: before.QualityHysteresisPercent, After: after.QualityHysteresisPercent},
	}
	result := make([]FieldChange, 0, len(values))
	for _, value := range values {
		if value.Before != value.After {
			result = append(result, value)
		}
	}
	return result
}

// DecodeRequest validates the complete closed HTTP/file DTO. Callers cannot
// inject partial, duplicate, unknown or trailing fields.
func DecodeRequest(data []byte) (c1.PerformancePolicy, error) {
	policy, err := decodePolicy(data)
	if err != nil {
		return c1.PerformancePolicy{}, ErrInvalidRequest
	}
	return policy, nil
}

func decodePolicy(data []byte) (c1.PerformancePolicy, error) {
	if len(data) == 0 || len(data) > MaxPolicyBytes || rejectDuplicateFields(data) != nil {
		return c1.PerformancePolicy{}, c1.ErrInvalidPerformancePolicy
	}
	type wire struct {
		SchemaVersion            *int `json:"schemaVersion"`
		ProbeIntervalSeconds     *int `json:"probeIntervalSeconds"`
		FailureThreshold         *int `json:"failureThreshold"`
		AdaptiveCadenceMinutes   *int `json:"adaptiveCadenceMinutes"`
		AdaptiveChallengerLimit  *int `json:"adaptiveChallengerLimit"`
		MinimumDwellMinutes      *int `json:"minimumDwellMinutes"`
		QualityHysteresisPercent *int `json:"qualityHysteresisPercent"`
	}
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return c1.PerformancePolicy{}, c1.ErrInvalidPerformancePolicy
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF || value.SchemaVersion == nil || value.ProbeIntervalSeconds == nil || value.FailureThreshold == nil || value.AdaptiveCadenceMinutes == nil || value.AdaptiveChallengerLimit == nil || value.MinimumDwellMinutes == nil || value.QualityHysteresisPercent == nil {
		return c1.PerformancePolicy{}, c1.ErrInvalidPerformancePolicy
	}
	policy := c1.PerformancePolicy{
		SchemaVersion: *value.SchemaVersion, ProbeIntervalSeconds: *value.ProbeIntervalSeconds,
		FailureThreshold: *value.FailureThreshold, AdaptiveCadenceMinutes: *value.AdaptiveCadenceMinutes,
		AdaptiveChallengerLimit: *value.AdaptiveChallengerLimit, MinimumDwellMinutes: *value.MinimumDwellMinutes,
		QualityHysteresisPercent: *value.QualityHysteresisPercent,
	}
	if err := c1.ValidatePerformancePolicy(policy); err != nil {
		return c1.PerformancePolicy{}, err
	}
	return policy, nil
}

func rejectDuplicateFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return c1.ErrInvalidPerformancePolicy
	}
	allowed := map[string]struct{}{
		"schemaVersion": {}, "probeIntervalSeconds": {}, "failureThreshold": {},
		"adaptiveCadenceMinutes": {}, "adaptiveChallengerLimit": {}, "minimumDwellMinutes": {},
		"qualityHysteresisPercent": {},
	}
	seen := make(map[string]struct{}, len(allowed))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return c1.ErrInvalidPerformancePolicy
		}
		if _, ok := allowed[name]; !ok {
			return c1.ErrInvalidPerformancePolicy
		}
		if _, ok := seen[name]; ok {
			return c1.ErrInvalidPerformancePolicy
		}
		seen[name] = struct{}{}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return c1.ErrInvalidPerformancePolicy
	}
	return nil
}

func readPolicy(path string) fileState {
	defaults := c1.DefaultPerformancePolicy()
	if path == "" {
		path = DefaultPath
	}
	directory := filepath.Dir(path)
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
			return invalidState(defaults, "directory-unsafe", metadataFingerprint(info, "directory-unsafe"))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return invalidState(defaults, "directory-unavailable", "directory-unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileState{policy: defaults, source: SourceDefault, valid: true, fingerprint: "absent"}
		}
		return invalidState(defaults, "file-unavailable", "file-unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return invalidState(defaults, "policy-symlink", metadataFingerprint(info, "policy-symlink"))
	}
	if !info.Mode().IsRegular() {
		return invalidState(defaults, "policy-non-regular", metadataFingerprint(info, "policy-non-regular"))
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return invalidState(defaults, "policy-permissions", metadataFingerprint(info, "policy-permissions"))
	}
	if info.Size() > MaxPolicyBytes {
		return invalidState(defaults, "policy-too-large", metadataFingerprint(info, "policy-too-large"))
	}
	file, err := os.Open(path)
	if err != nil {
		return invalidState(defaults, "file-unavailable", metadataFingerprint(info, "file-unavailable"))
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() > MaxPolicyBytes {
		return invalidState(defaults, "policy-changed", metadataFingerprint(info, "policy-changed"))
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxPolicyBytes+1))
	if err != nil {
		return invalidState(defaults, "file-unavailable", metadataFingerprint(info, "file-unavailable"))
	}
	digest := sha256.Sum256(contents)
	fingerprint := hex.EncodeToString(digest[:])
	if len(contents) > MaxPolicyBytes {
		return invalidState(defaults, "policy-too-large", fingerprint)
	}
	policy, err := decodePolicy(contents)
	if err != nil {
		return invalidState(defaults, "policy-invalid", fingerprint)
	}
	return fileState{policy: policy, source: SourcePersisted, valid: true, fingerprint: fingerprint}
}

func invalidState(defaults c1.PerformancePolicy, reason, fingerprint string) fileState {
	return fileState{policy: defaults, source: SourceDefault, reasonCode: reason, fingerprint: fingerprint}
}

func metadataFingerprint(info os.FileInfo, reason string) string {
	if info == nil {
		return reason
	}
	value := fmt.Sprintf("%s:%d:%d:%d", reason, info.Mode(), info.Size(), info.ModTime().UnixNano())
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func writePolicyAtomic(path string, policy c1.PerformancePolicy) error {
	if c1.ValidatePerformancePolicy(policy) != nil {
		return ErrSave
	}
	contents, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return ErrSave
	}
	contents = append(contents, '\n')
	directory := filepath.Dir(path)
	if err := ensureDirectory(directory); err != nil {
		return ErrSave
	}
	temporary, err := os.CreateTemp(directory, ".xkeen-performance-policy-*")
	if err != nil {
		return ErrSave
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return ErrSave
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return ErrSave
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ErrSave
	}
	if err := temporary.Close(); err != nil {
		return ErrSave
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return ErrSave
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return ErrSave
	}
	if runtime.GOOS != "windows" {
		directoryHandle, err := os.Open(directory)
		if err != nil {
			return ErrSave
		}
		syncErr := directoryHandle.Sync()
		closeErr := directoryHandle.Close()
		if syncErr != nil || closeErr != nil {
			return ErrSave
		}
	}
	return nil
}

func ensureDirectory(path string) error {
	if path == "" || path == "." {
		return ErrSave
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrSave
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return ErrSave
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrSave
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return ErrSave
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrSave
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o700); err != nil {
			return ErrSave
		}
	}
	return nil
}

func (s *Service) randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(s.config.Random, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Service) takePreview(binding, token string) (previewEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.previews[token]
	if !ok || entry.Binding != binding || !s.config.Now().Before(entry.ExpiresAt) {
		if ok {
			delete(s.previews, token)
		}
		return previewEntry{}, false
	}
	delete(s.previews, token)
	return entry, true
}

func (s *Service) purgeExpiredLocked(now time.Time) {
	for token, entry := range s.previews {
		if !now.Before(entry.ExpiresAt) {
			delete(s.previews, token)
		}
	}
}

func (s *Service) evictOldestLocked() {
	oldestToken := ""
	var oldest time.Time
	for token, entry := range s.previews {
		if oldestToken == "" || entry.CreatedAt.Before(oldest) || (entry.CreatedAt.Equal(oldest) && token < oldestToken) {
			oldestToken, oldest = token, entry.CreatedAt
		}
	}
	if oldestToken != "" {
		delete(s.previews, oldestToken)
	}
}

func cloneChanges(changes []FieldChange) []FieldChange {
	return append([]FieldChange(nil), changes...)
}
