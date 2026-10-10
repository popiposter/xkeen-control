package c1

import (
	"context"
	"errors"
	"testing"
)

func TestCoordinatorManagedAdmissionIsNonPreemptiveAndReleasesLifecycle(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil)
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

func TestCoordinatorWaitingOperatorWinsManagedAdmission(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil)
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
