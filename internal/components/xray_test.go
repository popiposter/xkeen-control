package components

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

func TestXrayApplyReResolvesExactIdentityBeforeDownload(t *testing.T) {
	fixture := newXrayFixture(t)
	fixture.resolver.identity = XrayReleaseIdentity{
		Tag:       "v1.2.4",
		Version:   "1.2.4",
		AssetName: xrayCandidateAsset,
		SizeBytes: fixture.identity.SizeBytes,
		SHA256:    fixture.identity.SHA256,
	}
	if err := fixture.service.Apply(context.Background(), fixture.identity); !errors.Is(err, ErrXrayCandidateStale) {
		t.Fatalf("stale identity error = %v", err)
	}
	if fixture.downloader.calls != 0 {
		t.Fatalf("stale identity downloaded an artifact: %d", fixture.downloader.calls)
	}
	if got := readFixtureFile(t, fixture.activePath); string(got) != "old-xray-binary" {
		t.Fatalf("stale identity changed active binary: %q", got)
	}
}

func TestXrayResolverAlwaysFetchesFreshExactMetadata(t *testing.T) {
	transport := &metadataFixtureTransport{responses: map[string][]byte{
		xrayMetadataPath: releaseMetadataForXray(t, "v1.2.3", 123, 'a'),
	}}
	resolver := NewXrayResolver(nil, &http.Client{Transport: transport})
	first, err := resolver.ResolveXray(context.Background())
	if err != nil {
		t.Fatalf("first resolution: %v", err)
	}
	second, err := resolver.ResolveXray(context.Background())
	if err != nil {
		t.Fatalf("second resolution: %v", err)
	}
	if !sameXrayIdentity(first, second) || first.SizeBytes != 123 || first.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("resolved identity = %+v, %+v", first, second)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.calls) != 2 {
		t.Fatalf("metadata calls = %v", transport.calls)
	}
}

func releaseMetadataForXray(t *testing.T, tag string, size int64, digest byte) []byte {
	t.Helper()
	draft, prerelease := false, false
	value, err := json.Marshal(githubReleaseMetadata{
		Draft: &draft, Prerelease: &prerelease, TagName: tag,
		Assets: []githubAssetMetadata{{Name: xrayCandidateAsset, Size: size, State: "uploaded", Digest: "sha256:" + strings.Repeat(string(digest), 64)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestXrayApplyRejectsAuthorityBeforeResolution(t *testing.T) {
	fixture := newXrayFixture(t)
	fixture.authority.err = errors.New("synthetic authority drift")
	if err := fixture.service.Apply(context.Background(), fixture.identity); !errors.Is(err, ErrXrayAuthorityUnavailable) {
		t.Fatalf("authority error = %v", err)
	}
	if fixture.resolver.calls != 0 || fixture.downloader.calls != 0 {
		t.Fatalf("upstream work started before authority proof: resolve=%d download=%d", fixture.resolver.calls, fixture.downloader.calls)
	}
}

func TestXrayApplyRejectsArtifactHashSizeAndFreeSpaceBeforeActivation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*xrayFixture)
		want   error
	}{
		{name: "digest mismatch", mutate: func(fixture *xrayFixture) {
			fixture.identity.SHA256 = strings.Repeat("b", 64)
			fixture.resolver.identity = fixture.identity
		}, want: ErrXrayArtifactRejected},
		{name: "size mismatch", mutate: func(fixture *xrayFixture) {
			fixture.identity.SizeBytes++
			fixture.resolver.identity = fixture.identity
		}, want: ErrXrayArtifactRejected},
		{name: "free space", mutate: func(fixture *xrayFixture) {
			fixture.service.config.AvailableSpace = func(string) (uint64, error) { return 0, nil }
		}, want: errXrayFreeSpaceInsufficient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newXrayFixture(t)
			test.mutate(fixture)
			err := fixture.service.Apply(context.Background(), fixture.identity)
			if !errors.Is(err, test.want) {
				t.Fatalf("apply error = %v, want %v", err, test.want)
			}
			if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
				t.Fatalf("rejected artifact changed active binary: %q", got)
			}
		})
	}
}

func TestXrayApplyStagesCompleteCandidateAndCommitsOnePreviousGeneration(t *testing.T) {
	fixture := newXrayFixture(t)
	if err := fixture.service.Apply(context.Background(), fixture.identity); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "new-xray-binary" {
		t.Fatalf("active binary = %q", got)
	}
	previous, err := fixture.service.loadPreviousGeneration()
	if err != nil {
		t.Fatalf("load previous generation: %v", err)
	}
	if previous.meta.Version != "1.0.0" || previous.meta.SHA256 != fixture.oldSHA256 {
		t.Fatalf("previous generation = %+v", previous.meta)
	}
	if _, err := os.Lstat(fixture.journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction journal remains: %v", err)
	}
	if fixture.service.Ready() != nil {
		t.Fatal("successful transaction left service unavailable")
	}
	if fixture.validator.calls != 1 || fixture.runtime.validateCalls != 1 || fixture.runtime.restartCalls != 1 || fixture.runtime.readyCalls != 1 || fixture.runtime.verifyCalls != 1 {
		t.Fatalf("runtime/candidate calls = validator=%d validate=%d restart=%d ready=%d verify=%d", fixture.validator.calls, fixture.runtime.validateCalls, fixture.runtime.restartCalls, fixture.runtime.readyCalls, fixture.runtime.verifyCalls)
	}
	if len(fixture.runtime.lastTags) != 1 || fixture.runtime.lastTags[0] != "proxy-node-11111111" {
		t.Fatalf("runtime outbound inventory = %v", fixture.runtime.lastTags)
	}
	if len(fixture.validator.seenFiles) != len(xrayCandidateFiles) {
		t.Fatalf("candidate files seen = %d, want %d", len(fixture.validator.seenFiles), len(xrayCandidateFiles))
	}
	if fixture.resolver.calls != 1 || fixture.downloader.calls != 1 {
		t.Fatalf("upstream calls = resolve=%d download=%d", fixture.resolver.calls, fixture.downloader.calls)
	}
}

func TestXrayPreJournalStagingCrashRecoversWithoutMutation(t *testing.T) {
	fixture := newXrayFixture(t)
	fixture.service.config.InjectFailure = func(stage XrayStage) error {
		if stage == XrayStagePreviousStaging {
			return errors.New("synthetic pre-journal process loss")
		}
		return nil
	}
	if err := fixture.service.Apply(context.Background(), fixture.identity); !errors.Is(err, ErrXrayApplyFailed) {
		t.Fatalf("pre-journal fault result = %v", err)
	}
	if _, err := os.Lstat(fixture.journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-journal fault created a journal: %v", err)
	}
	if _, err := os.Lstat(fixture.previousDir + xrayPreviousStagingSuffix); err != nil {
		t.Fatalf("pre-journal staging was not retained for recovery: %v", err)
	}
	resolveCalls, downloadCalls := fixture.resolver.calls, fixture.downloader.calls

	restarted := NewXrayService(fixture.config())
	if restarted.Ready() == nil {
		t.Fatal("staging-only restart was incorrectly reported ready")
	}
	if pending, err := restarted.HasPendingRecovery(); err != nil || !pending {
		t.Fatalf("staging-only recovery pending=%v err=%v", pending, err)
	}
	if err := restarted.RecoverStartup(context.Background()); err != nil {
		t.Fatalf("pre-journal startup recovery: %v", err)
	}
	if err := restarted.Ready(); err != nil {
		t.Fatalf("pre-journal recovery did not restore readiness: %v", err)
	}
	if _, err := os.Lstat(fixture.previousDir + xrayPreviousStagingSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-journal staging remains after recovery: %v", err)
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
		t.Fatalf("pre-journal recovery changed active binary: %q", got)
	}
	if fixture.resolver.calls != resolveCalls || fixture.downloader.calls != downloadCalls {
		t.Fatalf("pre-journal recovery contacted upstream: resolve=%d/%d download=%d/%d", fixture.resolver.calls, resolveCalls, fixture.downloader.calls, downloadCalls)
	}
}

func TestXrayApplyFaultsRecoverBeforeJournalClear(t *testing.T) {
	for _, stage := range []XrayStage{
		XrayStagePreviousSaved,
		XrayStageJournalPrepared,
		XrayStageBinaryCommitted,
		XrayStagePreviousSettled,
	} {
		t.Run(string(stage), func(t *testing.T) {
			fixture := newXrayFixture(t)
			fixture.service.config.InjectFailure = func(current XrayStage) error {
				if current == stage {
					return errors.New("synthetic fault")
				}
				return nil
			}
			if err := fixture.service.Apply(context.Background(), fixture.identity); !errors.Is(err, ErrXrayApplyFailed) {
				t.Fatalf("fault result = %v", err)
			}
			if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
				t.Fatalf("fault changed active binary: %q", got)
			}
			if _, err := os.Lstat(fixture.journalPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fault left journal: %v", err)
			}
			if err := fixture.service.Ready(); err != nil {
				t.Fatalf("recoverable fault left service unavailable: %v", err)
			}
		})
	}
}

func TestXrayPostSwapCancellationUsesIndependentRollbackContext(t *testing.T) {
	fixture := newXrayFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.runtime.cancelOnRestart = cancel
	fixture.runtime.firstRestartErr = errors.New("synthetic post-swap cancellation")

	if err := fixture.service.Apply(ctx, fixture.identity); !errors.Is(err, ErrXrayApplyFailed) {
		t.Fatalf("cancelled transaction result = %v", err)
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
		t.Fatalf("cancelled transaction did not restore active binary: %q", got)
	}
	if _, err := os.Lstat(fixture.journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled transaction left journal: %v", err)
	}
	if err := fixture.service.Ready(); err != nil {
		t.Fatalf("cancelled transaction left service unavailable: %v", err)
	}
	if fixture.runtime.restartCalls != 2 || fixture.runtime.verifyCalls != 1 {
		t.Fatalf("rollback runtime verification calls = restart=%d verify=%d", fixture.runtime.restartCalls, fixture.runtime.verifyCalls)
	}
}

func TestXrayJournalFaultFailsClosedAndStartupRecoveryIsLocalOnly(t *testing.T) {
	fixture := newXrayFixture(t)
	fixture.service.config.InjectFailure = func(stage XrayStage) error {
		if stage == XrayStageRuntimeVerified {
			return errors.New("synthetic journal durability fault")
		}
		return nil
	}
	if err := fixture.service.Apply(context.Background(), fixture.identity); !errors.Is(err, ErrXrayRecoveryFailed) {
		t.Fatalf("journal fault result = %v", err)
	}
	if fixture.service.Ready() == nil {
		t.Fatal("journal durability fault did not fail closed")
	}
	if fixture.resolver.calls != 1 || fixture.downloader.calls != 1 {
		t.Fatalf("unexpected preparation calls = resolve=%d download=%d", fixture.resolver.calls, fixture.downloader.calls)
	}
	fixture.service.config.InjectFailure = nil
	if err := fixture.service.RecoverStartup(context.Background()); err != nil {
		t.Fatalf("startup recovery: %v", err)
	}
	if fixture.service.Ready() != nil {
		t.Fatal("startup recovery did not restore readiness")
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
		t.Fatalf("recovery active binary = %q", got)
	}
	if fixture.resolver.calls != 1 || fixture.downloader.calls != 1 {
		t.Fatalf("startup recovery contacted upstream: resolve=%d download=%d", fixture.resolver.calls, fixture.downloader.calls)
	}
	previous, err := fixture.service.loadPreviousGeneration()
	if err != nil {
		t.Fatalf("load displaced previous generation: %v", err)
	}
	if previous.meta.Version != fixture.identity.Version {
		t.Fatalf("displaced candidate was not retained as previous: %+v", previous.meta)
	}
}

func TestXrayRollbackUsesPreviousWithoutUpstream(t *testing.T) {
	fixture := newXrayFixture(t)
	if err := fixture.service.Apply(context.Background(), fixture.identity); err != nil {
		t.Fatalf("apply before rollback: %v", err)
	}
	resolveCalls, downloadCalls := fixture.resolver.calls, fixture.downloader.calls
	if err := fixture.service.Rollback(context.Background()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
		t.Fatalf("rollback active binary = %q", got)
	}
	if fixture.resolver.calls != resolveCalls || fixture.downloader.calls != downloadCalls {
		t.Fatalf("rollback contacted upstream: resolve=%d/%d download=%d/%d", fixture.resolver.calls, resolveCalls, fixture.downloader.calls, downloadCalls)
	}
	previous, err := fixture.service.loadPreviousGeneration()
	if err != nil || previous.meta.Version != fixture.identity.Version {
		t.Fatalf("rollback previous generation = %+v, err=%v", previous.meta, err)
	}
}

func TestXrayRollbackFailureAfterPromotionPreservesRollbackTarget(t *testing.T) {
	fixture := newXrayFixture(t)
	if err := fixture.service.Apply(context.Background(), fixture.identity); err != nil {
		t.Fatalf("apply before rollback fault: %v", err)
	}
	original, err := fixture.service.loadPreviousGeneration()
	if err != nil {
		t.Fatalf("load original rollback target: %v", err)
	}
	originalBytes := append([]byte(nil), readFixtureFile(t, original.path)...)
	fixture.service.config.InjectFailure = func(stage XrayStage) error {
		if stage == XrayStagePreviousSettled {
			return errors.New("synthetic rollback promotion fault")
		}
		return nil
	}

	if err := fixture.service.Rollback(context.Background()); !errors.Is(err, ErrXrayRollbackFailed) {
		t.Fatalf("failed rollback result = %v", err)
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "new-xray-binary" {
		t.Fatalf("failed rollback did not restore active binary: %q", got)
	}
	preserved, err := fixture.service.loadPreviousGeneration()
	if err != nil {
		t.Fatalf("load preserved rollback target: %v", err)
	}
	if !sameBinaryMetadata(preserved.meta, original.meta) || !bytes.Equal(readFixtureFile(t, preserved.path), originalBytes) {
		t.Fatalf("failed rollback consumed rollback target: got=%+v want=%+v", preserved.meta, original.meta)
	}
	if _, err := os.Lstat(fixture.journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed rollback left journal: %v", err)
	}
	if err := fixture.service.Ready(); err != nil {
		t.Fatalf("failed rollback left service unavailable: %v", err)
	}
}

func TestXrayRecoveryRejectsRestoreConflictAndInvalidStaging(t *testing.T) {
	fixture := newXrayFixture(t)
	if err := fixture.service.Apply(context.Background(), fixture.identity); err != nil {
		t.Fatalf("apply before conflict: %v", err)
	}
	// Recreate a retained journal and a restore marker. The constructor and
	// recovery path must fail closed rather than choose an order.
	journal := xrayTransactionJournal{
		SchemaVersion: XrayTransactionSchemaVersion,
		Component:     string(KindXray),
		Operation:     xrayOperationUpdate,
		Phase:         xrayPhaseRuntimeVerified,
		Previous:      fixture.oldMeta,
		Candidate:     fixture.newMeta,
	}
	if err := fixture.service.writeJournal(journal); err != nil {
		t.Fatalf("write conflict journal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.restoreJournalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.restoreJournalPath, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	conflict := NewXrayService(fixture.config())
	if !errors.Is(conflict.Ready(), ErrXrayRecoveryConflict) {
		t.Fatalf("constructor conflict readiness = %v", conflict.Ready())
	}
	if !errors.Is(conflict.RecoverStartup(context.Background()), ErrXrayRecoveryConflict) {
		t.Fatalf("recovery conflict was not rejected: %v", conflict.RecoverStartup(context.Background()))
	}
	_ = os.Remove(fixture.restoreJournalPath)
	if err := os.Remove(fixture.journalPath); err != nil {
		t.Fatal(err)
	}
	// An incomplete/invalid staging directory remains fail-closed; the valid
	// pre-journal crash path is covered by TestXrayPreJournalStagingCrashRecoversWithoutMutation.
	if err := os.MkdirAll(fixture.previousDir+xrayPreviousStagingSuffix, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := NewXrayService(fixture.config())
	pending, err := stale.HasPendingRecovery()
	if err != nil || !pending {
		t.Fatalf("stale staging pending=%v err=%v", pending, err)
	}
	if err := stale.RecoverStartup(context.Background()); !errors.Is(err, ErrXrayRecoveryFailed) {
		t.Fatalf("stale staging recovery = %v", err)
	}
}

func TestXrayProbeAndIdentityBounds(t *testing.T) {
	valid := XrayVersionResult{Stdout: syntheticXrayVersionOutput("1.2.3"), ExitCode: 0}
	for _, test := range []struct {
		name string
		data XrayVersionResult
		want string
	}{
		{name: "valid", data: valid, want: ""},
		{name: "wrong architecture", data: XrayVersionResult{Stdout: []byte("Xray 1.2.3 (Synthetic.)\ngo1.27.0 linux/amd64\n"), ExitCode: 0}, want: "rejected"},
		{name: "wrong version", data: valid, want: "rejected"},
		{name: "nonzero exit", data: XrayVersionResult{Stdout: valid.Stdout, ExitCode: 1, Err: errors.New("synthetic")}, want: "rejected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected := "1.2.3"
			if test.name == "wrong version" {
				expected = "1.2.4"
			}
			err := validateXrayProbeResult(context.Background(), test.data, expected)
			if test.want == "" && err != nil || test.want != "" && err == nil {
				t.Fatalf("probe result error = %v", err)
			}
		})
	}
	validIdentity := XrayReleaseIdentity{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: 1, SHA256: strings.Repeat("a", 64)}
	if !validXrayIdentity(validIdentity) {
		t.Fatal("valid identity was rejected")
	}
	for _, invalid := range []XrayReleaseIdentity{
		{Tag: "v1.2.3/evil", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: 1, SHA256: strings.Repeat("a", 64)},
		{Tag: "v1.2.3", Version: "1.2.3", AssetName: "xray", SizeBytes: 1, SHA256: strings.Repeat("a", 64)},
		{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: 0, SHA256: strings.Repeat("a", 64)},
		{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: 1, SHA256: "not-a-digest"},
	} {
		if validXrayIdentity(invalid) {
			t.Fatalf("invalid identity accepted: %+v", invalid)
		}
	}
}

func TestXrayArtifactTransportIsFixedAndRedirectBounded(t *testing.T) {
	client := NewXrayArtifactDownloader(nil, nil)
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion < 0x0303 || transport.DialContext == nil {
		t.Fatalf("artifact transport is not fixed/netguarded: %#v", client.http.Transport)
	}
	request := &http.Request{URL: mustURL(t, "https://release-assets.githubusercontent.com/path")}
	if err := xrayArtifactRedirectPolicy(request, nil); err != nil {
		t.Fatalf("allowed redirect rejected: %v", err)
	}
	for _, raw := range []string{
		"http://release-assets.githubusercontent.com/path",
		"https://example.com/path",
		"https://release-assets.githubusercontent.com:8443/path",
	} {
		if err := xrayArtifactRedirectPolicy(&http.Request{URL: mustURL(t, raw)}, nil); err == nil {
			t.Fatalf("unsafe redirect accepted: %s", raw)
		}
	}
	if err := xrayArtifactRedirectPolicy(request, []*http.Request{{}, {}, {}}); err == nil {
		t.Fatal("redirect limit was not enforced")
	}
}

func TestXrayApplyRejectsCandidateProbeMismatchBeforeCommit(t *testing.T) {
	fixture := newXrayFixture(t)
	fixture.probe.newVersion = "1.2.4"
	if err := fixture.service.Apply(context.Background(), fixture.identity); !errors.Is(err, ErrXrayCandidateRejected) {
		t.Fatalf("candidate probe error = %v", err)
	}
	if got := string(readFixtureFile(t, fixture.activePath)); got != "old-xray-binary" {
		t.Fatalf("rejected candidate changed active binary: %q", got)
	}
}

func TestXrayApplyUsesCoordinatorBeforeLockedRecapture(t *testing.T) {
	fixture := newXrayFixture(t)
	if err := fixture.service.Apply(context.Background(), fixture.identity); err != nil {
		t.Fatalf("apply: %v", err)
	}
	coordinatorIndex, recaptureIndex := -1, -1
	for index, event := range fixture.events {
		if event == "coordinator-begin" && coordinatorIndex == -1 {
			coordinatorIndex = index
		}
		if event == "authority-snapshot" && coordinatorIndex >= 0 && recaptureIndex == -1 {
			recaptureIndex = index
		}
	}
	if coordinatorIndex < 0 || recaptureIndex <= coordinatorIndex {
		t.Fatalf("lock order events = %v", fixture.events)
	}
}

func TestFileAuthorityProviderRequiresAdoptedCanonicalCoherentState(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "xray", "configs")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	value := xrayTestAppliance()
	registry := xrayTestRegistry(t)
	files, err := appliance.RenderCandidateFiles(value, registry)
	if err != nil {
		t.Fatalf("render authority fixture: %v", err)
	}
	for name, contents := range files {
		var target string
		if strings.HasPrefix(name, "xray/") {
			target = filepath.Join(configDir, filepath.Base(name))
		} else {
			target = xkeenPath
		}
		writeFixtureFile(t, target, contents, 0o600)
	}
	applianceBytes, err := appliance.MarshalCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, appliancePath, applianceBytes, 0o600)
	registryBytes, err := nodes.MarshalCanonical(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, nodesPath, registryBytes, 0o600)
	if err := os.Chmod(filepath.Dir(appliancePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(nodesPath), 0o700); err != nil {
		t.Fatal(err)
	}
	lease := authority.NewLease()
	applianceService := appliance.NewService(appliance.Config{
		AppliancePath:       appliancePath,
		ConfigDir:           configDir,
		XkeenConfigPath:     xkeenPath,
		NodesPath:           nodesPath,
		ActiveOutboundsPath: outboundsPath,
		Validator:           syntheticApplianceValidator{},
	})
	nodeManager := nodes.NewManager(nodes.Config{Store: nodes.Store{Path: nodesPath}, AuthorityLease: lease})
	provider := NewFileAuthorityProvider(FileAuthorityConfig{
		Appliance:           applianceService,
		Nodes:               nodeManager,
		AppliancePath:       appliancePath,
		NodesPath:           nodesPath,
		ConfigDir:           configDir,
		XkeenConfigPath:     xkeenPath,
		ActiveOutboundsPath: outboundsPath,
	})
	snapshot, err := provider.SnapshotUnderLease(context.Background())
	if err != nil {
		t.Fatalf("coherent authority rejected: %v", err)
	}
	if snapshot.Generation == [sha256.Size]byte{} || snapshot.Registry.Validate() != nil || snapshot.Appliance.Validate() != nil {
		t.Fatalf("authority snapshot is not typed/coherent: %+v", snapshot)
	}
	if err := os.WriteFile(filepath.Join(configDir, "05_routing.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.SnapshotUnderLease(context.Background()); err == nil {
		t.Fatal("managed routing drift was accepted")
	}
}

type syntheticApplianceValidator struct{}

func (syntheticApplianceValidator) ValidateCandidate(_ context.Context, path string) error {
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			return err
		}
	}
	return nil
}

func TestXrayArchiveRejectsTraversalDuplicatesAndNonregularMembers(t *testing.T) {
	tests := []struct {
		name    string
		entries []syntheticZipEntry
	}{
		{name: "missing expected member", entries: []syntheticZipEntry{{name: "README", contents: []byte("ok")}}},
		{name: "wrong root", entries: []syntheticZipEntry{{name: "xray.exe", contents: []byte("bad")}}},
		{name: "traversal", entries: []syntheticZipEntry{{name: "../xray", contents: []byte("bad")}}},
		{name: "absolute", entries: []syntheticZipEntry{{name: "/xray", contents: []byte("bad")}}},
		{name: "backslash", entries: []syntheticZipEntry{{name: `dir\xray`, contents: []byte("bad")}}},
		{name: "duplicate", entries: []syntheticZipEntry{{name: "xray", contents: []byte("a")}, {name: "xray", contents: []byte("b")}}},
		{name: "symlink", entries: []syntheticZipEntry{{name: "xray", mode: os.ModeSymlink | 0o777, contents: []byte("target")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := writeSyntheticArchive(t, test.entries)
			path := filepath.Join(t.TempDir(), "candidate.zip")
			if err := os.WriteFile(path, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			candidate := filepath.Join(t.TempDir(), "candidate-xray")
			if err := extractXrayBinary(context.Background(), path, candidate); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
			if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected candidate remains: %v", err)
			}
		})
	}
	tooMany := []syntheticZipEntry{{name: "xray", contents: []byte("binary")}}
	for index := 0; index < MaxXrayArchiveEntries; index++ {
		tooMany = append(tooMany, syntheticZipEntry{name: fmt.Sprintf("README-%02d", index), contents: []byte("ancillary")})
	}
	t.Run("entry count", func(t *testing.T) {
		archive := writeSyntheticArchive(t, tooMany)
		archivePath := filepath.Join(t.TempDir(), "candidate.zip")
		if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := extractXrayBinary(context.Background(), archivePath, filepath.Join(t.TempDir(), "candidate-xray")); err == nil {
			t.Fatal("oversized archive entry count was accepted")
		}
	})
}

type xrayFixture struct {
	root               string
	activePath         string
	configDir          string
	assetDir           string
	previousDir        string
	journalPath        string
	restoreJournalPath string
	stagingDir         string
	identity           XrayReleaseIdentity
	oldMeta            xrayBinaryMetadata
	newMeta            xrayBinaryMetadata
	oldSHA256          string
	authority          *fakeXrayAuthority
	resolver           *fakeXrayResolver
	downloader         *fakeXrayDownloader
	probe              *fakeTransactionalProbe
	validator          *fakeXrayCandidateValidator
	runtime            *fakeXrayRuntime
	coordinator        *fakeXrayCoordinator
	lease              *authority.Lease
	service            *XrayService
	events             []string
}

func newXrayFixture(t *testing.T) *xrayFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &xrayFixture{
		root:               root,
		activePath:         filepath.Join(root, "xray", "xray"),
		configDir:          filepath.Join(root, "xray", "configs"),
		assetDir:           filepath.Join(root, "xray", "dat"),
		previousDir:        filepath.Join(root, "control", "previous", "components", "xray"),
		journalPath:        filepath.Join(root, "control", "state", "component-transaction.json"),
		restoreJournalPath: filepath.Join(root, "control", "state", "appliance-import-transaction.json"),
		stagingDir:         filepath.Join(root, "tmp", "xkeen-control", "components", "xray"),
		lease:              authority.NewLease(),
		coordinator:        &fakeXrayCoordinator{},
	}
	fixture.authority = &fakeXrayAuthority{events: &fixture.events}
	fixture.coordinator.events = &fixture.events
	if err := os.MkdirAll(filepath.Dir(fixture.activePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.activePath, []byte("old-xray-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldDigest := sha256.Sum256([]byte("old-xray-binary"))
	fixture.oldSHA256 = fmt.Sprintf("%x", oldDigest[:])
	fixture.oldMeta = xrayBinaryMetadata{Exists: true, Version: "1.0.0", SHA256: fixture.oldSHA256, Size: int64(len("old-xray-binary")), Mode: 0o700}
	archive := writeSyntheticArchive(t, []syntheticZipEntry{
		{name: "xray", mode: 0o700, contents: []byte("new-xray-binary")},
		{name: "README", contents: []byte("synthetic ancillary file")},
	})
	archiveDigest := sha256.Sum256(archive)
	fixture.identity = XrayReleaseIdentity{Tag: "v1.2.3", Version: "1.2.3", AssetName: xrayCandidateAsset, SizeBytes: int64(len(archive)), SHA256: fmt.Sprintf("%x", archiveDigest[:])}
	newDigest := sha256.Sum256([]byte("new-xray-binary"))
	fixture.newMeta = xrayBinaryMetadata{Exists: true, Version: fixture.identity.Version, SHA256: fmt.Sprintf("%x", newDigest[:]), Size: int64(len("new-xray-binary")), Mode: 0o700}
	fixture.authority.snapshot = XrayAuthoritySnapshot{Appliance: xrayTestAppliance(), Registry: xrayTestRegistry(t), Generation: sha256.Sum256([]byte("generation-1"))}
	fixture.resolver = &fakeXrayResolver{identity: fixture.identity}
	fixture.downloader = &fakeXrayDownloader{archive: archive}
	fixture.probe = &fakeTransactionalProbe{newVersion: fixture.identity.Version}
	fixture.validator = &fakeXrayCandidateValidator{}
	fixture.runtime = &fakeXrayRuntime{}
	fixture.service = NewXrayService(fixture.config())
	return fixture
}

func (f *xrayFixture) config() XrayConfig {
	return XrayConfig{
		Resolver:             f.resolver,
		Downloader:           f.downloader,
		Authority:            f.authority,
		Runtime:              f.runtime,
		CandidateProbe:       f.probe,
		CandidateValidator:   f.validator,
		AuthorityLease:       f.lease,
		Coordinator:          f.coordinator,
		ActiveBinaryPath:     f.activePath,
		ConfigDir:            f.configDir,
		AssetDir:             f.assetDir,
		PreviousDir:          f.previousDir,
		JournalPath:          f.journalPath,
		StagingDir:           f.stagingDir,
		RestoreJournalPath:   f.restoreJournalPath,
		AvailableSpace:       func(string) (uint64, error) { return ^uint64(0), nil },
		SyncDirectory:        func(string) error { return nil },
		PrepareTimeout:       DefaultXrayPrepareTimeout,
		ActivationTimeout:    DefaultXrayActivationTimeout,
		RollbackTimeout:      DefaultXrayRollbackTimeout,
		TransactionTimeout:   DefaultXrayTransactionTimeout,
		AuthorityWaitTimeout: DefaultXrayAuthorityWaitTimeout,
	}
}

type fakeXrayAuthority struct {
	mu       sync.Mutex
	snapshot XrayAuthoritySnapshot
	err      error
	calls    int
	events   *[]string
}

func (a *fakeXrayAuthority) SnapshotUnderLease(context.Context) (XrayAuthoritySnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.events != nil {
		*a.events = append(*a.events, "authority-snapshot")
	}
	if a.err != nil {
		return XrayAuthoritySnapshot{}, a.err
	}
	return a.snapshot, nil
}

type fakeXrayResolver struct {
	identity XrayReleaseIdentity
	err      error
	calls    int
}

func (r *fakeXrayResolver) ResolveXray(context.Context) (XrayReleaseIdentity, error) {
	r.calls++
	if r.err != nil {
		return XrayReleaseIdentity{}, r.err
	}
	return r.identity, nil
}

type fakeXrayDownloader struct {
	archive []byte
	err     error
	calls   int
}

func (d *fakeXrayDownloader) DownloadXray(_ context.Context, _ XrayReleaseIdentity, destination io.Writer) error {
	d.calls++
	if d.err != nil {
		return d.err
	}
	_, err := destination.Write(d.archive)
	return err
}

type fakeTransactionalProbe struct {
	newVersion string
}

func (p *fakeTransactionalProbe) ProbeXrayCandidate(_ context.Context, binary string) XrayVersionResult {
	contents, err := os.ReadFile(binary)
	if err != nil {
		return XrayVersionResult{ExitCode: -1, Err: err}
	}
	version := "1.0.0"
	if bytes.Equal(contents, []byte("new-xray-binary")) {
		version = p.newVersion
	}
	return XrayVersionResult{Stdout: syntheticXrayVersionOutput(version), ExitCode: 0}
}

type fakeXrayCandidateValidator struct {
	calls     int
	seenFiles map[string]struct{}
	err       error
}

func (v *fakeXrayCandidateValidator) ValidateXrayCandidate(_ context.Context, _ string, configDir, _ string) error {
	v.calls++
	if v.err != nil {
		return v.err
	}
	v.seenFiles = make(map[string]struct{})
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		v.seenFiles[filepath.ToSlash(filepath.Join("xray", entry.Name()))] = struct{}{}
	}
	if _, err := os.Stat(filepath.Join(configDir, "..", "xkeen", "xkeen.json")); err != nil {
		return errors.New("candidate fixed compatibility tree is incomplete")
	}
	v.seenFiles["xkeen/xkeen.json"] = struct{}{}
	for name := range xrayCandidateFiles {
		if strings.HasPrefix(name, "xray/") {
			if _, ok := v.seenFiles[name]; !ok {
				return errors.New("candidate config is incomplete")
			}
		}
	}
	return nil
}

type fakeXrayRuntime struct {
	validateCalls   int
	restartCalls    int
	readyCalls      int
	verifyCalls     int
	lastTags        []string
	err             error
	cancelOnRestart context.CancelFunc
	cancelOnce      sync.Once
	firstRestartErr error
}

func (r *fakeXrayRuntime) ValidateActiveConfig(context.Context) error {
	r.validateCalls++
	return r.err
}
func (r *fakeXrayRuntime) Restart(context.Context) error {
	r.restartCalls++
	if r.cancelOnRestart != nil {
		r.cancelOnce.Do(r.cancelOnRestart)
	}
	if r.restartCalls == 1 && r.firstRestartErr != nil {
		return r.firstRestartErr
	}
	return r.err
}
func (r *fakeXrayRuntime) WaitReady(context.Context) error {
	r.readyCalls++
	return r.err
}
func (r *fakeXrayRuntime) Verify(_ context.Context, tags []string) error {
	r.verifyCalls++
	r.lastTags = append([]string(nil), tags...)
	return r.err
}

type fakeXrayCoordinator struct {
	mu          sync.Mutex
	maintenance bool
	events      *[]string
}

func (c *fakeXrayCoordinator) BeginApply(context.Context) (func(), error) {
	c.mu.Lock()
	if c.maintenance {
		c.mu.Unlock()
		return nil, errors.New("synthetic coordinator maintenance")
	}
	if c.events != nil {
		*c.events = append(*c.events, "coordinator-begin")
	}
	c.mu.Unlock()
	return func() {}, nil
}

func (c *fakeXrayCoordinator) BeginRecovery(context.Context) (func(), error) {
	return func() {}, nil
}

func (c *fakeXrayCoordinator) EnterMaintenance() {
	c.mu.Lock()
	c.maintenance = true
	c.mu.Unlock()
}

func (c *fakeXrayCoordinator) ExitMaintenance() {
	c.mu.Lock()
	c.maintenance = false
	c.mu.Unlock()
}

func xrayTestAppliance() appliance.Appliance {
	return appliance.Appliance{
		SchemaVersion: appliance.SchemaVersion,
		DNS: appliance.DNSPolicy{
			Servers: []appliance.DNSServer{{Address: "localhost"}}, QueryStrategy: "UseIPv4",
			ServeStale: true, ServeExpiredTTL: 3600, DisableFallbackIfMatch: true,
			EnableParallelQuery: true, UseSystemHosts: true,
		},
		Routing: appliance.RoutingPolicy{
			DomainStrategy: "IPIfNonMatch", DomainMatcher: "hybrid",
			Rules: []appliance.RoutingRule{
				{Type: "field", InboundTag: []string{"api"}, Action: appliance.RuleAction{OutboundTag: "api"}},
				{Type: "field", InboundTag: []string{"tproxy"}, Action: appliance.RuleAction{BalancerTag: "bal-proxy"}},
			},
			Balancers: []appliance.Balancer{{Tag: "bal-proxy", Selector: []string{"proxy-"}, FallbackTag: "block", Strategy: appliance.BalancerStrategy{Type: "leastPing"}}},
		},
		Observatory: appliance.ObservatoryPolicy{SubjectSelector: []string{"proxy-"}, ProbeInterval: "5m"},
	}
}

func xrayTestRegistry(t *testing.T) nodes.Registry {
	t.Helper()
	profile := nodes.VLESS{
		UUID: "11111111-1111-4111-8111-111111111111", Host: "node.example.com", Port: 443,
		Encryption: "none", Security: "reality", ServerName: "node.example.com", Fingerprint: "chrome",
		PublicKey: "AAAAAAAAAAAAAAAA", ShortID: "0123456789abcdef", Network: "tcp",
	}
	node, err := nodes.NewNodeWithID(profile, "Synthetic node", nodes.Source{Type: "manual"}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	registry := nodes.NewRegistry()
	registry.Nodes = []nodes.Node{node}
	return registry
}

type syntheticZipEntry struct {
	name     string
	mode     os.FileMode
	contents []byte
}

func writeSyntheticArchive(t *testing.T, entries []syntheticZipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func syntheticXrayVersionOutput(version string) []byte {
	return []byte("Xray " + version + " (Synthetic.)\ngo1.27.0 linux/arm64\n")
}

func readFixtureFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
