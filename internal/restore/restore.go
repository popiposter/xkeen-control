// Package restore implements the Phase C1, non-HTTP restore core. It accepts
// only the typed Phase B bundle formats and owns no generic filesystem,
// archive, raw-config, or command surface.
package restore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

const (
	DefaultPreviewTTL                 = 5 * time.Minute
	DefaultMaxPreviews                = 4
	DefaultAuthorityWaitTimeout       = 15 * time.Second
	DefaultCandidateValidationTimeout = 45 * time.Second
	DefaultActivationTimeout          = 2 * time.Minute
	DefaultRollbackTimeout            = 2 * time.Minute
	DefaultTransactionTimeout         = 5 * time.Minute
	MaxJournalBytes                   = 16 << 10
	MaxPreviousMetadataBytes          = 1 << 10
)

var (
	ErrUnavailable             = errors.New("restore service unavailable")
	ErrRecoveryRequired        = errors.New("restore startup recovery is required")
	ErrRecoveryFailed          = errors.New("restore recovery failed")
	ErrInvalidMode             = errors.New("restore mode is invalid")
	ErrInvalidBundle           = errors.New("restore bundle is invalid")
	ErrEncryptedBundleRequired = errors.New("encrypted bundle is required for registry restore")
	ErrPreviewExpired          = errors.New("restore preview is expired or invalid")
	ErrPreviewStale            = errors.New("restore preview is stale")
	ErrCompatibilityBlocked    = errors.New("restore is blocked by current compatibility state")
	ErrCandidateInvalid        = errors.New("restore candidate is invalid")
	ErrApplyFailed             = errors.New("restore apply failed")
	ErrAuthorityBusy           = errors.New("restore authority is busy")
)

// Mode is the only restore composition mode supported by C1.
type Mode string

const (
	SettingsOnly    Mode = "settings-only"
	ReplaceRegistry Mode = "replace-registry"
	MergeRegistry   Mode = "merge-registry"

	// Explicit aliases make the mode names convenient at future HTTP and
	// command boundaries without introducing another set of values.
	ModeSettingsOnly    = SettingsOnly
	ModeReplaceRegistry = ReplaceRegistry
	ModeMergeRegistry   = MergeRegistry
)

// Stage names are safe fault-injection points for focused fixtures. They are
// not persisted and never contain candidate content.
type Stage string

const (
	StagePreviousSaved        Stage = "previous-saved"
	StageJournalPrepared      Stage = "journal-prepared"
	StageApplianceCommitted   Stage = "appliance-committed"
	StageNodesCommitted       Stage = "nodes-committed"
	StageAuthoritiesCommitted Stage = "authorities-committed"
	StageDNSCommitted         Stage = "dns-committed"
	StageRoutingCommitted     Stage = "routing-committed"
	StageObservatoryCommitted Stage = "observatory-committed"
	StageOutboundsCommitted   Stage = "outbounds-committed"
	StageGeneratedCommitted   Stage = "generated-committed"
	StageRestarted            Stage = "restarted"
	StageReady                Stage = "ready"
	StageRuntimeVerified      Stage = "runtime-verified"
)

// FailureInjector is test-only in production use. Its error is intentionally
// discarded by Service so sensitive fixture details cannot cross the service
// boundary.
type FailureInjector func(Stage) error

// ChangeSummary is a bounded, secret-safe logical diff. It deliberately has
// no identifiers, names, endpoints, or registry content.
type ChangeSummary struct {
	ApplianceChanged     bool `json:"applianceChanged"`
	SubscriptionsAdded   int  `json:"subscriptionsAdded"`
	SubscriptionsRemoved int  `json:"subscriptionsRemoved"`
	SubscriptionsChanged int  `json:"subscriptionsChanged"`
	NodesAdded           int  `json:"nodesAdded"`
	NodesRemoved         int  `json:"nodesRemoved"`
	NodesChanged         int  `json:"nodesChanged"`
}

// Compatibility is a safe preview projection. Codes are fixed, bounded
// values and never contain an underlying filesystem or runtime error.
type Compatibility struct {
	Blockers []string `json:"blockers,omitempty"`
}

// Preview is the only state returned before Apply. The token is opaque and
// the candidate itself remains in RAM inside the service.
type Preview struct {
	Token           string        `json:"previewToken"`
	Mode            Mode          `json:"mode"`
	ExpiresAt       time.Time     `json:"expiresAt"`
	ContainsSecrets bool          `json:"containsSecrets"`
	Noop            bool          `json:"noop"`
	Changes         ChangeSummary `json:"changes"`
	Compatibility   Compatibility `json:"compatibility"`
}

// ApplyResult contains only safe outcome metadata.
type ApplyResult struct {
	Mode           Mode          `json:"mode"`
	Noop           bool          `json:"noop"`
	Changes        ChangeSummary `json:"changes"`
	Classification string        `json:"classification"`
}

type Config struct {
	AppliancePath       string
	NodesPath           string
	ConfigDir           string
	XkeenConfigPath     string
	ActiveOutboundsPath string
	PreviousDir         string
	StateDir            string

	// Appliance is optional. When present it remains useful to callers that
	// already construct the Phase A service, but restore uses the exported
	// typed render primitives directly so current-state proof is explicit.
	Appliance   *appliance.Service
	Activator   nodes.Activator
	Coordinator interface {
		BeginApply(context.Context) (func(), error)
	}
	AuthorityLease *authority.Lease
	Authority      *authority.Lease

	PreviewTTL           time.Duration
	MaxPreviews          int
	AuthorityWaitTimeout time.Duration
	CandidateValidation  time.Duration
	Activation           time.Duration
	Rollback             time.Duration
	Transaction          time.Duration
	Now                  func() time.Time
	Random               io.Reader
	InjectFailure        FailureInjector
	// SyncDirectory is an internal persistence seam used by focused fixtures;
	// production uses the platform directory fsync implementation.
	SyncDirectory func(string) error
}

type Service struct {
	config Config

	mu          sync.Mutex
	previews    map[string]previewEntry
	ready       bool
	readyErr    error
	maintenance bool

	startupMu     sync.Mutex
	syncDirectory func(string) error
}

type maintenanceGate interface {
	EnterMaintenance()
	ExitMaintenance()
}

type recoveryCoordinator interface {
	BeginRecovery(context.Context) (func(), error)
}

type previewEntry struct {
	Binding         string
	Mode            Mode
	Appliance       appliance.Appliance
	Registry        nodes.Registry
	HasRegistry     bool
	BaseDigest      [sha256.Size]byte
	ContainsSecrets bool
	Noop            bool
	Changes         ChangeSummary
	Blockers        []string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type authoritySnapshot struct {
	applianceExists bool
	applianceValid  bool
	applianceBytes  []byte
	appliance       appliance.Appliance
	nodesExists     bool
	nodesValid      bool
	nodesStrict     bool
	nodesBytes      []byte
	registry        nodes.Registry
	digest          [sha256.Size]byte
}

type authorityMeta struct {
	Exists bool   `json:"exists"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type importJournal struct {
	SchemaVersion int         `json:"schemaVersion"`
	Mode          Mode        `json:"mode"`
	Phase         string      `json:"phase"`
	Previous      journalPair `json:"previous"`
	Candidate     journalPair `json:"candidate"`
}

type journalPair struct {
	Appliance authorityMeta `json:"appliance"`
	Nodes     authorityMeta `json:"nodes"`
}

const (
	journalSchemaVersion = 1
	phasePrepared        = "prepared"
	phaseAuthorities     = "authorities-committed"
	phaseGenerated       = "generated-committed"
	phaseRuntimeVerified = "runtime-verified"
)

var allowedBlockers = map[string]struct{}{
	"appliance-authority-not-adopted": {},
	"appliance-authority-unavailable": {},
	"appliance-authority-invalid":     {},
	"nodes-authority-unavailable":     {},
	"nodes-authority-invalid":         {},
	"nodes-authority-unsupported":     {},
	"runtime-verifier-unavailable":    {},
	"candidate-validator-unavailable": {},
}

// NewService constructs a bounded restore core. Startup recovery is explicit
// through RecoverStartup so callers can fail the process before accepting any
// normal mutation request and can inspect the typed error.
func NewService(config Config) *Service {
	if config.PreviewTTL <= 0 || config.PreviewTTL > DefaultPreviewTTL {
		config.PreviewTTL = DefaultPreviewTTL
	}
	if config.MaxPreviews <= 0 || config.MaxPreviews > DefaultMaxPreviews {
		config.MaxPreviews = DefaultMaxPreviews
	}
	if config.AuthorityWaitTimeout <= 0 {
		config.AuthorityWaitTimeout = DefaultAuthorityWaitTimeout
	}
	if config.CandidateValidation <= 0 {
		config.CandidateValidation = DefaultCandidateValidationTimeout
	}
	if config.Activation <= 0 {
		config.Activation = DefaultActivationTimeout
	}
	if config.Rollback <= 0 {
		config.Rollback = DefaultRollbackTimeout
	}
	if config.Transaction <= 0 {
		config.Transaction = DefaultTransactionTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.SyncDirectory == nil {
		config.SyncDirectory = syncDirectory
	}
	if config.ConfigDir == "" {
		config.ConfigDir = "/opt/etc/xray/configs"
	}
	if config.ActiveOutboundsPath == "" {
		config.ActiveOutboundsPath = filepath.Join(config.ConfigDir, "04_outbounds.json")
	}
	if config.XkeenConfigPath == "" {
		config.XkeenConfigPath = "/opt/etc/xkeen/xkeen.json"
	}
	if config.PreviousDir == "" {
		config.PreviousDir = filepath.Join(filepath.Dir(filepath.Dir(config.AppliancePath)), "previous", "appliance-import")
	}
	if config.StateDir == "" {
		config.StateDir = filepath.Join(filepath.Dir(filepath.Dir(config.AppliancePath)), "state")
	}
	lease := config.AuthorityLease
	if lease == nil {
		lease = config.Authority
	}
	if lease == nil {
		lease = authority.NewLease()
	}
	config.AuthorityLease = lease
	ready := true
	var readyErr error
	if _, err := os.Lstat(filepath.Join(config.StateDir, "appliance-import-transaction.json")); err == nil {
		ready = false
		readyErr = ErrRecoveryRequired
	} else if !errors.Is(err, os.ErrNotExist) {
		ready = false
		readyErr = ErrRecoveryFailed
	}
	service := &Service{
		config: config, previews: make(map[string]previewEntry), ready: ready, readyErr: readyErr,
		syncDirectory: config.SyncDirectory,
	}
	if !ready {
		service.enterMaintenance()
	}
	return service
}

// New is the concise constructor used by package-local callers.
func New(config Config) *Service { return NewService(config) }

// Ready reports whether startup recovery has completed successfully. A
// leftover journal makes a service unavailable until RecoverStartup runs.
func (s *Service) Ready() error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		if s.readyErr != nil {
			return s.readyErr
		}
		return ErrRecoveryRequired
	}
	return nil
}

// RecoverStartup converges a leftover interrupted import to its saved logical
// previous generation before declaring the service ready. With no journal it
// is a bounded no-op and does not require an adopted appliance authority.
func (s *Service) RecoverStartup(ctx context.Context) error {
	if s == nil {
		return ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.startupMu.Lock()
	defer s.startupMu.Unlock()

	journal, exists, err := s.readJournal()
	if err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ErrRecoveryFailed
	}
	if !exists {
		if s.isMaintenance() {
			s.failClosed()
			s.markNotReady(ErrRecoveryFailed)
			return ErrRecoveryFailed
		}
		s.markReady()
		return nil
	}

	admissionContext, cancelAdmission := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancelAdmission()
	releaseCoordinator, err := s.beginRecoveryCoordinator(admissionContext)
	if err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ErrRecoveryFailed
	}
	defer releaseCoordinator()
	releaseAuthority, err := s.config.AuthorityLease.AcquireForRecovery(admissionContext, s.config.AuthorityWaitTimeout)
	if err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ErrRecoveryFailed
	}
	defer releaseAuthority()

	previous, err := s.loadPrevious(journal.Previous)
	if err != nil || !previous.applianceExists || !previous.nodesExists || !previous.applianceValid || !previous.nodesValid {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ErrRecoveryFailed
	}
	if err := s.recoverFromSnapshot(ctx, previous); err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ErrRecoveryFailed
	}
	s.releaseMaintenance()
	s.markReady()
	return nil
}

// Preview decodes a Phase B safe or encrypted bundle and creates one
// session-bound, one-shot in-memory candidate. Authority bytes are captured
// under the shared lease before decryption/candidate work and the lease is
// released before those potentially expensive operations.
func (s *Service) Preview(ctx context.Context, binding string, mode Mode, contents []byte, passphrase string) (Preview, error) {
	if err := s.Ready(); err != nil {
		return Preview{}, err
	}
	if binding == "" {
		return Preview{}, ErrInvalidMode
	}
	mode, err := normalizeMode(mode)
	if err != nil {
		return Preview{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot, err := s.captureAuthorities(ctx)
	if err != nil {
		return Preview{}, ErrUnavailable
	}

	input := bytes.Clone(contents)
	defer clearBytes(input)
	bundle, containsSecrets, err := decodeBundle(input, passphrase)
	if err != nil {
		return Preview{}, err
	}
	defer zeroBundle(&bundle)
	if !containsSecrets && mode != SettingsOnly {
		return Preview{}, ErrEncryptedBundleRequired
	}
	if containsSecrets && mode != SettingsOnly && bundle.Nodes == nil {
		return Preview{}, ErrEncryptedBundleRequired
	}

	blockers := snapshotBlockers(snapshot)
	if mode != SettingsOnly && snapshot.nodesValid && !snapshot.nodesStrict {
		blockers = append(blockers, "nodes-authority-unsupported")
	}
	if snapshot.nodesValid && snapshot.applianceValid {
		if s.config.Activator == nil {
			blockers = append(blockers, "runtime-verifier-unavailable")
		}
	}

	candidateAppliance, err := cloneAppliance(bundle.Appliance)
	if err != nil {
		return Preview{}, ErrInvalidBundle
	}
	candidateRegistry := snapshot.registry
	hasRegistry := snapshot.nodesValid
	if snapshot.nodesValid {
		switch mode {
		case SettingsOnly:
			// The imported registry, when present, is intentionally ignored.
		case ReplaceRegistry:
			if bundle.Nodes == nil {
				return Preview{}, ErrEncryptedBundleRequired
			}
			candidateRegistry, err = cloneRegistry(*bundle.Nodes)
			if err == nil {
				sortRegistryByID(&candidateRegistry)
			}
		case MergeRegistry:
			if bundle.Nodes == nil {
				return Preview{}, ErrEncryptedBundleRequired
			}
			candidateRegistry, err = mergeRegistry(snapshot.registry, *bundle.Nodes)
		}
		if err != nil {
			return Preview{}, err
		}
	} else if snapshot.nodesExists {
		hasRegistry = false
	}

	blockers = normalizeBlockers(blockers)
	changes := ChangeSummary{}
	noop := false
	if snapshot.applianceValid && snapshot.nodesValid && hasRegistry {
		changes = summarize(snapshot.appliance, snapshot.registry, candidateAppliance, candidateRegistry)
		noop = changes == (ChangeSummary{})
	}
	if err := candidateAppliance.Validate(); err != nil {
		return Preview{}, ErrInvalidBundle
	}
	if hasRegistry {
		if err := candidateRegistry.Validate(); err != nil {
			return Preview{}, ErrCandidateInvalid
		}
	}

	created := s.config.Now()
	expires := created.Add(s.config.PreviewTTL)
	token, err := s.randomToken()
	if err != nil {
		return Preview{}, ErrUnavailable
	}
	entry := previewEntry{
		Binding: binding, Mode: mode, Appliance: candidateAppliance, Registry: candidateRegistry,
		HasRegistry: hasRegistry, BaseDigest: snapshot.digest, ContainsSecrets: containsSecrets,
		Noop: noop, Changes: changes, Blockers: blockers, CreatedAt: created, ExpiresAt: expires,
	}
	s.mu.Lock()
	s.purgeExpiredLocked(created)
	for oldToken, old := range s.previews {
		if old.Binding == binding {
			delete(s.previews, oldToken)
			zeroPreview(&old)
		}
	}
	for len(s.previews) >= s.config.MaxPreviews {
		s.evictOldestLocked()
	}
	s.previews[token] = entry
	s.mu.Unlock()
	return Preview{Token: token, Mode: mode, ExpiresAt: expires, ContainsSecrets: containsSecrets, Noop: noop, Changes: changes, Compatibility: Compatibility{Blockers: append([]string(nil), blockers...)}}, nil
}

// PreviewBundle is an argument-order convenience wrapper for future upload
// adapters. It does not create another parsing path.
func (s *Service) PreviewBundle(ctx context.Context, binding string, contents []byte, passphrase string, mode Mode) (Preview, error) {
	return s.Preview(ctx, binding, mode, contents, passphrase)
}

// Apply consumes a valid preview token after an exact authority re-check and
// performs the combined logical-authority/runtime transaction.
func (s *Service) Apply(ctx context.Context, binding, token string) (ApplyResult, error) {
	if err := s.Ready(); err != nil {
		return ApplyResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	entry, ok := s.peekPreview(binding, token)
	if !ok {
		return ApplyResult{}, ErrPreviewExpired
	}
	if len(entry.Blockers) != 0 {
		// A valid Apply request consumes the one-shot preview even when its
		// preview-time compatibility result already proves that mutation must
		// fail closed. This prevents a blocked candidate from being replayed
		// after an unrelated authority/runtime change.
		blockedEntry, ok := s.takePreviewIfCurrent(binding, token)
		if !ok {
			return ApplyResult{}, ErrPreviewExpired
		}
		zeroPreview(&blockedEntry)
		return ApplyResult{}, ErrCompatibilityBlocked
	}

	// The coordinator is deliberately acquired before the authority lease.
	// It remains the sole owner of runtime lifecycle exclusion.
	admissionContext, cancelAdmission := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancelAdmission()
	releaseCoordinator, err := s.beginCoordinator(admissionContext)
	if err != nil {
		return ApplyResult{}, ErrAuthorityBusy
	}
	defer releaseCoordinator()
	releaseAuthority, err := s.config.AuthorityLease.Acquire(admissionContext, s.config.AuthorityWaitTimeout)
	if err != nil {
		return ApplyResult{}, ErrAuthorityBusy
	}
	defer releaseAuthority()

	entry, ok = s.takePreviewIfCurrent(binding, token)
	if !ok {
		return ApplyResult{}, ErrPreviewExpired
	}
	defer zeroPreview(&entry)
	// The entry is now one-shot. Any subsequent failure is handled by the
	// persistent previous-generation protocol and cannot be retried with stale
	// candidate state.

	snapshot, err := s.captureAuthoritiesUnderLease()
	if err != nil || snapshot.digest != entry.BaseDigest {
		return ApplyResult{}, ErrPreviewStale
	}
	if blockers := snapshotBlockers(snapshot); len(blockers) != 0 || (entry.Mode != SettingsOnly && !snapshot.nodesStrict) {
		return ApplyResult{}, ErrCompatibilityBlocked
	}
	if err := s.verifyCurrent(admissionContext, snapshot); err != nil {
		return ApplyResult{}, ErrCompatibilityBlocked
	}
	if !entry.HasRegistry || !snapshot.nodesValid {
		return ApplyResult{}, ErrCompatibilityBlocked
	}
	if entry.Mode != SettingsOnly && !snapshot.nodesStrict {
		return ApplyResult{}, ErrCompatibilityBlocked
	}
	if err := entry.Appliance.Validate(); err != nil || entry.Registry.Validate() != nil {
		return ApplyResult{}, ErrCandidateInvalid
	}

	if sameAppliance(snapshot.appliance, entry.Appliance) && sameRegistry(snapshot.registry, entry.Registry) {
		return ApplyResult{Mode: entry.Mode, Noop: true, Changes: entry.Changes, Classification: "no-op"}, nil
	}

	transactionContext, cancelTransaction := context.WithTimeout(context.Background(), s.config.Transaction)
	defer cancelTransaction()
	candidateFiles, err := appliance.RenderCandidateFiles(entry.Appliance, entry.Registry)
	if err != nil {
		return ApplyResult{}, ErrCandidateInvalid
	}
	if err := s.validateCandidate(transactionContext, candidateFiles); err != nil {
		return ApplyResult{}, ErrCandidateInvalid
	}

	if err := s.savePrevious(snapshot); err != nil {
		return ApplyResult{}, ErrApplyFailed
	}
	if err := s.inject(StagePreviousSaved); err != nil {
		return ApplyResult{}, ErrApplyFailed
	}

	previousMeta := journalPair{Appliance: authorityMetaFor(snapshot.applianceExists, snapshot.applianceBytes), Nodes: authorityMetaFor(snapshot.nodesExists, snapshot.nodesBytes)}
	candidateApplianceBytes, err := appliance.MarshalCanonical(entry.Appliance)
	if err != nil {
		return ApplyResult{}, ErrCandidateInvalid
	}
	candidateNodesBytes, err := nodes.MarshalCanonical(entry.Registry)
	if err != nil {
		return ApplyResult{}, ErrCandidateInvalid
	}
	journal := importJournal{
		SchemaVersion: journalSchemaVersion, Mode: entry.Mode, Phase: phasePrepared,
		Previous:  previousMeta,
		Candidate: journalPair{Appliance: authorityMetaFor(true, candidateApplianceBytes), Nodes: authorityMetaFor(true, candidateNodesBytes)},
	}
	if err := s.writeJournal(journal); err != nil {
		return ApplyResult{}, ErrApplyFailed
	}
	if err := s.inject(StageJournalPrepared); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}

	if err := s.writeAuthorityIfChanged(s.config.AppliancePath, candidateApplianceBytes); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	if err := s.inject(StageApplianceCommitted); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	if entry.Mode != SettingsOnly {
		if err := s.writeAuthorityIfChanged(s.config.NodesPath, candidateNodesBytes); err != nil {
			return s.failAndRecover(transactionContext, snapshot)
		}
		if err := s.inject(StageNodesCommitted); err != nil {
			return s.failAndRecover(transactionContext, snapshot)
		}
	}
	journal.Phase = phaseAuthorities
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	if err := s.inject(StageAuthoritiesCommitted); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}

	if err := s.writeGenerated(candidateFiles, true); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	journal.Phase = phaseGenerated
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	if err := s.inject(StageGeneratedCommitted); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}

	activationContext, cancelActivation := context.WithTimeout(transactionContext, s.config.Activation)
	if err := s.activateAndVerify(activationContext, entry.Appliance, entry.Registry); err != nil {
		cancelActivation()
		return s.failAndRecover(transactionContext, snapshot)
	}
	cancelActivation()
	if err := s.inject(StageRestarted); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	if err := s.inject(StageReady); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	if err := s.inject(StageRuntimeVerified); err != nil {
		return s.failAndRecover(transactionContext, snapshot)
	}
	journal.Phase = phaseRuntimeVerified
	if err := s.writeJournal(journal); err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ApplyResult{}, ErrRecoveryFailed
	}
	if err := s.clearJournal(); err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ApplyResult{}, ErrRecoveryFailed
	}
	return ApplyResult{Mode: entry.Mode, Noop: false, Changes: entry.Changes, Classification: "applied"}, nil
}

// Cancel consumes a preview only when the binding matches.
func (s *Service) Cancel(binding, token string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if entry, ok := s.previews[token]; ok && entry.Binding == binding {
		delete(s.previews, token)
		zeroPreview(&entry)
	}
	s.mu.Unlock()
}

// Invalidate purges all previews bound to one caller/session binding.
func (s *Service) Invalidate(binding string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	for token, entry := range s.previews {
		if entry.Binding == binding {
			delete(s.previews, token)
			zeroPreview(&entry)
		}
	}
	s.mu.Unlock()
}

func normalizeMode(mode Mode) (Mode, error) {
	switch mode {
	case SettingsOnly, ReplaceRegistry, MergeRegistry:
		return mode, nil
	default:
		return "", ErrInvalidMode
	}
}

func decodeBundle(contents []byte, passphrase string) (backup.Bundle, bool, error) {
	if bundle, err := backup.ParseBundle(contents); err == nil {
		return bundle, false, nil
	}
	bundle, err := backup.OpenEncrypted(contents, passphrase)
	if err != nil {
		return backup.Bundle{}, false, ErrInvalidBundle
	}
	return bundle, true, nil
}

func (s *Service) captureAuthorities(ctx context.Context) (authoritySnapshot, error) {
	release, err := s.config.AuthorityLease.Acquire(ctx, s.config.AuthorityWaitTimeout)
	if err != nil {
		return authoritySnapshot{}, ErrAuthorityBusy
	}
	defer release()
	return s.captureAuthoritiesUnderLease()
}

func (s *Service) captureAuthoritiesUnderLease() (authoritySnapshot, error) {
	var snapshot authoritySnapshot
	applianceBytes, applianceExists, readErr := readOptionalAuthority(s.config.AppliancePath, appliance.MaxDocumentSize)
	snapshot.applianceBytes, snapshot.applianceExists = applianceBytes, applianceExists
	if readErr == nil && snapshot.applianceExists {
		var parseErr error
		snapshot.appliance, parseErr = appliance.Parse(snapshot.applianceBytes)
		snapshot.applianceValid = parseErr == nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		snapshot.applianceExists = false
	}

	nodesBytes, nodesExists, readErr := readOptionalAuthority(s.config.NodesPath, nodes.MaxRegistryDocument)
	snapshot.nodesBytes, snapshot.nodesExists = nodesBytes, nodesExists
	if readErr == nil && snapshot.nodesExists {
		var registry nodes.Registry
		if json.Unmarshal(snapshot.nodesBytes, &registry) == nil && registry.Validate() == nil {
			snapshot.registry = registry
			snapshot.nodesValid = true
			if _, strictErr := nodes.ParseCanonical(snapshot.nodesBytes); strictErr == nil {
				snapshot.nodesStrict = true
			}
		}
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		snapshot.nodesExists = false
	}
	snapshot.digest = combinedAuthorityDigest(snapshot.applianceExists, snapshot.applianceBytes, snapshot.nodesExists, snapshot.nodesBytes)
	return snapshot, nil
}

func snapshotBlockers(snapshot authoritySnapshot) []string {
	result := make([]string, 0, 4)
	if !snapshot.applianceExists {
		result = append(result, "appliance-authority-not-adopted")
	} else if !snapshot.applianceValid {
		result = append(result, "appliance-authority-invalid")
	}
	if !snapshot.nodesExists {
		result = append(result, "nodes-authority-unavailable")
	} else if !snapshot.nodesValid {
		result = append(result, "nodes-authority-invalid")
	}
	return result
}

func normalizeBlockers(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, allowed := allowedBlockers[value]; !allowed {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (s *Service) verifyCurrent(ctx context.Context, snapshot authoritySnapshot) error {
	if !snapshot.applianceExists || !snapshot.applianceValid || !snapshot.nodesExists || !snapshot.nodesValid {
		return ErrCompatibilityBlocked
	}
	policyFiles, err := appliance.RenderPolicyFiles(snapshot.appliance)
	if err != nil {
		return ErrCompatibilityBlocked
	}
	for _, name := range []string{"xray/02_dns.json", "xray/05_routing.json", "xray/07_observatory.json"} {
		actual, err := readRegularFile(filepath.Join(s.config.ConfigDir, filepath.Base(name)), appliance.MaxDocumentSize)
		if err != nil || !appliance.SemanticJSONEqual(actual, policyFiles[name]) {
			return ErrCompatibilityBlocked
		}
	}
	if err := s.verifyFixedFiles(); err != nil {
		return ErrCompatibilityBlocked
	}
	rendered, err := nodes.Render(snapshot.registry)
	if err != nil {
		return ErrCompatibilityBlocked
	}
	active, err := readRegularFile(s.config.ActiveOutboundsPath, nodes.MaxRegistryDocument)
	if err != nil || !bytes.Equal(active, rendered) {
		return ErrCompatibilityBlocked
	}
	if s.config.Activator == nil {
		return ErrCompatibilityBlocked
	}
	if err := s.config.Activator.VerifyOutboundTags(ctx, enabledTags(snapshot.registry)); err != nil {
		return ErrCompatibilityBlocked
	}
	return nil
}

func (s *Service) verifyFixedFiles() error {
	fixed, err := appliance.CompatibilityFiles()
	if err != nil {
		return err
	}
	for name, expected := range fixed {
		path := s.config.XkeenConfigPath
		if strings.HasPrefix(name, "xray/") {
			path = filepath.Join(s.config.ConfigDir, filepath.Base(name))
		}
		actual, err := readRegularFile(path, appliance.MaxDocumentSize)
		if err != nil || !appliance.SemanticJSONEqual(actual, expected) {
			return errors.New("fixed compatibility file drift")
		}
	}
	return nil
}

func (s *Service) validateCandidate(ctx context.Context, files map[string][]byte) error {
	if s.config.Activator == nil {
		return ErrCandidateInvalid
	}
	candidate, err := os.MkdirTemp("", "xkeen-restore-candidate-")
	if err != nil {
		return ErrCandidateInvalid
	}
	defer os.RemoveAll(candidate)
	if err := writeCandidateTree(candidate, files, s.syncDirectory); err != nil {
		return ErrCandidateInvalid
	}
	validationContext, cancel := context.WithTimeout(ctx, s.config.CandidateValidation)
	defer cancel()
	if err := s.config.Activator.ValidateCandidate(validationContext, filepath.Join(candidate, "xray")); err != nil {
		return ErrCandidateInvalid
	}
	return nil
}

func (s *Service) activateAndVerify(ctx context.Context, value appliance.Appliance, registry nodes.Registry) error {
	if s.config.Activator == nil {
		return ErrCandidateInvalid
	}
	if err := s.config.Activator.Restart(ctx); err != nil {
		return ErrCandidateInvalid
	}
	if err := s.config.Activator.WaitReady(ctx); err != nil {
		return ErrCandidateInvalid
	}
	if err := s.verifyGenerated(value, registry); err != nil {
		return ErrCandidateInvalid
	}
	if err := s.config.Activator.VerifyOutboundTags(ctx, enabledTags(registry)); err != nil {
		return ErrCandidateInvalid
	}
	return nil
}

func (s *Service) verifyGenerated(value appliance.Appliance, registry nodes.Registry) error {
	policyFiles, err := appliance.RenderPolicyFiles(value)
	if err != nil {
		return err
	}
	for _, name := range []string{"xray/02_dns.json", "xray/05_routing.json", "xray/07_observatory.json"} {
		actual, err := readRegularFile(filepath.Join(s.config.ConfigDir, filepath.Base(name)), appliance.MaxDocumentSize)
		if err != nil || !appliance.SemanticJSONEqual(actual, policyFiles[name]) {
			return errors.New("managed policy drift")
		}
	}
	rendered, err := nodes.Render(registry)
	if err != nil {
		return err
	}
	active, err := readRegularFile(s.config.ActiveOutboundsPath, nodes.MaxRegistryDocument)
	if err != nil || !bytes.Equal(active, rendered) {
		return errors.New("generated outbounds drift")
	}
	return nil
}

func (s *Service) writeGenerated(files map[string][]byte, inject bool) error {
	for _, item := range []struct {
		name  string
		path  string
		stage Stage
	}{
		{name: "xray/02_dns.json", path: filepath.Join(s.config.ConfigDir, "02_dns.json"), stage: StageDNSCommitted},
		{name: "xray/05_routing.json", path: filepath.Join(s.config.ConfigDir, "05_routing.json"), stage: StageRoutingCommitted},
		{name: "xray/07_observatory.json", path: filepath.Join(s.config.ConfigDir, "07_observatory.json"), stage: StageObservatoryCommitted},
		{name: "xray/04_outbounds.json", path: s.config.ActiveOutboundsPath, stage: StageOutboundsCommitted},
	} {
		contents, ok := files[item.name]
		if !ok {
			return ErrCandidateInvalid
		}
		if err := writeAtomicInExistingDir(item.path, contents, 0o600, s.syncDirectory); err != nil {
			return err
		}
		if inject {
			if err := s.inject(item.stage); err != nil {
				return err
			}
		}
	}
	return nil
}

func enabledTags(registry nodes.Registry) []string {
	result := make([]string, 0, len(registry.Nodes))
	for _, node := range registry.SortedNodes() {
		if node.Enabled {
			result = append(result, node.OutboundTag)
		}
	}
	return result
}

func (s *Service) failAndRecover(ctx context.Context, snapshot authoritySnapshot) (ApplyResult, error) {
	if err := s.recoverFromSnapshot(ctx, snapshot); err != nil {
		s.failClosed()
		s.markNotReady(ErrRecoveryFailed)
		return ApplyResult{}, ErrRecoveryFailed
	}
	return ApplyResult{}, ErrApplyFailed
}

func (s *Service) recoverFromSnapshot(_ context.Context, snapshot authoritySnapshot) error {
	recoveryContext, cancel := context.WithTimeout(context.Background(), s.config.Rollback)
	defer cancel()
	if err := s.verifyFixedFiles(); err != nil {
		return ErrRecoveryFailed
	}
	if err := s.writeExactAuthority(s.config.AppliancePath, snapshot.applianceExists, snapshot.applianceBytes); err != nil {
		return ErrRecoveryFailed
	}
	if err := s.writeExactAuthority(s.config.NodesPath, snapshot.nodesExists, snapshot.nodesBytes); err != nil {
		return ErrRecoveryFailed
	}
	files, err := appliance.RenderCandidateFiles(snapshot.appliance, snapshot.registry)
	if err != nil {
		return ErrRecoveryFailed
	}
	if err := s.writeGenerated(files, false); err != nil {
		return ErrRecoveryFailed
	}
	if err := s.activateAndVerify(recoveryContext, snapshot.appliance, snapshot.registry); err != nil {
		return ErrRecoveryFailed
	}
	if err := s.clearJournal(); err != nil {
		return ErrRecoveryFailed
	}
	return nil
}

func (s *Service) savePrevious(snapshot authoritySnapshot) error {
	parent := filepath.Dir(s.config.PreviousDir)
	if err := ensurePrivateDir(parent); err != nil {
		return err
	}
	staging := s.config.PreviousDir + ".staging"
	old := s.config.PreviousDir + ".old"
	if err := removeOwnedPath(staging); err != nil {
		return err
	}
	if err := removeOwnedPath(old); err != nil {
		return err
	}
	if err := ensurePrivateDir(staging); err != nil {
		return err
	}
	if err := writePreviousAuthority(staging, "appliance.json", ".appliance-absent", snapshot.applianceExists, snapshot.applianceBytes, s.syncDirectory); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := writePreviousAuthority(staging, "nodes.json", ".nodes-absent", snapshot.nodesExists, snapshot.nodesBytes, s.syncDirectory); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := verifyPreviousDirectory(staging, journalPair{Appliance: authorityMetaFor(snapshot.applianceExists, snapshot.applianceBytes), Nodes: authorityMetaFor(snapshot.nodesExists, snapshot.nodesBytes)}); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if info, err := os.Lstat(s.config.PreviousDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			_ = os.RemoveAll(staging)
			return errors.New("previous generation path is unsafe")
		}
		if err := os.Rename(s.config.PreviousDir, old); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
		if err := s.syncDirectory(parent); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	if err := os.Rename(staging, s.config.PreviousDir); err != nil {
		if _, oldErr := os.Lstat(old); oldErr == nil {
			_ = os.Rename(old, s.config.PreviousDir)
		}
		_ = os.RemoveAll(staging)
		return err
	}
	if err := s.syncDirectory(parent); err != nil {
		return err
	}
	if err := removeOwnedPath(old); err != nil {
		return err
	}
	if err := s.syncDirectory(parent); err != nil {
		return err
	}
	return nil
}

func writePreviousAuthority(root, name, absentName string, exists bool, contents []byte, syncDirectory func(string) error) error {
	if exists {
		return writeAtomicWithSync(filepath.Join(root, name), contents, 0o600, syncDirectory)
	}
	return writeAtomicWithSync(filepath.Join(root, absentName), []byte("1\n"), 0o600, syncDirectory)
}

func (s *Service) writeExactAuthority(path string, exists bool, contents []byte) error {
	if exists {
		return s.writeAuthorityIfChanged(path, contents)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrRecoveryFailed
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return s.syncDirectory(filepath.Dir(path))
}

func (s *Service) writeAuthorityIfChanged(path string, contents []byte) error {
	if existing, err := readRegularFile(path, maxAuthoritySizeForPath(path)); err == nil && bytes.Equal(existing, contents) {
		return nil
	}
	return writeAtomicWithSync(path, contents, 0o600, s.syncDirectory)
}

func maxAuthoritySizeForPath(path string) int {
	_ = path
	return max(appliance.MaxDocumentSize, nodes.MaxRegistryDocument)
}

func writeCandidateTree(root string, files map[string][]byte, syncDirectory func(string) error) error {
	if err := ensurePrivateDir(root); err != nil {
		return err
	}
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, relative := range keys {
		if relative == "" || filepath.IsAbs(filepath.FromSlash(relative)) || filepath.Clean(filepath.FromSlash(relative)) != filepath.FromSlash(relative) {
			return errors.New("candidate path is invalid")
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		if !pathWithin(path, root) {
			return errors.New("candidate path escapes root")
		}
		if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := writeAtomicWithSync(path, files[relative], 0o600, syncDirectory); err != nil {
			return err
		}
	}
	return nil
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func ensurePrivateDir(path string) error {
	if path == "" {
		return errors.New("private directory is unavailable")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("private directory is unsafe")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return nil
}

func removeOwnedPath(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if !info.IsDir() {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func writeAtomicWithSync(path string, contents []byte, mode os.FileMode, syncDirectory func(string) error) error {
	if path == "" {
		return errors.New("empty write path")
	}
	parent := filepath.Dir(path)
	if err := ensurePrivateDir(parent); err != nil {
		return err
	}
	return writeAtomicFile(path, contents, mode, syncDirectory)
}

// writeAtomicInExistingDir is used for active Xray generated files. The
// directory is an existing runtime-owned path: validate it, but never create
// or chmod it as part of a restore.
func writeAtomicInExistingDir(path string, contents []byte, mode os.FileMode, syncDirectory func(string) error) error {
	if path == "" {
		return errors.New("empty write path")
	}
	parent := filepath.Dir(path)
	if err := verifyExistingDirectory(parent); err != nil {
		return err
	}
	return writeAtomicFile(path, contents, mode, syncDirectory)
}

func writeAtomicFile(path string, contents []byte, mode os.FileMode, syncDirectory func(string) error) error {
	parent := filepath.Dir(path)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("write target is unsafe")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".xkeen-restore-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	_ = os.Chmod(path, mode)
	if syncDirectory == nil {
		syncDirectory = defaultSyncDirectory
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	return nil
}

func verifyExistingDirectory(path string) error {
	if path == "" {
		return errors.New("existing directory is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("existing directory is unsafe")
	}
	return nil
}

func defaultSyncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func syncDirectory(path string) error {
	return defaultSyncDirectory(path)
}

func readOptionalAuthority(path string, limit int) ([]byte, bool, error) {
	contents, err := readAuthorityFile(path, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return contents, true, nil
}

func readAuthorityFile(path string, limit int) ([]byte, error) {
	if err := checkPrivateFile(path); err != nil {
		return nil, err
	}
	contents, err := readRegularFile(path, limit)
	if err != nil {
		return nil, err
	}
	return contents, nil
}

func readPrivateFile(path string, limit int) ([]byte, error) {
	if err := checkPrivateFile(path); err != nil {
		return nil, err
	}
	return readRegularFile(path, limit)
}

func checkPrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("private file is unsafe")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return errors.New("private file permissions are invalid")
	}
	return checkPrivateDir(filepath.Dir(path))
}

func checkPrivateDir(path string) error {
	if path == "" {
		return errors.New("private directory is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("private directory is unsafe")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errors.New("private directory permissions are invalid")
	}
	return nil
}

func readRegularFile(path string, limit int) ([]byte, error) {
	if path == "" || limit <= 0 {
		return nil, errors.New("file is unavailable")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > int64(limit) {
		return nil, errors.New("file is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, errors.New("file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(contents) > limit {
		return nil, errors.New("file exceeds bounded size")
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || after.Size() != int64(len(contents)) {
		return nil, errors.New("file changed while reading")
	}
	return contents, nil
}

func authorityMetaFor(exists bool, contents []byte) authorityMeta {
	if !exists {
		return authorityMeta{Exists: false, Size: 0, SHA256: hex.EncodeToString(make([]byte, sha256.Size))}
	}
	digest := sha256.Sum256(contents)
	return authorityMeta{Exists: true, Size: len(contents), SHA256: hex.EncodeToString(digest[:])}
}

func combinedAuthorityDigest(applianceExists bool, applianceBytes []byte, nodesExists bool, nodesBytes []byte) [sha256.Size]byte {
	var encoded bytes.Buffer
	writeDigestPart(&encoded, applianceExists, applianceBytes)
	writeDigestPart(&encoded, nodesExists, nodesBytes)
	return sha256.Sum256(encoded.Bytes())
}

func writeDigestPart(buffer *bytes.Buffer, exists bool, contents []byte) {
	if exists {
		buffer.WriteByte(1)
	} else {
		buffer.WriteByte(0)
	}
	var size [8]byte
	value := uint64(len(contents))
	for index := range size {
		size[len(size)-1-index] = byte(value >> (8 * index))
	}
	buffer.Write(size[:])
	buffer.Write(contents)
}

func (s *Service) journalPath() string {
	return filepath.Join(s.config.StateDir, "appliance-import-transaction.json")
}

func (s *Service) writeJournal(journal importJournal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	contents, err := json.Marshal(journal)
	if err != nil || len(contents)+1 > MaxJournalBytes {
		return errors.New("journal exceeds bounded size")
	}
	contents = append(contents, '\n')
	return writeAtomicWithSync(s.journalPath(), contents, 0o600, s.syncDirectory)
}

func (s *Service) readJournal() (importJournal, bool, error) {
	contents, err := readPrivateFile(s.journalPath(), MaxJournalBytes)
	if errors.Is(err, os.ErrNotExist) {
		return importJournal{}, false, nil
	}
	if err != nil {
		return importJournal{}, false, err
	}
	var journal importJournal
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return importJournal{}, false, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return importJournal{}, false, errors.New("journal has trailing data")
	}
	if err := validateJournal(journal); err != nil {
		return importJournal{}, false, err
	}
	return journal, true, nil
}

func validateJournal(journal importJournal) error {
	if journal.SchemaVersion != journalSchemaVersion {
		return errors.New("journal schema is invalid")
	}
	if _, err := normalizeMode(journal.Mode); err != nil {
		return err
	}
	switch journal.Phase {
	case phasePrepared, phaseAuthorities, phaseGenerated, phaseRuntimeVerified:
	default:
		return errors.New("journal phase is invalid")
	}
	for _, pair := range []authorityMeta{journal.Previous.Appliance, journal.Previous.Nodes, journal.Candidate.Appliance, journal.Candidate.Nodes} {
		if pair.Size < 0 || pair.Size > nodes.MaxRegistryDocument || len(pair.SHA256) != sha256.Size*2 {
			return errors.New("journal integrity metadata is invalid")
		}
		if _, err := hex.DecodeString(pair.SHA256); err != nil {
			return errors.New("journal integrity metadata is invalid")
		}
		if !pair.Exists && pair.Size != 0 {
			return errors.New("journal absent metadata is invalid")
		}
	}
	return nil
}

func (s *Service) clearJournal() error {
	_, err := os.Lstat(s.journalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("journal path is unsafe")
	}
	if err := checkPrivateFile(s.journalPath()); err != nil {
		return errors.New("journal path is unsafe")
	}
	if err := os.Remove(s.journalPath()); err != nil {
		return err
	}
	if err := s.syncDirectory(s.config.StateDir); err != nil {
		return err
	}
	return nil
}

func verifyPreviousDirectory(path string, expected journalPair) error {
	if err := checkPrivateDir(path); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 4 {
		return errors.New("previous generation is unavailable")
	}
	allowed := map[string]struct{}{"appliance.json": {}, "nodes.json": {}, ".appliance-absent": {}, ".nodes-absent": {}}
	for _, entry := range entries {
		if _, ok := allowed[entry.Name()]; !ok || entry.IsDir() {
			return errors.New("previous generation contains unsupported material")
		}
	}
	for _, item := range []struct {
		name   string
		absent string
		meta   authorityMeta
		limit  int
	}{
		{name: "appliance.json", absent: ".appliance-absent", meta: expected.Appliance, limit: appliance.MaxDocumentSize},
		{name: "nodes.json", absent: ".nodes-absent", meta: expected.Nodes, limit: nodes.MaxRegistryDocument},
	} {
		contents, exists, err := readOptionalAuthority(filepath.Join(path, item.name), item.limit)
		if err != nil {
			return err
		}
		marker, markerErr := readPrivateFile(filepath.Join(path, item.absent), MaxPreviousMetadataBytes)
		markerExists := markerErr == nil
		if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
			return markerErr
		}
		if exists != item.meta.Exists || markerExists == item.meta.Exists {
			return errors.New("previous authority existence mismatch")
		}
		if exists {
			actual := authorityMetaFor(true, contents)
			if actual != item.meta {
				return errors.New("previous authority digest mismatch")
			}
		} else if !bytes.Equal(marker, []byte("1\n")) {
			return errors.New("previous authority marker is invalid")
		}
	}
	return nil
}

func (s *Service) loadPrevious(expected journalPair) (authoritySnapshot, error) {
	if err := verifyPreviousDirectory(s.config.PreviousDir, expected); err != nil {
		return authoritySnapshot{}, err
	}
	var snapshot authoritySnapshot
	var err error
	snapshot.applianceBytes, snapshot.applianceExists, err = readOptionalAuthority(filepath.Join(s.config.PreviousDir, "appliance.json"), appliance.MaxDocumentSize)
	if err != nil {
		return authoritySnapshot{}, err
	}
	snapshot.nodesBytes, snapshot.nodesExists, err = readOptionalAuthority(filepath.Join(s.config.PreviousDir, "nodes.json"), nodes.MaxRegistryDocument)
	if err != nil {
		return authoritySnapshot{}, err
	}
	if snapshot.applianceExists {
		snapshot.appliance, err = appliance.Parse(snapshot.applianceBytes)
		if err != nil {
			return authoritySnapshot{}, err
		}
		snapshot.applianceValid = true
	}
	if snapshot.nodesExists {
		if json.Unmarshal(snapshot.nodesBytes, &snapshot.registry) != nil || snapshot.registry.Validate() != nil {
			return authoritySnapshot{}, errors.New("previous registry is invalid")
		}
		snapshot.nodesValid = true
	}
	snapshot.digest = combinedAuthorityDigest(snapshot.applianceExists, snapshot.applianceBytes, snapshot.nodesExists, snapshot.nodesBytes)
	return snapshot, nil
}

func (s *Service) beginCoordinator(ctx context.Context) (func(), error) {
	if s.config.Coordinator == nil {
		return func() {}, nil
	}
	release, err := s.config.Coordinator.BeginApply(ctx)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return func() {}, nil
	}
	return release, nil
}

func (s *Service) beginRecoveryCoordinator(ctx context.Context) (func(), error) {
	if coordinator, ok := s.config.Coordinator.(recoveryCoordinator); ok {
		release, err := coordinator.BeginRecovery(ctx)
		if err != nil {
			return nil, err
		}
		if release == nil {
			return func() {}, nil
		}
		return release, nil
	}
	return s.beginCoordinator(ctx)
}

// enterMaintenance closes both mutation planes before a retained journal can
// be observed by another authority/lifecycle caller. It is idempotent because
// failure paths may discover the same unresolved journal more than once.
func (s *Service) enterMaintenance() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.maintenance = true
	s.ready = false
	if s.readyErr == nil {
		s.readyErr = ErrRecoveryFailed
	}
	s.mu.Unlock()
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Block()
	}
	if coordinator, ok := s.config.Coordinator.(maintenanceGate); ok {
		coordinator.EnterMaintenance()
	}
}

func (s *Service) releaseMaintenance() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.maintenance {
		s.mu.Unlock()
		return
	}
	s.maintenance = false
	s.mu.Unlock()
	if coordinator, ok := s.config.Coordinator.(maintenanceGate); ok {
		coordinator.ExitMaintenance()
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Unblock()
	}
}

func (s *Service) failClosed() {
	s.enterMaintenance()
}

func (s *Service) isMaintenance() bool {
	s.mu.Lock()
	maintenance := s.maintenance
	s.mu.Unlock()
	return maintenance
}

func (s *Service) inject(stage Stage) error {
	if s.config.InjectFailure == nil {
		return nil
	}
	return s.config.InjectFailure(stage)
}

func (s *Service) randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(s.config.Random, value); err != nil {
		clearBytes(value)
		return "", err
	}
	defer clearBytes(value)
	return hex.EncodeToString(value), nil
}

func (s *Service) peekPreview(binding, token string) (previewEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.previews[token]
	if !ok || entry.Binding != binding || !s.config.Now().Before(entry.ExpiresAt) {
		if ok {
			delete(s.previews, token)
			zeroPreview(&entry)
		}
		return previewEntry{}, false
	}
	return entry, true
}

func (s *Service) takePreviewIfCurrent(binding, token string) (previewEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.previews[token]
	if !ok || entry.Binding != binding || !s.config.Now().Before(entry.ExpiresAt) {
		if ok {
			delete(s.previews, token)
			zeroPreview(&entry)
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
			zeroPreview(&entry)
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
		entry := s.previews[oldestToken]
		delete(s.previews, oldestToken)
		zeroPreview(&entry)
	}
}

func (s *Service) markReady() {
	s.mu.Lock()
	s.ready = true
	s.readyErr = nil
	s.mu.Unlock()
}

func (s *Service) markNotReady(err error) {
	s.mu.Lock()
	s.ready = false
	s.readyErr = err
	s.mu.Unlock()
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func zeroPreview(entry *previewEntry) {
	if entry == nil {
		return
	}
	zeroAppliance(&entry.Appliance)
	zeroRegistry(&entry.Registry)
	entry.Binding = ""
	entry.Mode = ""
	entry.Blockers = nil
}

func zeroAppliance(value *appliance.Appliance) {
	if value == nil {
		return
	}
	*value = appliance.Appliance{}
}

func zeroRegistry(value *nodes.Registry) {
	if value == nil {
		return
	}
	for index := range value.Nodes {
		value.Nodes[index] = nodes.Node{}
	}
	for index := range value.Subscriptions {
		value.Subscriptions[index] = nodes.Subscription{}
	}
	*value = nodes.Registry{}
}

func zeroBundle(value *backup.Bundle) {
	if value == nil {
		return
	}
	zeroAppliance(&value.Appliance)
	if value.Nodes != nil {
		zeroRegistry(value.Nodes)
	}
	value.Nodes = nil
	value.Manifest = backup.Manifest{}
	value.Format = ""
	value.FormatVersion = 0
}

func cloneAppliance(value appliance.Appliance) (appliance.Appliance, error) {
	contents, err := appliance.MarshalCanonical(value)
	if err != nil {
		return appliance.Appliance{}, err
	}
	return appliance.Parse(contents)
}

func cloneRegistry(value nodes.Registry) (nodes.Registry, error) {
	contents, err := nodes.MarshalCanonical(value)
	if err != nil {
		return nodes.Registry{}, err
	}
	return nodes.ParseCanonical(contents)
}

func sameAppliance(left, right appliance.Appliance) bool {
	leftBytes, leftErr := appliance.MarshalCanonical(left)
	rightBytes, rightErr := appliance.MarshalCanonical(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func sameRegistry(left, right nodes.Registry) bool {
	leftBytes, leftErr := nodes.MarshalCanonical(left)
	rightBytes, rightErr := nodes.MarshalCanonical(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func mergeRegistry(destination, imported nodes.Registry) (nodes.Registry, error) {
	if err := destination.Validate(); err != nil {
		return nodes.Registry{}, ErrCandidateInvalid
	}
	if err := imported.Validate(); err != nil {
		return nodes.Registry{}, ErrCandidateInvalid
	}
	result, err := cloneRegistry(destination)
	if err != nil {
		return nodes.Registry{}, ErrCandidateInvalid
	}
	subscriptions := make(map[string]nodes.Subscription, len(result.Subscriptions))
	for _, value := range result.Subscriptions {
		subscriptions[value.ID] = value
	}
	for _, value := range imported.Subscriptions {
		if existing, ok := subscriptions[value.ID]; ok {
			if !sameSubscription(existing, value) {
				return nodes.Registry{}, ErrCandidateInvalid
			}
			continue
		}
		result.Subscriptions = append(result.Subscriptions, value)
		subscriptions[value.ID] = value
	}
	nodesByID := make(map[string]nodes.Node, len(result.Nodes))
	for _, value := range result.Nodes {
		nodesByID[value.ID] = value
	}
	for _, value := range imported.Nodes {
		if existing, ok := nodesByID[value.ID]; ok {
			if !sameNode(existing, value) {
				return nodes.Registry{}, ErrCandidateInvalid
			}
			continue
		}
		result.Nodes = append(result.Nodes, value)
		nodesByID[value.ID] = value
	}
	sort.Slice(result.Subscriptions, func(i, j int) bool { return result.Subscriptions[i].ID < result.Subscriptions[j].ID })
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].ID < result.Nodes[j].ID })
	if err := result.Validate(); err != nil {
		return nodes.Registry{}, ErrCandidateInvalid
	}
	return result, nil
}

func sortRegistryByID(registry *nodes.Registry) {
	if registry == nil {
		return
	}
	sort.Slice(registry.Subscriptions, func(i, j int) bool {
		return registry.Subscriptions[i].ID < registry.Subscriptions[j].ID
	})
	sort.Slice(registry.Nodes, func(i, j int) bool {
		return registry.Nodes[i].ID < registry.Nodes[j].ID
	})
}

func sameSubscription(left, right nodes.Subscription) bool {
	return reflect.DeepEqual(left, right)
}

func sameNode(left, right nodes.Node) bool {
	return reflect.DeepEqual(left, right)
}

func summarize(beforeAppliance appliance.Appliance, beforeRegistry nodes.Registry, afterAppliance appliance.Appliance, afterRegistry nodes.Registry) ChangeSummary {
	result := ChangeSummary{ApplianceChanged: !sameAppliance(beforeAppliance, afterAppliance)}
	result.SubscriptionsAdded, result.SubscriptionsRemoved, result.SubscriptionsChanged = summarizeSubscriptions(beforeRegistry.Subscriptions, afterRegistry.Subscriptions)
	result.NodesAdded, result.NodesRemoved, result.NodesChanged = summarizeNodes(beforeRegistry.Nodes, afterRegistry.Nodes)
	return result
}

func summarizeSubscriptions(before, after []nodes.Subscription) (int, int, int) {
	left := make(map[string]nodes.Subscription, len(before))
	for _, value := range before {
		left[value.ID] = value
	}
	right := make(map[string]nodes.Subscription, len(after))
	for _, value := range after {
		right[value.ID] = value
	}
	return summarizeMaps(left, right, sameSubscription)
}

func summarizeNodes(before, after []nodes.Node) (int, int, int) {
	left := make(map[string]nodes.Node, len(before))
	for _, value := range before {
		left[value.ID] = value
	}
	right := make(map[string]nodes.Node, len(after))
	for _, value := range after {
		right[value.ID] = value
	}
	return summarizeMaps(left, right, sameNode)
}

func summarizeMaps[T any](before, after map[string]T, equal func(T, T) bool) (int, int, int) {
	added, removed, changed := 0, 0, 0
	for key, value := range after {
		previous, exists := before[key]
		if !exists {
			added++
		} else if !equal(previous, value) {
			changed++
		}
	}
	for key := range before {
		if _, exists := after[key]; !exists {
			removed++
		}
	}
	return added, removed, changed
}
