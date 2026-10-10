// Package nativequality provides bounded, explicit comparisons for native Xray
// balancing. Selection and failover remain owned by Xray, never a panel override.
package nativequality

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

var ErrUnavailable = errors.New("native quality comparison unavailable")

type Measurement interface {
	MeasureNativeQuality(context.Context, c1.AdaptiveGeneration, func(c1.AdaptivePerformanceStatus)) (c1.AdaptiveResult, error)
	MeasureRTT(context.Context, []string) ([]c1.RTTSample, error)
}

type Status struct {
	StartReason             string                       `json:"startReason,omitempty"`
	LatencySource           string                       `json:"latencySource,omitempty"`
	ResourceProfile         resourcepolicy.Profile       `json:"resourceProfile"`
	Limits                  resourcepolicy.Limits        `json:"limits"`
	AutomaticReason         string                       `json:"automaticReason,omitempty"`
	State                   string                       `json:"state"`
	Digest                  string                       `json:"digest,omitempty"`
	Generation              uint64                       `json:"generation"`
	Progress                c1.AdaptivePerformanceStatus `json:"progress"`
	CanStage                bool                         `json:"canStage"`
	StageReason             string                       `json:"stageReason,omitempty"`
	PoolCount               int                          `json:"poolCount"`
	ActivePoolCount         int                          `json:"activePoolCount"`
	LatencyLimitMS          int64                        `json:"latencyLimitMs"`
	EligibleCount           int                          `json:"eligibleCount"`
	ManualSample            bool                         `json:"manualSample"`
	Ranking                 []RankedNode                 `json:"ranking,omitempty"`
	AppliedRanking          []RankedNode                 `json:"appliedRanking,omitempty"`
	ReviewTrigger           string                       `json:"reviewTrigger,omitempty"`
	ReviewPhase             string                       `json:"reviewPhase,omitempty"`
	RTTValidCount           int                          `json:"rttValidCount"`
	AttemptedCount          int                          `json:"attemptedCount"`
	ValidCount              int                          `json:"validCount"`
	BatchCount              int                          `json:"batchCount"`
	AggregateBytes          int64                        `json:"aggregateBytes"`
	NextDueAt               time.Time                    `json:"nextDueAt,omitempty"`
	ReviewReason            string                       `json:"reviewReason,omitempty"`
	InspectionRequired      bool                         `json:"inspectionRequired"`
	AppliedState            string                       `json:"appliedState,omitempty"`
	AppliedJobState         string                       `json:"appliedJobState,omitempty"`
	AppliedConfigState      string                       `json:"appliedConfigState,omitempty"`
	ManualAllowanceBytes    int64                        `json:"manualAllowanceBytes"`
	QuotaState              string                       `json:"quotaState"`
	QuotaUsedBytes          int64                        `json:"quotaUsedBytes"`
	QuotaRemainingBytes     int64                        `json:"quotaRemainingBytes"`
	QuotaReviewsUsed        int                          `json:"quotaReviewsUsed"`
	QuotaNextResetAt        time.Time                    `json:"quotaNextResetAt,omitempty"`
	TotalEligible           int                          `json:"totalEligible"`
	SelectedForSpeed        int                          `json:"selectedForSpeed"`
	DeferredForFutureReview int                          `json:"deferredForFutureReview"`
	SubsetState             string                       `json:"subsetState,omitempty"`
	FairCursor              int                          `json:"fairCursor"`
	FairCursorState         string                       `json:"fairCursorState,omitempty"`
	PoolDecision            string                       `json:"poolDecision,omitempty"`
	ActivePool              []string                     `json:"activePool,omitempty"`
	OrphanedPool            []string                     `json:"orphanedPool,omitempty"`
	ActivePoolState         string                       `json:"activePoolState,omitempty"`
	RecommendedPool         []string                     `json:"recommendedPool,omitempty"`
	AppliedPool             []string                     `json:"appliedPool,omitempty"`
	NativeSelected          string                       `json:"nativeSelected,omitempty"`
	NativeSelectedState     string                       `json:"nativeSelectedState,omitempty"`
	NativeSelectedAt        time.Time                    `json:"nativeSelectedAt,omitempty"`
}

type RankedNode struct {
	Tag  string  `json:"tag"`
	Rank int     `json:"rank"`
	Cost float64 `json:"cost"`
}

type Service struct {
	Resources         *resourcepolicy.Guard
	Editor            *xkeen.ConfigEditor
	Lease             *authority.Lease
	Reader            xrayapi.Reader
	Nodes             c1.NodeReader
	Measurement       Measurement
	Probe             *c1.ProbeRouter
	Control           xrayapi.RoutingController
	mu                sync.Mutex
	status            Status
	result            c1.AdaptiveResult
	pool              []string
	cancel            context.CancelFunc
	done              chan struct{}
	closed            bool
	lastStartedAt     time.Time
	QuotaPath         string
	batchPause        time.Duration // tests shorten this; zero keeps the production floor.
	Jobs              *xkeen.Jobs
	AutomaticDisabled bool
	autoApplying      bool
	sweepPlan         sweepPlan
	// rttAlive is the review's RTT pre-phase health: present and true when the
	// tag answered through its own outbound, present and false when it did not.
	rttAlive map[string]bool
	// orphans are exact selected members that no longer resolve to an enabled
	// outbound when the review froze its candidates.
	orphans []string
}

func (s *Service) Read() Status {
	s.mu.Lock()
	value := s.status
	activeReview := s.cancel != nil || s.autoApplying
	value.ResourceProfile = s.profile()
	value.Limits = s.profile().Comparison(false)
	value.ManualAllowanceBytes = s.profile().Review().Bytes
	if s.AutomaticDisabled {
		value.AutomaticReason = "operator-disabled"
	} else if !value.ResourceProfile.Automatic {
		value.AutomaticReason = "constrained-device"
	} else if err := s.Resources.CheckConflict(); err != nil {
		value.AutomaticReason = "native-speed-conflict-or-unavailable"
	}
	if value.State == "" {
		value.State = "idle"
	}
	value.Progress.Candidates = append([]c1.AdaptiveCandidateStatus(nil), value.Progress.Candidates...)
	value.ActivePool = append([]string(nil), value.ActivePool...)
	value.AppliedPool = append([]string(nil), value.AppliedPool...)
	value.OrphanedPool = append([]string(nil), value.OrphanedPool...)
	_, err := c1.NativeQualityCosts(s.result, time.Now().UTC(), s.pool)
	value.CanStage = value.State == "completed" && value.AppliedState != "applied" && value.AppliedState != "no-op" && err == nil
	if value.State == "completed" && err != nil {
		value.StageReason = "measurement-expired-or-incomplete"
	}
	// Rank the completed measurement using the same throughput/health cost as
	// Stage. Old results remain labelled as measurements, never as applied state.
	costs, rankErr := c1.NativeQualityCosts(s.result, s.result.CompletedAt, s.pool)
	if rankErr == nil {
		byMatch := make(map[string]float64)
		for _, cost := range costs {
			byMatch[cost.Match] = cost.Value
		}
		for i, tag := range c1.NativeQualityRanking(s.result, costs) {
			value.Ranking = append(value.Ranking, RankedNode{Tag: tag, Rank: i + 1, Cost: byMatch["^"+regexp.QuoteMeta(tag)+"$"]})
		}
	}
	s.mu.Unlock()
	value.RecommendedPool = make([]string, 0, len(value.Ranking))
	for _, node := range value.Ranking {
		value.RecommendedPool = append(value.RecommendedPool, node.Tag)
	}
	if !activeReview && s.Editor != nil && s.Nodes != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		w, err := s.Editor.Workspace(ctx)
		if value.CanStage {
			value.CanStage = err == nil && w.Pending == nil && w.TargetsComplete && w.Digest == value.Digest && len(value.Ranking) >= 2
			switch {
			case err != nil || !w.TargetsComplete:
				value.StageReason = "configuration-unavailable"
			case w.Pending != nil:
				value.StageReason = "configuration-pending"
			case w.Digest != value.Digest:
				value.StageReason = "configuration-changed"
			case len(value.Ranking) < 2:
				value.StageReason = "measurement-expired-or-incomplete"
			}
		}
		value.ActivePool, value.ActivePoolCount, value.ActivePoolState, value.OrphanedPool = nil, 0, "unavailable", nil
		if err == nil && w.Pending == nil && w.TargetsComplete {
			nodes := s.Nodes(ctx)
			if pool, orphans, _, poolErr := resolveActivePool(w.Documents["05_routing.json"].Text, nodes, w.Targets, true); poolErr == nil && len(orphans) > 0 {
				value.ActivePool, value.ActivePoolCount, value.ActivePoolState = pool, len(pool), "degraded-orphaned"
				value.OrphanedPool = orphans
			} else if pool, index, poolErr := routingPool(w.Documents["05_routing.json"].Text, nodes, w.Targets); poolErr == nil {
				value.ActivePool, value.ActivePoolCount, value.ActivePoolState = pool, len(pool), "verified"
				value.AppliedRanking = appliedRankingFromWorkspace(w, pool, index)
				if value.AppliedState == "applied" {
					value.AppliedPool = append([]string(nil), pool...)
				}
			}
		}
		cancel()
	} else if activeReview {
		value.ActivePoolState = "frozen-at-review"
	} else {
		value.ActivePool, value.ActivePoolCount, value.ActivePoolState, value.OrphanedPool = nil, 0, "unavailable", nil
		if value.CanStage {
			value.CanStage, value.StageReason = false, "configuration-unavailable"
		}
	}
	if value.NativeSelectedAt.IsZero() || value.NativeSelectedAt.After(time.Now()) || time.Since(value.NativeSelectedAt) > 2*time.Minute {
		value.NativeSelected, value.NativeSelectedState = "", "unavailable"
	}
	if value.ResourceProfile.Automatic {
		if q, err := quotaState(s.QuotaPath, time.Now().UTC(), s.profile().Review().Bytes); err == nil {
			value.QuotaState = "available"
			value.QuotaUsedBytes, value.QuotaRemainingBytes = q.UsedBytes, q.RemainingBytes
			value.QuotaReviewsUsed, value.QuotaNextResetAt = q.ReviewsUsed, q.NextResetAt
			value.FairCursor = q.FairCursor
			if q.InspectionRequired && !activeReview {
				value.InspectionRequired = true
				value.CanStage = false
				value.StageReason = "inspection-required"
			}
		} else if !activeReview {
			value.QuotaState = "unavailable"
			value.CanStage = false
			value.StageReason = "quota-unavailable"
		}
	}
	return value
}

func (s *Service) profile() resourcepolicy.Profile {
	if s.Resources != nil {
		return s.Resources.Profile
	}
	return resourcepolicy.Profile{Name: "standard", Automatic: true}
}

// Native selection is recorded only from an already-required control read.
// Status GET never opens a new Xray connection or infers a current target from
// an old observation.
func observeNativeSelection(status *Status, snapshot xrayapi.Snapshot, at time.Time) {
	status.NativeSelected, status.NativeSelectedState, status.NativeSelectedAt = "", "unavailable", time.Time{}
	if snapshot.RoutingReachable && snapshot.Balancer.Override == "" && snapshot.Balancer.NativeSelected != "" {
		status.NativeSelected = snapshot.Balancer.NativeSelected
		status.NativeSelectedState = "observed"
		status.NativeSelectedAt = at
	}
}

func appliedRankingFromWorkspace(w xkeen.EditorWorkspace, pool []string, index int) []RankedNode {
	var document struct {
		Routing struct {
			Balancers []struct {
				Selector []string `json:"selector"`
				Strategy struct {
					Type     string `json:"type"`
					Settings struct {
						Costs []c1.NativeQualityCost `json:"costs"`
					} `json:"settings"`
				} `json:"strategy"`
			} `json:"balancers"`
		} `json:"routing"`
	}
	if configjson.Decode([]byte(w.Documents["05_routing.json"].Text), &document) != nil || index >= len(document.Routing.Balancers) || document.Routing.Balancers[index].Strategy.Type != "leastLoad" {
		return nil
	}
	costs := document.Routing.Balancers[index].Strategy.Settings.Costs
	// Only the exact anchored weights produced by the native quality editor can
	// be attributed to individual nodes. Custom regex/substring weights stay native.
	values := map[string]float64{}
	for _, tag := range pool {
		for _, cost := range costs {
			if cost.Regexp && cost.Match == "^"+regexp.QuoteMeta(tag)+"$" && cost.Value >= 1 && cost.Value <= 100 && !math.IsNaN(cost.Value) {
				if _, exists := values[tag]; exists {
					return nil
				}
				values[tag] = cost.Value
			}
		}
	}
	if len(values) != len(pool) {
		return nil
	}
	var ranked []RankedNode
	selectors := document.Routing.Balancers[index].Selector
	if len(selectors) == len(pool) && len(selectors) <= c1.AdaptiveMaxCandidates && exactSelectors(selectors, w.Targets) {
		for i, tag := range selectors {
			ranked = append(ranked, RankedNode{Tag: tag, Rank: i + 1, Cost: values[tag]})
		}
		return ranked
	}
	for tag, cost := range values {
		ranked = append(ranked, RankedNode{Tag: tag, Cost: cost})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Cost == ranked[j].Cost {
			return ranked[i].Tag < ranked[j].Tag
		}
		return ranked[i].Cost < ranked[j].Cost
	})
	if len(ranked) > 6 {
		ranked = ranked[:6]
	}
	for i := range ranked {
		ranked[i].Rank = i + 1
	}
	return ranked
}

// SetManualOverride uses Xray's native volatile override; it does not start the
// retired panel selector or alter native files. A pin bypasses automatic choice
// until explicitly cleared or Xray is restarted.
func (s *Service) SetManualOverride(ctx context.Context, target string) error {
	if s.Editor == nil || s.Lease == nil || s.Reader == nil || s.Control == nil || s.Nodes == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	inspectionRequired := s.status.InspectionRequired
	s.mu.Unlock()
	if inspectionRequired {
		return ErrUnavailable
	}
	if s.profile().Automatic {
		if q, err := quotaState(s.QuotaPath, time.Now().UTC(), s.profile().Review().Bytes); err != nil || q.InspectionRequired {
			return ErrUnavailable
		}
	}
	release, err := s.Lease.TryAcquire()
	if err != nil {
		return c1.ErrManualBusy
	}
	defer release()
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete {
		return ErrUnavailable
	}
	pool, index, err := routingPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil {
		return ErrUnavailable
	}
	var document struct {
		Routing struct {
			Balancers []struct {
				Tag string `json:"tag"`
			} `json:"balancers"`
		} `json:"routing"`
	}
	if configjson.Decode([]byte(w.Documents["05_routing.json"].Text), &document) != nil || index >= len(document.Routing.Balancers) {
		return ErrUnavailable
	}
	balancer := document.Routing.Balancers[index].Tag
	if target != "" {
		found := false
		for _, tag := range pool {
			if tag == target {
				found = true
				break
			}
		}
		if !found {
			return ErrUnavailable
		}
	}
	before := s.Reader.Snapshot(ctx)
	if !before.APIReachable || !before.RoutingReachable {
		return ErrUnavailable
	}
	if err := s.Control.OverrideBalancerTarget(ctx, balancer, target); err != nil {
		return ErrUnavailable
	}
	after := s.Reader.Snapshot(ctx)
	if !after.RoutingReachable || after.Balancer.Override != target {
		return ErrUnavailable
	}
	return nil
}

// Start reserves the same panel lease before reading configuration and keeps it
// through diagnostic cleanup. The coordinator supplies the existing performance
// single-flight. External CLI/cron are not serialized: digest drift rejects Stage.
func (s *Service) Start(ctx context.Context) error {
	return s.startReview(ctx, "manual", true)
}

func (s *Service) Stop() {
	s.mu.Lock()
	s.closed = true
	done := s.done
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// Stage consumes the recommendation before saving; errors/ambiguous responses
// require a workspace readback, not replay. This is an ordinary pending native
// config change, validated by the same editor. The HTTP Apply action follows it
// with the existing native restart job; Stage itself does not restart.
func (s *Service) Stage(ctx context.Context, digest string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.cancel != nil || s.status.State != "completed" || s.status.AppliedState == "applied" || s.status.AppliedState == "no-op" || digest == "" || digest != s.status.Digest {
		return "", ErrUnavailable
	}
	costs, err := c1.NativeQualityCosts(s.result, time.Now().UTC(), s.pool)
	if err != nil {
		return "", ErrUnavailable
	}
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete || w.Digest != digest {
		return "", ErrUnavailable
	}
	pool, index, err := measurementPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil || strings.Join(pool, "\x00") != strings.Join(s.pool, "\x00") {
		return "", ErrUnavailable
	}
	selected := c1.NativeQualityRanking(s.result, costs)
	if len(selected) < 2 || !exactSelectors(selected, w.Targets) {
		return "", ErrUnavailable
	}
	selectedSet := map[string]bool{}
	for _, tag := range selected {
		selectedSet["^"+regexp.QuoteMeta(tag)+"$"] = true
	}
	selectedCosts := costs[:0]
	for _, cost := range costs {
		if selectedSet[cost.Match] {
			selectedCosts = append(selectedCosts, cost)
		}
	}
	text, err := replaceRecommendation(w.Documents["05_routing.json"].Text, index, selectedCosts, selected)
	if err != nil {
		return "", ErrUnavailable
	}
	observatory, err := observatoryForPool(w.Documents["07_observatory.json"].Text, selected, !s.profile().Constrained)
	if err != nil {
		return "", ErrUnavailable
	}
	s.status.State = "consumed"
	return s.Editor.SaveTexts(ctx, digest, map[string]string{"05_routing.json": string(text), "07_observatory.json": string(observatory)})
}

// routingPool refuses unmanaged selector matches instead of giving them Xray's
// default cost=1. This reads the applied pool, not the next diagnostic sample.
func routingPool(text string, nodes []c1.NodeState, targets []xkeen.ConfigTarget) ([]string, int, error) {
	pool, orphans, index, err := resolveActivePool(text, nodes, targets, false)
	if err != nil || len(orphans) != 0 {
		return nil, 0, ErrUnavailable
	}
	return pool, index, nil
}

// resolveActivePool resolves the bal-proxy selector against enabled managed
// outbounds. With allowOrphans, an exact quality selector (one with its own
// anchored native cost) that no longer resolves to an enabled outbound, for
// example after subscription churn, is returned as an orphan: an unhealthy
// member for the next review to replace (REQ-003). Every other mismatch,
// including an unresolved broad prefix, is still refused.
func resolveActivePool(text string, nodes []c1.NodeState, targets []xkeen.ConfigTarget, allowOrphans bool) ([]string, []string, int, error) {
	var doc struct {
		Routing struct {
			Balancers []struct {
				Tag      string
				Selector []string
				Strategy struct{ Type string }
			}
		}
	}
	if configjson.Decode([]byte(text), &doc) != nil {
		return nil, nil, 0, ErrUnavailable
	}
	index := -1
	var selectors []string
	for i, b := range doc.Routing.Balancers {
		if b.Tag == "bal-proxy" {
			if index >= 0 || (b.Strategy.Type != "leastPing" && b.Strategy.Type != "leastLoad") {
				return nil, nil, 0, ErrUnavailable
			}
			index = i
			selectors = b.Selector
		}
	}
	if index < 0 || len(selectors) == 0 {
		return nil, nil, 0, ErrUnavailable
	}
	exactCost := map[string]bool{}
	if allowOrphans {
		// Only the review path reads costs, and tolerantly: a malformed entry
		// simply does not mark its selector as quality-owned.
		var costs struct {
			Routing struct {
				Balancers []struct {
					Strategy struct {
						Settings struct {
							Costs []json.RawMessage `json:"costs"`
						} `json:"settings"`
					} `json:"strategy"`
				} `json:"balancers"`
			} `json:"routing"`
		}
		if configjson.Decode([]byte(text), &costs) == nil && index < len(costs.Routing.Balancers) {
			for _, raw := range costs.Routing.Balancers[index].Strategy.Settings.Costs {
				var cost c1.NativeQualityCost
				if json.Unmarshal(raw, &cost) == nil && cost.Regexp {
					exactCost[cost.Match] = true
				}
			}
		}
	}
	// P3-3: a duplicated selector can never be repaired by one review.
	distinct := make(map[string]bool, len(selectors))
	for _, selector := range selectors {
		if distinct[selector] {
			return nil, nil, 0, ErrUnavailable
		}
		distinct[selector] = true
	}
	disabled := map[string]bool{}
	for _, n := range nodes {
		if !n.Enabled {
			disabled[n.Tag] = true
		}
	}
	orphanable := func(selector string) bool {
		return allowOrphans && exactCost["^"+regexp.QuoteMeta(selector)+"$"]
	}
	selectorMatched := make([]bool, len(selectors))
	match := func(tag string) (int, bool) {
		index := -1
		for i, prefix := range selectors {
			if prefix == "" {
				return -1, false
			}
			if strings.HasPrefix(tag, prefix) {
				if index >= 0 {
					return -1, false
				}
				index = i
			}
		}
		return index, true
	}
	var pool []string
	seen := map[string]bool{}
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		selector, valid := match(n.Tag)
		if !valid {
			return nil, nil, 0, ErrUnavailable
		}
		if selector >= 0 {
			if seen[n.Tag] {
				return nil, nil, 0, ErrUnavailable
			}
			seen[n.Tag] = true
			pool = append(pool, n.Tag)
		}
	}
	if len(pool) > c1.MaxRegistryNodes {
		return nil, nil, 0, ErrUnavailable
	}
	var orphans []string
	disabledExact := make([]bool, len(selectors))
	matched := map[string]bool{}
	for _, target := range targets {
		if target.Kind != "outbound" {
			continue
		}
		selector, valid := match(target.Tag)
		if !valid {
			return nil, nil, 0, ErrUnavailable
		}
		if selector < 0 {
			continue
		}
		if !seen[target.Tag] {
			// A disabled registry member whose outbound is still present. It is
			// classified after the loop so outbound order cannot change it.
			if target.Tag == selectors[selector] && orphanable(target.Tag) && disabled[target.Tag] {
				disabledExact[selector] = true
				continue
			}
			return nil, nil, 0, ErrUnavailable
		}
		if matched[target.Tag] {
			return nil, nil, 0, ErrUnavailable
		}
		matched[target.Tag] = true
		selectorMatched[selector] = true
	}
	if len(matched) != len(pool) {
		return nil, nil, 0, ErrUnavailable
	}
	for i, ok := range selectorMatched {
		if ok {
			if disabledExact[i] {
				// The selector also prefixes an enabled outbound: it is not exact.
				return nil, nil, 0, ErrUnavailable
			}
			continue
		}
		if !orphanable(selectors[i]) {
			return nil, nil, 0, ErrUnavailable
		}
		orphans = append(orphans, selectors[i])
	}
	if len(pool)+len(orphans) < 2 || len(pool) == 0 && !allowOrphans {
		return nil, nil, 0, ErrUnavailable
	}
	sort.Strings(pool)
	sort.Strings(orphans)
	return pool, orphans, index, nil
}

// A restricted active selector must not restrict future diagnostic samples.
func measurementPool(text string, nodes []c1.NodeState, targets []xkeen.ConfigTarget) ([]string, int, error) {
	// Full validation owns native selector compatibility. Even an exhausted
	// restricted pool must allow a new test of enabled alternatives.
	var doc struct {
		Routing struct {
			Balancers []struct {
				Tag      string
				Strategy struct{ Type string }
			}
		}
	}
	if configjson.Decode([]byte(text), &doc) != nil {
		return nil, 0, ErrUnavailable
	}
	index := -1
	for i, b := range doc.Routing.Balancers {
		if b.Tag == "bal-proxy" {
			if index >= 0 || b.Strategy.Type != "leastLoad" && b.Strategy.Type != "leastPing" {
				return nil, 0, ErrUnavailable
			}
			index = i
		}
	}
	if index < 0 {
		return nil, 0, ErrUnavailable
	}
	available := map[string]int{}
	for _, target := range targets {
		if target.Kind == "outbound" {
			available[target.Tag]++
		}
	}
	var pool []string
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Enabled {
			if seen[n.Tag] || available[n.Tag] != 1 {
				return nil, 0, ErrUnavailable
			}
			seen[n.Tag] = true
			pool = append(pool, n.Tag)
		}
	}
	if len(pool) < 2 || len(pool) > c1.MaxRegistryNodes {
		return nil, 0, ErrUnavailable
	}
	sort.Strings(pool)
	return pool, index, nil
}

// Xray selectors are prefixes even when a full tag is supplied.
func exactSelectors(selected []string, targets []xkeen.ConfigTarget) bool {
	for _, prefix := range selected {
		matches := 0
		for _, target := range targets {
			if target.Kind == "outbound" && strings.HasPrefix(target.Tag, prefix) {
				if target.Tag != prefix {
					return false
				}
				matches++
			}
		}
		if matches != 1 {
			return false
		}
	}
	return true
}
