package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	GeodataTransactionSchemaVersion = XrayTransactionSchemaVersion

	DefaultGeodataPreviousDir         = "/opt/etc/xkeen-control/previous/components/geodata"
	DefaultGeodataComponentStagingDir = "/tmp/xkeen-control/components/geodata"

	DefaultGeodataAuthorityWaitTimeout = 15 * time.Second
	DefaultGeodataPrepareTimeout       = 3 * time.Minute
	DefaultGeodataActivationTimeout    = 2 * time.Minute
	DefaultGeodataRollbackTimeout      = 2 * time.Minute
	DefaultGeodataTransactionTimeout   = 5 * time.Minute

	MaxGeodataCandidateBytes = 128 << 20
	GeodataFreeSpaceReserve  = 8 << 20

	geodataMetadataName            = "metadata.json"
	geodataActivationTempDirName   = ".xkeen-geodata-transaction"
	geodataActivationOwnerName     = ".owner"
	geodataActivationOwnerContents = "xkeen-control geodata activation v1\n"
	geodataPreviousStaging         = ".staging"
	geodataPreviousOld             = ".old"
	geodataOperationUpdate         = "update"
	geodataOperationRollback       = "rollback"
	geodataPhasePrepared           = "prepared"
	geodataPhaseFilesCommitted     = "files-committed"
	geodataPhaseRuntimeVerified    = "runtime-verified"
)

var (
	ErrGeodataResolutionUnavailable  = errors.New("geodata release resolution unavailable")
	ErrGeodataCandidateRejected      = errors.New("geodata candidate was rejected")
	ErrGeodataCandidateStale         = errors.New("geodata candidate is stale")
	ErrGeodataArtifactRejected       = errors.New("geodata artifact was rejected")
	ErrGeodataAuthorityUnavailable   = errors.New("geodata authority is unavailable")
	ErrGeodataAuthorityBusy          = errors.New("geodata authority is busy")
	ErrGeodataTransactionUnavailable = errors.New("geodata transaction is unavailable")
	ErrGeodataApplyFailed            = errors.New("geodata activation failed")
	ErrGeodataRollbackFailed         = errors.New("geodata rollback failed")
	ErrGeodataPreviousUnavailable    = errors.New("previous geodata generation is unavailable")
	ErrGeodataRecoveryRequired       = errors.New("geodata component recovery is required")
	ErrGeodataRecoveryConflict       = errors.New("geodata component recovery conflicts with restore")
	ErrGeodataRecoveryFailed         = errors.New("geodata component recovery failed")
	ErrGeodataBusy                   = errors.New("geodata component transaction is busy")
	ErrGeodataPolicyUnsupported      = errors.New("geodata policy is outside the product catalog")

	errGeodataPolicyUnsupported     = ErrGeodataPolicyUnsupported
	errGeodataCatalogInvalid        = errors.New("product geodata catalog is invalid")
	errGeodataJournalInvalid        = errors.New("geodata transaction journal is invalid")
	errGeodataSetInvalid            = errors.New("geodata set is invalid")
	errGeodataGenerationChanged     = errors.New("geodata generation changed")
	errGeodataFreeSpaceUnavailable  = errors.New("geodata component free space is unavailable")
	errGeodataFreeSpaceInsufficient = errors.New("geodata component free space is insufficient")
)

// GeodataReleaseIdentity is the complete server-owned identity of one fixed
// catalog asset. It contains no URL or caller-supplied path.
type GeodataReleaseIdentity struct {
	ID         string
	Repository string
	Tag        string
	AssetName  string
	ActiveName string
	SizeBytes  int64
	SHA256     string
}

type GeodataCandidateSet struct {
	Items      []GeodataReleaseIdentity
	Generation string
}

// GeodataCandidateResolver has one purpose: resolve the six server-owned
// product sources into an ordered candidate set.
type GeodataCandidateResolver interface {
	ResolveGeodata(context.Context) (GeodataCandidateSet, error)
}

// GeodataArtifactDownloader receives only a server-owned identity and a
// bounded destination writer. It is not a generic URL or file downloader.
type GeodataArtifactDownloader interface {
	DownloadGeodata(context.Context, GeodataReleaseIdentity, io.Writer) error
}

type GeodataResolver struct {
	client *metadataClient
}

func NewGeodataResolver(resolver netguard.IPResolver, supplied *http.Client) *GeodataResolver {
	return &GeodataResolver{client: newMetadataClient(resolver, supplied)}
}

// ResolveGeodata is uncached. The five distinct fixed GitHub release sources
// are fetched at most once, including the shared Re-filter source.
func (r *GeodataResolver) ResolveGeodata(ctx context.Context) (GeodataCandidateSet, error) {
	if r == nil || r.client == nil {
		return GeodataCandidateSet{}, ErrGeodataResolutionUnavailable
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return GeodataCandidateSet{}, ErrGeodataResolutionUnavailable
	}
	return r.resolveEntries(ctx, entries)
}

func (r *GeodataResolver) resolveEntries(ctx context.Context, entries []catalogEntry) (GeodataCandidateSet, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	type result struct {
		path    string
		release githubReleaseMetadata
		failure *metadataFailure
		err     error
	}
	sources := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		source := geodataMetadataPath(entry)
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	results := make(chan result, len(sources))
	slots := make(chan struct{}, MaxConcurrentMetadata)
	budget := newNetworkBudget()
	var workers sync.WaitGroup
	for _, source := range sources {
		source := source
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results <- result{path: source, err: ctx.Err()}
				return
			}
			body, fetchErr := r.client.fetch(ctx, source, budget)
			<-slots
			if fetchErr != nil {
				results <- result{path: source, err: fetchErr}
				return
			}
			release, failure := decodeReleaseMetadata(body)
			results <- result{path: source, release: release, failure: failure}
		}()
	}
	workers.Wait()
	close(results)
	byPath := make(map[string]result, len(sources))
	for value := range results {
		byPath[value.path] = value
	}

	items := make([]GeodataReleaseIdentity, len(entries))
	var total int64
	for index, entry := range entries {
		value, ok := byPath[geodataMetadataPath(entry)]
		if !ok || value.err != nil {
			if value.err != nil && ctx.Err() != nil {
				return GeodataCandidateSet{}, ctx.Err()
			}
			return GeodataCandidateSet{}, ErrGeodataResolutionUnavailable
		}
		if value.failure != nil || value.release.TagName == "" {
			return GeodataCandidateSet{}, ErrGeodataCandidateRejected
		}
		if failure := validateReleaseMetadata(value.release); failure != nil || !metadataGenerationPattern.MatchString(value.release.TagName) {
			return GeodataCandidateSet{}, ErrGeodataCandidateRejected
		}
		asset, failure := selectMetadataAsset(value.release.Assets, entry.Asset, true)
		if failure != nil || asset.Size > MaxGeodataFileBytes {
			return GeodataCandidateSet{}, ErrGeodataCandidateRejected
		}
		if total > MaxGeodataCandidateBytes-asset.Size {
			return GeodataCandidateSet{}, ErrGeodataCandidateRejected
		}
		total += asset.Size
		items[index] = GeodataReleaseIdentity{
			ID: entry.ID, Repository: entry.Repository, Tag: value.release.TagName,
			AssetName: entry.Asset, ActiveName: entry.Name, SizeBytes: asset.Size, SHA256: asset.SHA256,
		}
	}
	set := GeodataCandidateSet{Items: items, Generation: geodataIdentityGeneration(items)}
	if err := validateGeodataCandidateSet(set); err != nil {
		return GeodataCandidateSet{}, ErrGeodataCandidateRejected
	}
	return set, nil
}

func CandidateSetGeneration(items []GeodataReleaseIdentity) string {
	return geodataIdentityGeneration(items)
}

func requiredProductGeodata(value appliance.Appliance) ([]catalogEntry, error) {
	if err := value.Validate(); err != nil {
		return nil, errGeodataPolicyUnsupported
	}
	if err := validateProductGeodataCatalog(); err != nil {
		return nil, err
	}
	byName := make(map[string]catalogEntry, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		byName[entry.Kind+":"+entry.Name] = entry
	}
	for _, expression := range applianceExpressions(value) {
		if !strings.HasPrefix(expression.value, "ext:") {
			continue
		}
		if !logicalExtExpression(expression.value) {
			return nil, errGeodataPolicyUnsupported
		}
		name := logicalFilename(expression.value)
		if _, ok := byName[expression.kind+":"+name]; !ok {
			return nil, errGeodataPolicyUnsupported
		}
	}
	result := append([]catalogEntry(nil), productGeodataCatalog...)
	return result, nil
}

func validateProductGeodataCatalog() error {
	if len(productGeodataCatalog) != 6 {
		return errGeodataCatalogInvalid
	}
	ids := make(map[string]struct{}, len(productGeodataCatalog))
	names := make(map[string]struct{}, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		if entry.ID == "" || entry.Kind != "geosite" && entry.Kind != "geoip" || !logicalFilenamePattern.MatchString(entry.Name) || !logicalFilenamePattern.MatchString(entry.Asset) || !strings.Contains(entry.Repository, "/") || strings.ContainsAny(entry.Repository, "\\:") {
			return errGeodataCatalogInvalid
		}
		if _, ok := ids[entry.ID]; ok {
			return errGeodataCatalogInvalid
		}
		if _, ok := names[entry.Kind+":"+entry.Name]; ok {
			return errGeodataCatalogInvalid
		}
		ids[entry.ID] = struct{}{}
		names[entry.Kind+":"+entry.Name] = struct{}{}
	}
	return nil
}

func validateGeodataCandidateSet(value GeodataCandidateSet) error {
	if len(value.Items) != len(productGeodataCatalog) || value.Generation == "" {
		return errGeodataSetInvalid
	}
	if value.Generation != geodataIdentityGeneration(value.Items) {
		return errGeodataSetInvalid
	}
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return err
	}
	var total int64
	for index, item := range value.Items {
		if !validGeodataIdentity(item, entries[index]) {
			return errGeodataSetInvalid
		}
		if total > MaxGeodataCandidateBytes-item.SizeBytes {
			return errGeodataSetInvalid
		}
		total += item.SizeBytes
	}
	return nil
}

func requiredProductGeodataForCatalog() ([]catalogEntry, error) {
	if err := validateProductGeodataCatalog(); err != nil {
		return nil, err
	}
	return append([]catalogEntry(nil), productGeodataCatalog...), nil
}

func validGeodataIdentity(value GeodataReleaseIdentity, entry catalogEntry) bool {
	return value.ID == entry.ID && value.Repository == entry.Repository && value.AssetName == entry.Asset && value.ActiveName == entry.Name && metadataGenerationPattern.MatchString(value.Tag) && value.SizeBytes > 0 && value.SizeBytes <= MaxGeodataFileBytes && isHexSHA256(value.SHA256)
}

func sameGeodataIdentity(left, right GeodataReleaseIdentity) bool {
	return left.ID == right.ID && left.Repository == right.Repository && left.Tag == right.Tag && left.AssetName == right.AssetName && left.ActiveName == right.ActiveName && left.SizeBytes == right.SizeBytes && strings.EqualFold(left.SHA256, right.SHA256)
}

func sameGeodataCandidateSet(left, right GeodataCandidateSet) bool {
	if left.Generation != right.Generation || len(left.Items) != len(right.Items) {
		return false
	}
	for index := range left.Items {
		if !sameGeodataIdentity(left.Items[index], right.Items[index]) {
			return false
		}
	}
	return true
}

func geodataIdentityGeneration(items []GeodataReleaseIdentity) string {
	hash := sha256.New()
	for _, item := range items {
		_ = writeAuthorityHashPart(hash, item.ID, []byte(item.Repository))
		_ = writeAuthorityHashPart(hash, "tag", []byte(item.Tag))
		_ = writeAuthorityHashPart(hash, "asset", []byte(item.AssetName))
		_ = writeAuthorityHashPart(hash, "active", []byte(item.ActiveName))
		_ = writeAuthorityHashPart(hash, "size", []byte(strconv.FormatInt(item.SizeBytes, 10)))
		_ = writeAuthorityHashPart(hash, "sha256", []byte(strings.ToLower(item.SHA256)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type GeodataStage string

const (
	GeodataStagePreviousStaging GeodataStage = "previous-staging"
	GeodataStagePreviousSaved   GeodataStage = "previous-saved"
	GeodataStageJournalPrepared GeodataStage = "journal-prepared"
	GeodataStageFileCommitted   GeodataStage = "file-committed"
	GeodataStageFilesCommitted  GeodataStage = "files-committed"
	GeodataStagePreviousSettled GeodataStage = "previous-settled"
	GeodataStageRuntimeVerified GeodataStage = "runtime-verified"
	GeodataStageJournalCleared  GeodataStage = "journal-cleared"
)

type GeodataConfig struct {
	Resolver   GeodataCandidateResolver
	Downloader GeodataArtifactDownloader
	Authority  XrayAuthorityProvider
	Runtime    XrayRuntime

	CandidateValidator XrayCandidateValidator
	CandidateProbe     XrayCandidateProbe

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
	InjectFailure  func(GeodataStage) error
}

type GeodataService struct {
	config       GeodataConfig
	mutationGate *ComponentMutationGate
	startupMu    sync.Mutex
	mu           sync.Mutex
	ready        bool
	readyErr     error
	maintenance  bool
}

type preparedGeodata struct {
	set           GeodataCandidateSet
	base          geodataBaseSnapshot
	stageDir      string
	candidatePath string
	candidate     geodataSetMetadata
}

type geodataBaseSnapshot struct {
	authority XrayAuthoritySnapshot
	xray      xrayBinaryMetadata
	active    geodataSetMetadata
}

type geodataFileMetadata struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type geodataSetMetadata struct {
	Items      []geodataFileMetadata `json:"items"`
	Generation string                `json:"generation"`
}

type geodataTransactionJournal struct {
	SchemaVersion int                `json:"schemaVersion"`
	Component     string             `json:"component"`
	Operation     string             `json:"operation"`
	Phase         string             `json:"phase"`
	Previous      geodataSetMetadata `json:"previous"`
	Candidate     geodataSetMetadata `json:"candidate"`
}

type geodataPreviousRecord struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Items         []geodataFileMetadata `json:"items"`
	Generation    string                `json:"generation"`
}

type loadedGeodataGeneration struct {
	path string
	meta geodataSetMetadata
}

func NewGeodataService(config GeodataConfig) *GeodataService {
	if config.Resolver == nil {
		config.Resolver = NewGeodataResolver(nil, nil)
	}
	if config.Downloader == nil {
		config.Downloader = NewGeodataArtifactDownloader(nil, nil)
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
		config.AssetDir = DefaultGeodataDir
	}
	if config.PreviousDir == "" {
		config.PreviousDir = DefaultGeodataPreviousDir
	}
	if config.JournalPath == "" {
		config.JournalPath = DefaultComponentTransactionJournal
	}
	if config.StagingDir == "" {
		config.StagingDir = DefaultGeodataComponentStagingDir
	}
	if config.RestoreJournalPath == "" {
		config.RestoreJournalPath = filepath.Join(filepath.Dir(config.JournalPath), "appliance-import-transaction.json")
	}
	if config.AuthorityWaitTimeout <= 0 {
		config.AuthorityWaitTimeout = DefaultGeodataAuthorityWaitTimeout
	}
	if config.PrepareTimeout <= 0 {
		config.PrepareTimeout = DefaultGeodataPrepareTimeout
	}
	if config.ActivationTimeout <= 0 {
		config.ActivationTimeout = DefaultGeodataActivationTimeout
	}
	if config.RollbackTimeout <= 0 {
		config.RollbackTimeout = DefaultGeodataRollbackTimeout
	}
	if config.TransactionTimeout <= 0 {
		config.TransactionTimeout = DefaultGeodataTransactionTimeout
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
	service := &GeodataService{config: config, mutationGate: config.MutationGate, ready: true}
	kind, present, journalErr := componentJournalKind(config.JournalPath)
	if journalErr != nil {
		service.markNotReady(ErrGeodataRecoveryFailed)
		service.enterMaintenance()
		return service
	}
	if (present && kind == KindGeodata) || service.stagingPresent() {
		service.ready = false
		if present && kind == KindGeodata {
			service.readyErr = ErrGeodataRecoveryRequired
		} else {
			service.readyErr = ErrGeodataRecoveryFailed
		}
		if componentTransactionPresentUnchecked(config.RestoreJournalPath) {
			service.readyErr = ErrGeodataRecoveryConflict
		}
		service.enterMaintenance()
	}
	return service
}

func componentTransactionPresentUnchecked(path string) bool {
	present, _ := componentTransactionPresent(path)
	return present
}

func NewGeodataTransaction(config GeodataConfig) *GeodataService { return NewGeodataService(config) }

func (s *GeodataService) Ready() error {
	if s == nil {
		return ErrGeodataTransactionUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		if s.readyErr != nil {
			return s.readyErr
		}
		return ErrGeodataRecoveryRequired
	}
	return nil
}

func (s *GeodataService) HasPendingRecovery() (bool, error) {
	if s == nil {
		return false, ErrGeodataTransactionUnavailable
	}
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return false, err
	}
	if present && kind == KindGeodata {
		return true, nil
	}
	return s.stagingPresent(), nil
}

// Apply accepts a typed intended set from an internal caller. It fresh
// re-resolves all fixed sources immediately before any download and requires
// an exact identity match.
func (s *GeodataService) Apply(ctx context.Context, intended GeodataCandidateSet) error {
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

func (s *GeodataService) Update(ctx context.Context, intended GeodataCandidateSet) error {
	return s.Apply(ctx, intended)
}

func (s *GeodataService) Rollback(ctx context.Context) error {
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
		return ErrGeodataPreviousUnavailable
	}
	// Rollback's candidate is copied into the private component staging root so
	// a crash after .old is settled still retains the exact rollback target.
	candidateDir, err := s.copySetToCandidate(previous.path, previous.meta)
	if err != nil {
		return ErrGeodataCandidateRejected
	}
	defer s.removeOwned(candidateDir)
	if err := s.validateLocalCandidate(transactionContext, candidateDir, base.authority); err != nil {
		return ErrGeodataCandidateRejected
	}
	return s.runCommitted(transactionContext, geodataOperationRollback, base, candidateDir, previous.meta, true)
}

func (s *GeodataService) RecoverStartup(ctx context.Context) error {
	if s == nil {
		return ErrGeodataTransactionUnavailable
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
	if componentPresent && componentKind != KindGeodata && s.stagingPresent() {
		s.failClosed()
		s.markNotReady(ErrGeodataRecoveryConflict)
		return ErrGeodataRecoveryConflict
	}
	journal, exists, err := s.readJournal()
	if err != nil {
		return s.recoveryFailure()
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
			staged, loadErr := s.loadGeneration(s.stagingPath())
			if loadErr != nil {
				return s.recoveryFailure()
			}
			active, activeErr := s.readActiveSet()
			if activeErr != nil || !sameGeodataSetMetadata(active, staged.meta) {
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
	authoritySnapshot, err := s.captureAuthorityHeld(transactionContext)
	if err != nil {
		return s.recoveryFailure()
	}
	current, currentErr := s.readActiveSet()
	if currentErr != nil {
		return s.recoveryFailure()
	}
	currentXray, xrayErr := binaryMetadata(s.config.ActiveBinaryPath, "", s.config.CandidateProbe, transactionContext)
	if xrayErr != nil {
		return s.recoveryFailure()
	}
	oldPresent, err := s.pathPresent(s.oldPreviousPath())
	if err != nil {
		return s.recoveryFailure()
	}
	if journal.Phase == geodataPhaseRuntimeVerified && !oldPresent && sameGeodataSetMetadata(current, journal.Candidate) {
		previous, previousErr := s.loadGeneration(s.config.PreviousDir)
		if previousErr == nil && sameGeodataSetMetadata(previous.meta, journal.Previous) && s.verifyRuntime(transactionContext, journal.Candidate, currentXray, authoritySnapshot) == nil {
			if err := s.cleanupNonPreviousResidue(); err != nil {
				return s.recoveryFailure()
			}
			if err := s.clearJournal(); err != nil {
				return s.recoveryFailure()
			}
			if err := s.removeOwnedAndSync(s.stagingPath()); err != nil {
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
	var displacedCandidatePath string
	if journal.Operation == geodataOperationRollback && !oldPresent && sameGeodataSetMetadata(current, journal.Candidate) {
		currentPrevious, previousErr := s.loadGeneration(s.config.PreviousDir)
		if previousErr != nil || !sameGeodataSetMetadata(currentPrevious.meta, journal.Candidate) {
			displacedCandidatePath, err = s.copySetToCandidate(s.config.AssetDir, current)
			if err != nil {
				return s.recoveryFailure()
			}
		}
	}
	if err := s.restoreRuntime(transactionContext, previous.path, previous.meta, currentXray, authoritySnapshot); err != nil {
		return s.recoveryFailure()
	}
	if oldPresent {
		old, oldErr := s.loadGeneration(s.oldPreviousPath())
		if oldErr != nil || journal.Operation == geodataOperationRollback && !sameGeodataSetMetadata(old.meta, journal.Candidate) {
			return s.recoveryFailure()
		}
		if err := s.restorePreviousAfterPromotionFailure(journal); err != nil {
			return s.recoveryFailure()
		}
	} else if journal.Operation == geodataOperationRollback {
		currentPrevious, previousErr := s.loadGeneration(s.config.PreviousDir)
		if previousErr == nil && sameGeodataSetMetadata(currentPrevious.meta, journal.Candidate) {
			// The original rollback target was never displaced, so it remains
			// the one-step previous generation.
		} else if displacedCandidatePath != "" {
			if err := s.promoteCandidateAsPrevious(displacedCandidatePath, journal.Candidate); err != nil {
				return s.recoveryFailure()
			}
		} else {
			return s.recoveryFailure()
		}
	}
	if err := s.cleanupNonPreviousResidue(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.clearJournal(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.removeOwnedAndSync(s.stagingPath()); err != nil {
		return s.recoveryFailure()
	}
	s.releaseMaintenance()
	s.markReady()
	return nil
}

func (s *GeodataService) prepare(ctx context.Context, intended GeodataCandidateSet) (preparedGeodata, error) {
	if err := validateGeodataCandidateSet(intended); err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	base, err := s.captureBase(ctx)
	if err != nil {
		return preparedGeodata{}, err
	}
	fresh, err := s.config.Resolver.ResolveGeodata(ctx)
	if err != nil {
		return preparedGeodata{}, err
	}
	if !sameGeodataCandidateSet(fresh, intended) {
		return preparedGeodata{}, ErrGeodataCandidateStale
	}
	if err := s.checkFreeSpace(candidateSetSize(intended), geodataSetSize(base.active)); err != nil {
		return preparedGeodata{}, err
	}
	stageDir, err := s.newStagingDir()
	if err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	cleanup := true
	defer func() {
		if cleanup {
			s.removeOwned(stageDir)
		}
	}()
	candidatePath := filepath.Join(stageDir, "candidate")
	if err := s.downloadCandidate(ctx, candidatePath, intended); err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	candidate, err := s.readCandidateSet(candidatePath, intended)
	if err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	activeXray, err := binaryMetadata(s.config.ActiveBinaryPath, base.xray.Version, s.config.CandidateProbe, ctx)
	if err != nil || !sameBinaryMetadata(activeXray, base.xray) {
		return preparedGeodata{}, ErrGeodataCandidateStale
	}
	files, err := appliance.RenderCandidateFiles(base.authority.Appliance, base.authority.Registry)
	if err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	configDir := filepath.Join(stageDir, "config")
	if err := writeXrayCandidateTree(configDir, files, s.config.SyncDirectory); err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	if err := s.config.CandidateValidator.ValidateXrayCandidate(ctx, s.config.ActiveBinaryPath, filepath.Join(configDir, "xray"), candidatePath); err != nil {
		return preparedGeodata{}, ErrGeodataCandidateRejected
	}
	cleanup = false
	return preparedGeodata{set: intended, base: base, stageDir: stageDir, candidatePath: candidatePath, candidate: candidate}, nil
}

func (s *GeodataService) applyPrepared(ctx context.Context, prepared preparedGeodata) error {
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
	if !sameGeodataBase(current, prepared.base) {
		return ErrGeodataCandidateStale
	}
	return s.runCommitted(ctx, geodataOperationUpdate, current, prepared.candidatePath, prepared.candidate, true)
}

func (s *GeodataService) captureBase(ctx context.Context) (geodataBaseSnapshot, error) {
	release, err := s.acquireAuthority(ctx, false)
	if err != nil {
		return geodataBaseSnapshot{}, err
	}
	defer release()
	return s.captureHeld(ctx)
}

func (s *GeodataService) captureHeld(ctx context.Context) (geodataBaseSnapshot, error) {
	authoritySnapshot, err := s.captureAuthorityHeld(ctx)
	if err != nil {
		return geodataBaseSnapshot{}, err
	}
	if _, err := requiredProductGeodata(authoritySnapshot.Appliance); err != nil {
		return geodataBaseSnapshot{}, ErrGeodataPolicyUnsupported
	}
	xray, err := binaryMetadata(s.config.ActiveBinaryPath, "", s.config.CandidateProbe, ctx)
	if err != nil {
		return geodataBaseSnapshot{}, ErrGeodataAuthorityUnavailable
	}
	active, err := s.readActiveSet()
	if err != nil {
		return geodataBaseSnapshot{}, ErrGeodataAuthorityUnavailable
	}
	return geodataBaseSnapshot{authority: authoritySnapshot, xray: xray, active: active}, nil
}

func (s *GeodataService) captureAuthorityHeld(ctx context.Context) (XrayAuthoritySnapshot, error) {
	if s.config.Authority == nil {
		return XrayAuthoritySnapshot{}, ErrGeodataAuthorityUnavailable
	}
	snapshot, err := s.config.Authority.SnapshotUnderLease(ctx)
	if err != nil || !validAuthoritySnapshot(snapshot) {
		return XrayAuthoritySnapshot{}, ErrGeodataAuthorityUnavailable
	}
	return snapshot, nil
}

func sameGeodataBase(left, right geodataBaseSnapshot) bool {
	return left.authority.Generation == right.authority.Generation && sameBinaryMetadata(left.xray, right.xray) && sameGeodataSetMetadata(left.active, right.active)
}

func (s *GeodataService) validateLocalCandidate(ctx context.Context, candidateDir string, authoritySnapshot XrayAuthoritySnapshot) error {
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
	return s.config.CandidateValidator.ValidateXrayCandidate(ctx, s.config.ActiveBinaryPath, filepath.Join(configDir, "xray"), candidateDir)
}

func (s *GeodataService) verifyRuntime(ctx context.Context, expected geodataSetMetadata, expectedXray xrayBinaryMetadata, authoritySnapshot XrayAuthoritySnapshot) error {
	active, err := s.readActiveSet()
	if err != nil || !sameGeodataSetMetadata(active, expected) {
		return errGeodataSetInvalid
	}
	xray, err := binaryMetadata(s.config.ActiveBinaryPath, expectedXray.Version, s.config.CandidateProbe, ctx)
	if err != nil || !sameBinaryMetadata(xray, expectedXray) {
		return errXrayBinaryInvalid
	}
	if s.config.Runtime == nil {
		return ErrGeodataTransactionUnavailable
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
		return errGeodataGenerationChanged
	}
	finalActive, err := s.readActiveSet()
	if err != nil || !sameGeodataSetMetadata(finalActive, expected) {
		return errGeodataSetInvalid
	}
	finalXray, err := binaryMetadata(s.config.ActiveBinaryPath, expectedXray.Version, s.config.CandidateProbe, ctx)
	if err != nil || !sameBinaryMetadata(finalXray, expectedXray) {
		return errXrayBinaryInvalid
	}
	return nil
}

func (s *GeodataService) restoreRuntime(ctx context.Context, source string, expected geodataSetMetadata, expectedXray xrayBinaryMetadata, authoritySnapshot XrayAuthoritySnapshot) error {
	if err := s.activateGeodataSetInternal(ctx, source, expected, false); err != nil {
		return err
	}
	return s.verifyRuntime(ctx, expected, expectedXray, authoritySnapshot)
}

func (s *GeodataService) runCommitted(ctx context.Context, operation string, base geodataBaseSnapshot, candidatePath string, candidate geodataSetMetadata, candidateValidated bool) error {
	if s.config.Runtime == nil || s.config.CandidateValidator == nil || s.config.CandidateProbe == nil {
		return ErrGeodataTransactionUnavailable
	}
	if !validGeodataSetMetadata(candidate) {
		return ErrGeodataCandidateRejected
	}
	if !candidateValidated {
		if err := s.validateLocalCandidate(ctx, candidatePath, base.authority); err != nil {
			return ErrGeodataCandidateRejected
		}
	}
	stagePath, err := s.savePreviousGeneration(base.active)
	if err != nil {
		if s.previousStagingPresent() {
			return s.recoveryFailure()
		}
		return ErrGeodataApplyFailed
	}
	if err := s.inject(GeodataStagePreviousStaging); err != nil {
		s.failClosed()
		s.markNotReady(ErrGeodataRecoveryRequired)
		return ErrGeodataApplyFailed
	}
	journal := geodataTransactionJournal{
		SchemaVersion: GeodataTransactionSchemaVersion,
		Component:     string(KindGeodata),
		Operation:     operation,
		Phase:         geodataPhasePrepared,
		Previous:      base.active,
		Candidate:     candidate,
	}
	if err := s.writeJournal(journal); err != nil {
		present, presentErr := componentTransactionPresent(s.config.JournalPath)
		if presentErr != nil {
			return s.recoveryFailure()
		}
		if present {
			// An atomic journal write can leave a complete journal on disk when
			// its directory sync reports an error. Clear it only after the
			// bounded previous staging is still available; if the clear is not
			// durable, retain both journal and staging for startup recovery.
			if clearErr := s.clearJournal(); clearErr != nil {
				return s.recoveryFailure()
			}
		}
		if cleanupErr := s.removeOwned(s.stagingPath()); cleanupErr != nil {
			return s.recoveryFailure()
		}
		return ErrGeodataApplyFailed
	}
	if err := s.inject(GeodataStagePreviousSaved); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(GeodataStageJournalPrepared); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.activateGeodataSet(ctx, candidatePath, candidate); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	journal.Phase = geodataPhaseFilesCommitted
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(GeodataStageFilesCommitted); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	activationContext, cancel := context.WithTimeout(ctx, s.config.ActivationTimeout)
	verifyErr := s.verifyRuntime(activationContext, candidate, base.xray, base.authority)
	cancel()
	if verifyErr != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, verifyErr)
	}
	if err := s.promotePreviousGeneration(); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(GeodataStagePreviousSettled); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	journal.Phase = geodataPhaseRuntimeVerified
	if err := s.writeJournal(journal); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.settlePreviousGeneration(); err != nil {
		return s.failAndRecover(ctx, journal, base, stagePath, candidatePath, err)
	}
	if err := s.inject(GeodataStageRuntimeVerified); err != nil {
		return s.failClosedResult()
	}
	if err := s.inject(GeodataStageJournalCleared); err != nil {
		return s.failClosedResult()
	}
	if err := s.cleanupTransactionResidue(); err != nil {
		return s.failClosedResult()
	}
	if err := s.clearJournal(); err != nil {
		return s.failClosedResult()
	}
	return nil
}

func (s *GeodataService) failAndRecover(ctx context.Context, journal geodataTransactionJournal, base geodataBaseSnapshot, stagePath, candidatePath string, _ error) error {
	rollbackContext, cancel := context.WithTimeout(context.Background(), s.config.RollbackTimeout)
	defer cancel()
	previous, err := s.loadJournalPrevious(journal.Previous)
	if err != nil {
		return s.recoveryFailure()
	}
	if err := s.restoreRuntime(rollbackContext, previous.path, journal.Previous, base.xray, base.authority); err != nil {
		return s.recoveryFailure()
	}
	oldPresent, err := s.pathPresent(s.oldPreviousPath())
	if err != nil {
		return s.recoveryFailure()
	}
	if oldPresent {
		old, oldErr := s.loadGeneration(s.oldPreviousPath())
		if oldErr != nil || journal.Operation == geodataOperationRollback && !sameGeodataSetMetadata(old.meta, journal.Candidate) {
			return s.recoveryFailure()
		}
		if err := s.restorePreviousAfterPromotionFailure(journal); err != nil {
			return s.recoveryFailure()
		}
	} else if journal.Operation == geodataOperationRollback {
		currentPrevious, previousErr := s.loadGeneration(s.config.PreviousDir)
		if previousErr == nil && sameGeodataSetMetadata(currentPrevious.meta, journal.Candidate) {
			// Activation never promoted the saved previous generation; the
			// original rollback target is still in place.
		} else if candidatePath != "" {
			candidate, candidateErr := s.readCandidateSet(candidatePath, candidateSetFromMetadata(journal.Candidate))
			if candidateErr != nil || !sameGeodataSetMetadata(candidate, journal.Candidate) || s.promoteCandidateAsPrevious(candidatePath, journal.Candidate) != nil {
				return s.recoveryFailure()
			}
		}
	} else {
		// An update that started without a previous generation must not turn
		// the recovered active bytes into a new rollback target.
		if currentPrevious, previousErr := s.loadGeneration(s.config.PreviousDir); previousErr == nil && sameGeodataSetMetadata(currentPrevious.meta, journal.Previous) {
			if removeErr := s.removeOwned(s.config.PreviousDir); removeErr != nil {
				return s.recoveryFailure()
			}
		}
	}
	if err := s.cleanupNonPreviousResidue(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.clearJournal(); err != nil {
		return s.recoveryFailure()
	}
	if err := s.removeOwnedAndSync(s.stagingPath()); err != nil {
		return s.recoveryFailure()
	}
	if journal.Operation == geodataOperationRollback {
		return ErrGeodataRollbackFailed
	}
	return ErrGeodataApplyFailed
}

func (s *GeodataService) failClosedResult() error {
	s.failClosed()
	return ErrGeodataRecoveryFailed
}

func (s *GeodataService) recoveryFailure() error {
	s.failClosed()
	s.markNotReady(ErrGeodataRecoveryFailed)
	return ErrGeodataRecoveryFailed
}

func (s *GeodataService) acquireMutation(ctx context.Context) (func(), error) {
	if s == nil {
		return nil, ErrGeodataTransactionUnavailable
	}
	release, err := s.mutationGate.Acquire(ctx)
	if err != nil {
		return nil, ErrGeodataBusy
	}
	return release, nil
}

func (s *GeodataService) acquireApply(ctx context.Context) (func(), func(), error) {
	admission, cancel := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancel()
	releaseCoordinator, err := s.beginCoordinator(admission, false)
	if err != nil {
		return nil, nil, ErrGeodataAuthorityBusy
	}
	releaseAuthority, err := s.acquireAuthority(admission, false)
	if err != nil {
		releaseCoordinator()
		return nil, nil, ErrGeodataAuthorityBusy
	}
	return releaseCoordinator, releaseAuthority, nil
}

func (s *GeodataService) acquireRecovery(ctx context.Context) (func(), func(), error) {
	admission, cancel := context.WithTimeout(ctx, s.config.AuthorityWaitTimeout)
	defer cancel()
	releaseCoordinator, err := s.beginCoordinator(admission, true)
	if err != nil {
		return nil, nil, ErrGeodataRecoveryFailed
	}
	releaseAuthority, err := s.acquireAuthority(admission, true)
	if err != nil {
		releaseCoordinator()
		return nil, nil, ErrGeodataRecoveryFailed
	}
	return releaseCoordinator, releaseAuthority, nil
}

func (s *GeodataService) beginCoordinator(ctx context.Context, recovery bool) (func(), error) {
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

func (s *GeodataService) acquireAuthority(ctx context.Context, recovery bool) (func(), error) {
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
		return nil, ErrGeodataAuthorityBusy
	}
	if release == nil {
		return func() {}, nil
	}
	return release, nil
}

func (s *GeodataService) inject(stage GeodataStage) error {
	if s.config.InjectFailure == nil {
		return nil
	}
	return s.config.InjectFailure(stage)
}

func (s *GeodataService) enterMaintenance() {
	s.mu.Lock()
	if s.maintenance {
		s.ready = false
		s.mu.Unlock()
		return
	}
	s.maintenance = true
	s.ready = false
	if s.readyErr == nil {
		s.readyErr = ErrGeodataRecoveryFailed
	}
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Enter(KindGeodata)
		return
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Block()
	}
	if gate, ok := s.config.Coordinator.(XrayMaintenanceGate); ok {
		gate.EnterMaintenance()
	}
}

func (s *GeodataService) releaseMaintenance() {
	s.mu.Lock()
	if !s.maintenance {
		s.mu.Unlock()
		return
	}
	s.maintenance = false
	s.mu.Unlock()
	if s.config.Maintenance != nil {
		s.config.Maintenance.Exit(KindGeodata)
		return
	}
	if gate, ok := s.config.Coordinator.(XrayMaintenanceGate); ok {
		gate.ExitMaintenance()
	}
	if s.config.AuthorityLease != nil {
		s.config.AuthorityLease.Unblock()
	}
}

func (s *GeodataService) failClosed() { s.enterMaintenance() }

func (s *GeodataService) isMaintenance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maintenance
}

func (s *GeodataService) markReady() {
	s.mu.Lock()
	s.ready = true
	s.readyErr = nil
	s.mu.Unlock()
}

func (s *GeodataService) markNotReady(err error) {
	s.mu.Lock()
	s.ready = false
	s.readyErr = err
	s.mu.Unlock()
}

func (s *GeodataService) previousStagingPresent() bool {
	info, err := os.Lstat(s.stagingPath())
	return err == nil && info.Mode()&os.ModeSymlink == 0
}

func (s *GeodataService) componentStagingRootPresent() bool {
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

func (s *GeodataService) stagingPresent() bool {
	return s.previousStagingPresent() || s.componentStagingRootPresent()
}

func (s *GeodataService) activationTempDir() string {
	return filepath.Join(filepath.Dir(filepath.Clean(s.config.AssetDir)), geodataActivationTempDirName)
}

func (s *GeodataService) activationTempOwnerPath() string {
	return filepath.Join(s.activationTempDir(), geodataActivationOwnerName)
}

func (s *GeodataService) validateActivationTempLocation() error {
	activeInfo, err := os.Lstat(s.config.AssetDir)
	if err != nil || activeInfo.Mode()&os.ModeSymlink != 0 || !activeInfo.IsDir() {
		return errGeodataSetInvalid
	}
	parent := filepath.Dir(s.activationTempDir())
	parentInfo, err := os.Lstat(parent)
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return errGeodataSetInvalid
	}
	same, err := sameFilesystem(s.config.AssetDir, parent)
	if err != nil || !same {
		return errGeodataSetInvalid
	}
	return nil
}

func (s *GeodataService) activationTempOwnerValid() error {
	rootInfo, err := os.Lstat(s.activationTempDir())
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errGeodataSetInvalid
	}
	if err := checkPrivateComponentDirectory(s.activationTempDir()); err != nil {
		return errGeodataSetInvalid
	}
	contents, err := readPrivateComponentFile(s.activationTempOwnerPath(), len(geodataActivationOwnerContents))
	if err != nil || !bytes.Equal(contents, []byte(geodataActivationOwnerContents)) {
		return errGeodataSetInvalid
	}
	return nil
}

func (s *GeodataService) prepareActivationTempDir() error {
	if err := s.validateActivationTempLocation(); err != nil {
		return err
	}
	info, err := os.Lstat(s.activationTempDir())
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(s.activationTempDir(), 0o700); err != nil {
			return errGeodataSetInvalid
		}
		if err := writeAtomicComponentFile(s.activationTempOwnerPath(), []byte(geodataActivationOwnerContents), 0o600, s.config.SyncDirectory); err != nil {
			return errGeodataSetInvalid
		}
		if err := s.config.SyncDirectory(filepath.Dir(s.activationTempDir())); err != nil {
			return errGeodataSetInvalid
		}
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errGeodataSetInvalid
	}
	return s.activationTempOwnerValid()
}

func (s *GeodataService) stagingPath() string {
	return componentRecoveryPath(s.config.PreviousDir, geodataPreviousStaging)
}

func (s *GeodataService) oldPreviousPath() string {
	return componentRecoveryPath(s.config.PreviousDir, geodataPreviousOld)
}

func (s *GeodataService) newStagingDir() (string, error) {
	if err := ensurePrivateDirectory(s.config.StagingDir); err != nil {
		return "", err
	}
	return os.MkdirTemp(s.config.StagingDir, ".geodata-transaction-")
}

func (s *GeodataService) downloadCandidate(ctx context.Context, directory string, set GeodataCandidateSet) error {
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	semaphore := make(chan struct{}, 2)
	errorsCh := make(chan error, len(set.Items))
	var workers sync.WaitGroup
	for _, identity := range set.Items {
		identity := identity
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				errorsCh <- ctx.Err()
				return
			}
			defer func() { <-semaphore }()
			destinationPath := filepath.Join(directory, identity.ActiveName)
			if filepath.Base(destinationPath) != identity.ActiveName || filepath.Dir(destinationPath) != directory {
				errorsCh <- errGeodataSetInvalid
				return
			}
			file, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				errorsCh <- err
				return
			}
			writer := &geodataArtifactWriter{destination: file, hash: sha256.New(), limit: identity.SizeBytes}
			downloadErr := s.config.Downloader.DownloadGeodata(ctx, identity, writer)
			syncErr := file.Sync()
			closeErr := file.Close()
			if downloadErr != nil || syncErr != nil || closeErr != nil || writer.count != identity.SizeBytes || !strings.EqualFold(hex.EncodeToString(writer.hash.Sum(nil)), identity.SHA256) {
				_ = os.Remove(destinationPath)
				errorsCh <- ErrGeodataArtifactRejected
				return
			}
			if err := os.Chmod(destinationPath, 0o600); err != nil {
				_ = os.Remove(destinationPath)
				errorsCh <- ErrGeodataArtifactRejected
				return
			}
		}()
	}
	workers.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			return err
		}
	}
	return s.config.SyncDirectory(directory)
}

type geodataArtifactWriter struct {
	destination io.Writer
	hash        hash.Hash
	limit       int64
	count       int64
}

func (w *geodataArtifactWriter) Write(value []byte) (int, error) {
	if w == nil || w.destination == nil || w.hash == nil || w.limit <= 0 || w.count+int64(len(value)) > w.limit {
		return 0, ErrGeodataArtifactRejected
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

func (s *GeodataService) readCandidateSet(directory string, intended GeodataCandidateSet) (geodataSetMetadata, error) {
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return geodataSetMetadata{}, err
	}
	if info, statErr := os.Lstat(directory); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return geodataSetMetadata{}, errGeodataSetInvalid
	}
	if err := checkPrivateComponentDirectory(directory); err != nil {
		return geodataSetMetadata{}, errGeodataSetInvalid
	}
	actual, err := readGeodataFiles(directory, entries, true)
	if err != nil {
		return geodataSetMetadata{}, err
	}
	for index, item := range intended.Items {
		if actual.Items[index].Name != item.ActiveName || actual.Items[index].Size != item.SizeBytes || !strings.EqualFold(actual.Items[index].SHA256, item.SHA256) {
			return geodataSetMetadata{}, errGeodataSetInvalid
		}
	}
	return actual, nil
}

func (s *GeodataService) readActiveSet() (geodataSetMetadata, error) {
	entries, err := requiredProductGeodataForCatalog()
	if err != nil {
		return geodataSetMetadata{}, err
	}
	return readGeodataFiles(s.config.AssetDir, entries, false)
}

func readGeodataFiles(directory string, entries []catalogEntry, exact bool) (geodataSetMetadata, error) {
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return geodataSetMetadata{}, errGeodataSetInvalid
	}
	if exact {
		items, readErr := os.ReadDir(directory)
		if readErr != nil || len(items) != len(entries) {
			return geodataSetMetadata{}, errGeodataSetInvalid
		}
		for _, item := range items {
			if item.IsDir() || item.Name() == geodataMetadataName {
				return geodataSetMetadata{}, errGeodataSetInvalid
			}
		}
	}
	result := geodataSetMetadata{Items: make([]geodataFileMetadata, len(entries))}
	for index, entry := range entries {
		meta, err := readGeodataFile(filepath.Join(directory, entry.Name), entry)
		if err != nil {
			return geodataSetMetadata{}, err
		}
		result.Items[index] = meta
	}
	result.Generation = geodataFileGeneration(result.Items)
	return result, nil
}

func readGeodataFile(path string, entry catalogEntry) (geodataFileMetadata, error) {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxGeodataFileBytes || before.Mode().Perm() == 0 || before.Mode().Perm()&^0o777 != 0 {
		return geodataFileMetadata{}, errGeodataSetInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return geodataFileMetadata{}, errGeodataSetInvalid
	}
	opened, err := file.Stat()
	if err != nil || opened.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || opened.Size() != before.Size() || !os.SameFile(before, opened) {
		_ = file.Close()
		return geodataFileMetadata{}, errGeodataSetInvalid
	}
	hash := sha256.New()
	count, copyErr := copyXrayBytes(context.Background(), hash, file, MaxGeodataFileBytes)
	closeErr := file.Close()
	after, statErr := os.Lstat(path)
	if copyErr != nil || closeErr != nil || statErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || after.Size() != count || count != before.Size() {
		return geodataFileMetadata{}, errGeodataSetInvalid
	}
	return geodataFileMetadata{ID: entry.ID, Name: entry.Name, Size: count, Mode: uint32(before.Mode().Perm()), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func geodataFileGeneration(items []geodataFileMetadata) string {
	hash := sha256.New()
	for _, item := range items {
		_ = writeAuthorityHashPart(hash, item.ID, []byte(item.Name))
		_ = writeAuthorityHashPart(hash, "size", []byte(strconv.FormatInt(item.Size, 10)))
		_ = writeAuthorityHashPart(hash, "mode", []byte(strconv.FormatUint(uint64(item.Mode), 10)))
		_ = writeAuthorityHashPart(hash, "sha256", []byte(strings.ToLower(item.SHA256)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func validGeodataFileMetadata(value geodataFileMetadata, entry catalogEntry) bool {
	return value.ID == entry.ID && value.Name == entry.Name && value.Size > 0 && value.Size <= MaxGeodataFileBytes && value.Mode != 0 && value.Mode&^0o777 == 0 && isHexSHA256(value.SHA256)
}

func validGeodataSetMetadata(value geodataSetMetadata) bool {
	entries, err := requiredProductGeodataForCatalog()
	if err != nil || len(value.Items) != len(entries) || value.Generation != geodataFileGeneration(value.Items) {
		return false
	}
	var total int64
	for index, item := range value.Items {
		if !validGeodataFileMetadata(item, entries[index]) || total > MaxGeodataCandidateBytes-item.Size {
			return false
		}
		total += item.Size
	}
	return true
}

func sameGeodataSetMetadata(left, right geodataSetMetadata) bool {
	if left.Generation != right.Generation || len(left.Items) != len(right.Items) {
		return false
	}
	for index := range left.Items {
		if left.Items[index].ID != right.Items[index].ID || left.Items[index].Name != right.Items[index].Name || left.Items[index].Size != right.Items[index].Size || left.Items[index].Mode != right.Items[index].Mode || !strings.EqualFold(left.Items[index].SHA256, right.Items[index].SHA256) {
			return false
		}
	}
	return true
}

func candidateSetFromMetadata(value geodataSetMetadata) GeodataCandidateSet {
	items := make([]GeodataReleaseIdentity, len(value.Items))
	for index, item := range value.Items {
		entry := productGeodataCatalog[index]
		items[index] = GeodataReleaseIdentity{ID: item.ID, Repository: entry.Repository, Tag: "recovered", AssetName: entry.Asset, ActiveName: item.Name, SizeBytes: item.Size, SHA256: item.SHA256}
	}
	return GeodataCandidateSet{Items: items, Generation: geodataIdentityGeneration(items)}
}

func candidateSetSize(value GeodataCandidateSet) int64 {
	var total int64
	for _, item := range value.Items {
		total += item.SizeBytes
	}
	return total
}

func geodataSetSize(value geodataSetMetadata) int64 {
	var total int64
	for _, item := range value.Items {
		total += item.Size
	}
	return total
}

func (s *GeodataService) checkFreeSpace(candidateSize, previousSize int64) error {
	if candidateSize <= 0 || previousSize <= 0 || candidateSize > MaxGeodataCandidateBytes || previousSize > MaxGeodataCandidateBytes {
		return errGeodataFreeSpaceInsufficient
	}
	need := uint64(candidateSize)
	if need > ^uint64(0)-uint64(previousSize) {
		return errGeodataFreeSpaceInsufficient
	}
	need += uint64(previousSize)
	if need > ^uint64(0)-uint64(candidateSize) {
		return errGeodataFreeSpaceInsufficient
	}
	need += uint64(candidateSize)
	if need > ^uint64(0)-uint64(GeodataFreeSpaceReserve) {
		return errGeodataFreeSpaceInsufficient
	}
	need += uint64(GeodataFreeSpaceReserve)
	paths := []string{
		existingDirectory(filepath.Dir(s.config.StagingDir)),
		existingDirectory(s.config.AssetDir),
		existingDirectory(filepath.Dir(s.config.PreviousDir)),
	}
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			return errGeodataFreeSpaceUnavailable
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		available, err := s.config.AvailableSpace(path)
		if err != nil {
			return errGeodataFreeSpaceUnavailable
		}
		if available < need {
			return errGeodataFreeSpaceInsufficient
		}
	}
	return nil
}

func (s *GeodataService) savePreviousGeneration(meta geodataSetMetadata) (string, error) {
	if !validGeodataSetMetadata(meta) {
		return "", errGeodataSetInvalid
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
	for _, item := range meta.Items {
		if err := copyGeodataFile(item, filepath.Join(s.config.AssetDir, item.Name), filepath.Join(staging, item.Name)); err != nil {
			_ = s.removeOwned(staging)
			return "", err
		}
	}
	if err := writeGeodataPreviousMetadata(filepath.Join(staging, geodataMetadataName), meta, s.config.SyncDirectory); err != nil {
		_ = s.removeOwned(staging)
		return "", err
	}
	return staging, nil
}

func (s *GeodataService) promotePreviousGeneration() error {
	staging := s.stagingPath()
	if _, err := s.loadGeneration(staging); err != nil {
		return err
	}
	parent := filepath.Dir(s.config.PreviousDir)
	old := s.oldPreviousPath()
	if err := s.removeOwned(old); err != nil {
		return err
	}
	if info, err := os.Lstat(s.config.PreviousDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errGeodataSetInvalid
		}
		if _, err := s.loadGeneration(s.config.PreviousDir); err != nil {
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
	return s.config.SyncDirectory(parent)
}

func (s *GeodataService) settlePreviousGeneration() error {
	if err := s.removeOwned(s.oldPreviousPath()); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(s.config.PreviousDir))
}

func (s *GeodataService) loadPreviousGeneration() (loadedGeodataGeneration, error) {
	return s.loadGeneration(s.config.PreviousDir)
}

func (s *GeodataService) loadJournalPrevious(expected geodataSetMetadata) (loadedGeodataGeneration, error) {
	for _, directory := range []string{s.stagingPath(), s.config.PreviousDir} {
		generation, err := s.loadGeneration(directory)
		if err == nil && sameGeodataSetMetadata(generation.meta, expected) {
			return generation, nil
		}
	}
	return loadedGeodataGeneration{}, errGeodataSetInvalid
}

func (s *GeodataService) loadGeneration(directory string) (loadedGeodataGeneration, error) {
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != len(productGeodataCatalog)+1 {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	metadata, err := readPrivateComponentFile(filepath.Join(directory, geodataMetadataName), MaxPreviousGenerationMetadata)
	if err != nil {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(metadata))
	decoder.DisallowUnknownFields()
	var record geodataPreviousRecord
	if err := decoder.Decode(&record); err != nil || record.SchemaVersion != GeodataTransactionSchemaVersion {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || len(record.Items) != len(productGeodataCatalog) {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	meta := geodataSetMetadata{Items: record.Items, Generation: record.Generation}
	if !validGeodataSetMetadata(meta) {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	actual, err := readGeodataFiles(directory, productGeodataCatalog, trueWithoutMetadata())
	if err != nil || !sameGeodataSetMetadata(actual, meta) {
		return loadedGeodataGeneration{}, errGeodataSetInvalid
	}
	return loadedGeodataGeneration{path: directory, meta: meta}, nil
}

func trueWithoutMetadata() bool { return false }

func writeGeodataPreviousMetadata(path string, meta geodataSetMetadata, syncDir func(string) error) error {
	if !validGeodataSetMetadata(meta) {
		return errGeodataSetInvalid
	}
	contents, err := json.Marshal(geodataPreviousRecord{SchemaVersion: GeodataTransactionSchemaVersion, Items: meta.Items, Generation: meta.Generation})
	if err != nil || len(contents)+1 > MaxPreviousGenerationMetadata {
		return errGeodataSetInvalid
	}
	return writeAtomicComponentFile(path, append(contents, '\n'), 0o600, syncDir)
}

func copyGeodataFile(meta geodataFileMetadata, source, destination string) error {
	if meta.Size <= 0 || meta.Size > MaxGeodataFileBytes || !isHexSHA256(meta.SHA256) {
		return errGeodataSetInvalid
	}
	before, err := os.Lstat(source)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() != meta.Size {
		return errGeodataSetInvalid
	}
	input, err := os.Open(source)
	if err != nil {
		return errGeodataSetInvalid
	}
	opened, err := input.Stat()
	if err != nil || opened.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || opened.Size() != meta.Size || !os.SameFile(before, opened) {
		_ = input.Close()
		return errGeodataSetInvalid
	}
	parent := filepath.Dir(destination)
	if err := ensurePrivateDirectory(parent); err != nil {
		_ = input.Close()
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(meta.Mode))
	if err != nil {
		_ = input.Close()
		return errGeodataSetInvalid
	}
	hash := sha256.New()
	count, copyErr := copyXrayBytes(context.Background(), io.MultiWriter(output, hash), input, MaxGeodataFileBytes)
	closeInputErr := input.Close()
	syncErr := output.Sync()
	closeErr := output.Close()
	after, afterErr := os.Lstat(source)
	if copyErr != nil || closeInputErr != nil || syncErr != nil || closeErr != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || count != meta.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), meta.SHA256) {
		_ = os.Remove(destination)
		return errGeodataSetInvalid
	}
	if err := os.Chmod(destination, os.FileMode(meta.Mode)); err != nil {
		_ = os.Remove(destination)
		return errGeodataSetInvalid
	}
	return syncDirectory(parent)
}

func (s *GeodataService) activateGeodataSet(ctx context.Context, source string, expected geodataSetMetadata) error {
	return s.activateGeodataSetInternal(ctx, source, expected, true)
}

func (s *GeodataService) activateGeodataSetInternal(ctx context.Context, source string, expected geodataSetMetadata, injectFiles bool) error {
	if !validGeodataSetMetadata(expected) || source == "" {
		return errGeodataSetInvalid
	}
	actual, err := readGeodataFiles(source, productGeodataCatalog, false)
	if err != nil || !sameGeodataSetMetadata(actual, expected) {
		return errGeodataSetInvalid
	}
	parent := s.config.AssetDir
	info, err := os.Lstat(parent)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errGeodataSetInvalid
	}
	if err := s.prepareActivationTempDir(); err != nil {
		return errGeodataSetInvalid
	}
	activationDir := s.activationTempDir()
	type replacement struct{ temporary, target string }
	replacements := make([]replacement, 0, len(expected.Items))
	cleanup := func() {
		for _, item := range replacements {
			_ = os.Remove(item.temporary)
		}
		_ = s.config.SyncDirectory(activationDir)
	}
	defer cleanup()
	for _, item := range expected.Items {
		target := filepath.Join(parent, item.Name)
		if filepath.Base(target) != item.Name || filepath.Dir(target) != parent {
			return errGeodataSetInvalid
		}
		if targetInfo, statErr := os.Lstat(target); statErr == nil {
			if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
				return errGeodataSetInvalid
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return errGeodataSetInvalid
		}
		temporary, err := os.CreateTemp(activationDir, ".owned-")
		if err != nil {
			return errGeodataSetInvalid
		}
		temporaryPath := temporary.Name()
		if err := temporary.Chmod(os.FileMode(item.Mode)); err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
			return errGeodataSetInvalid
		}
		input, err := os.Open(filepath.Join(source, item.Name))
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
			return errGeodataSetInvalid
		}
		hash := sha256.New()
		count, copyErr := copyXrayBytes(ctx, io.MultiWriter(temporary, hash), input, MaxGeodataFileBytes)
		closeInputErr := input.Close()
		syncErr := temporary.Sync()
		closeErr := temporary.Close()
		if copyErr != nil || closeInputErr != nil || syncErr != nil || closeErr != nil || count != item.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), item.SHA256) {
			_ = os.Remove(temporaryPath)
			return errGeodataSetInvalid
		}
		replacements = append(replacements, replacement{temporary: temporaryPath, target: target})
	}
	if err := s.config.SyncDirectory(activationDir); err != nil {
		return errGeodataSetInvalid
	}
	for _, item := range replacements {
		if err := renameComponentFile(item.temporary, item.target); err != nil {
			return errGeodataSetInvalid
		}
		if injectFiles {
			if err := s.inject(GeodataStageFileCommitted); err != nil {
				return err
			}
		}
	}
	if err := s.config.SyncDirectory(parent); err != nil {
		return errGeodataSetInvalid
	}
	return nil
}

func renameComponentFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	// Linux is the supported appliance target and uses atomic replacement. The
	// fallback keeps synthetic Windows qualification usable without widening
	// the production surface.
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func (s *GeodataService) copySetToCandidate(source string, expected geodataSetMetadata) (string, error) {
	stage, err := s.newStagingDir()
	if err != nil {
		return "", err
	}
	for _, item := range expected.Items {
		if err := copyGeodataFile(item, filepath.Join(source, item.Name), filepath.Join(stage, item.Name)); err != nil {
			_ = s.removeOwned(stage)
			return "", err
		}
	}
	if err := s.config.SyncDirectory(stage); err != nil {
		_ = s.removeOwned(stage)
		return "", err
	}
	return stage, nil
}

func (s *GeodataService) promoteCandidateAsPrevious(source string, expected geodataSetMetadata) error {
	stage := s.stagingPath()
	if err := s.removeOwned(stage); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(stage); err != nil {
		return err
	}
	for _, item := range expected.Items {
		if err := copyGeodataFile(item, filepath.Join(source, item.Name), filepath.Join(stage, item.Name)); err != nil {
			_ = s.removeOwned(stage)
			return err
		}
	}
	if err := writeGeodataPreviousMetadata(filepath.Join(stage, geodataMetadataName), expected, s.config.SyncDirectory); err != nil {
		_ = s.removeOwned(stage)
		return err
	}
	if err := s.promotePreviousGeneration(); err != nil {
		return err
	}
	return s.settlePreviousGeneration()
}

func (s *GeodataService) restorePreviousAfterPromotionFailure(journal geodataTransactionJournal) error {
	old, err := s.loadGeneration(s.oldPreviousPath())
	if err != nil {
		return errGeodataSetInvalid
	}
	if journal.Operation == geodataOperationRollback && !sameGeodataSetMetadata(old.meta, journal.Candidate) {
		return errGeodataSetInvalid
	}
	parent := filepath.Dir(s.config.PreviousDir)
	if _, err := os.Lstat(s.config.PreviousDir); err == nil {
		if _, err := s.loadGeneration(s.config.PreviousDir); err != nil {
			return err
		}
		if _, err := os.Lstat(s.stagingPath()); err == nil {
			return errGeodataSetInvalid
		}
		if err := os.Rename(s.config.PreviousDir, s.stagingPath()); err != nil {
			return err
		}
		if err := s.config.SyncDirectory(parent); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(s.oldPreviousPath(), s.config.PreviousDir); err != nil {
		return err
	}
	return s.config.SyncDirectory(parent)
}

func (s *GeodataService) pathPresent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *GeodataService) readJournal() (geodataTransactionJournal, bool, error) {
	kind, present, err := componentJournalKind(s.config.JournalPath)
	if err != nil {
		return geodataTransactionJournal{}, false, errGeodataJournalInvalid
	}
	if !present || kind != KindGeodata {
		return geodataTransactionJournal{}, false, nil
	}
	contents, err := readPrivateComponentFile(s.config.JournalPath, MaxComponentJournalBytes)
	if err != nil {
		return geodataTransactionJournal{}, false, errGeodataJournalInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var journal geodataTransactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return geodataTransactionJournal{}, false, errGeodataJournalInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return geodataTransactionJournal{}, false, errGeodataJournalInvalid
	}
	if err := validateGeodataJournal(journal); err != nil {
		return geodataTransactionJournal{}, false, err
	}
	return journal, true, nil
}

func validateGeodataJournal(value geodataTransactionJournal) error {
	if value.SchemaVersion != GeodataTransactionSchemaVersion || value.Component != string(KindGeodata) || value.Operation != geodataOperationUpdate && value.Operation != geodataOperationRollback {
		return errGeodataJournalInvalid
	}
	switch value.Phase {
	case geodataPhasePrepared, geodataPhaseFilesCommitted, geodataPhaseRuntimeVerified:
	default:
		return errGeodataJournalInvalid
	}
	if !validGeodataSetMetadata(value.Previous) || !validGeodataSetMetadata(value.Candidate) {
		return errGeodataJournalInvalid
	}
	return nil
}

func (s *GeodataService) writeJournal(value geodataTransactionJournal) error {
	if err := validateGeodataJournal(value); err != nil {
		return err
	}
	contents, err := json.Marshal(value)
	if err != nil || len(contents)+1 > MaxComponentJournalBytes {
		return errGeodataJournalInvalid
	}
	return writeAtomicComponentFile(s.config.JournalPath, append(contents, '\n'), 0o600, s.config.SyncDirectory)
}

func (s *GeodataService) clearJournal() error {
	info, err := os.Lstat(s.config.JournalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return errGeodataJournalInvalid
	}
	contents, readErr := readPrivateComponentFile(s.config.JournalPath, MaxComponentJournalBytes)
	if readErr != nil {
		return errGeodataJournalInvalid
	}
	if err := os.Remove(s.config.JournalPath); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.JournalPath)); err != nil {
		// An unlink followed by a failed directory sync is not treated as a
		// durable clear. Restore the bounded journal so startup still has a
		// recovery decision instead of observing an ambiguous empty state.
		if restoreErr := writeAtomicComponentFile(s.config.JournalPath, contents, 0o600, s.config.SyncDirectory); restoreErr != nil {
			return restoreErr
		}
		return err
	}
	return nil
}

func (s *GeodataService) cleanupTransactionResidue() error {
	if err := s.cleanupNonPreviousResidue(); err != nil {
		return err
	}
	return s.removeOwnedAndSync(s.stagingPath())
}

func (s *GeodataService) cleanupNonPreviousResidue() error {
	if err := s.removeOwned(s.oldPreviousPath()); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.PreviousDir)); err != nil {
		return err
	}
	if err := s.removeOwned(s.config.StagingDir); err != nil {
		return err
	}
	if err := s.config.SyncDirectory(filepath.Dir(s.config.StagingDir)); err != nil {
		return err
	}
	return s.removeActivationTemps()
}

func (s *GeodataService) removeActivationTemps() error {
	info, err := os.Lstat(s.activationTempDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errGeodataSetInvalid
	}
	if err := s.validateActivationTempLocation(); err != nil {
		return err
	}
	if err := s.activationTempOwnerValid(); err != nil {
		return err
	}
	if err := s.removeOwned(s.activationTempDir()); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(s.activationTempDir()))
}

func (s *GeodataService) removeOwned(path string) error {
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

func (s *GeodataService) removeOwnedAndSync(path string) error {
	if err := s.removeOwned(path); err != nil {
		return err
	}
	return s.config.SyncDirectory(filepath.Dir(path))
}
