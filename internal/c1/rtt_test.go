package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

type syntheticLatency struct {
	durations map[string]time.Duration
	failing   map[string]bool
	order     []string
	current   func() string
}

func (s *syntheticLatency) Latency(context.Context) (time.Duration, error) {
	tag := s.current()
	s.order = append(s.order, tag)
	if s.failing[tag] {
		return 0, errors.New("synthetic probe failure")
	}
	return s.durations[tag], nil
}

func rttFixture(t *testing.T) (*Coordinator, *benchmarkProbeAPI, *syntheticLatency) {
	t.Helper()
	api := &benchmarkProbeAPI{}
	coordinator := NewCoordinator(DefaultPolicy(), nil)
	coordinator.SetAdaptiveRunner(&AdaptiveRunner{Probe: NewProbeRouter(api)})
	transport := &syntheticLatency{durations: map[string]time.Duration{"proxy-a": 80 * time.Millisecond, "proxy-b": 120 * time.Millisecond}, failing: map[string]bool{"proxy-c": true}}
	transport.current = func() string {
		api.mu.Lock()
		defer api.mu.Unlock()
		return api.adds[len(api.adds)-1].OutboundTag
	}
	coordinator.SetRTTTransport(transport)
	return coordinator, api, transport
}

func TestMeasureRTTProbesEachTagThroughItsOwnRuleAndCleansUp(t *testing.T) {
	coordinator, api, transport := rttFixture(t)
	results, err := coordinator.MeasureRTT(context.Background(), []string{"proxy-a", "proxy-b", "proxy-c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || !results[0].Valid || results[0].RTTMS != 80 || !results[1].Valid || results[1].RTTMS != 120 || results[2].Valid || results[2].RTTMS != 0 {
		t.Fatalf("rtt results = %+v", results)
	}
	if len(transport.order) != 3 || transport.order[0] != "proxy-a" || transport.order[2] != "proxy-c" {
		t.Fatalf("probe order = %v", transport.order)
	}
	for _, rule := range api.adds {
		if rule.RuleTag != RTTRuleTag || rule.InboundTag != ProbeInboundTag {
			t.Fatalf("probe rule = %+v", rule)
		}
	}
	if len(api.rules) != 0 {
		t.Fatalf("probe rules left installed: %+v", api.rules)
	}
	if coordinator.IsLifecycleBusy() {
		t.Fatal("lifecycle token not released")
	}
}

func TestMeasureRTTRejectsInvalidTagsAndYieldsToApply(t *testing.T) {
	coordinator, api, _ := rttFixture(t)
	for _, tags := range [][]string{nil, {"direct"}, {"proxy-a", "proxy-a"}} {
		if _, err := coordinator.MeasureRTT(context.Background(), tags); !errors.Is(err, ErrManualInvalidTarget) {
			t.Fatalf("tags %v admitted: %v", tags, err)
		}
	}
	release, err := coordinator.BeginApply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.MeasureRTT(context.Background(), []string{"proxy-a"}); !errors.Is(err, ErrManualBusy) {
		t.Fatalf("probe admitted during Apply: %v", err)
	}
	release()
	if len(api.adds) != 0 {
		t.Fatal("refused probes installed rules")
	}
}

func TestMeasureRTTStopsOnCleanupFailure(t *testing.T) {
	coordinator, api, _ := rttFixture(t)
	api.failRemove = true
	results, err := coordinator.MeasureRTT(context.Background(), []string{"proxy-a", "proxy-b"})
	if !errors.Is(err, ErrProbeCleanup) || len(results) != 0 {
		t.Fatalf("cleanup failure = %+v, %v", results, err)
	}
}
