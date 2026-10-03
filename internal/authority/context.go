package authority

import (
	"context"
	"time"
)

// Context variants preserve caller cancellation/values across bounded rollback.
// They confer no capability to external native commands.
func (l *Lease) AcquireContext(ctx context.Context, timeout time.Duration) (context.Context, func() error, error) {
	return l.acquireContext(ctx, timeout, false, false)
}
func (l *Lease) TryAcquireContext(ctx context.Context) (context.Context, func() error, error) {
	return l.acquireContext(ctx, 0, true, false)
}
func (l *Lease) AcquireForRecoveryContext(ctx context.Context, timeout time.Duration) (context.Context, func() error, error) {
	return l.acquireContext(ctx, timeout, false, true)
}
func (l *Lease) acquireContext(ctx context.Context, timeout time.Duration, immediate, recovery bool) (context.Context, func() error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := l.acquire(ctx, timeout, immediate, recovery)
	if err != nil {
		return nil, nil, err
	}
	return ctx, func() error { release(); return nil }, nil
}
