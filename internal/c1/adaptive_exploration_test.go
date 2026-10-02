package c1

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveExplorationVisitsTailWithinExistingCap(t *testing.T) {
	now := time.Now().UTC()
	tags := []string{"proxy-current", "proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f", "proxy-g"}
	delays := []int64{100, 20, 30, 40, 50, 60, 70, 80}
	s, _, _, _ := adaptiveEvidenceFixture(t, tags[0], now.Add(-time.Hour), supervisorPolicy(), tags, delays, now)
	seedAdaptiveEvidence(s, tags, now, delays)
	for i, want := range []string{"proxy-e", "proxy-f", "proxy-g", "proxy-e"} {
		g, reason := s.PrepareAdaptiveGeneration(context.Background(), uint64(i+1))
		if reason != "" || len(g.Candidates) != 6 {
			t.Fatalf("generation: %+v reason=%s", g, reason)
		}
		if got := g.Candidates[4].Tag; got != want {
			t.Fatalf("generation %d exploration=%s want=%s", i+1, got, want)
		}
		for j, tag := range []string{"proxy-a", "proxy-b", "proxy-c", "proxy-d"} {
			if g.Candidates[j].Tag != tag {
				t.Fatalf("fast shortlist lost %s", tag)
			}
		}
		if g.Candidates[5].Tag != tags[0] {
			t.Fatal("current lost")
		}
	}
}

func TestAdaptiveExplorationStableIDsSurviveOrderRemovalAndSingleSlot(t *testing.T) {
	now := time.Now().UTC()
	tags := []string{"proxy-current", "proxy-a", "proxy-b", "proxy-c"}
	delays := []int64{100, 20, 30, 40}
	s, reader, _, _ := adaptiveEvidenceFixture(t, tags[0], now.Add(-time.Hour), supervisorPolicy(), tags, delays, now)
	seedAdaptiveEvidence(s, tags, now, delays)
	nodes := []NodeState{{ID: "current", Tag: tags[0], Enabled: true}, {ID: "z", Tag: tags[1], Enabled: true}, {ID: "a", Tag: tags[2], Enabled: true}, {ID: "m", Tag: tags[3], Enabled: true}}
	s.nodes = func(context.Context) []NodeState { return append([]NodeState(nil), nodes...) }
	s.performancePolicy.AdaptiveChallengerLimit = 1
	g, reason := s.PrepareAdaptiveGeneration(context.Background(), 1)
	if reason != "" || g.Candidates[0].Tag != "proxy-b" {
		t.Fatalf("stable ID order: %+v %s", g, reason)
	}
	// Remove cursor's node, reverse reader order: successor remains ID m.
	nodes = []NodeState{nodes[3], nodes[1], nodes[0]}
	reader.snapshot.OutboundHealth[2].Alive = false
	g, reason = s.PrepareAdaptiveGeneration(context.Background(), 2)
	if reason != "" || len(g.Candidates) != 2 || g.Candidates[0].Tag != "proxy-c" {
		t.Fatalf("successor after removal: %+v %s", g, reason)
	}
	g, reason = s.PrepareAdaptiveGeneration(context.Background(), 3)
	if reason != "" || g.Candidates[0].Tag != "proxy-a" {
		t.Fatalf("stable successor: %+v %s", g, reason)
	}

	nodes = append(nodes, NodeState{ID: "a", Tag: "proxy-b", Enabled: true})
	reader.snapshot.OutboundHealth[2].Alive = true
	g, reason = s.PrepareAdaptiveGeneration(context.Background(), 4)
	if reason != "" || g.Candidates[0].Tag != "proxy-b" {
		t.Fatalf("readded ID did not wrap deterministically: %+v %s", g, reason)
	}
}
