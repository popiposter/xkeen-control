// Package authority serializes panel operations. External XKeen CLI and cron
// are independent; this lease does not claim to exclude those writers.
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

type Lease struct {
	gate  chan struct{}
	mu    sync.RWMutex
	block bool
}

func NewLease() *Lease { return &Lease{gate: make(chan struct{}, 1)} }

func (l *Lease) acquire(ctx context.Context, timeout time.Duration, immediate, recovery bool) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l == nil {
		return func() {}, nil
	}
	if !recovery && l.isBlocked() {
		return nil, ErrBlocked
	}
	wait := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		wait, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if immediate {
		select {
		case l.gate <- struct{}{}:
		default:
			return nil, ErrBusy
		}
	} else {
		select {
		case l.gate <- struct{}{}:
		case <-wait.Done():
			return nil, wait.Err()
		}
	}
	var once sync.Once
	release := func() { once.Do(func() { <-l.gate }) }
	if err := wait.Err(); err != nil {
		release()
		return nil, err
	}
	if !recovery && l.isBlocked() {
		release()
		return nil, ErrBlocked
	}
	return release, nil
}

func (l *Lease) Acquire(ctx context.Context, timeout time.Duration) (func(), error) {
	return l.acquire(ctx, timeout, false, false)
}
func (l *Lease) TryAcquire() (func(), error) {
	return l.acquire(context.Background(), 0, true, false)
}

// Recovery alone may enter a retained block; its caller verifies the restored
// state and durable intent cleanup before Unblock.
func (l *Lease) AcquireForRecovery(ctx context.Context, timeout time.Duration) (func(), error) {
	return l.acquire(ctx, timeout, false, true)
}
func (l *Lease) Block() {
	if l != nil {
		l.mu.Lock()
		l.block = true
		l.mu.Unlock()
	}
}
func (l *Lease) Unblock() {
	if l != nil {
		l.mu.Lock()
		l.block = false
		l.mu.Unlock()
	}
}
func (l *Lease) isBlocked() bool { l.mu.RLock(); defer l.mu.RUnlock(); return l.block }
