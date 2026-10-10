package nativequality

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

// TASK-007: 52 enabled nodes, a six-member pool and 12 review slots. Every
// review keeps the incumbents; the six rotating slots visit every other node
// before any is repeated, and an eligible-set change reanchors without
// starving anyone.
func TestFiftyTwoNodeFairRotationAcrossReviews(t *testing.T) {
	all := make([]string, 52)
	for i := range all {
		all[i] = fmt.Sprintf("proxy-%02d", i)
	}
	active := all[:6]
	cursor, hash := 0, ""
	seen := map[string]int{}
	for review := 1; review <= 8; review++ {
		plan, err := planSweep(all, active, active[0], cursor, hash, 12)
		if err != nil || len(plan.Candidates) != 12 || plan.Deferred != 40 {
			t.Fatal("review", review, err, len(plan.Candidates), plan.Deferred)
		}
		for i, tag := range active {
			if plan.Candidates[i] != tag {
				t.Fatal("review", review, "dropped an incumbent", plan.Candidates)
			}
		}
		for _, tag := range plan.Candidates[6:] {
			seen[tag]++
			// 46 others in 6-slot windows: nothing repeats before review 8.
			if seen[tag] > 1 && review < 8 {
				t.Fatal("review", review, "repeated", tag, "before covering every node")
			}
		}
		cursor, hash = plan.NextCursor, plan.EligibleSetHash
	}
	if len(seen) != 46 {
		t.Fatal("eight reviews visited", len(seen), "of 46 non-incumbents")
	}
	// A subscription change reanchors the rotation; the next eight reviews
	// still visit every remaining node.
	changed := append(append([]string(nil), all[:51]...), "proxy-new")
	visited := map[string]bool{}
	for review := 1; review <= 8; review++ {
		plan, err := planSweep(changed, active, active[0], cursor, hash, 12)
		if err != nil {
			t.Fatal(err)
		}
		if review == 1 && plan.CursorState != "reanchored" {
			t.Fatal("eligible change did not reanchor", plan.CursorState)
		}
		for _, tag := range plan.Candidates[6:] {
			visited[tag] = true
		}
		cursor, hash = plan.NextCursor, plan.EligibleSetHash
	}
	if len(visited) != 46 || !visited["proxy-new"] {
		t.Fatal("rotation starved a node after the change", len(visited), visited["proxy-new"])
	}
}

func decisionFixture(scores map[string]int64) (c1.AdaptiveResult, []c1.NativeQualityCost) {
	now := time.Now().UTC()
	var result c1.AdaptiveResult
	var costs []c1.NativeQualityCost
	for tag, rtt := range scores {
		valid := rtt > 0
		result.Candidates = append(result.Candidates, c1.AdaptiveCandidateResult{Tag: tag, RTTMS: max(rtt, 1), SampledAt: now, Valid: valid})
		if valid {
			costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: 1})
		}
	}
	return result, costs
}

// TASK-007: mixed healthy, unhealthy and unknown evidence. Unknown is never
// treated as an outage, an unhealthy member may be replaced without a
// margin, and a healthy member is replaced only by a 15% better challenger.
func TestPoolDecisionMixedHealthyUnhealthyAndUnknownEvidence(t *testing.T) {
	now := time.Now().UTC()
	active := []string{"a", "b", "c", "d", "e", "f"}
	scores := map[string]int64{"a": 100, "b": 100, "c": 100, "d": 100, "e": 100, "f": 100, "g": 80, "h": 95, "i": 50}
	alive := map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true, "f": true, "g": true, "h": true, "i": true}
	plan := sweepPlan{Active: active}
	for _, tc := range []struct {
		name     string
		selected []string
		mutate   func(map[string]bool, map[string]int64)
		want     string
	}{
		{"identical pool", active, nil, "pool-unchanged"},
		{"15% better challenger", []string{"a", "b", "c", "d", "e", "g"}, nil, "material-improvement"},
		{"challenger below the margin", []string{"a", "b", "c", "d", "e", "h"}, nil, "insufficient-challenger-margin"},
		{"unhealthy member replaced without margin", []string{"a", "b", "c", "d", "e", "h"}, func(a map[string]bool, _ map[string]int64) { a["f"] = false }, "unhealthy-incumbent-replaced"},
		{"unknown member is not an outage", []string{"a", "b", "c", "d", "e", "h"}, func(a map[string]bool, _ map[string]int64) { delete(a, "f") }, "incumbent-unmeasured-or-unknown"},
		{"healthy member with an invalid sample is kept", []string{"a", "b", "c", "d", "e", "h"}, func(_ map[string]bool, s map[string]int64) { s["f"] = 0 }, "incumbent-unmeasured-or-unknown"},
		{"healthy native target with an invalid sample", []string{"b", "c", "d", "e", "f", "i"}, func(_ map[string]bool, s map[string]int64) { s["a"] = 0 }, "healthy-target-sample-invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := map[string]bool{}
			for k, v := range alive {
				a[k] = v
			}
			s := map[string]int64{}
			for k, v := range scores {
				s[k] = v
			}
			if tc.mutate != nil {
				tc.mutate(a, s)
			}
			result, costs := decisionFixture(s)
			p := plan
			p.NativeSelected = "a"
			if got := poolDecision(result, costs, active, tc.selected, p, a, "a", now); got != tc.want {
				t.Fatalf("decision = %s, want %s", got, tc.want)
			}
		})
	}
}

// TASK-007: a five-member imported pool grows to six measured members while
// keeping its incumbents; a full healthy pool needs at least six valid results.
func TestUndersizedPoolGrowsAndFullPoolNeedsSixResults(t *testing.T) {
	now := time.Now().UTC()
	five := []string{"a", "b", "c", "d", "e"}
	alive := map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true, "g": true}
	result, costs := decisionFixture(map[string]int64{"a": 100, "b": 100, "c": 100, "d": 100, "e": 100, "g": 100})
	if got := poolDecision(result, costs, five, []string{"a", "b", "c", "d", "e", "g"}, sweepPlan{Active: five}, alive, "", now); got != "pool-filled" {
		t.Fatal("five-member pool was not filled", got)
	}
	// Growing while dropping a healthy incumbent still needs the margin.
	if got := poolDecision(result, costs, five, []string{"a", "b", "c", "d", "g"}, sweepPlan{Active: five}, alive, "", now); got != "insufficient-challenger-margin" {
		t.Fatal("same-size swap without margin", got)
	}
	six := []string{"a", "b", "c", "d", "e", "f"}
	alive["f"] = true
	few, fewCosts := decisionFixture(map[string]int64{"a": 100, "b": 100, "g": 10, "h": 10, "i": 10})
	if got := poolDecision(few, fewCosts, six, []string{"a", "b", "g", "h", "i"}, sweepPlan{Active: six}, alive, "", now); got != "insufficient-valid-results" {
		t.Fatal("full healthy pool replaced on five results", got)
	}
}
