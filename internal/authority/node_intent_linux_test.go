//go:build linux

package authority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedNodeIntentCannotBeAdoptedByRecoveryContext(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "intent-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	l := NewNativeLease(root)
	ctx, release, err := l.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(root, ".pending"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("node-operation-pending\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := BindNodeIntent(ctx, f); err != nil {
		t.Fatal(err)
	}
	BlockContext(ctx)
	if !errors.Is(release(), ErrBlocked) {
		t.Fatal("unknown owner released")
	}
	f.Close()
	recovered, done, err := l.AcquireForRecoveryContext(context.Background(), 0)
	if err == nil {
		defer done()
		called := false
		_ = WithForeground(recovered, nil, func([]string) error { called = true; return nil })
		if called {
			t.Fatal("recovery context reused retained proof after original descriptor closed")
		}
	}
}

func TestNodeIntentBindingSurvivesWithoutCancelRollbackContext(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "intent-rollback-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	l := NewNativeLease(root)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, release, err := l.AcquireContext(parent, 0)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(root, ".pending"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("node-operation-pending\n"); err != nil {
		t.Fatal(err)
	}
	binding, err := BindNodeIntent(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	called := false
	if err := WithForeground(context.WithoutCancel(ctx), nil, func([]string) error { called = true; return nil }); err != nil || !called {
		t.Fatal("rollback lost current intent", err)
	}
	if err := binding.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.Name()); err != nil {
		t.Fatal(err)
	}
	if err := binding.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

type lateIntentFailure struct{ nativeClaim }

func (f lateIntentFailure) BindNodeIntent(created *os.File) (NodeIntentBinding, error) {
	if _, err := f.nativeClaim.(nodeIntentClaim).BindNodeIntent(created); err != nil {
		return nil, err
	}
	return nil, errors.New("synthetic failure after complete RAM publication")
}

func TestLateIntentBindingFailureCannotExposeProofToRecovery(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "intent-late-failure-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	l := NewNativeLease(root)
	ctx, release, err := l.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	l.held = lateIntentFailure{l.held}
	f, err := os.OpenFile(filepath.Join(root, ".pending"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("node-operation-pending\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := BindNodeIntent(ctx, f); !errors.Is(err, ErrOwnershipLost) {
		t.Fatal("binding failure hidden", err)
	}
	if _, err := os.Stat(filepath.Join(root, "operation.lock.d", "node-intent")); err != nil {
		t.Fatal("fixture did not publish proof", err)
	}
	if !errors.Is(release(), ErrBlocked) {
		t.Fatal("ambiguous owner released")
	}
	if _, _, err := l.AcquireForRecoveryContext(context.Background(), 0); !errors.Is(err, ErrBlocked) {
		t.Fatal("ambiguous proof was adopted", err)
	}
}
