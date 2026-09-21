// Package dnsobservatory owns the closed backend DNS and Observatory broker.
// It compiles only the typed projections supported by appliance v1 and sends
// settings Apply through restore's existing D.1 transaction owner.
package dnsobservatory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/restore"
)

const (
	DefaultPreviewTTL  = 5 * time.Minute
	DefaultMaxPreviews = 4
)

var (
	ErrUnavailable         = errors.New("DNS and Observatory policy unavailable")
	ErrInvalidRequest      = errors.New("DNS and Observatory policy request is invalid")
	ErrDriftDetected       = errors.New("DNS and Observatory policy drift detected")
	ErrPreviewExpired      = errors.New("DNS and Observatory policy preview is expired or invalid")
	ErrPreviewStale        = errors.New("DNS and Observatory policy preview is stale")
	ErrBusy                = errors.New("DNS and Observatory policy is busy")
	ErrCandidateInvalid    = errors.New("DNS and Observatory policy candidate is invalid")
	ErrTransactionRestored = errors.New("DNS and Observatory policy transaction was restored")
	ErrTransactionUnproven = errors.New("DNS and Observatory policy transaction is not proven")
)

type Editability string

const (
	EditabilityEditable      Editability = "editable"
	EditabilityDriftDetected Editability = "drift-detected"
	EditabilityUnavailable   Editability = "unavailable"
)

type LockedDNSFacts struct {
	QueryStrategy         string `json:"queryStrategy"`
	LeakPreventionEnabled bool   `json:"leakPreventionEnabled"`
	SystemFallbackPresent bool   `json:"systemFallbackPresent"`
}

type ProxyDomainCounts struct {
	Baseline int `json:"baseline"`
	Derived  int `json:"derived"`
}

type DNSProjection struct {
	appliance.DNSSettings
	ResolverCatalog   []appliance.DNSResolverOption `json:"resolverCatalog"`
	Locked            LockedDNSFacts                `json:"locked"`
	ProxyDomainCounts ProxyDomainCounts             `json:"proxyDomainCounts"`
}

type ObservatoryProjection struct {
	ProbeIntervalMinutes int    `json:"probeIntervalMinutes"`
	MinIntervalMinutes   int    `json:"minIntervalMinutes"`
	MaxIntervalMinutes   int    `json:"maxIntervalMinutes"`
	SubjectSelector      string `json:"subjectSelector"`
}

// Projection is the safe GET result. It contains no raw Xray policy, resolver
// address, domain list, node endpoint or subscription material.
type Projection struct {
	Editability   Editability           `json:"editability"`
	SchemaVersion int                   `json:"schemaVersion"`
	DNS           DNSProjection         `json:"dns"`
	Observatory   ObservatoryProjection `json:"observatory"`
}

type DNSDiff struct {
	ResolverIDsAdded      []string `json:"resolverIdsAdded"`
	ResolverIDsRemoved    []string `json:"resolverIdsRemoved"`
	ResolverIDsReordered  []string `json:"resolverIdsReordered"`
	FallbackModeBefore    string   `json:"fallbackModeBefore"`
	FallbackModeAfter     string   `json:"fallbackModeAfter"`
	CacheEnabledBefore    bool     `json:"cacheEnabledBefore"`
	CacheEnabledAfter     bool     `json:"cacheEnabledAfter"`
	ServeStaleBefore      bool     `json:"serveStaleBefore"`
	ServeStaleAfter       bool     `json:"serveStaleAfter"`
	StaleTTLSecondsBefore int      `json:"staleTTLSecondsBefore"`
	StaleTTLSecondsAfter  int      `json:"staleTTLSecondsAfter"`
	ParallelQueriesBefore bool     `json:"parallelQueriesBefore"`
	ParallelQueriesAfter  bool     `json:"parallelQueriesAfter"`
}

type ObservatoryDiff struct {
	ProbeIntervalMinutesBefore int `json:"probeIntervalMinutesBefore"`
	ProbeIntervalMinutesAfter  int `json:"probeIntervalMinutesAfter"`
}

type Diff struct {
	DNS                      DNSDiff         `json:"dns"`
	Observatory              ObservatoryDiff `json:"observatory"`
	DerivedDomainCountBefore int             `json:"derivedProxyDomainCountBefore"`
	DerivedDomainCountAfter  int             `json:"derivedProxyDomainCountAfter"`
	DerivedDomainCountDelta  int             `json:"derivedProxyDomainCountDelta"`
	RestartRequired          bool            `json:"restartRequired"`
}

type Preview struct {
	Token     string    `json:"previewToken"`
	ExpiresAt time.Time `json:"expiresAt"`
	Noop      bool      `json:"noop"`
	Diff      Diff      `json:"diff"`
}

type ApplyResult struct {
	Noop           bool   `json:"noop"`
	Classification string `json:"classification"`
	Diff           Diff   `json:"diff"`
}

type SettingsService interface {
	SnapshotSettings(context.Context) (restore.SettingsSnapshot, error)
	PrepareSettingsCandidate(context.Context, restore.SettingsSnapshot, appliance.Appliance) (restore.SettingsCandidate, error)
	ApplySettingsCandidate(context.Context, restore.SettingsCandidate) (restore.ApplyResult, error)
}

type Config struct {
	Appliance   *appliance.Service
	Settings    SettingsService
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
	Binding   string
	Candidate restore.SettingsCandidate
	Diff      Diff
	Noop      bool
	CreatedAt time.Time
	ExpiresAt time.Time
}

func NewService(config Config) *Service {
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

func (s *Service) Read(ctx context.Context) (Projection, error) {
	result := unavailableProjection()
	if s == nil || s.config.Appliance == nil || s.config.Settings == nil {
		return result, ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot, err := s.config.Settings.SnapshotSettings(ctx)
	if err != nil {
		return result, nil
	}
	active, err := s.config.Appliance.ActiveSnapshot()
	if err != nil || !sameAppliance(snapshot.Appliance, active) {
		result.Editability = EditabilityDriftDetected
		return result, nil
	}
	managed, err := appliance.DecompileManagedPolicy(snapshot.Appliance)
	if err != nil {
		result.Editability = EditabilityDriftDetected
		return result, nil
	}
	return makeProjection(snapshot.Appliance, managed, EditabilityEditable), nil
}

// Preview validates a complete DNS + Observatory projection, preserves the
// current classified routing rules, renders and validates a complete Xray
// candidate through restore, and stores only the resulting typed candidate in
// RAM.
func (s *Service) Preview(ctx context.Context, binding string, dns appliance.DNSSettings, observatory appliance.ObservatorySettings) (Preview, error) {
	if s == nil || s.config.Appliance == nil || s.config.Settings == nil {
		return Preview{}, ErrUnavailable
	}
	if binding == "" || appliance.ValidateDNSSettings(dns) != nil || appliance.ValidateObservatorySettings(observatory) != nil {
		return Preview{}, ErrInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot, err := s.config.Settings.SnapshotSettings(ctx)
	if err != nil {
		return Preview{}, mapPreviewError(err)
	}
	active, err := s.config.Appliance.ActiveSnapshot()
	if err != nil || !sameAppliance(snapshot.Appliance, active) {
		return Preview{}, ErrDriftDetected
	}
	before, err := appliance.DecompileManagedPolicy(snapshot.Appliance)
	if err != nil {
		return Preview{}, ErrDriftDetected
	}
	candidate, err := appliance.CompileDNSObservatory(snapshot.Appliance, dns, observatory)
	if err != nil {
		switch {
		case errors.Is(err, appliance.ErrManagedPolicyDrift), errors.Is(err, appliance.ErrCustomPolicyDrift):
			return Preview{}, ErrDriftDetected
		default:
			return Preview{}, ErrInvalidRequest
		}
	}
	prepared, err := s.config.Settings.PrepareSettingsCandidate(ctx, snapshot, candidate)
	if err != nil {
		return Preview{}, mapPreviewError(err)
	}
	after, err := appliance.DecompileManagedPolicy(candidate)
	if err != nil {
		return Preview{}, ErrCandidateInvalid
	}
	diff := makeDiff(before, after)
	noop := sameAppliance(snapshot.Appliance, candidate)
	diff.RestartRequired = !noop
	created := s.config.Now()
	expires := created.Add(s.config.PreviewTTL)
	token, err := s.randomToken()
	if err != nil {
		return Preview{}, ErrUnavailable
	}
	entry := previewEntry{Binding: binding, Candidate: prepared, Diff: diff, Noop: noop, CreatedAt: created, ExpiresAt: expires}
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
	return Preview{Token: token, ExpiresAt: expires, Noop: noop, Diff: diff}, nil
}

func (s *Service) Apply(ctx context.Context, binding, token string) (ApplyResult, error) {
	if s == nil || s.config.Settings == nil {
		return ApplyResult{}, ErrUnavailable
	}
	entry, ok := s.takePreview(binding, token)
	if !ok {
		return ApplyResult{}, ErrPreviewExpired
	}
	result, err := s.config.Settings.ApplySettingsCandidate(ctx, entry.Candidate)
	if err != nil {
		return ApplyResult{}, mapApplyError(err)
	}
	return ApplyResult{Noop: result.Noop, Classification: result.Classification, Diff: entry.Diff}, nil
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

func unavailableProjection() Projection {
	return Projection{
		Editability:   EditabilityUnavailable,
		SchemaVersion: appliance.SchemaVersion,
		DNS: DNSProjection{
			DNSSettings:       appliance.DNSSettings{ProxyResolverIDs: []string{}},
			ResolverCatalog:   appliance.DNSResolverCatalog(),
			Locked:            LockedDNSFacts{QueryStrategy: "UseIPv4"},
			ProxyDomainCounts: ProxyDomainCounts{},
		},
		Observatory: ObservatoryProjection{
			MinIntervalMinutes: appliance.MinObservatoryIntervalMinutes,
			MaxIntervalMinutes: appliance.MaxObservatoryIntervalMinutes,
		},
	}
}

func makeProjection(value appliance.Appliance, managed appliance.ManagedPolicy, editability Editability) Projection {
	baseline := appliance.ProductDefault()
	return Projection{
		Editability:   editability,
		SchemaVersion: value.SchemaVersion,
		DNS: DNSProjection{
			DNSSettings:       cloneDNSSettings(managed.DNS),
			ResolverCatalog:   appliance.DNSResolverCatalog(),
			Locked:            LockedDNSFacts{QueryStrategy: baseline.DNS.QueryStrategy, LeakPreventionEnabled: baseline.DNS.DisableFallbackIfMatch, SystemFallbackPresent: hasSystemFallback(baseline)},
			ProxyDomainCounts: ProxyDomainCounts{Baseline: managed.ProxyDNSBaselineDomainCount, Derived: managed.ProxyDNSDerivedDomainCount},
		},
		Observatory: ObservatoryProjection{
			ProbeIntervalMinutes: managed.Observatory.ProbeIntervalMinutes,
			MinIntervalMinutes:   appliance.MinObservatoryIntervalMinutes,
			MaxIntervalMinutes:   appliance.MaxObservatoryIntervalMinutes,
			SubjectSelector:      firstOrEmpty(baseline.Observatory.SubjectSelector),
		},
	}
}

func hasSystemFallback(value appliance.Appliance) bool {
	return len(value.DNS.Servers) > 0 && value.DNS.Servers[len(value.DNS.Servers)-1].Address == "localhost"
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func cloneDNSSettings(value appliance.DNSSettings) appliance.DNSSettings {
	value.ProxyResolverIDs = append([]string{}, value.ProxyResolverIDs...)
	return value
}

func makeDiff(before, after appliance.ManagedPolicy) Diff {
	dns := DNSDiff{
		ResolverIDsAdded:      difference(after.DNS.ProxyResolverIDs, before.DNS.ProxyResolverIDs),
		ResolverIDsRemoved:    difference(before.DNS.ProxyResolverIDs, after.DNS.ProxyResolverIDs),
		ResolverIDsReordered:  commonOrderDifference(before.DNS.ProxyResolverIDs, after.DNS.ProxyResolverIDs),
		FallbackModeBefore:    before.DNS.FallbackMode,
		FallbackModeAfter:     after.DNS.FallbackMode,
		CacheEnabledBefore:    before.DNS.CacheEnabled,
		CacheEnabledAfter:     after.DNS.CacheEnabled,
		ServeStaleBefore:      before.DNS.ServeStale,
		ServeStaleAfter:       after.DNS.ServeStale,
		StaleTTLSecondsBefore: before.DNS.StaleTTLSeconds,
		StaleTTLSecondsAfter:  after.DNS.StaleTTLSeconds,
		ParallelQueriesBefore: before.DNS.ParallelQueries,
		ParallelQueriesAfter:  after.DNS.ParallelQueries,
	}
	result := Diff{
		DNS: dns,
		Observatory: ObservatoryDiff{
			ProbeIntervalMinutesBefore: before.Observatory.ProbeIntervalMinutes,
			ProbeIntervalMinutesAfter:  after.Observatory.ProbeIntervalMinutes,
		},
		DerivedDomainCountBefore: before.ProxyDNSDerivedDomainCount,
		DerivedDomainCountAfter:  after.ProxyDNSDerivedDomainCount,
	}
	result.DerivedDomainCountDelta = result.DerivedDomainCountAfter - result.DerivedDomainCountBefore
	return result
}

func difference(left, right []string) []string {
	seen := make(map[string]struct{}, len(right))
	for _, value := range right {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(left))
	for _, value := range left {
		if _, exists := seen[value]; !exists {
			result = append(result, value)
		}
	}
	return result
}

func commonOrderDifference(before, after []string) []string {
	beforeSet := make(map[string]struct{}, len(before))
	afterSet := make(map[string]struct{}, len(after))
	for _, value := range before {
		beforeSet[value] = struct{}{}
	}
	for _, value := range after {
		afterSet[value] = struct{}{}
	}
	commonBefore := make([]string, 0, len(before))
	for _, value := range before {
		if _, ok := afterSet[value]; ok {
			commonBefore = append(commonBefore, value)
		}
	}
	commonAfter := make([]string, 0, len(after))
	for _, value := range after {
		if _, ok := beforeSet[value]; ok {
			commonAfter = append(commonAfter, value)
		}
	}
	if reflect.DeepEqual(commonBefore, commonAfter) {
		return []string{}
	}
	return commonAfter
}

func sameAppliance(left, right appliance.Appliance) bool {
	leftBytes, leftErr := appliance.MarshalCanonical(left)
	rightBytes, rightErr := appliance.MarshalCanonical(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func mapPreviewError(err error) error {
	switch {
	case errors.Is(err, restore.ErrAuthorityBusy):
		return ErrBusy
	case errors.Is(err, restore.ErrCompatibilityBlocked):
		return ErrDriftDetected
	case errors.Is(err, restore.ErrCandidateInvalid):
		return ErrCandidateInvalid
	case errors.Is(err, restore.ErrRecoveryRequired), errors.Is(err, restore.ErrRecoveryFailed), errors.Is(err, restore.ErrUnavailable):
		return ErrUnavailable
	default:
		return ErrUnavailable
	}
}

func mapApplyError(err error) error {
	switch {
	case errors.Is(err, restore.ErrAuthorityBusy):
		return ErrBusy
	case errors.Is(err, restore.ErrPreviewStale):
		return ErrPreviewStale
	case errors.Is(err, restore.ErrCompatibilityBlocked):
		return ErrDriftDetected
	case errors.Is(err, restore.ErrCandidateInvalid):
		return ErrCandidateInvalid
	case errors.Is(err, restore.ErrRecoveryFailed):
		return ErrTransactionUnproven
	case errors.Is(err, restore.ErrApplyFailed):
		return ErrTransactionRestored
	case errors.Is(err, restore.ErrRecoveryRequired), errors.Is(err, restore.ErrUnavailable):
		return ErrUnavailable
	default:
		return ErrUnavailable
	}
}
