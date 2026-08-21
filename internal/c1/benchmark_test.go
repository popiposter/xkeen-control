package c1

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/popiposter/xkeen-keenetic/internal/xrayapi"
)

type benchmarkProbeAPI struct {
	mu         sync.Mutex
	adds       []xrayapi.Rule
	appends    []bool
	removes    []string
	failRemove bool
	rules      map[string]xrayapi.Rule
}

func (f *benchmarkProbeAPI) OverrideBalancerTarget(context.Context, string, string) error { return nil }
func (f *benchmarkProbeAPI) AddRule(_ context.Context, rule xrayapi.Rule, appendOnly bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, rule)
	f.appends = append(f.appends, appendOnly)
	if f.rules == nil {
		f.rules = make(map[string]xrayapi.Rule)
	}
	f.rules[rule.RuleTag] = rule
	return nil
}
func (f *benchmarkProbeAPI) RemoveRule(_ context.Context, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, tag)
	if f.failRemove {
		return errors.New("remove failed")
	}
	delete(f.rules, tag)
	return nil
}
func (f *benchmarkProbeAPI) ListRules(context.Context) ([]xrayapi.Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]xrayapi.Rule, 0, len(f.rules))
	for _, rule := range f.rules {
		result = append(result, rule)
	}
	return result, nil
}

func TestProbeRouterAlwaysAppendsAndCleansAfterAction(t *testing.T) {
	api := &benchmarkProbeAPI{}
	probe := NewProbeRouter(api)
	called := false
	if err := probe.WithTarget(context.Background(), "benchmark", "proxy-node-a", func(context.Context) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("probe = %v called=%v", err, called)
	}
	if len(api.adds) != 1 || !api.appends[0] || api.adds[0].InboundTag != "probe" || api.adds[0].OutboundTag != "proxy-node-a" || len(api.removes) != 1 {
		t.Fatalf("probe routing trace = adds=%+v append=%v removes=%v", api.adds, api.appends, api.removes)
	}
}

func TestProbeCleanupFailureBlocksNextProbeUntilReconciled(t *testing.T) {
	api := &benchmarkProbeAPI{failRemove: true}
	probe := NewProbeRouter(api)
	if err := probe.WithTarget(context.Background(), "liveness", "proxy-node-a", nil); !errors.Is(err, ErrProbeCleanup) || !probe.Blocked() {
		t.Fatalf("cleanup failure = %v blocked=%v", err, probe.Blocked())
	}
	if err := probe.WithTarget(context.Background(), "benchmark", "proxy-node-b", nil); !errors.Is(err, ErrProbeBlocked) {
		t.Fatalf("blocked probe = %v", err)
	}
	api.failRemove = false
	if err := probe.Reconcile(context.Background()); err != nil || probe.Blocked() {
		t.Fatalf("reconcile = %v blocked=%v", err, probe.Blocked())
	}
}

func TestBenchmarkRunsAllEnabledNodesSequentiallyAndWritesOneSnapshot(t *testing.T) {
	api := &benchmarkProbeAPI{}
	probe := NewProbeRouter(api)
	statePath := filepath.Join(t.TempDir(), "benchmark.json")
	policy := DefaultPolicy()
	policy.TargetPayloadBytes = 4 * MiB
	policy.TotalBudgetBytes = 20 * MiB
	runner := NewBenchmarkRunner(policy, probe, BenchmarkStore{Path: statePath})
	var order []string
	runner.HTTPDo = func(_ context.Context, _ string, proxy string, payload int64, timeout time.Duration) (int64, time.Duration, error) {
		if proxy != ProbeAddress || payload != 4*MiB || timeout != DefaultPerNodeTimeout {
			t.Fatalf("sample policy = proxy %q payload %d timeout %s", proxy, payload, timeout)
		}
		order = append(order, api.adds[len(order)].OutboundTag)
		return 1024, time.Second, nil
	}
	nodes := []NodeState{{Tag: "proxy-node-c", Enabled: true}, {Tag: "proxy-node-a", Enabled: true}, {Tag: "proxy-node-b", Enabled: true}, {Tag: "proxy-disabled", Enabled: false}}
	result := runner.Run(context.Background(), nodes, "proxy-node-a")
	if result.ResultClass != "completed" || !result.SwitchAllowed || result.EligibleNodes != 3 || result.ValidSamples != 3 || result.AggregateBytes != 3072 || !result.CurrentValid {
		t.Fatalf("benchmark result = %+v", result)
	}
	if len(order) != 3 || order[0] != "proxy-node-a" || order[1] != "proxy-node-b" || order[2] != "proxy-node-c" {
		t.Fatalf("benchmark order = %v", order)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (BenchmarkStore{Path: statePath}).Load()
	if err != nil || snapshot.ValidSamples != 3 || snapshot.AggregateBytes != 3072 {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
}
