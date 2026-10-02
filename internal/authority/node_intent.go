package authority

import (
	"context"
	"os"
)

// NodeIntentBinding concerns only the current node transaction. No caller can
// supply a RAM record path, executable, recovery action, or durable schema.
type NodeIntentBinding interface {
	Verify() error
	Clear() error
}

type nodeIntentClaim interface {
	BindNodeIntent(*os.File) (NodeIntentBinding, error)
}

type boundNodeIntent struct {
	op    *operation
	inner NodeIntentBinding
}

// BindNodeIntent is called only after the transaction's O_EXCL create and file
// plus parent-directory fsync. Existing intents must never enter this path.
// Ordinary non-native authority keeps its existing durable-intent behavior.
func BindNodeIntent(ctx context.Context, created *os.File) (NodeIntentBinding, error) {
	if ctx == nil {
		return nil, nil
	}
	op, _ := ctx.Value(operationKey{}).(*operation)
	if op == nil {
		return nil, nil
	}
	op.foreground.Lock()
	defer op.foreground.Unlock()
	l := op.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.native {
		return nil, nil
	}
	if op.closed || l.active != op || op.poisoned || l.fault || l.block || op.intent != nil || ctx.Err() != nil {
		return nil, ErrOwnershipLost
	}
	claim, ok := l.held.(nodeIntentClaim)
	if !ok {
		return nil, ErrOwnershipLost
	}
	binding, err := claim.BindNodeIntent(created)
	if err != nil {
		l.block, l.fault = true, true
		op.poisoned = true
		return nil, ErrOwnershipLost
	}
	op.intent = binding
	return &boundNodeIntent{op: op, inner: binding}, nil
}

func (b *boundNodeIntent) access(clear bool) error {
	op := b.op
	op.foreground.Lock()
	defer op.foreground.Unlock()
	l := op.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if op.closed || l.active != op || op.poisoned || l.fault || op.intent != b.inner {
		return ErrOwnershipLost
	}
	var err error
	if clear {
		err = b.inner.Clear()
	} else {
		err = b.inner.Verify()
	}
	if err != nil {
		l.block = true
		op.poisoned = true
		return ErrOwnershipLost
	}
	if clear {
		op.intent = nil
	}
	return nil
}
func (b *boundNodeIntent) Verify() error { return b.access(false) }
func (b *boundNodeIntent) Clear() error  { return b.access(true) }
