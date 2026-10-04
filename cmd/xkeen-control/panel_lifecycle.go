package main

import (
	"context"
	"sync"

	"github.com/popiposter/xkeen-control/internal/authority"
	panelupdate "github.com/popiposter/xkeen-control/internal/update"
)

// Panel replacement/rebind must exclude native jobs and config writers as well
// as diagnostics. Keep node activation's coordinator-before-lease lock order.
// Both tokens remain owned by the existing helper handoff until it terminates.
type panelLifecycle struct {
	coordinator panelupdate.Lifecycle
	lease       *authority.Lease
}

func (p panelLifecycle) BeginApply(ctx context.Context) (func(), error) {
	releaseCoordinator, err := p.coordinator.BeginApply(ctx)
	if err != nil {
		return nil, err
	}
	releaseLease, err := p.lease.TryAcquire()
	if err != nil {
		releaseCoordinator()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseLease()
			releaseCoordinator()
		})
	}, nil
}
