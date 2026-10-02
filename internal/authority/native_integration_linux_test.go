//go:build linux

package authority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority/nativegate"
)

func nativeTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "authority-native-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

func TestNativeContextAdmissionAndForegroundProjection(t *testing.T) {
	root := nativeTestRoot(t)
	lease := NewNativeLease(root)
	ctx, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nativegate.Acquire(root, nativegate.Start); !errors.Is(err, nativegate.ErrBusy) {
		t.Fatalf("external contender = %v", err)
	}
	err = WithForeground(ctx, []string{"KEEP=yes", "XKEEN_GATE_ROOT=/tmp/forged", "XKEEN_GATE_TOKEN=forged"}, func(env []string) error {
		joined := strings.Join(env, "|")
		if !strings.Contains(joined, "KEEP=yes") || !strings.Contains(joined, "XKEEN_GATE_ROOT="+root) || strings.Contains(joined, "forged") {
			t.Fatalf("unsafe projection")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatalf("duplicate release = %v", err)
	}
	called := false
	if err := WithForeground(ctx, nil, func([]string) error { called = true; return nil }); !errors.Is(err, ErrOwnershipLost) || called {
		t.Fatalf("released context admitted: %v called=%v", err, called)
	}
	if _, err := lease.Acquire(context.Background(), 0); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("legacy native Acquire = %v", err)
	}
	if _, err := lease.TryAcquire(); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("legacy native TryAcquire = %v", err)
	}
}

func TestNativeRecoveryRetainsSameOwnerAndRejectsForeign(t *testing.T) {
	root := nativeTestRoot(t)
	lease := NewNativeLease(root)
	ctx, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(root, "operation.lock.d", "owner")
	before, _ := os.ReadFile(ownerPath)
	BlockContext(ctx)
	if err := release(); !errors.Is(err, ErrBlocked) {
		t.Fatalf("retained release = %v", err)
	}
	lease.Unblock() // not a recovery capability
	if _, _, err := lease.AcquireContext(context.Background(), 0); !errors.Is(err, ErrBlocked) {
		t.Fatalf("unblock escaped retained ownership: %v", err)
	}
	foreign := NewNativeLease(root)
	if _, _, err := foreign.AcquireForRecoveryContext(context.Background(), 0); !errors.Is(err, ErrBusy) {
		t.Fatalf("foreign recovery = %v", err)
	}
	recovery, done, err := lease.AcquireForRecoveryContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(ownerPath)
	if string(before) != string(after) {
		t.Fatal("recovery replaced native owner")
	}
	if err := WithForeground(recovery, nil, func([]string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	lease.Unblock()
	if err := done(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatal("resolved owner retained")
	}
}

func TestNativeReleaseFailureCannotBeUnblockedOrRecoveredBlindly(t *testing.T) {
	root := nativeTestRoot(t)
	lease := NewNativeLease(root)
	_, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "operation.lock.d", "unknown"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := release(); !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("release failure = %v", err)
	}
	lease.Unblock()
	if _, _, err := lease.AcquireForRecoveryContext(context.Background(), 0); !errors.Is(err, ErrBlocked) {
		t.Fatalf("fault recovery bypass = %v", err)
	}
	if _, _, err := lease.TryAcquireContext(context.Background()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("fault normal bypass = %v", err)
	}
}

func TestNativeReleasedContextCannotBlockLaterOwner(t *testing.T) {
	lease := NewNativeLease(nativeTestRoot(t))
	old, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	_, done, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	BlockContext(old)
	if err := done(); err != nil {
		t.Fatalf("old context blocked replacement: %v", err)
	}
}

func TestNativeForegroundHoldsCapabilityUntilCallbackEnds(t *testing.T) {
	root := nativeTestRoot(t)
	lease := NewNativeLease(root)
	ctx, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	entered, finish, ran := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { ran <- WithForeground(ctx, nil, func([]string) error { close(entered); <-finish; return nil }) }()
	<-entered
	released := make(chan error, 1)
	go func() { released <- release() }()
	if _, err := nativegate.Acquire(root, nativegate.Stop); !errors.Is(err, nativegate.ErrBusy) {
		t.Fatalf("foreground lost native ownership: %v", err)
	}
	close(finish)
	if err := <-ran; err != nil {
		t.Fatal(err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
}

func TestForegroundWithoutOwnedContextStripsInheritedTokens(t *testing.T) {
	if err := WithForeground(context.Background(), []string{"KEEP=yes", "XKEEN_GATE_ROOT=/tmp/forged", "XKEEN_GATE_TOKEN=forged"}, func(env []string) error {
		if strings.Join(env, "|") != "KEEP=yes" {
			t.Fatal("inherited token leaked")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCanceledAdmissionDoesNotCreateGate(t *testing.T) {
	root := nativeTestRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewNativeLease(root).AcquireContext(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "operation.lock.d")); !os.IsNotExist(err) {
		t.Fatal("cancelled admission wrote native gate")
	}
}

func TestNativeRecoveryUnknownRevokesCurrentCapability(t *testing.T) {
	for _, useContext := range []bool{false, true} {
		root := nativeTestRoot(t)
		lease := NewNativeLease(root)
		_, release, err := lease.AcquireContext(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		lease.Block()
		if err := release(); !errors.Is(err, ErrBlocked) {
			t.Fatal(err)
		}
		recovery, finish, err := lease.AcquireForRecoveryContext(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		// A new unknown result invalidates this recovery attempt. Its former
		// permission to inspect retained ownership cannot authorize more work.
		if useContext {
			BlockContext(recovery)
		} else {
			lease.Block()
		}
		lease.Unblock()
		called := false
		err = WithForeground(recovery, nil, func([]string) error { called = true; return nil })
		if !errors.Is(err, ErrBlocked) || called {
			t.Fatalf("unknown recovery context admitted: context=%v err=%v called=%v", useContext, err, called)
		}
		if err := finish(); !errors.Is(err, ErrBlocked) {
			t.Fatalf("unknown recovery released claim: %v", err)
		}
		if _, err := nativegate.Acquire(root, nativegate.Start); !errors.Is(err, nativegate.ErrBusy) {
			t.Fatalf("unknown recovery lost gate: %v", err)
		}
	}
}
