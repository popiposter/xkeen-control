package nodes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type fakeActivator struct {
	validateErr    error
	restartErr     error
	readyErr       error
	inventoryErr   error
	validate       func(context.Context) error
	onRestart      func(int)
	restartErrs    []error
	readyErrs      []error
	inventoryErrs  []error
	validatedPath  string
	policySeen     []byte
	outboundsSeen  []byte
	restarts       int
	readyCalls     int
	inventoryCalls int
}

func (f *fakeActivator) ValidateCandidate(ctx context.Context, path string) error {
	f.validatedPath = path
	f.policySeen, _ = os.ReadFile(filepath.Join(path, "05_routing.json"))
	f.outboundsSeen, _ = os.ReadFile(filepath.Join(path, "04_outbounds.json"))
	if f.validate != nil {
		return f.validate(ctx)
	}
	if f.validateErr != nil {
		return f.validateErr
	}
	return nil
}
func (f *fakeActivator) Restart(context.Context) error {
	f.restarts++
	if f.onRestart != nil {
		f.onRestart(f.restarts)
	}
	if f.restarts <= len(f.restartErrs) {
		return f.restartErrs[f.restarts-1]
	}
	return f.restartErr
}
func (f *fakeActivator) WaitReady(context.Context) error {
	f.readyCalls++
	if f.readyCalls <= len(f.readyErrs) {
		return f.readyErrs[f.readyCalls-1]
	}
	return f.readyErr
}
func (f *fakeActivator) VerifyOutboundTags(context.Context, []string) error {
	f.inventoryCalls++
	if f.inventoryCalls <= len(f.inventoryErrs) {
		return f.inventoryErrs[f.inventoryCalls-1]
	}
	return f.inventoryErr
}

func TestTransactionPreservesPolicyAndRollsBackActivationFailure(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	policy := []byte(`{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"strategy":{"type":"leastPing"}}]}}`)
	if err := os.WriteFile(filepath.Join(configDir, "05_routing.json"), policy, 0o600); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(configDir, "04_outbounds.json")
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-44444444", true)}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	oldRendered, err := Render(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, oldRendered, 0o600); err != nil {
		t.Fatal(err)
	}
	newRegistry := NewRegistry()
	newRegistry.Nodes = []Node{testNode(t, syntheticProfileTwo, "node-55555555", true)}
	activator := &fakeActivator{}
	tx := Transaction{Store: store, ActiveOutboundsPath: active, ConfigDir: configDir, PreviousDir: filepath.Join(dir, "previous"), Activator: activator}
	if err := tx.Apply(context.Background(), newRegistry); err != nil {
		t.Fatal(err)
	}
	if activator.validatedPath == "" {
		t.Fatal("candidate validation was not invoked")
	}
	if string(activator.policySeen) != string(policy) || !strings.Contains(string(activator.outboundsSeen), "proxy-node-55555555") {
		t.Fatal("candidate policy or outbounds were not validated as expected")
	}
	activeAfter, _ := os.ReadFile(active)
	if !strings.Contains(string(activeAfter), "proxy-node-55555555") {
		t.Fatal("new generated outbound was not activated")
	}
	if got, _ := os.ReadFile(filepath.Join(configDir, "05_routing.json")); string(got) != string(policy) {
		t.Fatal("active routing policy was modified")
	}

	rollbackActivator := &fakeActivator{restartErr: errors.New("synthetic restart failure")}
	tx.Activator = rollbackActivator
	if err := tx.Apply(context.Background(), old); err == nil {
		t.Fatal("restart failure unexpectedly succeeded")
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Nodes) != 1 || loaded.Nodes[0].ID != "node-55555555" {
		t.Fatalf("registry was not rolled back to pre-failure generation: %+v, %v", loaded, err)
	}
	activeAfterFailure, _ := os.ReadFile(active)
	if string(activeAfterFailure) != string(activeAfter) {
		t.Fatal("failed activation did not restore the current working generation")
	}

	runtimeFailure := &fakeActivator{inventoryErr: errors.New("balancer does not expose expected tags")}
	tx.Activator = runtimeFailure
	if err := tx.Apply(context.Background(), old); err == nil {
		t.Fatal("runtime balancer verification failure unexpectedly succeeded")
	}
	loaded, err = store.Load()
	if err != nil || loaded.Nodes[0].ID != "node-55555555" {
		t.Fatalf("runtime verification failure did not roll back registry: %+v, %v", loaded, err)
	}
	activeAfterRuntimeFailure, _ := os.ReadFile(active)
	if string(activeAfterRuntimeFailure) != string(activeAfter) || runtimeFailure.restarts != 2 {
		t.Fatalf("runtime verification rollback = restarts:%d active-restored:%t", runtimeFailure.restarts, string(activeAfterRuntimeFailure) == string(activeAfter))
	}
}

func TestTransactionRejectsCandidateBeforePersistentWrites(t *testing.T) {
	dir := t.TempDir()
	store := Store{Path: filepath.Join(dir, "nodes.json")}
	registry := NewRegistry()
	registry.Nodes = []Node{testNode(t, syntheticProfile, "node-66666666", true)}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(dir, "04_outbounds.json")
	old, _ := Render(registry)
	if err := os.WriteFile(active, old, 0o600); err != nil {
		t.Fatal(err)
	}
	candidate := NewRegistry()
	candidate.Nodes = []Node{testNode(t, syntheticProfileTwo, "node-77777777", true)}
	tx := Transaction{Store: store, ActiveOutboundsPath: active, PreviousDir: filepath.Join(dir, "previous"), Activator: &fakeActivator{validateErr: errors.New("bad candidate")}}
	if err := tx.Apply(context.Background(), candidate); err == nil {
		t.Fatal("invalid candidate was accepted")
	}
	loaded, err := store.Load()
	if err != nil || loaded.Nodes[0].ID != "node-66666666" {
		t.Fatal("registry changed before candidate validation")
	}
	got, _ := os.ReadFile(active)
	if string(got) != string(old) {
		t.Fatal("active outbounds changed before candidate validation")
	}
}

func TestTransactionReportsRollbackFailures(t *testing.T) {
	t.Run("successful recovery is confirmed", func(t *testing.T) {
		activator := &fakeActivator{restartErrs: []error{errors.New("synthetic primary failure"), nil}}
		tx, candidate, old, active := rollbackFixture(t, activator)
		err := tx.Apply(context.Background(), candidate)
		if err == nil || !strings.Contains(err.Error(), "previous generation restored") || errors.Is(err, ErrRollbackFailed) {
			t.Fatalf("successful rollback error = %v", err)
		}
		assertGeneration(t, tx.Store, active, old)
		if activator.restarts != 2 || activator.readyCalls != 1 || activator.inventoryCalls != 1 {
			t.Fatalf("rollback verification calls = restart:%d ready:%d inventory:%d", activator.restarts, activator.readyCalls, activator.inventoryCalls)
		}
	})

	t.Run("restore failure", func(t *testing.T) {
		activator := &fakeActivator{restartErrs: []error{errors.New("synthetic primary failure"), nil}}
		tx, candidate, _, _ := rollbackFixture(t, activator)
		registryDir := filepath.Dir(tx.Store.Path)
		activator.onRestart = func(call int) {
			if call != 1 {
				return
			}
			if err := os.RemoveAll(registryDir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(registryDir, []byte("blocks directory recreation"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		assertRollbackFailed(t, tx.Apply(context.Background(), candidate))
	})

	t.Run("rollback restart failure", func(t *testing.T) {
		activator := &fakeActivator{
			restartErrs:   []error{nil, errors.New("synthetic rollback restart failure")},
			inventoryErrs: []error{errors.New("synthetic primary inventory failure"), nil},
		}
		tx, candidate, old, active := rollbackFixture(t, activator)
		assertRollbackFailed(t, tx.Apply(context.Background(), candidate))
		assertGeneration(t, tx.Store, active, old)
	})

	t.Run("rollback readiness failure", func(t *testing.T) {
		activator := &fakeActivator{
			readyErrs:     []error{nil, errors.New("synthetic rollback readiness failure")},
			inventoryErrs: []error{errors.New("synthetic primary inventory failure"), nil},
		}
		tx, candidate, old, active := rollbackFixture(t, activator)
		assertRollbackFailed(t, tx.Apply(context.Background(), candidate))
		assertGeneration(t, tx.Store, active, old)
	})
}

func TestTransactionBoundsHangingCandidateValidator(t *testing.T) {
	activator := &fakeActivator{validate: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	tx, candidate, old, active := rollbackFixture(t, activator)
	tx.Budget = TransactionBudget{
		CandidateValidation: 30 * time.Millisecond,
		Activation:          100 * time.Millisecond,
		Rollback:            100 * time.Millisecond,
		Total:               250 * time.Millisecond,
	}
	started := time.Now()
	err := tx.Apply(context.Background(), candidate)
	if err == nil || !strings.Contains(err.Error(), "candidate Xray validation failed") {
		t.Fatalf("hanging validator error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("hanging validator exceeded its budget: %s", elapsed)
	}
	assertGeneration(t, tx.Store, active, old)
}

type deadlineActivator struct {
	restartRemaining []time.Duration
}

func (*deadlineActivator) ValidateCandidate(context.Context, string) error { return nil }
func (a *deadlineActivator) Restart(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	a.restartRemaining = append(a.restartRemaining, time.Until(deadline))
	<-ctx.Done()
	return ctx.Err()
}
func (*deadlineActivator) WaitReady(ctx context.Context) error { return ctx.Err() }
func (*deadlineActivator) VerifyOutboundTags(ctx context.Context, _ []string) error {
	return ctx.Err()
}

func TestTransactionTotalBudgetIncludesRollback(t *testing.T) {
	activator := &deadlineActivator{}
	tx, candidate, old, active := rollbackFixture(t, activator)
	tx.Budget = TransactionBudget{
		CandidateValidation: 10 * time.Millisecond,
		Activation:          60 * time.Millisecond,
		Rollback:            60 * time.Millisecond,
		Total:               85 * time.Millisecond,
	}
	started := time.Now()
	err := tx.Apply(context.Background(), candidate)
	elapsed := time.Since(started)
	assertRollbackFailed(t, err)
	if elapsed > 250*time.Millisecond {
		t.Fatalf("transaction exceeded hard total budget: %s", elapsed)
	}
	if len(activator.restartRemaining) != 2 {
		t.Fatalf("restart calls = %d", len(activator.restartRemaining))
	}
	if activator.restartRemaining[1] >= 45*time.Millisecond {
		t.Fatalf("rollback received a fresh phase budget instead of the remaining total: %s", activator.restartRemaining[1])
	}
	assertGeneration(t, tx.Store, active, old)
}

func rollbackFixture(t *testing.T, activator Activator) (Transaction, Registry, Registry, string) {
	t.Helper()
	dir := t.TempDir()
	store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	old := NewRegistry()
	old.Nodes = []Node{testNode(t, syntheticProfile, "node-12121212", true)}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(dir, "xray", "04_outbounds.json")
	oldRendered, err := Render(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(active, oldRendered, 0o600); err != nil {
		t.Fatal(err)
	}
	candidate := NewRegistry()
	candidate.Nodes = []Node{testNode(t, syntheticProfileTwo, "node-34343434", true)}
	tx := Transaction{Store: store, ActiveOutboundsPath: active, PreviousDir: filepath.Join(dir, "previous"), Activator: activator}
	return tx, candidate, old, active
}

func assertRollbackFailed(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrRollbackFailed) || strings.Contains(err.Error(), "restored") {
		t.Fatalf("rollback failure was not explicit: %v", err)
	}
}

func assertGeneration(t *testing.T, store Store, active string, expected Registry) {
	t.Helper()
	loaded, err := store.Load()
	if err != nil || len(loaded.Nodes) != 1 || loaded.Nodes[0].ID != expected.Nodes[0].ID {
		t.Fatalf("registry generation = %+v, %v", loaded, err)
	}
	expectedRendered, err := Render(expected)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(active)
	if err != nil || string(contents) != string(expectedRendered) {
		t.Fatal("outbound generation was not restored")
	}
}

func TestXrayEnvironmentPinsAssetDirectory(t *testing.T) {
	environment := xrayEnvironment("/opt/etc/xray/dat")
	found := ""
	for _, entry := range environment {
		if strings.HasPrefix(entry, "XRAY_LOCATION_ASSET=") {
			found = entry
		}
	}
	if found != "XRAY_LOCATION_ASSET=/opt/etc/xray/dat" {
		t.Fatalf("asset directory was not pinned: %q", found)
	}
}

func TestCommandActivatorRestartIsForegroundAndBounded(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	fakeXkeen := filepath.Join(dir, "xkeen")
	script := `#!/bin/sh
[ "${XKEEN_FOREGROUND:-}" = "1" ] || exit 91
printf started > "$XKEEN_FAKE_MARKER"
sleep "${XKEEN_FAKE_DELAY:-0}"
printf -- '-completed' >> "$XKEEN_FAKE_MARKER"
`
	if err := os.WriteFile(fakeXkeen, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XKEEN_FOREGROUND", "0")
	t.Setenv("XKEEN_FAKE_MARKER", marker)
	t.Setenv("XKEEN_FAKE_DELAY", "0.15")
	activator := CommandActivator{XkeenBinary: fakeXkeen, RestartTimeout: time.Second}
	started := time.Now()
	if err := activator.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Fatalf("foreground restart returned early after %s", elapsed)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "started-completed" {
		t.Fatalf("foreground marker = %q, %v", contents, err)
	}

	t.Setenv("XKEEN_FAKE_DELAY", "2")
	activator.RestartTimeout = 50 * time.Millisecond
	started = time.Now()
	if err := activator.Restart(context.Background()); err == nil {
		t.Fatal("restart timeout unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("restart timeout was not bounded: %s", elapsed)
	}
}

func TestCommandActivatorFallsBackToStartAfterFailedRestart(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	fakeXkeen := filepath.Join(dir, "xkeen")
	script := `#!/bin/sh
[ "${XKEEN_FOREGROUND:-}" = "1" ] || exit 91
case "$1" in
  -restart) printf restart > "$XKEEN_FAKE_MARKER"; exit 1 ;;
  -start) printf -- '-start' >> "$XKEEN_FAKE_MARKER"; exit 0 ;;
  *) exit 92 ;;
esac
`
	if err := os.WriteFile(fakeXkeen, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XKEEN_FAKE_MARKER", marker)
	activator := CommandActivator{XkeenBinary: fakeXkeen, RestartTimeout: time.Second}
	if err := activator.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "restart-start" {
		t.Fatalf("restart/start marker = %q, %v", contents, err)
	}
}

func TestCommandActivatorReservesTimeForStartAfterHangingRestart(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	fakeXkeen := filepath.Join(dir, "xkeen")
	script := `#!/bin/sh
case "$1" in
  -restart) sleep 2 ;;
  -start) printf start > "$XKEEN_FAKE_MARKER"; exit 0 ;;
  *) exit 92 ;;
esac
`
	if err := os.WriteFile(fakeXkeen, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XKEEN_FAKE_MARKER", marker)
	activator := CommandActivator{
		XkeenBinary:           fakeXkeen,
		RestartTimeout:        250 * time.Millisecond,
		RestartAttemptTimeout: 50 * time.Millisecond,
	}
	started := time.Now()
	if err := activator.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("restart/start fallback exceeded total budget: %s", elapsed)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "start" {
		t.Fatalf("fallback marker = %q, %v", contents, err)
	}
}

func TestCommandActivatorFixedLifecycleUsesOnlyPreservedInit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fixed-init execution fixture requires the Linux qualification environment")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	forbidden := filepath.Join(dir, "candidate-ran")
	initPath := filepath.Join(dir, "S05xkeen")
	candidate := filepath.Join(dir, "candidate")
	if err := os.WriteFile(candidate, []byte("#!/bin/sh\nprintf candidate > \"$XKEEN_FORBIDDEN\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	initScript := "#!/bin/sh\nprintf '%s:%s' \"$1\" \"$2\" >> \"$XKEEN_FIXED_MARKER\"\ncase \"$1\" in\n  restart) exit 1 ;;\n  start) exit 0 ;;\n  *) exit 2 ;;\nesac\n"
	if err := os.WriteFile(initPath, []byte(initScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XKEEN_FIXED_MARKER", marker)
	t.Setenv("XKEEN_FORBIDDEN", forbidden)
	activator := CommandActivator{XkeenBinary: candidate, FixedLifecycleInit: initPath, RestartTimeout: time.Second, RestartAttemptTimeout: 100 * time.Millisecond}
	if err := activator.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "restart:onstart:on" {
		t.Fatalf("fixed-init arguments = %q, %v", contents, err)
	}
	if _, err := os.Stat(forbidden); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate executable was invoked: %v", err)
	}
}

func TestBalancerRuntimeAllowsEmptySelectionAndRejectsForeignTags(t *testing.T) {
	expected := []string{"proxy-node-88888888"}
	if !validBalancerRuntime(xrayapi.BalancerRuntime{}, expected) {
		t.Fatal("empty leastPing selection was treated as an activation failure")
	}
	if !validBalancerRuntime(xrayapi.BalancerRuntime{PrincipleTargets: expected, Override: expected[0]}, expected) {
		t.Fatal("expected runtime tags were rejected")
	}
	if validBalancerRuntime(xrayapi.BalancerRuntime{PrincipleTargets: []string{"proxy-node-foreign"}}, expected) {
		t.Fatal("foreign principle target was accepted")
	}
	if validBalancerRuntime(xrayapi.BalancerRuntime{Override: "proxy-node-foreign"}, expected) {
		t.Fatal("foreign override was accepted")
	}
}

func TestCommandActivatorVerifiesActiveOutboundTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "04_outbounds.json")
	if err := os.WriteFile(path, []byte(`{"outbounds":[{"tag":"proxy-node-88888888"},{"tag":"direct"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	routingPath := filepath.Join(dir, "05_routing.json")
	if err := os.WriteFile(routingPath, []byte(`{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"strategy":{"type":"leastPing"}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeCalls := 0
	activator := CommandActivator{ActiveOutboundsPath: path, RoutingPath: routingPath, RuntimeVerifier: func(_ context.Context, _ string, balancer string, expected []string) error {
		runtimeCalls++
		if balancer != "bal-proxy" || len(expected) != 1 || expected[0] != "proxy-node-88888888" {
			return errors.New("unexpected runtime verification request")
		}
		return nil
	}}
	if err := activator.VerifyOutboundTags(context.Background(), []string{"proxy-node-88888888"}); err != nil {
		t.Fatalf("active tags were rejected: %v", err)
	}
	if runtimeCalls != 1 {
		t.Fatalf("runtime verification calls = %d", runtimeCalls)
	}
	if err := activator.VerifyOutboundTags(context.Background(), []string{"proxy-node-missing"}); err == nil {
		t.Fatal("missing active tag was accepted")
	}
}

func TestCommandActivatorRejectsFileOnlyVisibility(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "04_outbounds.json")
	routing := filepath.Join(dir, "05_routing.json")
	if err := os.WriteFile(active, []byte(`{"outbounds":[{"tag":"proxy-node-88888888"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routing, []byte(`{"routing":{"balancers":[{"tag":"bal-proxy","selector":["legacy-"],"strategy":{"type":"leastPing"}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	activator := CommandActivator{ActiveOutboundsPath: active, RoutingPath: routing, RuntimeVerifier: func(context.Context, string, string, []string) error { return nil }}
	if err := activator.VerifyOutboundTags(context.Background(), []string{"proxy-node-88888888"}); err == nil {
		t.Fatal("tag present in JSON but invisible to bal-proxy selector was accepted")
	}
	activator.RoutingPath = filepath.Join(dir, "missing-routing.json")
	if err := activator.VerifyOutboundTags(context.Background(), []string{"proxy-node-88888888"}); err == nil {
		t.Fatal("missing runtime selector contract was accepted")
	}
}
