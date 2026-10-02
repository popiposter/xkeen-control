//go:build linux

package xkeen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/authority/nativegate"
)

func TestLifecycleUsesOnlyContextBoundForegroundNativeAdmission(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "lifecycle-gate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	gateScript, err := filepath.Abs("../../scripts/native-operation-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "native")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Spoofed inherited context is stripped even when no lease was requested.
	t.Setenv("XKEEN_GATE_ROOT", "/tmp/forged")
	t.Setenv("XKEEN_GATE_TOKEN", "forged")
	write(`test -z "${XKEEN_GATE_ROOT+x}" && test -z "${XKEEN_GATE_TOKEN+x}"`)
	lifecycle := Lifecycle{Binary: binary, Timeout: time.Second}
	if err := lifecycle.Run(context.Background(), Start); err != nil {
		t.Fatalf("inherited gate context was forwarded: %v", err)
	}
	lease := authority.NewNativeLease(root)
	ctx, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	write(". '" + gateScript + "'\nnative_gate_join \"$XKEEN_GATE_ROOT\" \"$XKEEN_GATE_TOKEN\"")
	if err := lifecycle.Run(ctx, Restart); err != nil {
		t.Fatalf("context owner not forwarded: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Run(ctx, Restart); !errors.Is(err, authority.ErrOwnershipLost) {
		t.Fatalf("released context reused: %v", err)
	}
}

func TestLifecycleUnknownRetainsNativeGateBeforeRelease(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "lifecycle-unknown-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	binary := filepath.Join(root, "native")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	lease := authority.NewNativeLease(root)
	ctx, release, err := lease.AcquireContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Lifecycle{Binary: binary, Timeout: 20 * time.Millisecond}).Run(ctx, Stop); !errors.Is(err, ErrLifecycleUnknown) {
		t.Fatalf("timeout = %v", err)
	}
	if err := release(); !errors.Is(err, authority.ErrBlocked) {
		t.Fatalf("unknown owner released = %v", err)
	}
	if _, err := nativegate.Acquire(root, nativegate.Start); !errors.Is(err, nativegate.ErrBusy) {
		t.Fatalf("unknown owner gate lost: %v", err)
	}
}

func TestLifecycleAdmissionLossIsTypedAndNeverExecutes(t *testing.T) {
	for _, mode := range []string{"replaced-owner", "blocked", "released"} {
		t.Run(mode, func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "lifecycle-lost-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			marker := filepath.Join(root, "executed")
			binary := filepath.Join(root, "native")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf executed > '"+marker+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			lease := authority.NewNativeLease(root)
			ctx, release, err := lease.AcquireContext(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			underlying := authority.ErrOwnershipLost
			switch mode {
			case "replaced-owner":
				if err := os.WriteFile(filepath.Join(root, "operation.lock.d", "owner"), []byte("unresolved\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "blocked":
				lease.Block()
				underlying = authority.ErrBlocked
			case "released":
				if err := release(); err != nil {
					t.Fatal(err)
				}
			}
			err = (Lifecycle{Binary: binary, Timeout: time.Second}).Run(ctx, Restart)
			if !errors.Is(err, ErrLifecycleAdmission) || !errors.Is(err, underlying) {
				t.Fatalf("admission loss not classified: %v", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("unadmitted lifecycle command executed")
			}
		})
	}
}

func TestLifecycleNativeGateExitCodesRetainAdmission(t *testing.T) {
	for _, code := range []int{75, 76, 77, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "lifecycle-exit-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			binary := filepath.Join(root, "native")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit "+strconv.Itoa(code)+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			lease := authority.NewNativeLease(root)
			ctx, release, err := lease.AcquireContext(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			err = (Lifecycle{Binary: binary, Timeout: time.Second}).Run(ctx, Restart)
			if code == 7 {
				if !errors.Is(err, ErrLifecycleFailed) || errors.Is(err, ErrLifecycleAdmission) {
					t.Fatalf("ordinary exit classification = %v", err)
				}
				if err := release(); err != nil {
					t.Fatalf("ordinary failure poisoned ownership: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrLifecycleAdmission) || errors.Is(err, ErrLifecycleFailed) {
				t.Fatalf("native gate exit %d lost admission classification: %v", code, err)
			}
			called := false
			if err := authority.WithForeground(ctx, nil, func([]string) error { called = true; return nil }); !errors.Is(err, authority.ErrBlocked) || called {
				t.Fatalf("native refusal did not revoke context: %v called=%v", err, called)
			}
			if err := release(); !errors.Is(err, authority.ErrBlocked) {
				t.Fatalf("native refusal released ownership: %v", err)
			}
			if _, err := nativegate.Acquire(root, nativegate.Start); !errors.Is(err, nativegate.ErrBusy) {
				t.Fatalf("native refusal lost retained gate: %v", err)
			}
		})
	}
}
