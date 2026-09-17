package nodes

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

type managedRefreshCoordinator struct {
	tryCalls int
	releases int
	tryErr   error
}

func (c *managedRefreshCoordinator) TryBeginManagedApply() (func(), error) {
	c.tryCalls++
	if c.tryErr != nil {
		return nil, c.tryErr
	}
	return func() { c.releases++ }, nil
}

type countingSubscriptionFetcher struct {
	body      []byte
	url       string
	calls     int
	entered   chan struct{}
	cancelled chan struct{}
}

func (f *countingSubscriptionFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	f.calls++
	f.url = rawURL
	if f.entered != nil {
		select {
		case <-f.entered:
		default:
			close(f.entered)
		}
		<-ctx.Done()
		if f.cancelled != nil {
			close(f.cancelled)
		}
		return nil, ctx.Err()
	}
	return append([]byte(nil), f.body...), nil
}

type mutatingSubscriptionFetcher struct {
	store    Store
	registry Registry
	body     []byte
	calls    int
}

func (f *mutatingSubscriptionFetcher) Fetch(context.Context, string) ([]byte, error) {
	f.calls++
	if err := f.store.Save(f.registry); err != nil {
		return nil, err
	}
	return append([]byte(nil), f.body...), nil
}

func refresherRegistry(t *testing.T, enabled bool) Registry {
	t.Helper()
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewNodeWithID(parsed.VLESS, parsed.Name, Source{Type: "subscription", SubscriptionID: "sub-12345678"}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	return Registry{
		SchemaVersion: SchemaVersion,
		Nodes:         []Node{node},
		Subscriptions: []Subscription{{ID: "sub-12345678", Name: "Provider", URL: "https://subscription.example/token", Enabled: enabled}},
	}
}

func TestSubscriptionRefresherStartupAndRescanTiming(t *testing.T) {
	registry := refresherRegistry(t, true)
	manager, store, _ := testManager(t, &registry, &countingSubscriptionFetcher{body: []byte(syntheticProfile)})
	now := time.Date(2026, 9, 17, 12, 0, 0, 123, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	entry := refresher.entries["sub-12345678"]
	if entry == nil || entry.status.state != autoRefreshWaiting {
		t.Fatalf("startup entry = %+v", entry)
	}
	wantJitter := subscriptionRefreshJitter("sub-12345678")
	if wantJitter < 0 || wantJitter >= DefaultSubscriptionRefreshJitter || entry.nextRunAt.Sub(now) != DefaultSubscriptionRefreshStartupWait+wantJitter {
		t.Fatalf("startup due = %s jitter=%s", entry.nextRunAt.Sub(now), wantJitter)
	}
	fetcher := manager.fetcher.(*countingSubscriptionFetcher)
	if fetcher.calls != 0 {
		t.Fatal("initial rescan fetched a provider")
	}
	if got := refresher.AutoRefreshStatuses()["sub-12345678"]; got.NextRunAt == "" || got.State != autoRefreshWaiting {
		t.Fatalf("startup status = %+v", got)
	}

	updated := registry
	second := updated.Subscriptions[0]
	second.ID = "sub-87654321"
	second.Name = "Second provider"
	updated.Subscriptions = append(updated.Subscriptions, second)
	if err := store.Save(updated); err != nil {
		t.Fatal(err)
	}
	scanTime := now.Add(DefaultSubscriptionRefreshRescan)
	if err := refresher.reconcile(scanTime); err != nil {
		t.Fatal(err)
	}
	newEntry := refresher.entries[second.ID]
	if newEntry == nil || newEntry.nextRunAt.Sub(scanTime) != DefaultSubscriptionRefreshCadence {
		t.Fatalf("newly discovered subscription due = %+v", newEntry)
	}
}

func TestSubscriptionRefresherDisabledAndReenabledUsesStartupDelay(t *testing.T) {
	registry := refresherRegistry(t, false)
	manager, store, _ := testManager(t, &registry, &countingSubscriptionFetcher{body: []byte(syntheticProfile)})
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	entry := refresher.entries["sub-12345678"]
	if entry == nil || entry.status.state != autoRefreshDisabled || !entry.nextRunAt.IsZero() {
		t.Fatalf("disabled entry = %+v", entry)
	}

	registry.Subscriptions[0].Enabled = true
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	enabledAt := now.Add(DefaultSubscriptionRefreshRescan)
	if err := refresher.reconcile(enabledAt); err != nil {
		t.Fatal(err)
	}
	entry = refresher.entries["sub-12345678"]
	want := DefaultSubscriptionRefreshStartupWait + subscriptionRefreshJitter("sub-12345678")
	if entry.status.state != autoRefreshWaiting || entry.nextRunAt.Sub(enabledAt) != want {
		t.Fatalf("re-enabled entry = %+v want delay %s", entry, want)
	}
}

func TestAutomaticRefreshUsesSavedURLAndManagedAdmission(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{body: []byte(syntheticProfileTwo)}
	manager, store, active := testManager(t, &registry, fetcher)
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	if fetcher.url != "https://subscription.example/token" || fetcher.calls != 1 {
		t.Fatalf("automatic fetch = calls:%d url:%q", fetcher.calls, fetcher.url)
	}
	if coordinator.tryCalls != 1 || coordinator.releases != 1 {
		t.Fatalf("managed admission = calls:%d releases:%d", coordinator.tryCalls, coordinator.releases)
	}
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshWaiting || status.LastResult != autoRefreshUpdated || status.ErrorCode != "" {
		t.Fatalf("updated status = %+v", status)
	}
	updated, err := store.Load()
	if err != nil || len(updated.Nodes) != 1 || sameRegistry(updated, registry) {
		t.Fatalf("automatic candidate was not committed: %+v err=%v", updated, err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("automatic activation did not write generated outbounds: %v", err)
	}
}

func TestAutomaticRefreshNoopDoesNotAdmitOrWrite(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{body: []byte(syntheticProfile)}
	manager, _, active := testManager(t, &registry, fetcher)
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	if coordinator.tryCalls != 0 {
		t.Fatal("no-op automatic refresh entered managed admission")
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no-op automatic refresh wrote generated outbounds: %v", err)
	}
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshWaiting || status.LastResult != autoRefreshNoop || status.LastSuccessAt == "" || status.NextRunAt == "" {
		t.Fatalf("no-op status = %+v", status)
	}
}

func TestAutomaticRefreshDefersForLivePreviewAndPreservesOperatorToken(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{body: []byte(syntheticProfileTwo)}
	manager, store, active := testManager(t, &registry, fetcher)
	preview, err := manager.PreviewRefresh(context.Background(), "operator-session", "sub-12345678", "", "")
	if err != nil || preview.Noop {
		t.Fatalf("operator preview = %+v, %v", preview, err)
	}
	manager.mu.Lock()
	manager.previews["expired-preview"] = previewEntry{ExpiresAt: time.Now().UTC().Add(-time.Second)}
	manager.mu.Unlock()
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshDeferred || status.ErrorCode != autoRefreshPreview || coordinator.tryCalls != 0 {
		t.Fatalf("live-preview automatic status = %+v admissions=%d", status, coordinator.tryCalls)
	}
	manager.mu.Lock()
	_, tokenStillLive := manager.previews[preview.Token]
	_, expiredStillLive := manager.previews["expired-preview"]
	manager.mu.Unlock()
	if !tokenStillLive || expiredStillLive {
		t.Fatalf("preview store after automatic deferral: token=%v expired=%v", tokenStillLive, expiredStillLive)
	}
	committed, err := store.Load()
	if err != nil || !sameRegistry(committed, registry) {
		t.Fatalf("automatic refresh mutated around live preview: %+v err=%v", committed, err)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("automatic refresh wrote active outbounds around live preview: %v", err)
	}
	if _, err := manager.Apply(context.Background(), "operator-session", preview.Token, false); err != nil {
		t.Fatalf("operator preview became unusable after automatic deferral: %v", err)
	}
	committed, err = store.Load()
	if err != nil || sameRegistry(committed, registry) {
		t.Fatalf("operator preview did not commit after automatic deferral: %+v err=%v", committed, err)
	}
}

func TestAutomaticRefreshBusyBackoffIsFinite(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{body: []byte(syntheticProfileTwo)}
	manager, _, _ := testManager(t, &registry, fetcher)
	coordinator := &managedRefreshCoordinator{tryErr: errors.New("synthetic runtime busy")}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	entry := refresher.entries["sub-12345678"]
	entry.nextRunAt = now
	for index, want := range subscriptionRefreshBusyBackoff {
		refresher.runAttempt(context.Background(), "sub-12345678")
		status := refresher.AutoRefreshStatuses()["sub-12345678"]
		if status.State != autoRefreshDeferred || status.ErrorCode != autoRefreshRuntime {
			t.Fatalf("retry %d status = %+v", index+1, status)
		}
		if entry.nextRunAt.Sub(now) != want {
			t.Fatalf("retry %d delay = %s want %s", index+1, entry.nextRunAt.Sub(now), want)
		}
		now = entry.nextRunAt
	}
	refresher.runAttempt(context.Background(), "sub-12345678")
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshFailed || status.ErrorCode != autoRefreshRuntime || entry.nextRunAt.Sub(now) != DefaultSubscriptionRefreshCadence || entry.retry != 0 {
		t.Fatalf("terminal busy status = %+v entry=%+v", status, entry)
	}
}

func TestAutomaticRefreshRechecksDisabledBeforeFetch(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{body: []byte(syntheticProfileTwo)}
	manager, store, _ := testManager(t, &registry, fetcher)
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	registry.Subscriptions[0].Enabled = false
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	if fetcher.calls != 0 || coordinator.tryCalls != 0 {
		t.Fatalf("disabled subscription was fetched/admitted: calls=%d admissions=%d", fetcher.calls, coordinator.tryCalls)
	}
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshDisabled || status.NextRunAt != "" {
		t.Fatalf("disabled status = %+v", status)
	}
}

func TestAutomaticRefreshDefersWhenAuthorityIsBusyAndReleasesCoordinator(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{body: []byte(syntheticProfileTwo)}
	manager, _, _ := testManager(t, &registry, fetcher)
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	owned, err := manager.authority.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer owned()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshDeferred || status.ErrorCode != autoRefreshAuthority || coordinator.tryCalls != 1 || coordinator.releases != 1 {
		t.Fatalf("authority busy status=%+v admissions=%d releases=%d", status, coordinator.tryCalls, coordinator.releases)
	}
}

func TestAutomaticRefreshRejectsStaleBaseBeforeAdmission(t *testing.T) {
	registry := refresherRegistry(t, true)
	drifted, err := cloneRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	drifted.Subscriptions[0].Name = "Intervening provider"
	fetcher := &mutatingSubscriptionFetcher{registry: drifted, body: []byte(syntheticProfileTwo)}
	manager, store, _ := testManager(t, &registry, fetcher)
	fetcher.store = store
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshDeferred || status.ErrorCode != autoRefreshStale || coordinator.tryCalls != 0 {
		t.Fatalf("stale status=%+v admissions=%d", status, coordinator.tryCalls)
	}
	committed, err := store.Load()
	if err != nil || committed.Subscriptions[0].Name != "Intervening provider" {
		t.Fatalf("intervening registry was not preserved: %+v err=%v", committed, err)
	}
}

func TestAutomaticRefreshRechecksDisableAfterFetch(t *testing.T) {
	registry := refresherRegistry(t, true)
	disabled, err := cloneRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	disabled.Subscriptions[0].Enabled = false
	fetcher := &mutatingSubscriptionFetcher{registry: disabled, body: []byte(syntheticProfileTwo)}
	manager, store, _ := testManager(t, &registry, fetcher)
	fetcher.store = store
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if fetcher.calls != 1 || status.State != autoRefreshDisabled || coordinator.tryCalls != 0 {
		t.Fatalf("post-fetch disable status=%+v calls=%d admissions=%d", status, fetcher.calls, coordinator.tryCalls)
	}
}

func TestSubscriptionRefresherStopCancelsInFlightFetch(t *testing.T) {
	registry := refresherRegistry(t, true)
	fetcher := &countingSubscriptionFetcher{entered: make(chan struct{}), cancelled: make(chan struct{})}
	manager, _, _ := testManager(t, &registry, fetcher)
	coordinator := &managedRefreshCoordinator{}
	manager.managedCoordinator = coordinator
	refresher := NewSubscriptionRefresher(manager)
	now := time.Now().UTC()
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now.Add(-time.Second)
	refresher.Start(context.Background())
	select {
	case <-fetcher.entered:
	case <-time.After(time.Second):
		refresher.Stop()
		t.Fatal("refresher did not start the due attempt")
	}
	refresher.Stop()
	select {
	case <-fetcher.cancelled:
	default:
		t.Fatal("Stop did not cancel the in-flight fetch")
	}
	if coordinator.tryCalls != 0 {
		t.Fatal("canceled fetch reached managed admission")
	}
}

func TestSubscriptionRefreshJitterUsesOnlyStableID(t *testing.T) {
	ids := []string{"sub-11111111", "sub-22222222", "sub-33333333", "sub-44444444"}
	seen := make(map[time.Duration]struct{})
	for _, id := range ids {
		first := subscriptionRefreshJitter(id)
		if first < 0 || first >= DefaultSubscriptionRefreshJitter || first != subscriptionRefreshJitter(id) {
			t.Fatalf("unstable/out-of-range jitter for %q: %s", id, first)
		}
		seen[first] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("stable ID jitter did not distribute synthetic IDs: %v", seen)
	}
}
