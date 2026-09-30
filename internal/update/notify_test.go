package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/notifications"
	"github.com/popiposter/xkeen-control/internal/release"
)

func notifyFixture(t *testing.T) (*Manager, *NotifyScheduler, *time.Time, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	manifest, _ := testCandidate(t)
	data, _ := manifest.MarshalDeterministic()
	signature, _ := release.Sign(data, key)
	requests := &atomic.Int32{}
	sends := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch filepath.Base(r.URL.Path) {
		case "release-manifest.json":
			w.Write(data)
		case "release-manifest.sig":
			w.Write(signature)
		default:
			t.Error("background artifact download")
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	m := NewManager(Config{Current: buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", Channel: "stable", SourceCommit: strings.Repeat("b", 40)}, Client: release.NewClientForTest(server.URL, key.Public().(ed25519.PublicKey)), Now: func() time.Time { return now }, Paths: Paths{PolicyPath: filepath.Join(dir, "policy.json"), MarkerPath: filepath.Join(dir, "marker.json"), PreviousDir: filepath.Join(dir, "previous"), CandidateDir: filepath.Join(dir, "candidate")}})
	s := NewNotifyScheduler(NotifySchedulerConfig{Manager: m, Lifecycle: func() (bool, bool, bool) { return false, false, true }, Send: func(context.Context, notifications.Alert) error { sends.Add(1); return nil }})
	if _, err := m.SetPolicy(Policy{Channel: "stable", Mode: "notify", CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	return m, s, &now, requests, sends
}

func TestNotifyDiscoveryCannotArmOrReplaceExplicitCheckedApply(t *testing.T) {
	m, s, now, requests, sends := notifyFixture(t)
	ctx := context.Background()
	s.step(ctx)
	if requests.Load() != 0 || sends.Load() != 0 || !s.statusFor(m.readPolicy()).NextDueAt.Equal(now.Add(time.Hour)) {
		t.Fatal("startup catchup")
	}
	*now = now.Add(time.Hour - time.Second)
	s.step(ctx)
	if requests.Load() != 0 {
		t.Fatal("early discovery")
	}
	*now = now.Add(time.Second)
	s.step(ctx)
	status := m.Status(ctx)
	if requests.Load() != 2 || sends.Load() != 1 || status.LatestCompatible != "" || status.LatestSource != "" || status.LastCheckAt != "" || m.latest != nil {
		t.Fatalf("background armed Check: %+v", status)
	}
	if m.ApplyChecked(ctx, "stable", "1.2.3") == nil || m.ValidateChecked(ctx, "stable", "1.2.3") == nil || requests.Load() != 2 {
		t.Fatal("background authorizes Apply")
	}
	if _, err := m.Check(ctx, "stable", ""); err != nil {
		t.Fatal(err)
	}
	checked := *m.latest
	checkedAt := m.lastCheck
	result := m.lastResult
	if m.ValidateChecked(ctx, "stable", "1.2.3") != nil {
		t.Fatal("explicit Check did not arm Apply")
	}
	*now = now.Add(time.Hour)
	s.step(ctx)
	if m.latest == nil || m.latest.Version != checked.Version || m.latest.SourceCommit != checked.SourceCommit || m.lastCheck != checkedAt || m.lastResult != result {
		t.Fatal("discovery changed explicit candidate")
	}
	if sends.Load() != 1 {
		t.Fatal("success not deduped")
	}
}

func TestNotifyFailureRetryOnlyNextCadenceAndLifecycleSkips(t *testing.T) {
	m, s, now, requests, sends := notifyFixture(t)
	ctx := context.Background()
	s.send = func(context.Context, notifications.Alert) error {
		sends.Add(1)
		return errors.New("synthetic-token-sentinel")
	}
	s.step(ctx)
	*now = now.Add(time.Hour)
	s.step(ctx)
	if sends.Load() != 1 || s.statusFor(m.readPolicy()).NotificationState != "failed" {
		t.Fatal("failed delivery state")
	}
	for i := 0; i < 5; i++ {
		s.step(ctx)
	}
	if sends.Load() != 1 || requests.Load() != 2 {
		t.Fatal("immediate retry")
	}
	*now = now.Add(time.Hour)
	s.step(ctx)
	if sends.Load() != 2 {
		t.Fatal("next cadence retry absent")
	}
	for _, fixture := range []struct {
		maintenance, applying, available bool
		reason                           string
	}{{false, false, false, "lifecycle-unavailable"}, {true, false, true, "maintenance"}, {false, true, true, "applying"}} {
		s.lifecycle = func() (bool, bool, bool) { return fixture.maintenance, fixture.applying, fixture.available }
		before := requests.Load()
		*now = now.Add(time.Hour)
		s.step(ctx)
		if requests.Load() != before || s.statusFor(m.readPolicy()).LastSkipReason != fixture.reason {
			t.Fatal("lifecycle discovery bypass")
		}
	}
}

func TestNotifyPolicyChangesUnsupportedModesAndNewerVersions(t *testing.T) {
	m, s, now, requests, _ := notifyFixture(t)
	ctx := context.Background()
	s.step(ctx)
	*now = now.Add(30 * time.Minute)
	if _, err := m.SetPolicy(Policy{Channel: "stable", Mode: "notify", CheckCadenceMinutes: 120}); err != nil {
		t.Fatal(err)
	}
	s.step(ctx)
	if !s.statusFor(m.readPolicy()).NextDueAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatal("cadence change did not reset full delay")
	}
	*now = now.Add(time.Hour)
	s.step(ctx)
	if requests.Load() != 0 {
		t.Fatal("old due reused")
	}
	for _, fixture := range []struct{ channel, mode, state string }{{"beta", "notify", "unsupported-channel"}, {"stable", "auto-stable", "unsupported-mode"}, {"stable", "manual", "disabled"}} {
		if _, err := m.SetPolicy(Policy{Channel: fixture.channel, Mode: fixture.mode, CheckCadenceMinutes: 60}); err != nil {
			t.Fatal(err)
		}
		s.step(ctx)
		*now = now.Add(7 * 24 * time.Hour)
		s.step(ctx)
		if requests.Load() != 0 || m.Status(ctx).Scheduler.State != fixture.state || m.Status(ctx).Scheduler.NextDueAt != nil {
			t.Fatal("unsupported scheduler active")
		}
	}
	for _, fixture := range []struct {
		candidate, installed string
		want                 bool
	}{{"1.2.3", "1.2.2", true}, {"1.2.3", "1.2.3", false}, {"1.2.3", "1.2.4", false}, {"1.2.3", "1.2.3-beta.2", true}, {"0.2.0", "0.3.0-beta.2", false}, {"1.10.0", "1.9.0", true}, {"1.2.3", "dev", false}, {"1.2.3-beta.1", "1.2.2", false}, {"1.2.3", "1.2.3+build-label", false}} {
		if got := newerStable(fixture.candidate, fixture.installed); got != fixture.want {
			t.Errorf("newerStable(%s,%s)=%v", fixture.candidate, fixture.installed, got)
		}
	}
}

func TestNotifySignedCompatibilityAndConcurrentDiscovery(t *testing.T) {
	for _, kind := range []string{"bad-signature", "migration", "schema", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			m, s, now, _, sends := notifyFixture(t)
			manifest, _ := testCandidate(t)
			switch kind {
			case "migration":
				manifest.Compatibility.ManualMigrationRequired = true
			case "schema":
				manifest.Compatibility.StateSchemaMin = 1
				manifest.Compatibility.StateSchemaMax = 1
			case "rollback":
				manifest.Compatibility.RollbackCompatible = false
			}
			_, key, _ := ed25519.GenerateKey(rand.Reader)
			data, _ := manifest.MarshalDeterministic()
			signature, _ := release.Sign(data, key)
			if kind == "bad-signature" {
				signature = []byte("synthetic-bad-signature")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch filepath.Base(r.URL.Path) {
				case "release-manifest.json":
					w.Write(data)
				case "release-manifest.sig":
					w.Write(signature)
				default:
					t.Error("artifact body requested")
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			m.client = release.NewClientForTest(server.URL, key.Public().(ed25519.PublicKey))
			s.step(context.Background())
			*now = now.Add(time.Hour)
			s.step(context.Background())
			if sends.Load() != 0 || m.latest != nil || s.statusFor(m.readPolicy()).State != "failed" {
				t.Fatal("unverified/incompatible release notified")
			}
		})
	}
	m, s, now, _, sends := notifyFixture(t)
	manifest, _ := testCandidate(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	data, _ := manifest.MarshalDeterministic()
	signature, _ := release.Sign(data, key)
	entered := make(chan struct{}, 1)
	unblock := make(chan struct{})
	requests := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if filepath.Base(r.URL.Path) == "release-manifest.json" {
			entered <- struct{}{}
			<-unblock
			w.Write(data)
		} else {
			w.Write(signature)
		}
	}))
	defer server.Close()
	m.client = release.NewClientForTest(server.URL, key.Public().(ed25519.PublicKey))
	s.step(context.Background())
	*now = now.Add(time.Hour)
	done := make(chan struct{})
	go func() { defer close(done); s.step(context.Background()) }()
	<-entered
	s.step(context.Background())
	if requests.Load() != 1 {
		t.Fatal("parallel scheduled checks")
	}
	// A policy change while discovery is running discards delivery, then starts
	// an entirely new full cadence; it never clears/arms operator Check state.
	m.SetPolicy(Policy{Channel: "stable", Mode: "manual", CheckCadenceMinutes: 60})
	close(unblock)
	<-done
	if sends.Load() != 0 || m.latest != nil {
		t.Fatal("late policy delivery")
	}
	if _, err := m.SetPolicy(Policy{Channel: "stable", Mode: "notify", CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	s.step(context.Background())
	if !s.statusFor(m.readPolicy()).NextDueAt.Equal(now.Add(time.Hour)) {
		t.Fatal("policy change reused old due")
	}
}

func TestNotifyMalformedAndOversizePolicyNeverDiscovers(t *testing.T) {
	m, s, now, requests, _ := notifyFixture(t)
	for _, data := range []string{`{"channel":"stable","mode":"notify"}`, strings.Repeat(" ", maxPolicyBytes+1), `{"channel":"stable","mode":"webhook","checkCadenceMinutes":60}`} {
		if err := os.WriteFile(m.paths.PolicyPath, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		s.step(context.Background())
		*now = now.Add(24 * time.Hour)
		s.step(context.Background())
		if requests.Load() != 0 || m.Status(context.Background()).Scheduler.State != "disabled" {
			t.Fatal("malformed policy scheduled network")
		}
	}
}

func TestNotifyAtomicDeliveryAdmission(t *testing.T) {
	// Panel policy has no off mode. Manual, unsupported beta/auto-stable, and
	// leaving/re-entering identical stable notify values all revoke the epoch.
	for _, change := range []string{"manual", "beta-notify", "auto-stable", "notify-new-epoch"} {
		for _, admitted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/admitted=%t", change, admitted), func(t *testing.T) {
				m, s, now, _, sends := notifyFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				if admitted {
					s.send = func(ctx context.Context, _ notifications.Alert) error {
						sends.Add(1)
						close(entered)
						select {
						case <-release:
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				} else {
					s.beforeNotificationAdmission = func() { close(entered); <-release }
				}
				s.step(ctx)
				*now = now.Add(time.Hour)
				go func() { defer close(done); s.step(ctx) }()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("notification boundary not reached")
				}
				mutation := make(chan error, 1)
				go func() {
					policy := Policy{Channel: "stable", Mode: "manual", CheckCadenceMinutes: 60}
					if change == "beta-notify" {
						policy.Channel, policy.Mode = "beta", "notify"
					}
					if change == "auto-stable" {
						policy.Mode = "auto-stable"
					}
					_, err := m.SetPolicy(policy)
					if err == nil && change == "notify-new-epoch" {
						policy.Mode = "notify"
						_, err = m.SetPolicy(policy)
					}
					mutation <- err
				}()
				select {
				case err := <-mutation:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("policy mutation waited on notification network work")
				}
				unblock()
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("notification cycle did not finish")
				}
				want := int32(0)
				if admitted {
					want = 1
				}
				if sends.Load() != want || m.latest != nil || m.Status(ctx).LatestCompatible != "" {
					t.Fatalf("send calls=%d, want %d; candidate=%v", sends.Load(), want, m.latest)
				}
				if admitted && (s.status.NotificationState != "notified" || s.notified == "") {
					t.Fatal("already admitted delivery did not complete successfully")
				}
				if !admitted && (s.status.LastSkipReason != "policy-changed" || s.notified != "") {
					t.Fatal("revoked delivery was not skipped without dedupe")
				}
			})
		}
	}
}

func TestNotifyPolicyAndLifecycleRecheckedAfterDiscovery(t *testing.T) {
	m, s, now, requests, sends := notifyFixture(t)
	ctx := context.Background()
	s.step(ctx)
	// Change readiness while the signed metadata request is in flight.
	s.lifecycle = func() (bool, bool, bool) { return false, requests.Load() > 0, true }
	*now = now.Add(time.Hour)
	s.step(ctx)
	if sends.Load() != 0 || s.statusFor(m.readPolicy()).LastSkipReason != "lifecycle-changed" || m.latest != nil {
		t.Fatal("late lifecycle bypass")
	}
}
