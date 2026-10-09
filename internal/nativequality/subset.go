package nativequality

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

type sweepPlan struct {
	Candidates          []c1.AdaptiveCandidateInput
	Active              []string
	NativeSelected      string
	TotalEligible       int
	Deferred            int
	FirstInitialization bool
	NextCursor          int
	EligibleSetHash     string
	CursorState         string
	Freshness           time.Duration
}

// planSweep chooses a bounded review, not a purported global best. Fresh
// candidates arrive in RTT/tag order. The residual rotation uses tag order so
// latency fluctuations cannot continually return it to the same first slice.
func planSweep(eligible []c1.AdaptiveCandidateInput, active []string, nativeSelected string, priorCursor int, priorHash string) (sweepPlan, error) {
	if len(eligible) < 2 || len(eligible) > c1.MaxRegistryNodes || len(active) < 2 || priorCursor < 0 {
		return sweepPlan{}, ErrUnavailable
	}
	plan := sweepPlan{Active: append([]string(nil), active...), NativeSelected: nativeSelected, TotalEligible: len(eligible), FirstInitialization: len(active) > 6}
	byTag := make(map[string]c1.AdaptiveCandidateInput, len(eligible))
	var tags []string
	for _, candidate := range eligible {
		if candidate.Tag == "" || candidate.RTTMS <= 0 {
			return sweepPlan{}, ErrUnavailable
		}
		if _, exists := byTag[candidate.Tag]; exists {
			return sweepPlan{}, ErrUnavailable
		}
		byTag[candidate.Tag] = candidate
		tags = append(tags, candidate.Tag)
	}
	sort.Strings(tags)
	hash := sha256.Sum256([]byte(strings.Join(tags, "\x00")))
	plan.EligibleSetHash = hex.EncodeToString(hash[:])
	if len(eligible) <= sweepMaxEligible {
		plan.Candidates = append([]c1.AdaptiveCandidateInput(nil), eligible...)
		plan.CursorState = "all-eligible"
		plan.NextCursor = priorCursor
		return plan, nil
	}
	chosen := make(map[string]bool, sweepMaxEligible)
	appendCandidate := func(candidate c1.AdaptiveCandidateInput) {
		if !chosen[candidate.Tag] && len(plan.Candidates) < sweepMaxEligible {
			chosen[candidate.Tag] = true
			plan.Candidates = append(plan.Candidates, candidate)
		}
	}
	incumbent := make(map[string]bool, len(active))
	for _, tag := range active {
		if incumbent[tag] {
			return sweepPlan{}, ErrUnavailable
		}
		incumbent[tag] = true
	}
	if plan.FirstInitialization {
		if candidate, ok := byTag[nativeSelected]; ok && incumbent[nativeSelected] {
			appendCandidate(candidate)
		}
		backups := 0
		for _, candidate := range eligible {
			if incumbent[candidate.Tag] && !chosen[candidate.Tag] && backups < 2 {
				appendCandidate(candidate)
				backups++
			}
		}
	} else {
		for _, candidate := range eligible {
			if incumbent[candidate.Tag] {
				appendCandidate(candidate)
			}
		}
	}
	challengers := 0
	for _, candidate := range eligible {
		if !chosen[candidate.Tag] && challengers < 6 {
			appendCandidate(candidate)
			challengers++
		}
	}
	residual := make([]c1.AdaptiveCandidateInput, 0, len(eligible))
	for _, tag := range tags {
		if !chosen[tag] {
			residual = append(residual, byTag[tag])
		}
	}
	if len(residual) == 0 {
		return sweepPlan{}, ErrUnavailable
	}
	start := priorCursor % len(residual)
	plan.CursorState = "continued"
	if priorHash == "" {
		plan.CursorState = "initial"
	}
	if priorHash != "" && priorHash != plan.EligibleSetHash {
		shift := int(binary.BigEndian.Uint64(hash[:8]) % uint64(len(residual)))
		if shift == 0 {
			shift = 1
		}
		start = (start + shift) % len(residual)
		plan.CursorState = "reanchored"
	}
	rotating := sweepMaxEligible - len(plan.Candidates)
	for i := 0; i < rotating; i++ {
		appendCandidate(residual[(start+i)%len(residual)])
	}
	plan.NextCursor = (start + rotating) % len(residual)
	plan.Deferred = len(eligible) - len(plan.Candidates)
	if len(plan.Candidates) != sweepMaxEligible {
		return sweepPlan{}, ErrUnavailable
	}
	return plan, nil
}
