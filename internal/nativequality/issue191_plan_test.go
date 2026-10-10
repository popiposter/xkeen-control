//go:build linux

package nativequality

import (
	"fmt"
	"path/filepath"
	"regexp"
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
	plan, err := planSweep(all, active, active[0], 0, "")
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
	broad, err := planSweep(all, all, active[0], 0, "")
	if err != nil || !broad.FirstInitialization || len(broad.Candidates) != 18 || broad.Candidates[0] != active[0] {
		t.Fatal("broad selector did not anchor native selection", err)
	}
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	lock, err := acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserveSweepPlannedLocked(path, now, &plan); err != nil {
		t.Fatal(err)
	}
	if err := settleSweepLocked(path, now, true); err != nil {
		t.Fatal(err)
	}
	lock()
	q, err := quotaState(path, now.Add(time.Second))
	if err != nil || q.FairCursor != plan.NextCursor || q.ReviewsUsed != 1 || q.InspectionRequired {
		t.Fatal("durable cursor/quota", q, err)
	}
	prior, err := readPlanReceipt(path, now.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	next, err := planSweep(all, active, active[0], prior.FairCursor, prior.EligibleSetHash)
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
		p, e := planSweep(all, active, active[0], cursor, hash)
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
	reanchored, err := planSweep(drifted, active, active[0], cursor, hash)
	if err != nil || reanchored.CursorState != "reanchored" || len(reanchored.Candidates) != 18 {
		t.Fatal("membership drift did not reanchor", err)
	}
	// Same members never restart solely to refresh floating weights or order.
	result, costs, alive := scoreFixture(now)
	samePlan := sweepPlan{NativeSelected: active[0]}
	if got := poolDecision(result, costs, active, []string{active[5], active[4], active[3], active[2], active[1], active[0]}, samePlan, alive, active[0], now); got != "pool-unchanged" {
		t.Fatal(got)
	}
	changed := []string{active[0], active[1], active[2], active[3], active[4], all[6]}
	if got := poolDecision(result, costs, active, changed, samePlan, alive, active[0], now); got != "material-improvement" {
		t.Fatal("15 percent boundary", got)
	}
	for i := range result.Candidates {
		if result.Candidates[i].Tag == all[6] {
			result.Candidates[i].RTTMS = 86
		}
	}
	if got := poolDecision(result, costs, active, changed, samePlan, alive, active[0], now); got != "insufficient-challenger-margin" {
		t.Fatal("under 15 percent accepted", got)
	}
	alive[active[5]] = false // the pre-phase probe failed
	if got := poolDecision(result, costs, active, changed, samePlan, alive, active[0], now); got != "unhealthy-incumbent-replaced" {
		t.Fatal("unhealthy exception", got)
	}
	delete(alive, active[5]) // unprobed is unknown, never an outage
	if got := poolDecision(result, costs, active, changed, samePlan, alive, active[0], now); got == "unhealthy-incumbent-replaced" {
		t.Fatal("unprobed incumbent treated as unhealthy")
	}
	alive[active[5]] = true
	for i := range result.Candidates {
		if result.Candidates[i].Tag == active[0] {
			result.Candidates[i].Valid = false
		}
	}
	if got := poolDecision(result, costs, active, changed, samePlan, alive, active[0], now); got != "healthy-target-sample-invalid" {
		t.Fatal("one selected-target transfer failure retired it", got)
	}
	// A full healthy pool needs six valid results before it is replaced.
	few, fewCosts, fewAlive := scoreFixture(now)
	for i := range few.Candidates[:2] {
		few.Candidates[i+1].Valid = false
	}
	if got := poolDecision(few, fewCosts, active, changed, samePlan, fewAlive, active[0], now); got != "insufficient-valid-results" {
		t.Fatal("five valid results replaced a full healthy pool", got)
	}
	// Fifteen of eighteen valid samples qualifies the bounded subset; fourteen
	// does not. The other 28 are deferred, not recorded as failed transfers.
	full := c1.AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: 18, Generation: 1, State: "completed", StartedAt: now.Add(-time.Minute), CompletedAt: now, ShortlistCount: 18}
	for i, tag := range plan.Candidates {
		full.Candidates = append(full.Candidates, c1.AdaptiveCandidateResult{Tag: tag, SampledAt: now.Add(-time.Second), RTTMS: int64(100 + i), DownloadBPS: 1e6, UploadBPS: 1e6, Valid: i < 15})
	}
	full.ValidCount = 15
	if _, err := c1.NativeQualityCosts(full, now, all); err != nil {
		t.Fatal("15/18 rejected", err)
	}
	full.Candidates[14].Valid = false
	full.ValidCount = 14
	if _, err := c1.NativeQualityCosts(full, now, all); err == nil {
		t.Fatal("14/18 accepted")
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

// scoreFixture returns seven measured candidates and their RTT pre-phase
// health: six incumbents at 100 ms and one challenger at 85 ms.
func scoreFixture(now time.Time) (c1.AdaptiveResult, []c1.NativeQualityCost, map[string]bool) {
	result := c1.AdaptiveResult{}
	alive := map[string]bool{}
	var costs []c1.NativeQualityCost
	for i := 0; i < 7; i++ {
		tag := fmt.Sprintf("proxy-fixture-%02d", i)
		rtt := int64(100)
		if i == 6 {
			rtt = 85
		}
		result.Candidates = append(result.Candidates, c1.AdaptiveCandidateResult{Tag: tag, SampledAt: now, RTTMS: rtt, Valid: true})
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: 1})
		alive[tag] = true
	}
	return result, costs, alive
}
