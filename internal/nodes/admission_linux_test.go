//go:build linux

package nodes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/authority/nativegate"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestApplyHoldsNativeAdmissionThroughValidationAndActivation(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "node-admission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	m, _, _ := testManager(t, nil, nil)
	m.authority = authority.NewNativeLease(root)
	validateCalls := 0
	m.tx.Activator = &fakeActivator{onRestart: func(int) {
		if foreign, err := nativegate.Acquire(root, nativegate.Restart); !errors.Is(err, nativegate.ErrBusy) {
			if foreign != nil {
				_ = foreign.Release()
			}
			t.Fatal("native writer entered during activation")
		}
	}, validate: func(ctx context.Context) error {
		validateCalls++
		if foreign, err := nativegate.Acquire(root, nativegate.Restart); !errors.Is(err, nativegate.ErrBusy) {
			if foreign != nil {
				_ = foreign.Release()
			}
			t.Fatal("native writer entered during validation")
		}
		return authority.WithForeground(ctx, []string{"XKEEN_GATE_TOKEN=untrusted"}, func(env []string) error {
			var token string
			for _, entry := range env {
				if strings.HasPrefix(entry, "XKEEN_GATE_TOKEN=") {
					token = strings.TrimPrefix(entry, "XKEEN_GATE_TOKEN=")
				}
			}
			if _, err := nativegate.Join(root, token); err != nil {
				t.Fatal("operation did not propagate valid foreground admission")
			}
			return nil
		})
	}}
	p, err := m.PreviewImport("session", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(context.Background(), "session", p.Token, false); err != nil {
		t.Fatal(err)
	}
	if validateCalls != 1 {
		t.Fatal("validation not run")
	}
	claim, err := nativegate.Acquire(root, nativegate.Restart)
	if err != nil {
		t.Fatalf("settled Apply retained gate: %v", err)
	}
	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
}

type lostNativeAdmissionActivator struct {
	fakeActivator
	root    string
	command CommandActivator
}

func (a *lostNativeAdmissionActivator) Restart(ctx context.Context) error {
	a.restarts++
	if err := os.WriteFile(filepath.Join(a.root, "operation.lock.d", "owner"), []byte("invalid synthetic owner\n"), 0600); err != nil {
		return err
	}
	return a.command.Restart(ctx)
}

func TestNativeAdmissionLossNeverRestoresWithoutOwnership(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "node-admission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	m, store, active := testManager(t, &old, nil)
	before, _ := Render(old)
	if err := os.MkdirAll(filepath.Dir(active), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, before, 0600); err != nil {
		t.Fatal(err)
	}
	m.authority = authority.NewNativeLease(root)
	calls := filepath.Join(root, "unexpected-call")
	binary := filepath.Join(root, "fake-xkeen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch '"+calls+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := &lostNativeAdmissionActivator{root: root, command: CommandActivator{XkeenBinary: binary}}
	m.tx.Activator = a
	p, err := m.PreviewImport("session", syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Apply(context.Background(), "session", p.Token, false)
	if !errors.Is(err, ErrNodeRecoveryRequired) || !errors.Is(err, xkeen.ErrLifecycleAdmission) {
		t.Fatalf("lost admission not retained for recovery: %v", err)
	}
	current, err := store.Load()
	if err != nil || len(current.Nodes) != 2 || a.restarts != 1 {
		t.Fatalf("lost ownership triggered rollback: nodes=%d lifecycle=%d error=%v", len(current.Nodes), a.restarts, err)
	}
	if _, err := os.Stat(calls); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost owner executed native command")
	}
	if _, err := os.Stat(filepath.Join(m.tx.PreviousDir, ".pending")); err != nil {
		t.Fatal("recovery intent lost")
	}

}

func TestApplyDoesNotReportSuccessWhenNativeReleaseFails(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "node-admission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	m, _, _ := testManager(t, nil, nil)
	m.authority = authority.NewNativeLease(root)
	m.tx.Activator = &fakeActivator{onRestart: func(int) {
		if err := os.WriteFile(filepath.Join(root, "operation.lock.d", "foreign-entry"), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}}
	p, err := m.PreviewImport("session", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Apply(context.Background(), "session", p.Token, false)
	if !errors.Is(err, ErrNodeRecoveryRequired) || len(result.Nodes) != 0 {
		t.Fatalf("release failure reported success: %v", err)
	}
	if _, _, err := m.authority.TryAcquireContext(context.Background()); !errors.Is(err, authority.ErrBlocked) {
		t.Fatal("release failure reopened authority")
	}
}
