package c1

import (
	"context"
	"errors"
	"testing"
)

func TestCoordinatorStartsWithAvailableLifecycleToken(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil)
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

func TestCoordinatorMaintenanceRejectsNormalMutationsButAdmitsRecovery(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil)
	coordinator.EnterMaintenance()
	if snapshot := coordinator.Snapshot(); snapshot.Lifecycle == nil || !snapshot.Lifecycle.Maintenance || snapshot.Lifecycle.Applying {
		t.Fatalf("maintenance lifecycle projection = %+v", snapshot.Lifecycle)
	}
	if _, err := coordinator.BeginApply(context.Background()); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("normal lifecycle mutation while in maintenance = %v", err)
	}
	release, err := coordinator.BeginRecovery(context.Background())
	if err != nil {
		t.Fatalf("recovery admission while in maintenance = %v", err)
	}
	release()
	coordinator.ExitMaintenance()
	release, err = coordinator.BeginApply(context.Background())
	if err != nil {
		t.Fatalf("lifecycle mutation after maintenance = %v", err)
	}
	if snapshot := coordinator.Snapshot(); snapshot.Lifecycle == nil || snapshot.Lifecycle.Maintenance || !snapshot.Lifecycle.Applying {
		t.Fatalf("active Apply lifecycle projection = %+v", snapshot.Lifecycle)
	}
	release()
	if snapshot := coordinator.Snapshot(); snapshot.Lifecycle == nil || snapshot.Lifecycle.Maintenance || snapshot.Lifecycle.Applying {
		t.Fatalf("idle lifecycle projection = %+v", snapshot.Lifecycle)
	}
}

func TestCoordinatorApplyAdmissionWinsForcedInterleaving(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil)
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
	if snapshot := coordinator.Snapshot(); snapshot.Lifecycle == nil || !snapshot.Lifecycle.Applying {
		t.Fatalf("waiting Apply was not projected: %+v", snapshot.Lifecycle)
	}
	if release, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("managed work won after Apply admission: %v", err)
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
