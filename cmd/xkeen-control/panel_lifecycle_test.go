package main

import (
	"context"
	"errors"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

type panelCoordinatorStub struct {
	err      error
	released int
}

func (s *panelCoordinatorStub) BeginApply(context.Context) (func(), error) {
	if s.err != nil {
		return nil, s.err
	}
	return func() { s.released++ }, nil
}

func TestPanelLifecycleRejectsNativeBusyAndUnknown(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		lease := authority.NewLease()
		if blocked {
			lease.Block()
		} else {
			release, _ := lease.TryAcquire()
			defer release()
		}
		coordinator := &panelCoordinatorStub{}
		release, err := (panelLifecycle{coordinator, lease}).BeginApply(context.Background())
		if err == nil || release != nil || coordinator.released != 1 {
			t.Fatalf("blocked=%v: admitted panel handoff or leaked coordinator: %v", blocked, err)
		}
	}
}

func TestPanelLifecycleHoldsLeaseThroughHandoff(t *testing.T) {
	lease := authority.NewLease()
	coordinator := &panelCoordinatorStub{}
	release, err := (panelLifecycle{coordinator, lease}).BeginApply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.TryAcquire(); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("native writer entered while panel helper owns handoff")
	}
	release()
	release()
	if coordinator.released != 1 {
		t.Fatal("coordinator released more than once")
	}
	nativeRelease, err := lease.TryAcquire()
	if err != nil {
		t.Fatal("lease not released after helper termination")
	}
	nativeRelease()
}

func TestPanelLifecycleCoordinatorFailureDoesNotAcquireLease(t *testing.T) {
	lease := authority.NewLease()
	if _, err := (panelLifecycle{&panelCoordinatorStub{err: errors.New("busy")}, lease}).BeginApply(context.Background()); err == nil {
		t.Fatal("coordinator refusal ignored")
	}
	release, err := lease.TryAcquire()
	if err != nil {
		t.Fatal("coordinator refusal leaked native lease")
	}
	release()
}
