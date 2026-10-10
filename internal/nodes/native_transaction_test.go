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
	testNativeTimeout(t, false)
}
func TestTransactionNativeFallbackTimeoutRetainsRecoveryAndNeverReplays(t *testing.T) {
	testNativeTimeout(t, true)
}
func testNativeTimeout(t *testing.T, fallback bool) {
	if runtime.GOOS != "linux" {
		t.Skip("native timeout fixture requires Linux")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "xkeen")
	calls := filepath.Join(dir, "calls")
	t.Setenv("NATIVE_TEST_CALLS", calls)
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$NATIVE_TEST_CALLS\"\nsleep 2\n"
	if fallback {
		script = "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$NATIVE_TEST_CALLS\"\n[ \"$1\" = -restart ] && exit 7\nsleep 2\n"
	}
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(configDir, "04_outbounds.json")
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
	wantCalls := "-restart\n"
	if fallback {
		wantCalls += "-start\n"
	}
	if got, err := os.ReadFile(calls); err != nil || string(got) != wantCalls {
		t.Fatal("unexpected native lifecycle sequence")
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

func TestMetadataChangesPreserveRuntimeBytesAndNeverRestart(t *testing.T) {
	dir := t.TempDir()
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	old.Subscriptions = []Subscription{{ID: "sub-11111111", Name: "Provider", URL: "https://fixture.invalid/sub", Enabled: true}}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(configDir, "04_outbounds.json")
	before, _ := Render(old)
	before = append([]byte("// untouched native comment\n"), before...)
	if err := os.WriteFile(active, before, 0600); err != nil {
		t.Fatal(err)
	}
	next := old
	next.Nodes = append([]Node(nil), old.Nodes...)
	next.Nodes[0].Name = "New display name"
	next.Subscriptions = append([]Subscription(nil), old.Subscriptions...)
	next.Subscriptions[0].Name = "Renamed provider"
	a := &fakeActivator{validateErr: errors.New("must not validate metadata"), restartErr: errors.New("must not restart metadata")}
	signals := 0
	tx := Transaction{Store: store, ActiveOutboundsPath: active, PreviousDir: filepath.Join(dir, "previous"), Activator: a, OnRuntimeChange: func() { signals++ }}
	if err := tx.Apply(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if a.restarts != 0 || a.validatedPath != "" {
		t.Fatal("metadata invoked native service")
	}
	if signals != 0 {
		t.Fatal("a metadata-only commit signalled a quality review")
	}
	if got, _ := os.ReadFile(active); !bytes.Equal(got, before) {
		t.Fatal("metadata rewrote runtime bytes")
	}
	current, err := store.Load()
	if err != nil || current.Nodes[0].Name != next.Nodes[0].Name {
		t.Fatal("metadata not saved")
	}
	next.Nodes[0].Enabled = false
	a.validateErr = nil
	a.restartErr = nil
	if err := tx.Apply(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if a.restarts != 1 || a.validatedPath == "" {
		t.Fatal("real runtime change skipped activation")
	}
	if signals != 1 {
		t.Fatal("a committed runtime change did not signal exactly once", signals)
	}
	// A transaction refused by validation commits nothing and must not signal.
	next.Nodes[0].Enabled = true
	a.validateErr = errors.New("synthetic validation refusal")
	if err := tx.Apply(context.Background(), next); err == nil {
		t.Fatal("refused transaction committed")
	}
	if signals != 1 {
		t.Fatal("a refused transaction signalled a quality review", signals)
	}
}
