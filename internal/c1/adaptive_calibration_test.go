package c1

import (
	"math"
	"testing"
)

func calibratedPair(multiplier, unit float64) []AdaptiveCandidateResult {
	return []AdaptiveCandidateResult{
		{Tag: "proxy-current", RTTMS: 50, DownloadBPS: 10 * unit, UploadBPS: 5 * unit, Valid: true},
		{Tag: "proxy-challenger", RTTMS: 50, DownloadBPS: 10 * multiplier * unit, UploadBPS: 5 * multiplier * unit, Valid: true},
	}
}

func TestAdaptiveCalibrationMaterialThroughput(t *testing.T) {
	for _, multiplier := range []float64{2, 10} {
		results := calibratedPair(multiplier, float64(MiB))
		winner, best, current, challenger := scoreAdaptiveResults(results, "proxy-current")
		// Independent acceptance intervals, not a copy of the score expression.
		minimum, maximum := 1.19, 1.20
		if multiplier == 10 {
			minimum, maximum = 1.81, 1.83
		}
		gain := best / current
		if winner != "proxy-challenger" || !challenger || gain < minimum || gain > maximum || gain < AdaptiveQualityHysteresis {
			t.Fatalf("%gx throughput: winner=%s gain=%.9f challenger=%v", multiplier, winner, gain, challenger)
		}
		t.Logf("%gx throughput at equal RTT: gain %.6f%%", multiplier, 100*(gain-1))
	}
}

func TestAdaptiveCalibrationUnitsAndUnrelatedExtreme(t *testing.T) {
	baseline := calibratedPair(2, 1)
	_, _, _, _ = scoreAdaptiveResults(baseline, "proxy-current")
	want := baseline[1].Score / baseline[0].Score
	for _, unit := range []float64{1e-9, 1, float64(MiB), 1e100} {
		for _, extreme := range []bool{false, true} {
			results := calibratedPair(2, unit)
			if extreme {
				// Outside the RTT guard: not a selectable winner, but its rates
				// still participate in normalization and must not distort this pair.
				results = append(results, AdaptiveCandidateResult{Tag: "proxy-extreme", RTTMS: 1000, DownloadBPS: 1e200, UploadBPS: 1e150, Valid: true})
			}
			winner, _, _, _ := scoreAdaptiveResults(results, "proxy-current")
			got := results[1].Score / results[0].Score
			if winner != "proxy-challenger" || math.Abs(got-want) > 1e-12 {
				t.Fatalf("unit=%g extreme=%v winner=%s ratio=%.15g want=%.15g", unit, extreme, winner, got, want)
			}
		}
	}
}

func TestAdaptiveCalibrationFiniteExtremeRates(t *testing.T) {
	results := calibratedPair(1, 1)
	results[0].DownloadBPS, results[0].UploadBPS = math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64
	results[1].DownloadBPS, results[1].UploadBPS = math.MaxFloat64, math.MaxFloat64
	winner, best, current, _ := scoreAdaptiveResults(results, "proxy-current")
	if winner != "proxy-challenger" || !finitePositive(current) || !finitePositive(best) || best > 1 || current >= best {
		t.Fatalf("finite extreme scores winner=%s current=%g best=%g", winner, current, best)
	}
}

func TestAdaptiveCalibrationInvalidRatesAndRTTGuards(t *testing.T) {
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, upload := range []bool{false, true} {
			results := calibratedPair(10, float64(MiB))
			if upload {
				results[1].UploadBPS = rate
			} else {
				results[1].DownloadBPS = rate
			}
			winner, _, _, challenger := scoreAdaptiveResults(results, "proxy-current")
			if winner != "proxy-current" || challenger {
				t.Fatalf("invalid rate=%g upload=%v winner=%s challenger=%v", rate, upload, winner, challenger)
			}
		}
	}
	for _, test := range []struct {
		currentRTT, challengerRTT int64
		allowed                   bool
	}{{50, 100, true}, {50, 101, true}, {100, 750, true}, {100, 751, false}, {50, 0, false}, {50, -1, false}} {
		results := calibratedPair(10, float64(MiB))
		results[0].RTTMS, results[1].RTTMS = test.currentRTT, test.challengerRTT
		winner, _, _, challenger := scoreAdaptiveResults(results, "proxy-current")
		if challenger != test.allowed || (winner == "proxy-challenger") != test.allowed {
			t.Fatalf("RTT %+v: winner=%s challenger=%v", test, winner, challenger)
		}
	}
}
