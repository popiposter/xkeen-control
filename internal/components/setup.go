package components

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

const (
	SetupTransactionSchemaVersion = 1
	SetupOperation                = "setup"

	DefaultSetupPreviewTTL       = 5 * time.Minute
	DefaultSetupPreviewTimeout   = 2 * time.Minute
	DefaultSetupTransactionLimit = 10 * time.Minute
	DefaultSetupRecoveryTimeout  = 2 * time.Minute
	DefaultSetupStagingDir       = "/tmp/xkeen-control/setup"

	setupPhasePrepared           = "prepared"
	setupPhaseNodesCommitted     = "nodes-committed"
	setupPhaseAuthorityCommitted = "authority-committed"
	setupPhaseConfigCommitted    = "config-committed"
	setupPhaseGeodataCommitted   = "geodata-committed"
	setupPhaseXrayCommitted      = "xray-committed"
	setupPhaseXKeenCommitted     = "xkeen-committed"
	setupPhaseLifecycleCommitted = "lifecycle-committed"
	setupPhaseRuntimeVerified    = "runtime-verified"

	setupTokenBytes = 32
	setupMaxTokens  = 8
	setupMaxCreated = 32
)

var (
	ErrSetupUnavailable          = errors.New("setup service is unavailable")
	ErrSetupBusy                 = errors.New("setup is busy")
	ErrSetupPreviewExpired       = errors.New("setup preview is expired")
	ErrSetupPreviewStale         = errors.New("setup preview is stale")
	ErrSetupBlocked              = errors.New("setup layout is blocked")
	ErrSetupMaintenance          = errors.New("setup is in maintenance")
	ErrSetupSourceUnavailable    = errors.New("setup component source is unavailable")
	ErrSetupCandidateRejected    = errors.New("setup candidate was rejected")
	ErrSetupResourceInsufficient = errors.New("setup resources are insufficient")
	ErrSetupRuntimeUnavailable   = errors.New("setup runtime is unavailable")
	ErrSetupVerificationFailed   = errors.New("setup verification failed")
	ErrSetupTransactionRestored  = errors.New("setup transaction failed and fresh state was restored")
	ErrSetupTransactionUnproven  = errors.New("setup transaction outcome is not proven")
	ErrSetupRecoveryFailed       = errors.New("setup recovery failed")

	errSetupJournalInvalid = errors.New("setup transaction journal is invalid")
	errSetupLayoutInvalid  = errors.New("setup layout is invalid")
)

// SetupReasonCode is the closed safe vocabulary exposed by status/setup and
// setup mutation errors. It never contains a path, URL, upstream response or
// command output.
type SetupReasonCode string

const (
	SetupReasonFresh                SetupReasonCode = "fresh"
	SetupReasonAlreadyConfigured    SetupReasonCode = "already-configured"
	SetupReasonLayoutPartial        SetupReasonCode = "layout-partial"
	SetupReasonLayoutMixed          SetupReasonCode = "layout-mixed"
	SetupReasonAuthorityPresent     SetupReasonCode = "authority-present"
	SetupReasonJournalPending       SetupReasonCode = "journal-pending"
	SetupReasonMaintenance          SetupReasonCode = "maintenance"
	SetupReasonComponentUnavailable SetupReasonCode = "component-source-unavailable"
	SetupReasonCandidateStale       SetupReasonCode = "candidate-stale"
	SetupReasonCandidateRejected    SetupReasonCode = "candidate-rejected"
	SetupReasonResourceInsufficient SetupReasonCode = "resource-insufficient"
	SetupReasonTransactionRestored  SetupReasonCode = "transaction-restored"
	SetupReasonTransactionUnproven  SetupReasonCode = "transaction-unproven"
	SetupReasonRuntimeUnavailable   SetupReasonCode = "runtime-unavailable"
	SetupReasonVerificationFailed   SetupReasonCode = "verification-failed"
)

func (reason SetupReasonCode) valid() bool {
	switch reason {
	case SetupReasonFresh, SetupReasonAlreadyConfigured, SetupReasonLayoutPartial,
		SetupReasonLayoutMixed, SetupReasonAuthorityPresent, SetupReasonJournalPending,
		SetupReasonMaintenance, SetupReasonComponentUnavailable, SetupReasonCandidateStale,
		SetupReasonCandidateRejected, SetupReasonResourceInsufficient, SetupReasonTransactionRestored,
		SetupReasonTransactionUnproven, SetupReasonRuntimeUnavailable, SetupReasonVerificationFailed:
		return true
	default:
		return false
	}
}

// SetupProjection is the bounded status fragment consumed by the runtime
// collector. It is deliberately separate from SetupPlan and never contains a
// bearer token or candidate identity.
type SetupProjection struct {
	State      string          `json:"state"`
	Eligible   bool            `json:"eligible"`
	ReasonCode SetupReasonCode `json:"reasonCode,omitempty"`
}

type SetupXrayPlan struct {
	Tag       string `json:"tag"`
	Version   string `json:"version"`
	AssetName string `json:"assetName"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

type SetupGeodataItem struct {
	ID         string `json:"id"`
	Tag        string `json:"tag"`
	AssetName  string `json:"assetName"`
	ActiveName string `json:"activeName"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA256     string `json:"sha256"`
}

type SetupGeodataPlan struct {
	Items      []SetupGeodataItem `json:"items"`
	Generation string             `json:"generation"`
}

type SetupXKeenPlan struct {
	Repository         string `json:"repository"`
	Channel            string `json:"channel"`
	Tag                string `json:"tag"`
	Version            string `json:"version"`
	CommitSHA          string `json:"commitSha"`
	SourceParentSHA    string `json:"sourceParentSha"`
	AssetName          string `json:"assetName"`
	BlobSHA            string `json:"blobSha"`
	SizeBytes          int64  `json:"sizeBytes"`
	SHA256             string `json:"sha256"`
	GenerationSHA256   string `json:"generationSha256"`
	GenerationBytes    int64  `json:"generationBytes"`
	ArchiveMemberCount int    `json:"archiveMemberCount"`
	LifecycleClass     string `json:"lifecycleClass"`
	CompatibilityClass string `json:"compatibilityClass"`
}

type SetupLifecyclePlan struct {
	Name   string `json:"name"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

// SetupPlan is the only plan the server can issue. It has no caller-selected
// component, channel, repository, URL, path, command, timeout or policy.
type SetupPlan struct {
	SchemaVersion  int                `json:"schemaVersion"`
	ProductDefault bool               `json:"productDefault"`
	EmptyRegistry  bool               `json:"emptyRegistry"`
	Xray           SetupXrayPlan      `json:"xray"`
	Geodata        SetupGeodataPlan   `json:"geodata"`
	XKeen          SetupXKeenPlan     `json:"xkeen"`
	Lifecycle      SetupLifecyclePlan `json:"lifecycle"`
}

type SetupPreview struct {
	SchemaVersion int       `json:"schemaVersion"`
	PreviewToken  string    `json:"previewToken"`
	Operation     string    `json:"operation"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Plan          SetupPlan `json:"plan"`
}

type SetupResult struct {
	SchemaVersion int       `json:"schemaVersion"`
	Operation     string    `json:"operation"`
	State         string    `json:"state"`
	CompletedAt   time.Time `json:"completedAt"`
	Plan          SetupPlan `json:"plan"`
}

// SetupAPI is the intentionally small HTTP/UI capability. It contains no
// generic install or repair operation.
type SetupAPI interface {
	Preview(context.Context, string) (SetupPreview, error)
	Apply(context.Context, string, string) (SetupResult, error)
	Cancel(string, string)
	Invalidate(string)
	InvalidateAll()
	Status() SetupProjection
}

// SetupRuntime is the setup-specific runtime proof. VerifyEmpty deliberately
// does not reuse the ordinary non-empty outbound verifier.
type SetupRuntime interface {
	Start(context.Context) error
	WaitReady(context.Context) error
	ProbeReachable(context.Context) bool
	ValidateActiveConfig(context.Context) error
	VerifyEmpty(context.Context) error
}

// SetupRuntimeFuncs is a narrow production adapter and a deterministic test
// seam. It exposes only the fixed setup lifecycle/proof operations.
type SetupRuntimeFuncs struct {
	StartFunc                func(context.Context) error
	WaitReadyFunc            func(context.Context) error
	ProbeReachableFunc       func(context.Context) bool
	ValidateActiveConfigFunc func(context.Context) error
	VerifyEmptyFunc          func(context.Context) error
}

func (f SetupRuntimeFuncs) Start(ctx context.Context) error {
	if f.StartFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.StartFunc(ctx)
}
func (f SetupRuntimeFuncs) WaitReady(ctx context.Context) error {
	if f.WaitReadyFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.WaitReadyFunc(ctx)
}
func (f SetupRuntimeFuncs) ProbeReachable(ctx context.Context) bool {
	return f.ProbeReachableFunc != nil && f.ProbeReachableFunc(ctx)
}
func (f SetupRuntimeFuncs) ValidateActiveConfig(ctx context.Context) error {
	if f.ValidateActiveConfigFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.ValidateActiveConfigFunc(ctx)
}
func (f SetupRuntimeFuncs) VerifyEmpty(ctx context.Context) error {
	if f.VerifyEmptyFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.VerifyEmptyFunc(ctx)
}

// SetupPaths is the production-owned fixed path set. It is constructed by
// the server, never from an HTTP request.
type SetupPaths struct {
	XrayBinary          string
	XrayConfigDir       string
	XrayAssetDir        string
	XkeenBinary         string
	XkeenModuleDir      string
	XkeenConfig         string
	XkeenMarker         string
	LifecycleInit       string
	LegacyLifecycleInit string
	SiblingModule       string
	InstallHelper       string
	Appliance           string
	Nodes               string
	ActiveOutbounds     string
	Journal             string
	RestoreJournal      string
	StagingDir          string
}

func DefaultSetupPaths() SetupPaths {
	return SetupPaths{
		XrayBinary:          DefaultXrayBinary,
		XrayConfigDir:       DefaultXrayConfigDir,
		XrayAssetDir:        DefaultGeodataDir,
		XkeenBinary:         DefaultXkeenBinary,
		XkeenModuleDir:      DefaultXkeenModuleDir,
		XkeenConfig:         DefaultXkeenConfig,
		XkeenMarker:         DefaultXKeenMarkerPath,
		LifecycleInit:       DefaultXkeenRuntimeInit,
		LegacyLifecycleInit: DefaultXkeenLegacyRuntimeInit,
		SiblingModule:       filepath.Join(filepath.Dir(DefaultXkeenModuleDir), "_xkeen"),
		InstallHelper:       "/opt/root/install.sh",
		Appliance:           DefaultAppliancePath,
		Nodes:               "/opt/etc/xkeen-control/secrets/nodes.json",
		ActiveOutbounds:     filepath.Join(DefaultXrayConfigDir, "04_outbounds.json"),
		Journal:             DefaultComponentTransactionJournal,
		RestoreJournal:      filepath.Join(filepath.Dir(DefaultComponentTransactionJournal), "appliance-import-transaction.json"),
		StagingDir:          DefaultSetupStagingDir,
	}
}

type SetupConfig struct {
	Paths SetupPaths

	XrayResolver       XrayCandidateResolver
	XrayDownloader     XrayArtifactDownloader
	GeodataResolver    GeodataCandidateResolver
	GeodataDownloader  GeodataArtifactDownloader
	XKeenResolver      XKeenCandidateResolver
	XKeenDownloader    XKeenArtifactDownloader
	CandidateProbe     XrayCandidateProbe
	CandidateValidator XrayCandidateValidator
	Runtime            SetupRuntime

	MutationGate   *ComponentMutationGate
	Maintenance    *ComponentMaintenance
	Coordinator    XrayCoordinator
	AuthorityLease *authority.Lease

	PreviewTTL         time.Duration
	PreviewTimeout     time.Duration
	TransactionTimeout time.Duration
	RecoveryTimeout    time.Duration
	AvailableSpace     func(string) (uint64, error)
	SyncDirectory      func(string) error
	Now                func() time.Time
}

type setupCandidate struct {
	Xray      XrayReleaseIdentity
	Geodata   GeodataCandidateSet
	XKeen     XKeenReleaseIdentity
	Lifecycle []byte
}

type setupPreviewEntry struct {
	Token     string
	Binding   string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Candidate setupCandidate
	Plan      SetupPlan
	Sequence  uint64
}

type SetupService struct {
	config SetupConfig

	mu          sync.Mutex
	previews    map[string]setupPreviewEntry
	sequence    uint64
	applying    bool
	ready       bool
	readyErr    error
	maintenance bool
	previewed   bool
}

type setupLayout struct {
	state  string
	reason SetupReasonCode
}

func NewSetupService(config SetupConfig) *SetupService {
	paths := DefaultSetupPaths()
	mergeSetupPaths(&paths, config.Paths)
	config.Paths = paths
	if config.XrayResolver == nil {
		config.XrayResolver = NewXrayResolver(nil, nil)
	}
	if config.XrayDownloader == nil {
		config.XrayDownloader = NewXrayArtifactDownloader(nil, nil)
	}
	if config.GeodataResolver == nil {
		config.GeodataResolver = NewGeodataResolver(nil, nil)
	}
	if config.GeodataDownloader == nil {
		config.GeodataDownloader = NewGeodataArtifactDownloader(nil, nil)
	}
	if config.XKeenResolver == nil {
		config.XKeenResolver = NewXKeenResolver(nil, nil)
	}
	if config.XKeenDownloader == nil {
		config.XKeenDownloader = NewXKeenArtifactDownloader(nil, nil)
	}
	if config.CandidateProbe == nil {
		config.CandidateProbe = CommandXrayCandidateProbe{Binary: config.Paths.XrayBinary}
	}
	if config.CandidateValidator == nil {
		config.CandidateValidator = CommandXrayCandidateValidator{Binary: config.Paths.XrayBinary}
	}
	if config.MutationGate == nil {
		config.MutationGate = NewComponentMutationGate()
	}
	if config.PreviewTTL <= 0 {
		config.PreviewTTL = DefaultSetupPreviewTTL
	}
	if config.PreviewTimeout <= 0 {
		config.PreviewTimeout = DefaultSetupPreviewTimeout
	}
	if config.TransactionTimeout <= 0 || config.TransactionTimeout > DefaultSetupTransactionLimit {
		config.TransactionTimeout = DefaultSetupTransactionLimit
	}
	if config.RecoveryTimeout <= 0 {
		config.RecoveryTimeout = DefaultSetupRecoveryTimeout
	}
	if config.AvailableSpace == nil {
		config.AvailableSpace = availableFreeSpace
	}
	if config.SyncDirectory == nil {
		config.SyncDirectory = syncDirectory
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}

	service := &SetupService{config: config, previews: make(map[string]setupPreviewEntry), ready: true}
	if kind, present, err := componentJournalKind(config.Paths.Journal); err != nil {
		service.markMaintenance(ErrSetupRecoveryFailed)
	} else if present && kind == KindSetup {
		service.markMaintenance(ErrSetupRecoveryFailed)
	} else if pending, err := componentStagingRootPresent(config.Paths.StagingDir); err != nil {
		service.markMaintenance(ErrSetupRecoveryFailed)
	} else if pending {
		service.markMaintenance(ErrSetupRecoveryFailed)
	}
	return service
}

func mergeSetupPaths(target *SetupPaths, value SetupPaths) {
	if value.XrayBinary != "" {
		target.XrayBinary = value.XrayBinary
	}
	if value.XrayConfigDir != "" {
		target.XrayConfigDir = value.XrayConfigDir
	}
	if value.XrayAssetDir != "" {
		target.XrayAssetDir = value.XrayAssetDir
	}
	if value.XkeenBinary != "" {
		target.XkeenBinary = value.XkeenBinary
	}
	if value.XkeenModuleDir != "" {
		target.XkeenModuleDir = value.XkeenModuleDir
	}
	if value.XkeenConfig != "" {
		target.XkeenConfig = value.XkeenConfig
	}
	if value.XkeenMarker != "" {
		target.XkeenMarker = value.XkeenMarker
	}
	if value.LifecycleInit != "" {
		target.LifecycleInit = value.LifecycleInit
	}
	if value.LegacyLifecycleInit != "" {
		target.LegacyLifecycleInit = value.LegacyLifecycleInit
	}
	if value.SiblingModule != "" {
		target.SiblingModule = value.SiblingModule
	}
	if value.InstallHelper != "" {
		target.InstallHelper = value.InstallHelper
	}
	if value.Appliance != "" {
		target.Appliance = value.Appliance
	}
	if value.Nodes != "" {
		target.Nodes = value.Nodes
	}
	if value.ActiveOutbounds != "" {
		target.ActiveOutbounds = value.ActiveOutbounds
	}
	if value.Journal != "" {
		target.Journal = value.Journal
	}
	if value.RestoreJournal != "" {
		target.RestoreJournal = value.RestoreJournal
	}
	if value.StagingDir != "" {
		target.StagingDir = value.StagingDir
	}
}

func (s *SetupService) Ready() error {
	if s == nil {
		return ErrSetupUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		if s.readyErr != nil {
			return s.readyErr
		}
		return ErrSetupMaintenance
	}
	return nil
}

func (s *SetupService) HasPendingRecovery() (bool, error) {
	if s == nil {
		return false, ErrSetupUnavailable
	}
	kind, present, err := componentJournalKind(s.config.Paths.Journal)
	if err != nil {
		return false, err
	}
	if present && kind == KindSetup {
		return true, nil
	}
	return componentStagingRootPresent(s.config.Paths.StagingDir)
}

func (s *SetupService) Status() SetupProjection {
	if s == nil {
		return SetupProjection{State: "maintenance", ReasonCode: SetupReasonMaintenance}
	}
	s.mu.Lock()
	applying, maintenance, previewed := s.applying, s.maintenance, s.previewed
	s.mu.Unlock()
	if maintenance {
		return SetupProjection{State: "maintenance", ReasonCode: SetupReasonMaintenance}
	}
	if applying {
		return SetupProjection{State: "applying", Eligible: false}
	}
	layout, err := s.inspectLayout()
	if err != nil {
		return SetupProjection{State: "maintenance", ReasonCode: SetupReasonMaintenance}
	}
	if layout.state == "configured" {
		return SetupProjection{State: "ready", Eligible: false, ReasonCode: SetupReasonAlreadyConfigured}
	}
	if layout.state != "fresh" {
		return SetupProjection{State: "blocked", Eligible: false, ReasonCode: layout.reason}
	}
	if previewed {
		return SetupProjection{State: "previewable", Eligible: true, ReasonCode: SetupReasonFresh}
	}
	return SetupProjection{State: "fresh", Eligible: true, ReasonCode: SetupReasonFresh}
}

func (s *SetupService) markMaintenance(err error) {
	s.mu.Lock()
	s.ready = false
	s.readyErr = err
	s.maintenance = true
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Enter(KindSetup)
	}
}

func (s *SetupService) clearMaintenance() {
	s.mu.Lock()
	was := s.maintenance
	s.maintenance = false
	s.ready = true
	s.readyErr = nil
	s.mu.Unlock()
	if was && s.config.Maintenance != nil {
		s.config.Maintenance.Exit(KindSetup)
	}
}

func (s *SetupService) Preview(ctx context.Context, binding string) (SetupPreview, error) {
	if s == nil || strings.TrimSpace(binding) == "" {
		return SetupPreview{}, ErrSetupUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Ready(); err != nil {
		return SetupPreview{}, err
	}
	s.mu.Lock()
	if s.applying {
		s.mu.Unlock()
		return SetupPreview{}, ErrSetupBusy
	}
	s.mu.Unlock()
	layout, err := s.inspectLayout()
	if err != nil {
		return SetupPreview{}, ErrSetupMaintenance
	}
	if layout.state != "fresh" {
		return SetupPreview{}, &SetupLayoutError{ReasonCode: layout.reason}
	}
	previewContext, cancel := context.WithTimeout(ctx, s.config.PreviewTimeout)
	defer cancel()
	candidate, err := s.resolve(previewContext)
	if err != nil {
		return SetupPreview{}, err
	}
	plan, err := makeSetupPlan(candidate)
	if err != nil {
		return SetupPreview{}, ErrSetupCandidateRejected
	}
	now := s.config.Now()
	token, err := setupToken()
	if err != nil {
		return SetupPreview{}, ErrSetupUnavailable
	}
	entry := setupPreviewEntry{Token: token, Binding: binding, IssuedAt: now, ExpiresAt: now.Add(s.config.PreviewTTL), Candidate: candidate, Plan: plan}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applying || s.maintenance {
		return SetupPreview{}, ErrSetupBusy
	}
	s.sequence++
	entry.Sequence = s.sequence
	for len(s.previews) >= setupMaxTokens {
		s.removeOldestPreviewLocked()
	}
	s.previews[token] = entry
	s.previewed = true
	return SetupPreview{SchemaVersion: SetupTransactionSchemaVersion, PreviewToken: token, Operation: SetupOperation, ExpiresAt: entry.ExpiresAt, Plan: plan}, nil
}

func setupToken() (string, error) {
	var raw [setupTokenBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func ValidateSetupToken(value string) error {
	if value == "" || len(value) > 256 {
		return ErrSetupPreviewExpired
	}
	return nil
}

func (s *SetupService) takePreview(binding, token string) (setupPreviewEntry, error) {
	if s == nil || strings.TrimSpace(binding) == "" || ValidateSetupToken(token) != nil {
		return setupPreviewEntry{}, ErrSetupPreviewExpired
	}
	now := s.config.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.previews[token]
	if !ok || entry.Binding != binding || !now.Before(entry.ExpiresAt) {
		if ok && (!now.Before(entry.ExpiresAt) || entry.Binding == binding) {
			delete(s.previews, token)
			if len(s.previews) == 0 {
				s.previewed = false
			}
		}
		return setupPreviewEntry{}, ErrSetupPreviewExpired
	}
	delete(s.previews, token)
	if len(s.previews) == 0 {
		s.previewed = false
	}
	return entry, nil
}

func (s *SetupService) removeOldestPreviewLocked() {
	oldestToken := ""
	var oldest time.Time
	var sequence uint64
	for token, entry := range s.previews {
		if oldestToken == "" || entry.Sequence < sequence || entry.Sequence == sequence && entry.IssuedAt.Before(oldest) {
			oldestToken, oldest, sequence = token, entry.IssuedAt, entry.Sequence
		}
	}
	if oldestToken != "" {
		delete(s.previews, oldestToken)
	}
}

func (s *SetupService) Cancel(binding, token string) {
	if s == nil || strings.TrimSpace(binding) == "" || ValidateSetupToken(token) != nil {
		return
	}
	s.mu.Lock()
	if entry, ok := s.previews[token]; ok && entry.Binding == binding {
		delete(s.previews, token)
	}
	if len(s.previews) == 0 {
		s.previewed = false
	}
	s.mu.Unlock()
}

func (s *SetupService) Invalidate(binding string) {
	if s == nil || binding == "" {
		return
	}
	s.mu.Lock()
	for token, entry := range s.previews {
		if entry.Binding == binding {
			delete(s.previews, token)
		}
	}
	if len(s.previews) == 0 {
		s.previewed = false
	}
	s.mu.Unlock()
}

func (s *SetupService) InvalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.previews = make(map[string]setupPreviewEntry)
	s.previewed = false
	s.mu.Unlock()
}

type SetupLayoutError struct {
	ReasonCode SetupReasonCode
}

func (err *SetupLayoutError) Error() string { return ErrSetupBlocked.Error() }
func (err *SetupLayoutError) Unwrap() error { return ErrSetupBlocked }

func (s *SetupService) resolve(ctx context.Context) (setupCandidate, error) {
	if s.config.XrayResolver == nil || s.config.XrayDownloader == nil || s.config.GeodataResolver == nil || s.config.GeodataDownloader == nil || s.config.XKeenResolver == nil || s.config.XKeenDownloader == nil {
		return setupCandidate{}, ErrSetupSourceUnavailable
	}
	xray, err := s.config.XrayResolver.ResolveXray(ctx)
	if err != nil {
		return setupCandidate{}, ErrSetupSourceUnavailable
	}
	if !validXrayIdentity(xray) {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	geodata, err := s.config.GeodataResolver.ResolveGeodata(ctx)
	if err != nil {
		return setupCandidate{}, ErrSetupSourceUnavailable
	}
	if err := validateGeodataCandidateSet(geodata); err != nil {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	xkeen, err := s.config.XKeenResolver.ResolveXKeen(ctx)
	if err != nil {
		return setupCandidate{}, ErrSetupSourceUnavailable
	}
	if !validXKeenIdentity(xkeen) {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	lifecycle, err := setupLifecycleBytes()
	if err != nil {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	return setupCandidate{Xray: xray, Geodata: geodata, XKeen: xkeen, Lifecycle: lifecycle}, nil
}

func makeSetupPlan(candidate setupCandidate) (SetupPlan, error) {
	entry, err := installableXKeenEntry(candidate.XKeen)
	if err != nil {
		return SetupPlan{}, err
	}
	generationBytes, err := xkeenCatalogGenerationBytes(entry)
	if err != nil {
		return SetupPlan{}, err
	}
	items := make([]SetupGeodataItem, len(candidate.Geodata.Items))
	for index, item := range candidate.Geodata.Items {
		items[index] = SetupGeodataItem{ID: item.ID, Tag: item.Tag, AssetName: item.AssetName, ActiveName: item.ActiveName, SizeBytes: item.SizeBytes, SHA256: item.SHA256}
	}
	lifecycleDigest := sha256.Sum256(candidate.Lifecycle)
	return SetupPlan{
		SchemaVersion:  SetupTransactionSchemaVersion,
		ProductDefault: true,
		EmptyRegistry:  true,
		Xray:           SetupXrayPlan{Tag: candidate.Xray.Tag, Version: candidate.Xray.Version, AssetName: candidate.Xray.AssetName, SizeBytes: candidate.Xray.SizeBytes, SHA256: candidate.Xray.SHA256},
		Geodata:        SetupGeodataPlan{Items: items, Generation: candidate.Geodata.Generation},
		XKeen:          SetupXKeenPlan{Repository: candidate.XKeen.Repository, Channel: candidate.XKeen.Channel, Tag: candidate.XKeen.Tag, Version: candidate.XKeen.Version, CommitSHA: candidate.XKeen.CommitSHA, SourceParentSHA: candidate.XKeen.SourceParentSHA, AssetName: candidate.XKeen.AssetName, BlobSHA: candidate.XKeen.BlobSHA, SizeBytes: candidate.XKeen.SizeBytes, SHA256: candidate.XKeen.SHA256, GenerationSHA256: xkeenIdentityGeneration(candidate.XKeen), GenerationBytes: generationBytes, ArchiveMemberCount: len(entry.ArchiveMembers), LifecycleClass: entry.LifecycleClass, CompatibilityClass: entry.CompatibilityClass},
		Lifecycle:      SetupLifecyclePlan{Name: "S05xkeen", Mode: 0o755, SHA256: hex.EncodeToString(lifecycleDigest[:])},
	}, nil
}

func sameSetupCandidate(left, right setupCandidate) bool {
	return sameXrayIdentity(left.Xray, right.Xray) && sameGeodataCandidateSet(left.Geodata, right.Geodata) && sameXKeenIdentity(left.XKeen, right.XKeen) && bytes.Equal(left.Lifecycle, right.Lifecycle)
}

func (s *SetupService) inspectLayout() (setupLayout, error) {
	return s.inspectLayoutWithStaging("")
}

func (s *SetupService) inspectLayoutWithStaging(allowedStagingDir string) (setupLayout, error) {
	paths := s.config.Paths
	if kind, present, err := componentJournalKind(paths.Journal); err != nil {
		return setupLayout{}, err
	} else if present {
		if kind == KindSetup {
			return setupLayout{state: "blocked", reason: SetupReasonJournalPending}, nil
		}
		return setupLayout{state: "blocked", reason: SetupReasonJournalPending}, nil
	}
	if restore, err := componentTransactionPresent(paths.RestoreJournal); err != nil {
		return setupLayout{}, err
	} else if restore {
		return setupLayout{state: "blocked", reason: SetupReasonJournalPending}, nil
	}
	if pending, err := componentStagingRootPresent(paths.StagingDir); err != nil {
		return setupLayout{}, err
	} else if pending {
		if allowedStagingDir == "" || !setupStagingRootContainsOnly(paths.StagingDir, allowedStagingDir) {
			return setupLayout{state: "blocked", reason: SetupReasonJournalPending}, nil
		}
	}
	if err := validateSetupFixedPaths(paths); err != nil {
		return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
	}
	geodataEntries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return setupLayout{}, err
	}
	managed := make([]string, 0, 32)
	managed = append(managed, paths.XrayBinary, paths.XkeenBinary, paths.XkeenModuleDir, paths.XkeenConfig, paths.XkeenMarker, paths.LifecycleInit, paths.Appliance, paths.Nodes)
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		managed = append(managed, filepath.Join(paths.XrayConfigDir, name))
	}
	for _, entry := range geodataEntries {
		managed = append(managed, filepath.Join(paths.XrayAssetDir, entry.Name))
	}
	present := 0
	authorityPresent := false
	for _, path := range managed {
		state, err := setupPathState(path)
		if err != nil {
			return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
		}
		if state != setupPathAbsent {
			present++
		}
		if path == paths.Appliance || path == paths.Nodes {
			authorityPresent = authorityPresent || state != setupPathAbsent
		}
	}
	if conflict, err := setupDirectoryHasUnexpected(paths.XrayConfigDir, map[string]struct{}{"01_log.json": {}, "02_dns.json": {}, "03_inbounds.json": {}, "04_outbounds.json": {}, "05_routing.json": {}, "06_policy.json": {}, "07_observatory.json": {}, "08_api.json": {}}); err != nil {
		return setupLayout{}, err
	} else if conflict {
		return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
	}
	allowedAssets := make(map[string]struct{}, len(geodataEntries))
	for _, entry := range geodataEntries {
		allowedAssets[entry.Name] = struct{}{}
	}
	if conflict, err := setupDirectoryHasUnexpected(paths.XrayAssetDir, allowedAssets); err != nil {
		return setupLayout{}, err
	} else if conflict {
		return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
	}
	if forbidden, err := setupForbiddenPresent(paths); err != nil {
		return setupLayout{}, err
	} else if forbidden {
		return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
	}
	if present == 0 {
		return setupLayout{state: "fresh", reason: SetupReasonFresh}, nil
	}
	if s.setupConfigured(paths, managed) {
		return setupLayout{state: "configured", reason: SetupReasonAlreadyConfigured}, nil
	}
	if authorityPresent {
		return setupLayout{state: "blocked", reason: SetupReasonAuthorityPresent}, nil
	}
	return setupLayout{state: "blocked", reason: SetupReasonLayoutPartial}, nil
}

func setupStagingRootContainsOnly(root, allowedDir string) bool {
	rootState, err := setupPathState(root)
	if err != nil || rootState != setupPathDirectory {
		return false
	}
	allowedDir = filepath.Clean(allowedDir)
	root = filepath.Clean(root)
	relative, err := filepath.Rel(root, allowedDir)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Dir(relative) != "." {
		return false
	}
	allowedState, err := setupPathState(allowedDir)
	if err != nil || allowedState != setupPathDirectory {
		return false
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(allowedDir) {
		return false
	}
	return true
}

type setupPathKind uint8

const (
	setupPathAbsent setupPathKind = iota
	setupPathRegular
	setupPathDirectory
	setupPathInvalid
)

func setupPathState(path string) (setupPathKind, error) {
	if path == "" {
		return setupPathInvalid, errSetupLayoutInvalid
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return setupPathAbsent, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return setupPathInvalid, nil
	}
	if info.IsDir() {
		return setupPathDirectory, nil
	}
	if info.Mode().IsRegular() {
		return setupPathRegular, nil
	}
	return setupPathInvalid, nil
}

func setupDirectoryHasUnexpected(path string, allowed map[string]struct{}) (bool, error) {
	state, err := setupPathState(path)
	if err != nil {
		return false, err
	}
	if state == setupPathAbsent {
		return false, nil
	}
	if state != setupPathDirectory {
		return true, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, ok := allowed[entry.Name()]; !ok || entry.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

func setupForbiddenPresent(paths SetupPaths) (bool, error) {
	for _, path := range []string{paths.LegacyLifecycleInit, paths.SiblingModule, paths.InstallHelper} {
		state, err := setupPathState(path)
		if err != nil {
			return false, err
		}
		if state != setupPathAbsent {
			return true, nil
		}
	}
	return false, nil
}

func (s *SetupService) setupConfigured(paths SetupPaths, managed []string) bool {
	for _, path := range managed {
		state, err := setupPathState(path)
		if err != nil || state == setupPathAbsent || state == setupPathInvalid {
			return false
		}
	}
	if binaryInfo, err := os.Lstat(paths.XrayBinary); err != nil || binaryInfo.Mode()&os.ModeSymlink != 0 || !binaryInfo.Mode().IsRegular() || binaryInfo.Size() <= 0 || binaryInfo.Size() > MaxXrayCandidateBinaryBytes || runtime.GOOS != "windows" && binaryInfo.Mode().Perm()&0o111 == 0 {
		return false
	}
	appBytes, err := readBoundedSetupFile(paths.Appliance, appliance.MaxDocumentSize)
	if err != nil {
		return false
	}
	appValue, err := appliance.Parse(appBytes)
	if err != nil {
		return false
	}
	canonicalApp, err := appliance.MarshalCanonical(appValue)
	if err != nil || !bytes.Equal(appBytes, canonicalApp) {
		return false
	}
	nodesBytes, err := readBoundedSetupFile(paths.Nodes, nodes.MaxRegistryDocument)
	if err != nil {
		return false
	}
	registry, err := nodes.ParseCanonical(nodesBytes)
	if err != nil {
		return false
	}
	canonicalNodes, err := nodes.MarshalCanonical(registry)
	if err != nil || !bytes.Equal(nodesBytes, canonicalNodes) {
		return false
	}
	files, err := appliance.RenderCandidateFiles(appValue, registry)
	if err != nil {
		return false
	}
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		actual, readErr := readBoundedSetupFile(filepath.Join(paths.XrayConfigDir, name), appliance.MaxDocumentSize)
		if readErr != nil || !bytes.Equal(actual, files["xray/"+name]) {
			return false
		}
	}
	xkeenConfig, err := readBoundedSetupFile(paths.XkeenConfig, appliance.MaxDocumentSize)
	if err != nil || !bytes.Equal(xkeenConfig, files["xkeen/xkeen.json"]) {
		return false
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return false
	}
	geodata, err := readGeodataFiles(paths.XrayAssetDir, entries, true)
	if err != nil || !validGeodataSetMetadata(geodata) {
		return false
	}
	xkeenGeneration, err := readXKeenGeneration(paths.XkeenBinary, paths.XkeenModuleDir)
	if err != nil {
		return false
	}
	markerBytes, err := readBoundedSetupFile(paths.XkeenMarker, MaxXKeenMarkerBytes)
	if err != nil {
		return false
	}
	marker, err := parseXKeenMarker(markerBytes)
	if err != nil || !strings.EqualFold(marker.GenerationSHA256, xkeenGeneration.GenerationSHA256()) {
		return false
	}
	lifecycleInfo, err := os.Lstat(paths.LifecycleInit)
	if err != nil || lifecycleInfo.Mode()&os.ModeSymlink != 0 || !lifecycleInfo.Mode().IsRegular() || runtime.GOOS != "windows" && lifecycleInfo.Mode().Perm()&0o111 == 0 {
		return false
	}
	return true
}

func validateSetupFixedPaths(paths SetupPaths) error {
	for _, path := range []string{paths.XrayBinary, paths.XrayConfigDir, paths.XrayAssetDir, paths.XkeenBinary, paths.XkeenModuleDir, paths.XkeenConfig, paths.XkeenMarker, paths.LifecycleInit, paths.LegacyLifecycleInit, paths.SiblingModule, paths.InstallHelper, paths.Appliance, paths.Nodes, paths.Journal, paths.RestoreJournal, paths.StagingDir} {
		if strings.ContainsRune(path, '\x00') || filepath.Clean(path) == "." {
			return errSetupLayoutInvalid
		}
	}
	return nil
}

func (s *SetupService) applyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, s.config.TransactionTimeout)
}

func (s *SetupService) Apply(ctx context.Context, binding, token string) (SetupResult, error) {
	entry, err := s.takePreview(binding, token)
	if err != nil {
		return SetupResult{}, err
	}
	if err := s.Ready(); err != nil {
		return SetupResult{}, err
	}
	s.mu.Lock()
	if s.applying {
		s.mu.Unlock()
		return SetupResult{}, ErrSetupBusy
	}
	s.applying = true
	s.previewed = false
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.applying = false
		s.mu.Unlock()
	}()

	operationContext, cancel := s.applyContext(ctx)
	defer cancel()
	fresh, err := s.resolve(operationContext)
	if err != nil {
		return SetupResult{}, err
	}
	if !sameSetupCandidate(entry.Candidate, fresh) {
		return SetupResult{}, ErrSetupPreviewStale
	}
	prepared, err := s.prepare(operationContext, fresh, entry.Plan)
	if err != nil {
		return SetupResult{}, err
	}
	defer s.removeStage(prepared.stageDir)

	gateRelease, err := s.config.MutationGate.Acquire(operationContext)
	if err != nil {
		return SetupResult{}, ErrSetupBusy
	}
	defer gateRelease()
	ownedContext := withComponentMutationGate(operationContext, s.config.MutationGate)
	coordinatorRelease, err := s.beginApply(ownedContext)
	if err != nil {
		return SetupResult{}, ErrSetupBusy
	}
	defer coordinatorRelease()
	authorityRelease, err := s.acquireAuthority(ownedContext, false)
	if err != nil {
		return SetupResult{}, ErrSetupBusy
	}
	defer authorityRelease()

	layout, err := s.inspectLayoutWithStaging(prepared.stageDir)
	if err != nil {
		return SetupResult{}, ErrSetupMaintenance
	}
	if layout.state != "fresh" {
		return SetupResult{}, ErrSetupPreviewStale
	}
	journal := setupTransactionJournal{SchemaVersion: SetupTransactionSchemaVersion, Component: string(KindSetup), Operation: SetupOperation, Phase: setupPhasePrepared, Previous: setupPreviousRecord{AllAbsent: true}, Candidate: prepared.record()}
	if err := s.writeJournal(journal); err != nil {
		return SetupResult{}, ErrSetupTransactionUnproven
	}
	journalWritten := true
	if err := s.commit(ownedContext, &journal, prepared); err != nil {
		return SetupResult{}, s.failApply(journalWritten, journal, prepared, err)
	}
	if s.config.Runtime == nil {
		return SetupResult{}, s.failApply(true, journal, prepared, ErrSetupRuntimeUnavailable)
	}
	if err := s.config.Runtime.Start(ownedContext); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, ErrSetupRuntimeUnavailable)
	}
	if err := s.config.Runtime.WaitReady(ownedContext); err != nil || !s.config.Runtime.ProbeReachable(ownedContext) || s.config.Runtime.ValidateActiveConfig(ownedContext) != nil || s.config.Runtime.VerifyEmpty(ownedContext) != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, ErrSetupVerificationFailed)
	}
	if err := s.verifyInstalled(prepared); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, ErrSetupVerificationFailed)
	}
	journal.Phase = setupPhaseRuntimeVerified
	if err := s.writeJournal(journal); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, ErrSetupTransactionUnproven)
	}
	if err := s.clearJournal(); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, ErrSetupTransactionUnproven)
	}
	return SetupResult{SchemaVersion: SetupTransactionSchemaVersion, Operation: SetupOperation, State: "ready", CompletedAt: s.config.Now(), Plan: prepared.plan}, nil
}

func (s *SetupService) beginApply(ctx context.Context) (func(), error) {
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

func (s *SetupService) acquireAuthority(ctx context.Context, recovery bool) (func(), error) {
	if s.config.AuthorityLease == nil {
		return func() {}, nil
	}
	var (
		release func()
		err     error
	)
	if recovery {
		release, err = s.config.AuthorityLease.AcquireForRecovery(ctx, DefaultXrayAuthorityWaitTimeout)
	} else {
		release, err = s.config.AuthorityLease.Acquire(ctx, DefaultXrayAuthorityWaitTimeout)
	}
	if err != nil {
		return nil, err
	}
	if release == nil {
		return func() {}, nil
	}
	return release, nil
}

func (s *SetupService) InvalidateOnMaintenance() {
	s.markMaintenance(ErrSetupMaintenance)
}

func setupLifecycleBytes() ([]byte, error) {
	return []byte(setupLifecycleTemplate), nil
}

const setupLifecycleTemplate = "#!/bin/sh\nset -eu\naction=${1-}\nmode=${2-}\n[ \"$mode\" = on ] || exit 2\ncase \"$action\" in\n  start|restart) ;;\n  stop|status) exit 0 ;;\n  *) exit 2 ;;\nesac\nexport XRAY_LOCATION_ASSET=/opt/etc/xray/dat\nexport XKEEN_FOREGROUND=1\n/opt/sbin/xray run -confdir /opt/etc/xray/configs &\nxray_pid=$!\ntrap 'kill \"$xray_pid\" 2>/dev/null || true; wait \"$xray_pid\" 2>/dev/null || true; exit 130' HUP INT TERM\nwait \"$xray_pid\"\n"

func setupLifecycleDigest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

type preparedSetup struct {
	candidate       setupCandidate
	plan            SetupPlan
	stageDir        string
	configFiles     map[string][]byte
	appBytes        []byte
	nodesBytes      []byte
	xrayBinaryPath  string
	xrayMetadata    xrayBinaryMetadata
	geodataMetadata geodataSetMetadata
	xkeenPath       string
	xkeenMetadata   xkeenGenerationMetadata
	marker          []byte
	lifecycle       []byte
}

func (p preparedSetup) record() setupCandidateRecord {
	return setupCandidateRecord{
		Xray: p.candidate.Xray, XrayBinarySHA256: p.xrayMetadata.SHA256, XrayBinarySize: p.xrayMetadata.Size, XrayBinaryMode: p.xrayMetadata.Mode,
		Geodata: p.candidate.Geodata, XKeen: p.candidate.XKeen,
		LifecycleSHA256: setupLifecycleDigest(p.lifecycle),
	}
}

func (s *SetupService) prepare(ctx context.Context, candidate setupCandidate, plan SetupPlan) (preparedSetup, error) {
	if err := ctx.Err(); err != nil {
		return preparedSetup{}, err
	}
	value := appliance.ProductDefault()
	registry := nodes.NewRegistry()
	appBytes, err := appliance.MarshalCanonical(value)
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	nodesBytes, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	files, err := appliance.RenderCandidateFiles(value, registry)
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	lifecycle, err := setupLifecycleBytes()
	if err != nil || !validSetupLifecycle(lifecycle, plan.Lifecycle) {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	entry, err := installableXKeenEntry(candidate.XKeen)
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	generationBytes, err := xkeenCatalogGenerationBytes(entry)
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	var geodataBytes int64
	for _, item := range candidate.Geodata.Items {
		if item.SizeBytes <= 0 || geodataBytes > MaxGeodataCandidateBytes-item.SizeBytes {
			return preparedSetup{}, ErrSetupCandidateRejected
		}
		geodataBytes += item.SizeBytes
	}
	if err := ensurePrivateDirectory(s.config.Paths.StagingDir); err != nil {
		return preparedSetup{}, ErrSetupResourceInsufficient
	}
	need := uint64(candidate.Xray.SizeBytes) + uint64(geodataBytes) + uint64(candidate.XKeen.SizeBytes) + uint64(generationBytes) + uint64(MaxXrayCandidateBinaryBytes) + uint64(MaxComponentJournalBytes) + uint64(XrayFreeSpaceReserve)
	if free, freeErr := s.config.AvailableSpace(s.config.Paths.StagingDir); freeErr != nil || free < need {
		return preparedSetup{}, ErrSetupResourceInsufficient
	}
	stageDir, err := os.MkdirTemp(s.config.Paths.StagingDir, ".setup-")
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	cleanup := true
	defer func() {
		if cleanup {
			s.removeStage(stageDir)
		}
	}()
	configRoot := filepath.Join(stageDir, "config")
	if err := writeXrayCandidateTree(configRoot, files, s.config.SyncDirectory); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	assetRoot := filepath.Join(stageDir, "assets")
	if err := s.downloadGeodata(ctx, assetRoot, candidate.Geodata); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	geodataMetadata, err := readGeodataFiles(assetRoot, entries, true)
	if err != nil || !sameGeodataAgainstIdentity(geodataMetadata, candidate.Geodata) {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	xrayArchive := filepath.Join(stageDir, "xray.zip")
	if err := s.downloadXray(ctx, xrayArchive, candidate.Xray); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	xrayBinary := filepath.Join(stageDir, "xray")
	if err := extractXrayBinary(ctx, xrayArchive, xrayBinary); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	if err := validateXrayProbeResult(ctx, s.config.CandidateProbe.ProbeXrayCandidate(ctx, xrayBinary), candidate.Xray.Version); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	xrayMetadata, err := binaryMetadata(xrayBinary, candidate.Xray.Version, s.config.CandidateProbe, ctx)
	if err != nil || !validXrayBinaryMetadata(xrayMetadata, true) {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	if err := s.config.CandidateValidator.ValidateXrayCandidate(ctx, xrayBinary, filepath.Join(configRoot, "xray"), assetRoot); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	xkeenArchive := filepath.Join(stageDir, "xkeen.tar.gz")
	if err := s.downloadXKeen(ctx, xkeenArchive, candidate.XKeen); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	xkeenRoot := filepath.Join(stageDir, "xkeen")
	if err := ensureXKeenOwnedDirectory(xkeenRoot, xkeenOwnerValue); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	xkeenMetadata, err := extractXKeenArchive(ctx, xkeenArchive, xkeenRoot, entry)
	if err != nil || !strings.EqualFold(xkeenMetadata.GenerationSHA256(), xkeenIdentityGeneration(candidate.XKeen)) {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	_, marker, err := markerForGeneration(xkeenMetadata.GenerationSHA256())
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	cleanup = false
	return preparedSetup{candidate: candidate, plan: plan, stageDir: stageDir, configFiles: files, appBytes: appBytes, nodesBytes: nodesBytes, xrayBinaryPath: xrayBinary, xrayMetadata: xrayMetadata, geodataMetadata: geodataMetadata, xkeenPath: xkeenRoot, xkeenMetadata: xkeenMetadata, marker: marker, lifecycle: lifecycle}, nil
}

func validSetupLifecycle(contents []byte, plan SetupLifecyclePlan) bool {
	if len(contents) == 0 || plan.Name != "S05xkeen" || plan.Mode != 0o755 || plan.SHA256 != setupLifecycleDigest(contents) {
		return false
	}
	for _, forbidden := range []string{"opkg", "curl", "wget", "install.sh", "eval", "$@", "http://", "https://"} {
		if strings.Contains(strings.ToLower(string(contents)), strings.ToLower(forbidden)) {
			return false
		}
	}
	return true
}

func sameGeodataAgainstIdentity(meta geodataSetMetadata, candidate GeodataCandidateSet) bool {
	if len(meta.Items) != len(candidate.Items) {
		return false
	}
	for index, item := range candidate.Items {
		actual := meta.Items[index]
		if actual.ID != item.ID || actual.Name != item.ActiveName || actual.Size != item.SizeBytes || !strings.EqualFold(actual.SHA256, item.SHA256) {
			return false
		}
	}
	return true
}

func (s *SetupService) downloadXray(ctx context.Context, destination string, identity XrayReleaseIdentity) error {
	if destination == "" || identity.SizeBytes <= 0 || identity.SizeBytes > MaxCandidateAssetBytes {
		return ErrXrayArtifactRejected
	}
	if err := ensureSetupDirectory(filepath.Dir(destination)); err != nil {
		return ErrXrayArtifactRejected
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrXrayArtifactRejected
	}
	digest := sha256.New()
	writer := &xrayArtifactWriter{destination: file, hash: hashWriter{Hash: digest}, limit: identity.SizeBytes}
	downloadErr := s.config.XrayDownloader.DownloadXray(ctx, identity, writer)
	syncErr := file.Sync()
	closeErr := file.Close()
	if downloadErr != nil || syncErr != nil || closeErr != nil || writer.count != identity.SizeBytes || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), identity.SHA256) {
		_ = os.Remove(destination)
		return ErrXrayArtifactRejected
	}
	return nil
}

func (s *SetupService) downloadGeodata(ctx context.Context, directory string, set GeodataCandidateSet) error {
	if err := ensurePrivateDirectory(directory); err != nil {
		return ErrGeodataArtifactRejected
	}
	semaphore := make(chan struct{}, 2)
	results := make(chan error, len(set.Items))
	var workers sync.WaitGroup
	for _, identity := range set.Items {
		identity := identity
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				results <- ctx.Err()
				return
			}
			defer func() { <-semaphore }()
			if filepath.Base(identity.ActiveName) != identity.ActiveName || identity.ActiveName == "" {
				results <- ErrGeodataArtifactRejected
				return
			}
			path := filepath.Join(directory, identity.ActiveName)
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				results <- ErrGeodataArtifactRejected
				return
			}
			digest := sha256.New()
			writer := &geodataArtifactWriter{destination: file, hash: digest, limit: identity.SizeBytes}
			downloadErr := s.config.GeodataDownloader.DownloadGeodata(ctx, identity, writer)
			syncErr := file.Sync()
			closeErr := file.Close()
			if downloadErr != nil || syncErr != nil || closeErr != nil || writer.count != identity.SizeBytes || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), identity.SHA256) {
				_ = os.Remove(path)
				results <- ErrGeodataArtifactRejected
				return
			}
			results <- nil
		}()
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			return ErrGeodataArtifactRejected
		}
	}
	return s.config.SyncDirectory(directory)
}

func (s *SetupService) downloadXKeen(ctx context.Context, destination string, identity XKeenReleaseIdentity) error {
	if destination == "" || identity.SizeBytes <= 0 || identity.SizeBytes > MaxXKeenArchiveBytes {
		return ErrXKeenArtifactRejected
	}
	if err := ensureSetupDirectory(filepath.Dir(destination)); err != nil {
		return ErrXKeenArtifactRejected
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrXKeenArtifactRejected
	}
	digest := sha256.New()
	writer := &xkeenArtifactWriter{destination: file, hash: digest, limit: MaxXKeenArchiveBytes}
	downloadErr := s.config.XKeenDownloader.DownloadXKeen(ctx, identity, writer)
	syncErr := file.Sync()
	closeErr := file.Close()
	if downloadErr != nil || syncErr != nil || closeErr != nil || writer.count != identity.SizeBytes || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), identity.SHA256) {
		_ = os.Remove(destination)
		return ErrXKeenArtifactRejected
	}
	return nil
}

const (
	setupCreatedNodes     = "authority/nodes"
	setupCreatedAppliance = "authority/appliance"
	setupCreatedConfig    = "config"
	setupCreatedGeodata   = "geodata"
	setupCreatedXray      = "xray"
	setupCreatedXKeen     = "xkeen"
	setupCreatedLifecycle = "lifecycle"
)

type setupPreviousRecord struct {
	AllAbsent bool `json:"allAbsent"`
}

type setupCandidateRecord struct {
	Xray             XrayReleaseIdentity  `json:"xray"`
	XrayBinarySHA256 string               `json:"xrayBinarySha256"`
	XrayBinarySize   int64                `json:"xrayBinarySize"`
	XrayBinaryMode   uint32               `json:"xrayBinaryMode"`
	Geodata          GeodataCandidateSet  `json:"geodata"`
	XKeen            XKeenReleaseIdentity `json:"xkeen"`
	LifecycleSHA256  string               `json:"lifecycleSha256"`
}

type setupTransactionJournal struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Component     string               `json:"component"`
	Operation     string               `json:"operation"`
	Phase         string               `json:"phase"`
	Previous      setupPreviousRecord  `json:"previous"`
	Candidate     setupCandidateRecord `json:"candidate"`
	Created       []string             `json:"created,omitempty"`
}

func (p preparedSetup) candidateRecordMatches(record setupCandidateRecord) bool {
	return sameXrayIdentity(p.candidate.Xray, record.Xray) && sameGeodataCandidateSet(p.candidate.Geodata, record.Geodata) && sameXKeenIdentity(p.candidate.XKeen, record.XKeen) && record.LifecycleSHA256 == setupLifecycleDigest(p.lifecycle)
}

func validateSetupJournal(journal setupTransactionJournal) error {
	if journal.SchemaVersion != SetupTransactionSchemaVersion || journal.Component != string(KindSetup) || journal.Operation != SetupOperation || !journal.Previous.AllAbsent || !validXrayIdentity(journal.Candidate.Xray) || !validXrayBinaryMetadata(xrayBinaryMetadata{Exists: true, Version: journal.Candidate.Xray.Version, SHA256: journal.Candidate.XrayBinarySHA256, Size: journal.Candidate.XrayBinarySize, Mode: journal.Candidate.XrayBinaryMode}, true) || validateGeodataCandidateSet(journal.Candidate.Geodata) != nil || !validXKeenIdentity(journal.Candidate.XKeen) || !isHexSHA256(journal.Candidate.LifecycleSHA256) || len(journal.Created) > setupMaxCreated {
		return errSetupJournalInvalid
	}
	switch journal.Phase {
	case setupPhasePrepared, setupPhaseNodesCommitted, setupPhaseAuthorityCommitted, setupPhaseConfigCommitted, setupPhaseGeodataCommitted, setupPhaseXrayCommitted, setupPhaseXKeenCommitted, setupPhaseLifecycleCommitted, setupPhaseRuntimeVerified:
	default:
		return errSetupJournalInvalid
	}
	allowed := map[string]struct{}{setupCreatedNodes: {}, setupCreatedAppliance: {}, setupCreatedConfig: {}, setupCreatedGeodata: {}, setupCreatedXray: {}, setupCreatedXKeen: {}, setupCreatedLifecycle: {}}
	seen := make(map[string]struct{}, len(journal.Created))
	for _, value := range journal.Created {
		if _, ok := allowed[value]; !ok {
			return errSetupJournalInvalid
		}
		if _, ok := seen[value]; ok {
			return errSetupJournalInvalid
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (s *SetupService) writeJournal(journal setupTransactionJournal) error {
	if err := validateSetupJournal(journal); err != nil {
		return err
	}
	contents, err := json.Marshal(journal)
	if err != nil || len(contents)+1 > MaxComponentJournalBytes {
		return errSetupJournalInvalid
	}
	return writeAtomicComponentFile(s.config.Paths.Journal, append(contents, '\n'), 0o600, s.config.SyncDirectory)
}

func (s *SetupService) readJournal() (setupTransactionJournal, bool, error) {
	kind, present, err := componentJournalKind(s.config.Paths.Journal)
	if err != nil {
		return setupTransactionJournal{}, false, errSetupJournalInvalid
	}
	if !present {
		return setupTransactionJournal{}, false, nil
	}
	if kind != KindSetup {
		return setupTransactionJournal{}, false, nil
	}
	contents, err := readPrivateComponentFile(s.config.Paths.Journal, MaxComponentJournalBytes)
	if err != nil {
		return setupTransactionJournal{}, false, errSetupJournalInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var journal setupTransactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return setupTransactionJournal{}, false, errSetupJournalInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || validateSetupJournal(journal) != nil {
		return setupTransactionJournal{}, false, errSetupJournalInvalid
	}
	return journal, true, nil
}

func (s *SetupService) updateJournal(journal *setupTransactionJournal, phase string, created string) error {
	if journal == nil {
		return errSetupJournalInvalid
	}
	journal.Phase = phase
	if created != "" {
		journal.Created = append(journal.Created, created)
	}
	return s.writeJournal(*journal)
}

func (s *SetupService) commit(ctx context.Context, journal *setupTransactionJournal, prepared preparedSetup) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := s.config.Paths
	if err := s.writeSetupExclusive(paths.Nodes, prepared.nodesBytes, 0o600); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseNodesCommitted, setupCreatedNodes); err != nil {
		return err
	}
	if err := s.writeSetupExclusive(paths.Appliance, prepared.appBytes, 0o600); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseAuthorityCommitted, setupCreatedAppliance); err != nil {
		return err
	}
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		contents, ok := prepared.configFiles["xray/"+name]
		if !ok || s.writeSetupExclusive(filepath.Join(paths.XrayConfigDir, name), contents, 0o600) != nil {
			return ErrSetupCandidateRejected
		}
	}
	xkeenConfig, ok := prepared.configFiles["xkeen/xkeen.json"]
	if !ok {
		return ErrSetupCandidateRejected
	}
	if err := s.writeSetupExclusive(paths.XkeenConfig, xkeenConfig, 0o600); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseConfigCommitted, setupCreatedConfig); err != nil {
		return err
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		contents, readErr := readBoundedSetupFile(filepath.Join(prepared.stageDir, "assets", entry.Name), MaxGeodataFileBytes)
		if readErr != nil || s.writeSetupExclusive(filepath.Join(paths.XrayAssetDir, entry.Name), contents, 0o600) != nil {
			return ErrSetupCandidateRejected
		}
	}
	if err := s.updateJournal(journal, setupPhaseGeodataCommitted, setupCreatedGeodata); err != nil {
		return err
	}
	xrayContents, err := readBoundedSetupFile(prepared.xrayBinaryPath, MaxXrayCandidateBinaryBytes)
	if err != nil {
		return ErrSetupCandidateRejected
	}
	if err := s.writeSetupExclusive(paths.XrayBinary, xrayContents, 0o700); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseXrayCommitted, setupCreatedXray); err != nil {
		return err
	}
	if err := requireSetupPathAbsent(paths.XkeenBinary); err != nil {
		return err
	}
	if err := requireSetupPathAbsent(paths.XkeenModuleDir); err != nil {
		return err
	}
	if err := copyXKeenGenerationToActive(filepath.Join(prepared.xkeenPath, "xkeen"), filepath.Join(prepared.xkeenPath, ".xkeen"), paths.XkeenBinary, paths.XkeenModuleDir, prepared.xkeenMetadata, s.config.SyncDirectory); err != nil {
		return err
	}
	if err := s.writeSetupExclusive(paths.XkeenMarker, prepared.marker, 0o600); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseXKeenCommitted, setupCreatedXKeen); err != nil {
		return err
	}
	if err := s.writeSetupExclusive(paths.LifecycleInit, prepared.lifecycle, 0o755); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseLifecycleCommitted, setupCreatedLifecycle); err != nil {
		return err
	}
	return nil
}

func copyXKeenGenerationToActive(sourceBinary, sourceModule, destinationBinary, destinationModule string, expected xkeenGenerationMetadata, syncDirectory func(string) error) error {
	if !validXKeenGenerationMetadata(expected) || sourceBinary == "" || sourceModule == "" || destinationBinary == "" || destinationModule == "" {
		return errXKeenGenerationInvalid
	}
	if setupPath, err := setupPathState(destinationBinary); err != nil || setupPath != setupPathAbsent {
		return errXKeenGenerationInvalid
	}
	if setupPath, err := setupPathState(destinationModule); err != nil || setupPath != setupPathAbsent {
		return errXKeenGenerationInvalid
	}
	if err := ensureSetupDirectory(filepath.Dir(destinationBinary)); err != nil {
		return err
	}
	if err := ensureSetupDirectory(filepath.Dir(destinationModule)); err != nil {
		return err
	}
	var binaryEntry xkeenGenerationEntry
	for _, entry := range expected.Entries {
		if entry.Path == "xkeen" {
			binaryEntry = entry
			break
		}
	}
	if err := copyXKeenFile(sourceBinary, destinationBinary, binaryEntry); err != nil {
		return err
	}
	if err := copyXKeenDirectory(sourceModule, destinationModule, expected.Entries); err != nil {
		return err
	}
	actual, err := readXKeenGeneration(destinationBinary, destinationModule)
	if err != nil || !sameXKeenGeneration(actual, expected) {
		return errXKeenGenerationInvalid
	}
	if syncDirectory != nil {
		if err := syncDirectory(filepath.Dir(destinationBinary)); err != nil {
			return err
		}
		if filepath.Clean(filepath.Dir(destinationModule)) != filepath.Clean(filepath.Dir(destinationBinary)) {
			if err := syncDirectory(filepath.Dir(destinationModule)); err != nil {
				return err
			}
		}
	}
	return nil
}

func requireSetupPathAbsent(path string) error {
	state, err := setupPathState(path)
	if err != nil || state != setupPathAbsent {
		return ErrSetupPreviewStale
	}
	return nil
}

func (s *SetupService) writeSetupExclusive(path string, contents []byte, mode os.FileMode) error {
	if path == "" || len(contents) == 0 {
		return errSetupLayoutInvalid
	}
	if err := requireSetupPathAbsent(path); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := ensureSetupDirectory(parent); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".xkeen-setup-")
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
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return s.config.SyncDirectory(parent)
}

func ensureSetupDirectory(path string) error {
	if path == "" {
		return errSetupLayoutInvalid
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errSetupLayoutInvalid
	}
	return nil
}

func readBoundedSetupFile(path string, limit int64) ([]byte, error) {
	if path == "" || limit <= 0 {
		return nil, errSetupLayoutInvalid
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errSetupLayoutInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(contents)) != info.Size() || int64(len(contents)) > limit {
		return nil, errSetupLayoutInvalid
	}
	return contents, nil
}

func (s *SetupService) verifyInstalled(prepared preparedSetup) error {
	paths := s.config.Paths
	app, err := readBoundedSetupFile(paths.Appliance, appliance.MaxDocumentSize)
	if err != nil || !bytes.Equal(app, prepared.appBytes) {
		return ErrSetupVerificationFailed
	}
	if _, err := appliance.Parse(app); err != nil {
		return ErrSetupVerificationFailed
	}
	registry, err := readBoundedSetupFile(paths.Nodes, nodes.MaxRegistryDocument)
	if err != nil || !bytes.Equal(registry, prepared.nodesBytes) {
		return ErrSetupVerificationFailed
	}
	if _, err := nodes.ParseCanonical(registry); err != nil {
		return ErrSetupVerificationFailed
	}
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		contents, readErr := readBoundedSetupFile(filepath.Join(paths.XrayConfigDir, name), appliance.MaxDocumentSize)
		if readErr != nil || !bytes.Equal(contents, prepared.configFiles["xray/"+name]) {
			return ErrSetupVerificationFailed
		}
	}
	xkeenConfig, err := readBoundedSetupFile(paths.XkeenConfig, appliance.MaxDocumentSize)
	if err != nil || !bytes.Equal(xkeenConfig, prepared.configFiles["xkeen/xkeen.json"]) {
		return ErrSetupVerificationFailed
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return ErrSetupVerificationFailed
	}
	actualGeodata, err := readGeodataFiles(paths.XrayAssetDir, entries, true)
	if err != nil || !sameGeodataAgainstIdentity(actualGeodata, prepared.candidate.Geodata) {
		return ErrSetupVerificationFailed
	}
	actualXray, err := binaryMetadataWithoutProbe(paths.XrayBinary, prepared.candidate.Xray.Version)
	if err != nil || !sameBinaryMetadata(actualXray, prepared.xrayMetadata) {
		return ErrSetupVerificationFailed
	}
	actualXKeen, err := readXKeenGeneration(paths.XkeenBinary, paths.XkeenModuleDir)
	if err != nil || !sameXKeenGeneration(actualXKeen, prepared.xkeenMetadata) {
		return ErrSetupVerificationFailed
	}
	marker, err := readBoundedSetupFile(paths.XkeenMarker, MaxXKeenMarkerBytes)
	if err != nil || !bytes.Equal(marker, prepared.marker) {
		return ErrSetupVerificationFailed
	}
	if lifecycle, readErr := readBoundedSetupFile(paths.LifecycleInit, 16<<10); readErr != nil || !bytes.Equal(lifecycle, prepared.lifecycle) {
		return ErrSetupVerificationFailed
	}
	if forbidden, err := setupForbiddenPresent(paths); err != nil || forbidden {
		return ErrSetupVerificationFailed
	}
	return nil
}

func (s *SetupService) failApply(journalWritten bool, journal setupTransactionJournal, prepared preparedSetup, cause error) error {
	if !journalWritten {
		return cause
	}
	recoveryContext, cancel := context.WithTimeout(context.Background(), s.config.RecoveryTimeout)
	defer cancel()
	if err := s.rollbackCreated(recoveryContext, journal, prepared); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return ErrSetupTransactionUnproven
	}
	if err := s.clearJournal(); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return ErrSetupTransactionUnproven
	}
	return ErrSetupTransactionRestored
}

func (s *SetupService) rollbackCreated(ctx context.Context, journal setupTransactionJournal, prepared preparedSetup) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := s.config.Paths
	for _, name := range []string{"S05xkeen"} {
		if err := removeSetupFileExact(paths.LifecycleInit, prepared.lifecycle); err != nil {
			return err
		}
		_ = name
	}
	if err := removeSetupFileExact(paths.XkeenMarker, prepared.marker); err != nil {
		return err
	}
	if err := removeSetupGeneration(paths.XkeenBinary, paths.XkeenModuleDir, prepared.xkeenMetadata); err != nil {
		return err
	}
	if err := removeSetupFileHash(paths.XrayBinary, prepared.xrayMetadata.Size, prepared.xrayMetadata.SHA256); err != nil {
		return err
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var expected geodataFileMetadata
		for _, item := range prepared.geodataMetadata.Items {
			if item.Name == entry.Name {
				expected = item
				break
			}
		}
		if expected.Name == "" {
			return errSetupLayoutInvalid
		}
		if err := removeSetupFileHash(filepath.Join(paths.XrayAssetDir, entry.Name), expected.Size, expected.SHA256); err != nil {
			return err
		}
	}
	if err := removeSetupFileMap(paths.XrayConfigDir, prepared.configFiles, "xray/"); err != nil {
		return err
	}
	if err := removeSetupFileExact(paths.XkeenConfig, prepared.configFiles["xkeen/xkeen.json"]); err != nil {
		return err
	}
	if err := removeSetupFileExact(paths.Appliance, prepared.appBytes); err != nil {
		return err
	}
	if err := removeSetupFileExact(paths.Nodes, prepared.nodesBytes); err != nil {
		return err
	}
	return nil
}

func removeSetupFileMap(directory string, files map[string][]byte, prefix string) error {
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		if err := removeSetupFileExact(filepath.Join(directory, name), files[prefix+name]); err != nil {
			return err
		}
	}
	return nil
}

func removeSetupFileExact(path string, expected []byte) error {
	state, err := setupPathState(path)
	if err != nil {
		return err
	}
	if state == setupPathAbsent {
		return nil
	}
	if state != setupPathRegular || len(expected) == 0 {
		return errSetupLayoutInvalid
	}
	contents, err := readBoundedSetupFile(path, int64(len(expected)))
	if err != nil || !bytes.Equal(contents, expected) {
		return errSetupLayoutInvalid
	}
	return os.Remove(path)
}

func removeSetupFileHash(path string, size int64, expected string) error {
	state, err := setupPathState(path)
	if err != nil {
		return err
	}
	if state == setupPathAbsent {
		return nil
	}
	if state != setupPathRegular || size <= 0 || !isHexSHA256(expected) {
		return errSetupLayoutInvalid
	}
	contents, err := readBoundedSetupFile(path, size)
	if err != nil || int64(len(contents)) != size {
		return errSetupLayoutInvalid
	}
	digest := sha256.Sum256(contents)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), expected) {
		return errSetupLayoutInvalid
	}
	return os.Remove(path)
}

func removeSetupGeneration(binaryPath, modulePath string, expected xkeenGenerationMetadata) error {
	binaryState, err := setupPathState(binaryPath)
	if err != nil {
		return err
	}
	moduleState, err := setupPathState(modulePath)
	if err != nil {
		return err
	}
	if binaryState == setupPathAbsent && moduleState == setupPathAbsent {
		return nil
	}
	if binaryState != setupPathRegular || moduleState != setupPathDirectory {
		return errSetupLayoutInvalid
	}
	actual, err := readXKeenGeneration(binaryPath, modulePath)
	if err != nil || !sameXKeenGeneration(actual, expected) {
		return errSetupLayoutInvalid
	}
	if err := os.RemoveAll(modulePath); err != nil {
		return err
	}
	if err := os.Remove(binaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *SetupService) clearJournal() error {
	journal, present, err := s.readJournal()
	if err != nil {
		return err
	}
	if !present || journal.Component != string(KindSetup) {
		return nil
	}
	if err := os.Remove(s.config.Paths.Journal); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(s.config.Paths.Journal))
}

func (s *SetupService) removeStage(path string) {
	if path == "" {
		return
	}
	root := filepath.Clean(s.config.Paths.StagingDir)
	target := filepath.Clean(path)
	if filepath.Dir(target) != root || target == root {
		return
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	_ = os.RemoveAll(target)
}

func (s *SetupService) removeStagingRootEntries() error {
	root := filepath.Clean(s.config.Paths.StagingDir)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errSetupLayoutInvalid
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".setup-") {
			return errSetupLayoutInvalid
		}
		path := filepath.Join(root, entry.Name())
		child, childErr := os.Lstat(path)
		if childErr != nil || child.Mode()&os.ModeSymlink != 0 {
			return errSetupLayoutInvalid
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *SetupService) beginRecovery(ctx context.Context) (func(), error) {
	if s.config.Coordinator == nil {
		return func() {}, nil
	}
	if coordinator, ok := s.config.Coordinator.(XrayRecoveryCoordinator); ok {
		release, err := coordinator.BeginRecovery(ctx)
		if err != nil {
			return nil, err
		}
		if release == nil {
			return func() {}, nil
		}
		return release, nil
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

func (s *SetupService) RecoverStartup(ctx context.Context) error {
	if s == nil {
		return ErrSetupUnavailable
	}
	pending, err := s.HasPendingRecovery()
	if err != nil {
		s.markMaintenance(ErrSetupRecoveryFailed)
		return ErrSetupRecoveryFailed
	}
	if !pending {
		return s.Ready()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	recoveryContext, cancel := context.WithTimeout(ctx, s.config.RecoveryTimeout)
	defer cancel()
	gateRelease, err := s.config.MutationGate.Acquire(recoveryContext)
	if err != nil {
		return s.recoveryFailure()
	}
	defer gateRelease()
	ownedContext := withComponentMutationGate(recoveryContext, s.config.MutationGate)
	coordinatorRelease, err := s.beginRecovery(ownedContext)
	if err != nil {
		return s.recoveryFailure()
	}
	defer coordinatorRelease()
	authorityRelease, err := s.acquireAuthority(ownedContext, true)
	if err != nil {
		return s.recoveryFailure()
	}
	defer authorityRelease()

	journal, journalPresent, journalErr := s.readJournal()
	if journalErr != nil {
		return s.recoveryFailure()
	}
	if !journalPresent {
		// A crash during prepare can leave only the private setup staging
		// subtree. It has no journaled activation to finish; validate and remove
		// only its owned entries before classifying the durable layout again.
		if err := s.removeStagingRootEntries(); err != nil {
			return s.recoveryFailure()
		}
		layout, layoutErr := s.inspectLayout()
		if layoutErr != nil || layout.state != "fresh" {
			return s.recoveryFailure()
		}
		s.clearMaintenance()
		return nil
	}
	prepared, err := s.preparedFromJournal(journal)
	if err != nil {
		return s.recoveryFailure()
	}
	if journal.Phase == setupPhaseRuntimeVerified && s.runtimeProof(ownedContext) == nil && s.verifyInstalled(prepared) == nil {
		if err := s.clearJournal(); err != nil || s.removeStagingRootEntries() != nil {
			return s.recoveryFailure()
		}
		s.clearMaintenance()
		return nil
	}
	if err := s.rollbackCreated(ownedContext, journal, prepared); err != nil {
		return s.recoveryFailure()
	}
	if err := s.clearJournal(); err != nil || s.removeStagingRootEntries() != nil {
		return s.recoveryFailure()
	}
	s.clearMaintenance()
	return nil
}

func (s *SetupService) runtimeProof(ctx context.Context) error {
	if s.config.Runtime == nil {
		return ErrSetupRuntimeUnavailable
	}
	if err := s.config.Runtime.WaitReady(ctx); err != nil || !s.config.Runtime.ProbeReachable(ctx) || s.config.Runtime.ValidateActiveConfig(ctx) != nil || s.config.Runtime.VerifyEmpty(ctx) != nil {
		return ErrSetupVerificationFailed
	}
	return nil
}

func (s *SetupService) preparedFromJournal(journal setupTransactionJournal) (preparedSetup, error) {
	candidate := setupCandidate{Xray: journal.Candidate.Xray, Geodata: journal.Candidate.Geodata, XKeen: journal.Candidate.XKeen}
	lifecycle, err := setupLifecycleBytes()
	if err != nil || setupLifecycleDigest(lifecycle) != journal.Candidate.LifecycleSHA256 {
		return preparedSetup{}, errSetupJournalInvalid
	}
	value := appliance.ProductDefault()
	registry := nodes.NewRegistry()
	files, err := appliance.RenderCandidateFiles(value, registry)
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	appBytes, err := appliance.MarshalCanonical(value)
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	nodesBytes, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	meta := geodataSetMetadata{Items: make([]geodataFileMetadata, len(candidate.Geodata.Items))}
	for index, item := range candidate.Geodata.Items {
		if index >= len(entries) {
			return preparedSetup{}, errSetupJournalInvalid
		}
		meta.Items[index] = geodataFileMetadata{ID: item.ID, Name: item.ActiveName, Size: item.SizeBytes, Mode: 0o600, SHA256: item.SHA256}
	}
	meta.Generation = geodataFileGeneration(meta.Items)
	if !validGeodataSetMetadata(meta) {
		return preparedSetup{}, errSetupJournalInvalid
	}
	xrayMeta := xrayBinaryMetadata{Exists: true, Version: candidate.Xray.Version, SHA256: journal.Candidate.XrayBinarySHA256, Size: journal.Candidate.XrayBinarySize, Mode: journal.Candidate.XrayBinaryMode}
	if !validXrayBinaryMetadata(xrayMeta, true) {
		return preparedSetup{}, errSetupJournalInvalid
	}
	xkeenMeta := xkeenGenerationMetadata{}
	if state, stateErr := setupPathState(s.config.Paths.XkeenBinary); stateErr != nil {
		return preparedSetup{}, errSetupJournalInvalid
	} else if state != setupPathAbsent {
		current, readErr := readXKeenGeneration(s.config.Paths.XkeenBinary, s.config.Paths.XkeenModuleDir)
		if readErr != nil || !strings.EqualFold(current.GenerationSHA256(), xkeenIdentityGeneration(candidate.XKeen)) {
			return preparedSetup{}, errSetupJournalInvalid
		}
		xkeenMeta = current
	}
	_, marker, markerErr := markerForGeneration(xkeenIdentityGeneration(candidate.XKeen))
	if markerErr != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	plan, err := makeSetupPlan(setupCandidate{Xray: candidate.Xray, Geodata: candidate.Geodata, XKeen: candidate.XKeen, Lifecycle: lifecycle})
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	return preparedSetup{candidate: candidate, plan: plan, configFiles: files, appBytes: appBytes, nodesBytes: nodesBytes, xrayMetadata: xrayMeta, geodataMetadata: meta, xkeenMetadata: xkeenMeta, marker: marker, lifecycle: lifecycle}, nil
}

func (s *SetupService) recoveryFailure() error {
	s.markMaintenance(ErrSetupRecoveryFailed)
	return ErrSetupRecoveryFailed
}

var _ SetupAPI = (*SetupService)(nil)
