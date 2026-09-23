package performancepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

type runtimeStub struct {
	initialized c1.PerformancePolicy
	committed   c1.PerformancePolicy
	changed     bool
	commits     int
	busy        bool
	adaptive    c1.AdaptivePerformanceStatus
}

func (stub *runtimeStub) InitializePerformancePolicy(policy c1.PerformancePolicy, _ time.Time) error {
	stub.initialized = policy
	return nil
}

func (stub *runtimeStub) CommitPerformancePolicy(_ context.Context, policy c1.PerformancePolicy, _ time.Time, changed bool, persist func() error) error {
	stub.commits++
	if stub.busy {
		return c1.ErrBenchmarkBusy
	}
	if err := persist(); err != nil {
		return err
	}
	stub.committed = policy
	stub.changed = changed
	return nil
}

func (stub *runtimeStub) AdaptiveSnapshot() c1.AdaptivePerformanceStatus { return stub.adaptive }

func testService(t *testing.T, runtimeOwner *runtimeStub) (*Service, string) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(directory, "performance-policy.json")
	return NewService(Config{Path: path, Runtime: runtimeOwner}), path
}

func writeTestPolicy(t *testing.T, path string, policy c1.PerformancePolicy) {
	t.Helper()
	contents, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefaultsPersistedRoundTripAndHardCeilings(t *testing.T) {
	runtimeOwner := &runtimeStub{adaptive: c1.AdaptivePerformanceStatus{State: "waiting", Generation: 7}}
	service, path := testService(t, runtimeOwner)
	if err := service.InitializeRuntime(); err != nil {
		t.Fatal(err)
	}
	defaults := c1.DefaultPerformancePolicy()
	if runtimeOwner.initialized != defaults {
		t.Fatalf("initialized = %+v", runtimeOwner.initialized)
	}
	projection, err := service.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if projection.Policy != defaults || projection.Source != SourceDefault || projection.ReasonCode != "" || projection.AuthorityState != AuthorityEditable || projection.PersistedSource != SourceDefault || projection.HardCeilings.MaxCandidates != 6 || projection.HardCeilings.CandidateDownloadMiB != 16 || projection.HardCeilings.CandidateUploadMiB != 8 || projection.HardCeilings.CandidateMaxSeconds != 30 || projection.HardCeilings.GenerationMaxMiB != 144 || projection.HardCeilings.GenerationMaxSeconds != 180 || projection.HardCeilings.TransportIdentity != "source-owned" || projection.HardCeilings.RTTGuard != "source-owned" || projection.HardCeilings.Scoring != "source-owned" || projection.Adaptive.Generation != 7 {
		t.Fatalf("default projection = %+v", projection)
	}

	candidate := defaults
	candidate.ProbeIntervalSeconds = 300
	candidate.FailureThreshold = 5
	candidate.AdaptiveCadenceMinutes = 1440
	candidate.AdaptiveChallengerLimit = 1
	candidate.MinimumDwellMinutes = 1440
	candidate.QualityHysteresisPercent = 50
	preview, err := service.Preview(context.Background(), "session-a", candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Noop || preview.RestartRequired || !preview.NextRunTimeChanges || len(preview.Changes) != 6 {
		t.Fatalf("preview = %+v", preview)
	}
	result, err := service.Apply(context.Background(), "session-a", preview.Token)
	if err != nil {
		t.Fatal(err)
	}
	if result.Noop || result.RestartRequired || !result.NextRunTimeChanged || result.Policy != candidate || runtimeOwner.committed != candidate || !runtimeOwner.changed {
		t.Fatalf("apply = %+v runtime=%+v", result, runtimeOwner)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	projection, err = service.Read(context.Background())
	if err != nil || projection.Policy != candidate || projection.Source != SourcePersisted || projection.ReasonCode != "" || projection.AuthorityState != AuthorityEditable || projection.PersistedSource != SourcePersisted {
		t.Fatalf("persisted projection = %+v err=%v", projection, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"url", "schedule", "cron", "payload", "budget", "timeout", "rttGuard", "scoringWeight", "selectedTarget"} {
		if strings.Contains(strings.ToLower(string(contents)), strings.ToLower(forbidden)) {
			t.Fatalf("persisted forbidden field %q: %s", forbidden, contents)
		}
	}
}

func TestPerformancePolicyBoundsAndStrictDTO(t *testing.T) {
	valid := c1.DefaultPerformancePolicy()
	tests := []struct {
		name   string
		mutate func(*c1.PerformancePolicy)
		valid  bool
	}{
		{"probe lower", func(p *c1.PerformancePolicy) { p.ProbeIntervalSeconds = 60 }, true},
		{"probe upper", func(p *c1.PerformancePolicy) { p.ProbeIntervalSeconds = 300 }, true},
		{"probe below", func(p *c1.PerformancePolicy) { p.ProbeIntervalSeconds = 30 }, false},
		{"probe modulo", func(p *c1.PerformancePolicy) { p.ProbeIntervalSeconds = 61 }, false},
		{"failure lower", func(p *c1.PerformancePolicy) { p.FailureThreshold = 2 }, true},
		{"failure upper", func(p *c1.PerformancePolicy) { p.FailureThreshold = 5 }, true},
		{"failure outside", func(p *c1.PerformancePolicy) { p.FailureThreshold = 1 }, false},
		{"cadence lower", func(p *c1.PerformancePolicy) { p.AdaptiveCadenceMinutes = 180 }, true},
		{"cadence upper", func(p *c1.PerformancePolicy) { p.AdaptiveCadenceMinutes = 1440 }, true},
		{"cadence modulo", func(p *c1.PerformancePolicy) { p.AdaptiveCadenceMinutes = 181 }, false},
		{"challenger lower", func(p *c1.PerformancePolicy) { p.AdaptiveChallengerLimit = 1 }, true},
		{"challenger upper", func(p *c1.PerformancePolicy) { p.AdaptiveChallengerLimit = 5 }, true},
		{"challenger outside", func(p *c1.PerformancePolicy) { p.AdaptiveChallengerLimit = 6 }, false},
		{"dwell lower", func(p *c1.PerformancePolicy) { p.MinimumDwellMinutes = 30 }, true},
		{"dwell upper", func(p *c1.PerformancePolicy) { p.MinimumDwellMinutes = 1440 }, true},
		{"dwell outside", func(p *c1.PerformancePolicy) { p.MinimumDwellMinutes = 29 }, false},
		{"hysteresis lower", func(p *c1.PerformancePolicy) { p.QualityHysteresisPercent = 10 }, true},
		{"hysteresis upper", func(p *c1.PerformancePolicy) { p.QualityHysteresisPercent = 50 }, true},
		{"hysteresis outside", func(p *c1.PerformancePolicy) { p.QualityHysteresisPercent = 9 }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			err := c1.ValidatePerformancePolicy(candidate)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v candidate=%+v", test.valid, err, candidate)
			}
		})
	}

	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRequest(encoded); err != nil {
		t.Fatalf("valid DTO: %v", err)
	}
	invalid := []string{
		`{"schemaVersion":1,"probeIntervalSeconds":60,"failureThreshold":2,"adaptiveCadenceMinutes":180,"adaptiveChallengerLimit":5,"minimumDwellMinutes":30}`,
		`{"schemaVersion":1,"probeIntervalSeconds":60,"probeIntervalSeconds":90,"failureThreshold":2,"adaptiveCadenceMinutes":180,"adaptiveChallengerLimit":5,"minimumDwellMinutes":30,"qualityHysteresisPercent":10}`,
		`{"schemaVersion":1,"probeIntervalSeconds":60,"failureThreshold":2,"adaptiveCadenceMinutes":180,"adaptiveChallengerLimit":5,"minimumDwellMinutes":30,"qualityHysteresisPercent":10,"url":"https://example.invalid"}`,
		string(encoded) + `{}`,
	}
	for _, body := range invalid {
		if _, err := DecodeRequest([]byte(body)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("accepted strict-invalid DTO: %s err=%v", body, err)
		}
	}
}

func TestInvalidFilesFailClosedWithoutRewriteAndCanBeReplaced(t *testing.T) {
	create := func(t *testing.T, kind string) (*Service, string) {
		t.Helper()
		service, path := testService(t, &runtimeStub{})
		switch kind {
		case "malformed":
			if err := os.WriteFile(path, []byte(`{"schemaVersion":1`), 0o600); err != nil {
				t.Fatal(err)
			}
		case "oversize":
			if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxPolicyBytes+1)), 0o600); err != nil {
				t.Fatal(err)
			}
		case "permissions":
			if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS == "windows" {
				t.Skip("mode permissions are not enforced on Windows")
			}
		case "symlink":
			target := filepath.Join(t.TempDir(), "target.json")
			if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}
		if err := service.InitializeRuntime(); err != nil {
			t.Fatal(err)
		}
		return service, path
	}

	for _, kind := range []string{"malformed", "oversize", "permissions", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			service, path := create(t, kind)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := service.Read(context.Background())
			if err != nil || projection.Policy != c1.DefaultPerformancePolicy() || projection.Source != SourceDefault || projection.ReasonCode == "" {
				t.Fatalf("fail-closed projection = %+v err=%v", projection, err)
			}
			after, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode() != after.Mode() || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
				t.Fatalf("read rewrote invalid file: before=%+v after=%+v", before, after)
			}
			preview, err := service.Preview(context.Background(), "session", c1.DefaultPerformancePolicy())
			if err != nil || preview.Noop {
				t.Fatalf("replacement preview = %+v err=%v", preview, err)
			}
			if _, err := service.Apply(context.Background(), "session", preview.Token); err != nil {
				t.Fatal(err)
			}
			projection, err = service.Read(context.Background())
			if err != nil || projection.Source != SourcePersisted || projection.ReasonCode != "" {
				t.Fatalf("replacement projection = %+v err=%v", projection, err)
			}
		})
	}
}

func TestNoopBusyStaleCancelExpiryAndSessionBinding(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	runtimeOwner := &runtimeStub{}
	service, path := testService(t, runtimeOwner)
	service.config.Now = func() time.Time { return now }
	if err := service.InitializeRuntime(); err != nil {
		t.Fatal(err)
	}

	preview, err := service.Preview(context.Background(), "session-a", c1.DefaultPerformancePolicy())
	if err != nil || !preview.Noop {
		t.Fatalf("no-op preview = %+v err=%v", preview, err)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.Token); err != nil {
		t.Fatal(err)
	}
	if runtimeOwner.changed || runtimeOwner.commits != 1 {
		t.Fatalf("no-op runtime = %+v", runtimeOwner)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no-op wrote file: %v", err)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("token reusable: %v", err)
	}

	candidate := c1.DefaultPerformancePolicy()
	candidate.FailureThreshold = 3
	busyPreview, _ := service.Preview(context.Background(), "session-a", candidate)
	runtimeOwner.busy = true
	if _, err := service.Apply(context.Background(), "session-a", busyPreview.Token); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy apply = %v", err)
	}
	runtimeOwner.busy = false
	if _, err := service.Apply(context.Background(), "session-a", busyPreview.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("busy token reusable = %v", err)
	}

	boundPreview, _ := service.Preview(context.Background(), "session-a", candidate)
	if _, err := service.Apply(context.Background(), "session-b", boundPreview.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("cross-session apply = %v", err)
	}
	service.Cancel("session-a", boundPreview.Token)
	if _, err := service.Apply(context.Background(), "session-a", boundPreview.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("cancelled token = %v", err)
	}

	expired, _ := service.Preview(context.Background(), "session-a", candidate)
	now = now.Add(DefaultPreviewTTL)
	if _, err := service.Apply(context.Background(), "session-a", expired.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("expired token = %v", err)
	}

	first, _ := service.Preview(context.Background(), "session-a", candidate)
	second, _ := service.Preview(context.Background(), "session-a", candidate)
	if _, err := service.Apply(context.Background(), "session-a", first.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("older same-session preview survived: %v", err)
	}
	service.Invalidate("session-a")
	if _, err := service.Apply(context.Background(), "session-a", second.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("invalidated preview survived: %v", err)
	}

	now = now.Add(time.Second)
	stale, _ := service.Preview(context.Background(), "session-a", candidate)
	writeTestPolicy(t, path, c1.DefaultPerformancePolicy())
	if _, err := service.Apply(context.Background(), "session-a", stale.Token); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("stale apply = %v", err)
	}
	projection, err := service.Read(context.Background())
	if err != nil || projection.AuthorityState != AuthorityDriftDetected || projection.Policy != c1.DefaultPerformancePolicy() {
		t.Fatalf("stale drift projection = %+v err=%v", projection, err)
	}
	if _, err := service.Preview(context.Background(), "session-a", c1.DefaultPerformancePolicy()); !errors.Is(err, ErrDriftDetected) {
		t.Fatalf("drift preview = %v", err)
	}
}

func TestPostStartAuthorityDriftKeepsActiveRuntimeAndBlocksNoopTrap(t *testing.T) {
	for _, test := range []struct {
		name            string
		persistedSource Source
		persistedReason bool
		mutate          func(*testing.T, string, c1.PerformancePolicy)
	}{
		{"valid replacement", SourcePersisted, false, func(t *testing.T, path string, policy c1.PerformancePolicy) { writeTestPolicy(t, path, policy) }},
		{"removal", SourceDefault, false, func(t *testing.T, path string, _ c1.PerformancePolicy) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"invalidation", SourceDefault, true, func(t *testing.T, path string, _ c1.PerformancePolicy) {
			if err := os.WriteFile(path, []byte(`{"schemaVersion":1`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtimeOwner := &runtimeStub{}
			service, path := testService(t, runtimeOwner)
			active := c1.DefaultPerformancePolicy()
			active.FailureThreshold = 3
			writeTestPolicy(t, path, active)
			if err := service.InitializeRuntime(); err != nil {
				t.Fatal(err)
			}
			candidate := active
			candidate.FailureThreshold = 5
			test.mutate(t, path, candidate)

			projection, err := service.Read(context.Background())
			if err != nil || projection.Policy != active || projection.Source != SourcePersisted || projection.AuthorityState != AuthorityDriftDetected || projection.PersistedSource != test.persistedSource || (projection.PersistedReasonCode != "") != test.persistedReason || runtimeOwner.initialized != active {
				t.Fatalf("drift projection = %+v runtime=%+v err=%v", projection, runtimeOwner, err)
			}
			if _, err := service.Preview(context.Background(), "session", candidate); !errors.Is(err, ErrDriftDetected) {
				t.Fatalf("file-equivalent no-op trap was not blocked: %v", err)
			}
			if runtimeOwner.commits != 0 {
				t.Fatalf("drift reached runtime owner: %+v", runtimeOwner)
			}
		})
	}
}

func TestTypedApplyAdvancesActiveGenerationOnlyAfterProvenConvergence(t *testing.T) {
	runtimeOwner := &runtimeStub{}
	service, _ := testService(t, runtimeOwner)
	if err := service.InitializeRuntime(); err != nil {
		t.Fatal(err)
	}
	candidate := c1.DefaultPerformancePolicy()
	candidate.AdaptiveCadenceMinutes = 360
	preview, err := service.Preview(context.Background(), "session", candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), "session", preview.Token); err != nil {
		t.Fatal(err)
	}
	projection, err := service.Read(context.Background())
	if err != nil || projection.Policy != candidate || projection.Source != SourcePersisted || projection.PersistedSource != SourcePersisted || projection.AuthorityState != AuthorityEditable || runtimeOwner.committed != candidate || !runtimeOwner.changed {
		t.Fatalf("converged projection = %+v runtime=%+v err=%v", projection, runtimeOwner, err)
	}
}

func TestPostRenameFailureBecomesClosedDrift(t *testing.T) {
	runtimeOwner := &runtimeStub{}
	service, _ := testService(t, runtimeOwner)
	if err := service.InitializeRuntime(); err != nil {
		t.Fatal(err)
	}
	candidate := c1.DefaultPerformancePolicy()
	candidate.MinimumDwellMinutes = 90
	preview, err := service.Preview(context.Background(), "session", candidate)
	if err != nil {
		t.Fatal(err)
	}
	write := service.writePolicy
	service.writePolicy = func(path string, policy c1.PerformancePolicy) (writeOutcome, error) {
		outcome, err := write(path, policy)
		if err != nil {
			return outcome, err
		}
		return outcome, ErrSave
	}
	if _, err := service.Apply(context.Background(), "session", preview.Token); !errors.Is(err, ErrDriftDetected) {
		t.Fatalf("post-rename apply = %v", err)
	}
	projection, err := service.Read(context.Background())
	if err != nil || projection.Policy != c1.DefaultPerformancePolicy() || projection.PersistedSource != SourcePersisted || projection.AuthorityState != AuthorityDriftDetected || runtimeOwner.changed {
		t.Fatalf("post-rename projection = %+v runtime=%+v err=%v", projection, runtimeOwner, err)
	}
}
