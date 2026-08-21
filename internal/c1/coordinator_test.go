package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoordinatorStartsWithAvailableLifecycleToken(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil, nil, nil)
	if coordinator.IsLifecycleBusy() {
		t.Fatal("new coordinator reported a busy lifecycle")
	}
	release, err := coordinator.BeginApply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !coordinator.IsLifecycleBusy() {
		t.Fatal("held lifecycle was not reported busy")
	}
	release()
	if coordinator.IsLifecycleBusy() {
		t.Fatal("released lifecycle remained busy")
	}
}

func TestCoordinatorApplyAdmissionWinsForcedInterleaving(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil, &BenchmarkRunner{}, nil)
	hookEntered := make(chan struct{})
	hookRelease := make(chan struct{})
	coordinator.beforeApplyAcquire = func() {
		close(hookEntered)
		<-hookRelease
	}

	type applyResult struct {
		release func()
		err     error
	}
	result := make(chan applyResult, 1)
	go func() {
		release, err := coordinator.BeginApply(context.Background())
		result <- applyResult{release: release, err: err}
	}()
	<-hookEntered
	if err := coordinator.TriggerBenchmark(); !errors.Is(err, ErrBenchmarkBusy) {
		t.Fatalf("benchmark won after Apply admission: %v", err)
	}
	close(hookRelease)
	apply := <-result
	if apply.err != nil {
		t.Fatal(apply.err)
	}
	if !coordinator.IsLifecycleBusy() {
		t.Fatal("Apply did not hold lifecycle after the forced interleaving")
	}
	apply.release()
	if coordinator.IsLifecycleBusy() {
		t.Fatal("Apply release left lifecycle busy")
	}
}

func TestCoordinatorApplyCancelsAlreadyRunningBenchmark(t *testing.T) {
	policy := supervisorPolicy()
	reader := &supervisorReader{snapshot: supervisorSnapshot("proxy-main-01", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	probe := NewProbeRouter(api)
	runner := NewBenchmarkRunner(policy, probe, BenchmarkStore{Path: t.TempDir() + "/benchmark.json"})
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	first := true
	runner.HTTPDo = func(ctx context.Context, _ string, _ string, payload int64, _ time.Duration) (int64, time.Duration, error) {
		if first {
			first = false
			close(entered)
			<-ctx.Done()
			close(cancelled)
			return 0, 0, ctx.Err()
		}
		return payload, time.Millisecond, nil
	}
	store := SelectionStore{Path: t.TempDir() + "/selection.json"}
	when := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: when, LastSwitchReason: ReasonStartup, LastSwitchAt: when}); err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(policy, reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, probe, store)
	coordinator := NewCoordinator(policy, supervisor, runner, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	})
	if err := coordinator.TriggerBenchmark(); err != nil {
		t.Fatal(err)
	}
	<-entered
	if !coordinator.Snapshot().Benchmark.Running {
		t.Fatal("benchmark was not running before Apply admission")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := coordinator.BeginApply(ctx)
	if err != nil {
		t.Fatalf("Apply did not cancel the active benchmark: %v", err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("Apply returned before the current benchmark sample observed cancellation")
	}
	release()
	coordinator.Stop()
}

func TestBenchmarkDoesNotPauseIndependentLiveness(t *testing.T) {
	policy := supervisorPolicy()
	reader := &supervisorReader{snapshot: supervisorSnapshot("proxy-main-01", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	probe := NewProbeRouter(api)
	store := SelectionStore{Path: t.TempDir() + "/selection.json"}
	when := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: when, LastSwitchReason: ReasonStartup, LastSwitchAt: when}); err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(policy, reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, probe, store)
	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" {
			return errors.New("synthetic active liveness failure")
		}
		return nil
	})
	runner := NewBenchmarkRunner(policy, probe, BenchmarkStore{Path: t.TempDir() + "/benchmark.json"})
	entered := make(chan struct{})
	releaseSample := make(chan struct{})
	first := true
	runner.HTTPDo = func(ctx context.Context, _ string, _ string, payload int64, _ time.Duration) (int64, time.Duration, error) {
		if first {
			first = false
			close(entered)
			select {
			case <-releaseSample:
			case <-ctx.Done():
				return 0, 0, ctx.Err()
			}
		}
		return payload, time.Millisecond, nil
	}
	coordinator := NewCoordinator(policy, supervisor, runner, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	})
	if err := coordinator.TriggerBenchmark(); err != nil {
		t.Fatal(err)
	}
	<-entered
	if !coordinator.Snapshot().Benchmark.Running {
		t.Fatal("benchmark was not running while its first sample held the probe lease")
	}
	tickDone := make(chan error, 1)
	go func() {
		// Tick waits for the benchmark's per-sample probe lease. The actual
		// liveness result is checked after releasing that lease.
		tickDone <- supervisor.Tick(context.Background())
	}()
	close(releaseSample)
	if err := <-tickDone; err != nil {
		t.Fatal(err)
	}
	if supervisor.Snapshot().LivenessFailures != 1 {
		t.Fatalf("benchmark suppressed the first active liveness check: %+v", supervisor.Snapshot())
	}
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-02" {
		t.Fatalf("liveness did not fail over while benchmark was running: %q", reader.snapshot.Balancer.Override)
	}
	coordinator.Stop()
	if len(api.rules) != 0 {
		t.Fatalf("benchmark/liveness probe rules were not cleaned up: %v", api.rules)
	}
}
