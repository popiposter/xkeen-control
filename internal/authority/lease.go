// Package authority contains the small serialization primitive shared by
// logical appliance and node authority operations. It deliberately has no
// runtime lifecycle behavior; c1.Coordinator owns that separate concern.
package authority

import (
	"context"
	"sync"
	"time"
)

// Lease is a one-slot, context-aware authority lock.
type Lease struct {
	gate chan struct{}
}

// NewLease creates an available authority lease.
func NewLease() *Lease {
	return &Lease{gate: make(chan struct{}, 1)}
}

// Acquire reserves the lease until the returned release function is called.
// A positive timeout bounds waiting for the slot but does not bound the work
// performed while the caller owns it.
func (l *Lease) Acquire(ctx context.Context, timeout time.Duration) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	waitContext := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		waitContext, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	select {
	case l.gate <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-l.gate }) }, nil
	case <-waitContext.Done():
		return nil, waitContext.Err()
	}
}
