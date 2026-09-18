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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	DefaultSetupPreviousDir      = "/opt/etc/xkeen-control/previous/setup"
	DefaultSetupXKeenActivation  = "/opt/sbin/.xkeen-control-setup-activation"
	DefaultSetupCronRoot         = "/opt/var/spool/cron/crontabs/root"
	DefaultSetupSystemCronRoot   = "/etc/crontabs/root"
	DefaultSetupCronDir          = "/opt/etc/cron.d"

	setupPhasePrepared             = "prepared"
	setupPhaseSnapshotIntent       = "snapshot-intent"
	setupPhaseSnapshotReady        = "snapshot-ready"
	setupPhaseNodesCommitted       = "nodes-committed"
	setupPhaseAuthorityCommitted   = "authority-committed"
	setupPhaseConfigCommitted      = "config-committed"
	setupPhaseGeodataCommitted     = "geodata-committed"
	setupPhaseXrayCommitted        = "xray-committed"
	setupPhaseXKeenStaged          = "xkeen-staged"
	setupPhaseXKeenBinaryCommitted = "xkeen-binary-committed"
	setupPhaseXKeenModuleCommitted = "xkeen-module-committed"
	setupPhaseXKeenCommitted       = "xkeen-committed"
	setupPhaseLifecycleCommitted   = "lifecycle-committed"
	setupPhaseWritersRetired       = "writers-retired"
	setupPhaseRuntimeStarted       = "runtime-started"
	setupPhaseSelectionReconciled  = "selection-reconciled"
	setupPhaseRuntimeVerified      = "runtime-verified"

	setupTokenBytes          = 32
	setupMaxTokens           = 8
	setupMaxCreated          = 32
	setupMaxSnapshotEntries  = 1024
	setupMaxSnapshotBytes    = 256 << 20
	setupMaxCronBytes        = 128 << 10
	setupMaxSelectionBytes   = 8 << 10
	setupGeodataWriterScript = "update-" + "geodata.sh"
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
	ErrSetupWriterConflict       = errors.New("setup detected a competing automatic writer")

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
	SetupReasonManagedTakeover      SetupReasonCode = "managed-takeover"
	SetupReasonLegacyTakeover       SetupReasonCode = "legacy-xkeen-takeover"
	SetupReasonPolicyUnsupported    SetupReasonCode = "policy-unsupported"
	SetupReasonNodeAuthorityInvalid SetupReasonCode = "node-authority-invalid"
	SetupReasonProfileUnavailable   SetupReasonCode = "profile-source-unavailable"
	SetupReasonWriterConflict       SetupReasonCode = "writer-conflict"
	SetupReasonPersistentSpace      SetupReasonCode = "persistent-space-insufficient"
)

func (reason SetupReasonCode) valid() bool {
	switch reason {
	case SetupReasonFresh, SetupReasonAlreadyConfigured, SetupReasonLayoutPartial,
		SetupReasonLayoutMixed, SetupReasonAuthorityPresent, SetupReasonJournalPending,
		SetupReasonMaintenance, SetupReasonComponentUnavailable, SetupReasonCandidateStale,
		SetupReasonCandidateRejected, SetupReasonResourceInsufficient, SetupReasonTransactionRestored,
		SetupReasonTransactionUnproven, SetupReasonRuntimeUnavailable, SetupReasonVerificationFailed,
		SetupReasonManagedTakeover, SetupReasonLegacyTakeover, SetupReasonPolicyUnsupported,
		SetupReasonNodeAuthorityInvalid, SetupReasonProfileUnavailable, SetupReasonWriterConflict,
		SetupReasonPersistentSpace:
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

type SetupProfilePlan struct {
	Action string   `json:"action"`
	Count  int      `json:"count"`
	Labels []string `json:"labels,omitempty"`
}

type SetupPolicyPlan struct {
	Action string `json:"action"`
}

type SetupWriterPlan struct {
	Class string `json:"class"`
}

// SetupPlan is the only plan the server can issue. It has no caller-selected
// component, channel, repository, URL, path, command, timeout or policy.
type SetupPlan struct {
	SchemaVersion  int                `json:"schemaVersion"`
	SetupClass     string             `json:"setupClass"`
	Destructive    bool               `json:"destructive"`
	ProductDefault bool               `json:"productDefault"`
	EmptyRegistry  bool               `json:"emptyRegistry"`
	Profiles       SetupProfilePlan   `json:"profiles"`
	Policy         SetupPolicyPlan    `json:"policy"`
	Writers        []SetupWriterPlan  `json:"writers,omitempty"`
	PanelPreserved bool               `json:"panelPreserved"`
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

// SetupRuntimeQuiescer is required once Setup has started a target runtime.
// It is intentionally an optional extension so ordinary test/runtime adapters
// and the existing component contracts do not acquire a generic lifecycle API.
type SetupRuntimeQuiescer interface {
	Stop(context.Context) error
	VerifyStopped(context.Context) error
}

// SetupRuntimeVerifier proves a non-empty migrated/preserved registry through
// the ordinary outbound verification contract without changing that contract.
type SetupRuntimeVerifier interface {
	Verify(context.Context, []string) error
}

// SetupSelectionReconciler keeps selection.json under the existing typed C.1
// owner. Setup supplies only the post-activation enabled tags; it never edits
// panel-local selection bytes directly.
type SetupSelectionReconciler interface {
	ReconcileSetupSelection(context.Context, []string) error
}

// SetupSelectionTransaction is the recoverable extension used by takeover.
// The bytes are an opaque, bounded representation owned and validated by C.1;
// Setup only journals them and asks that same owner to restore them.
type SetupSelectionTransaction interface {
	SetupSelectionSnapshot(context.Context) ([]byte, error)
	RestoreSetupSelection(context.Context, []byte) error
}

// SetupRuntimeFuncs is a narrow production adapter and a deterministic test
// seam. It exposes only the fixed setup lifecycle/proof operations.
type SetupRuntimeFuncs struct {
	StartFunc                func(context.Context) error
	WaitReadyFunc            func(context.Context) error
	ProbeReachableFunc       func(context.Context) bool
	ValidateActiveConfigFunc func(context.Context) error
	VerifyEmptyFunc          func(context.Context) error
	StopFunc                 func(context.Context) error
	VerifyStoppedFunc        func(context.Context) error
	VerifyFunc               func(context.Context, []string) error
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
func (f SetupRuntimeFuncs) Stop(ctx context.Context) error {
	if f.StopFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.StopFunc(ctx)
}
func (f SetupRuntimeFuncs) VerifyStopped(ctx context.Context) error {
	if f.VerifyStoppedFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.VerifyStoppedFunc(ctx)
}
func (f SetupRuntimeFuncs) Verify(ctx context.Context, tags []string) error {
	if f.VerifyFunc == nil {
		return ErrSetupRuntimeUnavailable
	}
	return f.VerifyFunc(ctx, tags)
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
	XkeenActivation     string
	LifecycleInit       string
	LegacyLifecycleInit string
	SiblingModule       string
	InstallHelper       string
	Appliance           string
	Nodes               string
	LegacyOutbounds     string
	ActiveOutbounds     string
	Journal             string
	RestoreJournal      string
	StagingDir          string
	PreviousDir         string
	CronPaths           []string
	WriterScripts       []string
	PanelPaths          []string
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
		XkeenActivation:     DefaultSetupXKeenActivation,
		LifecycleInit:       DefaultXkeenRuntimeInit,
		LegacyLifecycleInit: DefaultXkeenLegacyRuntimeInit,
		SiblingModule:       filepath.Join(filepath.Dir(DefaultXkeenModuleDir), "_xkeen"),
		InstallHelper:       "/opt/root/install.sh",
		Appliance:           DefaultAppliancePath,
		Nodes:               "/opt/etc/xkeen-control/secrets/nodes.json",
		LegacyOutbounds:     "/opt/etc/xkeen-control/secrets/04_outbounds.json",
		ActiveOutbounds:     filepath.Join(DefaultXrayConfigDir, "04_outbounds.json"),
		Journal:             DefaultComponentTransactionJournal,
		RestoreJournal:      filepath.Join(filepath.Dir(DefaultComponentTransactionJournal), "appliance-import-transaction.json"),
		StagingDir:          DefaultSetupStagingDir,
		PreviousDir:         DefaultSetupPreviousDir,
		CronPaths:           []string{DefaultSetupCronRoot, DefaultSetupSystemCronRoot, "/opt/etc/cron.d"},
		WriterScripts: []string{
			"/opt/etc/xkeen/speed_failover_watchdog.sh",
			"/opt/etc/xkeen-control/speed_failover_watchdog.sh",
			"/opt/etc/xkeen-control/run-bounded-speed-benchmark.sh",
			"/opt/etc/xkeen-control/xkeen-control-watchdog",
			filepath.Join("/opt/etc/xkeen", setupGeodataWriterScript),
			filepath.Join("/opt/etc/xkeen-control", setupGeodataWriterScript),
		},
		PanelPaths: []string{
			"/opt/etc/xkeen-control/auth/password.bcrypt",
			"/opt/etc/xkeen-control/listen-address",
			"/opt/etc/xkeen-control/state/installed-release.json",
			"/opt/etc/xkeen-control/state/update-policy.json",
			"/opt/etc/xkeen-control/state/selection.json",
			"/opt/etc/xkeen-control/state/component-policy.json",
		},
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
	Selection          SetupSelectionReconciler

	MutationGate   *ComponentMutationGate
	Maintenance    *ComponentMaintenance
	Coordinator    XrayCoordinator
	AuthorityLease *authority.Lease

	PreviewTTL         time.Duration
	PreviewTimeout     time.Duration
	TransactionTimeout time.Duration
	RecoveryTimeout    time.Duration
	AvailableSpace     func(string) (uint64, error)
	SameFilesystem     func(string, string) (bool, error)
	SyncDirectory      func(string) error
	Now                func() time.Time
}

type setupCandidate struct {
	Xray       XrayReleaseIdentity
	Geodata    GeodataCandidateSet
	XKeen      XKeenReleaseIdentity
	Lifecycle  []byte
	Registry   nodes.Registry
	NodesBytes []byte
	Appliance  appliance.Appliance
	AppBytes   []byte
	Source     setupSourceManifest
	Writers    []setupWriter
}

type setupSourceManifest struct {
	Class          string
	ProfileAction  string
	PolicyAction   string
	ProfileCount   int
	ProfileLabels  []string
	Digest         string
	RegistryDigest string
	PolicyDigest   string
	WritersDigest  string
	PanelPreserved bool
}

type setupWriter struct {
	Class  string
	Path   string
	Digest string
}

type setupLayout struct {
	state   string
	reason  SetupReasonCode
	class   string
	source  setupSourceManifest
	writers []setupWriter
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

func NewSetupService(config SetupConfig) *SetupService {
	paths := DefaultSetupPaths()
	mergeSetupPaths(&paths, config.Paths)
	if paths.XkeenActivation == DefaultSetupXKeenActivation && paths.XkeenBinary != DefaultXkeenBinary {
		paths.XkeenActivation = filepath.Join(filepath.Dir(paths.XkeenBinary), ".xkeen-control-setup-activation")
	}
	if paths.PreviousDir == DefaultSetupPreviousDir && paths.Appliance != DefaultAppliancePath {
		paths.PreviousDir = filepath.Join(filepath.Dir(paths.Appliance), "previous-setup")
	}
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
	if config.SameFilesystem == nil {
		config.SameFilesystem = sameFilesystem
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
	} else if snapshot, snapshotErr := setupPathState(service.setupSnapshotRoot()); snapshotErr != nil {
		service.markMaintenance(ErrSetupRecoveryFailed)
	} else if snapshot != setupPathAbsent {
		service.markMaintenance(ErrSetupRecoveryFailed)
	} else if activation, activationErr := setupPathState(config.Paths.XkeenActivation); activationErr != nil {
		service.markMaintenance(ErrSetupRecoveryFailed)
	} else if activation != setupPathAbsent {
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
	if value.XkeenActivation != "" {
		target.XkeenActivation = value.XkeenActivation
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
	if value.LegacyOutbounds != "" {
		target.LegacyOutbounds = value.LegacyOutbounds
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
	if value.PreviousDir != "" {
		target.PreviousDir = value.PreviousDir
	}
	if value.CronPaths != nil {
		target.CronPaths = append([]string(nil), value.CronPaths...)
	}
	if value.WriterScripts != nil {
		target.WriterScripts = append([]string(nil), value.WriterScripts...)
	}
	if value.PanelPaths != nil {
		target.PanelPaths = append([]string(nil), value.PanelPaths...)
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
	if staging, err := componentStagingRootPresent(s.config.Paths.StagingDir); err != nil || staging {
		return staging, err
	}
	if snapshot, err := setupPathState(s.setupSnapshotRoot()); err != nil {
		return false, err
	} else if snapshot != setupPathAbsent {
		return true, nil
	}
	activation, err := setupPathState(s.config.Paths.XkeenActivation)
	return activation != setupPathAbsent, err
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
	if layout.state == "takeover" {
		if previewed {
			return SetupProjection{State: "previewable", Eligible: true, ReasonCode: layout.reason}
		}
		return SetupProjection{State: "takeover", Eligible: true, ReasonCode: layout.reason}
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
	if layout.state != "fresh" && layout.state != "takeover" {
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

type setupAuthorities struct {
	registry            nodes.Registry
	registryBytes       []byte
	app                 appliance.Appliance
	appBytes            []byte
	profilePresent      bool
	appPresent          bool
	policyPresent       bool
	legacyPresent       bool
	legacyPath          string
	profileAction       string
	policyAction        string
	profileSourceDigest string
	policySourceDigest  string
}

func digestSetupBytes(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func setupPresent(path string) (bool, error) {
	state, err := setupPathState(path)
	if err != nil {
		return false, err
	}
	if state == setupPathAbsent {
		return false, nil
	}
	if state != setupPathRegular {
		return false, errSetupLayoutInvalid
	}
	return true, nil
}

func (s *SetupService) readSetupAuthorities() (setupAuthorities, SetupReasonCode, error) {
	paths := s.config.Paths
	result := setupAuthorities{}

	if present, err := setupPresent(paths.Nodes); err != nil {
		return result, SetupReasonNodeAuthorityInvalid, nil
	} else if present {
		contents, readErr := readBoundedSetupFile(paths.Nodes, nodes.MaxRegistryDocument)
		if readErr != nil {
			return result, SetupReasonNodeAuthorityInvalid, nil
		}
		registry, parseErr := nodes.ParseCanonical(contents)
		if parseErr != nil {
			return result, SetupReasonNodeAuthorityInvalid, nil
		}
		result.registry, result.registryBytes, result.profilePresent, result.profileSourceDigest = registry, contents, true, digestSetupBytes(contents)
		result.profileAction = "preserve"
	} else {
		legacyCandidates := []string{paths.LegacyOutbounds, paths.ActiveOutbounds}
		for _, path := range legacyCandidates {
			if path == "" {
				continue
			}
			present, stateErr := setupPresent(path)
			if stateErr != nil {
				return result, SetupReasonProfileUnavailable, nil
			}
			if !present {
				continue
			}
			contents, readErr := readBoundedSetupFile(path, nodes.MaxLegacyDocument)
			if readErr != nil {
				return result, SetupReasonProfileUnavailable, nil
			}
			registry, migrateErr := nodes.MigrateLegacy(contents)
			if migrateErr != nil {
				return result, SetupReasonProfileUnavailable, nil
			}
			canonical, marshalErr := nodes.MarshalCanonical(registry)
			if marshalErr != nil {
				return result, SetupReasonProfileUnavailable, nil
			}
			result.registry, result.registryBytes, result.profilePresent, result.profileSourceDigest = registry, canonical, true, digestSetupBytes(contents)
			result.legacyPresent, result.legacyPath = true, path
			result.profileAction = "migrate"
			break
		}
	}

	if present, err := setupPresent(paths.Appliance); err != nil {
		return result, SetupReasonPolicyUnsupported, nil
	} else if present {
		contents, readErr := readBoundedSetupFile(paths.Appliance, appliance.MaxDocumentSize)
		if readErr != nil {
			return result, SetupReasonPolicyUnsupported, nil
		}
		value, parseErr := appliance.Parse(contents)
		if parseErr != nil {
			return result, SetupReasonPolicyUnsupported, nil
		}
		result.app, result.appBytes, result.appPresent, result.policyPresent, result.policySourceDigest = value, contents, true, true, digestSetupBytes(contents)
		result.policyAction = "preserve"
	} else {
		policyNames := []string{"02_dns.json", "05_routing.json", "07_observatory.json"}
		policyBytes := make(map[string][]byte, len(policyNames))
		policyCount := 0
		for _, name := range policyNames {
			path := filepath.Join(paths.XrayConfigDir, name)
			present, stateErr := setupPresent(path)
			if stateErr != nil {
				return result, SetupReasonPolicyUnsupported, nil
			}
			if !present {
				continue
			}
			policyCount++
			contents, readErr := readBoundedSetupFile(path, appliance.MaxDocumentSize)
			if readErr != nil {
				return result, SetupReasonPolicyUnsupported, nil
			}
			policyBytes[name] = contents
		}
		if policyCount != 0 {
			if policyCount != len(policyNames) {
				return result, SetupReasonPolicyUnsupported, nil
			}
			value, parseErr := appliance.ParseSupportedPolicyFiles(policyBytes["02_dns.json"], policyBytes["05_routing.json"], policyBytes["07_observatory.json"])
			if parseErr != nil {
				return result, SetupReasonPolicyUnsupported, nil
			}
			canonical, marshalErr := appliance.MarshalCanonical(value)
			if marshalErr != nil {
				return result, SetupReasonPolicyUnsupported, nil
			}
			result.app, result.appBytes, result.appPresent, result.policyPresent, result.policySourceDigest = value, canonical, false, true, digestSetupBytes(bytes.Join([][]byte{policyBytes["02_dns.json"], policyBytes["05_routing.json"], policyBytes["07_observatory.json"]}, []byte{0}))
			result.policyAction = "adopt-supported"
		}
	}
	return result, "", nil
}

func setupSafeProfileLabels(registry nodes.Registry) []string {
	ordered := registry.SortedNodes()
	labels := make([]string, 0, len(ordered))
	for index, node := range ordered {
		// Use the stored display name only. PublicNodes may derive a generic
		// label from the endpoint host, which is not needed in a Setup plan.
		label := strings.TrimSpace(node.Name)
		if label == "" {
			label = "Node " + strconv.Itoa(index+1)
		}
		if len(label) > 96 {
			label = label[:96]
		}
		labels = append(labels, label)
		if len(labels) == 32 {
			break
		}
	}
	return labels
}

func (a setupAuthorities) sourceManifest(class string, writers []setupWriter) setupSourceManifest {
	if a.profileAction == "" && a.profilePresent {
		a.profileAction = "preserve"
	}
	if a.policyAction == "" && a.appPresent {
		a.policyAction = "preserve"
	}
	registryDigest := a.profileSourceDigest
	if registryDigest == "" {
		registryDigest = digestSetupBytes(a.registryBytes)
	}
	policyDigest := a.policySourceDigest
	if policyDigest == "" {
		policyDigest = digestSetupBytes(a.appBytes)
	}
	writersDigest := setupWritersDigest(writers)
	digest := digestSetupBytes([]byte(strings.Join([]string{class, a.profileAction, a.policyAction, registryDigest, policyDigest, writersDigest}, "\x00")))
	return setupSourceManifest{Class: class, ProfileAction: a.profileAction, PolicyAction: a.policyAction, ProfileCount: len(a.registry.Nodes), ProfileLabels: setupSafeProfileLabels(a.registry), Digest: digest, RegistryDigest: registryDigest, PolicyDigest: policyDigest, WritersDigest: writersDigest, PanelPreserved: true}
}

func setupWritersDigest(writers []setupWriter) string {
	writerNames := make([]string, 0, len(writers))
	for _, writer := range writers {
		writerNames = append(writerNames, writer.Class+":"+filepath.Clean(writer.Path)+":"+writer.Digest)
	}
	sort.Strings(writerNames)
	return digestSetupBytes([]byte(strings.Join(writerNames, "\n")))
}

// setupSourceGenerationDigest binds the complete takeover-owned pre-state to a
// Preview. It intentionally records only bounded types, modes, sizes and
// hashes; secret-bearing contents never leave the local authority boundary.
// Panel-local paths are not part of setupSnapshotTargets and therefore remain
// outside both this binding and takeover replacement.
func (s *SetupService) setupSourceGenerationDigest(writers []setupWriter) (string, error) {
	entries := make([]setupSnapshotEntry, 0, setupMaxSnapshotEntries)
	totalBytes := int64(0)
	for _, target := range s.setupSnapshotTargets(writers) {
		maxFileBytes, maxRootBytes := setupSnapshotLimits(target)
		state, err := setupPathState(target.Path)
		if err != nil || state == setupPathInvalid {
			return "", errSetupLayoutInvalid
		}
		if state == setupPathAbsent {
			entries = append(entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Kind: "absent"})
			continue
		}
		if target.Recursive {
			if state != setupPathDirectory {
				return "", errSetupLayoutInvalid
			}
			entries = append(entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Kind: "directory", Mode: uint32(fileMode(target.Path))})
			rootBytes := int64(0)
			walkErr := filepath.WalkDir(target.Path, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if path == target.Path {
					return nil
				}
				if len(entries) >= setupMaxSnapshotEntries {
					return ErrSetupResourceInsufficient
				}
				info, infoErr := entry.Info()
				if infoErr != nil || info.Mode()&os.ModeSymlink != 0 {
					return errSetupLayoutInvalid
				}
				relative, relErr := filepath.Rel(target.Path, path)
				if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					return errSetupLayoutInvalid
				}
				if entry.IsDir() {
					entries = append(entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Relative: filepath.ToSlash(relative), Kind: "directory", Mode: uint32(info.Mode().Perm())})
					return nil
				}
				if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxFileBytes || rootBytes > maxRootBytes-info.Size() || totalBytes > setupMaxSnapshotBytes-info.Size() {
					return ErrSetupResourceInsufficient
				}
				contents, readErr := readBoundedSetupFile(path, maxFileBytes)
				if readErr != nil {
					return readErr
				}
				entries = append(entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Relative: filepath.ToSlash(relative), Kind: "file", Mode: uint32(info.Mode().Perm()), Size: int64(len(contents)), SHA256: digestSetupBytes(contents)})
				totalBytes += int64(len(contents))
				rootBytes += int64(len(contents))
				return nil
			})
			if walkErr != nil {
				return "", walkErr
			}
			continue
		}
		if state != setupPathRegular {
			return "", errSetupLayoutInvalid
		}
		contents, err := readBoundedSetupFile(target.Path, maxFileBytes)
		if err != nil || int64(len(contents)) > maxRootBytes || totalBytes > setupMaxSnapshotBytes-int64(len(contents)) {
			return "", errSetupLayoutInvalid
		}
		entries = append(entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Kind: "file", Mode: uint32(fileMode(target.Path)), Size: int64(len(contents)), SHA256: digestSetupBytes(contents)})
		totalBytes += int64(len(contents))
	}
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].Key + "\x00" + entries[i].Target + "\x00" + entries[i].Relative + "\x00" + entries[i].Kind
		right := entries[j].Key + "\x00" + entries[j].Target + "\x00" + entries[j].Relative + "\x00" + entries[j].Kind
		return left < right
	})
	contents, err := json.Marshal(entries)
	if err != nil {
		return "", errSetupLayoutInvalid
	}
	return digestSetupBytes(contents), nil
}

func normalizeSetupAuthorities(authorities setupAuthorities, class string) (setupAuthorities, error) {
	if !authorities.profilePresent {
		if class != "fresh" {
			return setupAuthorities{}, &SetupLayoutError{ReasonCode: SetupReasonProfileUnavailable}
		}
		registry := nodes.NewRegistry()
		contents, err := nodes.MarshalCanonical(registry)
		if err != nil {
			return setupAuthorities{}, ErrSetupCandidateRejected
		}
		authorities.registry, authorities.registryBytes, authorities.profileAction = registry, contents, "empty"
	}
	if !authorities.policyPresent {
		value := appliance.ProductDefault()
		contents, err := appliance.MarshalCanonical(value)
		if err != nil {
			return setupAuthorities{}, ErrSetupCandidateRejected
		}
		authorities.app, authorities.appBytes, authorities.policyAction = value, contents, "product-default"
	}
	return authorities, nil
}

func (s *SetupService) buildSetupSourceManifest(class string, authorities setupAuthorities, writers []setupWriter) (setupSourceManifest, error) {
	base := authorities.sourceManifest(class, writers)
	generationDigest, err := s.setupSourceGenerationDigest(writers)
	if err != nil {
		return setupSourceManifest{}, err
	}
	binding, err := json.Marshal(struct {
		Class            string `json:"class"`
		ProfileAction    string `json:"profileAction"`
		PolicyAction     string `json:"policyAction"`
		RegistryDigest   string `json:"registryDigest"`
		PolicyDigest     string `json:"policyDigest"`
		WritersDigest    string `json:"writersDigest"`
		GenerationDigest string `json:"generationDigest"`
		PanelPreserved   bool   `json:"panelPreserved"`
	}{
		Class: base.Class, ProfileAction: base.ProfileAction, PolicyAction: base.PolicyAction,
		RegistryDigest: base.RegistryDigest, PolicyDigest: base.PolicyDigest, WritersDigest: base.WritersDigest,
		GenerationDigest: generationDigest, PanelPreserved: base.PanelPreserved,
	})
	if err != nil {
		return setupSourceManifest{}, errSetupLayoutInvalid
	}
	base.Digest = digestSetupBytes(binding)
	return base, nil
}

func (s *SetupService) currentSetupSourceManifest(class string, writers []setupWriter) (setupSourceManifest, error) {
	authorities, reason, err := s.readSetupAuthorities()
	if err != nil {
		return setupSourceManifest{}, err
	}
	if reason != "" {
		return setupSourceManifest{}, &SetupLayoutError{ReasonCode: reason}
	}
	authorities, err = normalizeSetupAuthorities(authorities, class)
	if err != nil {
		return setupSourceManifest{}, err
	}
	return s.buildSetupSourceManifest(class, authorities, writers)
}

// verifySetupSource re-reads every takeover-owned source that participates in
// the Preview binding. The writer list is deliberately re-inspected rather
// than reused from an earlier admission point: a writer can disappear or
// reappear while a bounded previous-generation snapshot is being written.
func (s *SetupService) verifySetupSource(class string, expected setupSourceManifest) ([]setupWriter, error) {
	writers, err := s.inspectSetupWriters()
	if err != nil {
		return nil, ErrSetupWriterConflict
	}
	if setupWritersDigest(writers) != expected.WritersDigest {
		return nil, ErrSetupWriterConflict
	}
	current, err := s.currentSetupSourceManifest(class, writers)
	if err != nil || current.Digest != expected.Digest {
		return nil, ErrSetupPreviewStale
	}
	return writers, nil
}

func (s *SetupService) inspectSetupWriters() ([]setupWriter, error) {
	paths := s.config.Paths
	result := make([]setupWriter, 0, len(paths.WriterScripts)+len(paths.CronPaths)+len(paths.PanelPaths))
	seen := make(map[string]struct{})
	add := func(class, path, digest string) {
		key := class + "\x00" + path
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, setupWriter{Class: class, Path: path, Digest: digest})
	}
	for _, path := range paths.WriterScripts {
		present, err := setupPresent(path)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		base := strings.ToLower(filepath.Base(path))
		class := "known-writer"
		switch {
		case strings.Contains(base, "benchmark") || strings.Contains(base, "sbt"):
			class = "xkeen-speed-benchmark"
		case strings.Contains(base, "watchdog"):
			class = "xkeen-speed-watchdog"
		case strings.Contains(base, "geodata"):
			class = "xkeen-geofile-cron"
		}
		contents, readErr := readBoundedSetupFile(path, setupMaxCronBytes)
		if readErr != nil {
			return nil, readErr
		}
		add(class, path, digestSetupBytes(contents))
	}
	inspectCronFile := func(path string) error {
		contents, readErr := readBoundedSetupFile(path, setupMaxCronBytes)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
			class := ""
			switch {
			case setupXKeenCommand(line, "-ug", "-ugi", "-ugs", "-ux", "-uk"):
				class = "xkeen-automatic-writer"
			case setupXKeenCommand(line, "-sbt") || setupKnownWriterScript(line, "run-bounded-speed-benchmark.sh"):
				class = "xkeen-speed-benchmark"
			case setupKnownWriterScript(line, "speed_failover_watchdog.sh", "xkeen-control-watchdog"):
				class = "xkeen-speed-watchdog"
			case setupKnownWriterScript(line, "geofile", setupGeodataWriterScript):
				class = "xkeen-geofile-cron"
			}
			if class != "" {
				add(class, path, digestSetupBytes(contents))
			}
		}
		return nil
	}
	for _, path := range paths.CronPaths {
		state, err := setupPathState(path)
		if err != nil {
			return nil, err
		}
		switch state {
		case setupPathAbsent:
			continue
		case setupPathRegular:
			if err := inspectCronFile(path); err != nil {
				return nil, err
			}
		case setupPathDirectory:
			entries, readErr := os.ReadDir(path)
			if readErr != nil || len(entries) > 64 {
				return nil, errSetupLayoutInvalid
			}
			for _, entry := range entries {
				if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !validSetupFileName(entry.Name()) {
					return nil, errSetupLayoutInvalid
				}
				if err := inspectCronFile(filepath.Join(path, entry.Name())); err != nil {
					return nil, err
				}
			}
		default:
			return nil, errSetupLayoutInvalid
		}
	}
	for _, path := range paths.PanelPaths {
		present, err := setupPresent(path)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		contents, readErr := readBoundedSetupFile(path, setupMaxCronBytes)
		if readErr != nil {
			return nil, readErr
		}
		for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
			if setupXKeenCommand(line, "-i", "-ug", "-ugi", "-ugs", "-ux", "-uk", "-sbt") {
				add("panel-writer-conflict", path, digestSetupBytes(contents))
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Class+result[i].Path < result[j].Class+result[j].Path })
	return result, nil
}

// WriterConflict is the read-only admission hook shared with ordinary
// component mutations. A known automatic writer, or an inspection failure,
// blocks update/rollback until Setup has explicitly reconciled the state.
func (s *SetupService) WriterConflict() bool {
	if s == nil {
		return true
	}
	writers, err := s.inspectSetupWriters()
	return err != nil || len(writers) != 0
}

func setupXKeenCommand(line string, flags ...string) bool {
	wanted := make(map[string]struct{}, len(flags))
	for _, flag := range flags {
		wanted[flag] = struct{}{}
	}
	fields := setupWriterFields(line)
	for index, field := range fields {
		if strings.ToLower(filepath.Base(field)) != "xkeen" || index+1 >= len(fields) {
			continue
		}
		argument := fields[index+1]
		if _, ok := wanted[argument]; ok {
			return true
		}
	}
	return false
}

func setupKnownWriterScript(line string, names ...string) bool {
	for _, field := range setupWriterFields(line) {
		base := strings.ToLower(filepath.Base(field))
		for _, name := range names {
			if base == strings.ToLower(name) {
				return true
			}
		}
	}
	return false
}

func validSetupFileName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsRune(name, '\x00')
}

func setupWriterFields(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"';&|()<>`{}[],:", r)
	})
}

func setupPanelWriterConflict(writers []setupWriter) bool {
	for _, writer := range writers {
		if writer.Class == "panel-writer-conflict" {
			return true
		}
	}
	return false
}

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
	writers, err := s.inspectSetupWriters()
	if err != nil {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	authorities, reason, err := s.readSetupAuthorities()
	if err != nil {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	if reason != "" {
		return setupCandidate{}, &SetupLayoutError{ReasonCode: reason}
	}
	layout, layoutErr := s.inspectLayout()
	if layoutErr != nil {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	if layout.state != "fresh" && layout.state != "takeover" {
		return setupCandidate{}, &SetupLayoutError{ReasonCode: layout.reason}
	}
	class := layout.class
	if class == "" {
		class = "fresh"
	}
	authorities, err = normalizeSetupAuthorities(authorities, class)
	if err != nil {
		return setupCandidate{}, err
	}
	source, err := s.buildSetupSourceManifest(class, authorities, writers)
	if err != nil {
		return setupCandidate{}, ErrSetupCandidateRejected
	}
	return setupCandidate{Xray: xray, Geodata: geodata, XKeen: xkeen, Lifecycle: lifecycle, Registry: authorities.registry, NodesBytes: authorities.registryBytes, Appliance: authorities.app, AppBytes: authorities.appBytes, Source: source, Writers: writers}, nil
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
	if candidate.Source.Class == "" {
		candidate.Source.Class = "fresh"
	}
	writerPlans := make([]SetupWriterPlan, 0, len(candidate.Writers))
	seenWriters := make(map[string]struct{}, len(candidate.Writers))
	for _, writer := range candidate.Writers {
		if _, exists := seenWriters[writer.Class]; exists {
			continue
		}
		seenWriters[writer.Class] = struct{}{}
		writerPlans = append(writerPlans, SetupWriterPlan{Class: writer.Class})
	}
	sort.Slice(writerPlans, func(i, j int) bool { return writerPlans[i].Class < writerPlans[j].Class })
	return SetupPlan{
		SchemaVersion:  SetupTransactionSchemaVersion,
		SetupClass:     candidate.Source.Class,
		Destructive:    candidate.Source.Class != "managed-converged",
		ProductDefault: candidate.Source.PolicyAction == "product-default",
		EmptyRegistry:  candidate.Source.ProfileAction == "empty",
		Profiles:       SetupProfilePlan{Action: candidate.Source.ProfileAction, Count: candidate.Source.ProfileCount, Labels: append([]string(nil), candidate.Source.ProfileLabels...)},
		Policy:         SetupPolicyPlan{Action: candidate.Source.PolicyAction},
		Writers:        writerPlans,
		PanelPreserved: candidate.Source.PanelPreserved,
		Xray:           SetupXrayPlan{Tag: candidate.Xray.Tag, Version: candidate.Xray.Version, AssetName: candidate.Xray.AssetName, SizeBytes: candidate.Xray.SizeBytes, SHA256: candidate.Xray.SHA256},
		Geodata:        SetupGeodataPlan{Items: items, Generation: candidate.Geodata.Generation},
		XKeen:          SetupXKeenPlan{Repository: candidate.XKeen.Repository, Channel: candidate.XKeen.Channel, Tag: candidate.XKeen.Tag, Version: candidate.XKeen.Version, CommitSHA: candidate.XKeen.CommitSHA, SourceParentSHA: candidate.XKeen.SourceParentSHA, AssetName: candidate.XKeen.AssetName, BlobSHA: candidate.XKeen.BlobSHA, SizeBytes: candidate.XKeen.SizeBytes, SHA256: candidate.XKeen.SHA256, GenerationSHA256: xkeenIdentityGeneration(candidate.XKeen), GenerationBytes: generationBytes, ArchiveMemberCount: len(entry.ArchiveMembers), LifecycleClass: entry.LifecycleClass, CompatibilityClass: entry.CompatibilityClass},
		Lifecycle:      SetupLifecyclePlan{Name: "S05xkeen", Mode: 0o755, SHA256: hex.EncodeToString(lifecycleDigest[:])},
	}, nil
}

func sameSetupCandidate(left, right setupCandidate) bool {
	return sameXrayIdentity(left.Xray, right.Xray) && sameGeodataCandidateSet(left.Geodata, right.Geodata) && sameXKeenIdentity(left.XKeen, right.XKeen) && bytes.Equal(left.Lifecycle, right.Lifecycle) && left.Source.Digest == right.Source.Digest && bytes.Equal(left.NodesBytes, right.NodesBytes) && bytes.Equal(left.AppBytes, right.AppBytes)
}

func estimateSetupSnapshotBytes(paths SetupPaths) int64 {
	_ = paths
	// Snapshot payload bytes are independently capped at 256 MiB. Admission
	// must reserve that real bound plus the bounded manifest/entry overhead and
	// one temporary payload write. The temporary can be a full Xray binary;
	// the old 8 MiB placeholder (and a generation-file-sized allowance) could
	// pass /tmp while leaving persistent rollback storage unproven.
	maxFileBytes := int64(MaxXrayCandidateBinaryBytes)
	for _, limit := range []int64{MaxGeodataFileBytes, MaxXKeenGenerationFileBytes, nodes.MaxLegacyDocument, appliance.MaxDocumentSize, setupMaxCronBytes} {
		if limit > maxFileBytes {
			maxFileBytes = limit
		}
	}
	return int64(setupMaxSnapshotBytes) + int64(setupMaxCronBytes) + int64(setupMaxSnapshotEntries*256) + maxFileBytes
}

func setupSpaceProbePath(path string) string {
	path = filepath.Clean(path)
	for path != "." && path != string(filepath.Separator) {
		state, err := setupPathState(path)
		if err == nil && state != setupPathAbsent {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return path
}

type setupSpaceRequirement struct {
	path  string
	bytes uint64
}

type setupSpaceGroup struct {
	directory string
	bytes     uint64
}

func setupDoubleBytes(value uint64) uint64 {
	if value > ^uint64(0)/2 {
		return ^uint64(0)
	}
	return value * 2
}

// setupResourceDemand describes bytes that coexist on each destination while
// Setup is preparing and committing. Transient candidate bodies stay on the
// staging filesystem; target generations, journal/activation temporaries and
// the takeover snapshot stay on their actual persistent filesystems.
func setupResourceDemand(paths SetupPaths, class string, xrayArchive, geodataBytes, xkeenArchive, generationBytes uint64, configBytes, applianceBytes, nodesBytes, markerBytes, lifecycleBytes uint64) []setupSpaceRequirement {
	staging := xrayArchive + geodataBytes + xkeenArchive + generationBytes + configBytes + uint64(MaxXrayCandidateBinaryBytes)
	persistent := []setupSpaceRequirement{
		{path: filepath.Dir(paths.Journal), bytes: uint64(MaxComponentJournalBytes)},
		{path: filepath.Dir(paths.Appliance), bytes: setupDoubleBytes(applianceBytes)},
		{path: filepath.Dir(paths.Nodes), bytes: setupDoubleBytes(nodesBytes)},
		{path: paths.XrayConfigDir, bytes: setupDoubleBytes(configBytes)},
		{path: paths.XrayAssetDir, bytes: setupDoubleBytes(geodataBytes)},
		{path: filepath.Dir(paths.XrayBinary), bytes: uint64(MaxXrayCandidateBinaryBytes) * 2},
		{path: filepath.Dir(paths.XkeenBinary), bytes: setupDoubleBytes(generationBytes)},
		{path: filepath.Dir(paths.XkeenModuleDir), bytes: setupDoubleBytes(generationBytes)},
		{path: filepath.Dir(paths.XkeenMarker), bytes: setupDoubleBytes(markerBytes)},
		{path: filepath.Dir(paths.LifecycleInit), bytes: setupDoubleBytes(lifecycleBytes)},
		{path: filepath.Dir(paths.XkeenActivation), bytes: uint64(MaxXKeenGenerationBytes) + generationBytes},
	}
	for _, path := range paths.CronPaths {
		persistent = append(persistent, setupSpaceRequirement{path: filepath.Dir(path), bytes: uint64(setupMaxCronBytes)})
	}
	for _, path := range paths.WriterScripts {
		persistent = append(persistent, setupSpaceRequirement{path: filepath.Dir(path), bytes: uint64(setupMaxCronBytes)})
	}
	// Reserve the bounded previous-environment root on every Setup admission.
	// Fresh Setup does not populate it, but keeping the reserve in the same
	// typed demand model preserves the durable rollback budget if the layout is
	// concurrently classified as takeover before the first persistent write.
	persistent = append(persistent, setupSpaceRequirement{path: paths.PreviousDir, bytes: uint64(estimateSetupSnapshotBytes(paths))})
	if class != "fresh" {
		// During activation the new generation and the displaced old
		// generation can coexist in the activation directory in addition to
		// the bounded previous-environment snapshot.
		persistent = append(persistent, setupSpaceRequirement{path: filepath.Dir(paths.XkeenActivation), bytes: uint64(MaxXKeenGenerationBytes)})
	}
	return append([]setupSpaceRequirement{{path: paths.StagingDir, bytes: staging}}, persistent...)
}

func (s *SetupService) groupSetupResources(requirements []setupSpaceRequirement) ([]setupSpaceGroup, error) {
	groups := make([]setupSpaceGroup, 0, len(requirements))
	identity := s.config.SameFilesystem
	if identity == nil {
		identity = sameFilesystem
	}
	for _, requirement := range requirements {
		if requirement.path == "" {
			continue
		}
		probe := setupSpaceProbePath(requirement.path)
		if probe == "" {
			return nil, ErrSetupResourceInsufficient
		}
		groupIndex := -1
		for index, group := range groups {
			same, err := identity(group.directory, probe)
			if err != nil {
				return nil, ErrSetupResourceInsufficient
			}
			if same {
				groupIndex = index
				break
			}
		}
		if groupIndex < 0 {
			groups = append(groups, setupSpaceGroup{directory: probe, bytes: requirement.bytes})
			continue
		}
		if requirement.bytes > ^uint64(0)-groups[groupIndex].bytes {
			return nil, ErrSetupResourceInsufficient
		}
		groups[groupIndex].bytes += requirement.bytes
	}
	return groups, nil
}

func (s *SetupService) checkSetupResources(requirements []setupSpaceRequirement) error {
	groups, err := s.groupSetupResources(requirements)
	if err != nil {
		return ErrSetupResourceInsufficient
	}
	for _, group := range groups {
		if uint64(XrayFreeSpaceReserve) > ^uint64(0)-group.bytes {
			return ErrSetupResourceInsufficient
		}
		free, err := s.config.AvailableSpace(group.directory)
		if err != nil || free < group.bytes+uint64(XrayFreeSpaceReserve) {
			return ErrSetupResourceInsufficient
		}
	}
	return nil
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
	if activation, err := setupPathState(paths.XkeenActivation); err != nil {
		return setupLayout{}, err
	} else if activation != setupPathAbsent {
		return setupLayout{state: "blocked", reason: SetupReasonJournalPending}, nil
	}
	if snapshot, err := setupPathState(s.setupSnapshotRoot()); err != nil {
		return setupLayout{}, err
	} else if snapshot != setupPathAbsent {
		return setupLayout{state: "blocked", reason: SetupReasonJournalPending}, nil
	}
	if err := validateSetupFixedPaths(paths); err != nil {
		return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
	}
	geodataEntries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return setupLayout{}, err
	}
	managed := setupManagedPaths(paths, geodataEntries)
	for _, entry := range geodataEntries {
		_ = entry
	}
	present := 0
	for _, path := range managed {
		state, err := setupPathState(path)
		if err != nil {
			return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
		}
		if state != setupPathAbsent {
			present++
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
	if known, err := setupKnownLifecycleLayout(paths); err != nil {
		return setupLayout{}, err
	} else if !known {
		return setupLayout{state: "blocked", reason: SetupReasonLayoutMixed}, nil
	}
	writers, writerErr := s.inspectSetupWriters()
	if writerErr != nil {
		return setupLayout{state: "blocked", reason: SetupReasonWriterConflict}, nil
	}
	if setupPanelWriterConflict(writers) {
		return setupLayout{state: "blocked", reason: SetupReasonWriterConflict, writers: writers}, nil
	}
	authorities, reason, authorityErr := s.readSetupAuthorities()
	if authorityErr != nil {
		return setupLayout{}, authorityErr
	}
	if reason != "" {
		return setupLayout{state: "blocked", reason: reason}, nil
	}
	if s.setupConfigured(paths, managed) {
		if len(writers) == 0 {
			source := authorities.sourceManifest("managed-converged", writers)
			return setupLayout{state: "configured", reason: SetupReasonAlreadyConfigured, class: "managed-converged", source: source}, nil
		}
		// Recognized removable writers are an explicit Setup reconciliation
		// class. Ordinary component mutation remains blocked until this
		// takeover-owned operation retires them.
		class := "managed-takeover"
		source := authorities.sourceManifest(class, writers)
		return setupLayout{state: "takeover", reason: SetupReasonManagedTakeover, class: class, source: source, writers: writers}, nil
	}

	recognized, signalErr := setupRecognizedLegacySignal(paths, geodataEntries, present, writers, authorities)
	if signalErr != nil {
		return setupLayout{}, signalErr
	}
	if !recognized {
		if present == 0 {
			return setupLayout{state: "fresh", reason: SetupReasonFresh, class: "fresh"}, nil
		}
		return setupLayout{state: "blocked", reason: SetupReasonLayoutPartial}, nil
	}
	if !authorities.profilePresent {
		return setupLayout{state: "blocked", reason: SetupReasonProfileUnavailable}, nil
	}
	class := "managed-takeover"
	if authorities.legacyPresent && !setupPathExists(paths.Nodes) {
		class = "legacy-xkeen-takeover"
	}
	source := authorities.sourceManifest(class, writers)
	takeoverReason := SetupReasonManagedTakeover
	if class == "legacy-xkeen-takeover" {
		takeoverReason = SetupReasonLegacyTakeover
	}
	return setupLayout{state: "takeover", reason: takeoverReason, class: class, source: source, writers: writers}, nil
}

func setupPathExists(path string) bool {
	present, _ := setupPresent(path)
	return present
}

func setupManagedPaths(paths SetupPaths, entries []catalogEntry) []string {
	managed := make([]string, 0, 32)
	managed = append(managed, paths.XrayBinary, paths.XkeenBinary, paths.XkeenModuleDir, paths.XkeenConfig, paths.XkeenMarker, paths.LifecycleInit, paths.Appliance, paths.Nodes)
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		managed = append(managed, filepath.Join(paths.XrayConfigDir, name))
	}
	for _, entry := range entries {
		managed = append(managed, filepath.Join(paths.XrayAssetDir, entry.Name))
	}
	return managed
}

func setupRecognizedLegacySignal(paths SetupPaths, entries []catalogEntry, present int, writers []setupWriter, authorities setupAuthorities) (bool, error) {
	if len(writers) != 0 || authorities.profilePresent || authorities.appPresent || authorities.legacyPresent {
		return true, nil
	}
	for _, path := range []string{paths.LegacyLifecycleInit, paths.SiblingModule, paths.InstallHelper, paths.XkeenBinary, paths.XkeenModuleDir, paths.XkeenConfig, paths.XkeenMarker, paths.LifecycleInit} {
		if setupPathExists(path) {
			return true, nil
		}
	}
	for _, entry := range entries {
		if setupPathExists(filepath.Join(paths.XrayAssetDir, entry.Name)) {
			return true, nil
		}
	}
	for _, name := range []string{"02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		if setupPathExists(filepath.Join(paths.XrayConfigDir, name)) {
			return true, nil
		}
	}
	if present > 1 {
		return false, nil
	}
	return false, nil
}

func setupKnownLifecycleLayout(paths SetupPaths) (bool, error) {
	for _, path := range []string{paths.LifecycleInit, paths.LegacyLifecycleInit} {
		state, err := setupPathState(path)
		if err != nil {
			return false, err
		}
		if state == setupPathAbsent {
			continue
		}
		if state != setupPathRegular {
			return false, nil
		}
		if !IsReviewedSetupLifecycle(path) {
			return false, nil
		}
	}
	return true, nil
}

const (
	reviewedUpstreamS05SHA256 = "6e2998bd8c471637ed4d0128eebc2d10bf72dc15b1600208601d70f0a1d0ee13"
	reviewedUpstreamS24SHA256 = "4c6f3d8ddcc1e6fc37b8ff2cb577faf4382fcd7f2f8ef17c462c68e83dcdbcbf"
)

// IsReviewedSetupLifecycle is the closed identity predicate shared by Setup
// classification and the production CommandActivator. A pre-takeover init is
// admitted only as the exact source-owned template or a bounded, reviewed
// upstream generation. Names and incidental words such as "xray", "start" or
// "restart" are not evidence of a reviewed script.
func IsReviewedSetupLifecycle(path string) bool {
	return setupLifecycleIdentity(path) != ""
}

// IsReviewedLegacySetupLifecycle identifies a recognized upstream lifecycle
// that Setup may take over. CommandActivator uses this predicate to quiesce
// the managed Xray process directly; the foreign lifecycle is never executed
// as part of takeover.
func IsReviewedLegacySetupLifecycle(path string) bool {
	identity := setupLifecycleIdentity(path)
	return identity == "upstream-s05" || identity == "upstream-s24"
}

func setupLifecycleIdentity(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	contents, err := readBoundedSetupFile(path, 512<<10)
	if err != nil {
		return ""
	}
	expected, err := setupLifecycleBytes()
	if err == nil && bytes.Equal(contents, expected) {
		return "source-owned"
	}
	if filepath.Base(path) == "S05xkeen" && reviewedUpstreamS05(contents) {
		return "upstream-s05"
	}
	if filepath.Base(path) == "S24xray" && reviewedUpstreamS24(contents) {
		return "upstream-s24"
	}
	return ""
}

func reviewedUpstreamS05(contents []byte) bool {
	return reviewedLifecycleDigest(contents, reviewedUpstreamS05SHA256)
}

func reviewedUpstreamS24(contents []byte) bool {
	return reviewedLifecycleDigest(contents, reviewedUpstreamS24SHA256)
}

func reviewedLifecycleDigest(contents []byte, expected string) bool {
	for _, candidate := range [][]byte{contents, []byte(strings.ReplaceAll(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\r", "\n"))} {
		digest := sha256.Sum256(candidate)
		if hex.EncodeToString(digest[:]) == expected {
			return true
		}
	}
	return false
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
	if validXKeenOwner(allowedDir, setupStagingOwner) != nil {
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
	if forbidden, err := setupForbiddenPresent(paths); err != nil || forbidden {
		return false
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
	nodesBytes, err := readBoundedSetupFile(paths.Nodes, nodes.MaxRegistryDocument)
	if err != nil {
		return false
	}
	registry, err := nodes.ParseCanonical(nodesBytes)
	if err != nil {
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
	lifecycle, lifecycleErr := readBoundedSetupFile(paths.LifecycleInit, 16<<10)
	expectedLifecycle, expectedErr := setupLifecycleBytes()
	if lifecycleErr != nil || expectedErr != nil || !bytes.Equal(lifecycle, expectedLifecycle) {
		return false
	}
	return true
}

func validateSetupFixedPaths(paths SetupPaths) error {
	allPaths := []string{paths.XrayBinary, paths.XrayConfigDir, paths.XrayAssetDir, paths.XkeenBinary, paths.XkeenModuleDir, paths.XkeenConfig, paths.XkeenMarker, paths.XkeenActivation, paths.LifecycleInit, paths.LegacyLifecycleInit, paths.SiblingModule, paths.InstallHelper, paths.Appliance, paths.Nodes, paths.LegacyOutbounds, paths.ActiveOutbounds, paths.Journal, paths.RestoreJournal, paths.StagingDir, paths.PreviousDir}
	allPaths = append(allPaths, paths.CronPaths...)
	allPaths = append(allPaths, paths.WriterScripts...)
	allPaths = append(allPaths, paths.PanelPaths...)
	for _, path := range allPaths {
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
	if layout.state != "fresh" && layout.state != "takeover" || layout.class != prepared.candidate.Source.Class {
		return SetupResult{}, ErrSetupPreviewStale
	}
	currentWriters, writerErr := s.inspectSetupWriters()
	if writerErr != nil || setupWritersDigest(currentWriters) != prepared.candidate.Source.WritersDigest {
		return SetupResult{}, ErrSetupWriterConflict
	}
	currentSource, sourceErr := s.currentSetupSourceManifest(layout.class, currentWriters)
	if sourceErr != nil || currentSource.Digest != prepared.candidate.Source.Digest {
		return SetupResult{}, ErrSetupPreviewStale
	}
	if len(prepared.resourceDemand) != 0 {
		if err := s.checkSetupResources(prepared.resourceDemand); err != nil {
			return SetupResult{}, ErrSetupResourceInsufficient
		}
	}
	isFresh := prepared.candidate.Source.Class == "fresh"
	previous := setupPreviousRecord{AllAbsent: isFresh, Class: prepared.candidate.Source.Class}
	if !isFresh {
		previous.SnapshotDir = s.setupSnapshotRoot()
		if s.config.Selection != nil {
			selection, ok := s.config.Selection.(SetupSelectionTransaction)
			if !ok {
				return SetupResult{}, ErrSetupTransactionUnproven
			}
			previous.SelectionSnapshot, err = selection.SetupSelectionSnapshot(ownedContext)
			if err != nil || len(previous.SelectionSnapshot) > setupMaxSelectionBytes {
				return SetupResult{}, ErrSetupTransactionUnproven
			}
		}
	}
	journal := setupTransactionJournal{SchemaVersion: SetupTransactionSchemaVersion, Component: string(KindSetup), Operation: SetupOperation, Phase: setupPhasePrepared, Previous: previous, SourceClass: prepared.candidate.Source.Class, SourceDigest: prepared.candidate.Source.Digest, StageDir: prepared.stageDir, Candidate: prepared.record()}
	if !isFresh {
		// The journal intent is the first persistent byte of the previous
		// generation. A crash during any later snapshot write is therefore
		// visible to startup recovery and cannot strand a secret-bearing root.
		journal.Phase = setupPhaseSnapshotIntent
	}
	if err := s.writeJournal(journal); err != nil {
		return SetupResult{}, ErrSetupTransactionUnproven
	}
	journalWritten := true
	if !isFresh {
		snapshot, snapshotErr := s.captureSetupSnapshot(ownedContext, prepared.candidate.Source.Class, currentWriters)
		if snapshotErr != nil {
			_ = s.cleanupSetupSnapshotIntent()
			_ = s.clearJournal()
			return SetupResult{}, ErrSetupResourceInsufficient
		}
		prepared.snapshot = snapshot
		journal.Previous.SnapshotSHA = setupSnapshotDigest(snapshot.Manifest)
		if err := s.updateJournal(&journal, setupPhaseSnapshotReady, ""); err != nil {
			_ = s.removeSnapshot(snapshot)
			_ = s.clearJournal()
			return SetupResult{}, ErrSetupTransactionUnproven
		}
		var postSnapshotErr error
		currentWriters, postSnapshotErr = s.verifySetupSource(layout.class, prepared.candidate.Source)
		if postSnapshotErr != nil {
			if s.removeSnapshot(snapshot) != nil || s.clearJournal() != nil {
				s.markMaintenance(ErrSetupTransactionUnproven)
				return SetupResult{}, ErrSetupTransactionUnproven
			}
			return SetupResult{}, postSnapshotErr
		}
	}
	if prepared.candidate.Source.Class != "fresh" {
		if err := s.quiesceSetupRuntime(ownedContext); err != nil {
			return SetupResult{}, s.failApply(journalWritten, journal, prepared, false, ErrSetupRuntimeUnavailable)
		}
	}
	// Recompute after quiescing and immediately before the first replacement
	// write. This closes the gap between snapshot creation and commit while
	// preserving the ordinary ComponentMutationGate/Coordinator ownership.
	if _, sourceErr := s.verifySetupSource(layout.class, prepared.candidate.Source); sourceErr != nil {
		if prepared.candidate.Source.Class != "fresh" {
			recoveryContext, recoveryCancel := context.WithTimeout(context.Background(), s.config.RecoveryTimeout)
			restartErr := s.startAndProveRestoredRuntime(recoveryContext)
			recoveryCancel()
			if restartErr != nil || s.clearJournal() != nil || s.removeSnapshot(prepared.snapshot) != nil {
				s.markMaintenance(ErrSetupTransactionUnproven)
				return SetupResult{}, ErrSetupTransactionUnproven
			}
			return SetupResult{}, sourceErr
		}
		if err := s.clearJournal(); err != nil {
			s.markMaintenance(ErrSetupTransactionUnproven)
			return SetupResult{}, ErrSetupTransactionUnproven
		}
		return SetupResult{}, sourceErr
	}
	if err := s.commit(ownedContext, &journal, prepared); err != nil {
		return SetupResult{}, s.failApply(journalWritten, journal, prepared, false, err)
	}
	if s.config.Runtime == nil {
		return SetupResult{}, s.failApply(true, journal, prepared, false, ErrSetupRuntimeUnavailable)
	}
	runtimeStarted := true
	if err := s.config.Runtime.Start(ownedContext); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupRuntimeUnavailable)
	}
	if err := s.updateJournal(&journal, setupPhaseRuntimeStarted, ""); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupTransactionUnproven)
	}
	if err := s.runtimeProof(ownedContext, prepared.candidate.Registry); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupVerificationFailed)
	}
	if err := s.verifyInstalled(prepared); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupVerificationFailed)
	}
	if err := s.reconcileSetupSelection(ownedContext, prepared.candidate.Registry); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupVerificationFailed)
	}
	if err := s.updateJournal(&journal, setupPhaseSelectionReconciled, ""); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupTransactionUnproven)
	}
	journal.Phase = setupPhaseRuntimeVerified
	if err := s.writeJournal(journal); err != nil {
		return SetupResult{}, s.failApply(true, journal, prepared, runtimeStarted, ErrSetupTransactionUnproven)
	}
	if err := s.removeSnapshot(prepared.snapshot); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return SetupResult{}, ErrSetupTransactionUnproven
	}
	if err := s.clearJournal(); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return SetupResult{}, ErrSetupTransactionUnproven
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

const setupLifecycleTemplate = `#!/bin/sh
set -eu
action=${1-}
mode=${2-}
[ -z "$mode" ] || [ "$mode" = on ] || exit 2
[ "$#" -le 2 ] || exit 2
pidfile=/tmp/xkeen-control/xray.pid
logfile=/tmp/xkeen-control/xray.log
xray_binary=/opt/sbin/xray

read_pid() {
  [ -r "$pidfile" ] || return 1
  pid=$(cat "$pidfile" 2>/dev/null || true)
  case "$pid" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ -r "/proc/$pid/exe" ] || return 1
  executable=$(readlink "/proc/$pid/exe" 2>/dev/null || true)
  [ "$executable" = "$xray_binary" ] || [ "$executable" = "$xray_binary (deleted)" ] || return 1
  kill -0 "$pid" 2>/dev/null
}

stop_xray() {
  if read_pid; then
    pid=$(cat "$pidfile")
    kill "$pid" 2>/dev/null || true
    i=0
    while read_pid && [ "$i" -lt 10 ]; do
      i=$((i + 1))
      sleep 1
    done
    if read_pid; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
  rm -f "$pidfile"
}

start_xray() {
  if read_pid; then
    exit 0
  fi
  rm -f "$pidfile"
  mkdir -p "$(dirname "$pidfile")"
  export XRAY_LOCATION_ASSET=/opt/etc/xray/dat
  export XKEEN_FOREGROUND=1
  "$xray_binary" run -confdir /opt/etc/xray/configs </dev/null >>"$logfile" 2>&1 &
  xray_pid=$!
  printf '%s\n' "$xray_pid" >"$pidfile"
  kill -0 "$xray_pid" 2>/dev/null
}

case "$action" in
  start) start_xray ;;
  restart) stop_xray; start_xray ;;
  stop) stop_xray ;;
  status) if read_pid; then exit 0; else exit 1; fi ;;
  *) exit 2 ;;
esac
`

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
	resourceDemand  []setupSpaceRequirement
	snapshot        setupSnapshot
}

func (p preparedSetup) record() setupCandidateRecord {
	return setupCandidateRecord{
		Xray: p.candidate.Xray, XrayBinarySHA256: p.xrayMetadata.SHA256, XrayBinarySize: p.xrayMetadata.Size, XrayBinaryMode: p.xrayMetadata.Mode,
		Geodata: p.candidate.Geodata, XKeen: p.candidate.XKeen, XKeenGeneration: p.xkeenMetadata,
		LifecycleSHA256: setupLifecycleDigest(p.lifecycle),
	}
}

func (s *SetupService) prepare(ctx context.Context, candidate setupCandidate, plan SetupPlan) (preparedSetup, error) {
	if err := ctx.Err(); err != nil {
		return preparedSetup{}, err
	}
	value := candidate.Appliance
	registry := candidate.Registry
	if err := registry.Validate(); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	appBytes := append([]byte(nil), candidate.AppBytes...)
	if len(appBytes) == 0 {
		var err error
		appBytes, err = appliance.MarshalCanonical(value)
		if err != nil {
			return preparedSetup{}, ErrSetupCandidateRejected
		}
	}
	if _, err := appliance.Parse(appBytes); err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	nodesBytes := append([]byte(nil), candidate.NodesBytes...)
	if len(nodesBytes) == 0 {
		var err error
		nodesBytes, err = nodes.MarshalCanonical(registry)
		if err != nil {
			return preparedSetup{}, ErrSetupCandidateRejected
		}
	}
	if _, err := nodes.ParseCanonical(nodesBytes); err != nil {
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
	var configBytes uint64
	for _, contents := range files {
		configBytes += uint64(len(contents))
	}
	demand := setupResourceDemand(s.config.Paths, candidate.Source.Class, uint64(candidate.Xray.SizeBytes), uint64(geodataBytes), uint64(candidate.XKeen.SizeBytes), uint64(generationBytes), configBytes, uint64(len(appBytes)), uint64(len(nodesBytes)), uint64(MaxXKeenMarkerBytes), uint64(len(lifecycle)))
	if err := s.checkSetupResources(demand); err != nil {
		return preparedSetup{}, ErrSetupResourceInsufficient
	}
	if err := ensurePrivateDirectory(s.config.Paths.StagingDir); err != nil {
		return preparedSetup{}, ErrSetupResourceInsufficient
	}
	stageDir, err := os.MkdirTemp(s.config.Paths.StagingDir, ".setup-")
	if err != nil {
		return preparedSetup{}, ErrSetupCandidateRejected
	}
	if err := ensureXKeenOwnedDirectory(stageDir, setupStagingOwner); err != nil {
		_ = os.RemoveAll(stageDir)
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
	return preparedSetup{candidate: candidate, plan: plan, stageDir: stageDir, configFiles: files, appBytes: appBytes, nodesBytes: nodesBytes, xrayBinaryPath: xrayBinary, xrayMetadata: xrayMetadata, geodataMetadata: geodataMetadata, xkeenPath: xkeenRoot, xkeenMetadata: xkeenMetadata, marker: marker, lifecycle: lifecycle, resourceDemand: demand}, nil
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
	setupCreatedWriters   = "writers"
)

type setupPreviousRecord struct {
	AllAbsent         bool   `json:"allAbsent"`
	Class             string `json:"class,omitempty"`
	SnapshotDir       string `json:"snapshotDir,omitempty"`
	SnapshotSHA       string `json:"snapshotSha256,omitempty"`
	SelectionSnapshot []byte `json:"selectionSnapshot,omitempty"`
}

type setupSnapshotEntry struct {
	Key      string `json:"key"`
	Target   string `json:"target"`
	Relative string `json:"relative,omitempty"`
	Payload  string `json:"payload,omitempty"`
	Kind     string `json:"kind"`
	Mode     uint32 `json:"mode,omitempty"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

type setupSnapshotManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Owner         string               `json:"owner"`
	Class         string               `json:"class"`
	Bytes         int64                `json:"bytes"`
	Entries       []setupSnapshotEntry `json:"entries"`
}

type setupSnapshot struct {
	Dir      string
	Manifest setupSnapshotManifest
}

type setupCandidateRecord struct {
	Xray             XrayReleaseIdentity     `json:"xray"`
	XrayBinarySHA256 string                  `json:"xrayBinarySha256"`
	XrayBinarySize   int64                   `json:"xrayBinarySize"`
	XrayBinaryMode   uint32                  `json:"xrayBinaryMode"`
	Geodata          GeodataCandidateSet     `json:"geodata"`
	XKeen            XKeenReleaseIdentity    `json:"xkeen"`
	XKeenGeneration  xkeenGenerationMetadata `json:"xkeenGeneration"`
	LifecycleSHA256  string                  `json:"lifecycleSha256"`
}

type setupTransactionJournal struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Component     string               `json:"component"`
	Operation     string               `json:"operation"`
	Phase         string               `json:"phase"`
	Previous      setupPreviousRecord  `json:"previous"`
	Candidate     setupCandidateRecord `json:"candidate"`
	SourceClass   string               `json:"sourceClass"`
	SourceDigest  string               `json:"sourceDigest"`
	StageDir      string               `json:"stageDir"`
	Created       []string             `json:"created,omitempty"`
}

func (p preparedSetup) candidateRecordMatches(record setupCandidateRecord) bool {
	return sameXrayIdentity(p.candidate.Xray, record.Xray) && sameGeodataCandidateSet(p.candidate.Geodata, record.Geodata) && sameXKeenIdentity(p.candidate.XKeen, record.XKeen) && sameXKeenGeneration(p.xkeenMetadata, record.XKeenGeneration) && record.LifecycleSHA256 == setupLifecycleDigest(p.lifecycle)
}

func validateSetupJournal(journal setupTransactionJournal) error {
	if journal.SchemaVersion != SetupTransactionSchemaVersion || journal.Component != string(KindSetup) || journal.Operation != SetupOperation || !validXrayIdentity(journal.Candidate.Xray) || !validXrayBinaryMetadata(xrayBinaryMetadata{Exists: true, Version: journal.Candidate.Xray.Version, SHA256: journal.Candidate.XrayBinarySHA256, Size: journal.Candidate.XrayBinarySize, Mode: journal.Candidate.XrayBinaryMode}, true) || validateGeodataCandidateSet(journal.Candidate.Geodata) != nil || !validXKeenIdentity(journal.Candidate.XKeen) || !validXKeenGenerationMetadata(journal.Candidate.XKeenGeneration) || !strings.EqualFold(journal.Candidate.XKeenGeneration.Generation, xkeenIdentityGeneration(journal.Candidate.XKeen)) || !isHexSHA256(journal.Candidate.LifecycleSHA256) || len(journal.Created) > setupMaxCreated {
		return errSetupJournalInvalid
	}
	switch journal.SourceClass {
	case "fresh", "managed-takeover", "legacy-xkeen-takeover":
	default:
		return errSetupJournalInvalid
	}
	if journal.SourceClass == "" || !isHexSHA256(journal.SourceDigest) || journal.StageDir == "" || filepath.Base(filepath.Clean(journal.StageDir)) == "." || journal.SourceClass == "fresh" && !journal.Previous.AllAbsent || journal.SourceClass != "fresh" && (journal.Previous.AllAbsent || journal.Previous.SnapshotDir == "") {
		return errSetupJournalInvalid
	}
	switch journal.Phase {
	case setupPhasePrepared, setupPhaseSnapshotIntent, setupPhaseSnapshotReady, setupPhaseNodesCommitted, setupPhaseAuthorityCommitted, setupPhaseConfigCommitted, setupPhaseGeodataCommitted, setupPhaseXrayCommitted, setupPhaseXKeenStaged, setupPhaseXKeenBinaryCommitted, setupPhaseXKeenModuleCommitted, setupPhaseXKeenCommitted, setupPhaseLifecycleCommitted, setupPhaseWritersRetired, setupPhaseRuntimeStarted, setupPhaseSelectionReconciled, setupPhaseRuntimeVerified:
	default:
		return errSetupJournalInvalid
	}
	if journal.SourceClass == "fresh" {
		if journal.Previous.SnapshotDir != "" || journal.Previous.SnapshotSHA != "" || len(journal.Previous.SelectionSnapshot) != 0 {
			return errSetupJournalInvalid
		}
	} else if journal.Phase != setupPhaseSnapshotIntent && !isHexSHA256(journal.Previous.SnapshotSHA) {
		return errSetupJournalInvalid
	}
	if len(journal.Previous.SelectionSnapshot) > setupMaxSelectionBytes {
		return errSetupJournalInvalid
	}
	allowed := map[string]struct{}{setupCreatedNodes: {}, setupCreatedAppliance: {}, setupCreatedConfig: {}, setupCreatedGeodata: {}, setupCreatedXray: {}, setupCreatedXKeen: {}, setupCreatedLifecycle: {}, setupCreatedWriters: {}}
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
	writers, err := s.inspectSetupWriters()
	if err != nil || setupWritersDigest(writers) != prepared.candidate.Source.WritersDigest {
		return ErrSetupWriterConflict
	}
	paths := s.config.Paths
	replace := prepared.candidate.Source.Class != "fresh"
	write := func(path string, contents []byte, mode os.FileMode) error {
		if replace {
			return s.writeSetupReplace(path, contents, mode)
		}
		return s.writeSetupExclusive(path, contents, mode)
	}
	if err := write(paths.Nodes, prepared.nodesBytes, 0o600); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseNodesCommitted, setupCreatedNodes); err != nil {
		return err
	}
	if err := write(paths.Appliance, prepared.appBytes, 0o600); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseAuthorityCommitted, setupCreatedAppliance); err != nil {
		return err
	}
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		contents, ok := prepared.configFiles["xray/"+name]
		if !ok || write(filepath.Join(paths.XrayConfigDir, name), contents, 0o600) != nil {
			return ErrSetupCandidateRejected
		}
	}
	xkeenConfig, ok := prepared.configFiles["xkeen/xkeen.json"]
	if !ok {
		return ErrSetupCandidateRejected
	}
	if err := write(paths.XkeenConfig, xkeenConfig, 0o600); err != nil {
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
		if readErr != nil || write(filepath.Join(paths.XrayAssetDir, entry.Name), contents, 0o600) != nil {
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
	if err := write(paths.XrayBinary, xrayContents, 0o700); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseXrayCommitted, setupCreatedXray); err != nil {
		return err
	}
	if err := s.activateSetupXKeenGeneration(journal, filepath.Join(prepared.xkeenPath, "xkeen"), filepath.Join(prepared.xkeenPath, ".xkeen"), paths.XkeenBinary, paths.XkeenModuleDir, prepared.xkeenMetadata); err != nil {
		return err
	}
	if err := write(paths.XkeenMarker, prepared.marker, 0o600); err != nil {
		return err
	}
	if err := s.cleanupSetupActivation(); err != nil {
		return err
	}
	if journal.Phase != setupPhaseXKeenCommitted {
		if err := s.updateJournal(journal, setupPhaseXKeenCommitted, setupCreatedXKeen); err != nil {
			return err
		}
	} else if !containsSetupCreated(journal.Created, setupCreatedXKeen) {
		journal.Created = append(journal.Created, setupCreatedXKeen)
		if err := s.writeJournal(*journal); err != nil {
			return err
		}
	}
	if err := write(paths.LifecycleInit, prepared.lifecycle, 0o755); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseLifecycleCommitted, setupCreatedLifecycle); err != nil {
		return err
	}
	if err := s.retireLegacyArtifacts(); err != nil {
		return err
	}
	if err := s.retireSetupWriters(journal, prepared.candidate.Writers); err != nil {
		return err
	}
	return nil
}

func (s *SetupService) retireLegacyArtifacts() error {
	for _, path := range []string{s.config.Paths.LegacyLifecycleInit, s.config.Paths.SiblingModule, s.config.Paths.InstallHelper} {
		state, err := setupPathState(path)
		if err != nil {
			return err
		}
		if state == setupPathAbsent {
			continue
		}
		if state != setupPathRegular && state != setupPathDirectory {
			return errSetupLayoutInvalid
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if err := s.config.SyncDirectory(filepath.Dir(path)); err != nil {
			return err
		}
	}
	return nil
}

func containsSetupCreated(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (s *SetupService) activateSetupXKeenGeneration(journal *setupTransactionJournal, sourceBinary, sourceModule, destinationBinary, destinationModule string, expected xkeenGenerationMetadata) error {
	if journal == nil || !validXKeenGenerationMetadata(expected) || sourceBinary == "" || sourceModule == "" || destinationBinary == "" || destinationModule == "" {
		return errXKeenGenerationInvalid
	}
	parent := filepath.Dir(destinationBinary)
	if filepath.Dir(destinationModule) != parent || filepath.Dir(s.config.Paths.XkeenActivation) != parent || filepath.Clean(s.config.Paths.XkeenActivation) == parent {
		return errXKeenGenerationInvalid
	}
	if err := ensureSetupDirectory(parent); err != nil {
		return err
	}
	if err := ensureXKeenOwnedDirectory(s.config.Paths.XkeenActivation, xkeenActivationOwnerValue); err != nil {
		return err
	}
	activation := s.config.Paths.XkeenActivation
	newGeneration := filepath.Join(activation, "new")
	oldBinary := filepath.Join(activation, "old-xkeen")
	oldModule := filepath.Join(activation, "old-module")
	for _, path := range []string{newGeneration, oldBinary, oldModule} {
		if state, err := setupPathState(path); err != nil {
			return err
		} else if state != setupPathAbsent {
			if state == setupPathDirectory {
				if err := validXkeenActivationPayload(path); err != nil {
					return err
				}
			} else if state != setupPathRegular {
				return errXKeenGenerationInvalid
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	if err := ensureXKeenOwnedDirectory(newGeneration, xkeenActivationOwnerValue); err != nil {
		return err
	}
	if err := copyXKeenGeneration(sourceBinary, sourceModule, newGeneration, expected); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseXKeenStaged, ""); err != nil {
		return err
	}
	if state, err := setupPathState(destinationBinary); err != nil {
		return err
	} else if state != setupPathAbsent {
		same, sameErr := sameFilesystem(filepath.Dir(destinationBinary), activation)
		if state != setupPathRegular || sameErr != nil || !same {
			return errXKeenGenerationInvalid
		}
		if err := os.Rename(destinationBinary, oldBinary); err != nil {
			return err
		}
	}
	if err := os.Rename(filepath.Join(newGeneration, "xkeen"), destinationBinary); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseXKeenBinaryCommitted, ""); err != nil {
		return err
	}
	if state, err := setupPathState(destinationModule); err != nil {
		return err
	} else if state != setupPathAbsent {
		same, sameErr := sameFilesystem(filepath.Dir(destinationModule), activation)
		if state != setupPathDirectory || sameErr != nil || !same {
			return errXKeenGenerationInvalid
		}
		if err := os.Rename(destinationModule, oldModule); err != nil {
			return err
		}
	}
	if err := os.Rename(filepath.Join(newGeneration, ".xkeen"), destinationModule); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		return err
	}
	if err := s.updateJournal(journal, setupPhaseXKeenModuleCommitted, ""); err != nil {
		return err
	}
	return nil
}

func (s *SetupService) cleanupSetupActivation() error {
	path := s.config.Paths.XkeenActivation
	state, err := setupPathState(path)
	if err != nil || state == setupPathAbsent {
		return err
	}
	if state != setupPathDirectory || validXKeenOwner(path, xkeenActivationOwnerValue) != nil {
		return errXKeenGenerationInvalid
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(path))
}

func setupWriterLine(line string) bool {
	return setupXKeenCommand(line, "-ug", "-ugi", "-ugs", "-ux", "-uk", "-sbt") || setupKnownWriterScript(line, "run-bounded-speed-benchmark.sh", "speed_failover_watchdog.sh", "xkeen-control-watchdog", "geofile", setupGeodataWriterScript)
}

func (s *SetupService) retireSetupWriters(journal *setupTransactionJournal, writers []setupWriter) error {
	if len(writers) == 0 {
		return nil
	}
	paths := s.config.Paths
	cronPaths := make(map[string]struct{}, len(s.config.Paths.CronPaths))
	for _, path := range s.config.Paths.CronPaths {
		cronPaths[filepath.Clean(path)] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, writer := range writers {
		isPanelPath := false
		for _, panelPath := range paths.PanelPaths {
			if filepath.Clean(panelPath) == filepath.Clean(writer.Path) {
				isPanelPath = true
				break
			}
		}
		if isPanelPath {
			continue
		}
		if _, ok := seen[filepath.Clean(writer.Path)]; ok {
			continue
		}
		seen[filepath.Clean(writer.Path)] = struct{}{}
		contents, err := readBoundedSetupFile(writer.Path, setupMaxCronBytes)
		if err != nil || digestSetupBytes(contents) != writer.Digest {
			return ErrSetupWriterConflict
		}
		if _, isCron := cronPaths[filepath.Clean(writer.Path)]; isCron || setupCronPath(paths, writer.Path) {
			lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
			filtered := make([]string, 0, len(lines))
			for _, line := range lines {
				if !setupWriterLine(line) {
					filtered = append(filtered, line)
				}
			}
			updated := []byte(strings.Join(filtered, "\n"))
			if err := s.writeSetupReplace(writer.Path, updated, 0o600); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(writer.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := s.config.SyncDirectory(filepath.Dir(writer.Path)); err != nil {
			return err
		}
	}
	return s.updateJournal(journal, setupPhaseWritersRetired, setupCreatedWriters)
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
	return s.writeSetupFile(path, contents, mode, false)
}

func (s *SetupService) writeSetupReplace(path string, contents []byte, mode os.FileMode) error {
	return s.writeSetupFile(path, contents, mode, true)
}

func (s *SetupService) writeSetupFile(path string, contents []byte, mode os.FileMode, replace bool) error {
	if path == "" || !replace && len(contents) == 0 {
		return errSetupLayoutInvalid
	}
	if !replace {
		if err := requireSetupPathAbsent(path); err != nil {
			return err
		}
	} else if state, err := setupPathState(path); err != nil || state == setupPathInvalid || state == setupPathDirectory {
		if err != nil {
			return err
		}
		return errSetupLayoutInvalid
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
		if !replace {
			return err
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return err
		}
		if retryErr := os.Rename(temporaryPath, path); retryErr != nil {
			return retryErr
		}
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

const setupSnapshotOwner = "xkeen-control/setup-previous-v1\n"
const setupStagingOwner = "xkeen-control/setup-staging-v1\n"

type setupSnapshotTarget struct {
	Key       string
	Path      string
	Recursive bool
}

func setupSnapshotLimits(target setupSnapshotTarget) (int64, int64) {
	switch target.Key {
	case "xray-binary":
		return MaxXrayCandidateBinaryBytes, MaxXrayCandidateBinaryBytes
	case "xray-assets":
		return MaxGeodataFileBytes, MaxGeodataCandidateBytes
	case "xray-config":
		return nodes.MaxLegacyDocument, int64(nodes.MaxLegacyDocument) + 7*int64(appliance.MaxDocumentSize)
	case "xkeen-module":
		return MaxXKeenGenerationFileBytes, MaxXKeenGenerationBytes
	case "legacy-module":
		return MaxGeodataFileBytes, MaxGeodataCandidateBytes
	case "nodes":
		return nodes.MaxRegistryDocument, nodes.MaxRegistryDocument
	case "legacy-outbounds", "active-outbounds":
		return nodes.MaxLegacyDocument, nodes.MaxLegacyDocument
	case "appliance", "xkeen-config":
		return appliance.MaxDocumentSize, appliance.MaxDocumentSize
	case "lifecycle", "legacy-lifecycle", "install-helper":
		return setupMaxCronBytes, setupMaxCronBytes
	default:
		return MaxXKeenGenerationFileBytes, MaxXKeenGenerationBytes
	}
}

func (s *SetupService) setupSnapshotTargets(writers []setupWriter) []setupSnapshotTarget {
	paths := s.config.Paths
	result := []setupSnapshotTarget{
		{Key: "xray-binary", Path: paths.XrayBinary},
		{Key: "xray-config", Path: paths.XrayConfigDir, Recursive: true},
		{Key: "xray-assets", Path: paths.XrayAssetDir, Recursive: true},
		{Key: "xkeen-binary", Path: paths.XkeenBinary},
		{Key: "xkeen-module", Path: paths.XkeenModuleDir, Recursive: true},
		{Key: "xkeen-config", Path: paths.XkeenConfig},
		{Key: "xkeen-marker", Path: paths.XkeenMarker},
		{Key: "lifecycle", Path: paths.LifecycleInit},
		{Key: "legacy-lifecycle", Path: paths.LegacyLifecycleInit},
		{Key: "legacy-module", Path: paths.SiblingModule, Recursive: true},
		{Key: "install-helper", Path: paths.InstallHelper},
		{Key: "appliance", Path: paths.Appliance},
		{Key: "nodes", Path: paths.Nodes},
		{Key: "legacy-outbounds", Path: paths.LegacyOutbounds},
	}
	if filepath.Clean(paths.ActiveOutbounds) != filepath.Clean(filepath.Join(paths.XrayConfigDir, "04_outbounds.json")) {
		result = append(result, setupSnapshotTarget{Key: "active-outbounds", Path: paths.ActiveOutbounds})
	}
	seen := make(map[string]struct{}, len(result)+len(writers))
	for _, target := range result {
		seen[filepath.Clean(target.Path)] = struct{}{}
	}
	for _, writer := range writers {
		panelPath := false
		for _, candidate := range paths.PanelPaths {
			if filepath.Clean(candidate) == filepath.Clean(writer.Path) {
				panelPath = true
				break
			}
		}
		if panelPath {
			continue
		}
		if _, ok := seen[filepath.Clean(writer.Path)]; ok {
			continue
		}
		seen[filepath.Clean(writer.Path)] = struct{}{}
		result = append(result, setupSnapshotTarget{Key: "writer-" + strconv.Itoa(len(result)), Path: writer.Path})
	}
	return result
}

func (s *SetupService) setupSnapshotRoot() string {
	return filepath.Join(s.config.Paths.PreviousDir, ".setup-snapshot")
}

func setupSnapshotDigest(manifest setupSnapshotManifest) string {
	contents, _ := json.Marshal(manifest)
	return digestSetupBytes(contents)
}

func (s *SetupService) captureSetupSnapshot(ctx context.Context, class string, writers []setupWriter) (setupSnapshot, error) {
	if class == "fresh" {
		return setupSnapshot{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	root := s.setupSnapshotRoot()
	if state, err := setupPathState(root); err != nil {
		return setupSnapshot{}, err
	} else if state != setupPathAbsent {
		return setupSnapshot{}, ErrSetupResourceInsufficient
	}
	if err := ensureSetupDirectory(filepath.Dir(root)); err != nil {
		return setupSnapshot{}, err
	}
	if err := ensureXKeenOwnedDirectory(root, setupSnapshotOwner); err != nil {
		return setupSnapshot{}, err
	}
	manifest := setupSnapshotManifest{SchemaVersion: SetupTransactionSchemaVersion, Owner: setupSnapshotOwner, Class: class}
	totalBytes := int64(0)
	for _, target := range s.setupSnapshotTargets(writers) {
		maxFileBytes, maxRootBytes := setupSnapshotLimits(target)
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(root)
			return setupSnapshot{}, err
		}
		state, err := setupPathState(target.Path)
		if err != nil || state == setupPathInvalid {
			_ = os.RemoveAll(root)
			return setupSnapshot{}, errSetupLayoutInvalid
		}
		if state == setupPathAbsent {
			manifest.Entries = append(manifest.Entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Kind: "absent"})
			continue
		}
		if target.Recursive {
			if state != setupPathDirectory {
				_ = os.RemoveAll(root)
				return setupSnapshot{}, errSetupLayoutInvalid
			}
			manifest.Entries = append(manifest.Entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Kind: "directory", Mode: uint32(fileMode(target.Path))})
			rootBytes := int64(0)
			walkErr := filepath.WalkDir(target.Path, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if path == target.Path {
					return nil
				}
				if len(manifest.Entries) >= setupMaxSnapshotEntries {
					return ErrSetupResourceInsufficient
				}
				info, infoErr := entry.Info()
				if infoErr != nil || info.Mode()&os.ModeSymlink != 0 {
					return errSetupLayoutInvalid
				}
				relative, relErr := filepath.Rel(target.Path, path)
				if relErr != nil || relative == "." || strings.HasPrefix(relative, "..") {
					return errSetupLayoutInvalid
				}
				if entry.IsDir() {
					if len(manifest.Entries) >= setupMaxSnapshotEntries {
						return ErrSetupResourceInsufficient
					}
					manifest.Entries = append(manifest.Entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Relative: filepath.ToSlash(relative), Kind: "directory", Mode: uint32(info.Mode().Perm())})
					return nil
				}
				if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxFileBytes || rootBytes > maxRootBytes-info.Size() || totalBytes > setupMaxSnapshotBytes-info.Size() {
					return ErrSetupResourceInsufficient
				}
				contents, readErr := readBoundedSetupFile(path, maxFileBytes)
				if readErr != nil {
					return readErr
				}
				payload := filepath.Join(root, "payload", strconv.Itoa(len(manifest.Entries)))
				if err := writeSnapshotPayload(payload, contents, info.Mode().Perm()); err != nil {
					return err
				}
				digest := digestSetupBytes(contents)
				manifest.Entries = append(manifest.Entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Relative: filepath.ToSlash(relative), Payload: filepath.Base(payload), Kind: "file", Mode: uint32(info.Mode().Perm()), Size: int64(len(contents)), SHA256: digest})
				totalBytes += int64(len(contents))
				rootBytes += int64(len(contents))
				return nil
			})
			if walkErr != nil {
				_ = os.RemoveAll(root)
				return setupSnapshot{}, walkErr
			}
			continue
		}
		if state != setupPathRegular {
			_ = os.RemoveAll(root)
			return setupSnapshot{}, errSetupLayoutInvalid
		}
		contents, readErr := readBoundedSetupFile(target.Path, maxFileBytes)
		if readErr != nil || int64(len(contents)) > maxRootBytes || totalBytes > setupMaxSnapshotBytes-int64(len(contents)) {
			_ = os.RemoveAll(root)
			return setupSnapshot{}, ErrSetupResourceInsufficient
		}
		payload := filepath.Join(root, "payload", strconv.Itoa(len(manifest.Entries)))
		info, infoErr := os.Lstat(target.Path)
		if infoErr != nil || info.Mode()&os.ModeSymlink != 0 {
			_ = os.RemoveAll(root)
			return setupSnapshot{}, errSetupLayoutInvalid
		}
		if err := writeSnapshotPayload(payload, contents, info.Mode().Perm()); err != nil {
			_ = os.RemoveAll(root)
			return setupSnapshot{}, err
		}
		manifest.Entries = append(manifest.Entries, setupSnapshotEntry{Key: target.Key, Target: target.Path, Payload: filepath.Base(payload), Kind: "file", Mode: uint32(info.Mode().Perm()), Size: int64(len(contents)), SHA256: digestSetupBytes(contents)})
		totalBytes += int64(len(contents))
	}
	manifest.Bytes = totalBytes
	contents, err := json.Marshal(manifest)
	if err != nil || len(contents) > setupMaxCronBytes {
		_ = os.RemoveAll(root)
		return setupSnapshot{}, ErrSetupResourceInsufficient
	}
	if err := writeSnapshotPayload(filepath.Join(root, "manifest.json"), append(contents, '\n'), 0o600); err != nil {
		_ = os.RemoveAll(root)
		return setupSnapshot{}, err
	}
	if err := s.config.SyncDirectory(root); err != nil {
		_ = os.RemoveAll(root)
		return setupSnapshot{}, err
	}
	if err := s.config.SyncDirectory(filepath.Dir(root)); err != nil {
		_ = os.RemoveAll(root)
		return setupSnapshot{}, err
	}
	return setupSnapshot{Dir: root, Manifest: manifest}, nil
}

func fileMode(path string) os.FileMode {
	info, err := os.Lstat(path)
	if err != nil {
		return 0o700
	}
	return info.Mode().Perm()
}

func writeSnapshotPayload(path string, contents []byte, mode os.FileMode) error {
	if err := ensureSetupDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".snapshot-")
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
	return os.Rename(temporaryPath, path)
}

func (s *SetupService) readSetupSnapshot(journal setupTransactionJournal) (setupSnapshot, error) {
	if journal.Previous.AllAbsent || journal.Previous.SnapshotDir == "" {
		return setupSnapshot{}, nil
	}
	root := filepath.Clean(journal.Previous.SnapshotDir)
	if root != filepath.Clean(filepath.Join(s.config.Paths.PreviousDir, ".setup-snapshot")) {
		return setupSnapshot{}, errSetupJournalInvalid
	}
	if state, stateErr := setupPathState(root); stateErr != nil {
		return setupSnapshot{}, errSetupJournalInvalid
	} else if state == setupPathAbsent && journal.Phase == setupPhaseRuntimeVerified {
		// Success proof is journaled before transient snapshot cleanup. A
		// crash after that cleanup but before journal removal is already a
		// committed generation, not a recoverable takeover snapshot.
		return setupSnapshot{}, nil
	}
	contents, err := readBoundedSetupFile(filepath.Join(root, "manifest.json"), setupMaxCronBytes)
	if err != nil {
		return setupSnapshot{}, errSetupJournalInvalid
	}
	var manifest setupSnapshotManifest
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&manifest) != nil || decoder.Decode(&extra) != io.EOF || manifest.SchemaVersion != SetupTransactionSchemaVersion || manifest.Owner != setupSnapshotOwner || len(manifest.Entries) == 0 || len(manifest.Entries) > setupMaxSnapshotEntries || setupSnapshotDigest(manifest) != journal.Previous.SnapshotSHA || validXKeenOwner(root, setupSnapshotOwner) != nil {
		return setupSnapshot{}, errSetupJournalInvalid
	}
	return setupSnapshot{Dir: root, Manifest: manifest}, nil
}

func (s *SetupService) removeSnapshot(snapshot setupSnapshot) error {
	if snapshot.Dir == "" {
		return nil
	}
	if filepath.Clean(snapshot.Dir) != filepath.Clean(filepath.Join(s.config.Paths.PreviousDir, ".setup-snapshot")) {
		return errSetupLayoutInvalid
	}
	state, err := setupPathState(snapshot.Dir)
	if errors.Is(err, os.ErrNotExist) || state == setupPathAbsent {
		return nil
	}
	if err != nil || state != setupPathDirectory || validXKeenOwner(snapshot.Dir, setupSnapshotOwner) != nil {
		return errSetupLayoutInvalid
	}
	if err := os.RemoveAll(snapshot.Dir); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(snapshot.Dir))
}

func (s *SetupService) cleanupSetupSnapshotIntent() error {
	root := s.setupSnapshotRoot()
	state, err := setupPathState(root)
	if err != nil || state == setupPathAbsent {
		return err
	}
	if state != setupPathDirectory || validXKeenOwner(root, setupSnapshotOwner) != nil {
		return errSetupLayoutInvalid
	}
	maxFileBytes := int64(MaxXrayCandidateBinaryBytes)
	for _, limit := range []int64{MaxGeodataFileBytes, MaxXKeenGenerationFileBytes, nodes.MaxLegacyDocument, appliance.MaxDocumentSize, setupMaxCronBytes} {
		if limit > maxFileBytes {
			maxFileBytes = limit
		}
	}
	maxRootBytes := int64(setupMaxSnapshotBytes) + int64(setupMaxCronBytes) + int64(setupMaxSnapshotEntries*256) + maxFileBytes
	if err := setupRollbackDirectorySafe(root, maxFileBytes, maxRootBytes); err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(root))
}

func (s *SetupService) restoreSetupSnapshot(snapshot setupSnapshot) error {
	if snapshot.Dir == "" {
		return nil
	}
	if err := s.checkSetupWriterConflictForRestore(snapshot); err != nil {
		return err
	}
	targets := s.setupSnapshotTargets(nil)
	byKey := make(map[string]setupSnapshotTarget, len(targets))
	for _, target := range targets {
		byKey[target.Key] = target
	}
	for _, entry := range snapshot.Manifest.Entries {
		target, ok := byKey[entry.Key]
		if !ok && setupSnapshotTargetAllowed(s.config.Paths, entry.Target) {
			target = setupSnapshotTarget{Key: entry.Key, Path: entry.Target, Recursive: entry.Relative == "" && entry.Kind == "directory"}
			ok = true
		}
		if !ok {
			return errSetupLayoutInvalid
		}
		if target.Recursive && entry.Relative == "" && entry.Kind == "directory" {
			maxFileBytes, maxRootBytes := setupSnapshotLimits(target)
			if state, err := setupPathState(target.Path); err != nil {
				return err
			} else if state == setupPathDirectory {
				if err := setupRollbackDirectorySafe(target.Path, maxFileBytes, maxRootBytes); err != nil {
					return err
				}
				if err := os.RemoveAll(target.Path); err != nil {
					return err
				}
			} else if state != setupPathAbsent {
				return errSetupLayoutInvalid
			}
		}
	}
	for _, entry := range snapshot.Manifest.Entries {
		target, ok := byKey[entry.Key]
		if !ok && setupSnapshotTargetAllowed(s.config.Paths, entry.Target) {
			target = setupSnapshotTarget{Key: entry.Key, Path: entry.Target, Recursive: entry.Relative == "" && entry.Kind == "directory"}
			ok = true
		}
		if !ok {
			return errSetupLayoutInvalid
		}
		path := target.Path
		if entry.Relative != "" {
			relative := filepath.FromSlash(entry.Relative)
			if filepath.IsAbs(relative) || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
				return errSetupLayoutInvalid
			}
			path = filepath.Join(path, relative)
		}
		switch entry.Kind {
		case "absent":
			state, err := setupPathState(path)
			if err != nil {
				return err
			}
			if state == setupPathRegular {
				if err := os.Remove(path); err != nil {
					return err
				}
			} else if state == setupPathDirectory {
				maxFileBytes, maxRootBytes := setupSnapshotLimits(target)
				if !target.Recursive || setupRollbackDirectorySafe(path, maxFileBytes, maxRootBytes) != nil {
					return errSetupLayoutInvalid
				}
				if err := os.RemoveAll(path); err != nil {
					return err
				}
			} else if state != setupPathAbsent {
				return errSetupLayoutInvalid
			}
		case "directory":
			if err := ensureSetupDirectory(path); err != nil {
				return err
			}
			if entry.Mode != 0 {
				_ = os.Chmod(path, os.FileMode(entry.Mode))
			}
		case "file":
			if entry.Payload == "" || filepath.Base(entry.Payload) != entry.Payload || strings.ContainsAny(entry.Payload, `/\\`) {
				return errSetupLayoutInvalid
			}
			maxFileBytes, _ := setupSnapshotLimits(target)
			contents, err := readBoundedSetupFile(filepath.Join(snapshot.Dir, "payload", entry.Payload), maxFileBytes)
			if err != nil || int64(len(contents)) != entry.Size || digestSetupBytes(contents) != entry.SHA256 {
				return errSetupLayoutInvalid
			}
			if err := ensureSetupDirectory(filepath.Dir(path)); err != nil {
				return err
			}
			temporary, err := os.CreateTemp(filepath.Dir(path), ".restore-")
			if err != nil {
				return err
			}
			temporaryPath := temporary.Name()
			if err := temporary.Chmod(os.FileMode(entry.Mode)); err == nil {
				_, err = temporary.Write(contents)
			}
			if err == nil {
				err = temporary.Sync()
			}
			if closeErr := temporary.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(temporaryPath, path)
			}
			_ = os.Remove(temporaryPath)
			if err != nil {
				return err
			}
		default:
			return errSetupLayoutInvalid
		}
	}
	return nil
}

func setupRollbackDirectorySafe(path string, maxFileBytes, maxRootBytes int64) error {
	if maxFileBytes <= 0 {
		maxFileBytes = MaxXKeenGenerationFileBytes
	}
	if maxRootBytes <= 0 {
		maxRootBytes = MaxXKeenGenerationBytes
	}
	entries := 0
	rootBytes := int64(0)
	return filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > setupMaxSnapshotEntries || entry.Type()&os.ModeSymlink != 0 {
			return errSetupLayoutInvalid
		}
		if current == path || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxFileBytes || rootBytes > maxRootBytes-info.Size() {
			return errSetupLayoutInvalid
		}
		rootBytes += info.Size()
		return nil
	})
}

func (s *SetupService) checkSetupWriterConflictForRestore(snapshot setupSnapshot) error {
	current, err := s.inspectSetupWriters()
	if err != nil {
		return ErrSetupWriterConflict
	}
	original := make(map[string]string)
	for _, entry := range snapshot.Manifest.Entries {
		if strings.HasPrefix(entry.Key, "writer-") && entry.Kind == "file" {
			original[filepath.Clean(entry.Target)] = entry.SHA256
		}
	}
	for _, writer := range current {
		want, ok := original[filepath.Clean(writer.Path)]
		if !ok || want != writer.Digest {
			return ErrSetupWriterConflict
		}
	}
	return nil
}

func setupSnapshotTargetAllowed(paths SetupPaths, path string) bool {
	if path == "" {
		return false
	}
	for _, target := range (&SetupService{config: SetupConfig{Paths: paths}}).setupSnapshotTargets(nil) {
		if filepath.Clean(target.Path) == filepath.Clean(path) {
			return true
		}
	}
	for _, candidate := range paths.WriterScripts {
		if filepath.Clean(candidate) == filepath.Clean(path) {
			return true
		}
	}
	if setupCronPath(paths, path) {
		return true
	}
	return false
}

func setupCronPath(paths SetupPaths, path string) bool {
	path = filepath.Clean(path)
	for _, candidate := range paths.CronPaths {
		candidate = filepath.Clean(candidate)
		if candidate == path {
			return true
		}
		state, err := setupPathState(candidate)
		if err != nil || state != setupPathDirectory {
			continue
		}
		relative, relErr := filepath.Rel(candidate, path)
		if relErr == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && filepath.Dir(relative) != ".." {
			return true
		}
	}
	return false
}

func (s *SetupService) failApply(journalWritten bool, journal setupTransactionJournal, prepared preparedSetup, runtimeStarted bool, cause error) error {
	if !journalWritten {
		return cause
	}
	recoveryContext, cancel := context.WithTimeout(context.Background(), s.config.RecoveryTimeout)
	defer cancel()
	if runtimeStarted || journal.SourceClass != "fresh" {
		if err := s.quiesceSetupRuntime(recoveryContext); err != nil {
			s.markMaintenance(ErrSetupTransactionUnproven)
			return ErrSetupTransactionUnproven
		}
	}
	if err := s.rollbackCreated(recoveryContext, journal, prepared); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return ErrSetupTransactionUnproven
	}
	if err := s.restoreSetupSelection(recoveryContext, journal.Previous.SelectionSnapshot); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return ErrSetupTransactionUnproven
	}
	if journal.SourceClass != "fresh" && setupSnapshotHasLifecycle(prepared.snapshot) {
		if err := s.startAndProveRestoredRuntime(recoveryContext); err != nil {
			s.markMaintenance(ErrSetupTransactionUnproven)
			return ErrSetupTransactionUnproven
		}
	}
	if err := s.removeSnapshot(prepared.snapshot); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return ErrSetupTransactionUnproven
	}
	if err := s.clearJournal(); err != nil {
		s.markMaintenance(ErrSetupTransactionUnproven)
		return ErrSetupTransactionUnproven
	}
	if errors.Is(cause, ErrSetupWriterConflict) {
		return ErrSetupWriterConflict
	}
	return ErrSetupTransactionRestored
}

func (s *SetupService) rollbackCreated(ctx context.Context, journal setupTransactionJournal, prepared preparedSetup) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := s.config.Paths
	if !journal.Previous.AllAbsent {
		snapshot := prepared.snapshot
		if snapshot.Dir == "" {
			var err error
			snapshot, err = s.readSetupSnapshot(journal)
			if err != nil {
				return err
			}
		}
		if err := s.restoreSetupSnapshot(snapshot); err != nil {
			return err
		}
		if err := s.cleanupSetupActivation(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for _, name := range []string{"S05xkeen"} {
		if err := removeSetupFileExact(paths.LifecycleInit, prepared.lifecycle); err != nil {
			return err
		}
		_ = name
	}
	if err := removeSetupFileExact(paths.XkeenMarker, prepared.marker); err != nil {
		return err
	}
	if err := removeSetupGenerationPartial(paths.XkeenBinary, paths.XkeenModuleDir, prepared.xkeenMetadata); err != nil {
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
	if err := s.cleanupSetupActivation(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, directory := range []string{paths.XrayConfigDir, paths.XrayAssetDir, paths.XkeenModuleDir, filepath.Dir(paths.Appliance), filepath.Dir(paths.Nodes)} {
		removeEmptySetupDirectory(directory)
	}
	return nil
}

func setupSnapshotHasFile(snapshot setupSnapshot, key string) bool {
	for _, entry := range snapshot.Manifest.Entries {
		if entry.Key == key && entry.Kind == "file" && entry.Relative == "" {
			return true
		}
	}
	return false
}

func setupSnapshotHasLifecycle(snapshot setupSnapshot) bool {
	return setupSnapshotHasFile(snapshot, "lifecycle") || setupSnapshotHasFile(snapshot, "legacy-lifecycle")
}

func (s *SetupService) startAndProveRestoredRuntime(ctx context.Context) error {
	if s.config.Runtime == nil {
		return ErrSetupRuntimeUnavailable
	}
	if err := s.config.Runtime.Start(ctx); err != nil {
		return ErrSetupRuntimeUnavailable
	}
	authorities, reason, err := s.readSetupAuthorities()
	if err != nil || reason != "" || !authorities.profilePresent {
		return ErrSetupVerificationFailed
	}
	if err := s.runtimeProof(ctx, authorities.registry); err != nil {
		return err
	}
	return nil
}

func (s *SetupService) restoreSetupSelection(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 || s.config.Selection == nil {
		return nil
	}
	selection, ok := s.config.Selection.(SetupSelectionTransaction)
	if !ok {
		return ErrSetupTransactionUnproven
	}
	if len(snapshot) > setupMaxSelectionBytes {
		return ErrSetupTransactionUnproven
	}
	if err := selection.RestoreSetupSelection(ctx, snapshot); err != nil {
		return ErrSetupTransactionUnproven
	}
	return nil
}

func removeSetupGenerationPartial(binaryPath, modulePath string, expected xkeenGenerationMetadata) error {
	if !validXKeenGenerationMetadata(expected) {
		return errSetupLayoutInvalid
	}
	if state, err := setupPathState(binaryPath); err != nil {
		return err
	} else if state != setupPathAbsent {
		if state != setupPathRegular {
			return errSetupLayoutInvalid
		}
		var binaryEntry xkeenGenerationEntry
		for _, entry := range expected.Entries {
			if entry.Path == "xkeen" {
				binaryEntry = entry
				break
			}
		}
		contents, readErr := readBoundedSetupFile(binaryPath, MaxXKeenGenerationFileBytes)
		if readErr != nil || binaryEntry.Path == "" || int64(len(contents)) != binaryEntry.Size || digestSetupBytes(contents) != binaryEntry.SHA256 {
			return errSetupLayoutInvalid
		}
		if err := os.Remove(binaryPath); err != nil {
			return err
		}
	}
	if state, err := setupPathState(modulePath); err != nil {
		return err
	} else if state != setupPathAbsent {
		if state != setupPathDirectory || !validSetupPartialXKeenDirectory(modulePath, expected) {
			return errSetupLayoutInvalid
		}
		if err := os.RemoveAll(modulePath); err != nil {
			return err
		}
	}
	return nil
}

func validSetupPartialXKeenDirectory(root string, expected xkeenGenerationMetadata) bool {
	allowed := make(map[string]xkeenGenerationEntry, len(expected.Entries))
	for _, entry := range expected.Entries {
		if entry.Path == "xkeen" {
			continue
		}
		relative := strings.TrimPrefix(entry.Path, ".xkeen/")
		if relative == entry.Path || relative == "" {
			continue
		}
		allowed[filepath.FromSlash(relative)] = entry
	}
	valid := true
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !valid {
			valid = false
			return walkErr
		}
		if path == root {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.Mode()&os.ModeSymlink != 0 {
			valid = false
			return errSetupLayoutInvalid
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			valid = false
			return relErr
		}
		if entry.IsDir() {
			for key, item := range allowed {
				if item.Type == "directory" && filepath.Clean(key) == filepath.Clean(relative) {
					return nil
				}
			}
			// Empty directories are valid only when the expected generation also
			// owns that directory. This covers a crash immediately after mkdir.
			for key, item := range allowed {
				if item.Type == "directory" && filepath.Dir(key) == filepath.Clean(relative) {
					return nil
				}
			}
			valid = false
			return errSetupLayoutInvalid
		}
		item, ok := allowed[filepath.Clean(relative)]
		if !ok || item.Type != "file" || info.Size() != item.Size {
			valid = false
			return errSetupLayoutInvalid
		}
		contents, readErr := readBoundedSetupFile(path, MaxXKeenGenerationFileBytes)
		if readErr != nil || digestSetupBytes(contents) != item.SHA256 {
			valid = false
			return errSetupLayoutInvalid
		}
		return nil
	})
	return valid
}

func removeEmptySetupDirectory(path string) {
	if path == "" || path == "." || filepath.Clean(path) == string(filepath.Separator) {
		return
	}
	entries, err := os.ReadDir(path)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(path)
	}
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
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || validXKeenOwner(target, setupStagingOwner) != nil {
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
		if childErr != nil || child.Mode()&os.ModeSymlink != 0 || !child.IsDir() || validXKeenOwner(path, setupStagingOwner) != nil {
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
		// A crash before journal creation can leave only private setup staging
		// or an older orphaned snapshot. Remove only owned, bounded entries.
		if err := s.cleanupSetupSnapshotIntent(); err != nil {
			return s.recoveryFailure()
		}
		if err := s.removeStagingRootEntries(); err != nil {
			return s.recoveryFailure()
		}
		if activation, activationErr := setupPathState(s.config.Paths.XkeenActivation); activationErr != nil {
			return s.recoveryFailure()
		} else if activation != setupPathAbsent {
			if err := s.cleanupSetupActivation(); err != nil {
				return s.recoveryFailure()
			}
		}
		layout, layoutErr := s.inspectLayout()
		if layoutErr != nil || layout.state != "fresh" && layout.state != "takeover" {
			return s.recoveryFailure()
		}
		s.clearMaintenance()
		return nil
	}
	if journal.Phase == setupPhaseSnapshotIntent {
		// Snapshot creation has not yet reached the ready manifest phase. The
		// target environment is untouched; discard the owned partial/complete
		// snapshot and the journaled staging tree, then reclassify.
		if err := s.cleanupSetupSnapshotIntent(); err != nil || s.clearJournal() != nil || s.removeStagingRootEntries() != nil {
			return s.recoveryFailure()
		}
		layout, layoutErr := s.inspectLayout()
		if layoutErr != nil || layout.state != "fresh" && layout.state != "takeover" {
			return s.recoveryFailure()
		}
		s.clearMaintenance()
		return nil
	}
	if journal.Phase == setupPhaseSnapshotReady {
		// A bounded cleanup can clear the journal after proving that no target
		// write occurred. If power was lost after that clear's predecessor
		// removed the snapshot, the ready-phase journal is still recoverable as
		// an untouched previous environment; do not quiesce a live runtime or
		// attempt a rollback from a missing payload.
		if snapshotState, snapshotErr := setupPathState(s.setupSnapshotRoot()); snapshotErr != nil {
			return s.recoveryFailure()
		} else if snapshotState == setupPathAbsent {
			if s.clearJournal() != nil || s.removeStagingRootEntries() != nil {
				return s.recoveryFailure()
			}
			layout, layoutErr := s.inspectLayout()
			if layoutErr != nil || layout.state != "fresh" && layout.state != "takeover" {
				return s.recoveryFailure()
			}
			s.clearMaintenance()
			return nil
		}
	}
	prepared, err := s.preparedFromJournal(journal)
	if err != nil {
		return s.recoveryFailure()
	}
	if journal.Phase == setupPhaseRuntimeVerified && s.runtimeProof(ownedContext, prepared.candidate.Registry) == nil && s.verifyInstalled(prepared) == nil {
		if err := s.removeSnapshot(prepared.snapshot); err != nil || s.clearJournal() != nil || s.removeStagingRootEntries() != nil {
			return s.recoveryFailure()
		}
		s.clearMaintenance()
		return nil
	}
	if journal.SourceClass != "fresh" || journal.Phase == setupPhaseLifecycleCommitted || journal.Phase == setupPhaseWritersRetired || journal.Phase == setupPhaseRuntimeStarted || journal.Phase == setupPhaseSelectionReconciled || journal.Phase == setupPhaseRuntimeVerified {
		if err := s.quiesceSetupRuntime(ownedContext); err != nil {
			return s.recoveryFailure()
		}
	}
	if err := s.rollbackCreated(ownedContext, journal, prepared); err != nil {
		return s.recoveryFailure()
	}
	if err := s.restoreSetupSelection(ownedContext, journal.Previous.SelectionSnapshot); err != nil {
		return s.recoveryFailure()
	}
	if journal.SourceClass != "fresh" && setupSnapshotHasLifecycle(prepared.snapshot) {
		if err := s.startAndProveRestoredRuntime(ownedContext); err != nil {
			return s.recoveryFailure()
		}
	}
	if err := s.removeSnapshot(prepared.snapshot); err != nil || s.clearJournal() != nil || s.removeStagingRootEntries() != nil {
		return s.recoveryFailure()
	}
	s.clearMaintenance()
	return nil
}

func setupRuntimeTags(registry nodes.Registry) []string {
	ordered := registry.SortedNodes()
	tags := make([]string, 0, len(ordered))
	for _, node := range ordered {
		if node.Enabled && !node.Stale && !node.Missing {
			tags = append(tags, node.OutboundTag)
		}
	}
	return tags
}

func (s *SetupService) runtimeProof(ctx context.Context, registry nodes.Registry) error {
	if s.config.Runtime == nil {
		return ErrSetupRuntimeUnavailable
	}
	if err := s.config.Runtime.WaitReady(ctx); err != nil || !s.config.Runtime.ProbeReachable(ctx) || s.config.Runtime.ValidateActiveConfig(ctx) != nil {
		return ErrSetupVerificationFailed
	}
	tags := setupRuntimeTags(registry)
	if len(tags) == 0 {
		if err := s.config.Runtime.VerifyEmpty(ctx); err != nil {
			return ErrSetupVerificationFailed
		}
		return nil
	}
	verifier, ok := s.config.Runtime.(SetupRuntimeVerifier)
	if !ok {
		return ErrSetupRuntimeUnavailable
	}
	if err := verifier.Verify(ctx, tags); err != nil {
		return ErrSetupVerificationFailed
	}
	return nil
}

func (s *SetupService) reconcileSetupSelection(ctx context.Context, registry nodes.Registry) error {
	if s.config.Selection == nil {
		return nil
	}
	return s.config.Selection.ReconcileSetupSelection(ctx, setupRuntimeTags(registry))
}

func (s *SetupService) quiesceSetupRuntime(ctx context.Context) error {
	if s.config.Runtime == nil {
		return ErrSetupRuntimeUnavailable
	}
	quiescer, ok := s.config.Runtime.(SetupRuntimeQuiescer)
	if !ok {
		return ErrSetupRuntimeUnavailable
	}
	if err := quiescer.Stop(ctx); err != nil {
		return ErrSetupRuntimeUnavailable
	}
	if err := quiescer.VerifyStopped(ctx); err != nil {
		return ErrSetupVerificationFailed
	}
	// C.1 is a separate reachability witness from the process/API checks in
	// the quiescer. A late verification failure must not restore over a still
	// reachable previous runtime.
	if s.config.Runtime.ProbeReachable(ctx) {
		return ErrSetupVerificationFailed
	}
	return nil
}

func (s *SetupService) preparedFromJournal(journal setupTransactionJournal) (preparedSetup, error) {
	stageRoot := filepath.Clean(journal.StageDir)
	stagingRoot := filepath.Clean(s.config.Paths.StagingDir)
	relative, relErr := filepath.Rel(stagingRoot, stageRoot)
	if relErr != nil || relative == "." || filepath.Dir(relative) != "." || !strings.HasPrefix(filepath.Base(stageRoot), ".setup-") {
		return preparedSetup{}, errSetupJournalInvalid
	}
	if validXKeenOwner(stageRoot, setupStagingOwner) != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	lifecycle, err := setupLifecycleBytes()
	if err != nil || setupLifecycleDigest(lifecycle) != journal.Candidate.LifecycleSHA256 {
		return preparedSetup{}, errSetupJournalInvalid
	}
	value := appliance.ProductDefault()
	appBytes, err := appliance.MarshalCanonical(value)
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	registry := nodes.NewRegistry()
	nodesBytes, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	if journal.SourceClass != "fresh" {
		if currentApp, appErr := readBoundedSetupFile(s.config.Paths.Appliance, appliance.MaxDocumentSize); appErr == nil {
			if parsed, parseErr := appliance.Parse(currentApp); parseErr == nil {
				value, appBytes = parsed, currentApp
			}
		}
		if currentNodes, nodesErr := readBoundedSetupFile(s.config.Paths.Nodes, nodes.MaxRegistryDocument); nodesErr == nil {
			if parsed, parseErr := nodes.ParseCanonical(currentNodes); parseErr == nil {
				registry, nodesBytes = parsed, currentNodes
			}
		}
	}
	authorities := setupAuthorities{registry: registry, registryBytes: nodesBytes, app: value, appBytes: appBytes, profilePresent: true, appPresent: true, policyPresent: true, profileAction: "preserve", policyAction: "preserve"}
	if journal.SourceClass == "fresh" {
		authorities.profileAction = "empty"
		authorities.policyAction = "product-default"
	}
	source := authorities.sourceManifest(journal.SourceClass, nil)
	source.Digest = journal.SourceDigest
	candidate := setupCandidate{Xray: journal.Candidate.Xray, Geodata: journal.Candidate.Geodata, XKeen: journal.Candidate.XKeen, Lifecycle: lifecycle, Registry: registry, NodesBytes: nodesBytes, Appliance: value, AppBytes: appBytes, Source: source}
	files, err := appliance.RenderCandidateFiles(value, registry)
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
	stageXkeen := filepath.Join(journal.StageDir, "xkeen")
	generationPairs := [][2]string{
		{filepath.Join(stageXkeen, "xkeen"), filepath.Join(stageXkeen, ".xkeen")},
		{s.config.Paths.XkeenBinary, s.config.Paths.XkeenModuleDir},
		{s.config.Paths.XkeenBinary, filepath.Join(s.config.Paths.XkeenActivation, "new", ".xkeen")},
	}
	for _, pair := range generationPairs {
		current, readErr := readXKeenGeneration(pair[0], pair[1])
		if readErr == nil && strings.EqualFold(current.GenerationSHA256(), xkeenIdentityGeneration(candidate.XKeen)) {
			xkeenMeta = current
			break
		}
	}
	if !validXKeenGenerationMetadata(xkeenMeta) {
		if validXKeenGenerationMetadata(journal.Candidate.XKeenGeneration) && strings.EqualFold(journal.Candidate.XKeenGeneration.Generation, xkeenIdentityGeneration(candidate.XKeen)) {
			xkeenMeta = journal.Candidate.XKeenGeneration
		} else {
			return preparedSetup{}, errSetupJournalInvalid
		}
	}
	_, marker, markerErr := markerForGeneration(xkeenIdentityGeneration(candidate.XKeen))
	if markerErr != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	plan, err := makeSetupPlan(candidate)
	if err != nil {
		return preparedSetup{}, errSetupJournalInvalid
	}
	snapshot, snapshotErr := s.readSetupSnapshot(journal)
	if snapshotErr != nil {
		return preparedSetup{}, snapshotErr
	}
	return preparedSetup{candidate: candidate, plan: plan, stageDir: journal.StageDir, configFiles: files, appBytes: appBytes, nodesBytes: nodesBytes, xrayMetadata: xrayMeta, geodataMetadata: meta, xkeenMetadata: xkeenMeta, marker: marker, lifecycle: lifecycle, snapshot: snapshot}, nil
}

func (s *SetupService) recoveryFailure() error {
	s.markMaintenance(ErrSetupRecoveryFailed)
	return ErrSetupRecoveryFailed
}

var _ SetupAPI = (*SetupService)(nil)
