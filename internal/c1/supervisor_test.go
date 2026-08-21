package c1

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type supervisorReader struct {
	snapshot xrayapi.Snapshot
}

func (r *supervisorReader) Snapshot(context.Context) xrayapi.Snapshot { return r.snapshot }
func (r *supervisorReader) ProbeReachable(context.Context) bool       { return true }

type supervisorAPI struct {
	reader   *supervisorReader
	override []string
	adds     []xrayapi.Rule
	removes  []string
	rules    map[string]xrayapi.Rule
}

func (a *supervisorAPI) OverrideBalancerTarget(_ context.Context, _ string, target string) error {
	a.override = append(a.override, target)
	a.reader.snapshot.Balancer.Override = target
	return nil
}

func (a *supervisorAPI) AddRule(_ context.Context, rule xrayapi.Rule, appendOnly bool) error {
	if !appendOnly {
		return errors.New("probe rule was not append-only")
	}
	a.adds = append(a.adds, rule)
	if a.rules == nil {
		a.rules = make(map[string]xrayapi.Rule)
	}
	a.rules[rule.RuleTag] = rule
	return nil
}

func (a *supervisorAPI) RemoveRule(_ context.Context, tag string) error {
	a.removes = append(a.removes, tag)
	delete(a.rules, tag)
	return nil
}

func (a *supervisorAPI) ListRules(context.Context) ([]xrayapi.Rule, error) {
	result := make([]xrayapi.Rule, 0, len(a.rules))
	for _, rule := range a.rules {
		result = append(result, rule)
	}
	return result, nil
}

func supervisorPolicy() Policy {
	policy := DefaultPolicy()
	policy.MinimumDwell = time.Nanosecond
	return policy
}

func supervisorSnapshot(override string, current, candidate string) xrayapi.Snapshot {
	now := time.Now().UTC()
	return xrayapi.Snapshot{
		APIReachable: true, RoutingReachable: true, ObservatoryReachable: true,
		Balancer: xrayapi.BalancerState{NativeSelected: current, Override: override},
		OutboundHealth: []xrayapi.OutboundHealth{
			{Tag: current, Alive: true, DelayMS: 100, LastTry: now, LastSeen: now},
			{Tag: candidate, Alive: true, DelayMS: 20, LastTry: now, LastSeen: now},
		},
	}
}

func TestSupervisorStartsWithValidatedNativeTargetAndPersistsSelection(t *testing.T) {
	reader := &supervisorReader{snapshot: supervisorSnapshot("", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	path := t.TempDir() + "/selection.json"
	probe := NewProbeRouter(api)
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, probe, SelectionStore{Path: path})
	supervisor.SetActiveProbe(func(context.Context, string, int64) error { return nil })

	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := (SelectionStore{Path: path}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if record.Target != "proxy-main-01" || reader.snapshot.Balancer.Override != "proxy-main-01" {
		t.Fatalf("startup selection = %+v override=%q", record, reader.snapshot.Balancer.Override)
	}
	if supervisor.Snapshot().LastSwitchReason != ReasonStartup {
		t.Fatalf("startup status = %+v", supervisor.Snapshot())
	}
}

func TestSupervisorStartupTriesHealthyCandidateAfterNativeProbeFailure(t *testing.T) {
	path := t.TempDir() + "/selection.json"
	reader := &supervisorReader{snapshot: supervisorSnapshot("", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), SelectionStore{Path: path})
	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" {
			return errors.New("native target failed active probe")
		}
		return nil
	})
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-02" || supervisor.Snapshot().LastSwitchReason != ReasonStartup {
		t.Fatalf("startup recovery = override=%q status=%+v", reader.snapshot.Balancer.Override, supervisor.Snapshot())
	}
	record, err := (SelectionStore{Path: path}).Load()
	if err != nil || record.Target != "proxy-main-02" {
		t.Fatalf("startup recovery record = %+v err=%v", record, err)
	}
}

func TestSupervisorReappliesStableTargetWithoutSelectionWrite(t *testing.T) {
	path := t.TempDir() + "/selection.json"
	stableAt := time.Now().UTC().Add(-time.Hour)
	store := SelectionStore{Path: path}
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: stableAt, LastSwitchReason: ReasonStartup, LastSwitchAt: stableAt}); err != nil {
		t.Fatal(err)
	}
	before, err := readSelectionBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := &supervisorReader{snapshot: supervisorSnapshot("", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), store)
	supervisor.SetActiveProbe(func(context.Context, string, int64) error { return nil })
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := readSelectionBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || reader.snapshot.Balancer.Override != "proxy-main-01" {
		t.Fatalf("reapply changed persistent state or target: before=%q after=%q override=%q", before, after, reader.snapshot.Balancer.Override)
	}
	if supervisor.Snapshot().LastRuntimeAction != ReasonReapply {
		t.Fatalf("reapply status = %+v", supervisor.Snapshot())
	}
}

func TestSupervisorFailsOverOnlyAfterTwoLivenessFailuresAndClearsFirst(t *testing.T) {
	path := t.TempDir() + "/selection.json"
	now := time.Now().UTC().Add(-time.Hour)
	store := SelectionStore{Path: path}
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: now, LastSwitchReason: ReasonStartup, LastSwitchAt: now}); err != nil {
		t.Fatal(err)
	}
	reader := &supervisorReader{snapshot: supervisorSnapshot("proxy-main-01", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	failed := 0
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), store)
	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" {
			failed++
			return errors.New("synthetic liveness failure")
		}
		return nil
	})
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-01" || supervisor.Snapshot().LivenessFailures != 1 {
		t.Fatalf("first failure switched unexpectedly: override=%q status=%+v", reader.snapshot.Balancer.Override, supervisor.Snapshot())
	}
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-02" || !reflect.DeepEqual(api.override, []string{"", "proxy-main-02"}) {
		t.Fatalf("failover trace = %v override=%q", api.override, reader.snapshot.Balancer.Override)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if record.Target != "proxy-main-02" || supervisor.Snapshot().LastSwitchReason != ReasonHealthFailover || failed < 2 {
		t.Fatalf("failover state = %+v status=%+v failures=%d", record, supervisor.Snapshot(), failed)
	}
}

func TestSupervisorDoesNotReapplyDisabledStableTarget(t *testing.T) {
	path := t.TempDir() + "/selection.json"
	store := SelectionStore{Path: path}
	when := time.Now().UTC().Add(-time.Hour)
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: when, LastSwitchReason: ReasonStartup, LastSwitchAt: when}); err != nil {
		t.Fatal(err)
	}
	reader := &supervisorReader{snapshot: supervisorSnapshot("", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: false}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), store)
	supervisor.SetActiveProbe(func(context.Context, string, int64) error { return nil })
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-02" || !reflect.DeepEqual(api.override, []string{"", "proxy-main-02"}) {
		t.Fatalf("disabled target was reused: overrides=%v target=%q", api.override, reader.snapshot.Balancer.Override)
	}
}

func TestSupervisorManualOverrideIgnoresLatencyAndThroughput(t *testing.T) {
	reader := &supervisorReader{snapshot: supervisorSnapshot("", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), SelectionStore{Path: t.TempDir() + "/selection.json"})
	if err := supervisor.SetManualOverride(context.Background(), "proxy-main-01"); err != nil {
		t.Fatal(err)
	}
	reader.snapshot.OutboundHealth[0].DelayMS = 500
	reader.snapshot.OutboundHealth[1].DelayMS = 1
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.ApplyBenchmark(context.Background(), BenchmarkResult{
		SwitchAllowed: true,
		Samples: map[string]ThroughputSample{
			"proxy-main-01": {Valid: true, BytesPerSecond: 1},
			"proxy-main-02": {Valid: true, BytesPerSecond: 100},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-01" || supervisor.Snapshot().ManualOverride != "proxy-main-01" {
		t.Fatalf("manual override was influenced by automatic evidence: override=%q status=%+v", reader.snapshot.Balancer.Override, supervisor.Snapshot())
	}
}

func TestSupervisorManualOverrideUsesActiveLivenessNotObservatory(t *testing.T) {
	reader := &supervisorReader{snapshot: supervisorSnapshot("", "proxy-main-01", "proxy-main-02")}
	reader.snapshot.OutboundHealth[0].Alive = false
	api := &supervisorAPI{reader: reader}
	store := SelectionStore{Path: t.TempDir() + "/selection.json"}
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), store)
	probes := 0
	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" {
			probes++
		}
		return nil
	})
	if err := supervisor.SetManualOverride(context.Background(), "proxy-main-01"); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-01" || probes != 1 || supervisor.Snapshot().ManualOverride != "proxy-main-01" {
		t.Fatalf("Observatory false state replaced successful active liveness: override=%q probes=%d status=%+v", reader.snapshot.Balancer.Override, probes, supervisor.Snapshot())
	}

	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" {
			probes++
			return errors.New("synthetic active liveness failure")
		}
		return nil
	})
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-01" {
		t.Fatalf("manual target left after first active failure: %q", reader.snapshot.Balancer.Override)
	}
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-02" || record.Target != "proxy-main-02" || record.ManualOverride != "proxy-main-01" || supervisor.Snapshot().State != "manual-fallback" || probes != 3 {
		t.Fatalf("manual failover state = override=%q record=%+v status=%+v probes=%d", reader.snapshot.Balancer.Override, record, supervisor.Snapshot(), probes)
	}
	if err := supervisor.SetManualOverride(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	record, err = store.Load()
	if err != nil || record.ManualOverride != "" {
		t.Fatalf("manual clear record = %+v err=%v", record, err)
	}
}

func readSelectionBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}
