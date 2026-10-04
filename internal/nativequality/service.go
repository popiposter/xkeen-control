// Package nativequality provides bounded, explicit comparisons for native Xray
// balancing. Selection and failover remain owned by Xray, never a panel override.
package nativequality

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

var ErrUnavailable = errors.New("native quality comparison unavailable")

type Measurement interface {
	MeasureNativeQuality(context.Context, c1.AdaptiveGeneration, func(c1.AdaptivePerformanceStatus)) (c1.AdaptiveResult, error)
	NativeQualityEvidence(xrayapi.Snapshot) map[string]c1.AdaptiveCandidateInput
}

type Status struct {
	State          string                       `json:"state"`
	Digest         string                       `json:"digest,omitempty"`
	Generation     uint64                       `json:"generation"`
	Progress       c1.AdaptivePerformanceStatus `json:"progress"`
	CanStage       bool                         `json:"canStage"`
	PoolCount      int                          `json:"poolCount"`
	LatencyLimitMS int64                        `json:"latencyLimitMs"`
	EligibleCount  int                          `json:"eligibleCount"`
	ManualSample   bool                         `json:"manualSample"`
	Ranking        []RankedNode                 `json:"ranking,omitempty"`
	AppliedRanking []RankedNode                 `json:"appliedRanking,omitempty"`
}

type RankedNode struct {
	Tag  string  `json:"tag"`
	Rank int     `json:"rank"`
	Cost float64 `json:"cost"`
}

type Service struct {
	Editor        *xkeen.ConfigEditor
	Lease         *authority.Lease
	Reader        xrayapi.Reader
	Nodes         c1.NodeReader
	Measurement   Measurement
	Control       xrayapi.RoutingController
	mu            sync.Mutex
	status        Status
	result        c1.AdaptiveResult
	pool          []string
	cancel        context.CancelFunc
	done          chan struct{}
	closed        bool
	lastStartedAt time.Time
}

func (s *Service) Read() Status {
	s.mu.Lock()
	value := s.status
	if value.State == "" {
		value.State = "idle"
	}
	value.Progress.Candidates = append([]c1.AdaptiveCandidateStatus(nil), value.Progress.Candidates...)
	_, err := c1.NativeQualityCosts(s.result, time.Now().UTC(), s.pool)
	value.CanStage = value.State == "completed" && err == nil
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
	if value.CanStage && s.Editor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		w, err := s.Editor.Workspace(ctx)
		cancel()
		value.CanStage = err == nil && w.Pending == nil && w.TargetsComplete && w.Digest == value.Digest && len(value.Ranking) >= 2
	}
	value.AppliedRanking = s.appliedRanking()
	return value
}

func (s *Service) appliedRanking() []RankedNode {
	if s.Editor == nil || s.Nodes == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w, err := s.Editor.Workspace(ctx)
	if err != nil || !w.TargetsComplete || w.Pending != nil {
		return nil
	}
	pool, index, err := routingPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil {
		return nil
	}
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
	return s.start(ctx, true)
}

func (s *Service) start(ctx context.Context, broad bool) error {
	if s.Editor == nil || s.Lease == nil || s.Reader == nil || s.Nodes == nil || s.Measurement == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || s.closed {
		return c1.ErrManualBusy
	}
	release, err := s.Lease.TryAcquire()
	if err != nil {
		return c1.ErrManualBusy
	}
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete {
		release()
		return ErrUnavailable
	}
	pool, _, err := measurementPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil {
		release()
		return err
	}
	snapshot := s.Reader.Snapshot(ctx)
	mode := 0
	if broad {
		mode = 1
	}
	generation, err := prepare(snapshot, pool, s.status.Generation+1, mode, time.Now().UTC())
	if err != nil {
		release()
		return err
	}
	retainHealthEvidence(&generation, s.Measurement.NativeQualityEvidence(snapshot))
	s.lastStartedAt = time.Now().UTC()
	wall := c1.AdaptiveMaxGenerationWallTime
	if broad {
		wall = c1.NativeQualityBroadWallTime
	}
	job, cancel := context.WithTimeout(context.Background(), wall+c1.AdaptiveCleanupReserve)
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done
	s.pool = append([]string(nil), pool...)
	s.result = c1.AdaptiveResult{}
	s.status = Status{State: "running", Digest: w.Digest, Generation: generation.Generation, PoolCount: len(pool), ManualSample: broad, LatencyLimitMS: latencyLimit(generation.Candidates[0].RTTMS, broad), EligibleCount: eligibleCount(snapshot, pool, broad, generation.StartedAt), Progress: c1.AdaptivePerformanceStatus{State: "running", ShortlistCount: len(generation.Candidates)}}
	go func() {
		defer close(done)
		result, runErr := s.Measurement.MeasureNativeQuality(job, generation, func(progress c1.AdaptivePerformanceStatus) {
			s.mu.Lock()
			s.status.Progress = progress
			s.mu.Unlock()
		})
		cancel()
		release()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cancel = nil
		s.result = result
		s.status.State = result.State
		if runErr != nil {
			s.status.State = "failed"
			s.status.Progress.State = "failed"
			s.status.Progress.ReasonCode = "busy-or-unavailable"
		}
	}()
	return nil
}

func retainHealthEvidence(generation *c1.AdaptiveGeneration, evidenceByTag map[string]c1.AdaptiveCandidateInput) {
	for _, candidates := range [][]c1.AdaptiveCandidateInput{generation.Candidates, generation.Fallbacks} {
		for i, candidate := range candidates {
			if evidence, ok := evidenceByTag[candidate.Tag]; ok {
				candidates[i].Samples = evidence.Samples
				candidates[i].HealthPenalty = evidence.HealthPenalty
			}
		}
	}
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
	if s.closed || s.cancel != nil || s.status.State != "completed" || digest == "" || digest != s.status.Digest {
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
	strategy := struct {
		Type     string `json:"type"`
		Settings any    `json:"settings"`
	}{"leastLoad", struct {
		Expected int                    `json:"expected"`
		MaxRTT   string                 `json:"maxRTT"`
		Costs    []c1.NativeQualityCost `json:"costs"`
	}{1, "750ms", selectedCosts}}
	text, err := configjson.ReplacePath([]byte(w.Documents["05_routing.json"].Text), []string{"routing", "balancers", strconv.Itoa(index), "strategy"}, strategy)
	if err != nil {
		return "", ErrUnavailable
	}
	text, err = configjson.ReplacePath(text, []string{"routing", "balancers", strconv.Itoa(index), "selector"}, selected)
	if err != nil {
		return "", ErrUnavailable
	}
	s.status.State = "consumed"
	return s.Editor.SaveTexts(ctx, digest, map[string]string{"05_routing.json": string(text)})
}

// routingPool refuses unmanaged selector matches instead of giving them Xray's
// default cost=1. This reads the applied pool, not the next diagnostic sample.
func routingPool(text string, nodes []c1.NodeState, targets []xkeen.ConfigTarget) ([]string, int, error) {
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
		return nil, 0, ErrUnavailable
	}
	index := -1
	var selectors []string
	for i, b := range doc.Routing.Balancers {
		if b.Tag == "bal-proxy" {
			if index >= 0 || (b.Strategy.Type != "leastPing" && b.Strategy.Type != "leastLoad") {
				return nil, 0, ErrUnavailable
			}
			index = i
			selectors = b.Selector
		}
	}
	if index < 0 || len(selectors) == 0 {
		return nil, 0, ErrUnavailable
	}
	var pool []string
	seen := map[string]bool{}
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		for _, prefix := range selectors {
			if prefix == "" {
				return nil, 0, ErrUnavailable
			}
			if strings.HasPrefix(n.Tag, prefix) {
				if seen[n.Tag] {
					return nil, 0, ErrUnavailable
				}
				seen[n.Tag] = true
				pool = append(pool, n.Tag)
				break
			}
		}
	}
	if len(pool) < 2 || len(pool) > c1.MaxRegistryNodes {
		return nil, 0, ErrUnavailable
	}
	matched := map[string]bool{}
	for _, target := range targets {
		if target.Kind != "outbound" {
			continue
		}
		for _, prefix := range selectors {
			if strings.HasPrefix(target.Tag, prefix) {
				if !seen[target.Tag] || matched[target.Tag] {
					return nil, 0, ErrUnavailable
				}
				matched[target.Tag] = true
				break
			}
		}
	}
	if len(matched) != len(pool) {
		return nil, 0, ErrUnavailable
	}
	sort.Strings(pool)
	return pool, index, nil
}

func prepare(snapshot xrayapi.Snapshot, pool []string, id uint64, mode int, now time.Time) (c1.AdaptiveGeneration, error) {
	if !snapshot.APIReachable || !snapshot.RoutingReachable || !snapshot.ObservatoryReachable || snapshot.Balancer.Override != "" {
		return c1.AdaptiveGeneration{}, ErrUnavailable
	}
	known := map[string]bool{}
	for _, tag := range pool {
		known[tag] = true
	}
	var eligible []c1.AdaptiveCandidateInput
	seen := map[string]bool{}
	for _, h := range snapshot.OutboundHealth {
		if !known[h.Tag] {
			continue
		}
		if seen[h.Tag] {
			return c1.AdaptiveGeneration{}, ErrUnavailable
		}
		seen[h.Tag] = true
		if !h.Alive || h.DelayMS <= 0 || h.DelayMS > c1.AdaptiveMaximumRTTMS || h.LastTry.IsZero() || h.LastTry.After(now) || now.Sub(h.LastTry) > 2*time.Minute {
			continue
		}
		// One observation is not a stability window. No invented loss/jitter evidence.
		eligible = append(eligible, c1.AdaptiveCandidateInput{Tag: h.Tag, RTTMS: h.DelayMS, LatestAt: h.LastTry})
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].RTTMS == eligible[j].RTTMS {
			return eligible[i].Tag < eligible[j].Tag
		}
		return eligible[i].RTTMS < eligible[j].RTTMS
	})
	if len(eligible) < 2 {
		return c1.AdaptiveGeneration{}, ErrUnavailable
	}
	broad := mode == 1
	limit := latencyLimit(eligible[0].RTTMS, broad)
	eligible = eligible[:sort.Search(len(eligible), func(i int) bool { return eligible[i].RTTMS > limit })]
	if len(eligible) < 2 {
		return c1.AdaptiveGeneration{}, ErrUnavailable
	}
	maxCandidates, maxAttempts := c1.AdaptiveMaxCandidates, c1.NativeQualityMaxAttempts
	if broad {
		maxCandidates, maxAttempts = c1.NativeQualityBroadCandidates, c1.NativeQualityBroadAttempts
	}
	initial := min(len(eligible), maxCandidates)
	end := min(len(eligible), maxAttempts)
	return c1.AdaptiveGeneration{Generation: id, StartedAt: now, CurrentTarget: snapshot.Balancer.NativeSelected,
		NativeQuality: true, BroadSample: broad, Candidates: eligible[:initial], Fallbacks: eligible[initial:end]}, nil
}

func latencyLimit(lowest int64, broad bool) int64 {
	if !broad {
		return c1.AdaptiveMaximumRTTMS
	}
	return min(int64(c1.AdaptiveMaximumRTTMS), max(int64(300), 2*lowest))
}

func eligibleCount(snapshot xrayapi.Snapshot, pool []string, broad bool, now time.Time) int {
	known := map[string]bool{}
	for _, tag := range pool {
		known[tag] = true
	}
	lowest := int64(c1.AdaptiveMaximumRTTMS)
	for _, h := range snapshot.OutboundHealth {
		if known[h.Tag] && h.Alive && h.DelayMS > 0 && h.DelayMS <= lowest && !h.LastTry.IsZero() && !h.LastTry.After(now) && now.Sub(h.LastTry) <= 2*time.Minute {
			lowest = h.DelayMS
		}
	}
	count := 0
	for _, h := range snapshot.OutboundHealth {
		if known[h.Tag] && h.Alive && h.DelayMS > 0 && h.DelayMS <= latencyLimit(lowest, broad) && !h.LastTry.IsZero() && !h.LastTry.After(now) && now.Sub(h.LastTry) <= 2*time.Minute {
			count++
		}
	}
	return count
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
