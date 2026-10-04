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
	rotation      int
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
		for _, candidate := range s.result.Candidates {
			if candidate.Valid && candidate.DownloadBPS > 0 && candidate.UploadBPS > 0 && !math.IsNaN(candidate.DownloadBPS) && !math.IsInf(candidate.DownloadBPS, 0) && !math.IsNaN(candidate.UploadBPS) && !math.IsInf(candidate.UploadBPS, 0) {
				value.Ranking = append(value.Ranking, RankedNode{Tag: candidate.Tag, Cost: byMatch["^"+regexp.QuoteMeta(candidate.Tag)+"$"]})
			}
		}
		sort.Slice(value.Ranking, func(i, j int) bool {
			if value.Ranking[i].Cost == value.Ranking[j].Cost {
				return value.Ranking[i].Tag < value.Ranking[j].Tag
			}
			return value.Ranking[i].Cost < value.Ranking[j].Cost
		})
		for i := range value.Ranking {
			value.Ranking[i].Rank = i + 1
		}
	}
	s.mu.Unlock()
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
	pool, _, err := routingPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil {
		release()
		return err
	}
	snapshot := s.Reader.Snapshot(ctx)
	generation, err := prepare(snapshot, pool, s.status.Generation+1, s.rotation, time.Now().UTC())
	if err != nil {
		release()
		return err
	}
	evidenceByTag := s.Measurement.NativeQualityEvidence(snapshot)
	for i, candidate := range generation.Candidates {
		if evidence, ok := evidenceByTag[candidate.Tag]; ok {
			generation.Candidates[i].Samples = evidence.Samples
			generation.Candidates[i].HealthPenalty = evidence.HealthPenalty
		}
	}
	s.rotation++
	s.lastStartedAt = time.Now().UTC()
	job, cancel := context.WithTimeout(context.Background(), c1.AdaptiveMaxGenerationWallTime+c1.AdaptiveCleanupReserve)
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done
	s.pool = append([]string(nil), pool...)
	s.result = c1.AdaptiveResult{}
	s.status = Status{State: "running", Digest: w.Digest, Generation: generation.Generation, PoolCount: len(pool), Progress: c1.AdaptivePerformanceStatus{State: "running", ShortlistCount: len(generation.Candidates)}}
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
// config change, validated by the same editor; no restart or selection write.
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
	pool, index, err := routingPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil || strings.Join(pool, "\x00") != strings.Join(s.pool, "\x00") {
		return "", ErrUnavailable
	}
	strategy := struct {
		Type     string `json:"type"`
		Settings any    `json:"settings"`
	}{"leastLoad", struct {
		Expected int                    `json:"expected"`
		MaxRTT   string                 `json:"maxRTT"`
		Costs    []c1.NativeQualityCost `json:"costs"`
	}{1, "750ms", costs}}
	text, err := configjson.ReplacePath([]byte(w.Documents["05_routing.json"].Text), []string{"routing", "balancers", strconv.Itoa(index), "strategy"}, strategy)
	if err != nil {
		return "", ErrUnavailable
	}
	s.status.State = "consumed"
	return s.Editor.SaveTexts(ctx, digest, map[string]string{"05_routing.json": string(text)})
}

// routingPool refuses unmanaged selector matches instead of giving them Xray's
// default cost=1. Keep every enabled managed backup and all unrelated JSONC bytes.
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

func prepare(snapshot xrayapi.Snapshot, pool []string, id uint64, rotation int, now time.Time) (c1.AdaptiveGeneration, error) {
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
	g := c1.AdaptiveGeneration{Generation: id, StartedAt: now, CurrentTarget: snapshot.Balancer.NativeSelected}
	var other []c1.AdaptiveCandidateInput
	for _, n := range eligible {
		if n.Tag == g.CurrentTarget {
			g.Candidates = append(g.Candidates, n)
		} else {
			other = append(other, n)
		}
	}
	if len(g.Candidates) != 1 || len(other) == 0 {
		return g, ErrUnavailable
	}
	fast := len(other)
	if fast > 4 {
		fast = 4
	}
	g.Candidates = append(g.Candidates, other[:fast]...)
	if len(other) > fast {
		rest := other[fast:]
		sort.Slice(rest, func(i, j int) bool { return rest[i].Tag < rest[j].Tag })
		g.Candidates = append(g.Candidates, rest[rotation%len(rest)])
	}
	return g, nil
}
