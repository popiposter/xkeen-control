package components

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/nodes"
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
	startCalls   int
	readyCalls   int
	probeCalls   int
	configCalls  int
	emptyCalls   int
	stopCalls    int
	stoppedCalls int
	verifyCalls  int
	stopped      bool
	verifyFails  int
	emptyFails   int
}

type setupTestSelectionTransaction struct {
	snapshot            []byte
	reconciled          bool
	restoreCalls        int
	runtime             *setupTestRuntime
	restoreWhileStopped bool
	failureJournal      bool
	journalSyncs        int
	failureInjected     bool
}

func (s *setupTestSelectionTransaction) ReconcileSetupSelection(context.Context, []string) error {
	s.reconciled = true
	s.snapshot = []byte("new-selection")
	return nil
}

func (s *setupTestSelectionTransaction) SetupSelectionSnapshot(context.Context) ([]byte, error) {
	return append([]byte(nil), s.snapshot...), nil
}

func (s *setupTestSelectionTransaction) RestoreSetupSelection(_ context.Context, snapshot []byte) error {
	s.restoreCalls++
	if s.runtime != nil && s.runtime.stopped {
		s.restoreWhileStopped = true
		return errors.New("selection runtime is stopped")
	}
	s.snapshot = append([]byte(nil), snapshot...)
	s.reconciled = false
	return nil
}

func (r *setupTestRuntime) Start(context.Context) error {
	r.startCalls++
	r.stopped = false
	return nil
}
func (r *setupTestRuntime) WaitReady(context.Context) error {
	r.readyCalls++
	return nil
}
func (r *setupTestRuntime) ProbeReachable(context.Context) bool {
	r.probeCalls++
	return !r.stopped
}
func (r *setupTestRuntime) ValidateActiveConfig(context.Context) error {
	r.configCalls++
	return nil
}
func (r *setupTestRuntime) VerifyEmpty(context.Context) error {
	r.emptyCalls++
	if r.emptyFails > 0 {
		r.emptyFails--
		return errors.New("synthetic late empty verification failure")
	}
	return nil
}
func (r *setupTestRuntime) Stop(context.Context) error {
	r.stopCalls++
	r.stopped = true
	return nil
}
func (r *setupTestRuntime) VerifyStopped(context.Context) error {
	r.stoppedCalls++
	return nil
}
func (r *setupTestRuntime) Verify(context.Context, []string) error {
	r.verifyCalls++
	if r.verifyFails > 0 {
		r.verifyFails--
		return errors.New("synthetic late verification failure")
	}
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

func setupTestLegacyOutbounds(t *testing.T) ([]byte, nodes.Registry) {
	t.Helper()
	profileText := strings.Join([]string{
		"vless:", "//11111111-1111-4111-8111-111111111111@edge.example.com:443?",
		"encryption=none&security=reality&sni=front.example.com&fp=chrome&",
		"pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&type=tcp#Existing",
	}, "")
	profile, err := nodes.ParseProfile(profileText)
	if err != nil {
		t.Fatal(err)
	}
	node, err := nodes.NewNode(profile.VLESS, profile.Name, nodes.Source{Type: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	registry := nodes.NewRegistry()
	registry.Nodes = []nodes.Node{node}
	contents, err := nodes.Render(registry)
	if err != nil {
		t.Fatal(err)
	}
	return contents, registry
}

func TestSetupTakeoverUsesTypedAuthorityPrecedenceAndStrictLegacyMigration(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	paths.LegacyOutbounds = filepath.Join(root, "legacy-outbounds.json")
	legacy, legacyRegistry := setupTestLegacyOutbounds(t)
	if err := os.WriteFile(paths.LegacyOutbounds, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalNodes, err := nodes.MarshalCanonical(legacyRegistry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Nodes, canonicalNodes, 0o600); err != nil {
		t.Fatal(err)
	}
	service := setupTestService(t, paths)
	preview, err := service.Preview(context.Background(), "takeover")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.SetupClass != "managed-takeover" || preview.Plan.Profiles.Action != "preserve" || preview.Plan.Profiles.Count != 1 {
		t.Fatalf("valid nodes did not win precedence: %+v", preview.Plan)
	}
	publicPreview, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"11111111-1111-4111-8111-111111111111", "front.example.com", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "vless:" + "//"} {
		if bytes.Contains(publicPreview, []byte(secret)) {
			t.Fatalf("preview leaked profile secret %q: %s", secret, publicPreview)
		}
	}
	if got, err := os.ReadFile(paths.Nodes); err != nil || !bytes.Equal(got, canonicalNodes) {
		t.Fatalf("nodes authority changed during Preview: %v", err)
	}

	if err := os.WriteFile(paths.Nodes, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.InvalidateAll()
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonNodeAuthorityInvalid {
		t.Fatalf("invalid nodes did not block: %+v", projection)
	}
	if err := os.Remove(paths.Nodes); err != nil {
		t.Fatal(err)
	}
	if preview, err := service.Preview(context.Background(), "legacy"); err != nil {
		t.Fatal(err)
	} else if preview.Plan.SetupClass != "legacy-xkeen-takeover" || preview.Plan.Profiles.Action != "migrate" || preview.Plan.Profiles.Count != 1 {
		t.Fatalf("legacy plan = %+v", preview.Plan)
	}

	if err := os.WriteFile(paths.LegacyOutbounds, []byte(`{"outbounds":[{"tag":"unexpected","protocol":"freedom"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.InvalidateAll()
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonProfileUnavailable {
		t.Fatalf("unsupported legacy profile did not block: %+v", projection)
	}
}

func TestSetupLifecycleDoesNotSignalAReusedNonXrayPID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc executable identity fixture requires the Linux qualification environment")
	}
	root := t.TempDir()
	pidFile := filepath.Join(root, "xray.pid")
	logFile := filepath.Join(root, "xray.log")
	process := exec.Command("sleep", "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(process.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents := strings.ReplaceAll(setupLifecycleTemplate, "\r\n", "\n")
	contents = strings.ReplaceAll(contents, "pidfile=/tmp/xkeen-control/xray.pid", "pidfile="+pidFile)
	contents = strings.ReplaceAll(contents, "logfile=/tmp/xkeen-control/xray.log", "logfile="+logFile)
	contents = strings.ReplaceAll(contents, "/opt/sbin/xray", filepath.Join(root, "xray"))
	script := filepath.Join(root, "S05xkeen")
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(script, "stop", "on").Run(); err != nil {
		t.Fatalf("stale pidfile stop failed: %v", err)
	}
	if err := process.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("stale pidfile signalled a non-Xray process: %v", err)
	}
}

func TestSetupTakeoverAdoptsSupportedLegacyPolicyBeforeProductDefault(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	paths.LegacyOutbounds = filepath.Join(root, "legacy-outbounds.json")
	legacy, _ := setupTestLegacyOutbounds(t)
	if err := os.WriteFile(paths.LegacyOutbounds, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := nodes.MigrateLegacy(legacy)
	if err != nil {
		t.Fatal(err)
	}
	files, err := appliance.RenderCandidateFiles(appliance.ProductDefault(), migrated)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"02_dns.json", "05_routing.json", "07_observatory.json"} {
		if err := os.MkdirAll(paths.XrayConfigDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.XrayConfigDir, name), files["xray/"+name], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := setupTestService(t, paths)
	preview, err := service.Preview(context.Background(), "legacy-policy")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.Policy.Action != "adopt-supported" || preview.Plan.ProductDefault || preview.Plan.Profiles.Action != "migrate" {
		t.Fatalf("supported legacy policy plan = %+v", preview.Plan)
	}
	if _, err := os.Stat(paths.Appliance); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Preview wrote appliance authority: %v", err)
	}
}

func TestSetupTakeoverResourceAdmissionChecksPersistentSpaceBeforeWrites(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	if estimateSetupSnapshotBytes(paths) <= int64(8<<20) {
		t.Fatalf("snapshot admission reverted to the superseded small placeholder: %d", estimateSetupSnapshotBytes(paths))
	}
	paths.PreviousDir = filepath.Join(root, "persistent")
	if err := os.MkdirAll(paths.PreviousDir, 0o700); err != nil {
		t.Fatal(err)
	}
	service := NewSetupService(SetupConfig{
		Paths: paths,
		AvailableSpace: func(path string) (uint64, error) {
			if strings.Contains(path, "persistent") {
				return 0, nil
			}
			return ^uint64(0), nil
		},
		SameFilesystem: func(left, right string) (bool, error) {
			leftPersistent := strings.Contains(left, "persistent")
			rightPersistent := strings.Contains(right, "persistent")
			return leftPersistent == rightPersistent, nil
		},
	})
	demand := setupResourceDemand(paths, "managed-takeover", 40, 30, 20, 10, 100, 120, 80, 32, 64)
	if err := service.checkSetupResources(demand); !errors.Is(err, ErrSetupResourceInsufficient) {
		t.Fatalf("persistent space error = %v", err)
	}
	for _, path := range []string{paths.Journal, paths.Appliance, paths.Nodes, paths.XrayConfigDir, paths.StagingDir} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("persistent write occurred at %s: %v", path, statErr)
		}
	}
}

func TestSetupResourceAdmissionUsesPerFilesystemDemand(t *testing.T) {
	staging := t.TempDir()
	persistent := t.TempDir()
	paths := setupTestPaths(persistent)
	paths.StagingDir = staging
	paths.PreviousDir = filepath.Join(persistent, "previous")
	if err := os.MkdirAll(paths.PreviousDir, 0o700); err != nil {
		t.Fatal(err)
	}
	isStaging := func(path string) bool {
		path = filepath.Clean(path)
		return path == filepath.Clean(staging) || strings.HasPrefix(path, filepath.Clean(staging)+string(filepath.Separator))
	}
	demand := setupResourceDemand(paths, "managed-takeover", 40, 30, 20, 10, 100, 120, 80, 32, 64)
	config := SetupConfig{
		Paths:          paths,
		SameFilesystem: func(left, right string) (bool, error) { return isStaging(left) == isStaging(right), nil },
	}
	service := NewSetupService(config)
	groups, err := service.groupSetupResources(demand)
	if err != nil || len(groups) != 2 {
		t.Fatalf("filesystem demand groups = %+v err=%v", groups, err)
	}
	var stagingNeed, persistentNeed uint64
	for _, group := range groups {
		if isStaging(group.directory) {
			stagingNeed = group.bytes
		} else {
			persistentNeed = group.bytes
		}
	}
	if stagingNeed == 0 || persistentNeed == 0 || stagingNeed+persistentNeed <= stagingNeed || stagingNeed+persistentNeed <= persistentNeed {
		t.Fatalf("filesystem demands were not separated: staging=%d persistent=%d", stagingNeed, persistentNeed)
	}
	service.config.AvailableSpace = func(path string) (uint64, error) {
		if isStaging(path) {
			return stagingNeed + uint64(XrayFreeSpaceReserve) + 1, nil
		}
		return persistentNeed + uint64(XrayFreeSpaceReserve) + 1, nil
	}
	if err := service.checkSetupResources(demand); err != nil {
		t.Fatalf("split filesystem budget was rejected: %v", err)
	}
}

func TestSetupFreshResourceAdmissionDoesNotReservePreviousSnapshot(t *testing.T) {
	staging := t.TempDir()
	persistent := t.TempDir()
	paths := setupTestPaths(persistent)
	paths.StagingDir = staging
	paths.PreviousDir = filepath.Join(persistent, "previous")
	isStaging := func(path string) bool {
		path = filepath.Clean(path)
		return path == filepath.Clean(staging) || strings.HasPrefix(path, filepath.Clean(staging)+string(filepath.Separator))
	}
	fresh := setupResourceDemand(paths, "fresh", 40, 30, 20, 10, 100, 120, 80, 32, 64)
	takeover := setupResourceDemand(paths, "managed-takeover", 40, 30, 20, 10, 100, 120, 80, 32, 64)
	service := NewSetupService(SetupConfig{
		Paths: paths,
		SameFilesystem: func(left, right string) (bool, error) {
			return isStaging(left) == isStaging(right), nil
		},
	})
	freshGroups, err := service.groupSetupResources(fresh)
	if err != nil {
		t.Fatal(err)
	}
	takeoverGroups, err := service.groupSetupResources(takeover)
	if err != nil {
		t.Fatal(err)
	}
	var freshPersistent, takeoverPersistent uint64
	for _, group := range freshGroups {
		if !isStaging(group.directory) {
			freshPersistent = group.bytes
		}
	}
	for _, group := range takeoverGroups {
		if !isStaging(group.directory) {
			takeoverPersistent = group.bytes
		}
	}
	if freshPersistent == 0 || takeoverPersistent <= freshPersistent {
		t.Fatalf("fresh demand retained takeover snapshot reserve: fresh=%d takeover=%d", freshPersistent, takeoverPersistent)
	}
	service.config.AvailableSpace = func(path string) (uint64, error) {
		if isStaging(path) {
			for _, group := range freshGroups {
				if isStaging(group.directory) {
					return group.bytes + uint64(XrayFreeSpaceReserve) + 1, nil
				}
			}
		}
		return freshPersistent + uint64(XrayFreeSpaceReserve) + 1, nil
	}
	if err := service.checkSetupResources(fresh); err != nil {
		t.Fatalf("fresh Setup was rejected without snapshot reserve: %v", err)
	}
	if err := service.checkSetupResources(takeover); !errors.Is(err, ErrSetupResourceInsufficient) {
		t.Fatalf("takeover unexpectedly fit the fresh-only budget: %v", err)
	}
}

func TestSetupReviewedLifecycleSizeBoundAdmitsCurrentUpstreamS05(t *testing.T) {
	const currentUpstreamS05Bytes = 154485
	if int64(currentUpstreamS05Bytes) > setupMaxLifecycleBytes {
		t.Fatalf("reviewed current upstream S05 exceeds lifecycle bound: size=%d bound=%d", currentUpstreamS05Bytes, setupMaxLifecycleBytes)
	}
	root := t.TempDir()
	paths := setupTestPaths(root)
	contents := bytes.Repeat([]byte{'s'}, currentUpstreamS05Bytes)
	if err := os.WriteFile(paths.LifecycleInit, contents, 0o755); err != nil {
		t.Fatal(err)
	}
	service := setupTestService(t, paths)
	if _, err := service.setupSourceGenerationDigest(nil); err != nil {
		t.Fatalf("source generation rejected reviewed-size lifecycle: %v", err)
	}
	snapshot, err := service.captureSetupSnapshot(context.Background(), "managed-takeover", nil)
	if err != nil {
		t.Fatalf("snapshot rejected reviewed-size lifecycle: %v", err)
	}
	var lifecycle setupSnapshotEntry
	for _, entry := range snapshot.Manifest.Entries {
		if entry.Key == "lifecycle" {
			lifecycle = entry
			break
		}
	}
	if lifecycle.Size != currentUpstreamS05Bytes {
		t.Fatalf("snapshot lifecycle size=%d want=%d", lifecycle.Size, currentUpstreamS05Bytes)
	}
	if err := os.WriteFile(paths.LifecycleInit, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.restoreSetupSnapshot(snapshot); err != nil {
		t.Fatalf("restore rejected reviewed-size lifecycle: %v", err)
	}
	got, err := os.ReadFile(paths.LifecycleInit)
	if err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("restored lifecycle size/content mismatch: size=%d err=%v", len(got), err)
	}
	if err := service.removeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestSetupClassifierSupportsFreshAndRecognizedTakeoverButFailsClosed(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	paths.PanelPaths = []string{filepath.Join(root, "panel-state.json")}
	if err := os.WriteFile(paths.PanelPaths[0], []byte("panel-local-state"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonPolicyUnsupported {
		t.Fatalf("authority projection = %+v", projection)
	}
	if err := os.Remove(paths.Appliance); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(paths.StagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(paths.StagingDir, ".setup-synthetic")
	if err := ensureXKeenOwnedDirectory(staging, setupStagingOwner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "candidate"), []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if layout, err := service.inspectLayout(); err != nil || layout.state != "blocked" || layout.reason != SetupReasonJournalPending {
		t.Fatalf("staging layout = %+v, %v", layout, err)
	}
}

func TestSetupRejectsMagicWordManualLifecycleWithoutExecutingIt(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	marker := filepath.Join(root, "executed")
	manual := []byte("#!/bin/sh\n# xray start restart\nprintf executed > " + marker + "\n")
	if err := os.WriteFile(paths.LegacyLifecycleInit, manual, 0o700); err != nil {
		t.Fatal(err)
	}
	service := setupTestService(t, paths)
	projection := service.Status()
	if projection.State != "blocked" || projection.ReasonCode != SetupReasonLayoutMixed {
		t.Fatalf("magic-word lifecycle was accepted: %+v", projection)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manual lifecycle executed during classification: %v", err)
	}
}

func TestSetupRecoveryRemovesOwnedOrphanedPreviousSnapshot(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	service := setupTestService(t, paths)
	snapshotRoot := service.setupSnapshotRoot()
	if err := ensureXKeenOwnedDirectory(snapshotRoot, setupSnapshotOwner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotRoot, "payload"), []byte("bounded partial snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	pending, err := service.HasPendingRecovery()
	if err != nil || !pending {
		t.Fatalf("orphan snapshot was not admitted to recovery: pending=%v err=%v", pending, err)
	}
	if err := service.RecoverStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshotRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned orphaned snapshot remains: %v", err)
	}
	if err := service.Ready(); err != nil {
		t.Fatalf("service remained in recovery maintenance: %v", err)
	}
}

func TestSetupSourceManifestBindsEachTakeoverOwnedGeneration(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	paths.WriterScripts = []string{filepath.Join(root, "known-writer.sh")}
	paths.LegacyOutbounds = filepath.Join(root, "legacy-outbounds.json")
	service := setupTestService(t, paths)
	mutations := map[string]func() error{
		"xray": func() error { return os.WriteFile(paths.XrayBinary, []byte("xray-generation"), 0o700) },
		"xray-config": func() error {
			if err := os.MkdirAll(paths.XrayConfigDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(paths.XrayConfigDir, "01_log.json"), []byte("config-generation"), 0o600)
		},
		"xray-assets": func() error {
			if err := os.MkdirAll(paths.XrayAssetDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(paths.XrayAssetDir, productGeodataCatalog[0].Name), []byte("asset-generation"), 0o600)
		},
		"xkeen": func() error { return os.WriteFile(paths.XkeenBinary, []byte("xkeen-generation"), 0o700) },
		"xkeen-module": func() error {
			if err := os.MkdirAll(paths.XkeenModuleDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(paths.XkeenModuleDir, "runtime.sh"), []byte("module-generation"), 0o600)
		},
		"xkeen-marker": func() error { return os.WriteFile(paths.XkeenMarker, []byte("marker-generation"), 0o600) },
		"lifecycle": func() error {
			contents, err := setupLifecycleBytes()
			if err != nil {
				return err
			}
			return os.WriteFile(paths.LifecycleInit, append(contents, '\n'), 0o700)
		},
		"legacy-lifecycle": func() error {
			contents, err := setupLifecycleBytes()
			if err != nil {
				return err
			}
			return os.WriteFile(paths.LegacyLifecycleInit, contents, 0o700)
		},
		"sibling-module": func() error {
			if err := os.MkdirAll(paths.SiblingModule, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(paths.SiblingModule, "legacy.sh"), []byte("sibling-generation"), 0o600)
		},
		"install-helper": func() error { return os.WriteFile(paths.InstallHelper, []byte("helper-generation"), 0o700) },
		"appliance":      func() error { return os.WriteFile(paths.Appliance, []byte("appliance-generation"), 0o600) },
		"nodes":          func() error { return os.WriteFile(paths.Nodes, []byte("nodes-generation"), 0o600) },
		"legacy-outbounds": func() error {
			return os.WriteFile(paths.LegacyOutbounds, []byte("legacy-outbounds-generation"), 0o600)
		},
	}
	for name, mutate := range mutations {
		before, err := service.setupSourceGenerationDigest(nil)
		if err != nil {
			t.Fatalf("baseline %s: %v", name, err)
		}
		if err := mutate(); err != nil {
			t.Fatalf("mutate %s: %v", name, err)
		}
		after, err := service.setupSourceGenerationDigest(nil)
		if err != nil {
			t.Fatalf("changed %s: %v", name, err)
		}
		if before == after {
			t.Fatalf("source manifest ignored takeover-owned %s", name)
		}
	}
	beforeWriters, err := service.inspectSetupWriters()
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.setupSourceGenerationDigest(beforeWriters)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.WriterScripts[0], []byte("writer-generation"), 0o700); err != nil {
		t.Fatal(err)
	}
	afterWriters, err := service.inspectSetupWriters()
	if err != nil {
		t.Fatal(err)
	}
	after, err := service.setupSourceGenerationDigest(afterWriters)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("source manifest ignored a takeover-owned writer generation")
	}
}

func TestSetupBlocksPanelAutomaticWriterWithoutTouchingPanelState(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	paths.PanelPaths = []string{filepath.Join(root, "panel-state.json")}
	contents := []byte(`{"command":"` + "xkeen" + ` ` + "-i" + `"}`)
	if err := os.WriteFile(paths.PanelPaths[0], contents, 0o600); err != nil {
		t.Fatal(err)
	}
	service := setupTestService(t, paths)
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonWriterConflict {
		t.Fatalf("panel writer projection = %+v", projection)
	}
	if got, err := os.ReadFile(paths.PanelPaths[0]); err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("panel writer state changed: %v", err)
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
	staging := filepath.Join(paths.StagingDir, ".setup-orphan")
	if err := ensureXKeenOwnedDirectory(staging, setupStagingOwner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "candidate"), []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := setupTestService(t, paths)
	if err := service.RecoverStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
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
	selection := &setupTestSelectionTransaction{runtime: runtime}
	snapshotSyncObserved := false
	coordinator := &fakeXrayCoordinator{}
	service := NewSetupService(SetupConfig{
		Paths:        paths,
		XrayResolver: setupTestXrayResolver{value: xrayIdentity}, XrayDownloader: &fakeXrayDownloader{archive: xrayArchive},
		GeodataResolver: setupTestGeodataResolver{value: geodataSet}, GeodataDownloader: &fakeGeodataDownloader{payloads: geodataPayloads},
		XKeenResolver: setupTestXKeenResolver{value: xkeenIdentity}, XKeenDownloader: &fakeXKeenDownloader{archive: xkeenArchive},
		CandidateProbe: &fakeTransactionalProbe{newVersion: xrayIdentity.Version}, CandidateValidator: validator, Runtime: runtime, Selection: selection,
		MutationGate: NewComponentMutationGate(), Coordinator: coordinator, AuthorityLease: authority.NewLease(),
		AvailableSpace: func(string) (uint64, error) { return ^uint64(0), nil }, SyncDirectory: func(path string) error {
			if selection.failureJournal && selection.reconciled && filepath.Base(filepath.Clean(path)) == "state" && !selection.failureInjected {
				selection.journalSyncs++
				if selection.journalSyncs == 2 {
					selection.failureInjected = true
					return errors.New("synthetic journal failure after selection reconciliation")
				}
			}
			if filepath.Base(filepath.Clean(path)) == ".setup-snapshot" {
				snapshotSyncObserved = true
				if _, err := os.Stat(paths.Journal); err != nil {
					t.Fatalf("snapshot was synced before the transaction journal: %v", err)
				}
			}
			return nil
		},
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
	oldXray, err := os.ReadFile(paths.XrayBinary)
	if err != nil {
		t.Fatal(err)
	}
	oldNodes, err := os.ReadFile(paths.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	oldLifecycle, err := os.ReadFile(paths.LifecycleInit)
	if err != nil {
		t.Fatal(err)
	}
	previousMarker := []byte("{}\n")
	if err := os.WriteFile(paths.XkeenMarker, previousMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyLifecycle, _ := setupLifecycleBytes()
	if err := os.WriteFile(paths.LegacyLifecycleInit, legacyLifecycle, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime.emptyFails = 1
	selection.snapshot = []byte("old-selection")
	selection.reconciled = false
	takeover, err := service.Preview(context.Background(), "session-b")
	if err != nil {
		t.Fatalf("takeover preview: %v", err)
	}
	if takeover.Plan.SetupClass != "managed-takeover" || takeover.Plan.Profiles.Action != "preserve" || takeover.Plan.Policy.Action != "preserve" {
		t.Fatalf("takeover rollback plan = %+v", takeover.Plan)
	}
	if _, err := service.Apply(context.Background(), "session-b", takeover.PreviewToken); !errors.Is(err, ErrSetupTransactionRestored) {
		t.Fatalf("late verification error = %v", err)
	}
	if !snapshotSyncObserved {
		t.Fatal("takeover did not durably sync its previous-generation snapshot")
	}
	for path, expected := range map[string][]byte{paths.XkeenMarker: previousMarker, paths.XrayBinary: oldXray, paths.Nodes: oldNodes, paths.LifecycleInit: oldLifecycle} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("rollback changed %s: actual=%q expected=%q err=%v", path, actual, expected, readErr)
		}
	}
	if runtime.stopCalls != 2 || runtime.stoppedCalls != 2 || runtime.startCalls != 3 || runtime.emptyCalls != 3 || runtime.stopped {
		t.Fatalf("late failure lifecycle proof = %+v", runtime)
	}
	if selection.restoreWhileStopped {
		t.Fatal("selection owner was asked to restore a balancer target before the restored runtime started")
	}
	if _, err := os.Stat(paths.Journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback journal remains: %v", err)
	}
	if got, err := os.ReadFile(paths.LegacyLifecycleInit); err != nil || !bytes.Equal(got, legacyLifecycle) {
		t.Fatalf("legacy lifecycle was not restored: %q (%v)", got, err)
	}

	selection.failureJournal = true
	selection.journalSyncs = 0
	selection.failureInjected = false
	selection.snapshot = []byte("old-selection")
	selection.reconciled = false
	second, err := service.Preview(context.Background(), "session-c")
	if err != nil {
		t.Fatalf("selection rollback preview: %v", err)
	}
	if _, err := service.Apply(context.Background(), "session-c", second.PreviewToken); !errors.Is(err, ErrSetupTransactionRestored) {
		t.Fatalf("selection journal failure = %v", err)
	}
	if !bytes.Equal(selection.snapshot, []byte("old-selection")) || selection.restoreCalls == 0 {
		t.Fatalf("selection was not transactionally restored: snapshot=%q restores=%d", selection.snapshot, selection.restoreCalls)
	}

	// Recreate a crash after the new generation had reconciled selection but
	// before the transaction receipt was cleared. Recovery must use the same
	// runtime-before-C.1 ordering as synchronous failure rollback.
	candidate, err := service.resolve(context.Background())
	if err != nil {
		t.Fatalf("crash candidate: %v", err)
	}
	writers, err := service.inspectSetupWriters()
	if err != nil {
		t.Fatalf("crash writers: %v", err)
	}
	source, err := service.currentSetupSourceManifest("managed-takeover", writers)
	if err != nil {
		t.Fatalf("crash source: %v", err)
	}
	xrayMeta, err := binaryMetadataWithoutProbe(paths.XrayBinary, candidate.Xray.Version)
	if err != nil {
		t.Fatalf("crash xray metadata: %v", err)
	}
	crashXKeenMeta, err := readXKeenGeneration(paths.XkeenBinary, paths.XkeenModuleDir)
	if err != nil {
		t.Fatalf("crash XKeen metadata: %v", err)
	}
	lifecycle, err := setupLifecycleBytes()
	if err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(paths.StagingDir, ".setup-crash")
	if err := ensureXKeenOwnedDirectory(stageDir, setupStagingOwner); err != nil {
		t.Fatalf("crash stage: %v", err)
	}
	crashJournal := setupTransactionJournal{
		SchemaVersion: SetupTransactionSchemaVersion,
		Component:     string(KindSetup),
		Operation:     SetupOperation,
		Phase:         setupPhaseSnapshotIntent,
		Previous: setupPreviousRecord{
			AllAbsent:         false,
			Class:             "managed-takeover",
			SnapshotDir:       service.setupSnapshotRoot(),
			SelectionSnapshot: []byte("old-selection"),
		},
		Candidate: setupCandidateRecord{
			Xray:             candidate.Xray,
			XrayBinarySHA256: xrayMeta.SHA256,
			XrayBinarySize:   xrayMeta.Size,
			XrayBinaryMode:   xrayMeta.Mode,
			Geodata:          candidate.Geodata,
			XKeen:            candidate.XKeen,
			XKeenGeneration:  crashXKeenMeta,
			LifecycleSHA256:  setupLifecycleDigest(lifecycle),
		},
		SourceClass:  "managed-takeover",
		SourceDigest: source.Digest,
		StageDir:     stageDir,
	}
	if err := service.writeJournal(crashJournal); err != nil {
		t.Fatalf("crash journal intent: %v", err)
	}
	snapshot, err := service.captureSetupSnapshot(context.Background(), "managed-takeover", writers)
	if err != nil {
		t.Fatalf("crash snapshot: %v", err)
	}
	crashJournal.Previous.SnapshotSHA = setupSnapshotDigest(snapshot.Manifest)
	crashJournal.Phase = setupPhaseSelectionReconciled
	if err := service.writeJournal(crashJournal); err != nil {
		t.Fatalf("crash journal receipt: %v", err)
	}
	if err := os.WriteFile(paths.XkeenMarker, []byte("crashed-candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection.snapshot = []byte("new-selection")
	selection.reconciled = true
	if err := service.RecoverStartup(context.Background()); err != nil {
		t.Fatalf("crash recovery: %v", err)
	}
	if selection.restoreWhileStopped {
		t.Fatal("crash recovery asked C.1 to restore selection before the restored runtime started")
	}
	if !bytes.Equal(selection.snapshot, []byte("old-selection")) {
		t.Fatalf("crash recovery selection=%q", selection.snapshot)
	}
	if got, err := os.ReadFile(paths.XkeenMarker); err != nil || !bytes.Equal(got, previousMarker) {
		t.Fatalf("crash recovery marker=%q err=%v", got, err)
	}
	if _, err := os.Stat(paths.Journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crash recovery journal remains: %v", err)
	}
}

func TestSetupTakeoverPreservesAuthoritiesPanelStateAndRetiresReviewedWriters(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	paths.LegacyOutbounds = filepath.Join(root, "legacy-outbounds.json")
	paths.WriterScripts = []string{
		filepath.Join(root, "run-bounded-speed-benchmark.sh"),
		filepath.Join(root, "speed_failover_watchdog.sh"),
		filepath.Join(root, "xkeen-control-watchdog"),
		filepath.Join(root, "update-"+"geodata.sh"),
	}
	paths.CronPaths = []string{filepath.Join(root, "root.cron"), filepath.Join(root, "cron.d")}
	paths.PanelPaths = []string{filepath.Join(root, "password.bcrypt"), filepath.Join(root, "selection.json")}
	legacy, _ := setupTestLegacyOutbounds(t)
	if err := os.WriteFile(paths.LegacyOutbounds, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyLifecycle, _ := setupLifecycleBytes()
	if err := os.WriteFile(paths.LegacyLifecycleInit, legacyLifecycle, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.SiblingModule, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.SiblingModule, "legacy.sh"), []byte("legacy-module"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.InstallHelper, []byte("legacy-helper"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths.WriterScripts {
		if err := os.WriteFile(path, []byte("legacy writer"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cronBefore := "17 4 * * * /opt/etc/xkeen-control/run-bounded-speed-benchmark.sh\n18 4 * * * /opt/etc/xkeen-control/speed_failover_watchdog.sh\n19 4 * * * /opt/etc/xkeen-control/xkeen-control-watchdog\n20 4 * * * /opt/etc/xkeen/update-" + "geodata.sh\n23 * * * * /opt/etc/keep-this.sh\n"
	if err := os.WriteFile(paths.CronPaths[0], []byte(cronBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.CronPaths[1], 0o700); err != nil {
		t.Fatal(err)
	}
	cronDirWriter := filepath.Join(paths.CronPaths[1], "xkeen-updater")
	if err := os.WriteFile(cronDirWriter, []byte("xkeen -ug\n* * * * * /opt/etc/keep-dir.sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cronDirUnrelated := filepath.Join(paths.CronPaths[1], "keep")
	if err := os.WriteFile(cronDirUnrelated, []byte("* * * * * /opt/etc/keep-dir.sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	panelBefore := map[string][]byte{paths.PanelPaths[0]: []byte("bcrypt-state"), paths.PanelPaths[1]: []byte(`{"target":"proxy-existing"}`)}
	for path, contents := range panelBefore {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	xrayArchive := writeSyntheticArchive(t, []syntheticZipEntry{{name: "xray", mode: 0o700, contents: []byte("new-xray-binary")}})
	xrayDigest := sha256.Sum256(xrayArchive)
	xrayIdentity := XrayReleaseIdentity{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: int64(len(xrayArchive)), SHA256: hex.EncodeToString(xrayDigest[:])}
	geodataItems := make([]GeodataReleaseIdentity, len(productGeodataCatalog))
	geodataPayloads := make(map[string][]byte, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		payload := bytes.Repeat([]byte{byte('k' + index)}, index+1)
		digest := sha256.Sum256(payload)
		geodataPayloads[entry.Name] = payload
		geodataItems[index] = GeodataReleaseIdentity{ID: entry.ID, Repository: entry.Repository, Tag: "2026-09-05", AssetName: entry.Asset, ActiveName: entry.Name, SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(digest[:])}
	}
	geodata := GeodataCandidateSet{Items: geodataItems, Generation: geodataIdentityGeneration(geodataItems)}
	xkeenArchivePath := writeTestGzipTar(t, []testTarEntry{{name: "_xkeen/runtime.sh", kind: tar.TypeReg, mode: 0o644, format: tar.FormatGNU, contents: []byte("takeover-module")}, {name: "xkeen", kind: tar.TypeReg, mode: 0o755, format: tar.FormatGNU, contents: []byte("takeover-xkeen")}})
	xkeenArchive, err := os.ReadFile(xkeenArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	xkeenEntry, xkeenIdentity := installableCatalogFixture(t, xkeenArchive)
	xkeenEntry.ArchiveMembers = []XKeenArchiveMember{{Name: "_xkeen/runtime.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: int64(len("takeover-module"))}, {Name: "xkeen", Type: xkeenArchiveRegular, Mode: 0o755, Size: int64(len("takeover-xkeen"))}}
	probePath := filepath.Join(t.TempDir(), "xkeen-candidate")
	xkeenMeta, err := extractXKeenArchiveMembers(context.Background(), xkeenArchivePath, probePath, xkeenEntry.ArchiveMembers)
	if err != nil {
		t.Fatal(err)
	}
	xkeenEntry.GenerationSHA256 = xkeenMeta.GenerationSHA256()
	xkeenIdentity.GenerationSHA256 = xkeenEntry.GenerationSHA256
	xkeenIdentity.Generation = xkeenEntry.GenerationSHA256
	reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)] = xkeenEntry
	runtime := &setupTestRuntime{}
	service := NewSetupService(SetupConfig{
		Paths: paths, XrayResolver: setupTestXrayResolver{value: xrayIdentity}, XrayDownloader: &fakeXrayDownloader{archive: xrayArchive},
		GeodataResolver: setupTestGeodataResolver{value: geodata}, GeodataDownloader: &fakeGeodataDownloader{payloads: geodataPayloads},
		XKeenResolver: setupTestXKeenResolver{value: xkeenIdentity}, XKeenDownloader: &fakeXKeenDownloader{archive: xkeenArchive},
		CandidateProbe: &fakeTransactionalProbe{newVersion: xrayIdentity.Version}, CandidateValidator: &setupTestCandidateValidator{}, Runtime: runtime,
		MutationGate: NewComponentMutationGate(), Coordinator: &fakeXrayCoordinator{}, AuthorityLease: authority.NewLease(),
		AvailableSpace: func(string) (uint64, error) { return ^uint64(0), nil }, SyncDirectory: func(string) error { return nil },
	})
	preview, err := service.Preview(context.Background(), "takeover")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.SetupClass != "legacy-xkeen-takeover" || preview.Plan.Profiles.Action != "migrate" || len(preview.Plan.Writers) == 0 || !preview.Plan.PanelPreserved {
		t.Fatalf("takeover plan = %+v", preview.Plan)
	}
	if _, err := service.Apply(context.Background(), "takeover", preview.PreviewToken); err != nil {
		t.Fatal(err)
	}
	if runtime.stopCalls != 1 || runtime.stoppedCalls != 1 || runtime.verifyCalls != 1 {
		t.Fatalf("takeover lifecycle calls = %+v", runtime)
	}
	nodesAfter, err := os.ReadFile(paths.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	migratedRegistry, err := nodes.MigrateLegacy(legacy)
	if err != nil {
		t.Fatal(err)
	}
	expectedNodes, err := nodes.MarshalCanonical(migratedRegistry)
	if err != nil || !bytes.Equal(nodesAfter, expectedNodes) {
		t.Fatalf("migrated nodes were not preserved losslessly: %v", err)
	}
	cronAfter, err := os.ReadFile(paths.CronPaths[0])
	if err != nil || string(cronAfter) != "23 * * * * /opt/etc/keep-this.sh\n" {
		t.Fatalf("cron retirement changed unrelated state: %q (%v)", cronAfter, err)
	}
	for _, path := range paths.WriterScripts {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy writer remains at %s: %v", path, err)
		}
	}
	for path, before := range panelBefore {
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(after, before) {
			t.Fatalf("panel state changed at %s: %v", path, readErr)
		}
	}
	if projection := service.Status(); projection.State != "ready" || projection.Eligible {
		t.Fatalf("takeover post-state = %+v", projection)
	}
	if err := os.WriteFile(paths.WriterScripts[0], []byte("reappeared writer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if projection := service.Status(); projection.State != "takeover" || projection.ReasonCode != SetupReasonManagedTakeover || !projection.Eligible {
		t.Fatalf("writer reappearance did not become explicit Setup reconciliation: %+v", projection)
	}
	if !service.WriterConflict() {
		t.Fatal("ordinary component mutation admission did not remain blocked for the reappeared writer")
	}
	reconcile, err := service.Preview(context.Background(), "reconcile-writer")
	if err != nil {
		t.Fatalf("writer reconciliation preview: %v", err)
	}
	if _, err := service.Apply(context.Background(), "reconcile-writer", reconcile.PreviewToken); err != nil {
		t.Fatalf("writer reconciliation apply: %v", err)
	}
	if _, err := os.Stat(paths.WriterScripts[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reconciled writer remains: %v", err)
	}
	if projection := service.Status(); projection.State != "ready" || projection.Eligible {
		t.Fatalf("writer reconciliation post-state: %+v", projection)
	}
	if service.WriterConflict() {
		t.Fatal("writer conflict remained after explicit Setup reconciliation")
	}
	if got, err := os.ReadFile(cronDirWriter); err != nil || string(got) != "* * * * * /opt/etc/keep-dir.sh\n" {
		t.Fatalf("cron.d writer retirement changed unexpected state: %q (%v)", got, err)
	}
	if got, err := os.ReadFile(cronDirUnrelated); err != nil || string(got) != "* * * * * /opt/etc/keep-dir.sh\n" {
		t.Fatalf("cron.d unrelated state changed: %q (%v)", got, err)
	}
}
