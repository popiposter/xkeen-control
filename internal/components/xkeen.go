package components

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	XKeenTransactionSchemaVersion = 1

	DefaultXKeenPreviousDir         = "/opt/etc/xkeen-control/previous/components/xkeen"
	DefaultXKeenComponentStagingDir = "/tmp/xkeen-control/components/xkeen"
	DefaultXKeenActivationPath      = "/opt/sbin/.xkeen-control-activation"
	DefaultXKeenMarkerPath          = "/opt/etc/xkeen-control/state/xkeen-generation.json"

	DefaultXKeenAuthorityWaitTimeout = 15 * time.Second
	DefaultXKeenPrepareTimeout       = 3 * time.Minute
	DefaultXKeenActivationTimeout    = 2 * time.Minute
	DefaultXKeenRollbackTimeout      = 2 * time.Minute
	DefaultXKeenTransactionTimeout   = 6 * time.Minute

	MaxXKeenArchiveBytes       = 8 << 20
	MaxXKeenArchiveEntries     = 256
	MaxXKeenArchiveMemberBytes = 1 << 20
	MaxXKeenArchiveAggregate   = 16 << 20
	// GNU tar's default archive record is 20 blocks of 512 bytes.
	MaxXKeenArchivePaddingBytes = 20 * 512
	MaxXKeenGenerationEntries   = 512
	MaxXKeenGenerationFileBytes = 1 << 20
	MaxXKeenGenerationBytes     = 16 << 20
	MaxXKeenMarkerBytes         = 2 << 10
	MaxXKeenPreviousMetadata    = 32 << 10

	xkeenBuildCommitPath = "/repos/" + xkeenCatalogRepository + "/commits/" + xkeenCatalogBuildCommit
	xkeenBuildTreePath   = "/repos/" + xkeenCatalogRepository + "/git/trees/" + xkeenCatalogBuildCommit + "?recursive=1"
	xkeenBlobPathPrefix  = "/repos/" + xkeenCatalogRepository + "/git/blobs/"

	xkeenOperationUpdate          = "update"
	xkeenOperationRollback        = "rollback"
	xkeenPhasePrepared            = "prepared"
	xkeenPhaseXkeenCommitted      = "xkeen-committed"
	xkeenPhaseModuleCommitted     = "module-committed"
	xkeenPhaseGenerationCommitted = "generation-committed"
	// files-committed was emitted by the first E1 transaction draft. Keep it
	// readable so an interrupted draft transaction can still be recovered.
	xkeenPhaseFilesCommitted  = "files-committed"
	xkeenPhaseRuntimeVerified = "runtime-verified"

	xkeenPreviousMetadataName  = "metadata.json"
	xkeenPreviousPayloadDir    = "generation"
	xkeenPreviousMarkerName    = "xkeen-generation.json"
	xkeenOwnerName             = ".owner"
	xkeenOwnerValue            = "xkeen-control/xkeen-phase-e-v1\n"
	xkeenActivationOwnerValue  = "xkeen-control/xkeen-phase-e-activation-v1\n"
	xkeenMarkerStageSuffix     = ".staging"
	xkeenPreviousStagingSuffix = ".staging"
	xkeenPreviousOldSuffix     = ".old"
)

var (
	ErrXKeenResolutionUnavailable  = errors.New("XKeen release resolution unavailable")
	ErrXKeenCandidateRejected      = errors.New("XKeen candidate was rejected")
	ErrXKeenCandidateStale         = errors.New("XKeen candidate is stale")
	ErrXKeenArtifactRejected       = errors.New("XKeen artifact was rejected")
	ErrXKeenAuthorityUnavailable   = errors.New("XKeen authority is unavailable")
	ErrXKeenAuthorityBusy          = errors.New("XKeen authority is busy")
	ErrXKeenTransactionUnavailable = errors.New("XKeen transaction is unavailable")
	ErrXKeenApplyFailed            = errors.New("XKeen activation failed")
	ErrXKeenRollbackFailed         = errors.New("XKeen rollback failed")
	ErrXKeenPreviousUnavailable    = errors.New("previous Xkeen generation is unavailable")
	ErrXKeenRecoveryRequired       = errors.New("XKeen component recovery is required")
	ErrXKeenRecoveryConflict       = errors.New("XKeen component recovery conflicts with restore")
	ErrXKeenRecoveryFailed         = errors.New("XKeen component recovery failed")
	ErrXKeenBusy                   = errors.New("XKeen component transaction is busy")

	errXKeenArchiveRejected       = errors.New("XKeen archive was rejected")
	errXKeenArchiveTooLarge       = errors.New("XKeen archive exceeds the limit")
	errXKeenArtifactSizeMismatch  = errors.New("XKeen artifact size does not match metadata")
	errXKeenArtifactHashMismatch  = errors.New("XKeen artifact digest does not match metadata")
	errXKeenGenerationInvalid     = errors.New("XKeen installed generation is invalid")
	errXKeenMarkerInvalid         = errors.New("XKeen generation marker is invalid")
	errXKeenJournalInvalid        = errors.New("XKeen transaction journal is invalid")
	errXKeenPreviousInvalid       = errors.New("previous Xkeen generation is invalid")
	errXKeenAuthorityChanged      = errors.New("XKeen authority generation changed")
	errXKeenPreservedChanged      = errors.New("preserved appliance state changed")
	errXKeenFreeSpaceUnavailable  = errors.New("XKeen component free space is unavailable")
	errXKeenFreeSpaceInsufficient = errors.New("XKeen component free space is insufficient")
	errXKeenArchiveLayoutInvalid  = errors.New("XKeen archive layout is invalid")
	errXKeenActivationInvalid     = errors.New("XKeen activation path is invalid")
)

// XKeenReleaseIdentity is the server-owned identity of the one reviewed
// fixed XKeen release. Callers cannot supply a repository, URL or mirror.
type XKeenReleaseIdentity struct {
	Repository       string
	Channel          string
	Tag              string
	Version          string
	CommitSHA        string
	SourceParentSHA  string
	AssetName        string
	BlobSHA          string
	SizeBytes        int64
	SHA256           string
	GenerationSHA256 string
	// Generation is retained as a typed compatibility alias for internal
	// callers that use the CheckCandidate spelling.
	Generation string
}

type XKeenCandidateResolver interface {
	ResolveXKeen(context.Context) (XKeenReleaseIdentity, error)
}

type XKeenArtifactDownloader interface {
	DownloadXKeen(context.Context, XKeenReleaseIdentity, io.Writer) error
}

// XKeenResolver performs fresh exact build-commit and recursive-tree metadata
// reads. A successful result is always the product-owned catalog identity.
type XKeenResolver struct{ client *metadataClient }

func NewXKeenResolver(resolver netguard.IPResolver, supplied *http.Client) *XKeenResolver {
	return &XKeenResolver{client: newMetadataClient(resolver, supplied)}
}

func (r *XKeenResolver) ResolveXKeen(ctx context.Context) (XKeenReleaseIdentity, error) {
	entry, ok := reviewedXKeenEntry(xkeenCatalogBuildCommit, xkeenCatalogAsset)
	if !ok || validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable || r == nil || r.client == nil {
		return XKeenReleaseIdentity{}, ErrXKeenResolutionUnavailable
	}
	budget := newNetworkBudget()
	body, err := r.client.fetch(ctx, xkeenBuildCommitPath, budget)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return XKeenReleaseIdentity{}, ctx.Err()
		}
		return XKeenReleaseIdentity{}, ErrXKeenResolutionUnavailable
	}
	var commit githubXKeenCommitMetadata
	if json.Unmarshal(body, &commit) != nil || !strings.EqualFold(commit.SHA, entry.CommitSHA) ||
		!commit.Commit.Verification.Verified || strings.TrimSpace(commit.Commit.Message) != xkeenDevBuildCommitMessage ||
		len(commit.Parents) != 1 || !strings.EqualFold(commit.Parents[0].SHA, entry.SourceParentSHA) ||
		len(commit.Files) != 1 {
		return XKeenReleaseIdentity{}, ErrXKeenCandidateRejected
	}
	changed := commit.Files[0]
	if changed.Filename != entry.AssetName || changed.Status != "modified" || !strings.EqualFold(changed.SHA, entry.BlobSHA) {
		return XKeenReleaseIdentity{}, ErrXKeenCandidateRejected
	}
	treeBody, err := r.client.fetch(ctx, xkeenBuildTreePath, budget)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return XKeenReleaseIdentity{}, ctx.Err()
		}
		return XKeenReleaseIdentity{}, ErrXKeenResolutionUnavailable
	}
	var tree githubXKeenTreeMetadata
	if json.Unmarshal(treeBody, &tree) != nil || tree.Truncated || len(tree.Tree) > MaxXKeenDevTreeEntries {
		return XKeenReleaseIdentity{}, ErrXKeenCandidateRejected
	}
	matches := 0
	for _, item := range tree.Tree {
		if item.Path != entry.AssetName {
			continue
		}
		matches++
		if item.Type != "blob" || item.Mode != "100644" || item.Size != entry.SizeBytes || !strings.EqualFold(item.SHA, entry.BlobSHA) {
			return XKeenReleaseIdentity{}, ErrXKeenCandidateRejected
		}
	}
	if matches != 1 {
		return XKeenReleaseIdentity{}, ErrXKeenCandidateRejected
	}
	return XKeenReleaseIdentity{
		Repository: entry.Repository, Channel: entry.Channel, Tag: entry.Tag, Version: entry.Version, CommitSHA: entry.CommitSHA,
		SourceParentSHA: entry.SourceParentSHA, AssetName: entry.AssetName, BlobSHA: entry.BlobSHA, SizeBytes: entry.SizeBytes, SHA256: entry.SHA256,
		GenerationSHA256: entry.GenerationSHA256, Generation: entry.GenerationSHA256,
	}, nil
}

type XKeenStage string

const (
	XKeenStagePreviousStaging XKeenStage = "previous-staging"
	XKeenStagePreviousSaved   XKeenStage = "previous-saved"
	XKeenStageJournalPrepared XKeenStage = "journal-prepared"
	XKeenStageXkeenCommitted  XKeenStage = "xkeen-committed"
	XKeenStageModuleCommitted XKeenStage = "module-committed"
	XKeenStageFilesCommitted  XKeenStage = "files-committed"
	XKeenStagePreviousSettled XKeenStage = "previous-settled"
	XKeenStageRuntimeVerified XKeenStage = "runtime-verified"
	XKeenStageJournalCleared  XKeenStage = "journal-cleared"
)

type XKeenConfig struct {
	Resolver   XKeenCandidateResolver
	Downloader XKeenArtifactDownloader
	Authority  XrayAuthorityProvider
	Runtime    XrayRuntime

	CandidateProbe     XrayCandidateProbe
	CandidateValidator XrayCandidateValidator

	AuthorityLease *authority.Lease
	Coordinator    XrayCoordinator

	ActiveBinaryPath   string
	ModuleDir          string
	LifecycleInitPath  string
	LegacyInitPath     string
	SiblingModulePath  string
	InstallHelperPath  string
	MarkerPath         string
	XrayBinaryPath     string
	XrayConfigDir      string
	XrayAssetDir       string
	PreviousDir        string
	JournalPath        string
	StagingDir         string
	RestoreJournalPath string
	ActivationPath     string
	PreservedPaths     []string
	MutationGate       *ComponentMutationGate
	Maintenance        *ComponentMaintenance

	AuthorityWaitTimeout time.Duration
	PrepareTimeout       time.Duration
	ActivationTimeout    time.Duration
	RollbackTimeout      time.Duration
	TransactionTimeout   time.Duration

	AvailableSpace func(string) (uint64, error)
	SyncDirectory  func(string) error
	InjectFailure  func(XKeenStage) error
}

type XKeenService struct {
	config       XKeenConfig
	mutationGate *ComponentMutationGate
	startupMu    sync.Mutex
	mu           sync.Mutex
	ready        bool
	readyErr     error
	maintenance  bool
}

type preparedXKeen struct {
	identity      XKeenReleaseIdentity
	base          xkeenBaseSnapshot
	stageDir      string
	candidatePath string
	candidate     xkeenGenerationMetadata
	marker        []byte
}

type xkeenBaseSnapshot struct {
	authority            XrayAuthoritySnapshot
	xray                 xrayBinaryMetadata
	active               xkeenGenerationMetadata
	marker               []byte
	markerRecord         xkeenMarkerRecord
	preservedFingerprint string
}

type xkeenGenerationEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type xkeenGenerationMetadata struct {
	Generation string                 `json:"generation"`
	Entries    []xkeenGenerationEntry `json:"entries"`
	Bytes      int64                  `json:"bytes"`
}

type xkeenGenerationSummary struct {
	Generation    string `json:"generation"`
	Entries       int    `json:"entries"`
	Bytes         int64  `json:"bytes"`
	MarkerPresent bool   `json:"markerPresent"`
	MarkerSHA256  string `json:"markerSha256,omitempty"`
}

type xkeenMarkerRecord struct {
	SchemaVersion      int    `json:"schemaVersion"`
	Repository         string `json:"repository"`
	Channel            string `json:"channel"`
	Tag                string `json:"tag"`
	Version            string `json:"version"`
	BuildCommitSHA     string `json:"buildCommitSha"`
	SourceParentSHA    string `json:"sourceParentSha"`
	AssetName          string `json:"assetName"`
	BlobSHA            string `json:"blobSha"`
	ArchiveSHA256      string `json:"archiveSha256"`
	GenerationSHA256   string `json:"generationSha256"`
	LifecycleClass     string `json:"lifecycleClass"`
	CompatibilityClass string `json:"compatibilityClass"`
}

type xkeenTransactionJournal struct {
	SchemaVersion        int                    `json:"schemaVersion"`
	Component            string                 `json:"component"`
	Operation            string                 `json:"operation"`
	Phase                string                 `json:"phase"`
	Previous             xkeenGenerationSummary `json:"previous"`
	Candidate            xkeenGenerationSummary `json:"candidate"`
	AuthorityGeneration  string                 `json:"authorityGeneration"`
	PreservedFingerprint string                 `json:"preservedFingerprint"`
	Xray                 xrayBinaryMetadata     `json:"xray"`
}

type xkeenPreviousRecord struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Generation    string                 `json:"generation"`
	Entries       []xkeenGenerationEntry `json:"entries"`
	Bytes         int64                  `json:"bytes"`
	MarkerPresent bool                   `json:"markerPresent"`
	MarkerMode    uint32                 `json:"markerMode,omitempty"`
	MarkerSHA256  string                 `json:"markerSha256,omitempty"`
	Marker        xkeenMarkerRecord      `json:"marker,omitempty"`
}

type loadedXKeenGeneration struct {
	path       string
	meta       xkeenGenerationMetadata
	marker     []byte
	markerInfo os.FileInfo
}

func NewXKeenService(config XKeenConfig) *XKeenService {
	if config.Resolver == nil {
		config.Resolver = NewXKeenResolver(nil, nil)
	}
	if config.Downloader == nil {
		config.Downloader = NewXKeenArtifactDownloader(nil, nil)
	}
	if config.CandidateProbe == nil {
		config.CandidateProbe = CommandXrayCandidateProbe{}
	}
	if config.CandidateValidator == nil {
		config.CandidateValidator = CommandXrayCandidateValidator{}
	}
	if config.ActiveBinaryPath == "" {
		config.ActiveBinaryPath = DefaultXkeenBinary
	}
	if config.ModuleDir == "" {
		config.ModuleDir = DefaultXkeenModuleDir
	}
	if config.LifecycleInitPath == "" {
		config.LifecycleInitPath = DefaultXkeenRuntimeInit
	}
	if config.LegacyInitPath == "" {
		config.LegacyInitPath = DefaultXkeenLegacyRuntimeInit
	}
	if config.SiblingModulePath == "" {
		config.SiblingModulePath = filepath.Join(filepath.Dir(config.ModuleDir), "_xkeen")
	}
	if config.InstallHelperPath == "" {
		config.InstallHelperPath = "/opt/root/install.sh"
	}
	if config.MarkerPath == "" {
		config.MarkerPath = DefaultXKeenMarkerPath
	}
	if config.XrayBinaryPath == "" {
		config.XrayBinaryPath = DefaultXrayBinary
	}
	if config.XrayConfigDir == "" {
		config.XrayConfigDir = DefaultXrayConfigDir
	}
	if config.XrayAssetDir == "" {
		config.XrayAssetDir = DefaultXrayAssetDir
	}
	if config.PreviousDir == "" {
		config.PreviousDir = DefaultXKeenPreviousDir
	}
	if config.JournalPath == "" {
		config.JournalPath = DefaultComponentTransactionJournal
	}
	if config.StagingDir == "" {
		config.StagingDir = DefaultXKeenComponentStagingDir
	}
	if config.RestoreJournalPath == "" {
		config.RestoreJournalPath = filepath.Join(filepath.Dir(config.JournalPath), "appliance-import-transaction.json")
	}
	if config.ActivationPath == "" {
		config.ActivationPath = filepath.Join(filepath.Dir(config.ModuleDir), ".xkeen-control-activation")
	}
	if config.AuthorityWaitTimeout <= 0 {
		config.AuthorityWaitTimeout = DefaultXKeenAuthorityWaitTimeout
	}
	if config.PrepareTimeout <= 0 {
		config.PrepareTimeout = DefaultXKeenPrepareTimeout
	}
	if config.ActivationTimeout <= 0 {
		config.ActivationTimeout = DefaultXKeenActivationTimeout
	}
	if config.RollbackTimeout <= 0 {
		config.RollbackTimeout = DefaultXKeenRollbackTimeout
	}
	if config.TransactionTimeout <= 0 {
		config.TransactionTimeout = DefaultXKeenTransactionTimeout
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
	service := &XKeenService{config: config, mutationGate: config.MutationGate, ready: true}
	kind, present, journalErr := componentJournalKind(config.JournalPath)
	if journalErr != nil || present && kind != KindXKeen && service.stagingPresent() {
		service.ready, service.readyErr = false, ErrXKeenRecoveryFailed
		service.enterMaintenance()
		return service
	}
	if present && kind == KindXKeen || service.stagingPresent() {
		service.ready = false
		switch {
		case componentTransactionPresentUnchecked(config.RestoreJournalPath):
			service.readyErr = ErrXKeenRecoveryConflict
		case present:
			service.readyErr = ErrXKeenRecoveryRequired
		default:
			service.readyErr = ErrXKeenRecoveryFailed
		}
		service.enterMaintenance()
	}
	return service
}

func NewXKeenTransaction(config XKeenConfig) *XKeenService { return NewXKeenService(config) }

func (s *XKeenService) Ready() error {
	if s == nil {
		return ErrXKeenTransactionUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		if s.readyErr != nil {
			return s.readyErr
		}
		return ErrXKeenRecoveryRequired
	}
	return nil
}

func (s *XKeenService) HasPendingRecovery() (bool, error) {
	if s == nil {
		return false, ErrXKeenTransactionUnavailable
	}
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return false, err
	}
	if present && kind == KindXKeen {
		return true, nil
	}
	return s.stagingPresent(), nil
}

// Apply fresh-resolves the fixed catalog identity immediately before the
// archive download. It never accepts a caller URL, repository, tag or mirror.
func (s *XKeenService) Apply(ctx context.Context, intended XKeenReleaseIdentity) error {
	if err := s.Ready(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := s.acquireMutation(ctx)
	if err != nil {
		return err
	}
	defer release()
	transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	prepareContext, cancelPrepare := context.WithTimeout(transactionContext, s.config.PrepareTimeout)
	prepared, err := s.prepare(prepareContext, intended)
	cancelPrepare()
	if err != nil {
		return err
	}
	defer func() {
		_ = s.removeOwned(prepared.stageDir)
		_ = s.removeEmptyStagingRoot()
	}()
	return s.applyPrepared(transactionContext, prepared)
}

func (s *XKeenService) Update(ctx context.Context, intended XKeenReleaseIdentity) error {
	return s.Apply(ctx, intended)
}

func (s *XKeenService) Rollback(ctx context.Context) error {
	if err := s.Ready(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := s.acquireMutation(ctx)
	if err != nil {
		return err
	}
	defer release()
	transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	coordinator, authorityRelease, err := s.acquireApply(transactionContext)
	if err != nil {
		return err
	}
	defer coordinator()
	defer authorityRelease()
	base, err := s.captureHeld(transactionContext)
	if err != nil {
		return err
	}
	previous, err := s.loadPreviousGeneration()
	if err != nil {
		return ErrXKeenPreviousUnavailable
	}
	if err := s.checkFreeSpace(0, previous.meta.Bytes, base.active.Bytes); err != nil {
		return err
	}
	candidatePath, err := s.copyGenerationToCandidate(previous)
	if err != nil {
		return ErrXKeenCandidateRejected
	}
	defer func() {
		_ = s.removeOwned(candidatePath)
		_ = s.removeEmptyStagingRoot()
	}()
	if err := s.validateLocalCandidate(transactionContext, candidatePath, base.authority); err != nil {
		return ErrXKeenCandidateRejected
	}
	return s.runCommitted(transactionContext, xkeenOperationRollback, base, candidatePath, previous.meta, previous.marker, true)
}

func decodeXKeenCommitMetadata(body []byte) (string, error) {
	if len(body) == 0 || len(body) > MaxMetadataResponseBytes {
		return "", ErrXKeenResolutionUnavailable
	}
	var value struct {
		SHA string `json:"sha"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&value); err != nil {
		return "", ErrXKeenResolutionUnavailable
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || !isHexSHA1(value.SHA) {
		return "", ErrXKeenResolutionUnavailable
	}
	return strings.ToLower(value.SHA), nil
}

func isHexSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func makeXKeenGenerationSummary(value xkeenGenerationMetadata, marker []byte) xkeenGenerationSummary {
	result := xkeenGenerationSummary{Generation: value.Generation, Entries: len(value.Entries), Bytes: value.Bytes, MarkerPresent: len(marker) > 0}
	if len(marker) > 0 {
		digest := sha256.Sum256(marker)
		result.MarkerSHA256 = hex.EncodeToString(digest[:])
	}
	return result
}

func validXKeenGenerationSummary(value xkeenGenerationSummary) bool {
	return isHexSHA256(value.Generation) && value.Entries > 0 && value.Entries <= MaxXKeenGenerationEntries && value.Bytes >= 0 && value.Bytes <= MaxXKeenGenerationBytes && (!value.MarkerPresent && value.MarkerSHA256 == "" || value.MarkerPresent && isHexSHA256(value.MarkerSHA256))
}

func validXKeenGenerationMetadata(value xkeenGenerationMetadata) bool {
	if !validXKeenGenerationSummary(makeXKeenGenerationSummary(value, nil)) || len(value.Entries) != makeXKeenGenerationSummary(value, nil).Entries {
		return false
	}
	seen := make(map[string]struct{}, len(value.Entries))
	var total int64
	for _, entry := range value.Entries {
		if entry.Path == "" || entry.Path != path.Clean(filepath.ToSlash(entry.Path)) || strings.HasPrefix(entry.Path, "../") || strings.Contains(entry.Path, "/../") || strings.Contains(entry.Path, "\\") ||
			(entry.Type != "file" && entry.Type != "directory") || entry.Mode == 0 || entry.Mode&^0o777 != 0 || entry.Size < 0 || entry.Size > MaxXKeenGenerationFileBytes {
			return false
		}
		if _, ok := seen[entry.Path]; ok {
			return false
		}
		seen[entry.Path] = struct{}{}
		if entry.Type == "file" {
			if !isHexSHA256(entry.SHA256) || total > MaxXKeenGenerationBytes-entry.Size {
				return false
			}
			total += entry.Size
		} else if entry.Size != 0 || entry.SHA256 != "" {
			return false
		}
	}
	return total == value.Bytes && strings.EqualFold(canonicalXKeenGenerationDigest(value.Entries), value.Generation)
}

func canonicalXKeenGenerationDigest(entries []xkeenGenerationEntry) string {
	ordered := append([]xkeenGenerationEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	destination := sha256.New()
	for _, entry := range ordered {
		if writeXKeenHashPart(destination, entry.Path) != nil || writeXKeenHashPart(destination, entry.Type) != nil || writeXKeenHashPart(destination, fmt.Sprintf("%o", entry.Mode)) != nil || writeXKeenHashPart(destination, fmt.Sprintf("%d", entry.Size)) != nil || writeXKeenHashPart(destination, strings.ToLower(entry.SHA256)) != nil {
			return ""
		}
	}
	return hex.EncodeToString(destination.Sum(nil))
}

func writeXKeenHashPart(destination io.Writer, value string) error {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	if _, err := destination.Write(length[:]); err != nil {
		return err
	}
	_, err := io.WriteString(destination, value)
	return err
}

func readXKeenGeneration(binaryPath, modulePath string) (xkeenGenerationMetadata, error) {
	if binaryPath == "" || modulePath == "" {
		return xkeenGenerationMetadata{}, errXKeenGenerationInvalid
	}
	entries := make([]xkeenGenerationEntry, 0, 64)
	var total int64
	add := func(relative, absolute string, wantDirectory bool) error {
		info, err := os.Lstat(absolute)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errXKeenGenerationInvalid
		}
		if wantDirectory {
			if !info.IsDir() {
				return errXKeenGenerationInvalid
			}
			entries = append(entries, xkeenGenerationEntry{Path: relative, Type: "directory", Mode: uint32(info.Mode().Perm())})
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > MaxXKeenGenerationFileBytes || total > MaxXKeenGenerationBytes-info.Size() {
			return errXKeenGenerationInvalid
		}
		digest, count, err := hashXKeenFile(absolute, info.Size(), MaxXKeenGenerationFileBytes)
		if err != nil {
			return errXKeenGenerationInvalid
		}
		entries = append(entries, xkeenGenerationEntry{Path: relative, Type: "file", Mode: uint32(info.Mode().Perm()), Size: count, SHA256: digest})
		total += count
		return nil
	}
	if err := add("xkeen", binaryPath, false); err != nil {
		return xkeenGenerationMetadata{}, err
	}
	if err := walkXKeenDirectory(modulePath, ".xkeen", &entries, &total); err != nil {
		return xkeenGenerationMetadata{}, err
	}
	metadata := xkeenGenerationMetadata{Entries: entries, Bytes: total}
	metadata.Generation = canonicalXKeenGenerationDigest(entries)
	if !validXKeenGenerationMetadata(metadata) {
		return xkeenGenerationMetadata{}, errXKeenGenerationInvalid
	}
	return metadata, nil
}

func walkXKeenDirectory(directory, relative string, entries *[]xkeenGenerationEntry, total *int64) error {
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errXKeenGenerationInvalid
	}
	if len(*entries) >= MaxXKeenGenerationEntries {
		return errXKeenGenerationInvalid
	}
	*entries = append(*entries, xkeenGenerationEntry{Path: filepath.ToSlash(relative), Type: "directory", Mode: uint32(info.Mode().Perm())})
	children, err := os.ReadDir(directory)
	if err != nil {
		return errXKeenGenerationInvalid
	}
	for _, child := range children {
		if child.Name() == "" || child.Name() == "." || child.Name() == ".." || strings.ContainsAny(child.Name(), `/\\`) || len(child.Name()) > 128 {
			return errXKeenGenerationInvalid
		}
		childRelative := filepath.ToSlash(filepath.Join(relative, child.Name()))
		childPath := filepath.Join(directory, child.Name())
		childInfo, err := os.Lstat(childPath)
		if err != nil || childInfo.Mode()&os.ModeSymlink != 0 {
			return errXKeenGenerationInvalid
		}
		if childInfo.IsDir() {
			if err := walkXKeenDirectory(childPath, childRelative, entries, total); err != nil {
				return err
			}
			continue
		}
		if !childInfo.Mode().IsRegular() || childInfo.Size() > MaxXKeenGenerationFileBytes || *total > MaxXKeenGenerationBytes-childInfo.Size() {
			return errXKeenGenerationInvalid
		}
		digest, count, err := hashXKeenFile(childPath, childInfo.Size(), MaxXKeenGenerationFileBytes)
		if err != nil {
			return errXKeenGenerationInvalid
		}
		*entries = append(*entries, xkeenGenerationEntry{Path: childRelative, Type: "file", Mode: uint32(childInfo.Mode().Perm()), Size: count, SHA256: digest})
		*total += count
		if len(*entries) > MaxXKeenGenerationEntries {
			return errXKeenGenerationInvalid
		}
	}
	return nil
}

func hashXKeenFile(filePath string, expected, limit int64) (string, int64, error) {
	if expected < 0 || limit <= 0 || expected > limit {
		return "", 0, errXKeenGenerationInvalid
	}
	before, err := os.Lstat(filePath)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() != expected {
		return "", 0, errXKeenGenerationInvalid
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, errXKeenGenerationInvalid
	}
	opened, err := file.Stat()
	if err != nil || opened.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || opened.Size() != expected {
		_ = file.Close()
		return "", 0, errXKeenGenerationInvalid
	}
	digest := sha256.New()
	count, copyErr := copyXKeenBytes(context.Background(), digest, file, limit)
	closeErr := file.Close()
	after, afterErr := os.Lstat(filePath)
	if copyErr != nil || closeErr != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) || count != expected {
		return "", 0, errXKeenGenerationInvalid
	}
	return hex.EncodeToString(digest.Sum(nil)), count, nil
}

func copyXKeenBytes(ctx context.Context, destination io.Writer, source io.Reader, limit int64) (int64, error) {
	if limit < 0 {
		return 0, errXKeenGenerationInvalid
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
			if total > limit-int64(read) {
				return total, errXKeenArchiveTooLarge
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
		if read == 0 {
			return total, io.ErrNoProgress
		}
	}
}

func extractXKeenArchive(ctx context.Context, archivePath, destination string, entry xkeenCompatibilityEntry) (xkeenGenerationMetadata, error) {
	if validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable || archivePath == "" || destination == "" {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	return extractXKeenArchiveMembers(ctx, archivePath, destination, entry.ArchiveMembers)
}

// extractXKeenArchiveMembers is kept separate from the fixed identity check so
// fixtures can exercise the strict reader with small synthetic allowlists.
// Production callers always reach it through extractXKeenArchive, which first
// validates the product-pinned release record.
func extractXKeenArchiveMembers(ctx context.Context, archivePath, destination string, members []XKeenArchiveMember) (xkeenGenerationMetadata, error) {
	if archivePath == "" || destination == "" || len(members) == 0 || len(members) > MaxXKeenArchiveEntries {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	info, err := os.Lstat(archivePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxXKeenArchiveBytes {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	defer archive.Close()
	compressed := bufio.NewReaderSize(archive, 32<<10)
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	gzipReader.Multistream(false)
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	expected := make(map[string]XKeenArchiveMember, len(members))
	for _, member := range members {
		if member.Name == "" || member.Type != xkeenArchiveRegular || member.Size < 0 || member.Size > MaxXKeenArchiveMemberBytes || !validXKeenArchiveMemberMode(member) || validateXKeenArchiveName(member.Name, member.Type) != nil {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		if _, duplicate := expected[member.Name]; duplicate {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		expected[member.Name] = member
	}
	seen := make(map[string]struct{}, len(expected))
	var aggregate int64
	if err := ensurePrivateDirectory(destination); err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	if existing, err := os.ReadDir(destination); err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	} else if len(existing) != 0 {
		if len(existing) != 1 || existing[0].Name() != xkeenOwnerName || validXKeenOwner(destination, xkeenOwnerValue) != nil {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
	}
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil || header == nil || header.Name == "" || len(header.Name) > 256 {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		if header.Format != tar.FormatGNU || len(header.PAXRecords) != 0 || header.Linkname != "" {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		member, ok := expected[header.Name]
		if !ok {
			return xkeenGenerationMetadata{}, errXKeenArchiveLayoutInvalid
		}
		if _, duplicate := seen[header.Name]; duplicate {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		seen[header.Name] = struct{}{}
		if header.Mode&0o777 != int64(member.Mode) || header.Mode&^0o777 != 0 {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		if member.Type != xkeenArchiveRegular || header.Typeflag != tar.TypeReg || header.Size != member.Size || header.Size > MaxXKeenArchiveMemberBytes || aggregate > MaxXKeenArchiveAggregate-header.Size {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		destinationPath, err := xkeenArchiveDestination(destination, header.Name)
		if err != nil {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		if err := ensurePrivateDirectory(filepath.Dir(destinationPath)); err != nil {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(member.Mode))
		if err != nil {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		count, copyErr := copyXKeenBytes(ctx, output, reader, member.Size)
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || count != member.Size {
			_ = os.Remove(destinationPath)
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		if err := os.Chmod(destinationPath, os.FileMode(member.Mode)); err != nil {
			_ = os.Remove(destinationPath)
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
		aggregate += member.Size
	}
	if len(seen) != len(expected) {
		return xkeenGenerationMetadata{}, errXKeenArchiveLayoutInvalid
	}
	// GNU tar pads its final record with zero bytes after the two logical end
	// blocks. Accept only that bounded zero padding; compressed trailing data
	// remains rejected below.
	var trailing [32 << 10]byte
	var trailingBytes int64
	for {
		count, readErr := gzipReader.Read(trailing[:])
		if count > 0 {
			if trailingBytes > MaxXKeenArchivePaddingBytes-int64(count) {
				return xkeenGenerationMetadata{}, errXKeenArchiveRejected
			}
			for _, value := range trailing[:count] {
				if value != 0 {
					return xkeenGenerationMetadata{}, errXKeenArchiveRejected
				}
			}
			trailingBytes += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return xkeenGenerationMetadata{}, errXKeenArchiveRejected
		}
	}
	if _, readErr := compressed.Peek(1); !errors.Is(readErr, io.EOF) {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	if err := os.Chmod(filepath.Join(destination, ".xkeen"), 0o755); err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	metadata, err := readXKeenGeneration(filepath.Join(destination, "xkeen"), filepath.Join(destination, ".xkeen"))
	if err != nil {
		return xkeenGenerationMetadata{}, errXKeenArchiveRejected
	}
	return metadata, nil
}

func xkeenArchiveDestination(destination, name string) (string, error) {
	if name == "xkeen" {
		return filepath.Join(destination, "xkeen"), nil
	}
	if strings.HasPrefix(name, "_xkeen/") {
		relative := strings.TrimPrefix(name, "_xkeen/")
		if relative == "" || strings.HasSuffix(relative, "/") {
			return "", errXKeenArchiveRejected
		}
		clean := path.Clean(relative)
		if clean != relative || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || strings.Contains(clean, "\\") {
			return "", errXKeenArchiveRejected
		}
		return filepath.Join(destination, ".xkeen", filepath.FromSlash(clean)), nil
	}
	return "", errXKeenArchiveRejected
}

func ensureXKeenExtractDirectory(directory string, mode uint32) error {
	if directory == "" {
		return errXKeenArchiveRejected
	}
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errXKeenArchiveRejected
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, os.FileMode(mode)); err != nil {
			return err
		}
	} else {
		return err
	}
	return os.Chmod(directory, os.FileMode(mode))
}

func markerForGeneration(generation string) (xkeenMarkerRecord, []byte, error) {
	entry, ok := installableXKeenEntryForGeneration(generation)
	if !ok {
		return xkeenMarkerRecord{}, nil, errXKeenMarkerInvalid
	}
	record := xkeenMarkerRecord{
		SchemaVersion: XKeenTransactionSchemaVersion, Repository: entry.Repository, Channel: entry.Channel,
		Tag: entry.Tag, Version: entry.Version, BuildCommitSHA: entry.CommitSHA, SourceParentSHA: entry.SourceParentSHA,
		AssetName: entry.AssetName, BlobSHA: entry.BlobSHA, ArchiveSHA256: entry.SHA256,
		GenerationSHA256: strings.ToLower(generation), LifecycleClass: entry.LifecycleClass, CompatibilityClass: entry.CompatibilityClass,
	}
	contents, err := json.Marshal(record)
	if err != nil || len(contents)+1 > MaxXKeenMarkerBytes {
		return xkeenMarkerRecord{}, nil, errXKeenMarkerInvalid
	}
	return record, append(contents, '\n'), nil
}

func parseXKeenMarker(contents []byte) (xkeenMarkerRecord, error) {
	if len(contents) == 0 || len(contents) > MaxXKeenMarkerBytes {
		return xkeenMarkerRecord{}, errXKeenMarkerInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var record xkeenMarkerRecord
	if err := decoder.Decode(&record); err != nil {
		return xkeenMarkerRecord{}, errXKeenMarkerInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return xkeenMarkerRecord{}, errXKeenMarkerInvalid
	}
	entry, ok := reviewedXKeenEntry(record.BuildCommitSHA, record.AssetName)
	if !ok || validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable ||
		record.SchemaVersion != XKeenTransactionSchemaVersion || record.Repository != entry.Repository || record.Channel != entry.Channel ||
		record.Tag != entry.Tag || record.Version != entry.Version || record.BuildCommitSHA != entry.CommitSHA ||
		record.SourceParentSHA != entry.SourceParentSHA || record.AssetName != entry.AssetName || record.BlobSHA != entry.BlobSHA || record.ArchiveSHA256 != entry.SHA256 ||
		!strings.EqualFold(record.GenerationSHA256, entry.GenerationSHA256) || record.LifecycleClass != entry.LifecycleClass ||
		record.CompatibilityClass != entry.CompatibilityClass {
		return xkeenMarkerRecord{}, errXKeenMarkerInvalid
	}
	return record, nil
}

func readXKeenMarker(markerPath string) (xkeenMarkerRecord, []byte, os.FileInfo, error) {
	if markerPath == "" {
		return xkeenMarkerRecord{}, nil, nil, errXKeenMarkerInvalid
	}
	info, err := os.Lstat(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return xkeenMarkerRecord{}, nil, nil, os.ErrNotExist
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxXKeenMarkerBytes || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return xkeenMarkerRecord{}, nil, nil, errXKeenMarkerInvalid
	}
	contents, err := readPrivateComponentFile(markerPath, MaxXKeenMarkerBytes)
	if err != nil {
		return xkeenMarkerRecord{}, nil, nil, errXKeenMarkerInvalid
	}
	record, err := parseXKeenMarker(contents)
	if err != nil {
		return xkeenMarkerRecord{}, nil, nil, err
	}
	return record, contents, info, nil
}

func (s *XKeenService) captureBase(ctx context.Context) (xkeenBaseSnapshot, error) {
	release, err := s.acquireAuthority(ctx, false)
	if err != nil {
		return xkeenBaseSnapshot{}, err
	}
	defer release()
	return s.captureHeld(ctx)
}

func (s *XKeenService) captureHeld(ctx context.Context) (xkeenBaseSnapshot, error) {
	if err := s.validateXKeenLifecycle(); err != nil {
		return xkeenBaseSnapshot{}, ErrXKeenAuthorityUnavailable
	}
	authoritySnapshot, err := s.captureAuthorityHeld(ctx)
	if err != nil {
		return xkeenBaseSnapshot{}, err
	}
	if s.config.CandidateProbe == nil || s.config.XrayBinaryPath == "" {
		return xkeenBaseSnapshot{}, ErrXKeenAuthorityUnavailable
	}
	xray, err := binaryMetadata(s.config.XrayBinaryPath, "", s.config.CandidateProbe, ctx)
	if err != nil {
		return xkeenBaseSnapshot{}, ErrXKeenAuthorityUnavailable
	}
	if s.config.Runtime != nil {
		if err := s.config.Runtime.ValidateActiveConfig(ctx); err != nil {
			return xkeenBaseSnapshot{}, ErrXKeenAuthorityUnavailable
		}
	}
	active, marker, markerRecord, err := s.readActiveGeneration()
	if err != nil {
		return xkeenBaseSnapshot{}, ErrXKeenAuthorityUnavailable
	}
	preserved, err := s.preservedFingerprint()
	if err != nil {
		return xkeenBaseSnapshot{}, ErrXKeenAuthorityUnavailable
	}
	return xkeenBaseSnapshot{authority: authoritySnapshot, xray: xray, active: active, marker: marker, markerRecord: markerRecord, preservedFingerprint: preserved}, nil
}

func (s *XKeenService) validateXKeenLifecycle() error {
	if s == nil || s.config.LifecycleInitPath == "" || s.config.LegacyInitPath == "" ||
		s.config.SiblingModulePath == "" || s.config.InstallHelperPath == "" {
		return errXKeenActivationInvalid
	}
	initInfo, err := os.Lstat(s.config.LifecycleInitPath)
	if err != nil || initInfo.Mode()&os.ModeSymlink != 0 || !initInfo.Mode().IsRegular() || initInfo.Mode().Perm()&0o111 == 0 {
		return errXKeenActivationInvalid
	}
	for _, forbidden := range []string{s.config.LegacyInitPath, s.config.SiblingModulePath, s.config.InstallHelperPath} {
		if _, err := os.Lstat(forbidden); err == nil {
			return errXKeenActivationInvalid
		} else if !errors.Is(err, os.ErrNotExist) {
			return errXKeenActivationInvalid
		}
	}
	return nil
}

func (s *XKeenService) captureAuthorityHeld(ctx context.Context) (XrayAuthoritySnapshot, error) {
	if s.config.Authority == nil {
		return XrayAuthoritySnapshot{}, ErrXKeenAuthorityUnavailable
	}
	snapshot, err := s.config.Authority.SnapshotUnderLease(ctx)
	if err != nil || !validAuthoritySnapshot(snapshot) {
		return XrayAuthoritySnapshot{}, ErrXKeenAuthorityUnavailable
	}
	return snapshot, nil
}

func (s *XKeenService) readActiveGeneration() (xkeenGenerationMetadata, []byte, xkeenMarkerRecord, error) {
	metadata, err := readXKeenGeneration(s.config.ActiveBinaryPath, s.config.ModuleDir)
	if err != nil {
		return xkeenGenerationMetadata{}, nil, xkeenMarkerRecord{}, err
	}
	record, contents, _, markerErr := readXKeenMarker(s.config.MarkerPath)
	if errors.Is(markerErr, os.ErrNotExist) {
		return metadata, nil, xkeenMarkerRecord{}, nil
	}
	if markerErr != nil || !strings.EqualFold(record.GenerationSHA256, metadata.Generation) {
		return xkeenGenerationMetadata{}, nil, xkeenMarkerRecord{}, errXKeenMarkerInvalid
	}
	return metadata, contents, record, nil
}

func sameXKeenGeneration(left, right xkeenGenerationMetadata) bool {
	if left.Generation == "" || !strings.EqualFold(left.Generation, right.Generation) || left.Bytes != right.Bytes || len(left.Entries) != len(right.Entries) {
		return false
	}
	for _, leftEntry := range left.Entries {
		found := false
		for _, rightEntry := range right.Entries {
			if leftEntry.Path == rightEntry.Path && leftEntry.Type == rightEntry.Type && leftEntry.Mode == rightEntry.Mode && leftEntry.Size == rightEntry.Size && strings.EqualFold(leftEntry.SHA256, rightEntry.SHA256) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sameXKeenBase(left, right xkeenBaseSnapshot) bool {
	return left.authority.Generation == right.authority.Generation && sameBinaryMetadata(left.xray, right.xray) &&
		sameXKeenGeneration(left.active, right.active) && bytes.Equal(left.marker, right.marker) &&
		strings.EqualFold(left.preservedFingerprint, right.preservedFingerprint)
}

func (s *XKeenService) prepare(ctx context.Context, intended XKeenReleaseIdentity) (preparedXKeen, error) {
	if !validXKeenIdentity(intended) {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	base, err := s.captureBase(ctx)
	if err != nil {
		return preparedXKeen{}, err
	}
	fresh, err := s.config.Resolver.ResolveXKeen(ctx)
	if err != nil {
		return preparedXKeen{}, err
	}
	if !sameXKeenIdentity(fresh, intended) {
		return preparedXKeen{}, ErrXKeenCandidateStale
	}
	entry, ok := reviewedXKeenEntry(intended.CommitSHA, intended.AssetName)
	if !ok || validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	candidateBytes, err := xkeenCatalogGenerationBytes(entry)
	if err != nil {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	if err := s.checkFreeSpace(intended.SizeBytes, candidateBytes, base.active.Bytes); err != nil {
		return preparedXKeen{}, err
	}
	stageDir, err := s.newStagingDir()
	if err != nil {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.removeOwned(stageDir)
			_ = s.removeEmptyStagingRoot()
		}
	}()
	archivePath := filepath.Join(stageDir, "xkeen.tar")
	if err := s.downloadCandidate(ctx, intended, archivePath); err != nil {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	candidatePath := filepath.Join(stageDir, "candidate")
	if err := ensureXKeenOwnedDirectory(candidatePath, xkeenOwnerValue); err != nil {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	candidate, err := extractXKeenArchive(ctx, archivePath, candidatePath, entry)
	if err != nil || !strings.EqualFold(candidate.Generation, entry.GenerationSHA256) {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	_, marker, err := markerForGeneration(candidate.Generation)
	if err != nil {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	if err := s.validateLocalCandidate(ctx, candidatePath, base.authority); err != nil {
		return preparedXKeen{}, ErrXKeenCandidateRejected
	}
	cleanup = false
	return preparedXKeen{identity: intended, base: base, stageDir: stageDir, candidatePath: candidatePath, candidate: candidate, marker: marker}, nil
}

func sameXKeenIdentity(left, right XKeenReleaseIdentity) bool {
	return left.Repository == right.Repository && left.Channel == right.Channel && left.Tag == right.Tag && left.Version == right.Version &&
		left.CommitSHA == right.CommitSHA && left.SourceParentSHA == right.SourceParentSHA && left.AssetName == right.AssetName &&
		left.BlobSHA == right.BlobSHA && left.SizeBytes == right.SizeBytes && strings.EqualFold(left.SHA256, right.SHA256) &&
		strings.EqualFold(xkeenIdentityGeneration(left), xkeenIdentityGeneration(right))
}

func xkeenIdentityGeneration(value XKeenReleaseIdentity) string {
	if value.GenerationSHA256 != "" {
		return value.GenerationSHA256
	}
	return value.Generation
}

func (s *XKeenService) applyPrepared(ctx context.Context, prepared preparedXKeen) error {
	coordinator, authorityRelease, err := s.acquireApply(ctx)
	if err != nil {
		return err
	}
	defer coordinator()
	defer authorityRelease()
	current, err := s.captureHeld(ctx)
	if err != nil {
		return err
	}
	if !sameXKeenBase(current, prepared.base) {
		return ErrXKeenCandidateStale
	}
	return s.runCommitted(ctx, xkeenOperationUpdate, current, prepared.candidatePath, prepared.candidate, prepared.marker, true)
}

func (s *XKeenService) validateLocalCandidate(ctx context.Context, candidatePath string, authoritySnapshot XrayAuthoritySnapshot) error {
	if s.config.CandidateValidator == nil {
		return ErrXKeenTransactionUnavailable
	}
	return s.config.CandidateValidator.ValidateXrayCandidate(ctx, s.config.XrayBinaryPath, s.config.XrayConfigDir, s.config.XrayAssetDir)
}

func (s *XKeenService) downloadCandidate(ctx context.Context, identity XKeenReleaseIdentity, destinationPath string) error {
	if err := ensurePrivateDirectory(filepath.Dir(destinationPath)); err != nil {
		return ErrXKeenArtifactRejected
	}
	output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrXKeenArtifactRejected
	}
	digest := sha256.New()
	writer := &xkeenArtifactWriter{destination: output, hash: digest, limit: MaxXKeenArchiveBytes}
	downloadErr := s.config.Downloader.DownloadXKeen(ctx, identity, writer)
	syncErr := output.Sync()
	closeErr := output.Close()
	if downloadErr != nil || syncErr != nil || closeErr != nil || writer.count != identity.SizeBytes || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), identity.SHA256) {
		_ = os.Remove(destinationPath)
		if downloadErr == nil && writer.count != identity.SizeBytes {
			return errXKeenArtifactSizeMismatch
		}
		if downloadErr == nil && !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), identity.SHA256) {
			return errXKeenArtifactHashMismatch
		}
		return ErrXKeenArtifactRejected
	}
	return nil
}

type xkeenArtifactWriter struct {
	destination io.Writer
	hash        hash.Hash
	limit       int64
	count       int64
}

func (w *xkeenArtifactWriter) Write(value []byte) (int, error) {
	if w == nil || w.destination == nil || w.hash == nil || w.limit <= 0 || w.count > w.limit-int64(len(value)) {
		return 0, errXKeenArchiveTooLarge
	}
	written, err := w.destination.Write(value)
	if err != nil || written != len(value) {
		return written, err
	}
	if _, err := w.hash.Write(value); err != nil {
		return written, err
	}
	w.count += int64(written)
	return written, nil
}

func (s *XKeenService) newStagingDir() (string, error) {
	if err := ensureXKeenOwnedDirectory(s.config.StagingDir, xkeenOwnerValue); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp(s.config.StagingDir, ".xkeen-transaction-")
	if err != nil {
		_ = s.removeEmptyStagingRoot()
		return "", err
	}
	if err := writeXKeenOwner(directory, xkeenOwnerValue); err != nil {
		_ = os.RemoveAll(directory)
		_ = s.removeEmptyStagingRoot()
		return "", err
	}
	return directory, nil
}

func (s *XKeenService) stagingPath() string     { return s.config.PreviousDir + xkeenPreviousStagingSuffix }
func (s *XKeenService) oldPreviousPath() string { return s.config.PreviousDir + xkeenPreviousOldSuffix }
func (s *XKeenService) markerStagingPath() string {
	return s.config.MarkerPath + xkeenMarkerStageSuffix
}

func (s *XKeenService) stagingPresent() bool {
	return s.previousStagingPresent() || s.componentStagingRootPresent() || s.pathExists(s.config.ActivationPath) || s.pathExists(s.markerStagingPath())
}

func (s *XKeenService) previousStagingPresent() bool { return s.pathExists(s.stagingPath()) }

func (s *XKeenService) componentStagingRootPresent() bool {
	pending, _, err := componentXKeenStagingState(s.config.StagingDir)
	return err != nil || pending
}

func (s *XKeenService) ownerOnlyStagingRoot() (bool, error) {
	_, ownerOnly, err := componentXKeenStagingState(s.config.StagingDir)
	if err != nil {
		return false, errXKeenActivationInvalid
	}
	return ownerOnly, nil
}

func (s *XKeenService) pathExists(filePath string) bool {
	info, err := os.Lstat(filePath)
	return err == nil && info != nil && info.Mode()&os.ModeSymlink == 0
}

func ensureXKeenOwnedDirectory(directory, owner string) error {
	if directory == "" || owner == "" {
		return errXKeenActivationInvalid
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	ownerPath := filepath.Join(directory, xkeenOwnerName)
	contents, err := os.ReadFile(ownerPath)
	if err == nil {
		if !bytes.Equal(contents, []byte(owner)) {
			return errXKeenActivationInvalid
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		return readErr
	}
	for _, entry := range entries {
		if entry.Name() != xkeenOwnerName {
			return errXKeenActivationInvalid
		}
	}
	return writeXKeenOwner(directory, owner)
}

func writeXKeenOwner(directory, owner string) error {
	if directory == "" || owner == "" {
		return errXKeenActivationInvalid
	}
	path := filepath.Join(directory, xkeenOwnerName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(owner)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errXKeenActivationInvalid
	}
	return nil
}

func validXKeenOwner(directory, owner string) error {
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errXKeenActivationInvalid
	}
	contents, err := readPrivateComponentFile(filepath.Join(directory, xkeenOwnerName), MaxSignalBytes)
	if err != nil || !bytes.Equal(contents, []byte(owner)) {
		return errXKeenActivationInvalid
	}
	return nil
}

func (s *XKeenService) removeOwned(filePath string) error {
	if filePath == "" {
		return nil
	}
	info, err := os.Lstat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return errXKeenActivationInvalid
	}
	if info.IsDir() {
		if err := validXKeenOwner(filePath, xkeenOwnerValue); err != nil {
			if err := validXKeenOwner(filePath, xkeenActivationOwnerValue); err != nil {
				return err
			}
		}
		return os.RemoveAll(filePath)
	}
	return errXKeenActivationInvalid
}

func (s *XKeenService) removeOwnedAndSync(filePath string) error {
	if err := s.removeOwned(filePath); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(filePath))
}

func (s *XKeenService) removeEmptyStagingRoot() error {
	info, err := os.Lstat(s.config.StagingDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errXKeenActivationInvalid
	}
	entries, err := os.ReadDir(s.config.StagingDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != xkeenOwnerName {
			return nil
		}
	}
	return s.removeOwnedAndSync(s.config.StagingDir)
}

func sameFileMode(info os.FileInfo, expected uint32) bool {
	return uint32(info.Mode().Perm()) == expected
}

func copyXKeenFile(source, destination string, expected xkeenGenerationEntry) error {
	if expected.Type != "file" || expected.Size < 0 || !isHexSHA256(expected.SHA256) {
		return errXKeenGenerationInvalid
	}
	before, err := os.Lstat(source)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() != expected.Size || !sameFileMode(before, expected.Mode) {
		return errXKeenGenerationInvalid
	}
	parentInfo, err := os.Lstat(filepath.Dir(destination))
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return errXKeenGenerationInvalid
	}
	input, err := os.Open(source)
	if err != nil {
		return errXKeenGenerationInvalid
	}
	opened, err := input.Stat()
	if err != nil || opened.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) {
		_ = input.Close()
		return errXKeenGenerationInvalid
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(expected.Mode))
	if err != nil {
		_ = input.Close()
		return errXKeenGenerationInvalid
	}
	digest := sha256.New()
	count, copyErr := copyXKeenBytes(context.Background(), io.MultiWriter(output, digest), input, MaxXKeenGenerationFileBytes)
	closeInputErr := input.Close()
	syncErr := output.Sync()
	closeErr := output.Close()
	after, afterErr := os.Lstat(source)
	if copyErr != nil || closeInputErr != nil || syncErr != nil || closeErr != nil || afterErr != nil || !os.SameFile(opened, after) || count != expected.Size || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), expected.SHA256) {
		_ = os.Remove(destination)
		return errXKeenGenerationInvalid
	}
	if err := os.Chmod(destination, os.FileMode(expected.Mode)); err != nil {
		_ = os.Remove(destination)
		return errXKeenGenerationInvalid
	}
	return nil
}

func copyXKeenDirectory(source, destination string, expected []xkeenGenerationEntry) error {
	moduleMode := uint32(0o755)
	for _, item := range expected {
		if item.Path == ".xkeen" {
			if item.Type != "directory" {
				return errXKeenGenerationInvalid
			}
			moduleMode = item.Mode
			break
		}
	}
	if err := ensureXKeenExtractDirectory(destination, moduleMode); err != nil {
		return err
	}
	for _, item := range expected {
		if item.Path == ".xkeen" || item.Path == "xkeen" {
			continue
		}
		if item.Type == "directory" {
			if err := ensureXKeenExtractDirectory(filepath.Join(destination, filepath.FromSlash(strings.TrimPrefix(item.Path, ".xkeen/"))), item.Mode); err != nil {
				return err
			}
		}
	}
	for _, item := range expected {
		if item.Type != "file" {
			continue
		}
		if item.Path == "xkeen" {
			continue
		}
		if !strings.HasPrefix(item.Path, ".xkeen/") {
			return errXKeenGenerationInvalid
		}
		relative := strings.TrimPrefix(item.Path, ".xkeen/")
		destinationPath := filepath.Join(destination, filepath.FromSlash(relative))
		if err := copyXKeenFile(filepath.Join(source, filepath.FromSlash(relative)), destinationPath, item); err != nil {
			return err
		}
	}
	return nil
}

func copyXKeenGeneration(sourceBinary, sourceModule, destination string, expected xkeenGenerationMetadata) error {
	if !validXKeenGenerationMetadata(expected) {
		return errXKeenGenerationInvalid
	}
	if err := ensureXKeenExtractDirectory(destination, 0o700); err != nil {
		return err
	}
	var binaryEntry xkeenGenerationEntry
	for _, entry := range expected.Entries {
		if entry.Path == "xkeen" {
			binaryEntry = entry
			break
		}
	}
	if err := copyXKeenFile(sourceBinary, filepath.Join(destination, "xkeen"), binaryEntry); err != nil {
		return err
	}
	if err := copyXKeenDirectory(sourceModule, filepath.Join(destination, ".xkeen"), expected.Entries); err != nil {
		return err
	}
	actual, err := readXKeenGeneration(filepath.Join(destination, "xkeen"), filepath.Join(destination, ".xkeen"))
	if err != nil || !sameXKeenGeneration(actual, expected) {
		return errXKeenGenerationInvalid
	}
	return nil
}

func writeXKeenPreviousMetadata(path string, meta xkeenGenerationMetadata, marker []byte, syncDir func(string) error) error {
	if !validXKeenGenerationMetadata(meta) {
		return errXKeenPreviousInvalid
	}
	record := xkeenPreviousRecord{SchemaVersion: XKeenTransactionSchemaVersion, Generation: meta.Generation, Entries: meta.Entries, Bytes: meta.Bytes, MarkerPresent: len(marker) > 0}
	if len(marker) > 0 {
		parsed, err := parseXKeenMarker(marker)
		if err != nil {
			return errXKeenPreviousInvalid
		}
		record.Marker = parsed
		info := len(marker)
		if info > MaxXKeenMarkerBytes {
			return errXKeenPreviousInvalid
		}
		hashValue := sha256.Sum256(marker)
		record.MarkerSHA256 = hex.EncodeToString(hashValue[:])
	}
	contents, err := json.Marshal(record)
	if err != nil || len(contents)+1 > MaxXKeenPreviousMetadata {
		return errXKeenPreviousInvalid
	}
	return writeAtomicComponentFile(path, append(contents, '\n'), 0o600, syncDir)
}

func (s *XKeenService) savePreviousGeneration(base xkeenBaseSnapshot) (string, error) {
	if !validXKeenGenerationMetadata(base.active) {
		return "", errXKeenPreviousInvalid
	}
	if err := ensurePrivateDirectory(filepath.Dir(s.config.PreviousDir)); err != nil {
		return "", err
	}
	staging := s.stagingPath()
	if err := s.removeOwned(staging); err != nil {
		return "", err
	}
	if err := ensureXKeenOwnedDirectory(staging, xkeenOwnerValue); err != nil {
		return "", err
	}
	if err := copyXKeenGeneration(s.config.ActiveBinaryPath, s.config.ModuleDir, staging, base.active); err != nil {
		_ = s.removeOwned(staging)
		return "", err
	}
	if len(base.marker) > 0 {
		if err := copyBytesToOwnedFile(filepath.Join(staging, xkeenPreviousMarkerName), base.marker, 0o600); err != nil {
			_ = s.removeOwned(staging)
			return "", err
		}
	}
	if err := writeXKeenPreviousMetadata(filepath.Join(staging, xkeenPreviousMetadataName), base.active, base.marker, s.config.SyncDirectory); err != nil {
		_ = s.removeOwned(staging)
		return "", err
	}
	return staging, nil
}

func copyBytesToOwnedFile(destination string, contents []byte, mode os.FileMode) error {
	if len(contents) == 0 || int64(len(contents)) > MaxXKeenMarkerBytes {
		return errXKeenPreviousInvalid
	}
	if err := ensurePrivateDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errXKeenPreviousInvalid
	}
	return nil
}

const (
	maxXKeenPreservedEntries   = 8192
	maxXKeenPreservedFileBytes = MaxGeodataFileBytes
	maxXKeenPreservedRootBytes = MaxGeodataCandidateBytes
	// Bound the complete preserved snapshot to one geodata-sized root, one
	// Xray-sized binary and one additional bounded control tree. Individual
	// roots still use their owning component limits below.
	maxXKeenPreservedBytes = maxXKeenPreservedRootBytes + MaxXrayCandidateBinaryBytes + maxXKeenPreservedRootBytes
)

func (s *XKeenService) preservedFingerprint() (string, error) {
	configuredPaths := append([]string(nil), s.config.PreservedPaths...)
	configuredPaths = append(configuredPaths, s.config.LifecycleInitPath, s.config.LegacyInitPath, s.config.SiblingModulePath, s.config.InstallHelperPath)
	paths := make([]string, 0, len(configuredPaths))
	seen := make(map[string]struct{}, len(configuredPaths))
	for _, configured := range configuredPaths {
		configured = filepath.Clean(strings.TrimSpace(configured))
		if configured == "." || configured == "" || !filepath.IsAbs(configured) {
			return "", errXKeenPreservedChanged
		}
		if _, ok := seen[configured]; ok {
			continue
		}
		seen[configured] = struct{}{}
		paths = append(paths, configured)
	}
	sort.Strings(paths)
	destination := sha256.New()
	state := xkeenPreservedHashState{}
	for _, configured := range paths {
		limits := s.preservedLimits(configured)
		state.rootBytes = 0
		state.maxFileBytes = limits.maxFileBytes
		state.maxRootBytes = limits.maxRootBytes
		if err := hashXKeenPreservedPath(destination, configured, configured, &state); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(destination.Sum(nil)), nil
}

type xkeenPreservedHashState struct {
	entries      int
	bytes        int64
	rootBytes    int64
	maxFileBytes int64
	maxRootBytes int64
}

type xkeenPreservedLimits struct {
	maxFileBytes int64
	maxRootBytes int64
}

func (s *XKeenService) preservedLimits(path string) xkeenPreservedLimits {
	path = filepath.Clean(path)
	if path == filepath.Clean(s.config.XrayBinaryPath) {
		return xkeenPreservedLimits{maxFileBytes: MaxXrayCandidateBinaryBytes, maxRootBytes: MaxXrayCandidateBinaryBytes}
	}
	if path == filepath.Clean(s.config.XrayAssetDir) {
		return xkeenPreservedLimits{maxFileBytes: MaxGeodataFileBytes, maxRootBytes: MaxGeodataCandidateBytes}
	}
	return xkeenPreservedLimits{maxFileBytes: maxXKeenPreservedFileBytes, maxRootBytes: maxXKeenPreservedRootBytes}
}

func hashXKeenPreservedPath(destination io.Writer, absolute, logical string, state *xkeenPreservedHashState) error {
	if state == nil || state.entries >= maxXKeenPreservedEntries {
		return errXKeenPreservedChanged
	}
	if state.maxFileBytes <= 0 {
		state.maxFileBytes = maxXKeenPreservedFileBytes
	}
	if state.maxRootBytes <= 0 {
		state.maxRootBytes = maxXKeenPreservedRootBytes
	}
	state.entries++
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		for _, value := range []string{logical, "missing", "0", "0", ""} {
			if err := writeXKeenHashPart(destination, value); err != nil {
				return err
			}
		}
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return errXKeenPreservedChanged
	}
	if info.IsDir() {
		for _, value := range []string{logical, "directory", fmt.Sprintf("%o", info.Mode().Perm()), "0", ""} {
			if err := writeXKeenHashPart(destination, value); err != nil {
				return err
			}
		}
		children, err := os.ReadDir(absolute)
		if err != nil {
			return errXKeenPreservedChanged
		}
		for _, child := range children {
			if child.Name() == "" || strings.ContainsAny(child.Name(), `/\\`) {
				return errXKeenPreservedChanged
			}
			if err := hashXKeenPreservedPath(destination, filepath.Join(absolute, child.Name()), filepath.ToSlash(filepath.Join(logical, child.Name())), state); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > state.maxFileBytes ||
		state.rootBytes > state.maxRootBytes-info.Size() || state.bytes > maxXKeenPreservedBytes-info.Size() {
		return errXKeenPreservedChanged
	}
	digest, count, err := hashXKeenFile(absolute, info.Size(), state.maxFileBytes)
	if err != nil {
		return errXKeenPreservedChanged
	}
	state.bytes += count
	state.rootBytes += count
	for _, value := range []string{logical, "file", fmt.Sprintf("%o", info.Mode().Perm()), fmt.Sprintf("%d", count), digest} {
		if err := writeXKeenHashPart(destination, value); err != nil {
			return err
		}
	}
	return nil
}

func xkeenCatalogGenerationBytes(entry xkeenCompatibilityEntry) (int64, error) {
	if !entry.Installable || validateXKeenCompatibilityEntry(entry) != nil {
		return 0, errXKeenFreeSpaceInsufficient
	}
	var total int64
	for _, member := range entry.ArchiveMembers {
		if member.Type != xkeenArchiveRegular || member.Size < 0 || member.Size > MaxXKeenArchiveMemberBytes || total > MaxXKeenGenerationBytes-member.Size {
			return 0, errXKeenFreeSpaceInsufficient
		}
		total += member.Size
	}
	if total <= 0 {
		return 0, errXKeenFreeSpaceInsufficient
	}
	return total, nil
}

type xkeenFreeSpaceRequirement struct {
	directory string
	bytes     uint64
}

type xkeenFreeSpaceGroup struct {
	directory string
	bytes     uint64
}

func (s *XKeenService) checkFreeSpace(archiveSize, candidateSize, previousSize int64) error {
	if archiveSize < 0 || archiveSize > MaxXKeenArchiveBytes || candidateSize <= 0 || candidateSize > MaxXKeenGenerationBytes || previousSize <= 0 || previousSize > MaxXKeenGenerationBytes {
		return errXKeenFreeSpaceInsufficient
	}
	requirements := []xkeenFreeSpaceRequirement{
		{directory: existingDirectory(filepath.Dir(s.config.StagingDir)), bytes: uint64(archiveSize) + uint64(candidateSize)},
		{directory: existingDirectory(filepath.Dir(s.config.PreviousDir)), bytes: uint64(previousSize)},
		{directory: existingDirectory(filepath.Dir(s.config.ActiveBinaryPath)), bytes: uint64(candidateSize)},
		{directory: existingDirectory(filepath.Dir(s.config.MarkerPath))},
	}
	groups := make([]xkeenFreeSpaceGroup, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement.directory == "" {
			return errXKeenFreeSpaceUnavailable
		}
		groupIndex := -1
		for index, group := range groups {
			same, err := sameFilesystem(group.directory, requirement.directory)
			if err != nil {
				return errXKeenFreeSpaceUnavailable
			}
			if same {
				groupIndex = index
				break
			}
		}
		if groupIndex < 0 {
			groups = append(groups, xkeenFreeSpaceGroup{directory: requirement.directory, bytes: requirement.bytes})
			continue
		}
		if requirement.bytes > ^uint64(0)-groups[groupIndex].bytes {
			return errXKeenFreeSpaceInsufficient
		}
		groups[groupIndex].bytes += requirement.bytes
	}
	const reserve = uint64(8 << 20)
	for _, group := range groups {
		if reserve > ^uint64(0)-group.bytes {
			return errXKeenFreeSpaceInsufficient
		}
		need := group.bytes + reserve
		available, err := s.config.AvailableSpace(group.directory)
		if err != nil {
			return errXKeenFreeSpaceUnavailable
		}
		if available < need {
			return errXKeenFreeSpaceInsufficient
		}
	}
	return nil
}

func (s *XKeenService) copyGenerationToCandidate(previous loadedXKeenGeneration) (string, error) {
	directory, err := s.newStagingDir()
	if err != nil {
		return "", err
	}
	if err := copyXKeenGeneration(filepath.Join(previous.path, "xkeen"), filepath.Join(previous.path, ".xkeen"), directory, previous.meta); err != nil {
		_ = s.removeOwned(directory)
		return "", err
	}
	if len(previous.marker) > 0 {
		if err := copyBytesToOwnedFile(filepath.Join(directory, xkeenPreviousMarkerName), previous.marker, 0o600); err != nil {
			_ = s.removeOwned(directory)
			return "", err
		}
	}
	return directory, nil
}

func (s *XKeenService) loadPreviousGeneration() (loadedXKeenGeneration, error) {
	return s.loadGeneration(s.config.PreviousDir)
}

func (s *XKeenService) loadGeneration(directory string) (loadedXKeenGeneration, error) {
	if err := validXKeenOwner(directory, xkeenOwnerValue); err != nil {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	metadataContents, err := readPrivateComponentFile(filepath.Join(directory, xkeenPreviousMetadataName), MaxXKeenPreviousMetadata)
	if err != nil {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(metadataContents))
	decoder.DisallowUnknownFields()
	var record xkeenPreviousRecord
	if err := decoder.Decode(&record); err != nil {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || record.SchemaVersion != XKeenTransactionSchemaVersion {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	meta := xkeenGenerationMetadata{Generation: record.Generation, Entries: record.Entries, Bytes: record.Bytes}
	if !validXKeenGenerationMetadata(meta) {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	actual, err := readXKeenGeneration(filepath.Join(directory, "xkeen"), filepath.Join(directory, ".xkeen"))
	if err != nil || !sameXKeenGeneration(actual, meta) {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	allowed := map[string]bool{xkeenOwnerName: true, xkeenPreviousMetadataName: true, "xkeen": true, ".xkeen": true}
	var marker []byte
	if record.MarkerPresent {
		allowed[xkeenPreviousMarkerName] = true
		marker, err = readPrivateComponentFile(filepath.Join(directory, xkeenPreviousMarkerName), MaxXKeenMarkerBytes)
		if err != nil {
			return loadedXKeenGeneration{}, errXKeenPreviousInvalid
		}
		parsed, parseErr := parseXKeenMarker(marker)
		if parseErr != nil || !strings.EqualFold(parsed.GenerationSHA256, meta.Generation) || parsed != record.Marker {
			return loadedXKeenGeneration{}, errXKeenPreviousInvalid
		}
		hashValue := sha256.Sum256(marker)
		if record.MarkerSHA256 == "" || !strings.EqualFold(record.MarkerSHA256, hex.EncodeToString(hashValue[:])) {
			return loadedXKeenGeneration{}, errXKeenPreviousInvalid
		}
	} else if record.MarkerSHA256 != "" || record.Marker != (xkeenMarkerRecord{}) {
		return loadedXKeenGeneration{}, errXKeenPreviousInvalid
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return loadedXKeenGeneration{}, errXKeenPreviousInvalid
		}
	}
	return loadedXKeenGeneration{path: directory, meta: meta, marker: marker}, nil
}

func (s *XKeenService) prepareActivation(marker []byte) error {
	parent := filepath.Dir(s.config.ActiveBinaryPath)
	if filepath.Dir(s.config.ModuleDir) != parent || filepath.Dir(s.config.ActivationPath) != parent || filepath.Clean(s.config.ActivationPath) == parent {
		return errXKeenActivationInvalid
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return errXKeenActivationInvalid
	}
	if info, err := os.Lstat(s.config.ActiveBinaryPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errXKeenActivationInvalid
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if info, err := os.Lstat(s.config.ModuleDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errXKeenActivationInvalid
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if info, err := os.Lstat(s.config.MarkerPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errXKeenActivationInvalid
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if info, err := os.Lstat(s.markerStagingPath()); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errXKeenActivationInvalid
		}
		staged, readErr := readPrivateComponentFile(s.markerStagingPath(), MaxXKeenMarkerBytes)
		if readErr != nil {
			return errXKeenActivationInvalid
		}
		if len(marker) > 0 {
			if !bytes.Equal(staged, marker) {
				return errXKeenActivationInvalid
			}
		} else if _, parseErr := parseXKeenMarker(staged); parseErr != nil {
			return errXKeenActivationInvalid
		}
		if err := os.Remove(s.markerStagingPath()); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if info, err := os.Lstat(s.config.ActivationPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errXKeenActivationInvalid
		}
		if err := validXKeenOwner(s.config.ActivationPath, xkeenActivationOwnerValue); err != nil {
			return err
		}
		entries, readErr := os.ReadDir(s.config.ActivationPath)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			switch entry.Name() {
			case xkeenOwnerName, "new", "old-xkeen", "old-module":
			default:
				return errXKeenActivationInvalid
			}
			childPath := filepath.Join(s.config.ActivationPath, entry.Name())
			switch entry.Name() {
			case "new":
				if err := validXKeenOwner(childPath, xkeenOwnerValue); err != nil {
					return err
				}
				if err := validXkeenActivationPayload(childPath); err != nil {
					return err
				}
			case "old-xkeen":
				info, infoErr := os.Lstat(childPath)
				if infoErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > MaxXKeenGenerationBytes {
					return errXKeenActivationInvalid
				}
			case "old-module":
				if err := validXkeenActivationPayload(childPath); err != nil {
					return err
				}
			}
		}
		if err := s.removeOwned(s.config.ActivationPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if err := ensureXKeenOwnedDirectory(s.config.ActivationPath, xkeenActivationOwnerValue); err != nil {
		return err
	}
	if same, err := sameFilesystem(parent, s.config.ActivationPath); err != nil || !same {
		return errXKeenActivationInvalid
	}
	return nil
}

func (s *XKeenService) activateGeneration(ctx context.Context, source string, expected xkeenGenerationMetadata, marker []byte, inject bool) error {
	if !validXKeenGenerationMetadata(expected) || source == "" {
		return errXKeenGenerationInvalid
	}
	actual, err := readXKeenGeneration(filepath.Join(source, "xkeen"), filepath.Join(source, ".xkeen"))
	if err != nil || !sameXKeenGeneration(actual, expected) {
		return errXKeenGenerationInvalid
	}
	if len(marker) > 0 {
		record, markerErr := parseXKeenMarker(marker)
		if markerErr != nil || !strings.EqualFold(record.GenerationSHA256, expected.GenerationSHA256()) {
			return errXKeenMarkerInvalid
		}
	}
	if err := s.prepareActivation(marker); err != nil {
		return err
	}
	activation := s.config.ActivationPath
	newGeneration := filepath.Join(activation, "new")
	if _, err := os.Lstat(newGeneration); err == nil {
		if err := validXKeenOwner(newGeneration, xkeenOwnerValue); err != nil {
			return err
		}
		if err := s.removeOwned(newGeneration); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if err := ensureXKeenOwnedDirectory(newGeneration, xkeenOwnerValue); err != nil {
		return err
	}
	if err := copyXKeenGeneration(filepath.Join(source, "xkeen"), filepath.Join(source, ".xkeen"), newGeneration, expected); err != nil {
		return err
	}
	oldBinary := filepath.Join(activation, "old-xkeen")
	oldModule := filepath.Join(activation, "old-module")
	if err := s.removeActivationPath(oldBinary); err != nil {
		return err
	}
	if err := s.removeActivationPath(oldModule); err != nil {
		return err
	}
	if _, err := os.Lstat(s.config.ActiveBinaryPath); err == nil {
		if err := os.Rename(s.config.ActiveBinaryPath, oldBinary); err != nil {
			return errXKeenActivationInvalid
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if err := os.Rename(filepath.Join(newGeneration, "xkeen"), s.config.ActiveBinaryPath); err != nil {
		return errXKeenActivationInvalid
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.ActiveBinaryPath)); err != nil {
		return err
	}
	if inject {
		if err := s.updateJournalPhase(xkeenPhaseXkeenCommitted); err != nil {
			return err
		}
		if err := s.inject(XKeenStageXkeenCommitted); err != nil {
			return err
		}
	} else if err := s.updateJournalPhase(xkeenPhaseXkeenCommitted); err != nil {
		return err
	}
	if _, err := os.Lstat(s.config.ModuleDir); err == nil {
		if err := os.Rename(s.config.ModuleDir, oldModule); err != nil {
			return errXKeenActivationInvalid
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errXKeenActivationInvalid
	}
	if err := os.Rename(filepath.Join(newGeneration, ".xkeen"), s.config.ModuleDir); err != nil {
		return errXKeenActivationInvalid
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.ModuleDir)); err != nil {
		return err
	}
	if inject {
		if err := s.updateJournalPhase(xkeenPhaseModuleCommitted); err != nil {
			return err
		}
		if err := s.inject(XKeenStageModuleCommitted); err != nil {
			return err
		}
	} else if err := s.updateJournalPhase(xkeenPhaseModuleCommitted); err != nil {
		return err
	}
	if len(marker) > 0 {
		if err := writeAtomicComponentFile(s.markerStagingPath(), marker, 0o600, s.config.SyncDirectory); err != nil {
			return errXKeenMarkerInvalid
		}
		if err := os.Rename(s.markerStagingPath(), s.config.MarkerPath); err != nil {
			return errXKeenMarkerInvalid
		}
		if err := s.config.SyncDirectory(filepath.Dir(s.config.MarkerPath)); err != nil {
			return err
		}
	} else {
		if _, err := os.Lstat(s.config.MarkerPath); err == nil {
			if _, _, _, markerErr := readXKeenMarker(s.config.MarkerPath); markerErr != nil {
				return errXKeenMarkerInvalid
			}
			if err := os.Remove(s.config.MarkerPath); err != nil {
				return err
			}
			if err := s.config.SyncDirectory(filepath.Dir(s.config.MarkerPath)); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return errXKeenMarkerInvalid
		}
	}
	if err := s.updateJournalPhase(xkeenPhaseGenerationCommitted); err != nil {
		return err
	}
	return nil
}

func (m xkeenGenerationMetadata) GenerationSHA256() string { return m.Generation }

func (s *XKeenService) removeActivationPath(filePath string) error {
	info, err := os.Lstat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return errXKeenActivationInvalid
	}
	if info.IsDir() {
		if err := validXkeenActivationPayload(filePath); err != nil {
			return err
		}
		return os.RemoveAll(filePath)
	}
	if !info.Mode().IsRegular() {
		return errXKeenActivationInvalid
	}
	return os.Remove(filePath)
}

func validXkeenActivationPayload(root string) error {
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errXKeenActivationInvalid
	}
	entries := 0
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.Mode()&os.ModeSymlink != 0 {
			return errXKeenActivationInvalid
		}
		entries++
		if entries > MaxXKeenGenerationEntries || (!info.IsDir() && !info.Mode().IsRegular()) || info.IsDir() && info.Mode().Perm() == 0 {
			return errXKeenActivationInvalid
		}
		if !info.IsDir() && info.Size() > MaxXKeenGenerationBytes {
			return errXKeenActivationInvalid
		}
		return nil
	})
}

func (s *XKeenService) promotePreviousGeneration() error {
	if _, err := s.loadGeneration(s.stagingPath()); err != nil {
		return err
	}
	parent := filepath.Dir(s.config.PreviousDir)
	if err := ensurePrivateDirectory(parent); err != nil {
		return err
	}
	if err := s.removeOwned(s.oldPreviousPath()); err != nil {
		return err
	}
	if info, err := os.Lstat(s.config.PreviousDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errXKeenPreviousInvalid
		}
		if _, err := s.loadGeneration(s.config.PreviousDir); err != nil {
			return err
		}
		if err := os.Rename(s.config.PreviousDir, s.oldPreviousPath()); err != nil {
			return err
		}
		if err := s.config.SyncDirectory(parent); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(s.stagingPath(), s.config.PreviousDir); err != nil {
		if _, oldErr := os.Lstat(s.oldPreviousPath()); oldErr == nil {
			_ = os.Rename(s.oldPreviousPath(), s.config.PreviousDir)
		}
		return err
	}
	return s.config.SyncDirectory(parent)
}

func (s *XKeenService) settlePreviousGeneration() error {
	if err := s.removeOwned(s.oldPreviousPath()); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(s.config.PreviousDir))
}

func (s *XKeenService) verifyRuntime(ctx context.Context, expected xkeenGenerationMetadata, expectedMarker []byte, expectedXray xrayBinaryMetadata, expectedAuthority XrayAuthoritySnapshot, expectedPreserved string) error {
	if err := s.validateXKeenLifecycle(); err != nil {
		return err
	}
	active, marker, _, err := s.readActiveGeneration()
	if err != nil || !sameXKeenGeneration(active, expected) || !bytes.Equal(marker, expectedMarker) {
		return errXKeenGenerationInvalid
	}
	xray, err := binaryMetadata(s.config.XrayBinaryPath, expectedXray.Version, s.config.CandidateProbe, ctx)
	if err != nil || !sameBinaryMetadata(xray, expectedXray) {
		return errXrayBinaryInvalid
	}
	if current, err := s.captureAuthorityHeld(ctx); err != nil || current.Generation != expectedAuthority.Generation {
		return errXKeenAuthorityChanged
	}
	if preserved, err := s.preservedFingerprint(); err != nil || !strings.EqualFold(preserved, expectedPreserved) {
		return errXKeenPreservedChanged
	}
	if s.config.Runtime == nil {
		return ErrXKeenTransactionUnavailable
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
	if err := s.config.Runtime.Verify(ctx, expectedOutboundTags(expectedAuthority.Registry)); err != nil {
		return err
	}
	active, marker, _, err = s.readActiveGeneration()
	if err != nil || !sameXKeenGeneration(active, expected) || !bytes.Equal(marker, expectedMarker) {
		return errXKeenGenerationInvalid
	}
	xray, err = binaryMetadata(s.config.XrayBinaryPath, expectedXray.Version, s.config.CandidateProbe, ctx)
	if err != nil || !sameBinaryMetadata(xray, expectedXray) {
		return errXrayBinaryInvalid
	}
	current, err := s.captureAuthorityHeld(ctx)
	if err != nil || current.Generation != expectedAuthority.Generation {
		return errXKeenAuthorityChanged
	}
	preserved, err := s.preservedFingerprint()
	if err != nil || !strings.EqualFold(preserved, expectedPreserved) {
		return errXKeenPreservedChanged
	}
	return nil
}

func (s *XKeenService) restoreRuntime(ctx context.Context, source string, expected xkeenGenerationMetadata, marker []byte, expectedXray xrayBinaryMetadata, expectedAuthority XrayAuthoritySnapshot, expectedPreserved string) error {
	if err := s.activateGeneration(ctx, source, expected, marker, false); err != nil {
		return err
	}
	return s.verifyRuntime(ctx, expected, marker, expectedXray, expectedAuthority, expectedPreserved)
}

func (s *XKeenService) runCommitted(ctx context.Context, operation string, base xkeenBaseSnapshot, candidatePath string, candidate xkeenGenerationMetadata, marker []byte, candidateValidated bool) error {
	if s.config.Runtime == nil || s.config.CandidateProbe == nil || s.config.CandidateValidator == nil {
		return ErrXKeenTransactionUnavailable
	}
	if operation != xkeenOperationUpdate && operation != xkeenOperationRollback {
		return ErrXKeenCandidateRejected
	}
	current, err := s.captureHeld(ctx)
	if err != nil {
		return ErrXKeenAuthorityUnavailable
	}
	if !sameXKeenBase(current, base) {
		return ErrXKeenCandidateStale
	}
	base = current
	if !validXKeenGenerationMetadata(candidate) || candidatePath == "" {
		return ErrXKeenCandidateRejected
	}
	if operation == xkeenOperationUpdate && len(marker) == 0 {
		_, generatedMarker, markerErr := markerForGeneration(candidate.Generation)
		if markerErr != nil {
			return ErrXKeenCandidateRejected
		}
		marker = generatedMarker
	}
	if len(marker) > 0 {
		record, markerErr := parseXKeenMarker(marker)
		if markerErr != nil || !strings.EqualFold(record.GenerationSHA256, candidate.Generation) {
			return ErrXKeenCandidateRejected
		}
	}
	if !candidateValidated {
		if err := s.validateLocalCandidate(ctx, candidatePath, base.authority); err != nil {
			return ErrXKeenCandidateRejected
		}
	}
	stagePath, err := s.savePreviousGeneration(base)
	if err != nil {
		if s.previousStagingPresent() {
			s.failClosed()
			s.markNotReady(ErrXKeenRecoveryRequired)
		}
		return fmt.Errorf("%w: save previous: %v", ErrXKeenApplyFailed, err)
	}
	if err := s.inject(XKeenStagePreviousStaging); err != nil {
		s.failClosed()
		s.markNotReady(ErrXKeenRecoveryRequired)
		return ErrXKeenApplyFailed
	}
	journal := xkeenTransactionJournal{
		SchemaVersion: XKeenTransactionSchemaVersion, Component: string(KindXKeen), Operation: operation, Phase: xkeenPhasePrepared,
		Previous: makeXKeenGenerationSummary(base.active, base.marker), Candidate: makeXKeenGenerationSummary(candidate, marker),
		AuthorityGeneration: hex.EncodeToString(base.authority.Generation[:]), PreservedFingerprint: base.preservedFingerprint, Xray: base.xray,
	}
	if err := s.writeJournal(journal); err != nil {
		if present, presentErr := componentTransactionPresent(s.config.JournalPath); presentErr != nil || present && s.clearJournal() != nil {
			return s.recoveryFailure()
		}
		if cleanupErr := s.removeOwned(stagePath); cleanupErr != nil {
			return s.recoveryFailure()
		}
		_ = s.removeEmptyStagingRoot()
		return ErrXKeenApplyFailed
	}
	if err := s.inject(XKeenStagePreviousSaved); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	if err := s.inject(XKeenStageJournalPrepared); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	if err := s.activateGeneration(ctx, candidatePath, candidate, marker, true); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	journal.Phase = xkeenPhaseGenerationCommitted
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	if err := s.inject(XKeenStageFilesCommitted); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	activationContext, cancel := context.WithTimeout(ctx, s.config.ActivationTimeout)
	verifyErr := s.verifyRuntime(activationContext, candidate, marker, base.xray, base.authority, base.preservedFingerprint)
	cancel()
	if verifyErr != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, verifyErr)
	}
	if err := s.promotePreviousGeneration(); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	if err := s.inject(XKeenStagePreviousSettled); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	journal.Phase = xkeenPhaseRuntimeVerified
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	if err := s.settlePreviousGeneration(); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, marker, err)
	}
	if err := s.inject(XKeenStageRuntimeVerified); err != nil {
		return s.failClosedResult()
	}
	if err := s.removeOwnedAndSync(s.config.ActivationPath); err != nil {
		return s.failClosedResult()
	}
	if err := s.inject(XKeenStageJournalCleared); err != nil {
		return s.failClosedResult()
	}
	if err := s.clearJournal(); err != nil {
		return s.failClosedResult()
	}
	if err := s.removeOwned(stagePath); err != nil { /* promotion moved it; absence is expected */
		_ = err
	}
	if err := s.removeOwned(candidatePath); err != nil { /* caller-owned candidate may be cleaned by its defer */
		_ = err
	}
	_ = s.removeOwnedAndSync(s.config.MarkerPath + xkeenMarkerStageSuffix)
	_ = s.removeEmptyStagingRoot()
	return nil
}

func (s *XKeenService) failAndRecover(ctx context.Context, journal xkeenTransactionJournal, base xkeenBaseSnapshot, stagePath, candidatePath string, marker []byte, _ error) error {
	rollbackContext, cancel := context.WithTimeout(context.Background(), s.config.RollbackTimeout)
	defer cancel()
	previous, err := s.loadJournalPrevious(journal.Previous)
	if err != nil {
		return s.recoveryFailure()
	}
	if err := s.restoreRuntime(rollbackContext, previous.path, previous.meta, previous.marker, base.xray, base.authority, base.preservedFingerprint); err != nil {
		return s.recoveryFailure()
	}
	if err := s.restorePreviousAfterFailure(journal); err != nil {
		return s.recoveryFailure()
	}
	if err := s.cleanupResidue(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.clearJournal(); err != nil {
		return s.recoveryFailure()
	}
	if journal.Operation == xkeenOperationRollback {
		return ErrXKeenRollbackFailed
	}
	return ErrXKeenApplyFailed
}

func (s *XKeenService) loadJournalPrevious(expected xkeenGenerationSummary) (loadedXKeenGeneration, error) {
	for _, directory := range []string{s.stagingPath(), s.config.PreviousDir, s.oldPreviousPath()} {
		generation, err := s.loadGeneration(directory)
		if err == nil && sameXKeenLoadedSummary(generation, expected) {
			return generation, nil
		}
	}
	return loadedXKeenGeneration{}, errXKeenPreviousInvalid
}

func sameXKeenSummary(meta xkeenGenerationMetadata, summary xkeenGenerationSummary) bool {
	return validXKeenGenerationSummary(summary) && strings.EqualFold(meta.Generation, summary.Generation) && len(meta.Entries) == summary.Entries && meta.Bytes == summary.Bytes
}

func sameXKeenLoadedSummary(generation loadedXKeenGeneration, summary xkeenGenerationSummary) bool {
	return sameXKeenSummary(generation.meta, summary) && summary.MarkerPresent == (len(generation.marker) > 0) && (!summary.MarkerPresent || func() bool {
		digest := sha256.Sum256(generation.marker)
		return strings.EqualFold(hex.EncodeToString(digest[:]), summary.MarkerSHA256)
	}())
}

func (s *XKeenService) restorePreviousAfterFailure(journal xkeenTransactionJournal) error {
	oldInfo, err := os.Lstat(s.oldPreviousPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || oldInfo.Mode()&os.ModeSymlink != 0 || !oldInfo.IsDir() {
		return errXKeenPreviousInvalid
	}
	if _, err := s.loadGeneration(s.oldPreviousPath()); err != nil {
		return err
	}
	if journal.Operation == xkeenOperationRollback {
		if current, err := os.Lstat(s.config.PreviousDir); err == nil {
			if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() {
				return errXKeenPreviousInvalid
			}
			if err := s.removeOwned(s.config.PreviousDir); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(s.oldPreviousPath(), s.config.PreviousDir); err != nil {
			return err
		}
		return s.config.SyncDirectory(filepath.Dir(s.config.PreviousDir))
	}
	return s.removeOwnedAndSync(s.oldPreviousPath())
}

func (s *XKeenService) cleanupResidue() error {
	if err := s.removeOwned(s.config.ActivationPath); err != nil {
		return err
	}
	if err := s.removeMarkerStage(); err != nil {
		return err
	}
	if err := s.removeOwned(s.stagingPath()); err != nil {
		return err
	}
	if err := s.removeOwned(s.config.StagingDir); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.StagingDir)); err != nil {
		return err
	}
	return nil
}

func (s *XKeenService) removeMarkerStage() error {
	info, err := os.Lstat(s.markerStagingPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errXKeenMarkerInvalid
	}
	contents, readErr := readPrivateComponentFile(s.markerStagingPath(), MaxXKeenMarkerBytes)
	if readErr != nil {
		return errXKeenMarkerInvalid
	}
	if _, parseErr := parseXKeenMarker(contents); parseErr != nil {
		return errXKeenMarkerInvalid
	}
	if err := os.Remove(s.markerStagingPath()); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(s.markerStagingPath()))
}

func validateXKeenJournal(journal xkeenTransactionJournal) error {
	if journal.SchemaVersion != XKeenTransactionSchemaVersion || journal.Component != string(KindXKeen) || (journal.Operation != xkeenOperationUpdate && journal.Operation != xkeenOperationRollback) {
		return errXKeenJournalInvalid
	}
	switch journal.Phase {
	case xkeenPhasePrepared, xkeenPhaseXkeenCommitted, xkeenPhaseModuleCommitted, xkeenPhaseGenerationCommitted, xkeenPhaseFilesCommitted, xkeenPhaseRuntimeVerified:
	default:
		return errXKeenJournalInvalid
	}
	if !validXKeenGenerationSummary(journal.Previous) || !validXKeenGenerationSummary(journal.Candidate) || !isHexSHA256(journal.AuthorityGeneration) || !isHexSHA256(journal.PreservedFingerprint) || !validXrayBinaryMetadata(journal.Xray, true) {
		return errXKeenJournalInvalid
	}
	return nil
}

func (s *XKeenService) writeJournal(journal xkeenTransactionJournal) error {
	if err := validateXKeenJournal(journal); err != nil {
		return err
	}
	contents, err := json.Marshal(journal)
	if err != nil || len(contents)+1 > MaxComponentJournalBytes {
		return errXKeenJournalInvalid
	}
	return writeAtomicComponentFile(s.config.JournalPath, append(contents, '\n'), 0o600, s.config.SyncDirectory)
}

func (s *XKeenService) updateJournalPhase(phase string) error {
	journal, present, err := s.readJournal()
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	journal.Phase = phase
	return s.writeJournal(journal)
}

func (s *XKeenService) readJournal() (xkeenTransactionJournal, bool, error) {
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return xkeenTransactionJournal{}, false, errXKeenJournalInvalid
	}
	if !present {
		return xkeenTransactionJournal{}, false, nil
	}
	if kind != KindXKeen {
		return xkeenTransactionJournal{}, false, errXKeenJournalInvalid
	}
	contents, err := readPrivateComponentFile(s.config.JournalPath, MaxComponentJournalBytes)
	if err != nil {
		return xkeenTransactionJournal{}, false, errXKeenJournalInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var journal xkeenTransactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return xkeenTransactionJournal{}, false, errXKeenJournalInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || validateXKeenJournal(journal) != nil {
		return xkeenTransactionJournal{}, false, errXKeenJournalInvalid
	}
	return journal, true, nil
}

func (s *XKeenService) clearJournal() error {
	info, err := os.Lstat(s.config.JournalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return errXKeenJournalInvalid
	}
	contents, err := readPrivateComponentFile(s.config.JournalPath, MaxComponentJournalBytes)
	if err != nil {
		return errXKeenJournalInvalid
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

func (s *XKeenService) acquireMutation(ctx context.Context) (func(), error) {
	if s == nil || s.mutationGate == nil {
		return nil, ErrXKeenTransactionUnavailable
	}
	release, err := s.mutationGate.Acquire(ctx)
	if err != nil {
		return nil, ErrXKeenBusy
	}
	return release, nil
}

func (s *XKeenService) acquireApply(ctx context.Context) (func(), func(), error) {
	admission, cancel := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancel()
	coordinator, err := s.beginCoordinator(admission, false)
	if err != nil {
		return nil, nil, ErrXKeenAuthorityBusy
	}
	authorityRelease, err := s.acquireAuthority(admission, false)
	if err != nil {
		coordinator()
		return nil, nil, ErrXKeenAuthorityBusy
	}
	return coordinator, authorityRelease, nil
}

func (s *XKeenService) acquireRecovery(ctx context.Context) (func(), func(), error) {
	admission, cancel := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancel()
	coordinator, err := s.beginCoordinator(admission, true)
	if err != nil {
		return nil, nil, ErrXKeenRecoveryFailed
	}
	authorityRelease, err := s.acquireAuthority(admission, true)
	if err != nil {
		coordinator()
		return nil, nil, ErrXKeenRecoveryFailed
	}
	return coordinator, authorityRelease, nil
}

func (s *XKeenService) beginCoordinator(ctx context.Context, recovery bool) (func(), error) {
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

func (s *XKeenService) acquireAuthority(ctx context.Context, recovery bool) (func(), error) {
	if s.config.AuthorityLease == nil {
		return func() {}, nil
	}
	var release func()
	var err error
	if recovery {
		release, err = s.config.AuthorityLease.AcquireForRecovery(ctx, s.config.AuthorityWaitTimeout)
	} else {
		release, err = s.config.AuthorityLease.Acquire(ctx, s.config.AuthorityWaitTimeout)
	}
	if err != nil {
		return nil, ErrXKeenAuthorityBusy
	}
	if release == nil {
		return func() {}, nil
	}
	return release, nil
}

func (s *XKeenService) inject(stage XKeenStage) error {
	if s.config.InjectFailure == nil {
		return nil
	}
	return s.config.InjectFailure(stage)
}

func (s *XKeenService) enterMaintenance() {
	s.mu.Lock()
	if s.maintenance {
		s.ready = false
		s.mu.Unlock()
		return
	}
	s.maintenance = true
	s.ready = false
	if s.readyErr == nil {
		s.readyErr = ErrXKeenRecoveryFailed
	}
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Enter(KindXKeen)
		return
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Block()
	}
	if gate, ok := s.config.Coordinator.(XrayMaintenanceGate); ok {
		gate.EnterMaintenance()
	}
}

func (s *XKeenService) releaseMaintenance() {
	s.mu.Lock()
	if !s.maintenance {
		s.mu.Unlock()
		return
	}
	s.maintenance = false
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Exit(KindXKeen)
		return
	}
	if gate, ok := s.config.Coordinator.(XrayMaintenanceGate); ok {
		gate.ExitMaintenance()
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Unblock()
	}
}

func (s *XKeenService) failClosed() { s.enterMaintenance() }

func (s *XKeenService) failClosedResult() error {
	s.failClosed()
	s.markNotReady(ErrXKeenRecoveryFailed)
	return ErrXKeenRecoveryFailed
}
func (s *XKeenService) recoveryFailure() error {
	s.failClosed()
	s.markNotReady(ErrXKeenRecoveryFailed)
	return ErrXKeenRecoveryFailed
}

func (s *XKeenService) isMaintenance() bool {
	s.mu.Lock()
	value := s.maintenance
	s.mu.Unlock()
	return value
}
func (s *XKeenService) markReady() { s.mu.Lock(); s.ready = true; s.readyErr = nil; s.mu.Unlock() }
func (s *XKeenService) markNotReady(err error) {
	s.mu.Lock()
	s.ready = false
	s.readyErr = err
	s.mu.Unlock()
}

// RecoverStartup is local-only. It never resolves metadata or downloads an
// asset; it either proves a crash window is settled or restores the exact
// journaled generation under the recovery admission path.
func (s *XKeenService) RecoverStartup(ctx context.Context) error {
	if s == nil {
		return ErrXKeenTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.startupMu.Lock()
	defer s.startupMu.Unlock()
	release, err := s.acquireMutation(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := s.validateXKeenLifecycle(); err != nil {
		return s.recoveryFailure()
	}
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return s.recoveryFailure()
	}
	if present && kind != KindXKeen && s.stagingPresent() {
		s.markNotReady(ErrXKeenRecoveryConflict)
		s.failClosed()
		return ErrXKeenRecoveryConflict
	}
	journal, exists, err := s.readJournal()
	if err != nil {
		return s.recoveryFailure()
	}
	if !exists {
		if s.stagingPresent() {
			ownerOnly, ownerErr := s.ownerOnlyStagingRoot()
			if ownerErr != nil {
				return s.recoveryFailure()
			}
			activationResidue := s.pathExists(s.config.ActivationPath) || s.pathExists(s.markerStagingPath())
			if ownerOnly && !s.previousStagingPresent() && !activationResidue {
				if err := s.removeEmptyStagingRoot(); err != nil {
					return s.recoveryFailure()
				}
				s.releaseMaintenance()
			} else {
				if !s.previousStagingPresent() || s.componentStagingRootPresent() || activationResidue {
					return s.recoveryFailure()
				}
				transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
				defer cancel()
				coordinator, authorityRelease, err := s.acquireRecovery(transactionContext)
				if err != nil {
					return s.recoveryFailure()
				}
				defer coordinator()
				defer authorityRelease()
				staged, err := s.loadGeneration(s.stagingPath())
				if err != nil {
					return s.recoveryFailure()
				}
				active, marker, _, err := s.readActiveGeneration()
				if err != nil || !sameXKeenGeneration(active, staged.meta) || !bytes.Equal(marker, staged.marker) {
					return s.recoveryFailure()
				}
				if err := s.removeOwnedAndSync(s.stagingPath()); err != nil {
					return s.recoveryFailure()
				}
				s.releaseMaintenance()
			}
		}
		if s.isMaintenance() {
			return s.recoveryFailure()
		}
		s.markReady()
		return nil
	}

	transactionContext, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	coordinator, authorityRelease, err := s.acquireRecovery(transactionContext)
	if err != nil {
		return s.recoveryFailure()
	}
	defer coordinator()
	defer authorityRelease()
	authoritySnapshot, err := s.captureAuthorityHeld(transactionContext)
	if err != nil {
		return s.recoveryFailure()
	}
	if hex.EncodeToString(authoritySnapshot.Generation[:]) != journal.AuthorityGeneration {
		return s.recoveryFailure()
	}
	current, marker, _, currentErr := s.readActiveGeneration()
	currentXray, err := binaryMetadata(s.config.XrayBinaryPath, "", s.config.CandidateProbe, transactionContext)
	if err != nil {
		return s.recoveryFailure()
	}
	if !sameBinaryMetadata(currentXray, journal.Xray) {
		return s.recoveryFailure()
	}
	oldPresent := s.pathExists(s.oldPreviousPath())
	currentLoaded := loadedXKeenGeneration{meta: current, marker: marker}
	if currentErr == nil && journal.Phase == xkeenPhaseRuntimeVerified && !oldPresent && sameXKeenLoadedSummary(currentLoaded, journal.Candidate) {
		previous, previousErr := s.loadGeneration(s.config.PreviousDir)
		candidateMarker, markerErr := markerForJournalCandidate(journal, marker)
		if previousErr == nil && sameXKeenLoadedSummary(previous, journal.Previous) && markerErr == nil && s.verifyRuntime(transactionContext, current, candidateMarker, currentXray, authoritySnapshot, journal.PreservedFingerprint) == nil {
			if err := s.cleanupResidue(); err != nil {
				return s.recoveryFailure()
			}
			if err := s.clearJournal(); err != nil {
				return s.recoveryFailure()
			}
			s.releaseMaintenance()
			s.markReady()
			return nil
		}
	}
	previous, err := s.loadJournalPrevious(journal.Previous)
	if err != nil {
		return s.recoveryFailure()
	}
	if err := s.restoreRuntime(transactionContext, previous.path, previous.meta, previous.marker, currentXray, authoritySnapshot, journal.PreservedFingerprint); err != nil {
		return s.recoveryFailure()
	}
	if err := s.restorePreviousAfterFailure(journal); err != nil {
		return s.recoveryFailure()
	}
	if err := s.cleanupResidue(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.clearJournal(); err != nil {
		return s.recoveryFailure()
	}
	s.releaseMaintenance()
	s.markReady()
	return nil
}

func markerForJournalCandidate(journal xkeenTransactionJournal, current []byte) ([]byte, error) {
	if journal.Candidate.Generation == "" {
		return nil, errXKeenMarkerInvalid
	}
	if !journal.Candidate.MarkerPresent {
		if journal.Candidate.MarkerSHA256 != "" || len(current) != 0 {
			return nil, errXKeenMarkerInvalid
		}
		return nil, nil
	}
	if len(current) > 0 {
		if record, err := parseXKeenMarker(current); err == nil && strings.EqualFold(record.GenerationSHA256, journal.Candidate.Generation) {
			digest := sha256.Sum256(current)
			if strings.EqualFold(hex.EncodeToString(digest[:]), journal.Candidate.MarkerSHA256) {
				return current, nil
			}
		}
	}
	_, marker, err := markerForGeneration(journal.Candidate.Generation)
	if err == nil {
		digest := sha256.Sum256(marker)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), journal.Candidate.MarkerSHA256) {
			return nil, errXKeenMarkerInvalid
		}
	}
	return marker, err
}
