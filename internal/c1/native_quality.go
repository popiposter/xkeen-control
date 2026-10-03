package c1

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"time"
)

// NativeQualityCost is a native leastLoad cost, not an API balancer override.
// Exact regex matches avoid Xray's ordinary substring/prefix tag matching.
type NativeQualityCost struct {
	Regexp bool    `json:"regexp"`
	Match  string  `json:"match"`
	Value  float64 `json:"value"`
}

// NativeQualityCosts proposes bounded throughput/health weights for an existing
// native balancer. It never writes configuration, removes backup candidates or
// applies an override. Native health filtering remains the selection owner,
// including after the panel stops. Callers must preserve selector/fallback and
// validate the complete config through the native editor before explicit Apply.
func NativeQualityCosts(result AdaptiveResult, now time.Time, pool []string) ([]NativeQualityCost, error) {
	invalid := errors.New("fresh complete quality measurements required")
	if result.Generation == 0 || result.State != "completed" || result.StartedAt.IsZero() || result.CompletedAt.Before(result.StartedAt) || result.CompletedAt.Sub(result.StartedAt) > AdaptiveMaxGenerationWallTime+AdaptiveCleanupReserve || result.CompletedAt.IsZero() || result.CompletedAt.After(now) || now.Sub(result.CompletedAt) > 30*time.Minute || len(result.Candidates) > AdaptiveMaxCandidates || result.ShortlistCount != len(result.Candidates) || len(pool) > MaxRegistryNodes {
		return nil, invalid
	}
	known := make(map[string]bool, len(pool))
	for _, tag := range pool {
		if !validTag(tag) || known[tag] {
			return nil, invalid
		}
		known[tag] = true
	}
	var bestDown, bestUp float64
	seen := make(map[string]bool)
	valid := make([]AdaptiveCandidateResult, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if !known[candidate.Tag] || seen[candidate.Tag] {
			return nil, invalid
		}
		seen[candidate.Tag] = true
		if !candidate.Valid || !finitePositive(candidate.DownloadBPS) || !finitePositive(candidate.UploadBPS) || !finitePositive(adaptiveHealthPenalty(candidate.HealthPenalty)) {
			continue
		}
		valid = append(valid, candidate)
		bestDown = math.Max(bestDown, candidate.DownloadBPS)
		bestUp = math.Max(bestUp, candidate.UploadBPS)
	}
	if len(valid) < 2 {
		return nil, invalid
	}
	// Native leastLoad multiplies RTT/deviation by sqrt(cost). Do not count RTT
	// twice: this component weights throughput and observed health only. Keep all
	// unmeasured backups eligible with conservative cost, never blacklist them.
	// Otherwise Xray's default cost=1 would favor every unmeasured node.
	values := make(map[string]float64, len(pool))
	for _, tag := range pool {
		values[tag] = 100
	}
	for _, candidate := range valid {
		logQuality := 0.75*(math.Log(candidate.DownloadBPS)-math.Log(bestDown)) + 0.25*(math.Log(candidate.UploadBPS)-math.Log(bestUp)) - math.Log(adaptiveHealthPenalty(candidate.HealthPenalty))
		cost := math.Exp(math.Min(-2*logQuality, math.Log(100)))
		values[candidate.Tag] = clampFloat(cost, 1, 100)
	}
	costs := make([]NativeQualityCost, 0, len(pool))
	for _, tag := range pool {
		costs = append(costs, NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: values[tag]})
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i].Match < costs[j].Match })
	return costs, nil
}
