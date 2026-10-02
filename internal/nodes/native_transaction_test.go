package nodes

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

type nativeTimeoutActivator struct {
	*fakeActivator
	command CommandActivator
}

func (a *nativeTimeoutActivator) Restart(ctx context.Context) error {
	a.restarts++
	return a.command.Restart(ctx)
}

func TestTransactionNativeTimeoutRetainsRecoveryAndNeverReplays(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native timeout fixture requires Linux")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "xkeen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(dir, "04_outbounds.json")
	before, _ := Render(old)
	if err := os.WriteFile(active, before, 0600); err != nil {
		t.Fatal(err)
	}
	next := NewRegistry()
	next.Nodes = []Node{testNode(t, syntheticProfileTwo, "node-22222222", true)}
	a := &nativeTimeoutActivator{fakeActivator: &fakeActivator{}, command: CommandActivator{XkeenBinary: binary, RestartTimeout: 100 * time.Millisecond, RestartAttemptTimeout: 20 * time.Millisecond}}
	tx := Transaction{Store: store, ActiveOutboundsPath: active, PreviousDir: filepath.Join(dir, "previous"), Activator: a}
	err := tx.Apply(context.Background(), next)
	if !errors.Is(err, xkeen.ErrLifecycleUnknown) || !errors.Is(err, ErrNodeRecoveryRequired) || a.restarts != 1 {
		t.Fatalf("unknown was retried or lost: %v calls=%d", err, a.restarts)
	}
	if got, err := os.ReadFile(filepath.Join(tx.PreviousDir, "04_outbounds.json")); err != nil || !bytes.Equal(got, before) {
		t.Fatal("previous snapshot lost")
	}
	current, err := store.Load()
	if err != nil || len(current.Nodes) != 1 || current.Nodes[0].ID != next.Nodes[0].ID {
		t.Fatal("unknown operation silently restored registry")
	}
	if err := tx.Apply(context.Background(), old); !errors.Is(err, ErrNodeRecoveryRequired) || a.restarts != 1 {
		t.Fatal("new apply overwrote unresolved operation")
	}
	m := &Manager{tx: tx, store: store}
	m.tx.ConfigDir = dir
	if err := m.ReconcileRuntime(context.Background()); !errors.Is(err, ErrNodeRecoveryRequired) || a.restarts != 1 {
		t.Fatal("reconciliation replayed unknown lifecycle")
	}
}

func TestReconciliationNativeTimeoutIsFirstOperationAndCannotReplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native timeout fixture requires Linux")
	}
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.Mkdir(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "xkeen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	registry := NewRegistry()
	registry.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(configDir, "04_outbounds.json")
	before, _ := Render(registry)
	if err := os.WriteFile(active, before, 0600); err != nil {
		t.Fatal(err)
	}
	a := &nativeTimeoutActivator{fakeActivator: &fakeActivator{}, command: CommandActivator{XkeenBinary: binary, RestartTimeout: 100 * time.Millisecond, RestartAttemptTimeout: 20 * time.Millisecond}}
	tx := Transaction{Store: store, ConfigDir: configDir, ActiveOutboundsPath: active, PreviousDir: filepath.Join(dir, "previous"), Activator: a}
	m := &Manager{tx: tx, store: store}
	if err := m.ReconcileRuntime(context.Background()); !errors.Is(err, xkeen.ErrLifecycleUnknown) || !errors.Is(err, ErrNodeRecoveryRequired) || a.restarts != 1 {
		t.Fatalf("first reconciliation unknown lost/replayed: %v calls=%d", err, a.restarts)
	}
	if err := m.ReconcileRuntime(context.Background()); !errors.Is(err, ErrNodeRecoveryRequired) || a.restarts != 1 {
		t.Fatal("reconciliation replayed")
	}
	if err := tx.Apply(context.Background(), registry); !errors.Is(err, ErrNodeRecoveryRequired) || a.restarts != 1 {
		t.Fatal("apply followed unresolved reconciliation")
	}
	if got, err := os.ReadFile(filepath.Join(tx.PreviousDir, "04_outbounds.json")); err != nil || !bytes.Equal(got, before) {
		t.Fatal("reconciliation snapshot lost")
	}
}
