package nativequality

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

func TestStatusReportsActualGenerationBudget(t *testing.T) {
	for _, tt := range []struct {
		generation uint64
		manual     bool
		bytes      int64
		seconds    int
	}{{0, false, 288 * c1.MiB, 360}, {1, false, 144 * c1.MiB, 180}, {1, true, 288 * c1.MiB, 360}} {
		s := &Service{status: Status{State: "running", Generation: tt.generation, ManualSample: tt.manual}}
		v := s.Read()
		if v.Limits.Bytes != tt.bytes || v.Limits.Seconds != tt.seconds {
			t.Fatal(v)
		}
	}
	s := &Service{Resources: &resourcepolicy.Guard{Profile: resourcepolicy.ForPlatform("mipsle", 254472)}, status: Status{StartReason: "native-speed-conflict"}}
	v := s.Read()
	if v.AutomaticReason != "" || v.StartReason != "native-speed-conflict" {
		t.Fatal(v)
	}
}

func TestBroadSampleUsesThresholdAndMoreThanSixWithoutCurrentPoolPriority(t *testing.T) {
	now := time.Now().UTC()
	snapshot := xrayapi.Snapshot{APIReachable: true, RoutingReachable: true, ObservatoryReachable: true}
	var pool []string
	for i := 0; i < 22; i++ {
		tag := fmt.Sprintf("proxy-%02d", i)
		pool = append(pool, tag)
		snapshot.OutboundHealth = append(snapshot.OutboundHealth, xrayapi.OutboundHealth{Tag: tag, Alive: true, DelayMS: int64(150 + i*8), LastTry: now})
	}
	snapshot.OutboundHealth[21].LastTry = now.Add(-3 * time.Minute)
	g, err := prepare(snapshot, pool, 1, 1, now)
	if err != nil || !g.BroadSample || len(g.Candidates) != 12 || len(g.Fallbacks) != 6 || g.Candidates[0].Tag != "proxy-00" || g.Fallbacks[5].Tag != "proxy-17" || eligibleCount(snapshot, pool, true, now) != 19 || latencyLimit(150, true) != 300 {
		t.Fatalf("bad expanded threshold sample: %+v %v", g, err)
	}
	if latencyLimit(240, true) != 480 || latencyLimit(500, true) != 750 {
		t.Fatal("dynamic threshold is not bounded")
	}
}

func TestMeasurementPoolIncludesNodesOutsideRestrictedActivePoolAndRejectsPrefixCollision(t *testing.T) {
	text := `{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-a","proxy-b"],"strategy":{"type":"leastLoad"}}]}}`
	nodes := []c1.NodeState{{Tag: "proxy-a", Enabled: true}, {Tag: "proxy-b", Enabled: true}, {Tag: "proxy-c", Enabled: true}, {Tag: "proxy-disabled"}}
	targets := []xkeen.ConfigTarget{{Tag: "proxy-a", Kind: "outbound"}, {Tag: "proxy-b", Kind: "outbound"}, {Tag: "proxy-c", Kind: "outbound"}}
	pool, _, err := measurementPool(text, nodes, targets)
	if err != nil || len(pool) != 3 || pool[2] != "proxy-c" {
		t.Fatal("new tests restricted by old active pool", pool, err)
	}
	if !exactSelectors([]string{"proxy-a", "proxy-b"}, targets) {
		t.Fatal("exact selectors rejected")
	}
	if exactSelectors([]string{"proxy-a"}, append(targets, xkeen.ConfigTarget{Tag: "proxy-a-backup", Kind: "outbound"})) {
		t.Fatal("prefix collision admitted")
	}
	// All selected nodes can be gone while new healthy alternatives remain.
	nodes = []c1.NodeState{{Tag: "proxy-c", Enabled: true}, {Tag: "proxy-d", Enabled: true}}
	targets = append(targets, xkeen.ConfigTarget{Tag: "proxy-d", Kind: "outbound"})
	if _, _, err := measurementPool(text, nodes, targets); err != nil {
		t.Fatal("exhausted pool cannot be resampled", err)
	}
}

func TestStopClosesAdmissionAndWaitsForCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	stopped := make(chan struct{})
	s := &Service{cancel: cancel, done: done}
	go func() { s.Stop(); close(stopped) }()
	<-ctx.Done()
	select {
	case <-stopped:
		t.Fatal("Stop returned before cleanup")
	default:
	}
	close(done)
	<-stopped
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		t.Fatal("admission not closed")
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("post-stop start admitted")
	}
}

func TestPrepareUsesLowestFreshLatencyAndOrderedFallbacks(t *testing.T) {
	now := time.Now().UTC()
	s := xrayapi.Snapshot{APIReachable: true, RoutingReachable: true, ObservatoryReachable: true, Balancer: xrayapi.BalancerState{NativeSelected: "proxy-current"}}
	pool := []string{"proxy-current", "proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f", "proxy-dead", "proxy-stale"}
	for i, tag := range pool {
		s.OutboundHealth = append(s.OutboundHealth, xrayapi.OutboundHealth{Tag: tag, Alive: true, DelayMS: int64(10 + i*10), LastTry: now})
	}
	s.OutboundHealth[0].DelayMS = 700
	s.OutboundHealth[7].Alive = false
	s.OutboundHealth[8].LastTry = now.Add(-3 * time.Minute)
	first, err := prepare(s, pool, 1, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepare(s, pool, 2, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Candidates) != 6 || first.Candidates[0].Tag != "proxy-a" || first.Candidates[5].Tag != "proxy-f" || second.Candidates[5].Tag != "proxy-f" || len(first.Fallbacks) != 1 || first.Fallbacks[0].Tag != "proxy-current" {
		t.Fatalf("bad latency order or replacement queue: %+v %+v", first, second)
	}
	for _, n := range first.Candidates {
		if n.Tag == "proxy-dead" || n.Tag == "proxy-stale" || n.Samples != 0 || n.HealthPenalty != 0 {
			t.Fatalf("invented health or ineligible candidate: %+v", n)
		}
	}
	s.Balancer.Override = "proxy-current"
	if _, err := prepare(s, pool, 3, 0, now); err == nil {
		t.Fatal("override bypass admitted")
	}
}

func TestReplacementRetainsHealthEvidenceWithoutReplacingFreshLatency(t *testing.T) {
	generation := c1.AdaptiveGeneration{
		Candidates: []c1.AdaptiveCandidateInput{{Tag: "proxy-a", RTTMS: 20}},
		Fallbacks:  []c1.AdaptiveCandidateInput{{Tag: "proxy-b", RTTMS: 80}},
	}
	retainHealthEvidence(&generation, map[string]c1.AdaptiveCandidateInput{
		"proxy-a": {Samples: 3, HealthPenalty: 1.2},
		"proxy-b": {Samples: 8, HealthPenalty: 2.5, RTTMS: 200},
	})
	if generation.Candidates[0].Samples != 3 || generation.Candidates[0].HealthPenalty != 1.2 || generation.Fallbacks[0].Samples != 8 || generation.Fallbacks[0].HealthPenalty != 2.5 || generation.Fallbacks[0].RTTMS != 80 {
		t.Fatalf("lost retained replacement evidence or fresh latency: %+v", generation)
	}
}

func TestRoutingPoolRejectsUnweightedSelectorMembers(t *testing.T) {
	text := `{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"strategy":{"type":"leastPing"}}]}}`
	nodes := []c1.NodeState{{ID: "a", Tag: "proxy-a", Enabled: true}, {ID: "b", Tag: "proxy-b", Enabled: true}}
	targets := []xkeen.ConfigTarget{{Tag: "proxy-a", Kind: "outbound"}, {Tag: "proxy-b", Kind: "outbound"}, {Tag: "direct", Kind: "outbound"}}
	pool, index, err := routingPool(text, nodes, targets)
	if err != nil || len(pool) != 2 || index != 0 {
		t.Fatalf("%v %v %v", pool, index, err)
	}
	targets = append(targets, xkeen.ConfigTarget{Tag: "proxy-unmanaged", Kind: "outbound"})
	if _, _, err = routingPool(text, nodes, targets); err == nil {
		t.Fatal("unmanaged default cost=1 admitted")
	}
	if _, _, err = routingPool(text, nodes, targets[:1]); err == nil {
		t.Fatal("missing runtime outbound admitted")
	}
}

func TestReadExpiredRecommendationCannotStage(t *testing.T) {
	now := time.Now().UTC()
	s := &Service{pool: []string{"proxy-a", "proxy-b"}, status: Status{State: "completed"}, result: c1.AdaptiveResult{Generation: 1, State: "completed", StartedAt: now.Add(-time.Hour), CompletedAt: now.Add(-time.Hour + time.Second), ShortlistCount: 2, Candidates: []c1.AdaptiveCandidateResult{{Tag: "proxy-a", RTTMS: 20, Valid: true, DownloadBPS: 1, UploadBPS: 1}, {Tag: "proxy-b", RTTMS: 20, Valid: true, DownloadBPS: 2, UploadBPS: 2}}}}
	if s.Read().CanStage {
		t.Fatal("stale recommendation usable")
	}
	status := s.Read()
	if status.StageReason != "measurement-expired-or-incomplete" {
		t.Fatal("expired result lacks explanation", status)
	}
	if len(status.Ranking) != 2 || status.Ranking[0].Tag != "proxy-b" || status.Ranking[0].Rank != 1 {
		t.Fatal("throughput ranking lost or confused with freshness", status)
	}
}
