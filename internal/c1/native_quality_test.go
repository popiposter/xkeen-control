package c1

import (
	"fmt"
	"math"
	"regexp"
	"testing"
	"time"
)

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

func TestQualityPenalizesRepeatedFailuresAndJitter(t *testing.T) {
	now := time.Now()
	values := []sample{{now.Add(-3 * time.Minute), 30, true}, {now.Add(-2 * time.Minute), 90, true}, {now.Add(-time.Minute), 60, false}, {now, 60, true}}
	penalty := adaptiveWindowPenalty(values, now.Add(-5*time.Minute), now, 60)
	if penalty <= 2 || !finitePositive(penalty) {
		t.Fatalf("failure/jitter penalty=%g", penalty)
	}
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

func TestPreferredCostsReduceNoiseWithoutRemovingBackups(t *testing.T) {
	costs := []NativeQualityCost{{true, "^proxy-a$", 2}, {true, "^proxy-b$", 3}, {true, "^proxy-c$", 100}}
	out, err := NativeQualityPreferredCosts(costs, []string{"proxy-a", "proxy-b", "proxy-c"})
	if err != nil || len(out) != 3 || out[0].Value != 2 || out[1].Value != 12 || out[2].Value != 400 {
		t.Fatal(out, err)
	}
	if costs[1].Value != 3 {
		t.Fatal("measurement costs mutated")
	}
	// A small RTT fluctuation no longer beats the measured leader; a major
	// slowdown still can. Dead/maxRTT filtering is still Xray's responsibility.
	if !(170*math.Sqrt(out[0].Value) < 150*math.Sqrt(out[1].Value)) || !(700*math.Sqrt(out[0].Value) > 150*math.Sqrt(out[1].Value)) {
		t.Fatal("preference margin lost")
	}
	for _, pool := range [][]string{{"proxy-a"}, {"proxy-a", "proxy-a"}, {"proxy-a", "proxy-missing"}} {
		if _, err := NativeQualityPreferredCosts(costs, pool); err == nil {
			t.Fatal("invalid pool admitted", pool)
		}
	}
}
