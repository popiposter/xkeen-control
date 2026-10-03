package nodes

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestNodeCommitCheckPreventsRestartAndConsumesPreview(t *testing.T) {
	manager, store, active := testManager(t, nil, nil)
	preview, err := manager.PreviewImport("csrf", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	manager.beforeCommit = func(ctx context.Context) error {
		_, release, err := manager.authority.TryAcquireContext(ctx)
		if err == nil {
			release()
			t.Fatal("commit check did not own authority lease")
		}
		return errors.New("synthetic saved config pending")
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrOperationUnavailable) {
		t.Fatal(err)
	}
	for _, path := range []string{store.Path, active} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("commit check permitted a write", err)
		}
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatal("blocked token replayed", err)
	}
}
func TestAutomaticRefreshDefersPendingConfigWithoutWriting(t *testing.T) {
	registry := refresherRegistry(t, true)
	manager, store, active := testManager(t, &registry, &countingSubscriptionFetcher{body: []byte(syntheticProfileTwo)})
	manager.managedCoordinator = &managedRefreshCoordinator{}
	manager.beforeCommit = func(context.Context) error { return errors.New("synthetic saved config pending") }
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	refresher := newSubscriptionRefresher(manager, func() time.Time { return now })
	if err := refresher.reconcile(now); err != nil {
		t.Fatal(err)
	}
	refresher.entries["sub-12345678"].nextRunAt = now
	refresher.runAttempt(context.Background(), "sub-12345678")
	status := refresher.AutoRefreshStatuses()["sub-12345678"]
	if status.State != autoRefreshDeferred || status.ErrorCode != autoRefreshRuntime {
		t.Fatalf("%+v", status)
	}
	current, err := store.Load()
	if err != nil || !sameRegistry(current, registry) {
		t.Fatal("pending config allowed registry commit", err)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pending config allowed outbounds commit", err)
	}
}
