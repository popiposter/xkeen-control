package c1

import (
	"fmt"
	"math"
	"regexp"
	"testing"
	"time"
)

func TestNativeSweepRequiresCompleteFreshEightyPercentCoverage(t *testing.T) {
	now := time.Now().UTC()
	r := AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: 14, Generation: 1, StartedAt: now.Add(-15 * time.Minute), CompletedAt: now, State: "completed", ShortlistCount: 14}
	var pool []string
	for i := 0; i < 14; i++ {
		tag := fmt.Sprintf("proxy-%02d", i)
		pool = append(pool, tag)
		r.Candidates = append(r.Candidates, AdaptiveCandidateResult{Tag: tag, SampledAt: now.Add(-time.Duration(14-i) * time.Minute), RTTMS: int64(100 + i), Valid: i < 12, DownloadBPS: 1e6, UploadBPS: 1e6})
	}
	if costs, err := NativeQualityCosts(r, now, pool); err != nil || len(costs) != 14 || len(NativeQualityRanking(r, costs)) != 6 {
		t.Fatalf("complete sweep rejected: %v %v", costs, err)
	}
	r.Candidates[11].Valid = false
	if _, err := NativeQualityCosts(r, now, pool); err == nil {
		t.Fatal("eleven of fourteen valid samples admitted")
	}
	r.Candidates[11].Valid = true
	r.Candidates[0].SampledAt = now.Add(-31 * time.Minute)
	if _, err := NativeQualityCosts(r, now, pool); err == nil {
		t.Fatal("stale sample admitted")
	}
	r.Candidates[0].SampledAt = now.Add(-14 * time.Minute)
	r.Candidates = r.Candidates[:13]
	r.ShortlistCount = 13
	if _, err := NativeQualityCosts(r, now, pool); err == nil {
		t.Fatal("incomplete eligible coverage admitted")
	}
}

func TestBalancedRankingSelectsSixFromBroadSampleInsteadOfWeakLowPing(t *testing.T) {
	now := time.Now()
	r := AdaptiveResult{NativeQuality: true, BroadSample: true, Generation: 1, State: "completed", StartedAt: now.Add(-4 * time.Minute), CompletedAt: now, ShortlistCount: 12}
	var pool []string
	for i := 0; i < 12; i++ {
		tag := fmt.Sprintf("proxy-%02d", i)
		pool = append(pool, tag)
		r.Candidates = append(r.Candidates, AdaptiveCandidateResult{Tag: tag, RTTMS: int64(150 + i*5), DownloadBPS: 80e6 / 8, UploadBPS: 30e6 / 8, Valid: true})
	}
	r.Candidates[0].DownloadBPS = 8.4e6 / 8
	r.Candidates[1].HealthPenalty = 4
	r.Candidates[11].Valid = false
	costs, err := NativeQualityCosts(r, now, pool)
	if err != nil {
		t.Fatal(err)
	}
	ranked := NativeQualityRanking(r, costs)
	if len(ranked) != 6 || ranked[0] != "proxy-02" || ranked[5] != "proxy-07" {
		t.Fatal("weak low ping or instability displaced balanced candidates", ranked)
	}
	r.BroadSample = false
	if _, err := NativeQualityCosts(r, now, pool); err == nil {
		t.Fatal("automatic budget silently expanded")
	}
}

func TestQualityFastDownloadCanBeatLowPing(t *testing.T) {
	results := []AdaptiveCandidateResult{
		{Tag: "proxy-low-ping", RTTMS: 20, DownloadBPS: 1e6, UploadBPS: 1e6, Valid: true},
		{Tag: "proxy-fast", RTTMS: 60, DownloadBPS: 100e6, UploadBPS: 10e6, Valid: true},
	}
	winner, best, current, eligible := scoreAdaptiveResults(results, "proxy-low-ping")
	if winner != "proxy-fast" || !eligible || best/current < AdaptiveQualityHysteresis {
		t.Fatalf("fast candidate excluded: %s %g %g %v", winner, best, current, eligible)
	}
}

func TestQualityHealthPenaltyOutweighsBurstSpeed(t *testing.T) {
	penalty := 3.0
	results := []AdaptiveCandidateResult{
		{Tag: "proxy-stable", RTTMS: 60, DownloadBPS: 10e6, UploadBPS: 10e6, Valid: true},
		{Tag: "proxy-flaky", RTTMS: 60, DownloadBPS: 20e6, UploadBPS: 20e6, Valid: true, HealthPenalty: penalty},
	}
	winner, _, _, _ := scoreAdaptiveResults(results, "proxy-stable")
	if winner != "proxy-stable" {
		t.Fatal("burst speed defeated repeated failure evidence")
	}
}

func TestNativeQualityCostsRemainBoundedExactAndFresh(t *testing.T) {
	now := time.Now()
	result := AdaptiveResult{Generation: 1, ShortlistCount: 3, State: "completed", StartedAt: now.Add(-time.Minute), CompletedAt: now, Candidates: []AdaptiveCandidateResult{
		{Tag: "proxy-a.b", Valid: true, DownloadBPS: 1e100, UploadBPS: 1e100},
		{Tag: "proxy-slow", Valid: true, DownloadBPS: 1e-100, UploadBPS: 1e-100},
		{Tag: "proxy-failed", Valid: false},
	}}
	pool := []string{"proxy-a.b", "proxy-slow", "proxy-failed", "proxy-unmeasured"}
	costs, err := NativeQualityCosts(result, now, pool)
	if err != nil || len(costs) != len(pool) {
		t.Fatalf("costs=%v err=%v", costs, err)
	}
	for _, cost := range costs {
		if !cost.Regexp || cost.Value < 1 || cost.Value > 100 || math.IsNaN(cost.Value) {
			t.Fatalf("invalid native cost=%+v", cost)
		}
	}
	match := regexp.MustCompile(costs[0].Match)
	if !match.MatchString("proxy-a.b") || match.MatchString("proxy-aXb") || match.MatchString("proxy-a.b-backup") {
		t.Fatal("cost leaked onto unrelated native outbound")
	}
	if costs[1].Value != 100 || costs[3].Value != 100 {
		t.Fatal("failed/unknown backup given best native cost")
	}
	if _, err := NativeQualityCosts(result, now.Add(31*time.Minute), pool); err == nil {
		t.Fatal("expired recommendation accepted")
	}
	result.ShortlistCount++
	if _, err := NativeQualityCosts(result, now, pool); err == nil {
		t.Fatal("partial generation accepted")
	}
}

func TestQualityRejectsInvalidHealthEvidence(t *testing.T) {
	for _, penalty := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, 0.5} {
		for _, index := range []int{0, 1} {
			results := calibratedPair(10, 1)
			results[index].HealthPenalty = penalty
			winner, _, _, eligible := scoreAdaptiveResults(results, "proxy-current")
			if winner == results[index].Tag || adaptiveCandidateValid(results, results[index].Tag) || countAdaptiveValid(results) != 1 || index == 1 && eligible {
				t.Fatalf("invalid penalty participated: index=%d penalty=%g winner=%s", index, penalty, winner)
			}
		}
	}
}
