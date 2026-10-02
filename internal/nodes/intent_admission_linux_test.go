//go:build linux

package nodes

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestNativeIntentBindingCoversActivationRollbackAndSettlement(t *testing.T) {
	for _, outcome := range []string{"success", "rollback", "unknown", "replacement"} {
		t.Run(outcome, func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "node-intent-gate-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			m, _, _ := testManager(t, nil, nil)
			m.authority = authority.NewNativeLease(root)
			pending := filepath.Join(m.tx.PreviousDir, ".pending")
			proof := filepath.Join(root, "operation.lock.d", "node-intent")
			var first string
			a := &fakeActivator{onRestart: func(call int) {
				data, err := os.ReadFile(proof)
				if err != nil {
					t.Fatalf("current intent has no RAM binding: %v", err)
				}
				owner, err := os.ReadFile(filepath.Join(root, "operation.lock.d", "owner"))
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(pending)
				if err != nil {
					t.Fatal(err)
				}
				st := info.Sys().(*syscall.Stat_t)
				content, err := os.ReadFile(pending)
				if err != nil {
					t.Fatal(err)
				}
				want := fmt.Sprintf("v1 %x %d %d %d %x\n", sha256.Sum256(owner), st.Dev, st.Ino, info.Size(), sha256.Sum256(content))
				if string(data) != want {
					t.Fatal("binding does not describe exact live owner and file")
				}
				if call == 1 {
					first = string(data)
				} else if first != string(data) {
					t.Fatal("rollback reminted the binding")
				}
				if outcome == "replacement" {
					if err := os.Rename(pending, pending+".retained-original"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(pending, content, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}}
			if outcome == "rollback" {
				a.restartErrs = []error{errors.New("known native failure"), nil}
			}
			if outcome == "unknown" {
				a.restartErr = xkeen.ErrLifecycleUnknown
			}
			m.tx.Activator = a
			p, err := m.PreviewImport("session", syntheticProfile)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Apply(context.Background(), "session", p.Token, false)
			retained := outcome == "unknown" || outcome == "replacement"
			if retained {
				if !errors.Is(err, ErrNodeRecoveryRequired) {
					t.Fatalf("unsettled intent accepted: %v", err)
				}
				for _, path := range []string{pending, proof} {
					if _, err := os.Lstat(path); err != nil {
						t.Fatal("retained evidence lost", path)
					}
				}
				fds, err := os.ReadDir("/proc/self/fd")
				if err != nil {
					t.Fatal(err)
				}
				for _, fd := range fds {
					target, _ := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
					if target == pending || target == pending+".retained-original" {
						t.Fatal("failed operation leaked its intent descriptor")
					}
				}
			} else {
				if outcome == "success" && err != nil {
					t.Fatal(err)
				}
				if outcome == "rollback" && (err == nil || !strings.Contains(err.Error(), "previous generation restored") || a.restarts != 2) {
					t.Fatalf("rollback: %v calls=%d", err, a.restarts)
				}
				for _, path := range []string{pending, filepath.Dir(proof)} {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("settled metadata retained", path, err)
					}
				}
			}
		})
	}
}

func TestNativeReconciliationCreatesAndSettlesItsOwnIntentBinding(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "node-reconcile-intent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	m, _, active := testManager(t, &old, nil)
	m.authority = authority.NewNativeLease(root)
	m.tx.ConfigDir = filepath.Dir(active)
	if err := os.MkdirAll(m.tx.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := Render(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, data, 0600); err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(root, "operation.lock.d", "node-intent")
	m.tx.Activator = &fakeActivator{onRestart: func(int) {
		if _, err := os.Stat(proof); err != nil {
			t.Fatal("reconciliation bypassed binding", err)
		}
	}}
	if err := m.ReconcileRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(proof)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reconciliation retained gate", err)
	}
}

func TestPreexistingIntentNeverGetsCurrentRAMBinding(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "node-stale-intent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	m, _, _ := testManager(t, nil, nil)
	m.authority = authority.NewNativeLease(root)
	if err := os.MkdirAll(m.tx.PreviousDir, 0700); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(m.tx.PreviousDir, ".pending")
	if err := os.WriteFile(pending, []byte("node-operation-pending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := m.PreviewImport("session", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(context.Background(), "session", p.Token, false); !errors.Is(err, ErrNodeRecoveryRequired) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "operation.lock.d", "node-intent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale intent adopted")
	}
}
