package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeFetcher struct {
	body []byte
	url  string
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) ([]byte, error) {
	f.url = rawURL
	return append([]byte(nil), f.body...), nil
}

func testManager(t *testing.T, registry *Registry, fetcher SubscriptionFetcher) (*Manager, Store, string) {
	t.Helper()
	dir := t.TempDir()
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	active := filepath.Join(dir, "xray", "04_outbounds.json")
	if registry != nil {
		if err := store.Save(*registry); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewManager(Config{
		Store:   store,
		Fetcher: fetcher,
		Transaction: Transaction{
			Store: store, ActiveOutboundsPath: active, PreviousDir: filepath.Join(dir, "previous"),
		},
	})
	return manager, store, active
}

func TestPreviewIsSessionBoundOneShotAndDoesNotWrite(t *testing.T) {
	manager, store, active := testManager(t, nil, nil)
	preview, err := manager.PreviewImport("csrf-a", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(preview)
	if strings.Contains(string(encoded), "11111111-1111-4111-8111-111111111111") || strings.Contains(string(encoded), "edge.example.com") {
		t.Fatal("preview exposed profile secret/endpoint")
	}
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote registry")
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote active outbounds")
	}
	if _, err := manager.Apply(context.Background(), "wrong-session", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("wrong session apply = %v", err)
	}
	if _, err := manager.Apply(context.Background(), "csrf-a", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf-a", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("second apply = %v", err)
	}
	preview, err = manager.PreviewImport("csrf-a", syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	manager.Invalidate("csrf-a")
	if _, err := manager.Apply(context.Background(), "csrf-a", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("logout invalidation = %v", err)
	}
}

func TestApplyRejectsContendedGateWithinSeparateBound(t *testing.T) {
	manager, store, active := testManager(t, nil, nil)
	manager.gateTimeout = 20 * time.Millisecond
	preview, err := manager.PreviewImport("csrf", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	manager.applyGate <- struct{}{}
	started := time.Now()
	_, err = manager.Apply(context.Background(), "csrf", preview.Token, false)
	if err == nil || !strings.Contains(err.Error(), "node activation gate busy") {
		t.Fatalf("contended apply error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("gate contention was not bounded: %s", elapsed)
	}
	<-manager.applyGate
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate rejection wrote registry: %v", err)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate rejection wrote active outbounds: %v", err)
	}
}

type gateRollbackBudgetActivator struct {
	restartRemaining []time.Duration
	restarts         int
}

func (*gateRollbackBudgetActivator) ValidateCandidate(context.Context, string) error { return nil }
func (a *gateRollbackBudgetActivator) Restart(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	a.restartRemaining = append(a.restartRemaining, time.Until(deadline))
	a.restarts++
	if a.restarts == 1 {
		return errors.New("synthetic activation failure")
	}
	return nil
}
func (*gateRollbackBudgetActivator) WaitReady(context.Context) error                    { return nil }
func (*gateRollbackBudgetActivator) VerifyOutboundTags(context.Context, []string) error { return nil }

func TestApplyGateWaitDoesNotConsumeRollbackBudget(t *testing.T) {
	manager, _, _ := testManager(t, nil, nil)
	manager.gateTimeout = 500 * time.Millisecond
	activator := &gateRollbackBudgetActivator{}
	manager.tx.Activator = activator
	manager.tx.Budget = TransactionBudget{
		CandidateValidation: 10 * time.Millisecond,
		Activation:          40 * time.Millisecond,
		Rollback:            100 * time.Millisecond,
		Total:               140 * time.Millisecond,
	}
	preview, err := manager.PreviewImport("csrf", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	manager.applyGate <- struct{}{}
	result := make(chan error, 1)
	go func() {
		_, applyErr := manager.Apply(context.Background(), "csrf", preview.Token, false)
		result <- applyErr
	}()
	time.Sleep(100 * time.Millisecond)
	<-manager.applyGate
	if err := <-result; err == nil || !strings.Contains(err.Error(), "previous generation restored") {
		t.Fatalf("activation rollback result = %v", err)
	}
	if len(activator.restartRemaining) != 2 {
		t.Fatalf("restart calls = %d", len(activator.restartRemaining))
	}
	if activator.restartRemaining[1] < 70*time.Millisecond {
		t.Fatalf("rollback reserve was consumed by gate wait: %s", activator.restartRemaining[1])
	}
}

func TestSubscriptionRefreshPreservesIdentityAndRequiresMissingAcceptance(t *testing.T) {
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-11111111"
	node, err := NewNodeWithID(parsed.VLESS, parsed.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-88888888")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	fetcher := &fakeFetcher{body: []byte(syntheticProfileTwo)}
	manager, store, _ := testManager(t, &registry, fetcher)
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "Provider", "https://subscription.example/new-token")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.RequiresAcceptance {
		t.Fatal("missing provider node did not require explicit acceptance")
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrMissingAcceptance) {
		t.Fatalf("missing acceptance = %v", err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, true); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Nodes) != 2 || !updated.Nodes[0].Missing {
		t.Fatalf("stale node was not preserved: %+v", updated.Nodes)
	}
	if updated.Subscriptions[0].URL != "https://subscription.example/new-token" {
		t.Fatal("subscription source was not updated in local registry")
	}
	public := updated.PublicNodes()
	serialized, _ := json.Marshal(public)
	if strings.Contains(string(serialized), "new-token") {
		t.Fatal("subscription URL reached safe projection")
	}
}

func TestSubscriptionUpdatePreservesIDAndTag(t *testing.T) {
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-22222222"
	node, err := NewNodeWithID(parsed.VLESS, parsed.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-99999999")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	changedUUID := strings.Replace(syntheticProfile, "11111111-1111-4111-8111-111111111111", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 1)
	manager, _, _ := testManager(t, &registry, &fakeFetcher{body: []byte(changedUUID)})
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "Provider", "https://subscription.example/token")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Before != "enabled" || preview.Changes[0].After != "enabled" {
		t.Fatalf("unexpected update diff: %+v", preview.Changes)
	}
	result, err := manager.Apply(context.Background(), "csrf", preview.Token, false)
	if err != nil || len(result.Nodes) != 1 || result.Nodes[0].ID != "node-99999999" || result.Nodes[0].OutboundTag != "proxy-node-99999999" {
		t.Fatalf("identity changed during replacement: %+v, %v", result, err)
	}
}

func TestSubscriptionLifecycleDisablesEnablesAndRemovesNodes(t *testing.T) {
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-44444444"
	node, err := NewNodeWithID(parsed.VLESS, parsed.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-44444444")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Travel", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	manager, store, _ := testManager(t, &registry, nil)

	preview, err := manager.PreviewSubscriptionState("csrf", subscriptionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil || updated.Subscriptions[0].Enabled || updated.Nodes[0].Enabled {
		t.Fatalf("disabled subscription state = %+v err=%v", updated, err)
	}

	preview, err = manager.PreviewSubscriptionState("csrf", subscriptionID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err = store.Load()
	if err != nil || !updated.Subscriptions[0].Enabled || !updated.Nodes[0].Enabled {
		t.Fatalf("enabled subscription state = %+v err=%v", updated, err)
	}

	preview, err = manager.PreviewSubscriptionRemove("csrf", subscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err = store.Load()
	if err != nil || len(updated.Subscriptions) != 0 || len(updated.Nodes) != 0 {
		t.Fatalf("removed subscription state = %+v err=%v", updated, err)
	}
}

func TestSubscriptionIdentityUsesProviderNameButNotUUID(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	changedUUID, err := ParseProfile(strings.Replace(syntheticProfile, "11111111-1111-4111-8111-111111111111", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 1))
	if err != nil {
		t.Fatal(err)
	}
	if subscriptionSourceKey(primary.VLESS, "Sweden TCP") != subscriptionSourceKey(changedUUID.VLESS, "Sweden TCP") {
		t.Fatal("UUID rotation changed subscription identity")
	}
	if subscriptionSourceKey(primary.VLESS, "Sweden TCP") == subscriptionSourceKey(primary.VLESS, "Sweden XHTTP") {
		t.Fatal("distinct provider names collapsed onto one subscription identity")
	}
}

func TestExistingSubscriptionRefreshUsesStoredSecretURL(t *testing.T) {
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-33333333"
	node, err := NewNodeWithID(parsed.VLESS, parsed.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-77777777")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	fetcher := &fakeFetcher{body: []byte(syntheticProfile)}
	manager, _, _ := testManager(t, &registry, fetcher)
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.url != "https://subscription.example/token" || !preview.Noop || len(preview.Changes) != 0 {
		t.Fatalf("stored subscription refresh = url-used:%t preview:%+v", fetcher.url == "https://subscription.example/token", preview)
	}
	public, err := manager.ListSubscriptions()
	if err != nil || len(public) != 1 || public[0].NodeCount != 1 {
		t.Fatalf("public subscriptions = %+v, %v", public, err)
	}
	serialized, _ := json.Marshal(public)
	if strings.Contains(string(serialized), "token") || strings.Contains(string(serialized), "subscription.example") {
		t.Fatal("public subscription exposed secret URL")
	}
}

func TestPreviewExpiry(t *testing.T) {
	now := time.Now()
	manager, _, _ := testManager(t, nil, nil)
	manager.now = func() time.Time { return now }
	manager.ttl = time.Second
	preview, err := manager.PreviewImport("csrf", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now.Add(2 * time.Second) }
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("expired preview = %v", err)
	}
	if len(manager.previews) != 0 {
		t.Fatalf("expired preview was retained: %d", len(manager.previews))
	}
}

func TestPreviewStoreIsOnePerBindingAndGloballyBounded(t *testing.T) {
	now := time.Now()
	manager, _, _ := testManager(t, nil, nil)
	manager.now = func() time.Time { return now }
	manager.maxPreviews = 2

	first, err := manager.PreviewImport("session-a", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	replacement, err := manager.PreviewImport("session-a", syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.previews[first.Token]; ok {
		t.Fatal("new preview did not invalidate the previous token for its binding")
	}
	now = now.Add(time.Second)
	second, err := manager.PreviewImport("session-b", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	third, err := manager.PreviewImport("session-c", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if len(manager.previews) != 2 {
		t.Fatalf("preview cardinality = %d, want 2", len(manager.previews))
	}
	if _, ok := manager.previews[replacement.Token]; ok {
		t.Fatal("global cap did not evict the oldest preview")
	}
	if _, ok := manager.previews[second.Token]; !ok {
		t.Fatal("newer preview was unexpectedly evicted")
	}
	manager.Cancel("session-c", third.Token)
	if _, ok := manager.previews[third.Token]; ok {
		t.Fatal("explicit preview cancellation did not release the entry")
	}
}

func TestRandomSubscriptionIDsAlwaysValidate(t *testing.T) {
	for range 128 {
		id, err := randomSubscriptionID()
		if err != nil || !validSubscriptionID(id) {
			t.Fatalf("invalid generated subscription id %q: %v", id, err)
		}
	}
}
