package components

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"path"
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
	XrayTransactionSchemaVersion = 1

	DefaultComponentTransactionJournal = "/opt/etc/xkeen-control/state/component-transaction.json"
	DefaultXrayPreviousDir             = "/opt/etc/xkeen-control/previous/components/xray"
	DefaultXrayComponentStagingDir     = "/tmp/xkeen-control/components/xray"
	DefaultXrayAssetDir                = "/opt/etc/xray/dat"
	DefaultXrayConfigDir               = "/opt/etc/xray/configs"

	DefaultXrayAuthorityWaitTimeout = 15 * time.Second
	DefaultXrayPrepareTimeout       = 2 * time.Minute
	DefaultXrayActivationTimeout    = 2 * time.Minute
	DefaultXrayRollbackTimeout      = 2 * time.Minute
	DefaultXrayTransactionTimeout   = 5 * time.Minute

	MaxXrayArchiveEntries          = 64
	MaxXrayArchiveEntryBytes       = 64 << 20
	MaxXrayArchiveUncompressedSize = 128 << 20
	MaxXrayCandidateBinaryBytes    = 64 << 20
	XrayFreeSpaceReserve           = 8 << 20
	MaxComponentJournalBytes       = 16 << 10
	MaxPreviousGenerationMetadata  = 2 << 10

	xrayPreviousBinaryName    = "xray"
	xrayPreviousMetadataName  = "metadata.json"
	xrayPreviousStagingSuffix = ".staging"
	xrayPreviousOldSuffix     = ".old"
	xrayExpectedArchiveMember = "xray"
	xrayOperationUpdate       = "update"
	xrayOperationRollback     = "rollback"
	xrayPhasePrepared         = "prepared"
	xrayPhaseBinaryCommitted  = "binary-committed"
	xrayPhaseRuntimeVerified  = "runtime-verified"
)

var (
	ErrXrayResolutionUnavailable  = errors.New("xray release resolution unavailable")
	ErrXrayCandidateRejected      = errors.New("xray candidate was rejected")
	ErrXrayCandidateStale         = errors.New("xray candidate is stale")
	ErrXrayArtifactRejected       = errors.New("xray artifact was rejected")
	ErrXrayAuthorityUnavailable   = errors.New("xray authority is unavailable")
	ErrXrayAuthorityBusy          = errors.New("xray authority is busy")
	ErrXrayTransactionUnavailable = errors.New("xray transaction is unavailable")
	ErrXrayApplyFailed            = errors.New("xray activation failed")
	ErrXrayRollbackFailed         = errors.New("xray rollback failed")
	ErrXrayPreviousUnavailable    = errors.New("previous xray generation is unavailable")
	ErrXrayRecoveryRequired       = errors.New("xray component recovery is required")
	ErrXrayRecoveryConflict       = errors.New("xray component recovery conflicts with restore")
	ErrXrayRecoveryFailed         = errors.New("xray component recovery failed")
	ErrXrayBusy                   = errors.New("xray component transaction is busy")

	errXrayArchiveRejected        = errors.New("xray archive was rejected")
	errXrayArchiveTooLarge        = errors.New("xray archive exceeds the limit")
	errXrayArtifactTooLarge       = errors.New("xray artifact exceeds the limit")
	errXrayArtifactSizeMismatch   = errors.New("xray artifact size does not match metadata")
	errXrayArtifactHashMismatch   = errors.New("xray artifact digest does not match metadata")
	errXrayArtifactRejected       = errors.New("xray artifact was rejected")
	errXrayBinaryInvalid          = errors.New("xray binary is invalid")
	errXrayCandidateConfigInvalid = errors.New("xray candidate configuration is invalid")
	errXrayJournalInvalid         = errors.New("xray transaction journal is invalid")
	errXrayPreviousInvalid        = errors.New("previous xray generation is invalid")
	errXrayGenerationChanged      = errors.New("xray authority generation changed")
	errXrayFreeSpaceUnavailable   = errors.New("xray component free space is unavailable")
	errXrayFreeSpaceInsufficient  = errors.New("xray component free space is insufficient")
	errXrayRedirectRejected       = errors.New("xray artifact redirect rejected")
)

// XrayAuthoritySnapshot contains typed D.1 authorities plus a digest of the
// complete adopted/coherent generation. Registry credentials may exist only
// in this internal in-memory value; Generation is the only value retained for
// stale checks and journal decisions.
type XrayAuthoritySnapshot struct {
	Appliance  appliance.Appliance
	Registry   nodes.Registry
	Generation [sha256.Size]byte
}

// XrayAuthorityProvider is called only while the shared authority lease is
// held by XrayService. Implementations must prove adopted D.1 authority,
// generated/fixed-file coherence and return a hash-only generation token.
type XrayAuthorityProvider interface {
	SnapshotUnderLease(context.Context) (XrayAuthoritySnapshot, error)
}

// XrayArtifactDownloader is purpose-built for the fixed Xray release asset.
// It receives no URL, repository, path or archive-member input. The service
// supplies a bounded writer in its private staging area.
type XrayArtifactDownloader interface {
	DownloadXray(context.Context, XrayReleaseIdentity, io.Writer) error
}

// XrayCandidateProbe is deliberately narrower than a command runner. The
// service supplies only the private staged candidate or fixed active binary.
type XrayCandidateProbe interface {
	ProbeXrayCandidate(context.Context, string) XrayVersionResult
}

// XrayCandidateValidator validates a complete fixed config tree with one
// purpose-specific Xray binary. It is not exposed through HTTP or CLI.
type XrayCandidateValidator interface {
	ValidateXrayCandidate(context.Context, string, string, string) error
}

// XrayRuntime owns the existing fixed foreground lifecycle and its structured
// runtime checks. Verify must cover balancer inventory and the dedicated C.1
// probe path; the component transaction does not accept process existence as
// success.
type XrayRuntime interface {
	ValidateActiveConfig(context.Context) error
	Restart(context.Context) error
	WaitReady(context.Context) error
	Verify(context.Context, []string) error
}

// XrayCoordinator is the only lifecycle admission capability accepted by
// the transaction engine. Production wiring supplies c1.Coordinator.
type XrayCoordinator interface {
	BeginApply(context.Context) (func(), error)
}

// XrayRecoveryCoordinator is the recovery-only admission capability shared
// with the appliance restore transaction.
type XrayRecoveryCoordinator interface {
	BeginRecovery(context.Context) (func(), error)
}

type XrayMaintenanceGate interface {
	EnterMaintenance()
	ExitMaintenance()
}

// XrayStage names bounded synthetic fault-injection points. They are not
// serialized and never contain candidate content.
type XrayStage string

const (
	XrayStagePreviousStaging XrayStage = "previous-staging"
	XrayStagePreviousSaved   XrayStage = "previous-saved"
	XrayStageJournalPrepared XrayStage = "journal-prepared"
	XrayStageBinaryCommitted XrayStage = "binary-committed"
	XrayStagePreviousSettled XrayStage = "previous-settled"
	XrayStageRuntimeVerified XrayStage = "runtime-verified"
	XrayStageJournalCleared  XrayStage = "journal-cleared"
)

type XrayConfig struct {
	Resolver   XrayCandidateResolver
	Downloader XrayArtifactDownloader
	Authority  XrayAuthorityProvider
	Runtime    XrayRuntime

	CandidateProbe     XrayCandidateProbe
	CandidateValidator XrayCandidateValidator

	AuthorityLease *authority.Lease
	Coordinator    XrayCoordinator

	ActiveBinaryPath   string
	ConfigDir          string
	AssetDir           string
	PreviousDir        string
	JournalPath        string
	StagingDir         string
	RestoreJournalPath string
	MutationGate       *ComponentMutationGate
	Maintenance        *ComponentMaintenance

	AuthorityWaitTimeout time.Duration
	PrepareTimeout       time.Duration
	ActivationTimeout    time.Duration
	RollbackTimeout      time.Duration
	TransactionTimeout   time.Duration

	AvailableSpace func(string) (uint64, error)
	SyncDirectory  func(string) error
	InjectFailure  func(XrayStage) error
}

type XrayService struct {
	config XrayConfig

	mutationGate *ComponentMutationGate
	startupMu    sync.Mutex
	mu           sync.Mutex
	ready        bool
	readyErr     error
	maintenance  bool
}

type preparedXray struct {
	identity      XrayReleaseIdentity
	base          xrayBaseSnapshot
	stageDir      string
	candidatePath string
	candidateMeta xrayBinaryMetadata
}

type xrayBaseSnapshot struct {
	authority XrayAuthoritySnapshot
	active    xrayBinaryMetadata
}

// NewXrayService constructs the internal Phase C transaction engine. It does
// not create directories, contact upstream or mutate any runtime state.
func NewXrayService(config XrayConfig) *XrayService {
	if config.Resolver == nil {
		config.Resolver = NewXrayResolver(nil, nil)
	}
	if config.Downloader == nil {
		config.Downloader = NewXrayArtifactDownloader(nil, nil)
	}
	if config.CandidateProbe == nil {
		config.CandidateProbe = CommandXrayCandidateProbe{}
	}
	if config.CandidateValidator == nil {
		config.CandidateValidator = CommandXrayCandidateValidator{}
	}
	if config.ActiveBinaryPath == "" {
		config.ActiveBinaryPath = DefaultXrayBinary
	}
	if config.ConfigDir == "" {
		config.ConfigDir = DefaultXrayConfigDir
	}
	if config.AssetDir == "" {
		config.AssetDir = DefaultXrayAssetDir
	}
	if config.PreviousDir == "" {
		config.PreviousDir = DefaultXrayPreviousDir
	}
	if config.JournalPath == "" {
		config.JournalPath = DefaultComponentTransactionJournal
	}
	if config.StagingDir == "" {
		config.StagingDir = DefaultXrayComponentStagingDir
	}
	if config.RestoreJournalPath == "" {
		config.RestoreJournalPath = filepath.Join(filepath.Dir(config.JournalPath), "appliance-import-transaction.json")
	}
	if config.AuthorityWaitTimeout <= 0 {
		config.AuthorityWaitTimeout = DefaultXrayAuthorityWaitTimeout
	}
	if config.PrepareTimeout <= 0 {
		config.PrepareTimeout = DefaultXrayPrepareTimeout
	}
	if config.ActivationTimeout <= 0 {
		config.ActivationTimeout = DefaultXrayActivationTimeout
	}
	if config.RollbackTimeout <= 0 {
		config.RollbackTimeout = DefaultXrayRollbackTimeout
	}
	if config.TransactionTimeout <= 0 {
		config.TransactionTimeout = DefaultXrayTransactionTimeout
	}
	if config.AvailableSpace == nil {
		config.AvailableSpace = availableFreeSpace
	}
	if config.SyncDirectory == nil {
		config.SyncDirectory = syncDirectory
	}
	if config.MutationGate == nil {
		config.MutationGate = NewComponentMutationGate()
	}
	service := &XrayService{config: config, mutationGate: config.MutationGate, ready: true}
	componentKind, componentPresent, componentErr := componentJournalKind(config.JournalPath)
	restorePresent, restoreErr := componentTransactionPresent(config.RestoreJournalPath)
	if componentErr != nil || restoreErr != nil || (componentPresent && componentKind != KindXray) && service.stagingPresent() {
		service.ready = false
		service.readyErr = ErrXrayRecoveryFailed
		service.enterMaintenance()
		return service
	}
	if (componentPresent && componentKind == KindXray) || service.stagingPresent() {
		service.ready = false
		if restorePresent {
			service.readyErr = ErrXrayRecoveryConflict
		} else if componentPresent {
			service.readyErr = ErrXrayRecoveryRequired
		} else {
			service.readyErr = ErrXrayRecoveryFailed
		}
		service.enterMaintenance()
	}
	return service
}

// NewXrayTransaction is a descriptive constructor alias for future typed
// Phase F adapters.
func NewXrayTransaction(config XrayConfig) *XrayService { return NewXrayService(config) }

// Ready reports whether startup recovery has proved that no Xray transaction
// journal remains unresolved.
func (s *XrayService) Ready() error {
	if s == nil {
		return ErrXrayTransactionUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		if s.readyErr != nil {
			return s.readyErr
		}
		return ErrXrayRecoveryRequired
	}
	return nil
}

// HasPendingRecovery is used by process startup arbitration before normal
// HTTP service starts. It does not parse or mutate the journal.
func (s *XrayService) HasPendingRecovery() (bool, error) {
	if s == nil {
		return false, ErrXrayTransactionUnavailable
	}
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return false, err
	}
	if present && kind == KindXray {
		return true, nil
	}
	return s.stagingPresent(), nil
}

// Apply performs a fresh uncached resolution, trusted artifact preparation,
// complete candidate validation and the serialized Xray transaction. The
// intended identity is mandatory so a future preview/token caller cannot be
// silently upgraded to a newer latest release.
func (s *XrayService) Apply(ctx context.Context, intended XrayReleaseIdentity) error {
	if err := s.Ready(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	releaseMutation, err := s.acquireMutation(ctx)
	if err != nil {
		return err
	}
	defer releaseMutation()

	transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	prepareContext, cancelPrepare := context.WithTimeout(transactionContext, s.config.PrepareTimeout)
	prepared, err := s.prepare(prepareContext, intended)
	cancelPrepare()
	if err != nil {
		return err
	}
	defer s.removeOwned(prepared.stageDir)
	return s.applyPrepared(transactionContext, prepared)
}

// Update is an explicit spelling for the future typed caller; it introduces
// no second transaction implementation.
func (s *XrayService) Update(ctx context.Context, intended XrayReleaseIdentity) error {
	return s.Apply(ctx, intended)
}

// Rollback activates the one saved previous Xray generation without any
// upstream dependency. It follows the same Coordinator -> authority lease
// order and the same runtime verification path as Apply.
func (s *XrayService) Rollback(ctx context.Context) error {
	if err := s.Ready(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	releaseMutation, err := s.acquireMutation(ctx)
	if err != nil {
		return err
	}
	defer releaseMutation()

	transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	releaseCoordinator, releaseAuthority, err := s.acquireApply(transactionContext)
	if err != nil {
		return err
	}
	defer releaseCoordinator()
	defer releaseAuthority()

	base, err := s.captureHeld(transactionContext)
	if err != nil {
		return err
	}
	previous, err := s.loadPreviousGeneration()
	if err != nil {
		return ErrXrayPreviousUnavailable
	}
	if err := s.validateLocalCandidate(transactionContext, previous.path, base.authority); err != nil {
		return ErrXrayCandidateRejected
	}
	return s.runCommitted(transactionContext, xrayOperationRollback, base, previous.path, previous.meta, true)
}

// RecoverStartup performs local-only recovery of a retained component
// journal. It never calls the resolver or downloader. A restore journal in
// parallel is a hard conflict and never guessed around.
func (s *XrayService) RecoverStartup(ctx context.Context) error {
	if s == nil {
		return ErrXrayTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.startupMu.Lock()
	defer s.startupMu.Unlock()
	releaseMutation, err := s.acquireMutation(ctx)
	if err != nil {
		return err
	}
	defer releaseMutation()

	componentKind, componentPresent, componentErr := componentJournalKind(s.config.JournalPath)
	if componentErr != nil {
		return s.recoveryFailure()
	}
	if componentPresent && componentKind != KindXray && s.stagingPresent() {
		return s.recoveryFailureWith(ErrXrayRecoveryConflict)
	}
	journal, exists, err := s.readJournal()
	if err != nil {
		return s.recoveryFailure()
	}
	restorePresent, restoreErr := componentTransactionPresent(s.config.RestoreJournalPath)
	if restoreErr != nil || restorePresent && exists {
		return s.recoveryFailureWith(ErrXrayRecoveryConflict)
	}
	if !exists {
		if s.stagingPresent() {
			if !s.previousStagingPresent() || s.componentStagingRootPresent() {
				return s.recoveryFailure()
			}
			transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
			defer cancel()
			releaseCoordinator, releaseAuthority, err := s.acquireRecovery(transactionContext)
			if err != nil {
				return s.recoveryFailure()
			}
			defer releaseCoordinator()
			defer releaseAuthority()
			if err := s.reconcilePreJournalStaging(transactionContext); err != nil {
				return s.recoveryFailure()
			}
			if err := s.removeOwnedAndSync(s.stagingPath()); err != nil {
				return s.recoveryFailure()
			}
			s.releaseMaintenance()
		}
		if s.isMaintenance() {
			return s.recoveryFailure()
		}
		s.markReady()
		return nil
	}

	transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	releaseCoordinator, releaseAuthority, err := s.acquireRecovery(transactionContext)
	if err != nil {
		return s.recoveryFailure()
	}
	defer releaseCoordinator()
	defer releaseAuthority()

	previous, err := s.loadJournalPrevious(journal.Previous)
	if err != nil {
		return s.recoveryFailure()
	}
	oldPresent, err := s.pathPresent(s.oldPreviousPath())
	if err != nil {
		return s.recoveryFailure()
	}
	if oldPresent && journal.Operation == xrayOperationRollback {
		old, oldErr := s.loadGeneration(s.oldPreviousPath())
		if oldErr != nil || !sameBinaryMetadata(old.meta, journal.Candidate) {
			return s.recoveryFailure()
		}
	}
	authoritySnapshot, err := s.captureAuthorityHeld(transactionContext)
	if err != nil {
		return s.recoveryFailure()
	}
	current, currentErr := binaryMetadata(s.config.ActiveBinaryPath, "", s.config.CandidateProbe, transactionContext)
	if currentErr != nil {
		// An interrupted rename may leave a regular candidate that cannot be
		// probed until the old generation is restored. The journal-bound hash
		// still lets recovery preserve that displaced candidate safely.
		current, currentErr = binaryMetadataWithoutProbe(s.config.ActiveBinaryPath, journal.Candidate.Version)
		if currentErr == nil && !sameBinaryMetadata(current, journal.Candidate) {
			currentErr = errXrayBinaryInvalid
		}
	}
	if currentErr != nil {
		if _, statErr := os.Lstat(s.config.ActiveBinaryPath); statErr == nil {
			// A present but unrecognizable active file is not assumed to be the
			// interrupted candidate. Recovery may overwrite only an absent
			// active path; unknown drift remains fail-closed.
			return s.recoveryFailure()
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return s.recoveryFailure()
		}
	}
	var displacedPath string
	if currentErr == nil && !sameBinaryMetadata(current, journal.Previous) {
		if !sameBinaryMetadata(current, journal.Candidate) {
			return s.recoveryFailure()
		}
		displacedPath, err = s.stageDisplacedCurrent(current, transactionContext)
		if err != nil {
			return s.recoveryFailure()
		}
	}
	if err := s.restoreRuntime(transactionContext, previous.path, journal.Previous, authoritySnapshot); err != nil {
		return s.recoveryFailure()
	}
	if oldPresent && displacedPath == "" {
		if err := s.restorePreviousAfterPromotionFailure(journal); err != nil {
			return s.recoveryFailure()
		}
	}
	if displacedPath != "" {
		if err := s.savePreviousFromSource(displacedPath, current); err != nil {
			return s.recoveryFailure()
		}
	}
	if err := s.clearJournal(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.removeOwned(s.oldPreviousPath()); err != nil {
		return s.recoveryFailure()
	}
	if displacedPath != "" {
		if err := s.removeOwned(filepath.Dir(displacedPath)); err != nil {
			return s.recoveryFailure()
		}
	}
	if err := s.removeOwned(s.stagingPath()); err != nil {
		return s.recoveryFailure()
	}
	if err := s.removeOwned(s.config.StagingDir); err != nil {
		return s.recoveryFailure()
	}
	s.releaseMaintenance()
	s.markReady()
	return nil
}

func (s *XrayService) reconcilePreJournalStaging(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	staged, err := s.loadGeneration(s.stagingPath())
	if err != nil {
		return errXrayPreviousInvalid
	}
	active, err := binaryMetadataWithoutProbe(s.config.ActiveBinaryPath, staged.meta.Version)
	if err != nil || !sameBinaryMetadata(active, staged.meta) {
		return errXrayGenerationChanged
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (s *XrayService) prepare(ctx context.Context, intended XrayReleaseIdentity) (preparedXray, error) {
	if !validXrayIdentity(intended) {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	base, err := s.captureBase(ctx)
	if err != nil {
		return preparedXray{}, err
	}
	fresh, err := s.config.Resolver.ResolveXray(ctx)
	if err != nil {
		return preparedXray{}, err
	}
	if !sameXrayIdentity(fresh, intended) {
		return preparedXray{}, ErrXrayCandidateStale
	}
	if err := s.checkFreeSpace(intended.SizeBytes); err != nil {
		return preparedXray{}, err
	}
	stageDir, err := s.newStagingDir()
	if err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	cleanup := true
	defer func() {
		if cleanup {
			s.removeOwned(stageDir)
		}
	}()

	archivePath := filepath.Join(stageDir, "candidate.zip")
	archive, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	writer := &xrayArtifactWriter{destination: archive, hash: hashWriter{Hash: sha256.New()}, limit: intended.SizeBytes}
	downloadErr := s.config.Downloader.DownloadXray(ctx, intended, writer)
	syncErr := archive.Sync()
	closeErr := archive.Close()
	if downloadErr != nil || syncErr != nil || closeErr != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	if writer.count != intended.SizeBytes {
		return preparedXray{}, ErrXrayArtifactRejected
	}
	if !strings.EqualFold(hex.EncodeToString(writer.hash.Sum(nil)), intended.SHA256) {
		return preparedXray{}, ErrXrayArtifactRejected
	}

	candidatePath := filepath.Join(stageDir, "candidate-xray")
	if err := extractXrayBinary(ctx, archivePath, candidatePath); err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	if err := s.validateCandidateBinary(ctx, candidatePath, intended); err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	files, err := appliance.RenderCandidateFiles(base.authority.Appliance, base.authority.Registry)
	if err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	configDir := filepath.Join(stageDir, "config")
	if err := writeXrayCandidateTree(configDir, files, s.config.SyncDirectory); err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	if err := s.config.CandidateValidator.ValidateXrayCandidate(ctx, candidatePath, filepath.Join(configDir, "xray"), s.config.AssetDir); err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	candidateMeta, err := binaryMetadata(candidatePath, intended.Version, s.config.CandidateProbe, ctx)
	if err != nil {
		return preparedXray{}, ErrXrayCandidateRejected
	}
	cleanup = false
	return preparedXray{identity: intended, base: base, stageDir: stageDir, candidatePath: candidatePath, candidateMeta: candidateMeta}, nil
}

func (s *XrayService) applyPrepared(ctx context.Context, prepared preparedXray) error {
	releaseCoordinator, releaseAuthority, err := s.acquireApply(ctx)
	if err != nil {
		return err
	}
	defer releaseCoordinator()
	defer releaseAuthority()

	current, err := s.captureHeld(ctx)
	if err != nil {
		return err
	}
	if !sameXrayBase(current, prepared.base) {
		return ErrXrayCandidateStale
	}
	return s.runCommitted(ctx, xrayOperationUpdate, current, prepared.candidatePath, prepared.candidateMeta, true)
}

func (s *XrayService) captureBase(ctx context.Context) (xrayBaseSnapshot, error) {
	release, err := s.acquireAuthority(ctx, false)
	if err != nil {
		return xrayBaseSnapshot{}, err
	}
	defer release()
	return s.captureHeld(ctx)
}

func (s *XrayService) captureHeld(ctx context.Context) (xrayBaseSnapshot, error) {
	authoritySnapshot, err := s.captureAuthorityHeld(ctx)
	if err != nil {
		return xrayBaseSnapshot{}, err
	}
	active, err := binaryMetadata(s.config.ActiveBinaryPath, "", s.config.CandidateProbe, ctx)
	if err != nil {
		return xrayBaseSnapshot{}, ErrXrayAuthorityUnavailable
	}
	return xrayBaseSnapshot{authority: authoritySnapshot, active: active}, nil
}

func (s *XrayService) captureAuthorityHeld(ctx context.Context) (XrayAuthoritySnapshot, error) {
	if s.config.Authority == nil {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	authoritySnapshot, err := s.config.Authority.SnapshotUnderLease(ctx)
	if err != nil || !validAuthoritySnapshot(authoritySnapshot) {
		return XrayAuthoritySnapshot{}, ErrXrayAuthorityUnavailable
	}
	return authoritySnapshot, nil
}

func validAuthoritySnapshot(value XrayAuthoritySnapshot) bool {
	return value.Appliance.Validate() == nil && value.Registry.Validate() == nil && value.Generation != [sha256.Size]byte{}
}

func sameXrayBase(left, right xrayBaseSnapshot) bool {
	return left.authority.Generation == right.authority.Generation && sameBinaryMetadata(left.active, right.active)
}

func sameXrayIdentity(left, right XrayReleaseIdentity) bool {
	return left.Tag == right.Tag && left.Version == right.Version && left.AssetName == right.AssetName && left.SizeBytes == right.SizeBytes && strings.EqualFold(left.SHA256, right.SHA256)
}

func validXrayIdentity(value XrayReleaseIdentity) bool {
	version, ok := parseStrictVersion(value.Version)
	if !ok || version.String() != value.Version || value.Tag == "" || value.AssetName != xrayCandidateAsset || value.SizeBytes <= 0 || value.SizeBytes > MaxCandidateAssetBytes || !isHexSHA256(value.SHA256) {
		return false
	}
	tagVersion, ok := parseStrictVersion(value.Tag)
	return ok && tagVersion == version
}

func (s *XrayService) validateCandidateBinary(ctx context.Context, path string, intended XrayReleaseIdentity) error {
	result := s.config.CandidateProbe.ProbeXrayCandidate(ctx, path)
	if err := validateXrayProbeResult(ctx, result, intended.Version); err != nil {
		return err
	}
	return nil
}

func validateXrayProbeResult(ctx context.Context, result XrayVersionResult, expectedVersion string) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if len(result.Stdout)+len(result.Stderr) > MaxXrayProbeOutput || result.Err != nil || result.ExitCode != 0 {
		return errXrayBinaryInvalid
	}
	signal, err := ParseXrayVersionOutput(result.Stdout, result.Stderr)
	if err != nil || signal.Architecture != "arm64" || expectedVersion != "" && signal.Version != expectedVersion {
		return errXrayBinaryInvalid
	}
	return nil
}

func expectedOutboundTags(registry nodes.Registry) []string {
	result := make([]string, 0, len(registry.Nodes))
	for _, node := range registry.SortedNodes() {
		if node.Enabled {
			result = append(result, node.OutboundTag)
		}
	}
	return result
}

func (s *XrayService) validateLocalCandidate(ctx context.Context, candidatePath string, authoritySnapshot XrayAuthoritySnapshot) error {
	files, err := appliance.RenderCandidateFiles(authoritySnapshot.Appliance, authoritySnapshot.Registry)
	if err != nil {
		return err
	}
	stage, err := s.newStagingDir()
	if err != nil {
		return err
	}
	defer s.removeOwned(stage)
	configDir := filepath.Join(stage, "config")
	if err := writeXrayCandidateTree(configDir, files, s.config.SyncDirectory); err != nil {
		return err
	}
	return s.config.CandidateValidator.ValidateXrayCandidate(ctx, candidatePath, filepath.Join(configDir, "xray"), s.config.AssetDir)
}

func (s *XrayService) verifyRuntime(ctx context.Context, expected xrayBinaryMetadata, authoritySnapshot XrayAuthoritySnapshot) error {
	active, err := binaryMetadata(s.config.ActiveBinaryPath, expected.Version, s.config.CandidateProbe, ctx)
	if err != nil || !sameBinaryMetadata(active, expected) {
		return errXrayBinaryInvalid
	}
	if err := s.config.Runtime.ValidateActiveConfig(ctx); err != nil {
		return err
	}
	if err := s.config.Runtime.Restart(ctx); err != nil {
		return err
	}
	if err := s.config.Runtime.WaitReady(ctx); err != nil {
		return err
	}
	if err := s.config.Runtime.Verify(ctx, expectedOutboundTags(authoritySnapshot.Registry)); err != nil {
		return err
	}
	currentAuthority, err := s.config.Authority.SnapshotUnderLease(ctx)
	if err != nil || !validAuthoritySnapshot(currentAuthority) || currentAuthority.Generation != authoritySnapshot.Generation {
		return errXrayGenerationChanged
	}
	return nil
}

func (s *XrayService) restoreRuntime(ctx context.Context, source string, expected xrayBinaryMetadata, authoritySnapshot XrayAuthoritySnapshot) error {
	if err := s.activateBinary(ctx, source, expected); err != nil {
		return err
	}
	return s.verifyRuntime(ctx, expected, authoritySnapshot)
}

func (s *XrayService) runCommitted(ctx context.Context, operation string, base xrayBaseSnapshot, candidatePath string, candidate xrayBinaryMetadata, candidateValidated bool) error {
	if s.config.Runtime == nil || s.config.CandidateValidator == nil || s.config.CandidateProbe == nil {
		return ErrXrayTransactionUnavailable
	}
	if !validXrayBinaryMetadata(candidate, true) {
		return ErrXrayCandidateRejected
	}
	if !candidateValidated {
		if err := s.validateLocalCandidate(ctx, candidatePath, base.authority); err != nil {
			return ErrXrayCandidateRejected
		}
	}
	stagePath, err := s.savePreviousGeneration(base.active)
	if err != nil {
		return ErrXrayApplyFailed
	}
	// A process loss in this window leaves only the product-owned staging
	// generation and no durable intent. Startup recovery proves that the
	// active bytes still equal the staged pre-operation generation, then
	// discards the abandoned staging directory.
	if err := s.inject(XrayStagePreviousStaging); err != nil {
		s.failClosed()
		s.markNotReady(ErrXrayRecoveryRequired)
		return ErrXrayApplyFailed
	}
	journal := xrayTransactionJournal{
		SchemaVersion: XrayTransactionSchemaVersion,
		Component:     string(KindXray),
		Operation:     operation,
		Phase:         xrayPhasePrepared,
		Previous:      base.active,
		Candidate:     candidate,
	}
	if err := s.writeJournal(journal); err != nil {
		present, presentErr := componentTransactionPresent(s.config.JournalPath)
		if presentErr != nil {
			return s.recoveryFailure()
		}
		if present {
			if clearErr := s.clearJournal(); clearErr != nil {
				return s.recoveryFailure()
			}
		}
		if cleanupErr := s.removeOwned(s.stagingPath()); cleanupErr != nil {
			return s.recoveryFailure()
		}
		return ErrXrayApplyFailed
	}
	if err := s.inject(XrayStagePreviousSaved); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(XrayStageJournalPrepared); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}

	if err := s.activateBinary(ctx, candidatePath, candidate); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	journal.Phase = xrayPhaseBinaryCommitted
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(XrayStageBinaryCommitted); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}

	activationContext, cancel := context.WithTimeout(ctx, s.config.ActivationTimeout)
	verifyErr := s.verifyRuntime(activationContext, candidate, base.authority)
	cancel()
	if verifyErr != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, verifyErr)
	}
	if err := s.promotePreviousGeneration(); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(XrayStagePreviousSettled); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	journal.Phase = xrayPhaseRuntimeVerified
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.settlePreviousGeneration(); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(XrayStageRuntimeVerified); err != nil {
		return s.failClosedResult()
	}
	if err := s.inject(XrayStageJournalCleared); err != nil {
		return s.failClosedResult()
	}
	if err := s.clearJournal(); err != nil {
		return s.failClosedResult()
	}
	return nil
}

func (s *XrayService) failAndRecover(ctx context.Context, journal xrayTransactionJournal, base xrayBaseSnapshot, stagePath, candidatePath string, _ error) error {
	promotedPrevious, err := s.previousPromotionPresent(journal)
	if err != nil {
		return s.recoveryFailure()
	}
	rollbackContext, cancel := context.WithTimeout(context.Background(), s.config.RollbackTimeout)
	defer cancel()
	var displacedRollbackPath string
	if promotedPrevious && journal.Operation == xrayOperationRollback {
		current, currentErr := binaryMetadataWithoutProbe(s.config.ActiveBinaryPath, journal.Candidate.Version)
		if currentErr != nil || !sameBinaryMetadata(current, journal.Candidate) {
			return s.recoveryFailure()
		}
		displacedRollbackPath, err = s.stageDisplacedCurrent(current, rollbackContext)
		if err != nil {
			return s.recoveryFailure()
		}
	}
	previousPath := stagePath
	if !ownedRegularPath(previousPath) {
		previousPath = filepath.Join(s.config.PreviousDir, xrayPreviousBinaryName)
	}
	err = s.restoreRuntime(rollbackContext, previousPath, journal.Previous, base.authority)
	if err != nil {
		return s.recoveryFailure()
	}
	if promotedPrevious {
		oldPresent, presentErr := s.pathPresent(s.oldPreviousPath())
		if presentErr != nil {
			return s.recoveryFailure()
		}
		if oldPresent {
			if err := s.restorePreviousAfterPromotionFailure(journal); err != nil {
				return s.recoveryFailure()
			}
		} else {
			// If .old was already settled before its durability step failed,
			// the active candidate backup is the only remaining rollback
			// target. Preserve it and leave the pre-operation generation in
			// staging so a journal-clear failure remains recoverable.
			if journal.Operation != xrayOperationRollback || displacedRollbackPath == "" {
				return s.recoveryFailure()
			}
			if err := s.savePreviousFromSource(displacedRollbackPath, journal.Candidate); err != nil {
				return s.recoveryFailure()
			}
			if _, err := s.savePreviousGeneration(base.active); err != nil {
				return s.recoveryFailure()
			}
			if err := s.settlePreviousGeneration(); err != nil {
				return s.recoveryFailure()
			}
		}
		if displacedRollbackPath != "" {
			if err := s.removeOwned(filepath.Dir(displacedRollbackPath)); err != nil {
				return s.recoveryFailure()
			}
		}
	}
	if err := s.clearJournal(); err != nil {
		return s.recoveryFailure()
	}
	// Keep the old settled generation when activation did not reach the
	// promotion point. A terminal cleanup never removes the fixed product
	// parent, only the transaction-local staging material.
	if !promotedPrevious && !ownedRegularPath(filepath.Join(s.config.PreviousDir, xrayPreviousBinaryName)) && ownedRegularPath(candidatePath) {
		if err := s.saveDisplacedGeneration(candidatePath, journal.Candidate.Version); err != nil {
			return s.recoveryFailure()
		}
	}
	if err := s.removeOwned(s.stagingPath()); err != nil {
		return s.recoveryFailure()
	}
	if journal.Operation == xrayOperationRollback {
		return ErrXrayRollbackFailed
	}
	return ErrXrayApplyFailed
}

func (s *XrayService) failClosedResult() error {
	s.failClosed()
	return ErrXrayRecoveryFailed
}

func (s *XrayService) recoveryFailure() error {
	return s.recoveryFailureWith(ErrXrayRecoveryFailed)
}

func (s *XrayService) recoveryFailureWith(err error) error {
	s.failClosed()
	s.markNotReady(err)
	return err
}

func (s *XrayService) acquireMutation(ctx context.Context) (func(), error) {
	if s == nil {
		return nil, ErrXrayTransactionUnavailable
	}
	release, err := s.mutationGate.Acquire(ctx)
	if err != nil {
		return nil, ErrXrayBusy
	}
	return release, nil
}

func (s *XrayService) acquireApply(ctx context.Context) (func(), func(), error) {
	admission, cancel := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancel()
	releaseCoordinator, err := s.beginCoordinator(admission, false)
	if err != nil {
		return nil, nil, ErrXrayAuthorityBusy
	}
	releaseAuthority, err := s.acquireAuthority(admission, false)
	if err != nil {
		releaseCoordinator()
		return nil, nil, ErrXrayAuthorityBusy
	}
	return releaseCoordinator, releaseAuthority, nil
}

func (s *XrayService) acquireRecovery(ctx context.Context) (func(), func(), error) {
	admission, cancel := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancel()
	releaseCoordinator, err := s.beginCoordinator(admission, true)
	if err != nil {
		return nil, nil, ErrXrayRecoveryFailed
	}
	releaseAuthority, err := s.acquireAuthority(admission, true)
	if err != nil {
		releaseCoordinator()
		return nil, nil, ErrXrayRecoveryFailed
	}
	return releaseCoordinator, releaseAuthority, nil
}

func (s *XrayService) beginCoordinator(ctx context.Context, recovery bool) (func(), error) {
	if s.config.Coordinator == nil {
		return func() {}, nil
	}
	if recovery {
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

func (s *XrayService) acquireAuthority(ctx context.Context, recovery bool) (func(), error) {
	if s.config.AuthorityLease == nil {
		return func() {}, nil
	}
	var (
		release func()
		err     error
	)
	if recovery {
		release, err = s.config.AuthorityLease.AcquireForRecovery(ctx, s.config.AuthorityWaitTimeout)
	} else {
		release, err = s.config.AuthorityLease.Acquire(ctx, s.config.AuthorityWaitTimeout)
	}
	if err != nil {
		return nil, ErrXrayAuthorityBusy
	}
	if release == nil {
		return func() {}, nil
	}
	return release, nil
}

func (s *XrayService) inject(stage XrayStage) error {
	if s.config.InjectFailure == nil {
		return nil
	}
	return s.config.InjectFailure(stage)
}

func (s *XrayService) enterMaintenance() {
	s.mu.Lock()
	if s.maintenance {
		s.ready = false
		s.mu.Unlock()
		return
	}
	s.maintenance = true
	s.ready = false
	if s.readyErr == nil {
		s.readyErr = ErrXrayRecoveryFailed
	}
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Enter(KindXray)
		return
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Block()
	}
	if gate, ok := s.config.Coordinator.(XrayMaintenanceGate); ok {
		gate.EnterMaintenance()
	}
}

func (s *XrayService) releaseMaintenance() {
	s.mu.Lock()
	if !s.maintenance {
		s.mu.Unlock()
		return
	}
	s.maintenance = false
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Exit(KindXray)
		return
	}
	if gate, ok := s.config.Coordinator.(XrayMaintenanceGate); ok {
		gate.ExitMaintenance()
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Unblock()
	}
}

func (s *XrayService) failClosed() {
	s.enterMaintenance()
}

func (s *XrayService) isMaintenance() bool {
	s.mu.Lock()
	value := s.maintenance
	s.mu.Unlock()
	return value
}

func (s *XrayService) markReady() {
	s.mu.Lock()
	s.ready = true
	s.readyErr = nil
	s.mu.Unlock()
}

func (s *XrayService) markNotReady(err error) {
	s.mu.Lock()
	s.ready = false
	s.readyErr = err
	s.mu.Unlock()
}

func (s *XrayService) recoveryJournalExists() bool {
	kind, present, _ := componentJournalKind(s.config.JournalPath)
	return present && kind == KindXray
}

type xrayBinaryMetadata struct {
	Exists  bool   `json:"exists"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
}

type xrayTransactionJournal struct {
	SchemaVersion int                `json:"schemaVersion"`
	Component     string             `json:"component"`
	Operation     string             `json:"operation"`
	Phase         string             `json:"phase"`
	Previous      xrayBinaryMetadata `json:"previous"`
	Candidate     xrayBinaryMetadata `json:"candidate"`
}

type xrayPreviousRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode"`
}

type loadedXrayGeneration struct {
	path string
	meta xrayBinaryMetadata
}

func validXrayBinaryMetadata(value xrayBinaryMetadata, requireVersion bool) bool {
	if !value.Exists || value.Size <= 0 || value.Size > MaxXrayCandidateBinaryBytes || !isHexSHA256(value.SHA256) || value.Mode&0o111 == 0 && runtime.GOOS != "windows" {
		return false
	}
	if requireVersion {
		version, ok := parseStrictVersion(value.Version)
		if !ok || version.String() != value.Version {
			return false
		}
	}
	return true
}

func sameBinaryMetadata(left, right xrayBinaryMetadata) bool {
	return left.Exists == right.Exists && left.Version == right.Version && strings.EqualFold(left.SHA256, right.SHA256) && left.Size == right.Size && left.Mode == right.Mode
}

func binaryMetadata(path, expectedVersion string, probe XrayCandidateProbe, ctx context.Context) (xrayBinaryMetadata, error) {
	if path == "" || probe == nil {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !executableMode(info.Mode()) || info.Size() <= 0 || info.Size() > MaxXrayCandidateBinaryBytes {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	hash := sha256.New()
	count, copyErr := copyXrayBytes(ctx, hash, file, MaxXrayCandidateBinaryBytes)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || count != info.Size() {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	opened, err := os.Lstat(path)
	if err != nil || opened.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, opened) || opened.Size() != count {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	result := probe.ProbeXrayCandidate(ctx, path)
	if err := validateXrayProbeResult(ctx, result, expectedVersion); err != nil {
		return xrayBinaryMetadata{}, err
	}
	signal, err := ParseXrayVersionOutput(result.Stdout, result.Stderr)
	if err != nil {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	return xrayBinaryMetadata{Exists: true, Version: signal.Version, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: count, Mode: uint32(info.Mode().Perm())}, nil
}

func copyXrayBytes(ctx context.Context, destination io.Writer, source io.Reader, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, errXrayBinaryInvalid
	}
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return total, err
			}
		}
		read, err := source.Read(buffer)
		if read > 0 {
			if total+int64(read) > limit {
				return total, errXrayArchiveTooLarge
			}
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, err
		}
	}
}

type xrayArtifactWriter struct {
	destination io.Writer
	hash        hashWriter
	limit       int64
	count       int64
}

type hashWriter struct{ hash.Hash }

func (w *hashWriter) Write(value []byte) (int, error) {
	return w.Hash.Write(value)
}

func (w *xrayArtifactWriter) Write(value []byte) (int, error) {
	if w == nil || w.destination == nil || w.hash.Hash == nil || w.limit <= 0 {
		return 0, errXrayArtifactRejected
	}
	if w.count+int64(len(value)) > w.limit {
		return 0, errXrayArtifactTooLarge
	}
	written, err := w.destination.Write(value)
	if err != nil || written != len(value) {
		return written, err
	}
	if _, err := w.hash.Write(value[:written]); err != nil {
		return written, err
	}
	w.count += int64(written)
	return written, nil
}

func extractXrayBinary(ctx context.Context, archivePath, destinationPath string) error {
	info, err := os.Stat(archivePath)
	if err != nil || info.Size() <= 0 || info.Size() > MaxCandidateAssetBytes {
		return errXrayArchiveRejected
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return errXrayArchiveRejected
	}
	defer archive.Close()
	reader, err := zip.NewReader(archive, info.Size())
	if err != nil || len(reader.File) == 0 || len(reader.File) > MaxXrayArchiveEntries {
		return errXrayArchiveRejected
	}
	var selected *zip.File
	var aggregate uint64
	seen := make(map[string]struct{}, len(reader.File))
	for _, entry := range reader.File {
		if entry == nil || entry.Name == "" || strings.Contains(entry.Name, "\\") || strings.HasPrefix(entry.Name, "/") || strings.Contains(entry.Name, ":") || !safeArchiveName(entry.Name) {
			return errXrayArchiveRejected
		}
		if _, exists := seen[entry.Name]; exists {
			return errXrayArchiveRejected
		}
		seen[entry.Name] = struct{}{}
		if entry.UncompressedSize64 > MaxXrayArchiveEntryBytes || aggregate > MaxXrayArchiveUncompressedSize-entry.UncompressedSize64 {
			return errXrayArchiveTooLarge
		}
		aggregate += entry.UncompressedSize64
		mode := entry.Mode()
		if !mode.IsRegular() || mode&os.ModeSymlink != 0 {
			return errXrayArchiveRejected
		}
		if entry.Name == xrayExpectedArchiveMember {
			if selected != nil {
				return errXrayArchiveRejected
			}
			selected = entry
		}
	}
	if selected == nil || selected.UncompressedSize64 == 0 || selected.UncompressedSize64 > MaxXrayCandidateBinaryBytes {
		return errXrayArchiveRejected
	}
	input, err := selected.Open()
	if err != nil {
		return errXrayArchiveRejected
	}
	defer input.Close()
	parent := filepath.Dir(destinationPath)
	if err := ensurePrivateDirectory(parent); err != nil {
		return errXrayArchiveRejected
	}
	output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return errXrayArchiveRejected
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(destinationPath)
		}
	}()
	count, err := copyXrayBytes(ctx, output, input, MaxXrayCandidateBinaryBytes)
	if err == nil && count != int64(selected.UncompressedSize64) {
		err = errXrayArchiveRejected
	}
	if syncErr := output.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errXrayArchiveRejected
	}
	if err := os.Chmod(destinationPath, 0o700); err != nil {
		return errXrayArchiveRejected
	}
	remove = false
	return nil
}

func safeArchiveName(value string) bool {
	if value == "." || value == ".." || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (s *XrayService) newStagingDir() (string, error) {
	if err := ensurePrivateDirectory(s.config.StagingDir); err != nil {
		return "", err
	}
	return os.MkdirTemp(s.config.StagingDir, ".xray-transaction-")
}

func (s *XrayService) stagingPath() string {
	return s.config.PreviousDir + xrayPreviousStagingSuffix
}

func (s *XrayService) oldPreviousPath() string {
	return s.config.PreviousDir + xrayPreviousOldSuffix
}

func (s *XrayService) savePreviousGeneration(meta xrayBinaryMetadata) (string, error) {
	if !validXrayBinaryMetadata(meta, true) {
		return "", errXrayPreviousInvalid
	}
	parent := filepath.Dir(s.config.PreviousDir)
	if err := ensurePrivateDirectory(parent); err != nil {
		return "", err
	}
	staging := s.stagingPath()
	if err := s.removeOwned(staging); err != nil {
		return "", err
	}
	if err := ensurePrivateDirectory(staging); err != nil {
		return "", err
	}
	if err := copyXrayFile(meta, s.config.ActiveBinaryPath, filepath.Join(staging, xrayPreviousBinaryName), s.config.SyncDirectory); err != nil {
		s.removeOwned(staging)
		return "", err
	}
	if err := writeXrayPreviousMetadata(filepath.Join(staging, xrayPreviousMetadataName), meta, s.config.SyncDirectory); err != nil {
		s.removeOwned(staging)
		return "", err
	}
	return filepath.Join(staging, xrayPreviousBinaryName), nil
}

func (s *XrayService) promotePreviousGeneration() error {
	staging := s.stagingPath()
	if err := verifyPreviousXrayDirectory(staging, xrayBinaryMetadata{}); err != nil {
		return err
	}
	parent := filepath.Dir(s.config.PreviousDir)
	old := s.oldPreviousPath()
	if err := s.removeOwned(old); err != nil {
		return err
	}
	if info, err := os.Lstat(s.config.PreviousDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errXrayPreviousInvalid
		}
		if err := verifyPreviousXrayDirectory(s.config.PreviousDir, xrayBinaryMetadata{}); err != nil {
			return err
		}
		if err := os.Rename(s.config.PreviousDir, old); err != nil {
			return err
		}
		if err := s.config.SyncDirectory(parent); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, s.config.PreviousDir); err != nil {
		if _, oldErr := os.Lstat(old); oldErr == nil {
			_ = os.Rename(old, s.config.PreviousDir)
		}
		return err
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		return err
	}
	// Keep the displaced previous generation until the runtime-verified
	// journal state is durable. A failed rollback must be able to restore the
	// exact one-step rollback target after this promotion.
	return nil
}

func (s *XrayService) previousPromotionPresent(journal xrayTransactionJournal) (bool, error) {
	if present, err := s.pathPresent(s.oldPreviousPath()); err != nil {
		return false, err
	} else if present {
		return true, nil
	}
	if present, err := s.pathPresent(s.stagingPath()); err != nil {
		return false, err
	} else if present {
		return false, nil
	}
	previous, err := s.loadGeneration(s.config.PreviousDir)
	if err != nil {
		return false, nil
	}
	// An update with no settled previous generation legitimately has only the
	// newly promoted PreviousDir after promotion. The old-less fallback is
	// needed only for rollback, where the target must still be preserved if
	// settlement already removed .old.
	return journal.Operation == xrayOperationRollback && sameBinaryMetadata(previous.meta, journal.Previous), nil
}

func (s *XrayService) restorePreviousAfterPromotionFailure(journal xrayTransactionJournal) error {
	oldPath := s.oldPreviousPath()
	old, err := s.loadGeneration(oldPath)
	if err != nil {
		return errXrayPreviousInvalid
	}
	if journal.Operation == xrayOperationRollback && !sameBinaryMetadata(old.meta, journal.Candidate) {
		return errXrayPreviousInvalid
	}

	parent := filepath.Dir(s.config.PreviousDir)
	previousInfo, err := os.Lstat(s.config.PreviousDir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(oldPath, s.config.PreviousDir); err != nil {
			return err
		}
		return s.config.SyncDirectory(parent)
	}
	if err != nil || previousInfo.Mode()&os.ModeSymlink != 0 || !previousInfo.IsDir() {
		return errXrayPreviousInvalid
	}
	if err := verifyPreviousXrayDirectory(s.config.PreviousDir, xrayBinaryMetadata{}); err != nil {
		return err
	}
	if present, err := s.pathPresent(s.stagingPath()); err != nil {
		return err
	} else if present {
		return errXrayPreviousInvalid
	}
	if err := os.Rename(s.config.PreviousDir, s.stagingPath()); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		_ = os.Rename(s.stagingPath(), s.config.PreviousDir)
		_ = s.config.SyncDirectory(parent)
		return err
	}
	if err := os.Rename(oldPath, s.config.PreviousDir); err != nil {
		_ = os.Rename(s.stagingPath(), s.config.PreviousDir)
		_ = s.config.SyncDirectory(parent)
		return err
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		return err
	}
	return nil
}

func (s *XrayService) settlePreviousGeneration() error {
	if err := s.removeOwned(s.oldPreviousPath()); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(s.config.PreviousDir))
}

func (s *XrayService) loadPreviousGeneration() (loadedXrayGeneration, error) {
	return s.loadGeneration(s.config.PreviousDir)
}

func (s *XrayService) loadJournalPrevious(expected xrayBinaryMetadata) (loadedXrayGeneration, error) {
	for _, directory := range []string{s.stagingPath(), s.config.PreviousDir} {
		generation, err := s.loadGeneration(directory)
		if err == nil && sameBinaryMetadata(generation.meta, expected) {
			return generation, nil
		}
	}
	return loadedXrayGeneration{}, errXrayPreviousInvalid
}

func (s *XrayService) loadGeneration(directory string) (loadedXrayGeneration, error) {
	if err := verifyPreviousXrayDirectory(directory, xrayBinaryMetadata{}); err != nil {
		return loadedXrayGeneration{}, errXrayPreviousInvalid
	}
	metadataPath := filepath.Join(directory, xrayPreviousMetadataName)
	contents, err := readPrivateComponentFile(metadataPath, MaxPreviousGenerationMetadata)
	if err != nil {
		return loadedXrayGeneration{}, errXrayPreviousInvalid
	}
	var record xrayPreviousRecord
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || record.SchemaVersion != XrayTransactionSchemaVersion || !validXrayBinaryMetadata(xrayBinaryMetadata{Exists: true, Version: record.Version, SHA256: record.SHA256, Size: record.Size, Mode: record.Mode}, true) {
		return loadedXrayGeneration{}, errXrayPreviousInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return loadedXrayGeneration{}, errXrayPreviousInvalid
	}
	binaryPath := filepath.Join(directory, xrayPreviousBinaryName)
	meta, err := binaryMetadataWithoutProbe(binaryPath, record.Version)
	if err != nil || meta.SHA256 != record.SHA256 || meta.Size != record.Size || meta.Mode != record.Mode {
		return loadedXrayGeneration{}, errXrayPreviousInvalid
	}
	return loadedXrayGeneration{path: binaryPath, meta: meta}, nil
}

func verifyPreviousXrayDirectory(directory string, expected xrayBinaryMetadata) error {
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		return errXrayPreviousInvalid
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		return errXrayPreviousInvalid
	}
	for _, entry := range entries {
		if entry.Name() != xrayPreviousBinaryName && entry.Name() != xrayPreviousMetadataName {
			return errXrayPreviousInvalid
		}
		if entry.IsDir() {
			return errXrayPreviousInvalid
		}
	}
	contents, err := readPrivateComponentFile(filepath.Join(directory, xrayPreviousMetadataName), MaxPreviousGenerationMetadata)
	if err != nil {
		return errXrayPreviousInvalid
	}
	var record xrayPreviousRecord
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || record.SchemaVersion != XrayTransactionSchemaVersion {
		return errXrayPreviousInvalid
	}
	meta := xrayBinaryMetadata{Exists: true, Version: record.Version, SHA256: record.SHA256, Size: record.Size, Mode: record.Mode}
	if !validXrayBinaryMetadata(meta, true) {
		return errXrayPreviousInvalid
	}
	if expected.Exists && !sameBinaryMetadata(meta, expected) {
		return errXrayPreviousInvalid
	}
	actual, err := binaryMetadataWithoutProbe(filepath.Join(directory, xrayPreviousBinaryName), record.Version)
	if err != nil || !sameBinaryMetadata(actual, meta) {
		return errXrayPreviousInvalid
	}
	return nil
}

func binaryMetadataWithoutProbe(path, expectedVersion string) (xrayBinaryMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !executableMode(info.Mode()) || info.Size() <= 0 || info.Size() > MaxXrayCandidateBinaryBytes {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	hash := sha256.New()
	count, copyErr := copyXrayBytes(context.Background(), hash, file, MaxXrayCandidateBinaryBytes)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || count != info.Size() {
		return xrayBinaryMetadata{}, errXrayBinaryInvalid
	}
	if expectedVersion != "" {
		version, ok := parseStrictVersion(expectedVersion)
		if !ok || version.String() != expectedVersion {
			return xrayBinaryMetadata{}, errXrayBinaryInvalid
		}
	}
	return xrayBinaryMetadata{Exists: true, Version: expectedVersion, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: count, Mode: uint32(info.Mode().Perm())}, nil
}

func writeXrayPreviousMetadata(path string, meta xrayBinaryMetadata, syncDirectory func(string) error) error {
	contents, err := json.Marshal(xrayPreviousRecord{SchemaVersion: XrayTransactionSchemaVersion, Version: meta.Version, SHA256: meta.SHA256, Size: meta.Size, Mode: meta.Mode})
	if err != nil {
		return errXrayPreviousInvalid
	}
	contents = append(contents, '\n')
	return writeAtomicComponentFile(path, contents, 0o600, syncDirectory)
}

func copyXrayFile(meta xrayBinaryMetadata, source, destination string, syncDirectory func(string) error) error {
	if !validXrayBinaryMetadata(meta, true) {
		return errXrayBinaryInvalid
	}
	before, err := os.Lstat(source)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() != meta.Size {
		return errXrayBinaryInvalid
	}
	input, err := os.Open(source)
	if err != nil {
		return errXrayBinaryInvalid
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || opened.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || opened.Size() != meta.Size || !os.SameFile(before, opened) {
		return errXrayBinaryInvalid
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(meta.Mode))
	if err != nil {
		return errXrayBinaryInvalid
	}
	hash := sha256.New()
	writer := io.MultiWriter(output, hash)
	count, copyErr := copyXrayBytes(context.Background(), writer, input, MaxXrayCandidateBinaryBytes)
	syncErr := output.Sync()
	closeErr := output.Close()
	after, afterErr := os.Lstat(source)
	if copyErr != nil || syncErr != nil || closeErr != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || after.Size() != count || count != meta.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), meta.SHA256) {
		_ = os.Remove(destination)
		return errXrayBinaryInvalid
	}
	if err := os.Chmod(destination, os.FileMode(meta.Mode)); err != nil {
		_ = os.Remove(destination)
		return errXrayBinaryInvalid
	}
	if syncDirectory != nil {
		if err := syncDirectory(filepath.Dir(destination)); err != nil {
			return err
		}
	}
	return nil
}

func (s *XrayService) activateBinary(ctx context.Context, source string, expected xrayBinaryMetadata) error {
	if source == "" || !validXrayBinaryMetadata(expected, true) {
		return errXrayBinaryInvalid
	}
	sourceMeta, err := binaryMetadataWithoutProbe(source, expected.Version)
	if err != nil || !sameBinaryMetadata(sourceMeta, expected) {
		return errXrayBinaryInvalid
	}
	parent := filepath.Dir(s.config.ActiveBinaryPath)
	if err := verifyExistingOrMissingDirectory(parent); err != nil {
		return errXrayBinaryInvalid
	}
	if info, err := os.Lstat(s.config.ActiveBinaryPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errXrayBinaryInvalid
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXrayBinaryInvalid
	}
	// The temporary file is deliberately created in the active directory so
	// the final rename is same-filesystem and atomic.
	temporary, err := os.CreateTemp(parent, ".xkeen-xray-")
	if err != nil {
		return errXrayBinaryInvalid
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(os.FileMode(expected.Mode)); err != nil {
		_ = temporary.Close()
		return errXrayBinaryInvalid
	}
	input, err := os.Open(source)
	if err != nil {
		_ = temporary.Close()
		return errXrayBinaryInvalid
	}
	digest := sha256.New()
	count, copyErr := copyXrayBytes(ctx, io.MultiWriter(temporary, digest), input, MaxXrayCandidateBinaryBytes)
	closeInputErr := input.Close()
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil || closeInputErr != nil || syncErr != nil || closeErr != nil || count != expected.Size || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), expected.SHA256) {
		return errXrayBinaryInvalid
	}
	if err := os.Rename(temporaryPath, s.config.ActiveBinaryPath); err != nil {
		return errXrayBinaryInvalid
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		return errXrayBinaryInvalid
	}
	return nil
}

func verifyExistingOrMissingDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errXrayBinaryInvalid
	}
	return nil
}

func (s *XrayService) checkFreeSpace(assetSize int64) error {
	if assetSize <= 0 {
		return errXrayFreeSpaceInsufficient
	}
	need := uint64(assetSize)
	need += uint64(MaxXrayCandidateBinaryBytes) * 2
	need += uint64(XrayFreeSpaceReserve)
	paths := []string{
		existingDirectory(filepath.Dir(s.config.StagingDir)),
		existingDirectory(filepath.Dir(s.config.ActiveBinaryPath)),
		existingDirectory(filepath.Dir(s.config.PreviousDir)),
	}
	seen := make(map[string]struct{}, len(paths))
	for _, target := range paths {
		if target == "" {
			return errXrayFreeSpaceUnavailable
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		available, err := s.config.AvailableSpace(target)
		if err != nil {
			return errXrayFreeSpaceUnavailable
		}
		if available < need {
			return errXrayFreeSpaceInsufficient
		}
	}
	return nil
}

func existingDirectory(path string) string {
	path = filepath.Clean(path)
	for path != "" && path != "." {
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	return ""
}

func ensurePrivateDirectory(path string) error {
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

func readPrivateComponentFile(path string, limit int) ([]byte, error) {
	if path == "" || limit <= 0 {
		return nil, errXrayJournalInvalid
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > int64(limit) {
		return nil, errXrayJournalInvalid
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return nil, errXrayJournalInvalid
	}
	if err := checkPrivateComponentDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, opened) {
		return nil, errXrayJournalInvalid
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(contents) > limit {
		return nil, errXrayJournalInvalid
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || after.Size() != int64(len(contents)) {
		return nil, errXrayJournalInvalid
	}
	return contents, nil
}

func checkPrivateComponentDirectory(path string) error {
	if path == "" {
		return errXrayJournalInvalid
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errXrayJournalInvalid
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errXrayJournalInvalid
	}
	return nil
}

func writeAtomicComponentFile(path string, contents []byte, mode os.FileMode, syncDir func(string) error) error {
	if path == "" {
		return errXrayJournalInvalid
	}
	parent := filepath.Dir(path)
	if err := ensurePrivateDirectory(parent); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errXrayJournalInvalid
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".xkeen-component-")
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
	if syncDir == nil {
		syncDir = syncDirectory
	}
	if syncDir != nil {
		return syncDir(parent)
	}
	return nil
}

func (s *XrayService) writeJournal(journal xrayTransactionJournal) error {
	if err := validateXrayJournal(journal); err != nil {
		return err
	}
	contents, err := json.Marshal(journal)
	if err != nil || len(contents)+1 > MaxComponentJournalBytes {
		return errXrayJournalInvalid
	}
	contents = append(contents, '\n')
	return writeAtomicComponentFile(s.config.JournalPath, contents, 0o600, s.config.SyncDirectory)
}

func (s *XrayService) readJournal() (xrayTransactionJournal, bool, error) {
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return xrayTransactionJournal{}, false, errXrayJournalInvalid
	}
	if !present {
		return xrayTransactionJournal{}, false, nil
	}
	if kind != KindXray {
		// The shared startup arbiter dispatches a geodata journal to geodata.
		// Xray deliberately treats it as not-owned and never clears or blocks it.
		return xrayTransactionJournal{}, false, nil
	}
	contents, err := readPrivateComponentFile(s.config.JournalPath, MaxComponentJournalBytes)
	if errors.Is(err, os.ErrNotExist) {
		return xrayTransactionJournal{}, false, nil
	}
	if err != nil {
		return xrayTransactionJournal{}, false, errXrayJournalInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var journal xrayTransactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return xrayTransactionJournal{}, false, errXrayJournalInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return xrayTransactionJournal{}, false, errXrayJournalInvalid
	}
	if err := validateXrayJournal(journal); err != nil {
		return xrayTransactionJournal{}, false, err
	}
	return journal, true, nil
}

func validateXrayJournal(journal xrayTransactionJournal) error {
	if journal.SchemaVersion != XrayTransactionSchemaVersion || journal.Component != string(KindXray) || (journal.Operation != xrayOperationUpdate && journal.Operation != xrayOperationRollback) {
		return errXrayJournalInvalid
	}
	switch journal.Phase {
	case xrayPhasePrepared, xrayPhaseBinaryCommitted, xrayPhaseRuntimeVerified:
	default:
		return errXrayJournalInvalid
	}
	if !validXrayBinaryMetadata(journal.Previous, true) || !validXrayBinaryMetadata(journal.Candidate, true) {
		return errXrayJournalInvalid
	}
	return nil
}

func (s *XrayService) clearJournal() error {
	info, err := os.Lstat(s.config.JournalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errXrayJournalInvalid
	}
	if err := checkPrivateComponentDirectory(filepath.Dir(s.config.JournalPath)); err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return errXrayJournalInvalid
	}
	contents, readErr := readPrivateComponentFile(s.config.JournalPath, MaxComponentJournalBytes)
	if readErr != nil {
		return errXrayJournalInvalid
	}
	if err := os.Remove(s.config.JournalPath); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.JournalPath)); err != nil {
		if restoreErr := writeAtomicComponentFile(s.config.JournalPath, contents, 0o600, s.config.SyncDirectory); restoreErr != nil {
			return restoreErr
		}
		return err
	}
	return nil
}

func ownedRegularPath(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

func (s *XrayService) removeOwned(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func (s *XrayService) removeOwnedAndSync(path string) error {
	if err := s.removeOwned(path); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(path))
}

func (s *XrayService) pathPresent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *XrayService) stagingPresent() bool {
	return s.previousStagingPresent() || s.componentStagingRootPresent()
}

func (s *XrayService) previousStagingPresent() bool {
	info, err := os.Lstat(s.stagingPath())
	return err == nil && info != nil && info.Mode()&os.ModeSymlink == 0
}

func (s *XrayService) componentStagingRootPresent() bool {
	info, err := os.Lstat(s.config.StagingDir)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return true
	}
	entries, err := os.ReadDir(s.config.StagingDir)
	return err != nil || len(entries) > 0
}

func (s *XrayService) saveDisplacedGeneration(source, expectedVersion string) error {
	meta, err := binaryMetadataWithoutProbe(source, expectedVersion)
	if err != nil {
		return err
	}
	return s.savePreviousFromSource(source, meta)
}

func (s *XrayService) savePreviousFromSource(source string, meta xrayBinaryMetadata) error {
	parent := filepath.Dir(s.config.PreviousDir)
	if err := ensurePrivateDirectory(parent); err != nil {
		return err
	}
	staging := s.stagingPath()
	if err := s.removeOwned(staging); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(staging); err != nil {
		return err
	}
	if err := copyXrayFile(meta, source, filepath.Join(staging, xrayPreviousBinaryName), s.config.SyncDirectory); err != nil {
		return err
	}
	if err := writeXrayPreviousMetadata(filepath.Join(staging, xrayPreviousMetadataName), meta, s.config.SyncDirectory); err != nil {
		return err
	}
	return s.promotePreviousGeneration()
}

func (s *XrayService) stageDisplacedCurrent(meta xrayBinaryMetadata, ctx context.Context) (string, error) {
	if !validXrayBinaryMetadata(meta, true) {
		return "", errXrayBinaryInvalid
	}
	if err := ensurePrivateDirectory(s.config.StagingDir); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp(s.config.StagingDir, ".xray-recovery-")
	if err != nil {
		return "", err
	}
	destination := filepath.Join(directory, xrayPreviousBinaryName)
	if err := copyXrayFile(meta, s.config.ActiveBinaryPath, destination, s.config.SyncDirectory); err != nil {
		_ = s.removeOwned(directory)
		return "", err
	}
	if ctx != nil && ctx.Err() != nil {
		_ = s.removeOwned(directory)
		return "", ctx.Err()
	}
	return destination, nil
}

var xrayCandidateFiles = map[string]struct{}{
	"xray/01_log.json":         {},
	"xray/02_dns.json":         {},
	"xray/03_inbounds.json":    {},
	"xray/04_outbounds.json":   {},
	"xray/05_routing.json":     {},
	"xray/06_policy.json":      {},
	"xray/07_observatory.json": {},
	"xray/08_api.json":         {},
	"xkeen/xkeen.json":         {},
}

func writeXrayCandidateTree(root string, files map[string][]byte, syncDir func(string) error) error {
	if root == "" || len(files) != len(xrayCandidateFiles) {
		return errXrayCandidateConfigInvalid
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return errXrayCandidateConfigInvalid
	}
	for name, contents := range files {
		if _, ok := xrayCandidateFiles[name]; !ok || len(contents) == 0 || len(contents) > appliance.MaxDocumentSize {
			return errXrayCandidateConfigInvalid
		}
		if path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
			return errXrayCandidateConfigInvalid
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if filepath.Dir(target) != filepath.Join(root, filepath.FromSlash(filepath.Dir(name))) {
			return errXrayCandidateConfigInvalid
		}
		if err := ensurePrivateDirectory(filepath.Dir(target)); err != nil {
			return errXrayCandidateConfigInvalid
		}
		if err := writeAtomicComponentFile(target, contents, 0o600, syncDir); err != nil {
			return errXrayCandidateConfigInvalid
		}
	}
	return nil
}

func syncDirectory(directory string) error {
	if directory == "" || runtime.GOOS == "windows" {
		return nil
	}
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
