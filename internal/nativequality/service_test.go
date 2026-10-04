package nativequality

import (
	"context"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

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

func TestPrepareNativeCurrentFirstExploresWithoutOverride(t *testing.T) {
	now := time.Now().UTC()
	s := xrayapi.Snapshot{APIReachable: true, RoutingReachable: true, ObservatoryReachable: true, Balancer: xrayapi.BalancerState{NativeSelected: "proxy-current"}}
	pool := []string{"proxy-current", "proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f", "proxy-dead", "proxy-stale"}
	for i, tag := range pool {
		s.OutboundHealth = append(s.OutboundHealth, xrayapi.OutboundHealth{Tag: tag, Alive: true, DelayMS: int64(10 + i*10), LastTry: now})
	}
	s.OutboundHealth[7].Alive = false
	s.OutboundHealth[8].LastTry = now.Add(-3 * time.Minute)
	first, err := prepare(s, pool, 1, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepare(s, pool, 2, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Candidates) != 6 || first.Candidates[0].Tag != "proxy-current" || first.Candidates[5].Tag == second.Candidates[5].Tag {
		t.Fatalf("bad bounded exploration: %+v %+v", first, second)
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
	s := &Service{pool: []string{"proxy-a", "proxy-b"}, status: Status{State: "completed"}, result: c1.AdaptiveResult{Generation: 1, State: "completed", StartedAt: now.Add(-time.Hour), CompletedAt: now.Add(-time.Hour + time.Second), ShortlistCount: 2, Candidates: []c1.AdaptiveCandidateResult{{Tag: "proxy-a", Valid: true, DownloadBPS: 1, UploadBPS: 1}, {Tag: "proxy-b", Valid: true, DownloadBPS: 2, UploadBPS: 2}}}}
	if s.Read().CanStage {
		t.Fatal("stale recommendation usable")
	}
	status := s.Read()
	if len(status.Ranking) != 2 || status.Ranking[0].Tag != "proxy-b" || status.Ranking[0].Rank != 1 {
		t.Fatal("throughput ranking lost or confused with freshness", status)
	}
}
