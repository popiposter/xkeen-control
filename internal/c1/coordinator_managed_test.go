package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoordinatorManagedAdmissionIsNonPreemptiveAndReleasesLifecycle(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil, nil, nil)
	release, err := coordinator.TryBeginManagedApply()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := coordinator.Snapshot(); snapshot.Lifecycle == nil || !snapshot.Lifecycle.Applying {
		t.Fatalf("managed Apply projection = %+v", snapshot.Lifecycle)
	}
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("second managed admission = %v", err)
	}
	release()
	if coordinator.IsLifecycleBusy() {
		t.Fatal("managed release left lifecycle busy")
	}

	coordinator.EnterMaintenance()
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed admission during maintenance = %v", err)
	}
	coordinator.ExitMaintenance()

	operatorRelease, err := coordinator.BeginApply(context.Background())
	if err != nil {
		t.Fatalf("operator admission after maintenance = %v", err)
	}
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed admission beside operator Apply = %v", err)
	}
	operatorRelease()
}

func TestCoordinatorManagedAdmissionDoesNotCancelSupervisorOrBenchmark(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil, &BenchmarkRunner{}, nil)
	benchmarkCanceled := false
	coordinator.mu.Lock()
	coordinator.benchmarkCancel = func() { benchmarkCanceled = true }
	coordinator.benchmarkDone = make(chan struct{})
	coordinator.mu.Unlock()
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed admission beside benchmark = %v", err)
	}
	if benchmarkCanceled {
		t.Fatal("managed admission canceled benchmark work")
	}
	coordinator.mu.Lock()
	coordinator.benchmarkCancel = nil
	coordinator.benchmarkDone = nil
	coordinator.mu.Unlock()

	supervisor := &Supervisor{}
	coordinator = NewCoordinator(DefaultPolicy(), supervisor, nil, nil)
	entered := make(chan struct{})
	allow := make(chan struct{})
	canceled := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- coordinator.runSupervisorOperation(context.Background(), func(ctx context.Context) error {
			close(entered)
			select {
			case <-allow:
				return nil
			case <-ctx.Done():
				close(canceled)
				return ctx.Err()
			}
		})
	}()
	<-entered
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed admission beside supervisor = %v", err)
	}
	select {
	case <-canceled:
		t.Fatal("managed admission canceled supervisor work")
	default:
	}
	close(allow)
	if err := <-done; err != nil {
		t.Fatalf("supervisor operation = %v", err)
	}
}

func TestCoordinatorWaitingOperatorWinsManagedAdmission(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil, nil, nil)
	hookEntered := make(chan struct{})
	hookRelease := make(chan struct{})
	coordinator.beforeApplyAcquire = func() {
		close(hookEntered)
		<-hookRelease
	}
	result := make(chan struct {
		release func()
		err     error
	}, 1)
	go func() {
		release, err := coordinator.BeginApply(context.Background())
		result <- struct {
			release func()
			err     error
		}{release: release, err: err}
	}()
	<-hookEntered
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed admission ahead of waiting operator = %v", err)
	}
	close(hookRelease)
	operator := <-result
	if operator.err != nil {
		t.Fatal(operator.err)
	}
	operator.release()
}

func TestCoordinatorManagedReleaseRequestsSupervisorReconciliation(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), &Supervisor{}, nil, nil)
	release, err := coordinator.TryBeginManagedApply()
	if err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-coordinator.supervisorWake:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("managed release did not request supervisor reconciliation")
	}
}
