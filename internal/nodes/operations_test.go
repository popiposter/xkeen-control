package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type fakeFetcher struct {
	body []byte
	url  string
	err  error
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) ([]byte, error) {
	f.url = rawURL
	if f.err != nil {
		return nil, f.err
	}
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
	releaseGate, err := manager.authority.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = manager.Apply(context.Background(), "csrf", preview.Token, false)
	if err == nil || !strings.Contains(err.Error(), "node activation gate busy") {
		t.Fatalf("contended apply error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("gate contention was not bounded: %s", elapsed)
	}
	releaseGate()
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate rejection wrote registry: %v", err)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate rejection wrote active outbounds: %v", err)
	}
}

type gateRollbackBudgetActivator struct {
	validationRemaining time.Duration
	restartRemaining    []time.Duration
	restarts            int
}

func (a *gateRollbackBudgetActivator) ValidateCandidate(ctx context.Context, _ string) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("candidate validation deadline missing")
	}
	a.validationRemaining = time.Until(deadline)
	return nil
}
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
	manager.gateTimeout = 2 * time.Second
	activator := &gateRollbackBudgetActivator{}
	manager.tx.Activator = activator
	manager.tx.Budget = TransactionBudget{
		CandidateValidation: 2 * time.Second,
		Activation:          100 * time.Millisecond,
		Rollback:            300 * time.Millisecond,
		Total:               900 * time.Millisecond,
	}
	preview, err := manager.PreviewImport("csrf", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	releaseGate, err := manager.authority.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, applyErr := manager.Apply(context.Background(), "csrf", preview.Token, false)
		result <- applyErr
	}()
	// Keep the gate wait long enough that a transaction budget started before
	// gate acquisition would leave less than the rollback floor below, while a
	// correctly post-gate budget still has ample scheduler/filesystem margin.
	time.Sleep(750 * time.Millisecond)
	releaseGate()
	if err := <-result; err == nil || !strings.Contains(err.Error(), "previous generation restored") {
		t.Fatalf("activation rollback result = %v", err)
	}
	if activator.validationRemaining < 500*time.Millisecond {
		t.Fatalf("candidate validation received an exhausted transaction budget: %s", activator.validationRemaining)
	}
	if len(activator.restartRemaining) != 2 {
		t.Fatalf("restart calls = %d", len(activator.restartRemaining))
	}
}

func TestSubscriptionRefreshReconcilesExactMembership(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := ParseProfile(syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	otherProfile, err := ParseProfile(syntheticXHTTPFinalMaskProfile)
	if err != nil {
		t.Fatal(err)
	}
	newProfile := strings.NewReplacer(
		"22222222-2222-4222-8222-222222222222", "44444444-4444-4444-8444-444444444444",
		"edge-2.example.com", "edge-4.example.com",
		"front-2.example.com", "front-4.example.com",
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
		"beef", "d0d0",
		"Secondary", "Tertiary",
	).Replace(syntheticProfileTwo)
	const targetID = "sub-11111111"
	const otherSubscriptionID = "sub-22222222"
	matched, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: targetID}, "node-88888888")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := NewNodeWithID(secondary.VLESS, secondary.Name, Source{Type: "subscription", SubscriptionID: targetID}, "node-99999999")
	if err != nil {
		t.Fatal(err)
	}
	missing.Stale, missing.Missing = true, true
	other, err := NewNodeWithID(otherProfile.VLESS, otherProfile.Name, Source{Type: "subscription", SubscriptionID: otherSubscriptionID}, "node-77777777")
	if err != nil {
		t.Fatal(err)
	}
	other.Stale, other.Missing = true, true
	manual := testNode(t, syntheticProfile, "node-66666666", true)
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{
		{ID: targetID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true},
		{ID: otherSubscriptionID, Name: "Other", URL: "https://other.example/token", Enabled: true},
	}
	registry.Nodes = []Node{manual, matched, other, missing}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{body: []byte(strings.Join([]string{
		strings.Replace(syntheticProfile, "11111111-1111-4111-8111-111111111111", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 1),
		newProfile,
	}, "\n"))}
	manager, store, _ := testManager(t, &registry, fetcher)
	activator := &batchCountingActivator{}
	manager.tx.Activator = activator
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", targetID, "Provider renamed", "https://subscription.example/new-token")
	if err != nil {
		t.Fatal(err)
	}
	if preview.RequiresAcceptance || preview.Noop || len(preview.Changes) != 3 {
		t.Fatalf("exact refresh preview = %+v", preview)
	}
	removed := 0
	for _, change := range preview.Changes {
		if change.After == "removed" {
			removed++
			if change.ID != missing.ID || change.SourceType != "subscription" {
				t.Fatalf("unexpected removed change: %+v", change)
			}
		}
	}
	if removed != 1 {
		t.Fatalf("exact refresh removal count = %d", removed)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Nodes) != 4 || updated.Subscriptions[0].Name != "Provider renamed" || updated.Subscriptions[0].URL != "https://subscription.example/new-token" {
		t.Fatalf("exact refresh committed state = %+v", updated)
	}
	if !reflect.DeepEqual(updated.Nodes[0], manual) || updated.Nodes[1].ID != matched.ID || updated.Nodes[1].OutboundTag != matched.OutboundTag {
		t.Fatalf("unrelated or matched node placement changed: before=%+v after=%+v", registry.Nodes, updated.Nodes)
	}
	if updated.Nodes[1].VLESS.UUID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || updated.Nodes[1].Stale || updated.Nodes[1].Missing || !updated.Nodes[1].Enabled {
		t.Fatalf("matched node was not refreshed in place: %+v", updated.Nodes[1])
	}
	if !reflect.DeepEqual(updated.Nodes[2], other) || !updated.Nodes[2].Stale || !updated.Nodes[2].Missing {
		t.Fatalf("other subscription drifted: %+v", updated.Nodes[2])
	}
	if updated.Nodes[3].Source.SubscriptionID != targetID || !updated.Nodes[3].Enabled || updated.Nodes[3].Stale || updated.Nodes[3].Missing || updated.Nodes[3].VLESS.Host != "edge-4.example.com" {
		t.Fatalf("new target member = %+v", updated.Nodes[3])
	}
	if activator.validations != 1 || activator.restarts != 1 {
		t.Fatalf("exact refresh did not use one transaction boundary: %+v", activator)
	}
	public, err := manager.ListSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	serialized, _ := json.Marshal(public)
	if strings.Contains(string(serialized), "new-token") || strings.Contains(string(serialized), "subscription.example") {
		t.Fatal("subscription URL reached safe projection")
	}
}

func TestSubscriptionRefreshReorderedSnapshotIsNoop(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := ParseProfile(syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-12121212"
	first, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-12121212")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewNodeWithID(secondary.VLESS, secondary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-34343434")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{first, second}
	manager, store, active := testManager(t, &registry, &fakeFetcher{body: []byte(syntheticProfileTwo + "\n" + syntheticProfile)})
	activator := &batchCountingActivator{}
	manager.tx.Activator = activator
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "Provider", "https://subscription.example/token")
	if err != nil || !preview.Noop || len(preview.Changes) != 0 {
		t.Fatalf("reordered exact refresh = %+v, %v", preview, err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil || !sameRegistry(updated, registry) {
		t.Fatalf("reordered snapshot changed committed registry: %+v, %v", updated, err)
	}
	if activator.validations != 0 || activator.restarts != 0 {
		t.Fatalf("no-op exact refresh entered transaction: %+v", activator)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no-op exact refresh wrote active outbounds: %v", err)
	}
}

func TestSubscriptionRefreshConvergesLegacyStaleMembers(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := ParseProfile(syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-56565656"
	present, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-56565656")
	if err != nil {
		t.Fatal(err)
	}
	present.Stale, present.Missing = true, true
	absent, err := NewNodeWithID(secondary.VLESS, secondary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-78787878")
	if err != nil {
		t.Fatal(err)
	}
	absent.Stale, absent.Missing = true, true
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{present, absent}
	manager, store, _ := testManager(t, &registry, &fakeFetcher{body: []byte(syntheticProfile)})
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", "")
	if err != nil || preview.RequiresAcceptance || preview.Noop {
		t.Fatalf("legacy convergence preview = %+v, %v", preview, err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil || len(updated.Nodes) != 1 || updated.Nodes[0].ID != present.ID || updated.Nodes[0].Stale || updated.Nodes[0].Missing {
		t.Fatalf("legacy stale/missing convergence = %+v, %v", updated, err)
	}
}

func TestDisabledSubscriptionRefreshKeepsResultingMembersDisabled(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-90909090"
	present, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-90909090")
	if err != nil {
		t.Fatal(err)
	}
	present.Enabled = false
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Disabled provider", URL: "https://subscription.example/token", Enabled: false}}
	registry.Nodes = []Node{present}
	newProfile := strings.Replace(strings.Replace(syntheticProfileTwo, "22222222-2222-4222-8222-222222222222", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", 1), "Secondary", "Disabled new", 1)
	manager, store, _ := testManager(t, &registry, &fakeFetcher{body: []byte(syntheticProfile + "\n" + newProfile)})
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", "")
	if err != nil || preview.Noop {
		t.Fatalf("disabled refresh preview = %+v, %v", preview, err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil || len(updated.Nodes) != 2 || updated.Subscriptions[0].Enabled || updated.Nodes[0].Enabled || updated.Nodes[1].Enabled {
		t.Fatalf("disabled subscription members were enabled: %+v, %v", updated, err)
	}
}

func TestSubscriptionRefreshFailuresPreserveCommittedRegistry(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-31313131"
	node, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-31313131")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	fetcher := &fakeFetcher{}
	manager, store, _ := testManager(t, &registry, fetcher)
	cases := []struct {
		name string
		body []byte
		err  error
		want error
	}{
		{name: "fetch", err: errors.New("synthetic fetch failure"), want: ErrSubscriptionFetch},
		{name: "empty", body: []byte("\n"), want: ErrSubscriptionContent},
		{name: "unsupported", body: []byte("https://not-a-vless-profile.example"), want: ErrSubscriptionContent},
		{name: "duplicate", body: []byte(syntheticProfile + "\n" + syntheticProfile), want: ErrSubscriptionDuplicate},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fetcher.body, fetcher.err = test.body, test.err
			_, gotErr := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "Replacement name", "https://subscription.example/replacement-token")
			if !errors.Is(gotErr, test.want) {
				t.Fatalf("refresh error = %v, want %v", gotErr, test.want)
			}
			got, err := store.Load()
			if err != nil || !sameRegistry(got, registry) {
				t.Fatalf("failed refresh changed committed registry: %+v, %v", got, err)
			}
			if len(manager.previews) != 0 {
				t.Fatalf("failed refresh created preview: %d", len(manager.previews))
			}
		})
	}
}

func TestSubscriptionRefreshRejectsRegistryOverflowBeforePreview(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-41414141"
	subscriptionNode, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-aaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = append(registry.Nodes, subscriptionNode)
	for index := 0; index < MaxNodes-1; index++ {
		registry.Nodes = append(registry.Nodes, testNode(t, syntheticProfile, fmt.Sprintf("node-%08x", index), true))
	}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	newProfile := strings.Replace(strings.Replace(syntheticProfileTwo, "22222222-2222-4222-8222-222222222222", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", 1), "Secondary", "New one", 1)
	newProfileTwo := strings.Replace(strings.Replace(syntheticXHTTPFinalMaskProfile, "33333333-3333-4333-8333-333333333333", "ffffffff-ffff-4fff-8fff-ffffffffffff", 1), "Germany XHTTP", "New two", 1)
	manager, store, _ := testManager(t, &registry, &fakeFetcher{body: []byte(newProfile + "\n" + newProfileTwo)})
	if _, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", ""); !errors.Is(err, ErrPreviewCandidate) {
		t.Fatalf("overflow refresh error = %v", err)
	}
	got, err := store.Load()
	if err != nil || !sameRegistry(got, registry) {
		t.Fatalf("overflow refresh changed committed registry: %+v, %v", got, err)
	}
	if len(manager.previews) != 0 {
		t.Fatalf("overflow refresh created preview: %d", len(manager.previews))
	}
}

func TestSubscriptionRefreshApplyValidationFailureConsumesTokenAndPreservesState(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-51515151"
	node, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-51515151")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	manager, store, active := testManager(t, &registry, &fakeFetcher{body: []byte(syntheticProfileTwo)})
	manager.tx.Activator = &fakeActivator{validateErr: errors.New("synthetic candidate rejection")}
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err == nil || !strings.Contains(err.Error(), "candidate Xray validation failed") {
		t.Fatalf("candidate validation failure = %v", err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("failed apply token remained usable: %v", err)
	}
	got, err := store.Load()
	if err != nil || !sameRegistry(got, registry) {
		t.Fatalf("candidate validation failure changed registry: %+v, %v", got, err)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate validation failure wrote active outbounds: %v", err)
	}
}

func TestSubscriptionRefreshApplyRejectsStaleBaseBeforeTransaction(t *testing.T) {
	primary, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	const subscriptionID = "sub-61616161"
	node, err := NewNodeWithID(primary.VLESS, primary.Name, Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-61616161")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{node}
	manager, store, _ := testManager(t, &registry, &fakeFetcher{body: []byte(syntheticProfileTwo)})
	activator := &batchCountingActivator{}
	manager.tx.Activator = activator
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	intervening := registry
	intervening.Subscriptions[0].Name = "Intervening update"
	if err := store.Save(intervening); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("stale refresh apply = %v", err)
	}
	if activator.validations != 0 || activator.restarts != 0 {
		t.Fatalf("stale refresh entered transaction: %+v", activator)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("stale refresh token remained usable: %v", err)
	}
}

func TestSubscriptionRefreshAppendsNewMembersBySourceKey(t *testing.T) {
	firstProfile := strings.Replace(strings.Replace(syntheticProfileTwo, "22222222-2222-4222-8222-222222222222", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", 1), "Secondary", "Zulu", 1)
	secondProfile := strings.Replace(strings.Replace(syntheticXHTTPFinalMaskProfile, "33333333-3333-4333-8333-333333333333", "ffffffff-ffff-4fff-8fff-ffffffffffff", 1), "Germany XHTTP", "Alpha", 1)
	const subscriptionID = "sub-71717171"
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: subscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	manager, store, _ := testManager(t, &registry, &fakeFetcher{body: []byte(secondProfile + "\n" + firstProfile)})
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", subscriptionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Changes) != 2 {
		t.Fatalf("new-member preview = %+v", preview)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load()
	if err != nil || len(updated.Nodes) != 2 {
		t.Fatalf("new-member registry = %+v, %v", updated, err)
	}
	keys := []string{updated.Nodes[0].SourceKey, updated.Nodes[1].SourceKey}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("new target members were not appended in source-key order: %v", keys)
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
