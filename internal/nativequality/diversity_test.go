package nativequality

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func TestEighteenSlotShortlistKeepsOtherSources(t *testing.T) {
	var inputs []c1.AdaptiveCandidateInput
	groups := make(map[string]string)
	for i := 0; i < 24; i++ {
		tag := fmt.Sprintf("a%02d", i)
		inputs = append(inputs, c1.AdaptiveCandidateInput{Tag: tag, RTTMS: int64(10 + i)})
		groups[tag] = "a"
	}
	for i, group := range []string{"b", "c"} {
		for j := 0; j < 6; j++ {
			tag := fmt.Sprintf("%s%02d", group, j)
			inputs = append(inputs, c1.AdaptiveCandidateInput{Tag: tag, RTTMS: int64(100 + i*10 + j)})
			groups[tag] = group
		}
	}
	got := fastestAnswering(inputs, 18, groups)
	counts := map[string]int{}
	for _, candidate := range got {
		counts[groups[candidate.Tag]]++
	}
	if len(got) != 18 || counts["a"] != 6 || counts["b"] != 6 || counts["c"] != 6 {
		t.Fatalf("18-slot source balance = %v", counts)
	}
}

func TestFastestAnsweringTieBreakAndDisabledSource(t *testing.T) {
	groups := sourceGroupsForNodes([]c1.NodeState{{Tag: "z", Enabled: true, SourceGroup: "a"}, {Tag: "a", Enabled: true, SourceGroup: "b"}, {Tag: "disabled", Enabled: false, SourceGroup: "c"}})
	if _, exists := groups["disabled"]; exists {
		t.Fatal("disabled source admitted into quality grouping")
	}
	input := []c1.AdaptiveCandidateInput{{Tag: "z", RTTMS: 10}, {Tag: "a", RTTMS: 10}}
	got := fastestAnswering(input, 2, groups)
	if got[0].Tag != "a" || got[1].Tag != "z" {
		t.Fatalf("equal RTT tie break = %+v", got)
	}
}

func TestFastestAnsweringBalancesSourcesBeforeRTTRank(t *testing.T) {
	var inputs []c1.AdaptiveCandidateInput
	groups := make(map[string]string)
	for i, tag := range []string{"a1", "a2", "a3", "a4", "b1", "b2", "c1", "c2"} {
		inputs = append(inputs, c1.AdaptiveCandidateInput{Tag: tag, RTTMS: int64(10 + i)})
		groups[tag] = string(tag[0])
	}
	got := fastestAnswering(inputs, 6, groups)
	var tags []string
	for _, item := range got {
		tags = append(tags, item.Tag)
	}
	if want := []string{"a1", "b1", "c1", "a2", "b2", "c2"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("balanced shortlist = %v, want %v", tags, want)
	}
}

func TestSpreadRankedTagsFallsBackToValidAvailableSources(t *testing.T) {
	groups := map[string]string{"a1": "a", "a2": "a", "a3": "a", "b1": "b", "c1": "c"}
	input := []string{"a1", "a2", "a3", "b1", "c1"}
	if got, want := spreadRankedTags(input, groups, 5), []string{"a1", "b1", "c1", "a2", "a3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source fallback = %v, want %v", got, want)
	}
	if got := spreadRankedTags([]string{"x", "y", "z"}, nil, 3); !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Fatalf("unknown sources changed rank order: %v", got)
	}
}

func TestRankPoolUsesValidSourcesAndConfiguredSize(t *testing.T) {
	now := time.Now().UTC()
	s := &Service{sourceGroups: map[string]string{"a1": "a", "a2": "a", "a3": "a", "a4": "a", "b1": "b", "b2": "b", "c1": "c", "c2": "c"}}
	result := c1.AdaptiveResult{NativeQuality: true, Sweep: true, Generation: 1, State: "completed", StartedAt: now.Add(-time.Minute), CompletedAt: now}
	var costs []c1.NativeQualityCost
	for i, tag := range []string{"a1", "a2", "a3", "a4", "b1", "b2", "c1", "c2"} {
		result.Candidates = append(result.Candidates, c1.AdaptiveCandidateResult{Tag: tag, SampledAt: now, RTTMS: int64(10 + i), DownloadBPS: 1e6, UploadBPS: 1e6, Valid: true})
		costs = append(costs, c1.NativeQualityCost{Match: "^" + tag + "$", Value: 1})
	}
	if got, want := s.rankPool(result, costs), []string{"a1", "b1", "c1", "a2", "b2", "c2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("balanced six = %v, want %v", got, want)
	}
	result.Candidates[7].Valid = false // One source has only one usable member.
	if got, want := s.rankPool(result, costs), []string{"a1", "b1", "c1", "a2", "b2", "a3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid-sample fallback = %v, want %v", got, want)
	}
}

func TestRepeatCandidatesBalancesSources(t *testing.T) {
	now := time.Now().UTC()
	s := &Service{sourceGroups: make(map[string]string)}
	result := c1.AdaptiveResult{NativeQuality: true, Sweep: true, Generation: 1, State: "completed", StartedAt: now.Add(-time.Minute), CompletedAt: now}
	var admitted []c1.AdaptiveCandidateInput
	for i, group := range []string{"a", "a", "a", "a", "a", "a", "a", "a", "b", "b", "b", "c", "c", "c"} {
		tag := fmt.Sprintf("proxy-%s%02d", group, i)
		s.sourceGroups[tag] = group
		rtt := int64(10 + i)
		admitted = append(admitted, c1.AdaptiveCandidateInput{Tag: tag, RTTMS: rtt})
		s.pool = append(s.pool, tag)
		result.Candidates = append(result.Candidates, c1.AdaptiveCandidateResult{Tag: tag, SampledAt: now, RTTMS: rtt, DownloadBPS: 1e6, UploadBPS: 1e6, Valid: true})
	}
	result.ShortlistCount = len(result.Candidates)
	result.SweepEligibleCount = len(result.Candidates)
	result.ValidCount = len(result.Candidates)
	got := s.repeatCandidates(result, admitted, 8)
	var groups []string
	for _, candidate := range got {
		groups = append(groups, s.sourceGroups[candidate.Tag])
	}
	if want := []string{"a", "b", "c", "a", "b", "c", "a", "b"}; !reflect.DeepEqual(groups, want) {
		t.Fatalf("second-pass source order = %v, want %v", groups, want)
	}
}
