//go:build linux

package nativequality

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

// TestIssue191ReadOnlyPlanQualification is safe to run in a disposable MIPS
// stage. It has no network, config editor, native job, or production path.
func TestIssue191ReadOnlyPlanQualification(t *testing.T) {
	now := time.Now().UTC()
	all := make([]string, 46)
	for i := range all {
		all[i] = fmt.Sprintf("proxy-fixture-%02d", i)
	}
	active := append([]string(nil), all[:6]...)
	plan, err := planSweep(all, active, active[0], 0, "", 18)
	if err != nil || plan.TotalEligible != 46 || len(plan.Candidates) != 18 || plan.Deferred != 28 || plan.FirstInitialization {
		t.Fatal("bounded 18/46 plan", err, plan.TotalEligible, len(plan.Candidates), plan.Deferred)
	}
	seen := map[string]bool{}
	for _, tag := range plan.Candidates {
		if seen[tag] {
			t.Fatal("duplicate candidate")
		}
		seen[tag] = true
	}
	for _, tag := range active {
		if !seen[tag] {
			t.Fatal("incumbent omitted")
		}
	}
	broad, err := planSweep(all, all, active[0], 0, "", 18)
	if err != nil || !broad.FirstInitialization || len(broad.Candidates) != 18 || broad.Candidates[0] != active[0] {
		t.Fatal("broad selector did not anchor native selection", err)
	}
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	lock, err := acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeQuotaLocked(path, quotaReceipt{Version: 1, LastComparisonStartedAt: now, FairCursor: plan.NextCursor, EligibleSetHash: plan.EligibleSetHash}); err != nil {
		t.Fatal(err)
	}
	if err := settleSweepLocked(path, now, true, false); err != nil {
		t.Fatal(err)
	}
	lock()
	q, err := quotaState(path, now.Add(time.Second), testReviewBytes)
	if err != nil || q.FairCursor != plan.NextCursor || q.InspectionRequired {
		t.Fatal("durable recovery cursor", q, err)
	}
	prior, err := readPlanReceipt(path, now.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	next, err := planSweep(all, active, active[0], prior.FairCursor, prior.EligibleSetHash, 18)
	if err != nil || next.CursorState != "continued" || next.NextCursor == plan.NextCursor {
		t.Fatal("cursor did not advance", err)
	}
	initialRotation := map[string]bool{}
	for _, tag := range plan.Candidates[6:] {
		initialRotation[tag] = true
	}
	for _, tag := range next.Candidates[6:] {
		if initialRotation[tag] {
			t.Fatal("immediate rotation repeated")
		}
	}
	// Across repeated receipts every non-incumbent gets an opportunity.
	covered := map[string]bool{}
	cursor, hash := 0, ""
	for review := 0; review < 4; review++ {
		p, e := planSweep(all, active, active[0], cursor, hash, 18)
		if e != nil {
			t.Fatal(e)
		}
		for _, tag := range p.Candidates[6:] {
			covered[tag] = true
		}
		cursor, hash = p.NextCursor, p.EligibleSetHash
	}
	for _, tag := range all[6:] {
		if !covered[tag] {
			t.Fatal("deferred alternative never reached", tag)
		}
	}
	drifted := append([]string(nil), all...)
	drifted[len(drifted)-1] = "proxy-fixture-new"
	reanchored, err := planSweep(drifted, active, active[0], cursor, hash, 18)
	if err != nil || reanchored.CursorState != "reanchored" || len(reanchored.Candidates) != 18 {
		t.Fatal("membership drift did not reanchor", err)
	}
	// Same members never restart solely to refresh floating weights or order.
	if got := poolDecision(active, nil, nil, []string{active[5], active[4], active[3], active[2], active[1], active[0]}, false); got != "pool-unchanged" {
		t.Fatal(got)
	}
	// Any different best six replaces the pool; there is no challenger margin.
	changed := []string{active[0], active[1], active[2], active[3], active[4], all[6]}
	if got := poolDecision(active, nil, nil, changed, false); got != "best-six" {
		t.Fatal("best six not applied", got)
	}
	// Fewer than six valid results never shrink a larger live pool.
	if got := poolDecision(active, nil, nil, changed[:5], false); got != "insufficient-valid-results" {
		t.Fatal("five valid results shrank a six-member pool", got)
	}
	// Orphaned members are not live: a two-member live pool may be replaced.
	if got := poolDecision(active, active[2:], nil, changed[:3], false); got != "best-six" {
		t.Fatal("orphaned members blocked the replacement", got)
	}
	// Any two valid samples are ranked; there is no coverage threshold.
	full := c1.AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: 18, Generation: 1, State: "completed", StartedAt: now.Add(-time.Minute), CompletedAt: now, ShortlistCount: 18}
	for i, tag := range plan.Candidates {
		full.Candidates = append(full.Candidates, c1.AdaptiveCandidateResult{Tag: tag, SampledAt: now.Add(-time.Second), RTTMS: int64(100 + i), DownloadBPS: 1e6, UploadBPS: 1e6, Valid: i < 2})
	}
	full.ValidCount = 2
	if _, err := c1.NativeQualityCosts(full, now, all); err != nil {
		t.Fatal("two valid results rejected", err)
	}
	full.Candidates[1].Valid = false
	full.ValidCount = 1
	if _, err := c1.NativeQualityCosts(full, now, all); err == nil {
		t.Fatal("one valid result accepted")
	}
}

func readPlanReceipt(path string, now time.Time) (quotaReceipt, error) {
	unlock, err := acquireQuotaLock(path)
	if err != nil {
		return quotaReceipt{}, err
	}
	defer unlock()
	return readQuotaLocked(path, now)
}
