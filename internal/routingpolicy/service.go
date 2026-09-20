// Package routingpolicy owns the narrow backend custom-routing editor. It
// compiles only the typed client-routing projection and delegates persistence
// to restore's existing D.1 settings transaction owner.
package routingpolicy

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
	ErrUnavailable         = errors.New("routing policy unavailable")
	ErrInvalidRequest      = errors.New("routing policy request is invalid")
	ErrDriftDetected       = errors.New("routing policy drift detected")
	ErrPreviewExpired      = errors.New("routing policy preview is expired or invalid")
	ErrPreviewStale        = errors.New("routing policy preview is stale")
	ErrBusy                = errors.New("routing policy is busy")
	ErrCandidateInvalid    = errors.New("routing policy candidate is invalid")
	ErrTransactionRestored = errors.New("routing policy transaction was restored")
	ErrTransactionUnproven = errors.New("routing policy transaction is not proven")
)

type Editability string

const (
	EditabilityEditable      Editability = "editable"
	EditabilityDriftDetected Editability = "drift-detected"
	EditabilityUnavailable   Editability = "unavailable"
)

type RuleChange struct {
	Name   string                     `json:"name"`
	Action appliance.CustomRuleAction `json:"action"`
}

type MatchCounts struct {
	Rules     int `json:"rules"`
	Domains   int `json:"domains"`
	IPs       int `json:"ips"`
	Protocols int `json:"protocols"`
	Networks  int `json:"networks"`
	Ports     int `json:"ports"`
}

type Diff struct {
	Added                       []RuleChange `json:"added"`
	Removed                     []RuleChange `json:"removed"`
	Changed                     []RuleChange `json:"changed"`
	Reordered                   []RuleChange `json:"reordered"`
	BeforeMatches               MatchCounts  `json:"beforeMatches"`
	AfterMatches                MatchCounts  `json:"afterMatches"`
	DNSDerivedDomainCountBefore int          `json:"dnsDerivedDomainCountBefore"`
	DNSDerivedDomainCountAfter  int          `json:"dnsDerivedDomainCountAfter"`
	DNSDerivedDomainCountDelta  int          `json:"dnsDerivedDomainCountDelta"`
	RestartRequired             bool         `json:"restartRequired"`
}

type ProtectedFacts struct {
	TotalRuleCount        int    `json:"totalRuleCount"`
	PrefixRuleCount       int    `json:"prefixRuleCount"`
	CustomRegionRuleCount int    `json:"customRegionRuleCount"`
	RegionPlacement       string `json:"regionPlacement"`
	FinalCatchAllPresent  bool   `json:"finalCatchAllPresent"`
}

type DNSFacts struct {
	ProxyResolverCount  int `json:"proxyResolverCount"`
	BaselineDomainCount int `json:"baselineDomainCount"`
	DerivedDomainCount  int `json:"derivedDomainCount"`
}

type ObservatoryFacts struct {
	ProbeInterval string `json:"probeInterval"`
}

// Projection is the safe GET result. An unavailable or drifted state carries
// no editable rules and never includes raw generated files.
type Projection struct {
	Editability   Editability            `json:"editability"`
	SchemaVersion int                    `json:"schemaVersion"`
	Rules         []appliance.CustomRule `json:"rules"`
	Protected     ProtectedFacts         `json:"protected"`
	DNS           DNSFacts               `json:"dns"`
	Observatory   ObservatoryFacts       `json:"observatory"`
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

// Read returns a bounded policy projection. Known authority/runtime problems
// are represented by editability state so the UI can distinguish unavailable
// from protected drift without receiving implementation details.
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
	classified, err := appliance.DecompileCustomPolicy(snapshot.Appliance)
	if err != nil {
		result.Editability = EditabilityDriftDetected
		return result, nil
	}
	return makeProjection(snapshot.Appliance, classified, EditabilityEditable), nil
}

// Preview validates, compiles and candidate-validates one complete typed
// custom-rule list, then stores only a session-bound RAM candidate.
func (s *Service) Preview(ctx context.Context, binding string, rules []appliance.CustomRule) (Preview, error) {
	if s == nil || s.config.Appliance == nil || s.config.Settings == nil {
		return Preview{}, ErrUnavailable
	}
	if binding == "" || appliance.ValidateCustomRules(rules) != nil {
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
	before, err := appliance.DecompileCustomPolicy(snapshot.Appliance)
	if err != nil {
		return Preview{}, ErrDriftDetected
	}
	candidate, err := appliance.CompileCustomRules(snapshot.Appliance, rules)
	if err != nil {
		if errors.Is(err, appliance.ErrCustomPolicyDrift) {
			return Preview{}, ErrDriftDetected
		}
		return Preview{}, ErrInvalidRequest
	}
	prepared, err := s.config.Settings.PrepareSettingsCandidate(ctx, snapshot, candidate)
	if err != nil {
		return Preview{}, mapPreviewError(err)
	}
	after, err := appliance.DecompileCustomPolicy(candidate)
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

// Apply consumes a policy preview before dispatching its typed candidate to
// the shared D.1 settings transaction owner.
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
		Rules:         []appliance.CustomRule{},
		Protected:     ProtectedFacts{RegionPlacement: "immediately-before-final-catch-all"},
		DNS:           DNSFacts{},
		Observatory:   ObservatoryFacts{},
	}
}

func makeProjection(value appliance.Appliance, policy appliance.CustomPolicy, editability Editability) Projection {
	return Projection{
		Editability:   editability,
		SchemaVersion: value.SchemaVersion,
		Rules:         cloneRules(policy.Rules),
		Protected: ProtectedFacts{
			TotalRuleCount:        policy.ProtectedRuleCount + policy.CustomRegionRuleCount,
			PrefixRuleCount:       policy.ProtectedPrefixRuleCount,
			CustomRegionRuleCount: policy.CustomRegionRuleCount,
			RegionPlacement:       "immediately-before-final-catch-all",
			FinalCatchAllPresent:  true,
		},
		DNS: DNSFacts{
			ProxyResolverCount:  policy.ProxyDNSResolverCount,
			BaselineDomainCount: policy.ProxyDNSBaselineDomainCount,
			DerivedDomainCount:  policy.ProxyDNSDerivedDomainCount,
		},
		Observatory: ObservatoryFacts{ProbeInterval: value.Observatory.ProbeInterval},
	}
}

func makeDiff(before, after appliance.CustomPolicy) Diff {
	result := Diff{
		Added:                       []RuleChange{},
		Removed:                     []RuleChange{},
		Changed:                     []RuleChange{},
		Reordered:                   []RuleChange{},
		BeforeMatches:               matchCounts(before.Rules),
		AfterMatches:                matchCounts(after.Rules),
		DNSDerivedDomainCountBefore: before.ProxyDNSDerivedDomainCount,
		DNSDerivedDomainCountAfter:  after.ProxyDNSDerivedDomainCount,
	}
	result.DNSDerivedDomainCountDelta = result.DNSDerivedDomainCountAfter - result.DNSDerivedDomainCountBefore
	left := make(map[string]appliance.CustomRule, len(before.Rules))
	right := make(map[string]appliance.CustomRule, len(after.Rules))
	for _, rule := range before.Rules {
		left[rule.Name] = rule
	}
	for _, rule := range after.Rules {
		right[rule.Name] = rule
		if previous, exists := left[rule.Name]; !exists {
			result.Added = append(result.Added, RuleChange{Name: rule.Name, Action: rule.Action})
		} else if !reflect.DeepEqual(previous, rule) {
			result.Changed = append(result.Changed, RuleChange{Name: rule.Name, Action: rule.Action})
		}
	}
	for _, rule := range before.Rules {
		if _, exists := right[rule.Name]; !exists {
			result.Removed = append(result.Removed, RuleChange{Name: rule.Name, Action: rule.Action})
		}
	}
	commonBefore := commonNames(before.Rules, right)
	commonAfter := commonNames(after.Rules, left)
	if !reflect.DeepEqual(commonBefore, commonAfter) {
		for _, rule := range after.Rules {
			if _, exists := left[rule.Name]; exists {
				result.Reordered = append(result.Reordered, RuleChange{Name: rule.Name, Action: rule.Action})
			}
		}
	}
	return result
}

func commonNames(rules []appliance.CustomRule, other map[string]appliance.CustomRule) []string {
	result := make([]string, 0, len(rules))
	for _, rule := range rules {
		if _, exists := other[rule.Name]; exists {
			result = append(result, rule.Name)
		}
	}
	return result
}

func matchCounts(rules []appliance.CustomRule) MatchCounts {
	result := MatchCounts{Rules: len(rules)}
	for _, rule := range rules {
		result.Domains += len(rule.Domains)
		result.IPs += len(rule.IPs)
		result.Protocols += len(rule.Protocols)
		result.Networks += len(rule.Networks)
		result.Ports += len(rule.Ports)
	}
	return result
}

func cloneRules(values []appliance.CustomRule) []appliance.CustomRule {
	result := make([]appliance.CustomRule, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Domains = append([]string{}, value.Domains...)
		result[index].IPs = append([]string{}, value.IPs...)
		result[index].Protocols = append([]string{}, value.Protocols...)
		result[index].Networks = append([]string{}, value.Networks...)
		result[index].Ports = append([]appliance.PortRange{}, value.Ports...)
	}
	return result
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
