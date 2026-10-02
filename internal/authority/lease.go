// Package authority contains the small serialization primitive shared by
// logical appliance and node authority operations. It deliberately has no
// runtime lifecycle behavior; c1.Coordinator owns that separate concern.
package authority

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrBlocked = errors.New("authority lease is blocked for recovery")
	ErrBusy    = errors.New("authority lease is busy")
)

// Lease is a one-slot, context-aware authority lock.
type Lease struct {
	gate   chan struct{}
	mu     sync.RWMutex
	block  bool
	native bool
	root   string
	held   nativeClaim
	fault  bool
	active *operation
}

// NewLease creates an available authority lease.
func NewLease() *Lease {
	return &Lease{gate: make(chan struct{}, 1)}
}

// Acquire reserves the lease until the returned release function is called.
// A positive timeout bounds waiting for the slot but does not bound the work
// performed while the caller owns it.
func (l *Lease) Acquire(ctx context.Context, timeout time.Duration) (func(), error) {
	if l != nil && l.native {
		return nil, ErrContextRequired
	}
	_, release, err := l.AcquireContext(ctx, timeout)
	if err != nil {
		return nil, err
	}
	return func() { _ = release() }, nil
}

// TryAcquire reserves the normal authority lease only when it is immediately
// available. It never waits, so a background caller can defer without holding
// another lifecycle ownership boundary while an unrelated operation finishes.
// A recovery block is checked both before and after the one-slot admission;
// an operation that already owns the lease remains valid when Block is called,
// matching the ordinary Acquire semantics.
func (l *Lease) TryAcquire() (func(), error) {
	if l != nil && l.native {
		return nil, ErrContextRequired
	}
	_, release, err := l.TryAcquireContext(context.Background())
	if err != nil {
		return nil, err
	}
	return func() { _ = release() }, nil
}

// AcquireForRecovery reserves the lease for the bounded startup recovery path.
// It is the only operation allowed to pass a retained maintenance block. The
// caller must clear the block only after the journal removal has been made
// durable and the restored runtime has been verified.
func (l *Lease) AcquireForRecovery(ctx context.Context, timeout time.Duration) (func(), error) {
	if l != nil && l.native {
		return nil, ErrContextRequired
	}
	_, release, err := l.AcquireForRecoveryContext(ctx, timeout)
	if err != nil {
		return nil, err
	}
	return func() { _ = release() }, nil
}

// Block prevents all normal authority operations from entering the shared
// lease until a fresh recovery path has proved the retained import journal is
// resolved. It does not interrupt a running foreground callback, but revokes
// that operation context's permission to start another foreground command.
func (l *Lease) Block() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.block = true
	if l.active != nil {
		l.active.poisoned = true
	}
	l.mu.Unlock()
}

// Unblock reopens normal authority operations after successful recovery.
func (l *Lease) Unblock() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if !l.fault && (!l.native || (l.active == nil || !l.active.poisoned) && (l.held == nil || l.active != nil && l.active.recovery)) {
		l.block = false
	}
	l.mu.Unlock()
}

func (l *Lease) isBlocked() bool {
	l.mu.RLock()
	blocked := l.block
	l.mu.RUnlock()
	return blocked
}
