package nativequality

import (
	"math"
	"regexp"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func samePoolMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, tag := range a {
		if seen[tag] {
			return false
		}
		seen[tag] = true
	}
	for _, tag := range b {
		if !seen[tag] {
			return false
		}
		delete(seen, tag)
	}
	return len(seen) == 0
}

// poolDecision compares only measured scores. One failed transfer from a still
// healthy selected node is never evidence to retire it. Health comes from this
// review's RTT pre-phase: a tag that answered is healthy, a tag that did not is
// unhealthy, and an unprobed tag is unknown, never an outage.
func poolDecision(result c1.AdaptiveResult, costs []c1.NativeQualityCost, active, selected []string, plan sweepPlan, alive map[string]bool, nativeSelected string, now time.Time) string {
	// A REQ-009 recovery pool was never speed-ranked: the first complete
	// review applies its measured top six, even with the same members, so
	// the equal recovery costs are replaced by measured ones.
	if plan.Provisional {
		if len(selected) < 2 || len(selected) > 6 {
			return "initial-pool-invalid"
		}
		return "provisional-pool-replaced"
	}
	if samePoolMembers(active, selected) {
		return "pool-unchanged"
	}
	valid := make(map[string]float64, len(result.Candidates))
	weights := make(map[string]float64, len(costs))
	for _, cost := range costs {
		weights[cost.Match] = cost.Value
	}
	for _, candidate := range result.Candidates {
		if candidate.Valid && candidate.RTTMS > 0 && !candidate.SampledAt.IsZero() && now.Sub(candidate.SampledAt) <= 30*time.Minute {
			weight := weights["^"+regexp.QuoteMeta(candidate.Tag)+"$"]
			if weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0) {
				valid[candidate.Tag] = float64(candidate.RTTMS) * math.Sqrt(weight)
			}
		}
	}
	healthy := func(tag string) bool {
		probed, ok := alive[tag]
		return ok && probed
	}
	unhealthy := func(tag string) bool {
		probed, ok := alive[tag]
		return ok && !probed
	}
	for _, tag := range []string{plan.NativeSelected, nativeSelected} {
		if tag != "" && healthy(tag) && valid[tag] == 0 {
			return "healthy-target-sample-invalid"
		}
	}
	if plan.FirstInitialization {
		if len(selected) < 2 || len(selected) > 6 {
			return "initial-pool-invalid"
		}
		return "first-pool-initialization"
	}
	old := make(map[string]bool, len(active))
	for _, tag := range active {
		old[tag] = true
	}
	newPool := make(map[string]bool, len(selected))
	for _, tag := range selected {
		newPool[tag] = true
	}
	// A full, entirely healthy pool is replaced only on at least six valid
	// speed results; a smaller sample cannot rank a complete alternative.
	if len(active) >= 6 && len(valid) < 6 {
		allHealthy := true
		for _, tag := range active {
			if !healthy(tag) {
				allHealthy = false
				break
			}
		}
		if allHealthy {
			return "insufficient-valid-results"
		}
	}
	var incoming, replaced []float64
	removedUnhealthy := false
	for _, tag := range selected {
		if !old[tag] {
			incoming = append(incoming, valid[tag])
		}
	}
	for _, tag := range active {
		if newPool[tag] {
			continue
		}
		if unhealthy(tag) {
			removedUnhealthy = true
			continue
		}
		if !healthy(tag) || valid[tag] == 0 {
			return "incumbent-unmeasured-or-unknown"
		}
		replaced = append(replaced, valid[tag])
	}
	if len(incoming) < len(replaced) {
		return "insufficient-challenger-margin"
	}
	sort.Float64s(incoming)
	sort.Float64s(replaced)
	for i, incumbent := range replaced {
		if incoming[i] <= 0 || incoming[i] > incumbent*0.85 {
			return "insufficient-challenger-margin"
		}
	}
	if len(replaced) == 0 && !removedUnhealthy {
		// A pure addition should improve the best measured incumbent materially.
		best := math.Inf(1)
		for _, tag := range active {
			if score := valid[tag]; score > 0 && score < best {
				best = score
			}
		}
		if len(incoming) == 0 || math.IsInf(best, 1) || incoming[0] > best*0.85 {
			return "insufficient-challenger-margin"
		}
	}
	if removedUnhealthy {
		return "unhealthy-incumbent-replaced"
	}
	return "material-improvement"
}
