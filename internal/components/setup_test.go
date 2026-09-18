package components

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

type setupTestXrayResolver struct{ value XrayReleaseIdentity }

func (r setupTestXrayResolver) ResolveXray(context.Context) (XrayReleaseIdentity, error) {
	return r.value, nil
}

type setupTestGeodataResolver struct{ value GeodataCandidateSet }

func (r setupTestGeodataResolver) ResolveGeodata(context.Context) (GeodataCandidateSet, error) {
	return r.value, nil
}

type setupTestXKeenResolver struct{ value XKeenReleaseIdentity }

func (r setupTestXKeenResolver) ResolveXKeen(context.Context) (XKeenReleaseIdentity, error) {
	return r.value, nil
}

func setupTestPaths(root string) SetupPaths {
	return SetupPaths{
		XrayBinary:          filepath.Join(root, "xray"),
		XrayConfigDir:       filepath.Join(root, "configs"),
		XrayAssetDir:        filepath.Join(root, "assets"),
		XkeenBinary:         filepath.Join(root, "xkeen"),
		XkeenModuleDir:      filepath.Join(root, ".xkeen"),
		XkeenConfig:         filepath.Join(root, "xkeen.json"),
		XkeenMarker:         filepath.Join(root, "xkeen-generation.json"),
		LifecycleInit:       filepath.Join(root, "S05xkeen"),
		LegacyLifecycleInit: filepath.Join(root, "S24xray"),
		SiblingModule:       filepath.Join(root, "_xkeen"),
		InstallHelper:       filepath.Join(root, "install.sh"),
		Appliance:           filepath.Join(root, "appliance.json"),
		Nodes:               filepath.Join(root, "nodes.json"),
		ActiveOutbounds:     filepath.Join(root, "configs", "04_outbounds.json"),
		Journal:             filepath.Join(root, "state", "component-transaction.json"),
		RestoreJournal:      filepath.Join(root, "state", "restore.json"),
		StagingDir:          filepath.Join(root, "staging"),
	}
}

func setupTestGeodata() GeodataCandidateSet {
	items := make([]GeodataReleaseIdentity, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		items[index] = GeodataReleaseIdentity{
			ID: entry.ID, Repository: entry.Repository, Tag: "2026-09-05", AssetName: entry.Asset,
			ActiveName: entry.Name, SizeBytes: int64(index + 1), SHA256: strings.Repeat(string(rune('a'+index)), 64),
		}
	}
	return GeodataCandidateSet{Items: items, Generation: geodataIdentityGeneration(items)}
}

func setupTestXKeen() XKeenReleaseIdentity {
	entry, ok := reviewedXKeenEntry(xkeenCatalogBuildCommit, xkeenCatalogAsset)
	if !ok {
		panic("test XKeen catalog entry is missing")
	}
	return XKeenReleaseIdentity{
		Repository: entry.Repository, Channel: entry.Channel, Tag: entry.Tag, Version: entry.Version,
		CommitSHA: entry.CommitSHA, SourceParentSHA: entry.SourceParentSHA, AssetName: entry.AssetName,
		BlobSHA: entry.BlobSHA, SizeBytes: entry.SizeBytes, SHA256: entry.SHA256, GenerationSHA256: entry.GenerationSHA256,
	}
}

type setupTestCandidateValidator struct {
	calls    int
	assetDir string
}

func (v *setupTestCandidateValidator) ValidateXrayCandidate(_ context.Context, binary, configDir, assetDir string) error {
	v.calls++
	if binary == "" || configDir == "" || assetDir == "" {
		return errors.New("setup candidate paths are missing")
	}
	v.assetDir = assetDir
	for _, entry := range productGeodataCatalog {
		info, err := os.Stat(filepath.Join(assetDir, entry.Name))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("staged geodata is incomplete")
		}
	}
	routing, err := os.ReadFile(filepath.Join(configDir, "05_routing.json"))
	if err != nil || !bytes.Contains(routing, []byte("proxy-")) {
		return errors.New("staged ProductDefault routing is missing")
	}
	return nil
}

type setupTestRuntime struct {
	startCalls  int
	readyCalls  int
	probeCalls  int
	configCalls int
	emptyCalls  int
}

func (r *setupTestRuntime) Start(context.Context) error {
	r.startCalls++
	return nil
}
func (r *setupTestRuntime) WaitReady(context.Context) error {
	r.readyCalls++
	return nil
}
func (r *setupTestRuntime) ProbeReachable(context.Context) bool {
	r.probeCalls++
	return true
}
func (r *setupTestRuntime) ValidateActiveConfig(context.Context) error {
	r.configCalls++
	return nil
}
func (r *setupTestRuntime) VerifyEmpty(context.Context) error {
	r.emptyCalls++
	return nil
}

func setupTestService(t *testing.T, paths SetupPaths) *SetupService {
	t.Helper()
	return NewSetupService(SetupConfig{
		Paths:           paths,
		XrayResolver:    setupTestXrayResolver{value: f1XrayIdentity()},
		GeodataResolver: setupTestGeodataResolver{value: setupTestGeodata()},
		XKeenResolver:   setupTestXKeenResolver{value: setupTestXKeen()},
	})
}

func TestSetupClassifierIsFreshOnlyAndFailClosed(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	service := setupTestService(t, paths)
	if projection := service.Status(); projection.State != "fresh" || !projection.Eligible || projection.ReasonCode != SetupReasonFresh {
		t.Fatalf("fresh projection = %+v", projection)
	}

	if err := os.WriteFile(paths.XrayBinary, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonLayoutPartial || projection.Eligible {
		t.Fatalf("partial projection = %+v", projection)
	}
	if err := os.Remove(paths.XrayBinary); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.LegacyLifecycleInit, []byte("manual"), 0o700); err != nil {
		t.Fatal(err)
	}
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonLayoutMixed {
		t.Fatalf("manual lifecycle projection = %+v", projection)
	}
	if err := os.Remove(paths.LegacyLifecycleInit); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.Appliance, []byte("authority"), 0o600); err != nil {
		t.Fatal(err)
	}
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonAuthorityPresent {
		t.Fatalf("authority projection = %+v", projection)
	}
	if err := os.Remove(paths.Appliance); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(paths.StagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StagingDir, ".setup-synthetic"), []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if layout, err := service.inspectLayout(); err != nil || layout.state != "blocked" || layout.reason != SetupReasonJournalPending {
		t.Fatalf("staging layout = %+v, %v", layout, err)
	}
}

func TestSetupPreviewPinsServerOwnedPlanAndTokenIsOneShot(t *testing.T) {
	service := setupTestService(t, setupTestPaths(t.TempDir()))
	preview, err := service.Preview(context.Background(), "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Operation != SetupOperation || preview.Plan.SchemaVersion != SetupTransactionSchemaVersion || !preview.Plan.ProductDefault || !preview.Plan.EmptyRegistry {
		t.Fatalf("preview = %+v", preview)
	}
	if len(preview.Plan.Geodata.Items) != len(productGeodataCatalog) || preview.Plan.Lifecycle.Name != "S05xkeen" {
		t.Fatalf("preview plan omitted fixed setup components: %+v", preview.Plan)
	}
	if projection := service.Status(); projection.State != "previewable" || !projection.Eligible {
		t.Fatalf("preview projection = %+v", projection)
	}
	if _, err := service.takePreview("session-b", preview.PreviewToken); !errors.Is(err, ErrSetupPreviewExpired) {
		t.Fatalf("cross-session token error = %v", err)
	}
	if _, err := service.takePreview("session-a", preview.PreviewToken); err != nil {
		t.Fatalf("take preview = %v", err)
	}
	if _, err := service.takePreview("session-a", preview.PreviewToken); !errors.Is(err, ErrSetupPreviewExpired) {
		t.Fatalf("replayed token error = %v", err)
	}
	if projection := service.Status(); projection.State != "fresh" {
		t.Fatalf("post-consumption projection = %+v", projection)
	}
}

func TestSetupRecoveryRemovesOnlyOrphanedPrivateStaging(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	if err := os.MkdirAll(paths.StagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StagingDir, ".setup-orphan"), []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := setupTestService(t, paths)
	if err := service.RecoverStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.StagingDir, ".setup-orphan")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan staging remains: %v", err)
	}
	if err := service.Ready(); err != nil {
		t.Fatalf("service remained unavailable: %v", err)
	}
}

func TestSetupApplyCommitsOneCombinedSyntheticFreshGeneration(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)

	xrayArchive := writeSyntheticArchive(t, []syntheticZipEntry{{name: "xray", mode: 0o700, contents: []byte("new-xray-binary")}})
	xrayArchiveDigest := sha256.Sum256(xrayArchive)
	xrayIdentity := XrayReleaseIdentity{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: int64(len(xrayArchive)), SHA256: hex.EncodeToString(xrayArchiveDigest[:])}

	geodataItems := make([]GeodataReleaseIdentity, len(productGeodataCatalog))
	geodataPayloads := make(map[string][]byte, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		payload := bytes.Repeat([]byte{byte('a' + index)}, index+1)
		digest := sha256.Sum256(payload)
		geodataPayloads[entry.Name] = payload
		geodataItems[index] = GeodataReleaseIdentity{ID: entry.ID, Repository: entry.Repository, Tag: "2026-09-05", AssetName: entry.Asset, ActiveName: entry.Name, SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:])}
	}
	geodataSet := GeodataCandidateSet{Items: geodataItems, Generation: geodataIdentityGeneration(geodataItems)}

	xkeenArchivePath := writeTestGzipTar(t, []testTarEntry{
		{name: "_xkeen/runtime.sh", kind: tar.TypeReg, mode: 0o644, format: tar.FormatGNU, contents: []byte("candidate module")},
		{name: "xkeen", kind: tar.TypeReg, mode: 0o755, format: tar.FormatGNU, contents: []byte("candidate xkeen")},
	})
	xkeenArchive, err := os.ReadFile(xkeenArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	xkeenEntry, xkeenIdentity := installableCatalogFixture(t, xkeenArchive)
	xkeenEntry.ArchiveMembers = []XKeenArchiveMember{
		{Name: "_xkeen/runtime.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: int64(len("candidate module"))},
		{Name: "xkeen", Type: xkeenArchiveRegular, Mode: 0o755, Size: int64(len("candidate xkeen"))},
	}
	probePath := filepath.Join(t.TempDir(), "xkeen-candidate")
	xkeenMeta, err := extractXKeenArchiveMembers(context.Background(), xkeenArchivePath, probePath, xkeenEntry.ArchiveMembers)
	if err != nil {
		t.Fatal(err)
	}
	xkeenEntry.GenerationSHA256 = xkeenMeta.GenerationSHA256()
	xkeenIdentity.GenerationSHA256 = xkeenEntry.GenerationSHA256
	xkeenIdentity.Generation = xkeenEntry.GenerationSHA256
	reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)] = xkeenEntry

	validator := &setupTestCandidateValidator{}
	runtime := &setupTestRuntime{}
	coordinator := &fakeXrayCoordinator{}
	service := NewSetupService(SetupConfig{
		Paths:        paths,
		XrayResolver: setupTestXrayResolver{value: xrayIdentity}, XrayDownloader: &fakeXrayDownloader{archive: xrayArchive},
		GeodataResolver: setupTestGeodataResolver{value: geodataSet}, GeodataDownloader: &fakeGeodataDownloader{payloads: geodataPayloads},
		XKeenResolver: setupTestXKeenResolver{value: xkeenIdentity}, XKeenDownloader: &fakeXKeenDownloader{archive: xkeenArchive},
		CandidateProbe: &fakeTransactionalProbe{newVersion: xrayIdentity.Version}, CandidateValidator: validator, Runtime: runtime,
		MutationGate: NewComponentMutationGate(), Coordinator: coordinator, AuthorityLease: authority.NewLease(),
		AvailableSpace: func(string) (uint64, error) { return ^uint64(0), nil }, SyncDirectory: func(string) error { return nil },
	})

	preview, err := service.Preview(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.PreviewToken); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if runtime.startCalls != 1 || runtime.readyCalls != 1 || runtime.probeCalls != 1 || runtime.configCalls != 1 || runtime.emptyCalls != 1 {
		t.Fatalf("runtime proof calls = %+v", runtime)
	}
	if validator.calls != 1 || validator.assetDir == "" {
		t.Fatalf("combined candidate validation = %+v", validator)
	}
	if projection := service.Status(); projection.State != "ready" || projection.Eligible {
		t.Fatalf("post-apply setup projection = %+v", projection)
	}
	if _, err := os.Stat(paths.Journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup journal remains: %v", err)
	}
	if _, err := os.Stat(paths.Appliance); err != nil {
		t.Fatalf("appliance authority missing: %v", err)
	}
	if _, err := os.Stat(paths.Nodes); err != nil {
		t.Fatalf("empty node authority missing: %v", err)
	}
	if _, err := os.Stat(paths.LifecycleInit); err != nil {
		t.Fatalf("fixed lifecycle missing: %v", err)
	}
}
