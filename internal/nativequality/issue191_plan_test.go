//go:build linux

package nativequality

import (
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

// TestIssue191ReadOnlyPlanQualification is safe to run in a disposable MIPS
// stage. It has no network, config editor, native job, or production path.
func TestIssue191ReadOnlyPlanQualification(t *testing.T) {
	now := time.Now().UTC()
	eligible := make([]c1.AdaptiveCandidateInput, 46)
	all := make([]string, 46)
	for i := range eligible {
		tag := fmt.Sprintf("proxy-fixture-%02d", i)
		all[i] = tag
		rtt := int64(100 + i)
		if i < 6 {
			rtt += 1000
		} // incumbents are anchors, not RTT winners.
		eligible[i] = c1.AdaptiveCandidateInput{Tag: tag, RTTMS: rtt, LatestAt: now}
	}
	// prepareWithCriteria emits RTT/tag order; the planner is tested with that
	// actual order, not an assumed registry order.
	for i := range eligible {
		for j := i + 1; j < len(eligible); j++ {
			if eligible[j].RTTMS < eligible[i].RTTMS {
				eligible[i], eligible[j] = eligible[j], eligible[i]
			}
		}
	}
	active := append([]string(nil), all[:6]...)
	plan, err := planSweep(eligible, active, active[0], 0, "")
	if err != nil || plan.TotalEligible != 46 || len(plan.Candidates) != 18 || plan.Deferred != 28 || plan.FirstInitialization {
		t.Fatal("bounded 18/46 plan", err, plan.TotalEligible, len(plan.Candidates), plan.Deferred)
	}
	seen := map[string]bool{}
	for _, candidate := range plan.Candidates {
		if seen[candidate.Tag] {
			t.Fatal("duplicate candidate")
		}
		seen[candidate.Tag] = true
	}
	for _, tag := range active {
		if !seen[tag] {
			t.Fatal("incumbent omitted")
		}
	}
	for _, tag := range all[6:12] {
		if !seen[tag] {
			t.Fatal("lowest challenger omitted")
		}
	}
	broad, err := planSweep(eligible, all, active[0], 0, "")
	if err != nil || !broad.FirstInitialization || len(broad.Candidates) != 18 || broad.Candidates[0].Tag != active[0] {
		t.Fatal("broad selector did not anchor native selection", err)
	}
	if broad.Candidates[1].Tag != all[6] || broad.Candidates[2].Tag != all[7] {
		t.Fatal("broad selector did not retain two lowest-RTT backups")
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
	next, err := planSweep(eligible, active, active[0], prior.FairCursor, prior.EligibleSetHash)
	if err != nil || next.CursorState != "continued" || next.NextCursor == plan.NextCursor {
		t.Fatal("cursor did not advance", err)
	}
	initialRotation := map[string]bool{}
	for _, candidate := range plan.Candidates[12:] {
		initialRotation[candidate.Tag] = true
	}
	for _, candidate := range next.Candidates[12:] {
		if initialRotation[candidate.Tag] {
			t.Fatal("immediate rotation repeated")
		}
	}
	// Across repeated receipts every non-anchor alternative gets an opportunity.
	covered := map[string]bool{}
	cursor, hash := 0, ""
	for review := 0; review < 6; review++ {
		p, e := planSweep(eligible, active, active[0], cursor, hash)
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range p.Candidates[12:] {
			covered[c.Tag] = true
		}
		cursor, hash = p.NextCursor, p.EligibleSetHash
	}
	for _, tag := range all[12:] {
		if !covered[tag] {
			t.Fatal("deferred alternative never reached", tag)
		}
	}
	drifted := append([]c1.AdaptiveCandidateInput(nil), eligible...)
	drifted[len(drifted)-1].Tag = "proxy-fixture-new"
	reanchored, err := planSweep(drifted, active, active[0], cursor, hash)
	if err != nil || reanchored.CursorState != "reanchored" || len(reanchored.Candidates) != 18 {
		t.Fatal("membership drift did not reanchor", err)
	}
	// Same members never restart solely to refresh floating weights or order.
	result, costs, live := scoreFixture(now)
	if got := poolDecision(result, costs, active, []string{active[5], active[4], active[3], active[2], active[1], active[0]}, sweepPlan{NativeSelected: active[0], Freshness: 2 * time.Minute}, live, now); got != "pool-unchanged" {
		t.Fatal(got)
	}
	changed := []string{active[0], active[1], active[2], active[3], active[4], all[6]}
	if got := poolDecision(result, costs, active, changed, sweepPlan{NativeSelected: active[0], Freshness: 2 * time.Minute}, live, now); got != "material-improvement" {
		t.Fatal("15 percent boundary", got)
	}
	for i := range result.Candidates {
		if result.Candidates[i].Tag == all[6] {
			result.Candidates[i].RTTMS = 86
		}
	}
	if got := poolDecision(result, costs, active, changed, sweepPlan{NativeSelected: active[0], Freshness: 2 * time.Minute}, live, now); got != "insufficient-challenger-margin" {
		t.Fatal("under 15 percent accepted", got)
	}
	for i := range live.OutboundHealth {
		if live.OutboundHealth[i].Tag == active[5] {
			live.OutboundHealth[i].Alive = false
		}
	}
	if got := poolDecision(result, costs, active, changed, sweepPlan{NativeSelected: active[0], Freshness: 2 * time.Minute}, live, now); got != "unhealthy-incumbent-replaced" {
		t.Fatal("unhealthy exception", got)
	}
	for i := range result.Candidates {
		if result.Candidates[i].Tag == active[0] {
			result.Candidates[i].Valid = false
		}
	}
	if got := poolDecision(result, costs, active, changed, sweepPlan{NativeSelected: active[0], Freshness: 2 * time.Minute}, live, now); got != "healthy-target-sample-invalid" {
		t.Fatal("one selected-target transfer failure retired it", got)
	}
	// Fifteen of eighteen valid samples qualifies the bounded subset; fourteen
	// does not. The other 28 are deferred, not recorded as failed transfers.
	full := c1.AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: 18, Generation: 1, State: "completed", StartedAt: now.Add(-time.Minute), CompletedAt: now, ShortlistCount: 18}
	for i, candidate := range plan.Candidates {
		full.Candidates = append(full.Candidates, c1.AdaptiveCandidateResult{Tag: candidate.Tag, SampledAt: now.Add(-time.Second), RTTMS: candidate.RTTMS, DownloadBPS: 1e6, UploadBPS: 1e6, Valid: i < 15})
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

func scoreFixture(now time.Time) (c1.AdaptiveResult, []c1.NativeQualityCost, xrayapi.Snapshot) {
	result := c1.AdaptiveResult{}
	live := xrayapi.Snapshot{Balancer: xrayapi.BalancerState{NativeSelected: "proxy-fixture-00"}}
	var costs []c1.NativeQualityCost
	for i := 0; i < 7; i++ {
		tag := fmt.Sprintf("proxy-fixture-%02d", i)
		rtt := int64(100)
		if i == 6 {
			rtt = 85
		}
		result.Candidates = append(result.Candidates, c1.AdaptiveCandidateResult{Tag: tag, SampledAt: now, RTTMS: rtt, Valid: true})
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: 1})
		live.OutboundHealth = append(live.OutboundHealth, xrayapi.OutboundHealth{Tag: tag, Alive: true, LastTry: now})
	}
	return result, costs, live
}
