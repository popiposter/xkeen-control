package c1

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
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
