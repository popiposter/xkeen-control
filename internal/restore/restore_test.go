package restore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

const testPassphrase = "correct synthetic passphrase"

type fakeActivator struct {
	mu             sync.Mutex
	validateErr    error
	restartErrs    []error
	readyErrs      []error
	inventoryErrs  []error
	validations    int
	restarts       int
	readyCalls     int
	inventoryCalls int
	lastCandidate  string
}

func (a *fakeActivator) ValidateCandidate(_ context.Context, path string) error {
	a.mu.Lock()
	a.validations++
	a.lastCandidate = path
	err := a.validateErr
	a.mu.Unlock()
	if err != nil {
		return err
	}
	for _, name := range []string{
		"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json",
		"05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json",
	} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			return errors.New("candidate is incomplete")
		}
	}
	return nil
}

func (a *fakeActivator) Restart(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := a.restarts
	a.restarts++
	if index < len(a.restartErrs) {
		return a.restartErrs[index]
	}
	return nil
}

func (a *fakeActivator) WaitReady(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := a.readyCalls
	a.readyCalls++
	if index < len(a.readyErrs) {
		return a.readyErrs[index]
	}
	return nil
}

func (a *fakeActivator) VerifyOutboundTags(context.Context, []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := a.inventoryCalls
	a.inventoryCalls++
	if index < len(a.inventoryErrs) {
		return a.inventoryErrs[index]
	}
	return nil
}

type fakeCoordinator struct {
	mu       sync.Mutex
	begins   int
	releases int
}

func (c *fakeCoordinator) BeginApply(context.Context) (func(), error) {
	c.mu.Lock()
	c.begins++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.releases++
			c.mu.Unlock()
		})
	}, nil
}

type restoreFixture struct {
	root          string
	configDir     string
	xkeenPath     string
	nodesPath     string
	appliancePath string
	outboundsPath string
	previousDir   string
	stateDir      string
	appliance     appliance.Appliance
	registry      nodes.Registry
	activator     *fakeActivator
	coordinator   *fakeCoordinator
	service       *Service
	lease         *authority.Lease
}

func newRestoreFixture(t *testing.T) *restoreFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &restoreFixture{
		root:          root,
		configDir:     filepath.Join(root, "xray"),
		xkeenPath:     filepath.Join(root, "xkeen", "xkeen.json"),
		nodesPath:     filepath.Join(root, "control", "secrets", "nodes.json"),
		appliancePath: filepath.Join(root, "control", "config", "appliance.json"),
		previousDir:   filepath.Join(root, "control", "previous", "appliance-import"),
		stateDir:      filepath.Join(root, "control", "state"),
		activator:     &fakeActivator{},
		coordinator:   &fakeCoordinator{},
		lease:         authority.NewLease(),
		appliance:     testAppliance(),
		registry:      testRegistry(t),
	}
	fixture.outboundsPath = filepath.Join(fixture.configDir, "04_outbounds.json")
	repoRoot := repositoryRoot(t)
	for _, name := range []string{"01_log.json", "03_inbounds.json", "06_policy.json", "08_api.json"} {
		copyFile(t, filepath.Join(repoRoot, "config", "xray", name), filepath.Join(fixture.configDir, name))
	}
	copyFile(t, filepath.Join(repoRoot, "config", "xkeen", "xkeen.json"), fixture.xkeenPath)
	applianceBytes, err := appliance.MarshalCanonical(fixture.appliance)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, fixture.appliancePath, applianceBytes)
	policyFiles, err := appliance.RenderPolicyFiles(fixture.appliance)
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range policyFiles {
		writeFile(t, filepath.Join(fixture.configDir, filepath.Base(name)), contents)
	}
	registryBytes, err := nodes.MarshalCanonical(fixture.registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, fixture.nodesPath, registryBytes)
	outbounds, err := nodes.Render(fixture.registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, fixture.outboundsPath, outbounds)
	fixture.service = fixture.newService(nil)
	if err := fixture.service.RecoverStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *restoreFixture) newService(inject FailureInjector) *Service {
	return NewService(Config{
		AppliancePath:       f.appliancePath,
		NodesPath:           f.nodesPath,
		ConfigDir:           f.configDir,
		XkeenConfigPath:     f.xkeenPath,
		ActiveOutboundsPath: f.outboundsPath,
		PreviousDir:         f.previousDir,
		StateDir:            f.stateDir,
		Activator:           f.activator,
		Coordinator:         f.coordinator,
		AuthorityLease:      f.lease,
		InjectFailure:       inject,
		Now:                 func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
	})
}

func testAppliance() appliance.Appliance {
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

func testRegistry(t *testing.T) nodes.Registry {
	t.Helper()
	profile := nodes.VLESS{
		UUID: "11111111-1111-4111-8111-111111111111", Host: "node.example.com", Port: 443,
		Encryption: "none", Security: "reality", ServerName: "node.example.com", Fingerprint: "chrome",
		PublicKey: "AAAAAAAAAAAAAAAA", ShortID: "0123456789abcdef", Network: "tcp",
	}
	node, err := nodes.NewNodeWithID(profile, "Synthetic node", nodes.Source{Type: "subscription", SubscriptionID: "sub-11111111"}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	registry := nodes.NewRegistry()
	registry.Subscriptions = []nodes.Subscription{{ID: "sub-11111111", Name: "Synthetic provider", URL: "https://subscription.example/synthetic-token", Enabled: true}}
	registry.Nodes = []nodes.Node{node}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func bundleBytes(t *testing.T, value appliance.Appliance, registry *nodes.Registry, encrypted bool) []byte {
	t.Helper()
	service := backup.NewService(backup.Config{
		Appliance: staticAppliance{value: value}, Nodes: staticRegistry{value: registry},
		Build:  buildinfo.Info{Product: "xkeen-control", Version: "dev", SourceCommit: "dev", Channel: "development"},
		Now:    func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 256)), GOOS: "linux", GOARCH: "arm64",
	})
	var (
		contents []byte
		err      error
	)
	if encrypted {
		contents, err = service.ExportSecret(context.Background(), testPassphrase)
	} else {
		contents, err = service.Export(context.Background())
	}
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

type staticAppliance struct{ value appliance.Appliance }

func (s staticAppliance) Snapshot() (appliance.Appliance, error) { return s.value, nil }

type staticRegistry struct{ value *nodes.Registry }

func (s staticRegistry) Snapshot(context.Context) (nodes.Registry, error) {
	if s.value == nil {
		return nodes.Registry{}, errors.New("not requested")
	}
	return *s.value, nil
}

func changedAppliance(value appliance.Appliance) appliance.Appliance {
	value.DNS.ServeStale = !value.DNS.ServeStale
	return value
}

func addNode(t *testing.T, registry *nodes.Registry, id, host string) {
	t.Helper()
	profile := nodes.VLESS{
		UUID: "22222222-2222-4222-8222-222222222222", Host: host, Port: 443,
		Encryption: "none", Security: "reality", ServerName: host, Fingerprint: "chrome",
		PublicKey: "BBBBBBBBBBBBBBBB", ShortID: "fedcba9876543210", Network: "tcp",
	}
	node, err := nodes.NewNodeWithID(profile, "Imported "+id, nodes.Source{Type: "manual"}, id)
	if err != nil {
		t.Fatal(err)
	}
	registry.Nodes = append(registry.Nodes, node)
}

func cloneTestRegistry(t *testing.T, value nodes.Registry) nodes.Registry {
	t.Helper()
	contents, err := nodes.MarshalCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := nodes.ParseCanonical(contents)
	if err != nil {
		t.Fatal(err)
	}
	return copy
}

func authorityBytes(t *testing.T, f *restoreFixture) (applianceBytes, nodesBytes, outbounds []byte) {
	t.Helper()
	applianceBytes, err := os.ReadFile(f.appliancePath)
	if err != nil {
		t.Fatal(err)
	}
	nodesBytes, err = os.ReadFile(f.nodesPath)
	if err != nil {
		t.Fatal(err)
	}
	outbounds, err = os.ReadFile(f.outboundsPath)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func assertRestored(t *testing.T, f *restoreFixture, applianceBytes, nodesBytes, outbounds []byte) {
	t.Helper()
	got, _ := os.ReadFile(f.appliancePath)
	if !bytes.Equal(got, applianceBytes) {
		t.Fatal("appliance authority was not restored byte-for-byte")
	}
	got, _ = os.ReadFile(f.nodesPath)
	if !bytes.Equal(got, nodesBytes) {
		t.Fatal("nodes authority was not restored byte-for-byte")
	}
	got, _ = os.ReadFile(f.outboundsPath)
	if !bytes.Equal(got, outbounds) {
		t.Fatal("generated outbounds were not re-rendered from the previous registry")
	}
	for _, name := range []string{"02_dns.json", "05_routing.json", "07_observatory.json"} {
		value, err := os.ReadFile(filepath.Join(f.configDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "02_dns.json" && !bytes.Contains(value, []byte(`"serveStale": true`)) {
			t.Fatal("DNS policy was not restored")
		}
	}
}

func TestSafeSettingsOnlyApplyPreservesNodesAndTrueNoopDoesNotRestart(t *testing.T) {
	fixture := newRestoreFixture(t)
	originalAppliance, originalNodes, originalOutbounds := authorityBytes(t, fixture)
	candidate := changedAppliance(fixture.appliance)
	preview, err := fixture.service.Preview(context.Background(), "session-a", SettingsOnly, bundleBytes(t, candidate, nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	if preview.ContainsSecrets || preview.Noop || len(preview.Compatibility.Blockers) != 0 || !preview.Changes.ApplianceChanged {
		t.Fatalf("safe settings preview = %+v", preview)
	}
	result, err := fixture.service.Apply(context.Background(), "session-a", preview.Token)
	if err != nil || result.Classification != "applied" || result.Noop {
		t.Fatalf("settings apply = %+v, %v", result, err)
	}
	newApplianceBytes, err := appliance.MarshalCanonical(candidate)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(fixture.appliancePath)
	if !bytes.Equal(got, newApplianceBytes) {
		t.Fatal("settings authority was not committed")
	}
	got, _ = os.ReadFile(fixture.nodesPath)
	if !bytes.Equal(got, originalNodes) {
		t.Fatal("settings-only rewrote the destination node authority")
	}
	if fixture.activator.restarts != 1 {
		t.Fatalf("restart count after settings apply = %d", fixture.activator.restarts)
	}
	entries, err := os.ReadDir(fixture.previousDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "appliance.json" && entry.Name() != "nodes.json" {
			t.Fatalf("previous generation retained non-authority material %q", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "appliance-import-transaction.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful apply journal = %v", err)
	}
	_ = originalAppliance
	_ = originalOutbounds

	beforeRestarts := fixture.activator.restarts
	current, err := appliance.Parse(newApplianceBytes)
	if err != nil {
		t.Fatal(err)
	}
	noopPreview, err := fixture.service.Preview(context.Background(), "session-noop", SettingsOnly, bundleBytes(t, current, nil, false), "")
	if err != nil || !noopPreview.Noop {
		t.Fatalf("noop preview = %+v, %v", noopPreview, err)
	}
	noopResult, err := fixture.service.Apply(context.Background(), "session-noop", noopPreview.Token)
	if err != nil || !noopResult.Noop || noopResult.Classification != "no-op" {
		t.Fatalf("noop apply = %+v, %v", noopResult, err)
	}
	if fixture.activator.restarts != beforeRestarts {
		t.Fatal("no-op apply restarted Xray")
	}
}

func TestReplaceMergeUseStableIDsAndRejectConflicts(t *testing.T) {
	fixture := newRestoreFixture(t)
	imported := cloneTestRegistry(t, fixture.registry)
	imported.Subscriptions = append(imported.Subscriptions, nodes.Subscription{ID: "sub-22222222", Name: "Second provider", URL: "https://subscription.example/second", Enabled: true})
	addNode(t, &imported, "node-33333333", "second.example.com")
	addNode(t, &imported, "node-22222222", "third.example.com")
	// Deliberately reverse both new-entry orders. Merge canonicalizes them by
	// stable ID, not upload order.
	imported.Nodes[1], imported.Nodes[2] = imported.Nodes[2], imported.Nodes[1]
	if err := imported.Validate(); err != nil {
		t.Fatal(err)
	}
	secret := bundleBytes(t, fixture.appliance, &imported, true)
	preview, err := fixture.service.Preview(context.Background(), "merge-session", MergeRegistry, secret, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Changes.NodesAdded != 2 || preview.Changes.SubscriptionsAdded != 1 || len(preview.Compatibility.Blockers) != 0 {
		t.Fatalf("merge preview = %+v", preview)
	}
	if _, err := fixture.service.Apply(context.Background(), "merge-session", preview.Token); err != nil {
		t.Fatal(err)
	}
	merged, err := (nodes.Store{Path: fixture.nodesPath}).Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(merged.Nodes))
	for _, node := range merged.Nodes {
		ids = append(ids, node.ID)
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("merged node order = %v", ids)
	}
	subIDs := make([]string, 0, len(merged.Subscriptions))
	for _, subscription := range merged.Subscriptions {
		subIDs = append(subIDs, subscription.ID)
	}
	if !sort.StringsAreSorted(subIDs) {
		t.Fatalf("merged subscription order = %v", subIDs)
	}

	conflictFixture := newRestoreFixture(t)
	conflicting := cloneTestRegistry(t, conflictFixture.registry)
	conflicting.Subscriptions[0].URL = "https://subscription.example/other"
	if _, err := conflictFixture.service.Preview(context.Background(), "conflict-session", MergeRegistry, bundleBytes(t, conflictFixture.appliance, &conflicting, true), testPassphrase); !errors.Is(err, ErrCandidateInvalid) {
		t.Fatalf("subscription conflict = %v", err)
	}
	conflicting = cloneTestRegistry(t, conflictFixture.registry)
	conflicting.Nodes[0].VLESS.Host = "different.example.com"
	conflicting.Nodes[0].Source = nodes.Source{Type: "manual"}
	conflicting.Nodes[0].SourceKey = conflicting.Nodes[0].VLESS.SourceKey()
	if _, err := conflictFixture.service.Preview(context.Background(), "node-conflict-session", MergeRegistry, bundleBytes(t, conflictFixture.appliance, &conflicting, true), testPassphrase); !errors.Is(err, ErrCandidateInvalid) {
		t.Fatalf("node conflict = %v", err)
	}

	safe := bundleBytes(t, conflictFixture.appliance, nil, false)
	if _, err := conflictFixture.service.Preview(context.Background(), "safe-replace", ReplaceRegistry, safe, ""); !errors.Is(err, ErrEncryptedBundleRequired) {
		t.Fatalf("safe replace = %v", err)
	}
}

func TestPreviewBindingTTLBoundEvictionAndInvalidation(t *testing.T) {
	fixture := newRestoreFixture(t)
	clock := time.Unix(1_750_000_000, 0).UTC()
	fixture.service = NewService(Config{
		AppliancePath: fixture.appliancePath, NodesPath: fixture.nodesPath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, ActiveOutboundsPath: fixture.outboundsPath,
		PreviousDir: fixture.previousDir, StateDir: fixture.stateDir, Activator: fixture.activator,
		AuthorityLease: fixture.lease, MaxPreviews: 2, PreviewTTL: time.Minute,
		Now: func() time.Time { return clock },
	})
	bundle := bundleBytes(t, fixture.appliance, nil, false)
	first, err := fixture.service.Preview(context.Background(), "a", SettingsOnly, bundle, "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	second, err := fixture.service.Preview(context.Background(), "b", SettingsOnly, bundle, "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := fixture.service.Preview(context.Background(), "c", SettingsOnly, bundle, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Apply(context.Background(), "a", first.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("oldest preview after bound = %v", err)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := fixture.service.Apply(context.Background(), "b", second.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("expired preview = %v", err)
	}
	clock = time.Unix(1_750_000_010, 0).UTC()
	third, err := fixture.service.Preview(context.Background(), "bound", SettingsOnly, bundle, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Apply(context.Background(), "wrong-binding", third.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("binding mismatch = %v", err)
	}
	fixture.service.Invalidate("bound")
	if _, err := fixture.service.Apply(context.Background(), "bound", third.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("invalidated preview = %v", err)
	}
}

func TestBlockedApplyConsumesOneShotPreview(t *testing.T) {
	fixture := newRestoreFixture(t)
	if err := os.Remove(fixture.appliancePath); err != nil {
		t.Fatal(err)
	}
	preview, err := fixture.service.Preview(context.Background(), "blocked", SettingsOnly, bundleBytes(t, fixture.appliance, nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Compatibility.Blockers) == 0 {
		t.Fatal("missing appliance did not produce a preview blocker")
	}
	if _, err := fixture.service.Apply(context.Background(), "blocked", preview.Token); !errors.Is(err, ErrCompatibilityBlocked) {
		t.Fatalf("blocked apply = %v", err)
	}
	if _, err := fixture.service.Apply(context.Background(), "blocked", preview.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("blocked preview was replayable: %v", err)
	}
}

func TestStaleUnsupportedCurrentAndCompatibilityDriftBlockBeforeMutation(t *testing.T) {
	fixture := newRestoreFixture(t)
	preview, err := fixture.service.Preview(context.Background(), "stale", SettingsOnly, bundleBytes(t, changedAppliance(fixture.appliance), nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	changed := cloneTestRegistry(t, fixture.registry)
	addNode(t, &changed, "node-22222222", "new.example.com")
	if err := (nodes.Store{Path: fixture.nodesPath}).Save(changed); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Apply(context.Background(), "stale", preview.Token); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("stale authority = %v", err)
	}

	unsupportedFixture := newRestoreFixture(t)
	originalAppliance, originalNodes, _ := authorityBytes(t, unsupportedFixture)
	legacy, err := json.Marshal(map[string]any{
		"schemaVersion": unsupportedFixture.registry.SchemaVersion,
		"nodes":         unsupportedFixture.registry.Nodes,
		"subscriptions": unsupportedFixture.registry.Subscriptions,
		"futureRoot":    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy = append(legacy, '\n')
	writeFile(t, unsupportedFixture.nodesPath, legacy)
	preview, err = unsupportedFixture.service.Preview(context.Background(), "future", SettingsOnly, bundleBytes(t, changedAppliance(unsupportedFixture.appliance), nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Compatibility.Blockers) != 0 {
		t.Fatalf("settings-only compatibility blockers = %+v", preview.Compatibility)
	}
	if _, err := unsupportedFixture.service.Apply(context.Background(), "future", preview.Token); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(unsupportedFixture.nodesPath)
	if !bytes.Equal(got, legacy) {
		t.Fatal("settings-only did not preserve compatibility registry bytes")
	}
	_ = originalAppliance
	_ = originalNodes

	driftFixture := newRestoreFixture(t)
	originalAppliance, originalNodes, originalOutbounds := authorityBytes(t, driftFixture)
	preview, err = driftFixture.service.Preview(context.Background(), "drift", SettingsOnly, bundleBytes(t, changedAppliance(driftFixture.appliance), nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(driftFixture.configDir, "03_inbounds.json"), []byte(`{"inbounds":[]}`))
	if _, err := driftFixture.service.Apply(context.Background(), "drift", preview.Token); !errors.Is(err, ErrCompatibilityBlocked) {
		t.Fatalf("fixed drift = %v", err)
	}
	assertRestored(t, driftFixture, originalAppliance, originalNodes, originalOutbounds)
	if _, err := os.Stat(driftFixture.previousDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("drift created previous generation: %v", err)
	}
}

func TestCandidateValidationPrecedesPersistentMutation(t *testing.T) {
	fixture := newRestoreFixture(t)
	originalAppliance, originalNodes, originalOutbounds := authorityBytes(t, fixture)
	fixture.activator.validateErr = errors.New("synthetic validator failure")
	fixture.service = fixture.newService(nil)
	preview, err := fixture.service.Preview(context.Background(), "candidate", SettingsOnly, bundleBytes(t, changedAppliance(fixture.appliance), nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Apply(context.Background(), "candidate", preview.Token); !errors.Is(err, ErrCandidateInvalid) {
		t.Fatalf("candidate validation = %v", err)
	}
	assertRestored(t, fixture, originalAppliance, originalNodes, originalOutbounds)
	if _, err := os.Stat(fixture.previousDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate rejection created previous generation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "appliance-import-transaction.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate rejection created journal: %v", err)
	}
}

func TestEveryPostJournalFailureRestoresLogicalGenerationAndRuntime(t *testing.T) {
	stages := []Stage{
		StageJournalPrepared, StageApplianceCommitted, StageAuthoritiesCommitted,
		StageDNSCommitted, StageRoutingCommitted, StageObservatoryCommitted,
		StageOutboundsCommitted, StageGeneratedCommitted, StageRestarted, StageReady,
		StageRuntimeVerified,
	}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			fixture := newRestoreFixture(t)
			originalAppliance, originalNodes, originalOutbounds := authorityBytes(t, fixture)
			failed := false
			fixture.service = fixture.newService(func(got Stage) error {
				if got == stage && !failed {
					failed = true
					return errors.New("synthetic stage failure")
				}
				return nil
			})
			preview, err := fixture.service.Preview(context.Background(), "failure", SettingsOnly, bundleBytes(t, changedAppliance(fixture.appliance), nil, false), "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.service.Apply(context.Background(), "failure", preview.Token); !errors.Is(err, ErrApplyFailed) {
				t.Fatalf("stage %s apply error = %v", stage, err)
			}
			assertRestored(t, fixture, originalAppliance, originalNodes, originalOutbounds)
			if _, err := os.Stat(filepath.Join(fixture.stateDir, "appliance-import-transaction.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stage %s journal = %v", stage, err)
			}
			if fixture.activator.restarts < 1 {
				t.Fatalf("stage %s did not converge runtime", stage)
			}
		})
	}
}

func TestCombinedReplaceFailureAfterNodesCommitAndSecretSafePersistence(t *testing.T) {
	fixture := newRestoreFixture(t)
	originalAppliance, originalNodes, originalOutbounds := authorityBytes(t, fixture)
	imported := cloneTestRegistry(t, fixture.registry)
	addNode(t, &imported, "node-22222222", "new.example.com")
	failed := false
	fixture.service = fixture.newService(func(stage Stage) error {
		if stage == StageNodesCommitted && !failed {
			failed = true
			return errors.New("synthetic node commit failure")
		}
		return nil
	})
	preview, err := fixture.service.Preview(context.Background(), "replace", ReplaceRegistry, bundleBytes(t, changedAppliance(fixture.appliance), &imported, true), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Apply(context.Background(), "replace", preview.Token); !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("combined failure = %v", err)
	}
	assertRestored(t, fixture, originalAppliance, originalNodes, originalOutbounds)
	entries, err := os.ReadDir(fixture.previousDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "04_outbounds.json" || strings.Contains(entry.Name(), "routing") || strings.Contains(entry.Name(), "policy") {
			t.Fatalf("previous generation contains generated material %q", entry.Name())
		}
	}
}

func TestInterruptedJournalStartupRecoveryAndFailedRecoveryRetention(t *testing.T) {
	fixture := newRestoreFixture(t)
	originalAppliance, originalNodes, originalOutbounds := authorityBytes(t, fixture)
	fixture.activator.restartErrs = []error{errors.New("recovery restart failure")}
	failed := false
	fixture.service = fixture.newService(func(stage Stage) error {
		if stage == StageGeneratedCommitted && !failed {
			failed = true
			return errors.New("power interruption injection")
		}
		return nil
	})
	preview, err := fixture.service.Preview(context.Background(), "crash", SettingsOnly, bundleBytes(t, changedAppliance(fixture.appliance), nil, false), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Apply(context.Background(), "crash", preview.Token); !errors.Is(err, ErrRecoveryFailed) {
		t.Fatalf("failed recovery result = %v", err)
	}
	journalPath := filepath.Join(fixture.stateDir, "appliance-import-transaction.json")
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"11111111-1111-4111-8111-111111111111", "AAAAAAAAAAAAAAAA", "synthetic-token", "VLESS"} {
		if bytes.Contains(journalBytes, []byte(marker)) {
			t.Fatalf("journal contains secret marker %q", marker)
		}
	}
	if err := NewService(Config{
		AppliancePath: fixture.appliancePath, NodesPath: fixture.nodesPath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, ActiveOutboundsPath: fixture.outboundsPath,
		PreviousDir: fixture.previousDir, StateDir: fixture.stateDir, Activator: &fakeActivator{},
		AuthorityLease: authority.NewLease(),
	}).Ready(); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("new service readiness with journal = %v", err)
	}
	recovered := NewService(Config{
		AppliancePath: fixture.appliancePath, NodesPath: fixture.nodesPath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, ActiveOutboundsPath: fixture.outboundsPath,
		PreviousDir: fixture.previousDir, StateDir: fixture.stateDir, Activator: &fakeActivator{},
		Coordinator: fixture.coordinator, AuthorityLease: authority.NewLease(),
	})
	if err := recovered.RecoverStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRestored(t, fixture, originalAppliance, originalNodes, originalOutbounds)
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered journal = %v", err)
	}
	if err := recovered.Ready(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedAuthorityLeaseSerializesBackupSnapshotAndRestorePreview(t *testing.T) {
	fixture := newRestoreFixture(t)
	releaseLease, err := fixture.lease.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := fixture.service.Preview(ctx, "busy", SettingsOnly, bundleBytes(t, fixture.appliance, nil, false), ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("preview while authority lease held = %v", err)
	}
	releaseLease()
	preview, err := fixture.service.Preview(context.Background(), "free", SettingsOnly, bundleBytes(t, fixture.appliance, nil, false), "")
	if err != nil || len(preview.Compatibility.Blockers) != 0 {
		t.Fatalf("preview after lease release = %+v, %v", preview, err)
	}
	if fixture.coordinator.begins != 0 {
		t.Fatal("restore Preview entered the runtime coordinator")
	}

	manager := nodes.NewManager(nodes.Config{
		Store: fixtureStore(fixture), AuthorityLease: fixture.lease,
	})
	backupService := backup.NewService(backup.Config{
		Appliance: staticAppliance{value: fixture.appliance}, Nodes: manager,
		AuthorityLease: fixture.lease,
		Build:          buildinfo.Info{Product: "xkeen-control", Version: "dev", SourceCommit: "dev", Channel: "development"},
		Now:            func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
		Random:         bytes.NewReader(bytes.Repeat([]byte{0x44}, 256)), GOOS: "linux", GOARCH: "arm64",
	})
	releaseLease, err = fixture.lease.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := backupService.ExportSecret(ctx, testPassphrase); !errors.Is(err, backup.ErrUnavailable) {
		t.Fatalf("secret snapshot while authority lease held = %v", err)
	}
	releaseLease()
}

func fixtureStore(fixture *restoreFixture) nodes.Store {
	return nodes.Store{Path: fixture.nodesPath}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, destination, contents)
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
