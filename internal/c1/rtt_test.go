package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

type syntheticLatency struct {
	durations map[string]time.Duration
	sequence  map[string][]time.Duration
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
	if next := s.sequence[tag]; len(next) > 0 {
		s.sequence[tag] = next[1:]
		if next[0] < 0 {
			return 0, errors.New("synthetic late failure")
		}
		return next[0], nil
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
	// Three requests per answering node; a node that fails its first request
	// is not retried.
	if len(transport.order) != 7 || transport.order[0] != "proxy-a" || transport.order[2] != "proxy-a" || transport.order[3] != "proxy-b" || transport.order[6] != "proxy-c" {
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

// The probe reports the median of its answered requests, so one slow or
// failed request does not decide a node's latency.
func TestMeasureRTTUsesTheMedianOfThreeRequests(t *testing.T) {
	coordinator, _, transport := rttFixture(t)
	transport.sequence = map[string][]time.Duration{
		"proxy-a": {900 * time.Millisecond, 90 * time.Millisecond, 120 * time.Millisecond},
		"proxy-b": {200 * time.Millisecond, -1, 100 * time.Millisecond},
	}
	results, err := coordinator.MeasureRTT(context.Background(), []string{"proxy-a", "proxy-b"})
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].Valid || results[0].RTTMS != 120 {
		t.Fatal("median of three", results[0])
	}
	// 200 and 100 answered, the middle request failed: the upper of the two.
	if !results[1].Valid || results[1].RTTMS != 200 {
		t.Fatal("median with a late failure", results[1])
	}
}

// A probe whose rule never installed sent no request through the node: its
// invalid sample is not routed, unlike a node that failed its request.
func TestMeasureRTTMarksProbeRouteFailuresUnrouted(t *testing.T) {
	coordinator, api, transport := rttFixture(t)
	results, err := coordinator.MeasureRTT(context.Background(), []string{"proxy-a", "proxy-c"})
	if err != nil || !results[0].Routed || !results[1].Routed || results[1].Valid {
		t.Fatalf("routed results = %+v, %v", results, err)
	}
	api.failAdd = true
	transport.order = nil
	results, err = coordinator.MeasureRTT(context.Background(), []string{"proxy-a"})
	if err != nil || len(results) != 1 || results[0].Valid || results[0].Routed || len(transport.order) != 0 {
		t.Fatalf("unrouted results = %+v, %v, requests %v", results, err, transport.order)
	}
}
