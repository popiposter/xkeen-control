package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoordinatorApplyCancelsAndDrainsSupervisorOperation(t *testing.T) {
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
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	supervisor.SetActiveProbe(func(ctx context.Context, target string, _ int64) error {
		if target != "proxy-main-01" {
			return nil
		}
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-ctx.Done()
		select {
		case <-cancelled:
		default:
			close(cancelled)
		}
		return ctx.Err()
	})
	coordinator := NewCoordinator(policy, supervisor, nil, nil)

	supervisorDone := make(chan error, 1)
	go func() {
		supervisorDone <- coordinator.runSupervisorOperation(context.Background(), supervisor.Tick)
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := coordinator.BeginApply(ctx)
	if err != nil {
		t.Fatalf("Apply did not cancel/drain the active supervisor operation: %v", err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("Apply returned before the active liveness probe observed cancellation")
	}
	if err := <-supervisorDone; err != nil {
		t.Fatalf("cancelled supervisor tick returned an unexpected error: %v", err)
	}
	if len(api.rules) != 0 {
		t.Fatalf("Apply admission left a temporary probe rule behind: %v", api.rules)
	}
	if supervisor.Snapshot().LivenessFailures != 0 {
		t.Fatalf("cancelled supervisor tick leaked a liveness failure into Apply: %+v", supervisor.Snapshot())
	}

	ran := false
	if err := coordinator.runSupervisorOperation(context.Background(), func(context.Context) error {
		ran = true
		return nil
	}); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("supervisor operation admitted while Apply held lifecycle: %v", err)
	}
	if ran {
		t.Fatal("supervisor operation executed while Apply held lifecycle")
	}

	release()
	if err := coordinator.runSupervisorOperation(context.Background(), func(context.Context) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("supervisor operation did not resume after Apply release: %v", err)
	}
	if !ran {
		t.Fatal("supervisor operation did not execute after Apply release")
	}
}

func TestCoordinatorApplyReleaseWakesReconciliation(t *testing.T) {
	policy := supervisorPolicy()
	policy.ProbeInterval = time.Hour
	reader := &supervisorReader{snapshot: supervisorSnapshot("proxy-main-01", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	probe := NewProbeRouter(api)
	store := SelectionStore{Path: t.TempDir() + "/selection.json"}
	when := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: when, LastSwitchReason: ReasonStartup, LastSwitchAt: when}); err != nil {
		t.Fatal(err)
	}
	nodes := []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	supervisor := NewSupervisor(policy, reader, api, func(context.Context) []NodeState {
		return append([]NodeState(nil), nodes...)
	}, probe, store)
	initialProbeEntered := make(chan struct{})
	allowInitialProbe := make(chan struct{})
	recoveryProbe := make(chan struct{})
	probeCount := 0
	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" && probeCount == 0 {
			probeCount++
			close(initialProbeEntered)
			<-allowInitialProbe
			return nil
		}
		if target == "proxy-main-02" {
			select {
			case <-recoveryProbe:
			default:
				close(recoveryProbe)
			}
		}
		return nil
	})
	coordinator := NewCoordinator(policy, supervisor, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coordinator.Start(ctx)
	defer coordinator.Stop()
	<-initialProbeEntered
	close(allowInitialProbe)
	_ = supervisor.Snapshot()
	nodes = []NodeState{{Tag: "proxy-main-02", Enabled: true}}

	applyContext, applyCancel := context.WithTimeout(context.Background(), time.Second)
	release, err := coordinator.BeginApply(applyContext)
	applyCancel()
	if err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-recoveryProbe:
	case <-time.After(2 * time.Second):
		t.Fatal("Apply release did not wake supervisor reconciliation")
	}
	_ = supervisor.Snapshot()
	if reader.snapshot.Balancer.Override != "proxy-main-02" {
		t.Fatalf("post-Apply reconciliation did not replace removed stable target: %q", reader.snapshot.Balancer.Override)
	}
}
