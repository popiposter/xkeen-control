package authority

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrContextRequired   = errors.New("native authority requires ownership context")
	ErrOwnershipLost     = errors.New("native authority ownership lost")
	ErrNativeUnavailable = errors.New("native authority unavailable")
)

// nativeClaim keeps platform-specific admission behind the existing lease.
type nativeClaim interface {
	Release() error
	Verify() error
	ChildEnvironment([]string) []string
}

type operationKey struct{}
type operation struct {
	lease *Lease
	// Serializes foreground work with release. State below is protected by
	// lease.mu; foreground callbacks may call BlockContext without deadlocking.
	foreground sync.Mutex
	recovery   bool
	poisoned   bool
	closed     bool
	releaseErr error
	intent     NodeIntentBinding
}

// NewNativeLease opts in to a pre-created RAM root. It performs no IO and must
// not be enabled until all mutation callers and native entrypoints participate.
// Non-Linux admission fails closed; NewLease remains platform independent.
func NewNativeLease(root string) *Lease {
	l := NewLease()
	l.native, l.root = true, root
	return l
}

// AcquireContext must precede the operation baseline read. Pass the returned
// context through foreground native calls, including bounded rollback contexts.
func (l *Lease) AcquireContext(ctx context.Context, timeout time.Duration) (context.Context, func() error, error) {
	return l.acquireContext(ctx, timeout, false, false)
}
func (l *Lease) TryAcquireContext(ctx context.Context) (context.Context, func() error, error) {
	return l.acquireContext(ctx, 0, true, false)
}

// AcquireForRecoveryContext may reuse only this lease's retained live native
// claim. It never adopts or removes a foreign, incomplete or stale native gate.
// A retained node-intent binding cannot be adopted by a new recovery operation.
func (l *Lease) AcquireForRecoveryContext(ctx context.Context, timeout time.Duration) (context.Context, func() error, error) {
	return l.acquireContext(ctx, timeout, false, true)
}

func (l *Lease) acquireContext(ctx context.Context, timeout time.Duration, immediate, recovery bool) (context.Context, func() error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil {
		return ctx, func() error { return nil }, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	l.mu.RLock()
	blocked := l.fault || l.block && !recovery
	l.mu.RUnlock()
	if blocked {
		return nil, nil, ErrBlocked
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
			return nil, nil, ErrBusy
		}
	} else {
		select {
		case l.gate <- struct{}{}:
		case <-wait.Done():
			return nil, nil, wait.Err()
		}
	}
	l.mu.Lock()
	fail := func(err error) (context.Context, func() error, error) {
		l.mu.Unlock()
		<-l.gate
		return nil, nil, err
	}
	if err := wait.Err(); err != nil {
		return fail(err)
	}
	if l.fault || l.block && !recovery {
		return fail(ErrBlocked)
	}
	if l.native {
		if l.held != nil {
			if !recovery || l.retainedNodeIntent || l.held.Verify() != nil {
				l.block, l.fault = true, true
				return fail(ErrOwnershipLost)
			}
		} else {
			claim, err := acquireNative(l.root)
			if err != nil {
				return fail(err)
			}
			l.held = claim
		}
	}
	op := &operation{lease: l, recovery: recovery}
	l.active = op
	l.mu.Unlock()
	return context.WithValue(ctx, operationKey{}, op), op.release, nil
}

func (op *operation) release() error {
	op.foreground.Lock()
	defer op.foreground.Unlock()
	l := op.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if op.closed {
		return op.releaseErr
	}
	op.closed = true
	if l.native && l.held != nil {
		if op.intent != nil {
			l.block, l.retainedNodeIntent = true, true
		}
		if l.block || l.fault {
			op.releaseErr = ErrBlocked
		} else if err := l.held.Release(); err != nil {
			l.block, l.fault = true, true
			op.releaseErr = ErrOwnershipLost
		} else {
			l.held = nil
		}
	}
	l.active = nil
	<-l.gate
	return op.releaseErr
}

// BlockContext retains only the operation represented by a still-active context.
// Lifecycle unknown outcomes use this before their foreground callback returns.
func BlockContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	op, _ := ctx.Value(operationKey{}).(*operation)
	if op == nil {
		return
	}
	l := op.lease
	l.mu.Lock()
	if !op.closed && l.active == op {
		l.block = true
		op.poisoned = true
	}
	l.mu.Unlock()
}

// WithForeground strips untrusted inherited gate variables, then projects only
// a live context capability. It holds that capability through callback completion
// and serializes foreground children. Callers must join all work before returning;
// native dispatch must strip this context before spawning background services.
func WithForeground(ctx context.Context, env []string, run func([]string) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	clean := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, "XKEEN_GATE_ROOT=") && !strings.HasPrefix(entry, "XKEEN_GATE_TOKEN=") {
			clean = append(clean, entry)
		}
	}
	op, _ := ctx.Value(operationKey{}).(*operation)
	if op == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return run(clean)
	}
	op.foreground.Lock()
	defer op.foreground.Unlock()
	l := op.lease
	l.mu.Lock()
	if op.closed || l.active != op {
		l.mu.Unlock()
		return ErrOwnershipLost
	}
	if op.poisoned || l.fault || l.block && !op.recovery {
		l.mu.Unlock()
		return ErrBlocked
	}
	if err := ctx.Err(); err != nil {
		l.mu.Unlock()
		return err
	}
	if l.held != nil {
		if err := l.held.Verify(); err != nil {
			l.block, l.fault = true, true
			l.mu.Unlock()
			return ErrOwnershipLost
		}
		if op.intent != nil && op.intent.Verify() != nil {
			l.block, l.fault = true, true
			l.mu.Unlock()
			return ErrOwnershipLost
		}
		clean = l.held.ChildEnvironment(clean)
	}
	l.mu.Unlock()
	return run(clean)
}
