package components

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	MutationSchemaVersion = 1

	MutationOperationUpdate   = "update"
	MutationOperationRollback = "rollback"
	MutationChannelStable     = "stable"
	MutationChannelDev        = "dev"

	DefaultMutationPreviewTTL     = 5 * time.Minute
	DefaultMutationMaxPreviews    = 16
	DefaultMutationPreviewTimeout = MaxCheckDuration
	DefaultMutationWaitTimeout    = 15 * time.Second
	DefaultMutationResponseGrace  = 15 * time.Second
	// The transaction services already include their authority admission inside
	// their transaction context. Shared component-gate admission is bounded
	// separately by DefaultMutationWaitTimeout, so this is the full transaction
	// budget after the broker has admitted the operation.
	DefaultMutationOperationTimeout = DefaultXKeenTransactionTimeout + DefaultXKeenAuthorityWaitTimeout
	// Ordinary transaction recovery deliberately runs on an independent bounded
	// context after a late activation failure. Keep that recovery reserve in the
	// synchronous HTTP response window as well.
	DefaultMutationRecoveryTimeout = max(DefaultXrayRollbackTimeout, DefaultGeodataRollbackTimeout, DefaultXKeenRollbackTimeout)
	MaxMutationTokenBytes          = 256

	mutationTokenBytes = 32
)

var (
	ErrInvalidMutationRequest      = errors.New("component mutation request is invalid")
	ErrMutationUnavailable         = errors.New("component mutation is unavailable")
	ErrMutationBusy                = errors.New("component mutation is busy")
	ErrMutationPreviewExpired      = errors.New("component mutation preview is expired")
	ErrMutationPreviewStale        = errors.New("component mutation preview is stale")
	ErrMutationOperationMismatch   = errors.New("component mutation operation does not match the preview")
	ErrMutationNoPrevious          = errors.New("component mutation has no previous generation")
	ErrMutationMaintenance         = errors.New("component mutation is in maintenance")
	ErrMutationMetadataUnavailable = errors.New("component mutation metadata is unavailable")
	ErrMutationCandidateRejected   = errors.New("component mutation candidate was rejected")
	ErrMutationTransactionFailed   = errors.New("component transaction failed; previous generation restored")
	ErrMutationTransactionUnproven = errors.New("component transaction failed; outcome is not proven")
	ErrMutationRollbackUnproven    = errors.New("component rollback or recovery is not proven")
)

// MutationRequest is the closed F1 intent shape. A rollback request omits
// channel; the unexported presence bit lets strict JSON decoding reject an
// explicitly supplied rollback channel while still allowing internal typed
// callers to construct the zero value.
type MutationRequest struct {
	Component ComponentKind `json:"component"`
	Operation string        `json:"operation"`
	Channel   string        `json:"channel,omitempty"`

	channelPresent bool
}

func (r *MutationRequest) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateMutationFields(data, "component", "operation", "channel"); err != nil {
		return err
	}
	type wireMutationRequest struct {
		Component ComponentKind `json:"component"`
		Operation string        `json:"operation"`
		Channel   string        `json:"channel"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value wireMutationRequest
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("mutation request contains trailing JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("mutation request must be a JSON object")
	}
	_, channelPresent := fields["channel"]
	*r = MutationRequest{Component: value.Component, Operation: value.Operation, Channel: value.Channel, channelPresent: channelPresent}
	return nil
}

func ValidateMutationRequest(request MutationRequest) error {
	switch request.Operation {
	case MutationOperationUpdate:
		switch request.Component {
		case KindXray, KindGeodata:
			if request.Channel == MutationChannelStable {
				return nil
			}
		case KindXKeen:
			if request.Channel == MutationChannelDev {
				return nil
			}
		}
	case MutationOperationRollback:
		if request.Channel == "" && !request.channelPresent {
			switch request.Component {
			case KindXray, KindGeodata, KindXKeen:
				return nil
			}
		}
	}
	return ErrInvalidMutationRequest
}

// MutationTokenRequest is the closed body accepted by Apply, Rollback and
// Cancel. The token is opaque and is never a path, URL, generation or caller
// supplied component identity.
type MutationTokenRequest struct {
	PreviewToken string `json:"previewToken"`
}

func (r *MutationTokenRequest) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateMutationFields(data, "previewToken"); err != nil {
		return err
	}
	type wireMutationTokenRequest MutationTokenRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value wireMutationTokenRequest
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("mutation token request contains trailing JSON")
	}
	*r = MutationTokenRequest(value)
	return nil
}

func rejectDuplicateMutationFields(data []byte, allowedNames ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return errors.New("mutation request must be a JSON object")
	}
	allowed := make(map[string]struct{}, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowedNames))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return errors.New("mutation request field name is invalid")
		}
		if _, allowed := allowed[name]; !allowed {
			return errors.New("mutation request contains an unknown field")
		}
		if _, exists := seen[name]; exists {
			return errors.New("mutation request contains duplicate fields")
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return errors.New("mutation request object is not closed")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("mutation request contains trailing JSON")
	}
	return nil
}

func ValidateMutationToken(token string) error {
	if token == "" || len(token) > MaxMutationTokenBytes {
		return ErrInvalidMutationRequest
	}
	return nil
}

type MutationItem struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Tag       string `json:"tag,omitempty"`
	AssetName string `json:"assetName,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

type MutationCandidate struct {
	Version         string         `json:"version,omitempty"`
	Generation      string         `json:"generation,omitempty"`
	AssetName       string         `json:"assetName,omitempty"`
	SizeBytes       int64          `json:"sizeBytes,omitempty"`
	SHA256          string         `json:"sha256,omitempty"`
	BuildCommitSHA  string         `json:"buildCommitSha,omitempty"`
	SourceCommitSHA string         `json:"sourceCommitSha,omitempty"`
	BlobSHA         string         `json:"blobSha,omitempty"`
	Items           []MutationItem `json:"items,omitempty"`
}

type MutationPrevious struct {
	Version        string         `json:"version,omitempty"`
	Generation     string         `json:"generation,omitempty"`
	SizeBytes      int64          `json:"sizeBytes,omitempty"`
	SHA256         string         `json:"sha256,omitempty"`
	BuildCommitSHA string         `json:"buildCommitSha,omitempty"`
	Entries        int            `json:"entries,omitempty"`
	Bytes          int64          `json:"bytes,omitempty"`
	MarkerPresent  bool           `json:"markerPresent,omitempty"`
	MarkerSHA256   string         `json:"markerSha256,omitempty"`
	Items          []MutationItem `json:"items,omitempty"`
}

type MutationPreview struct {
	SchemaVersion int                `json:"schemaVersion"`
	PreviewToken  string             `json:"previewToken"`
	Component     ComponentKind      `json:"component"`
	Operation     string             `json:"operation"`
	Channel       string             `json:"channel,omitempty"`
	ExpiresAt     time.Time          `json:"expiresAt"`
	Candidate     *MutationCandidate `json:"candidate,omitempty"`
	Previous      *MutationPrevious  `json:"previous,omitempty"`
}

type MutationResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	Component     ComponentKind  `json:"component"`
	Operation     string         `json:"operation"`
	Channel       string         `json:"channel,omitempty"`
	State         string         `json:"state"`
	Version       string         `json:"version,omitempty"`
	Generation    string         `json:"generation,omitempty"`
	Items         []MutationItem `json:"items,omitempty"`
}

// These interfaces are intentionally one typed seam per component. The F1
// broker cannot receive a generic descriptor, command, path, URL or package
// manager capability.
type XrayMutationBackend interface {
	Ready() error
	PreviousGeneration() (XrayPreviousGeneration, error)
	Apply(context.Context, XrayReleaseIdentity) error
	RollbackExpected(context.Context, XrayPreviousGeneration) error
}

type GeodataMutationBackend interface {
	Ready() error
	PreviousGeneration() (GeodataPreviousGeneration, error)
	Apply(context.Context, GeodataCandidateSet) error
	RollbackExpected(context.Context, GeodataPreviousGeneration) error
}

type XKeenMutationBackend interface {
	Ready() error
	PreviousGeneration() (XKeenPreviousGeneration, error)
	Apply(context.Context, XKeenReleaseIdentity) error
	RollbackExpected(context.Context, XKeenPreviousGeneration) error
}

type MutationConfig struct {
	XrayResolver    XrayCandidateResolver
	Xray            XrayMutationBackend
	GeodataResolver GeodataCandidateResolver
	Geodata         GeodataMutationBackend
	XKeenResolver   XKeenCandidateResolver
	XKeen           XKeenMutationBackend

	PreviewTTL       time.Duration
	MaxPreviews      int
	PreviewTimeout   time.Duration
	AdmissionTimeout time.Duration
	OperationTimeout time.Duration
	MutationGate     *ComponentMutationGate
	Now              func() time.Time
	Random           io.Reader
}

type MutationService struct {
	config MutationConfig

	mu       sync.Mutex
	sequence uint64
	previews map[string]mutationPreviewEntry
}

type mutationPreviewEntry struct {
	Token     string
	Binding   string
	Request   MutationRequest
	Sequence  uint64
	IssuedAt  time.Time
	ExpiresAt time.Time

	XrayCandidate    *XrayReleaseIdentity
	GeodataCandidate *GeodataCandidateSet
	XKeenCandidate   *XKeenReleaseIdentity

	XrayPrevious    *XrayPreviousGeneration
	GeodataPrevious *GeodataPreviousGeneration
	XKeenPrevious   *XKeenPreviousGeneration
}

var componentMutationPreviewGate = make(chan struct{}, 1)

func NewMutationService(config MutationConfig) *MutationService {
	if config.PreviewTTL <= 0 || config.PreviewTTL > DefaultMutationPreviewTTL {
		config.PreviewTTL = DefaultMutationPreviewTTL
	}
	if config.MaxPreviews <= 0 {
		config.MaxPreviews = DefaultMutationMaxPreviews
	}
	if config.MaxPreviews > DefaultMutationMaxPreviews {
		config.MaxPreviews = DefaultMutationMaxPreviews
	}
	if config.PreviewTimeout <= 0 {
		config.PreviewTimeout = DefaultMutationPreviewTimeout
	}
	if config.AdmissionTimeout <= 0 || config.AdmissionTimeout > DefaultMutationWaitTimeout {
		config.AdmissionTimeout = DefaultMutationWaitTimeout
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = DefaultMutationOperationTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &MutationService{config: config, previews: make(map[string]mutationPreviewEntry, config.MaxPreviews)}
}

// NewComponentMutationService is a descriptive constructor alias for the
// HTTP-facing role.
func NewComponentMutationService(config MutationConfig) *MutationService {
	return NewMutationService(config)
}

func (s *MutationService) Supports(component ComponentKind, channel string) bool {
	if s == nil {
		return false
	}
	switch component {
	case KindXray:
		return channel == MutationChannelStable && s.config.XrayResolver != nil && s.config.Xray != nil
	case KindGeodata:
		return channel == MutationChannelStable && s.config.GeodataResolver != nil && s.config.Geodata != nil
	case KindXKeen:
		return channel == MutationChannelDev && s.config.XKeenResolver != nil && s.config.XKeen != nil
	default:
		return false
	}
}

func (s *MutationService) Preview(ctx context.Context, binding string, request MutationRequest) (MutationPreview, error) {
	if s == nil || strings.TrimSpace(binding) == "" {
		return MutationPreview{}, ErrInvalidMutationRequest
	}
	if err := ValidateMutationRequest(request); err != nil {
		return MutationPreview{}, err
	}
	if !s.supportsRequest(request) {
		return MutationPreview{}, ErrMutationUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return MutationPreview{}, ErrMutationMetadataUnavailable
	}
	select {
	case componentMutationPreviewGate <- struct{}{}:
		defer func() { <-componentMutationPreviewGate }()
	default:
		return MutationPreview{}, ErrMutationBusy
	}

	if err := s.backendReady(request.Component); err != nil {
		return MutationPreview{}, err
	}
	previewContext, cancel := context.WithTimeout(ctx, s.config.PreviewTimeout)
	defer cancel()

	entry := mutationPreviewEntry{Binding: binding, Request: request}
	switch request.Operation {
	case MutationOperationUpdate:
		if err := s.resolveUpdate(previewContext, request, &entry); err != nil {
			return MutationPreview{}, err
		}
	case MutationOperationRollback:
		if err := s.resolveRollback(request.Component, &entry); err != nil {
			return MutationPreview{}, err
		}
	default:
		return MutationPreview{}, ErrInvalidMutationRequest
	}
	return s.storePreview(entry)
}

func (s *MutationService) Apply(ctx context.Context, binding, token string) (MutationResult, error) {
	entry, err := s.take(binding, token, MutationOperationUpdate)
	if err != nil {
		return MutationResult{}, err
	}
	operationContext, releaseOperation, err := s.beginOperation(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer releaseOperation()

	switch entry.Request.Component {
	case KindXray:
		if entry.XrayCandidate == nil || s.config.Xray == nil {
			return MutationResult{}, ErrMutationUnavailable
		}
		err = s.config.Xray.Apply(operationContext, *entry.XrayCandidate)
	case KindGeodata:
		if entry.GeodataCandidate == nil || s.config.Geodata == nil {
			return MutationResult{}, ErrMutationUnavailable
		}
		err = s.config.Geodata.Apply(operationContext, *entry.GeodataCandidate)
	case KindXKeen:
		if entry.XKeenCandidate == nil || s.config.XKeen == nil {
			return MutationResult{}, ErrMutationUnavailable
		}
		err = s.config.XKeen.Apply(operationContext, *entry.XKeenCandidate)
	default:
		return MutationResult{}, ErrInvalidMutationRequest
	}
	if err != nil {
		return MutationResult{}, s.classifyOperationError(entry.Request.Component, entry.Request.Operation, err)
	}
	return mutationResult(entry, "applied"), nil
}

func (s *MutationService) Rollback(ctx context.Context, binding, token string) (MutationResult, error) {
	entry, err := s.take(binding, token, MutationOperationRollback)
	if err != nil {
		return MutationResult{}, err
	}
	operationContext, releaseOperation, err := s.beginOperation(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer releaseOperation()

	switch entry.Request.Component {
	case KindXray:
		if entry.XrayPrevious == nil || s.config.Xray == nil {
			return MutationResult{}, ErrMutationUnavailable
		}
		err = s.config.Xray.RollbackExpected(operationContext, *entry.XrayPrevious)
	case KindGeodata:
		if entry.GeodataPrevious == nil || s.config.Geodata == nil {
			return MutationResult{}, ErrMutationUnavailable
		}
		err = s.config.Geodata.RollbackExpected(operationContext, *entry.GeodataPrevious)
	case KindXKeen:
		if entry.XKeenPrevious == nil || s.config.XKeen == nil {
			return MutationResult{}, ErrMutationUnavailable
		}
		err = s.config.XKeen.RollbackExpected(operationContext, *entry.XKeenPrevious)
	default:
		return MutationResult{}, ErrInvalidMutationRequest
	}
	if err != nil {
		return MutationResult{}, s.classifyOperationError(entry.Request.Component, entry.Request.Operation, err)
	}
	return mutationResult(entry, "rolled-back"), nil
}

// Cancel is deliberately idempotent. A token belonging to another session is
// not removed, and neither case reveals token ownership to the caller.
func (s *MutationService) Cancel(binding, token string) {
	if s == nil || strings.TrimSpace(binding) == "" || ValidateMutationToken(token) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.previews[token]
	if ok && entry.Binding == binding {
		delete(s.previews, token)
	}
}

func (s *MutationService) Invalidate(binding string) {
	if s == nil || binding == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, entry := range s.previews {
		if entry.Binding == binding {
			delete(s.previews, token)
		}
	}
}

func (s *MutationService) InvalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.previews = make(map[string]mutationPreviewEntry, s.config.MaxPreviews)
	s.mu.Unlock()
}

func (s *MutationService) supportsRequest(request MutationRequest) bool {
	if request.Operation == MutationOperationUpdate {
		return s.Supports(request.Component, request.Channel)
	}
	if request.Operation != MutationOperationRollback {
		return false
	}
	switch request.Component {
	case KindXray:
		return s.config.Xray != nil
	case KindGeodata:
		return s.config.Geodata != nil
	case KindXKeen:
		return s.config.XKeen != nil
	default:
		return false
	}
}

// beginOperation admits Apply/Rollback through the one shared component gate
// before creating the transaction context. The gate is acquired exactly once
// here; the existing transaction core sees the ownership marker and keeps its
// established Coordinator -> authority lease order without a second gate
// acquisition. A token is already consumed when this is called, so a bounded
// admission conflict cannot be replayed.
func (s *MutationService) beginOperation(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var (
		gate    = s.config.MutationGate
		release func()
	)
	if gate != nil {
		admissionContext, cancelAdmission := context.WithTimeout(ctx, s.config.AdmissionTimeout)
		var err error
		release, err = gate.Acquire(admissionContext)
		cancelAdmission()
		if err != nil {
			return nil, nil, ErrMutationBusy
		}
	}
	operationContext, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	if gate != nil {
		operationContext = withComponentMutationGate(operationContext, gate)
	}
	return operationContext, func() {
		cancel()
		if release != nil {
			release()
		}
	}, nil
}

func (s *MutationService) backendReady(component ComponentKind) error {
	var err error
	switch component {
	case KindXray:
		err = s.config.Xray.Ready()
	case KindGeodata:
		err = s.config.Geodata.Ready()
	case KindXKeen:
		err = s.config.XKeen.Ready()
	default:
		return ErrMutationUnavailable
	}
	if err == nil {
		return nil
	}
	return classifyReadyError(err)
}

func classifyReadyError(err error) error {
	switch {
	case errors.Is(err, ErrXrayRecoveryRequired), errors.Is(err, ErrXrayRecoveryConflict), errors.Is(err, ErrXrayRecoveryFailed),
		errors.Is(err, ErrGeodataRecoveryRequired), errors.Is(err, ErrGeodataRecoveryConflict), errors.Is(err, ErrGeodataRecoveryFailed),
		errors.Is(err, ErrXKeenRecoveryRequired), errors.Is(err, ErrXKeenRecoveryConflict), errors.Is(err, ErrXKeenRecoveryFailed):
		return ErrMutationMaintenance
	case errors.Is(err, ErrXrayTransactionUnavailable), errors.Is(err, ErrGeodataTransactionUnavailable), errors.Is(err, ErrXKeenTransactionUnavailable):
		return ErrMutationUnavailable
	default:
		return ErrMutationUnavailable
	}
}

func (s *MutationService) resolveUpdate(ctx context.Context, request MutationRequest, entry *mutationPreviewEntry) error {
	switch request.Component {
	case KindXray:
		candidate, err := s.config.XrayResolver.ResolveXray(ctx)
		if err != nil {
			return classifyResolverError(err, KindXray)
		}
		if !validXrayIdentity(candidate) {
			return ErrMutationCandidateRejected
		}
		copy := candidate
		entry.XrayCandidate = &copy
	case KindGeodata:
		candidate, err := s.config.GeodataResolver.ResolveGeodata(ctx)
		if err != nil {
			return classifyResolverError(err, KindGeodata)
		}
		if err := validateGeodataCandidateSet(candidate); err != nil {
			return ErrMutationCandidateRejected
		}
		copy := cloneMutationGeodataCandidateSet(candidate)
		entry.GeodataCandidate = &copy
	case KindXKeen:
		candidate, err := s.config.XKeenResolver.ResolveXKeen(ctx)
		if err != nil {
			return classifyResolverError(err, KindXKeen)
		}
		if !validXKeenIdentity(candidate) {
			return ErrMutationCandidateRejected
		}
		copy := candidate
		entry.XKeenCandidate = &copy
	default:
		return ErrInvalidMutationRequest
	}
	return nil
}

func (s *MutationService) resolveRollback(component ComponentKind, entry *mutationPreviewEntry) error {
	switch component {
	case KindXray:
		previous, err := s.config.Xray.PreviousGeneration()
		if err != nil {
			return classifyPreviousError(err, KindXray)
		}
		copy := previous
		entry.XrayPrevious = &copy
	case KindGeodata:
		previous, err := s.config.Geodata.PreviousGeneration()
		if err != nil {
			return classifyPreviousError(err, KindGeodata)
		}
		copy := GeodataPreviousGeneration{Generation: previous.Generation, Items: append([]GeodataPreviousItem(nil), previous.Items...)}
		entry.GeodataPrevious = &copy
	case KindXKeen:
		previous, err := s.config.XKeen.PreviousGeneration()
		if err != nil {
			return classifyPreviousError(err, KindXKeen)
		}
		copy := previous
		entry.XKeenPrevious = &copy
	default:
		return ErrInvalidMutationRequest
	}
	return nil
}

func classifyResolverError(err error, component ComponentKind) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrMutationMetadataUnavailable
	}
	switch component {
	case KindXray:
		if errors.Is(err, ErrXrayCandidateRejected) {
			return ErrMutationCandidateRejected
		}
	case KindGeodata:
		if errors.Is(err, ErrGeodataCandidateRejected) {
			return ErrMutationCandidateRejected
		}
	case KindXKeen:
		if errors.Is(err, ErrXKeenCandidateRejected) {
			return ErrMutationCandidateRejected
		}
	}
	return ErrMutationMetadataUnavailable
}

func classifyPreviousError(err error, component ComponentKind) error {
	switch component {
	case KindXray:
		if errors.Is(err, ErrXrayPreviousUnavailable) {
			return ErrMutationNoPrevious
		}
	case KindGeodata:
		if errors.Is(err, ErrGeodataPreviousUnavailable) {
			return ErrMutationNoPrevious
		}
	case KindXKeen:
		if errors.Is(err, ErrXKeenPreviousUnavailable) {
			return ErrMutationNoPrevious
		}
	}
	return classifyReadyError(err)
}

func classifyMutationError(component ComponentKind, operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrMutationPreviewStale) || errors.Is(err, ErrMutationNoPrevious) {
		return err
	}
	if errors.Is(err, ErrMutationTransactionUnproven) {
		return ErrMutationTransactionUnproven
	}
	switch component {
	case KindXray:
		switch {
		case errors.Is(err, ErrXrayCandidateStale):
			return ErrMutationPreviewStale
		case errors.Is(err, ErrXrayPreviousUnavailable):
			return ErrMutationNoPrevious
		case errors.Is(err, ErrXrayBusy), errors.Is(err, ErrXrayAuthorityBusy):
			return ErrMutationBusy
		case errors.Is(err, ErrXrayRecoveryRequired), errors.Is(err, ErrXrayRecoveryConflict), errors.Is(err, ErrXrayRecoveryFailed), errors.Is(err, ErrXrayTransactionUnavailable), errors.Is(err, ErrXrayAuthorityUnavailable):
			return ErrMutationMaintenance
		case errors.Is(err, ErrXrayResolutionUnavailable):
			return ErrMutationMetadataUnavailable
		case errors.Is(err, ErrXrayCandidateRejected), errors.Is(err, ErrXrayArtifactRejected):
			return ErrMutationCandidateRejected
		case errors.Is(err, ErrXrayRollbackFailed):
			return ErrMutationRollbackUnproven
		case errors.Is(err, ErrXrayApplyRestored):
			return ErrMutationTransactionFailed
		case errors.Is(err, ErrXrayApplyFailed):
			return ErrMutationTransactionUnproven
		}
	case KindGeodata:
		switch {
		case errors.Is(err, ErrGeodataCandidateStale):
			return ErrMutationPreviewStale
		case errors.Is(err, ErrGeodataPreviousUnavailable):
			return ErrMutationNoPrevious
		case errors.Is(err, ErrGeodataBusy), errors.Is(err, ErrGeodataAuthorityBusy):
			return ErrMutationBusy
		case errors.Is(err, ErrGeodataRecoveryRequired), errors.Is(err, ErrGeodataRecoveryConflict), errors.Is(err, ErrGeodataRecoveryFailed), errors.Is(err, ErrGeodataTransactionUnavailable), errors.Is(err, ErrGeodataAuthorityUnavailable):
			return ErrMutationMaintenance
		case errors.Is(err, ErrGeodataResolutionUnavailable):
			return ErrMutationMetadataUnavailable
		case errors.Is(err, ErrGeodataCandidateRejected), errors.Is(err, ErrGeodataArtifactRejected):
			return ErrMutationCandidateRejected
		case errors.Is(err, ErrGeodataRollbackFailed):
			return ErrMutationRollbackUnproven
		case errors.Is(err, ErrGeodataApplyRestored):
			return ErrMutationTransactionFailed
		case errors.Is(err, ErrGeodataApplyFailed):
			return ErrMutationTransactionUnproven
		}
	case KindXKeen:
		switch {
		case errors.Is(err, ErrXKeenCandidateStale):
			return ErrMutationPreviewStale
		case errors.Is(err, ErrXKeenPreviousUnavailable):
			return ErrMutationNoPrevious
		case errors.Is(err, ErrXKeenBusy), errors.Is(err, ErrXKeenAuthorityBusy):
			return ErrMutationBusy
		case errors.Is(err, ErrXKeenRecoveryRequired), errors.Is(err, ErrXKeenRecoveryConflict), errors.Is(err, ErrXKeenRecoveryFailed), errors.Is(err, ErrXKeenTransactionUnavailable), errors.Is(err, ErrXKeenAuthorityUnavailable):
			return ErrMutationMaintenance
		case errors.Is(err, ErrXKeenResolutionUnavailable):
			return ErrMutationMetadataUnavailable
		case errors.Is(err, ErrXKeenCandidateRejected), errors.Is(err, ErrXKeenArtifactRejected):
			return ErrMutationCandidateRejected
		case errors.Is(err, ErrXKeenRollbackFailed):
			return ErrMutationRollbackUnproven
		case errors.Is(err, ErrXKeenApplyRestored):
			return ErrMutationTransactionFailed
		case errors.Is(err, ErrXKeenApplyFailed):
			return ErrMutationTransactionUnproven
		}
	}
	if operation == MutationOperationRollback {
		return ErrMutationRollbackUnproven
	}
	return ErrMutationTransactionUnproven
}

func (s *MutationService) classifyOperationError(component ComponentKind, operation string, err error) error {
	classified := classifyMutationError(component, operation, err)
	switch classified {
	case ErrMutationTransactionFailed, ErrMutationTransactionUnproven, ErrMutationRollbackUnproven:
		// Continue below and verify that the core did not leave unresolved
		// maintenance state before exposing any operation outcome.
	default:
		return classified
	}
	// A few existing transaction failure points intentionally return a typed
	// failure while marking the core not-ready when recovery is required. Re-read
	// that state so the broker never reports a restored or neutral outcome for an
	// unresolved transaction.
	readiness := s.backendReady(component)
	if readiness == ErrMutationMaintenance || readiness == ErrMutationUnavailable {
		return readiness
	}
	return classified
}

func (s *MutationService) storePreview(entry mutationPreviewEntry) (MutationPreview, error) {
	for attempt := 0; attempt < 4; attempt++ {
		rawToken := make([]byte, mutationTokenBytes)
		if _, err := io.ReadFull(s.config.Random, rawToken); err != nil {
			return MutationPreview{}, ErrMutationUnavailable
		}
		token := base64.RawURLEncoding.EncodeToString(rawToken)
		now := s.clock()
		entry.Token = token
		entry.IssuedAt = now
		entry.ExpiresAt = now.Add(s.config.PreviewTTL)

		s.mu.Lock()
		s.purgeExpiredLocked(now)
		if _, exists := s.previews[token]; exists {
			s.mu.Unlock()
			continue
		}
		s.sequence++
		entry.Sequence = s.sequence
		for len(s.previews) >= s.config.MaxPreviews {
			s.evictOldestLocked()
		}
		s.previews[token] = entry
		s.mu.Unlock()
		return mutationPreview(entry), nil
	}
	return MutationPreview{}, ErrMutationUnavailable
}

func (s *MutationService) take(binding, token, operation string) (mutationPreviewEntry, error) {
	if s == nil || strings.TrimSpace(binding) == "" {
		return mutationPreviewEntry{}, ErrInvalidMutationRequest
	}
	if err := ValidateMutationToken(token); err != nil {
		return mutationPreviewEntry{}, err
	}
	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	entry, ok := s.previews[token]
	if !ok || entry.Binding != binding {
		return mutationPreviewEntry{}, ErrMutationPreviewExpired
	}
	if entry.Request.Operation != operation {
		return mutationPreviewEntry{}, ErrMutationOperationMismatch
	}
	delete(s.previews, token)
	return entry, nil
}

func (s *MutationService) purgeExpiredLocked(now time.Time) {
	for token, entry := range s.previews {
		if !now.Before(entry.ExpiresAt) {
			delete(s.previews, token)
		}
	}
}

func (s *MutationService) evictOldestLocked() {
	oldestToken := ""
	var oldest time.Time
	var oldestSequence uint64
	for token, entry := range s.previews {
		if oldestToken == "" || entry.Sequence < oldestSequence || entry.Sequence == oldestSequence && (entry.IssuedAt.Before(oldest) || entry.IssuedAt.Equal(oldest) && token < oldestToken) {
			oldestToken = token
			oldestSequence = entry.Sequence
			oldest = entry.IssuedAt
		}
	}
	if oldestToken != "" {
		delete(s.previews, oldestToken)
	}
}

func (s *MutationService) clock() time.Time {
	if s == nil || s.config.Now == nil {
		return time.Now().UTC()
	}
	return s.config.Now().UTC()
}

func mutationPreview(entry mutationPreviewEntry) MutationPreview {
	result := MutationPreview{
		SchemaVersion: MutationSchemaVersion,
		PreviewToken:  entry.Token,
		Component:     entry.Request.Component,
		Operation:     entry.Request.Operation,
		Channel:       entry.Request.Channel,
		ExpiresAt:     entry.ExpiresAt.UTC(),
	}
	if entry.XrayCandidate != nil {
		result.Candidate = mutationCandidateFromXray(*entry.XrayCandidate)
	}
	if entry.GeodataCandidate != nil {
		result.Candidate = mutationCandidateFromGeodata(*entry.GeodataCandidate)
	}
	if entry.XKeenCandidate != nil {
		result.Candidate = mutationCandidateFromXKeen(*entry.XKeenCandidate)
	}
	if entry.XrayPrevious != nil {
		result.Previous = mutationPreviousFromXray(*entry.XrayPrevious)
	}
	if entry.GeodataPrevious != nil {
		result.Previous = mutationPreviousFromGeodata(*entry.GeodataPrevious)
	}
	if entry.XKeenPrevious != nil {
		result.Previous = mutationPreviousFromXKeen(*entry.XKeenPrevious)
	}
	return result
}

func mutationResult(entry mutationPreviewEntry, state string) MutationResult {
	result := MutationResult{SchemaVersion: MutationSchemaVersion, Component: entry.Request.Component, Operation: entry.Request.Operation, Channel: entry.Request.Channel, State: state}
	if entry.XrayCandidate != nil {
		candidate := mutationCandidateFromXray(*entry.XrayCandidate)
		result.Version, result.Generation = candidate.Version, candidate.Generation
	}
	if entry.GeodataCandidate != nil {
		candidate := mutationCandidateFromGeodata(*entry.GeodataCandidate)
		result.Generation, result.Items = candidate.Generation, candidate.Items
	}
	if entry.XKeenCandidate != nil {
		candidate := mutationCandidateFromXKeen(*entry.XKeenCandidate)
		result.Version, result.Generation = candidate.Version, candidate.Generation
	}
	if entry.XrayPrevious != nil {
		previous := mutationPreviousFromXray(*entry.XrayPrevious)
		result.Version, result.Generation = previous.Version, previous.Generation
	}
	if entry.GeodataPrevious != nil {
		previous := mutationPreviousFromGeodata(*entry.GeodataPrevious)
		result.Generation, result.Items = previous.Generation, previous.Items
	}
	if entry.XKeenPrevious != nil {
		previous := mutationPreviousFromXKeen(*entry.XKeenPrevious)
		result.Version, result.Generation = previous.Version, previous.Generation
	}
	return result
}

func mutationCandidateFromXray(value XrayReleaseIdentity) *MutationCandidate {
	return &MutationCandidate{Version: value.Version, Generation: strings.ToLower(value.SHA256), AssetName: value.AssetName, SizeBytes: value.SizeBytes, SHA256: strings.ToLower(value.SHA256)}
}

func mutationCandidateFromGeodata(value GeodataCandidateSet) *MutationCandidate {
	items := make([]MutationItem, len(value.Items))
	for index, item := range value.Items {
		items[index] = MutationItem{ID: item.ID, Name: item.ActiveName, Tag: item.Tag, AssetName: item.AssetName, SizeBytes: item.SizeBytes, SHA256: strings.ToLower(item.SHA256)}
	}
	return &MutationCandidate{Generation: value.Generation, Items: items}
}

func mutationCandidateFromXKeen(value XKeenReleaseIdentity) *MutationCandidate {
	return &MutationCandidate{Version: value.Version, Generation: xkeenIdentityGeneration(value), AssetName: value.AssetName, SizeBytes: value.SizeBytes, SHA256: strings.ToLower(value.SHA256), BuildCommitSHA: value.CommitSHA, SourceCommitSHA: value.SourceParentSHA, BlobSHA: value.BlobSHA}
}

func mutationPreviousFromXray(value XrayPreviousGeneration) *MutationPrevious {
	return &MutationPrevious{Version: value.Version, Generation: value.Generation, SizeBytes: value.SizeBytes, SHA256: strings.ToLower(value.SHA256)}
}

func mutationPreviousFromGeodata(value GeodataPreviousGeneration) *MutationPrevious {
	items := make([]MutationItem, len(value.Items))
	for index, item := range value.Items {
		items[index] = MutationItem{ID: item.ID, Name: item.Name, SizeBytes: item.SizeBytes, SHA256: strings.ToLower(item.SHA256)}
	}
	return &MutationPrevious{Generation: value.Generation, Items: items}
}

func mutationPreviousFromXKeen(value XKeenPreviousGeneration) *MutationPrevious {
	return &MutationPrevious{Version: value.Version, Generation: value.Generation, Entries: value.Entries, Bytes: value.Bytes, MarkerPresent: value.MarkerPresent, MarkerSHA256: strings.ToLower(value.MarkerSHA256), BuildCommitSHA: value.BuildCommitSHA}
}

func cloneMutationGeodataCandidateSet(value GeodataCandidateSet) GeodataCandidateSet {
	return GeodataCandidateSet{Items: append([]GeodataReleaseIdentity(nil), value.Items...), Generation: value.Generation}
}
