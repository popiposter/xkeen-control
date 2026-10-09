package authority

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExternalAdmissionFenceAlsoBlocksRecovery(t *testing.T) {
	l := NewLease()
	blocked := true
	l.Admission = func() error {
		if blocked {
			return ErrBlocked
		}
		return nil
	}
	l.Block()
	l.Unblock()
	if _, e := l.AcquireForRecovery(context.Background(), 0); !errors.Is(e, ErrBlocked) {
		t.Fatal("recovery bypassed durable external fence", e)
	}
	if _, e := l.TryAcquire(); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	blocked = false
	release, e := l.TryAcquire()
	if e != nil {
		t.Fatal(e)
	}
	release()
}

func TestTryAcquireIsImmediateAndPreservesBlockingAcquire(t *testing.T) {
	lease := NewLease()
	release, err := lease.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := lease.TryAcquire(); !errors.Is(err, ErrBusy) {
		t.Fatalf("TryAcquire while owned = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("TryAcquire waited for an owned lease: %s", elapsed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := lease.Acquire(ctx, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ordinary Acquire while owned = %v", err)
	}
	release()

	release, err = lease.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire after release = %v", err)
	}
	release()
}

func TestTryAcquireFailsClosedDuringRecoveryBlock(t *testing.T) {
	lease := NewLease()
	lease.Block()
	if _, err := lease.TryAcquire(); !errors.Is(err, ErrBlocked) {
		t.Fatalf("TryAcquire during recovery block = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := lease.Acquire(ctx, 0); !errors.Is(err, ErrBlocked) {
		t.Fatalf("ordinary Acquire during recovery block = %v", err)
	}
	release, err := lease.AcquireForRecovery(context.Background(), 0)
	if err != nil {
		t.Fatalf("recovery Acquire during block = %v", err)
	}
	release()
	lease.Unblock()
	if release, err := lease.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire after recovery = %v", err)
	} else {
		release()
	}
}

func TestDefaultLeaseUnblockWhileOwnedPreservesLegacyBehavior(t *testing.T) {
	lease := NewLease()
	release, err := lease.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	lease.Block()
	lease.Unblock()
	release()
	next, err := lease.TryAcquire()
	if err != nil {
		t.Fatalf("default Unblock changed legacy behavior: %v", err)
	}
	next()
}
