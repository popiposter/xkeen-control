package nativequality

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsDefaultOnMissingOrInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "maintenance.json")
	if got := NewSettingsStore(path).Get(); got != DefaultSettings() {
		t.Fatal("missing file", got)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"{broken", `{"window":"4:00"}`, `{"unknown":1}`, `{"poolSize":9}`, `{} {}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := NewSettingsStore(path).Get(); got != DefaultSettings() {
			t.Fatal("invalid file not defaulted", body, got)
		}
	}
	// A partial file keeps the defaults for absent fields.
	if err := os.WriteFile(path, []byte(`{"window":"02:15","poolSize":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := NewSettingsStore(path).Get()
	if got.Window != "02:15" || got.PoolSize != 4 || !got.Enabled || got.SpeedCandidates != MaxSpeedCandidates {
		t.Fatal("partial file", got)
	}
}

func TestSettingsSaveIsPrivateAndWakesTheSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "maintenance.json")
	store := NewSettingsStore(path)
	value := DefaultSettings()
	value.Window, value.SpeedCandidates, value.PoolSize = "01:05", 8, 3
	if _, err := store.Set(value); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.Changes():
	default:
		t.Fatal("save did not signal the schedule")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("settings file not private", err)
	}
	if NewSettingsStore(path).Get() != value {
		t.Fatal("not persisted")
	}
	value.PoolSize = 9
	if _, err := store.Set(value); err == nil {
		t.Fatal("out-of-bounds pool size saved")
	}
	value.PoolSize, value.SpeedCandidates = 6, 4
	if _, err := store.Set(value); err == nil {
		t.Fatal("fewer candidates than the pool saved")
	}
	if d, ok := ParseWindow("23:59"); !ok || d != 23*time.Hour+59*time.Minute {
		t.Fatal("window parse")
	}
}

// Steps the operator turned off are skipped; the schedule follows the
// configured time and plans nothing while the window is off.
func TestNightFollowsSettings(t *testing.T) {
	store := NewSettingsStore(filepath.Join(t.TempDir(), "maintenance.json"))
	value := DefaultSettings()
	value.Window, value.RefreshSubscriptions, value.Review, value.UpdateGeodata, value.Report = "02:30", false, false, false, false
	if _, err := store.Set(value); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Settings: store}
	s := NewSchedule(svc)
	s.settle = 0
	zone := time.FixedZone("router", 3*3600)
	s.Location = func() *time.Location { return zone }
	if got := s.NextWindow(time.Date(2026, 10, 11, 1, 0, 0, 0, zone)); !got.Equal(time.Date(2026, 10, 11, 2, 30, 0, 0, zone)) {
		t.Fatal("configured window ignored", got)
	}
	if !s.NextSubscriptionRefresh(time.Now()).IsZero() {
		t.Fatal("subscriptions planned with the step off")
	}
	calls := map[string]int{}
	s.Subscriptions = func(context.Context) { calls["subscriptions"]++ }
	s.Geodata = func(context.Context) (string, error) { calls["geodata"]++; return "completed", nil }
	s.start = func(context.Context, bool) (<-chan struct{}, error) { calls["review"]++; return nil, ErrUnavailable }
	s.Finished = func(context.Context, MaintenanceStatus, Status) { calls["report"]++ }
	s.runNight(context.Background())
	m := svc.maintenance
	if len(calls) != 0 || m.SubscriptionsRefreshed || m.ReviewStarted || m.ReviewReason != "review-disabled" || m.GeodataState != "disabled" {
		t.Fatal("disabled steps ran", calls, m)
	}
	value.Review, value.AutoApply = true, false
	if _, err := store.Set(value); err != nil {
		t.Fatal(err)
	}
	var applied []bool
	s.start = func(_ context.Context, apply bool) (<-chan struct{}, error) {
		applied = append(applied, apply)
		done := make(chan struct{})
		close(done)
		return done, nil
	}
	s.runNight(context.Background())
	if len(applied) != 1 || applied[0] || !svc.maintenance.ReviewStarted {
		t.Fatal("recommend-only review applied", applied)
	}
	value.Enabled = false
	if _, err := store.Set(value); err != nil {
		t.Fatal(err)
	}
	if !s.NextWindow(time.Now()).IsZero() {
		t.Fatal("a disabled window was planned")
	}
}

// A transient read failure is retried, and a planted link is never followed.
func TestSettingsRetryTransientReadAndRefuseLinks(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewSettingsStore(filepath.Join(blocker, "maintenance.json"))
	if store.Get() != DefaultSettings() || store.loaded {
		t.Fatal("a transient read failure fixed the defaults")
	}
	path := filepath.Join(dir, "state", "maintenance.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"window":"01:00"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if got := NewSettingsStore(path).Get(); got != DefaultSettings() {
		t.Fatal("followed a link on read", got)
	}
	if _, err := NewSettingsStore(path).Set(DefaultSettings()); !errors.Is(err, ErrSettingsStorage) {
		t.Fatal("wrote through a link", err)
	}
}

// A recommend-only nightly review stays stageable for 12 hours; any other
// review for 30 minutes.
func TestNightlyRecommendationValidity(t *testing.T) {
	s := &Service{}
	if s.stageAge() != 30*time.Minute {
		t.Fatal("default staging age")
	}
	s.status.ManualSample, s.status.ReviewTrigger = true, "nightly"
	if s.stageAge() != NightlyRecommendationValidity {
		t.Fatal("nightly recommendation age")
	}
	s.status.ReviewTrigger = "manual"
	if s.stageAge() != 30*time.Minute {
		t.Fatal("manual test age")
	}
}
