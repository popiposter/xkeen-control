package nativequality

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/popiposter/xkeen-control/internal/c1"
)

type sweepPlan struct {
	Candidates          []string
	Active              []string
	NativeSelected      string
	TotalEligible       int
	Deferred            int
	FirstInitialization bool
	NextCursor          int
	EligibleSetHash     string
	CursorState         string
}

// planSweep freezes a bounded review candidate set, not a purported global
// best. It needs no Observatory evidence: every candidate gets a targeted RTT
// probe before any speed transfer. Incumbents always take part, so they can be
// kept or replaced on fresh evidence; the rest of the slots rotate through the
// remaining enabled nodes in tag order using the durable fair cursor. A broad
// selector (first initialization) keeps only the native-selected member.
func planSweep(eligible []string, active []string, nativeSelected string, priorCursor int, priorHash string) (sweepPlan, error) {
	if len(eligible) < 2 || len(eligible) > c1.MaxRegistryNodes || len(active) < 1 || priorCursor < 0 {
		return sweepPlan{}, ErrUnavailable
	}
	plan := sweepPlan{Active: append([]string(nil), active...), NativeSelected: nativeSelected, TotalEligible: len(eligible), FirstInitialization: len(active) > 6}
	known := make(map[string]bool, len(eligible))
	tags := make([]string, 0, len(eligible))
	for _, tag := range eligible {
		if tag == "" || known[tag] {
			return sweepPlan{}, ErrUnavailable
		}
		known[tag] = true
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	hash := sha256.Sum256([]byte(strings.Join(tags, "\x00")))
	plan.EligibleSetHash = hex.EncodeToString(hash[:])
	if len(tags) <= sweepMaxEligible {
		plan.Candidates = tags
		plan.CursorState = "all-eligible"
		plan.NextCursor = priorCursor
		return plan, nil
	}
	chosen := make(map[string]bool, sweepMaxEligible)
	add := func(tag string) {
		if known[tag] && !chosen[tag] && len(plan.Candidates) < sweepMaxEligible {
			chosen[tag] = true
			plan.Candidates = append(plan.Candidates, tag)
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
		if incumbent[nativeSelected] {
			add(nativeSelected)
		}
	} else {
		for _, tag := range tags {
			if incumbent[tag] {
				add(tag)
			}
		}
	}
	residual := make([]string, 0, len(tags))
	for _, tag := range tags {
		if !chosen[tag] {
			residual = append(residual, tag)
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
		add(residual[(start+i)%len(residual)])
	}
	plan.NextCursor = (start + rotating) % len(residual)
	plan.Deferred = len(tags) - len(plan.Candidates)
	if len(plan.Candidates) != sweepMaxEligible {
		return sweepPlan{}, ErrUnavailable
	}
	return plan, nil
}
