package components

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type f1XrayResolver struct {
	identity XrayReleaseIdentity
	calls    atomic.Int32
	started  chan struct{}
	release  <-chan struct{}
}

func (r *f1XrayResolver) ResolveXray(ctx context.Context) (XrayReleaseIdentity, error) {
	r.calls.Add(1)
	if r.started != nil {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}
	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return XrayReleaseIdentity{}, ctx.Err()
		}
	}
	return r.identity, nil
}

type f1XrayBackend struct {
	mu          sync.Mutex
	readyErr    error
	previous    XrayPreviousGeneration
	previousErr error
	applied     []XrayReleaseIdentity
	rolledBack  []XrayPreviousGeneration
	applyErr    error
	rollbackErr error
}

func (b *f1XrayBackend) Ready() error { return b.readyErr }

func (b *f1XrayBackend) PreviousGeneration() (XrayPreviousGeneration, error) {
	return b.previous, b.previousErr
}

func (b *f1XrayBackend) Apply(_ context.Context, identity XrayReleaseIdentity) error {
	b.mu.Lock()
	b.applied = append(b.applied, identity)
	err := b.applyErr
	b.mu.Unlock()
	return err
}

func (b *f1XrayBackend) RollbackExpected(_ context.Context, previous XrayPreviousGeneration) error {
	b.mu.Lock()
	b.rolledBack = append(b.rolledBack, previous)
	err := b.rollbackErr
	b.mu.Unlock()
	return err
}

type f1GeodataResolver struct{ set GeodataCandidateSet }

func (r f1GeodataResolver) ResolveGeodata(context.Context) (GeodataCandidateSet, error) {
	return r.set, nil
}

type f1GeodataBackend struct {
	previous  GeodataPreviousGeneration
	updates   []GeodataCandidateSet
	rollbacks []GeodataPreviousGeneration
}

func (b *f1GeodataBackend) Ready() error { return nil }
func (b *f1GeodataBackend) PreviousGeneration() (GeodataPreviousGeneration, error) {
	return b.previous, nil
}
func (b *f1GeodataBackend) Apply(_ context.Context, set GeodataCandidateSet) error {
	b.updates = append(b.updates, set)
	return nil
}
func (b *f1GeodataBackend) RollbackExpected(_ context.Context, previous GeodataPreviousGeneration) error {
	b.rollbacks = append(b.rollbacks, previous)
	return nil
}

type f1XKeenResolver struct{ identity XKeenReleaseIdentity }

func (r f1XKeenResolver) ResolveXKeen(context.Context) (XKeenReleaseIdentity, error) {
	return r.identity, nil
}

type f1XKeenBackend struct {
	previous  XKeenPreviousGeneration
	updates   []XKeenReleaseIdentity
	rollbacks []XKeenPreviousGeneration
}

func (b *f1XKeenBackend) Ready() error { return nil }
func (b *f1XKeenBackend) PreviousGeneration() (XKeenPreviousGeneration, error) {
	return b.previous, nil
}
func (b *f1XKeenBackend) Apply(_ context.Context, identity XKeenReleaseIdentity) error {
	b.updates = append(b.updates, identity)
	return nil
}
func (b *f1XKeenBackend) RollbackExpected(_ context.Context, previous XKeenPreviousGeneration) error {
	b.rollbacks = append(b.rollbacks, previous)
	return nil
}

func f1XrayIdentity() XrayReleaseIdentity {
	return XrayReleaseIdentity{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: 123, SHA256: strings.Repeat("a", 64)}
}

func f1GeodataSet() GeodataCandidateSet {
	items := make([]GeodataReleaseIdentity, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		items[index] = GeodataReleaseIdentity{
			ID: entry.ID, Repository: entry.Repository, Tag: "2026-09-05", AssetName: entry.Asset,
			ActiveName: entry.Name, SizeBytes: int64(index + 1), SHA256: strings.Repeat(string(rune('a'+index)), 64),
		}
	}
	return GeodataCandidateSet{Items: items, Generation: geodataIdentityGeneration(items)}
}

func TestMutationRequestIsClosedAndRollbackHasNoChannel(t *testing.T) {
	valid := MutationRequest{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelStable}
	if err := ValidateMutationRequest(valid); err != nil {
		t.Fatal(err)
	}
	for _, request := range []MutationRequest{
		{Component: KindPanel, Operation: MutationOperationUpdate, Channel: MutationChannelStable},
		{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelDev},
		{Component: KindXKeen, Operation: MutationOperationUpdate, Channel: MutationChannelStable},
		{Component: KindXray, Operation: MutationOperationRollback, Channel: MutationChannelStable},
		{Component: KindXray, Operation: "install", Channel: MutationChannelStable},
	} {
		if !errors.Is(ValidateMutationRequest(request), ErrInvalidMutationRequest) {
			t.Fatalf("request unexpectedly accepted: %+v", request)
		}
	}
	if err := ValidateMutationRequest(MutationRequest{Component: KindXray, Operation: MutationOperationRollback}); err != nil {
		t.Fatal(err)
	}
	var decoded MutationRequest
	if err := json.Unmarshal([]byte(`{"component":"xray","operation":"rollback"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMutationRequest(decoded); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"component":"xray","operation":"rollback","channel":""}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ValidateMutationRequest(decoded), ErrInvalidMutationRequest) {
		t.Fatal("explicit rollback channel was accepted")
	}
	if err := json.Unmarshal([]byte(`{"component":"xray","operation":"update","channel":"stable","url":"https://evil.example"}`), &decoded); err == nil {
		t.Fatal("unknown mutation field was accepted")
	}
	for _, raw := range []string{
		`{"component":"xray","operation":"update","channel":"stable","component":"xray"}`,
		`{"Component":"xray","operation":"rollback"}`,
		`{"component":"xray","operation":"rollback","Channel":"stable"}`,
	} {
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
			t.Fatalf("non-canonical mutation fields were accepted: %s", raw)
		}
	}
}

func TestMutationServiceFreshPreviewTypedDispatchAndOneShot(t *testing.T) {
	resolver := &f1XrayResolver{identity: f1XrayIdentity()}
	backend := &f1XrayBackend{previous: XrayPreviousGeneration{Generation: strings.Repeat("b", 64), Version: "1.0.0", SizeBytes: 10, SHA256: strings.Repeat("b", 64), Mode: 0o755}}
	service := NewMutationService(MutationConfig{XrayResolver: resolver, Xray: backend})

	preview, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelStable})
	if err != nil {
		t.Fatal(err)
	}
	if preview.SchemaVersion != MutationSchemaVersion || preview.PreviewToken == "" || preview.Candidate == nil || preview.Candidate.Version != "1.2.3" || preview.Candidate.Generation != strings.Repeat("a", 64) {
		t.Fatalf("update preview = %+v", preview)
	}
	if strings.Contains(string(mustJSON(t, preview)), "https://") {
		t.Fatal("preview contains a URL")
	}
	if _, err := service.Apply(context.Background(), "session-b", preview.PreviewToken); !errors.Is(err, ErrMutationPreviewExpired) {
		t.Fatalf("cross-session apply error = %v", err)
	}
	result, err := service.Apply(context.Background(), "session-a", preview.PreviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "applied" || len(backend.applied) != 1 || backend.applied[0] != resolver.identity {
		t.Fatalf("apply result=%+v applied=%+v", result, backend.applied)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.PreviewToken); !errors.Is(err, ErrMutationPreviewExpired) {
		t.Fatalf("second apply error = %v", err)
	}

	rollbackPreview, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindXray, Operation: MutationOperationRollback})
	if err != nil {
		t.Fatal(err)
	}
	if rollbackPreview.Candidate != nil || rollbackPreview.Previous == nil || rollbackPreview.Previous.Generation != strings.Repeat("b", 64) {
		t.Fatalf("rollback preview = %+v", rollbackPreview)
	}
	if _, err := service.Apply(context.Background(), "session-a", rollbackPreview.PreviewToken); !errors.Is(err, ErrMutationOperationMismatch) {
		t.Fatalf("update route accepted rollback token: %v", err)
	}
	result, err = service.Rollback(context.Background(), "session-a", rollbackPreview.PreviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "rolled-back" || len(backend.rolledBack) != 1 || backend.rolledBack[0] != backend.previous {
		t.Fatalf("rollback result=%+v rollback=%+v", result, backend.rolledBack)
	}
}

func TestMutationServiceExpiryCancellationAndRetention(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	resolver := &f1XrayResolver{identity: f1XrayIdentity()}
	backend := &f1XrayBackend{}
	service := NewMutationService(MutationConfig{XrayResolver: resolver, Xray: backend, PreviewTTL: time.Minute, MaxPreviews: 2, Now: func() time.Time { return now }})
	request := MutationRequest{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelStable}
	p1, err := service.Preview(context.Background(), "session-a", request)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := service.Preview(context.Background(), "session-a", request)
	if err != nil {
		t.Fatal(err)
	}
	p3, err := service.Preview(context.Background(), "session-a", request)
	if err != nil {
		t.Fatal(err)
	}
	if len(service.previews) != 2 {
		t.Fatalf("retained previews = %d", len(service.previews))
	}
	if _, err := service.Apply(context.Background(), "session-a", p1.PreviewToken); !errors.Is(err, ErrMutationPreviewExpired) {
		t.Fatalf("oldest preview error = %v", err)
	}
	if _, err := service.Apply(context.Background(), "session-a", p2.PreviewToken); err != nil {
		t.Fatalf("retained preview apply = %v", err)
	}
	service.Cancel("session-a", p3.PreviewToken)
	if _, err := service.Apply(context.Background(), "session-a", p3.PreviewToken); !errors.Is(err, ErrMutationPreviewExpired) {
		t.Fatalf("cancelled preview error = %v", err)
	}

	p4, err := service.Preview(context.Background(), "session-a", request)
	if err != nil {
		t.Fatal(err)
	}
	p5, err := service.Preview(context.Background(), "session-b", request)
	if err != nil {
		t.Fatal(err)
	}
	service.Invalidate("session-a")
	if _, err := service.Apply(context.Background(), "session-a", p4.PreviewToken); !errors.Is(err, ErrMutationPreviewExpired) {
		t.Fatalf("session invalidation error = %v", err)
	}
	if _, err := service.Apply(context.Background(), "session-b", p5.PreviewToken); err != nil {
		t.Fatalf("other session was invalidated: %v", err)
	}

	p6, err := service.Preview(context.Background(), "session-b", request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := service.Apply(context.Background(), "session-b", p6.PreviewToken); !errors.Is(err, ErrMutationPreviewExpired) {
		t.Fatalf("expired preview error = %v", err)
	}
	service.InvalidateAll()
	if len(service.previews) != 0 {
		t.Fatalf("global invalidation retained %d previews", len(service.previews))
	}
}

func TestMutationServiceAllowsOnlyOneConcurrentPreview(t *testing.T) {
	release := make(chan struct{})
	resolver := &f1XrayResolver{identity: f1XrayIdentity(), started: make(chan struct{}, 1), release: release}
	service := NewMutationService(MutationConfig{XrayResolver: resolver, Xray: &f1XrayBackend{}})
	result := make(chan error, 1)
	go func() {
		_, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelStable})
		result <- err
	}()
	select {
	case <-resolver.started:
	case <-time.After(time.Second):
		t.Fatal("first preview did not start")
	}
	if _, err := service.Preview(context.Background(), "session-b", MutationRequest{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelStable}); !errors.Is(err, ErrMutationBusy) {
		t.Fatalf("concurrent preview error = %v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestMutationServiceDispatchesGeodataAndXKeenTypedIdentities(t *testing.T) {
	set := f1GeodataSet()
	geodataBackend := &f1GeodataBackend{previous: GeodataPreviousGeneration{Generation: "previous", Items: []GeodataPreviousItem{{ID: "geosite-refilter", Name: "geosite_refilter.dat", SizeBytes: 1, Mode: 0o600, SHA256: strings.Repeat("b", 64)}}}}
	entry, ok := reviewedXKeenEntry(xkeenCatalogBuildCommit, xkeenCatalogAsset)
	if !ok {
		t.Fatal("catalog entry unavailable")
	}
	xkeenBackend := &f1XKeenBackend{previous: XKeenPreviousGeneration{Generation: strings.Repeat("c", 64), Entries: 1, Bytes: 1}}
	service := NewMutationService(MutationConfig{
		GeodataResolver: f1GeodataResolver{set: set}, Geodata: geodataBackend,
		XKeenResolver: f1XKeenResolver{identity: xkeenReleaseIdentityFromEntry(entry)}, XKeen: xkeenBackend,
	})
	geodataPreview, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindGeodata, Operation: MutationOperationUpdate, Channel: MutationChannelStable})
	if err != nil || geodataPreview.Candidate == nil || len(geodataPreview.Candidate.Items) != len(productGeodataCatalog) {
		t.Fatalf("geodata preview=%+v err=%v", geodataPreview, err)
	}
	if _, err := service.Apply(context.Background(), "session-a", geodataPreview.PreviewToken); err != nil || len(geodataBackend.updates) != 1 || !sameGeodataCandidateSet(geodataBackend.updates[0], set) {
		t.Fatalf("geodata apply err=%v updates=%+v", err, geodataBackend.updates)
	}
	xkeenPreview, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindXKeen, Operation: MutationOperationUpdate, Channel: MutationChannelDev})
	if err != nil || xkeenPreview.Candidate == nil || xkeenPreview.Candidate.BuildCommitSHA != xkeenCatalogBuildCommit {
		t.Fatalf("xkeen preview=%+v err=%v", xkeenPreview, err)
	}
	if _, err := service.Apply(context.Background(), "session-a", xkeenPreview.PreviewToken); err != nil || len(xkeenBackend.updates) != 1 || xkeenBackend.updates[0].CommitSHA != xkeenCatalogBuildCommit {
		t.Fatalf("xkeen apply err=%v updates=%+v", err, xkeenBackend.updates)
	}
}

func TestXKeenMovingDevResolverUsesFreshMovingMetadataAndNoBlob(t *testing.T) {
	buildCommit := xkeenCatalogBuildCommit
	responses := map[string][]byte{
		xkeenDevCommitListPath:                                        xkeenDevCommitList(t, buildCommit),
		xkeenDevCommitPathPrefix + buildCommit:                        xkeenDevCommit(t, buildCommit, xkeenCatalogSourceParent, true, xkeenDevBuildCommitMessage, xkeenCatalogBlobSHA),
		xkeenDevTreePathPrefix + buildCommit + xkeenDevTreePathSuffix: xkeenDevTree(t, xkeenDevArtifactPath, "100644", "blob", xkeenCatalogBlobSHA, xkeenCatalogAssetSize),
	}
	transport := &metadataFixtureTransport{responses: responses}
	resolver := NewXKeenMovingDevResolver(nil, &http.Client{Transport: transport})
	identity, err := resolver.ResolveXKeen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity != xkeenReleaseIdentityFromEntry(mustCatalogEntry(t)) {
		t.Fatalf("moving identity = %+v", identity)
	}
	for _, call := range transportCalls(transport) {
		if call == xkeenBlobPathPrefix+xkeenCatalogBlobSHA {
			t.Fatal("moving resolver fetched artifact bytes")
		}
	}

	movedBuild := strings.Repeat("c", 40)
	movedResponses := map[string][]byte{
		xkeenDevCommitListPath:                                       xkeenDevCommitList(t, movedBuild),
		xkeenDevCommitPathPrefix + movedBuild:                        xkeenDevCommit(t, movedBuild, xkeenCatalogSourceParent, true, xkeenDevBuildCommitMessage, xkeenCatalogBlobSHA),
		xkeenDevTreePathPrefix + movedBuild + xkeenDevTreePathSuffix: xkeenDevTree(t, xkeenDevArtifactPath, "100644", "blob", xkeenCatalogBlobSHA, xkeenCatalogAssetSize),
	}
	moved := NewXKeenMovingDevResolver(nil, &http.Client{Transport: &metadataFixtureTransport{responses: movedResponses}})
	if _, err := moved.ResolveXKeen(context.Background()); !errors.Is(err, ErrXKeenCandidateRejected) {
		t.Fatalf("moved main error = %v", err)
	}
}

func TestCheckerReportsMutationAvailabilityOnlyThroughConfiguredBroker(t *testing.T) {
	transport := &metadataFixtureTransport{responses: map[string][]byte{
		xrayMetadataPath: releaseMetadata(t, "v1.2.3", testAsset(xrayCandidateAsset, 123, checkDigest('a'))),
	}}
	checker := NewChecker(CheckerConfig{
		HTTPClient: transportHTTPClient(transport),
		MutationAvailable: func(component ComponentKind, channel string) bool {
			return component == KindXray && channel == MutationChannelStable
		},
	})
	result, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: MutationChannelStable})
	if err != nil || !result.Eligible || !result.MutationAvailable {
		t.Fatalf("configured mutation availability result=%+v err=%v", result, err)
	}
}

func transportHTTPClient(transport *metadataFixtureTransport) *http.Client {
	return &http.Client{Transport: transport}
}

func mustCatalogEntry(t *testing.T) xkeenCompatibilityEntry {
	t.Helper()
	entry, ok := reviewedXKeenEntry(xkeenCatalogBuildCommit, xkeenCatalogAsset)
	if !ok {
		t.Fatal("catalog entry unavailable")
	}
	return entry
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

var _ XrayCandidateResolver = (*f1XrayResolver)(nil)
var _ XrayMutationBackend = (*f1XrayBackend)(nil)
var _ GeodataCandidateResolver = (f1GeodataResolver{})
var _ GeodataMutationBackend = (*f1GeodataBackend)(nil)
var _ XKeenCandidateResolver = (f1XKeenResolver{})
var _ XKeenMutationBackend = (*f1XKeenBackend)(nil)
