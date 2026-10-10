package nativequality

import (
	"context"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestStatusReportsBatchAndReviewBudgets(t *testing.T) {
	for _, tt := range []struct {
		profile resourcepolicy.Profile
		batch   int64
		seconds int
		review  int64
	}{{resourcepolicy.ForPlatform("arm64", 1<<20), 144 * c1.MiB, 180, 288 * c1.MiB}, {resourcepolicy.ForPlatform("mipsle", 254472), 24 * c1.MiB, 90, 72 * c1.MiB}} {
		s := &Service{Resources: &resourcepolicy.Guard{Profile: tt.profile}, status: Status{State: "running", Generation: 1}}
		v := s.Read()
		if v.Limits.Bytes != tt.batch || v.Limits.Seconds != tt.seconds || v.ManualAllowanceBytes != tt.review {
			t.Fatal(tt.profile.Name, v.Limits, v.ManualAllowanceBytes)
		}
	}
	s := &Service{Resources: &resourcepolicy.Guard{Profile: resourcepolicy.ForPlatform("mipsle", 254472)}, status: Status{StartReason: "native-speed-conflict"}}
	v := s.Read()
	if v.AutomaticReason != "" || v.StartReason != "native-speed-conflict" {
		t.Fatal(v)
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
	overlapping := `{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-","proxy-a"],"strategy":{"type":"leastLoad"}}]}}`
	if _, _, err := routingPool(overlapping, nodes, targets); err == nil {
		t.Fatal("overlapping selector prefixes admitted")
	}
	unused := `{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-","unused-"],"strategy":{"type":"leastLoad"}}]}}`
	if _, _, err := routingPool(unused, nodes, targets); err == nil {
		t.Fatal("unresolved selector prefix admitted")
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
