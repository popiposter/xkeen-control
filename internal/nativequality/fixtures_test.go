package nativequality

import (
	"fmt"
	"testing"
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

// TASK-007 under the nightly rule: the best six replace the pool whatever
// the margin, a broad selector adopts any two or more valid results, and an
// imported five-member pool grows to six.
func TestNightlyBestSixDecision(t *testing.T) {
	five := []string{"a", "b", "c", "d", "e"}
	if got := poolDecision(five, nil, []string{"a", "b", "c", "d", "e", "g"}, false); got != "best-six" {
		t.Fatal("five-member pool was not filled", got)
	}
	if got := poolDecision(five, nil, []string{"a", "b", "c", "d", "g"}, false); got != "best-six" {
		t.Fatal("a better same-size pool was not applied", got)
	}
	broad := make([]string, 52)
	for i := range broad {
		broad[i] = fmt.Sprintf("proxy-%02d", i)
	}
	if got := poolDecision(broad, nil, broad[:3], true); got != "insufficient-valid-results" {
		t.Fatal("three results replaced a broad selector", got)
	}
	if got := poolDecision(broad, nil, broad[:6], true); got != "best-six" {
		t.Fatal("six results did not adopt a broad selector", got)
	}
}
