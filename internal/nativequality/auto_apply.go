package nativequality

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

const sweepJobOwner = "native-quality-sweep"

func sameRecommendation(text string, index int, selected []string, costs []c1.NativeQualityCost) bool {
	var doc struct {
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
	if configjson.Decode([]byte(text), &doc) != nil || index < 0 || index >= len(doc.Routing.Balancers) {
		return false
	}
	b := doc.Routing.Balancers[index]
	if !reflect.DeepEqual(b.Selector, selected) {
		return false
	}
	if b.Strategy.Type == "leastPing" {
		return true
	}
	if b.Strategy.Type != "leastLoad" || len(b.Strategy.Settings.Costs) != len(costs) {
		return false
	}
	byMatch := make(map[string]float64, len(costs))
	for _, cost := range costs {
		if !cost.Regexp {
			return false
		}
		if _, exists := byMatch[cost.Match]; exists {
			return false
		}
		byMatch[cost.Match] = cost.Value
	}
	for _, cost := range b.Strategy.Settings.Costs {
		value, found := byMatch[cost.Match]
		if !found || !cost.Regexp || value != cost.Value {
			return false
		}
		delete(byMatch, cost.Match)
	}
	return len(byMatch) == 0
}

func (s *Service) applyOutcome(state, reason string, inspection bool) {
	s.mu.Lock()
	s.status.AppliedState = state
	s.status.ReviewReason = reason
	s.status.InspectionRequired = s.status.InspectionRequired || inspection
	s.mu.Unlock()
}

// applySweep never retries Save or native Restart. The existing editor's
// pending digest and Jobs.beginApply recheck are the interposition fence.
func (s *Service) applySweep(parent context.Context, result c1.AdaptiveResult, digest string, pool []string) {
	if s.Jobs == nil || s.Editor == nil || s.Resources == nil {
		s.applyOutcome("not-applied", "application-unavailable", false)
		return
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if err := s.Resources.CheckConflict(); err != nil {
		s.applyOutcome("not-applied", "native-speed-conflict-or-unavailable", false)
		return
	}
	guarded, stop, err := s.Resources.Start(ctx)
	if err != nil {
		s.applyOutcome("not-applied", "resource-pressure-or-unavailable", false)
		return
	}
	guardErr := guarded.Err()
	stop()
	if guardErr != nil {
		s.applyOutcome("not-applied", "resource-pressure-or-unavailable", false)
		return
	}
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete || w.Digest != digest {
		s.applyOutcome("not-applied", "configuration-changed-or-pending", false)
		return
	}
	currentPool, index, err := measurementPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil || strings.Join(currentPool, "\x00") != strings.Join(pool, "\x00") {
		s.applyOutcome("not-applied", "node-profile-changed", false)
		return
	}
	currentActive, _, err := routingPool(w.Documents["05_routing.json"].Text, s.Nodes(ctx), w.Targets)
	if err != nil {
		s.applyOutcome("not-applied", "selector-unavailable", false)
		return
	}
	plan := s.sweepPlan
	if plan.TotalEligible == 0 {
		plan = sweepPlan{Active: currentActive, NativeSelected: result.CurrentTarget, FirstInitialization: len(currentActive) > 6}
	} else if !samePoolMembers(currentActive, plan.Active) || result.CurrentTarget != plan.NativeSelected {
		s.applyOutcome("not-applied", "selector-changed", false)
		return
	}
	snapshot := s.Reader.Snapshot(ctx)
	s.mu.Lock()
	observeNativeSelection(&s.status, snapshot, time.Now().UTC())
	s.mu.Unlock()
	if !snapshot.APIReachable || !snapshot.RoutingReachable || !snapshot.ObservatoryReachable || snapshot.Balancer.Override != "" || !s.Reader.ProbeReachable(ctx) {
		s.applyOutcome("not-applied", "native-override-or-unavailable", false)
		return
	}
	costs, err := c1.NativeQualityCosts(result, time.Now().UTC(), pool)
	if err != nil {
		s.applyOutcome("not-applied", "review-insufficient-or-expired", false)
		return
	}
	selected := c1.NativeQualityRanking(result, costs)
	if len(selected) < 2 || !exactSelectors(selected, w.Targets) {
		s.applyOutcome("not-applied", "selector-unavailable", false)
		return
	}
	wanted := make(map[string]bool, len(selected))
	for _, tag := range selected {
		wanted["^"+regexp.QuoteMeta(tag)+"$"] = true
	}
	selectedCosts := make([]c1.NativeQualityCost, 0, len(selected))
	for _, cost := range costs {
		if wanted[cost.Match] {
			selectedCosts = append(selectedCosts, cost)
		}
	}
	if len(selectedCosts) != len(selected) {
		s.applyOutcome("not-applied", "selector-unavailable", false)
		return
	}
	s.mu.Lock()
	alive := s.rttAlive
	s.mu.Unlock()
	decision := poolDecision(result, costs, currentActive, selected, plan, alive, snapshot.Balancer.NativeSelected, time.Now().UTC())
	s.mu.Lock()
	s.status.PoolDecision = decision
	s.mu.Unlock()
	if decision == "pool-unchanged" {
		s.applyOutcome("no-op", "pool-unchanged", false)
		return
	}
	if decision != "first-pool-initialization" && decision != "material-improvement" && decision != "unhealthy-incumbent-replaced" {
		s.applyOutcome("not-applied", decision, false)
		return
	}
	proposed, err := replaceRecommendation(w.Documents["05_routing.json"].Text, index, selectedCosts, selected)
	if err != nil {
		s.applyOutcome("not-applied", "recommendation-invalid", false)
		return
	}
	// From this point, an error can follow a durable save and must be inspected.
	if parent.Err() != nil {
		s.applyOutcome("not-applied", "review-cancelled", false)
		return
	}
	saveCtx, saveCancel := context.WithTimeout(parent, 180*time.Second)
	saved, err := s.Editor.SaveTexts(saveCtx, digest, map[string]string{"05_routing.json": string(proposed)})
	saveCancel()
	if err != nil {
		s.applyOutcome("inspection-required", "save-outcome-unknown", true)
		return
	}
	postCtx, postCancel := context.WithTimeout(parent, 10*time.Second)
	post, err := s.Editor.Workspace(postCtx)
	postCancel()
	if err != nil || post.Pending == nil || post.Digest != saved || post.Pending.Drift || post.Pending.ApplyID != "" {
		s.applyOutcome("inspection-required", "saved-digest-unconfirmed", true)
		return
	}
	postPool, _, poolErr := measurementPool(post.Documents["05_routing.json"].Text, s.Nodes(parent), post.Targets)
	postActive, _, activeErr := routingPool(post.Documents["05_routing.json"].Text, s.Nodes(parent), post.Targets)
	if poolErr != nil || activeErr != nil || strings.Join(postPool, "\x00") != strings.Join(pool, "\x00") || !samePoolMembers(postActive, selected) {
		s.applyOutcome("inspection-required", "saved-membership-unconfirmed", true)
		return
	}
	job, err := s.Jobs.ApplyConfigs(sweepJobOwner, s.Editor, saved)
	if err != nil {
		s.applyOutcome("inspection-required", "apply-admission-unknown", true)
		return
	}
	s.mu.Lock()
	s.status.AppliedState = "running"
	s.status.AppliedJobState = job.State
	s.mu.Unlock()
	readCtx, readCancel := context.WithTimeout(parent, 4*time.Minute)
	defer readCancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for job.State == "running" {
		select {
		case <-readCtx.Done():
			s.applyOutcome("inspection-required", "apply-outcome-unknown", true)
			return
		case <-tick.C:
			job, err = s.Jobs.Read(sweepJobOwner, job.ID, 0)
			if err != nil {
				s.applyOutcome("inspection-required", "apply-readback-unavailable", true)
				return
			}
		}
	}
	s.mu.Lock()
	s.status.AppliedJobState = job.State
	s.status.AppliedConfigState = job.ConfigurationState
	s.mu.Unlock()
	if job.State != "completed" || job.ConfigurationState != "applied" || job.ExitCode == nil || *job.ExitCode != 0 {
		s.applyOutcome("inspection-required", "apply-not-verified", true)
		return
	}
	verified, err := s.Editor.Workspace(readCtx)
	if err != nil || verified.Pending != nil || verified.Digest != saved || !sameRecommendation(verified.Documents["05_routing.json"].Text, index, selected, selectedCosts) {
		s.applyOutcome("inspection-required", "applied-config-readback-mismatch", true)
		return
	}
	actual := s.Reader.Snapshot(readCtx)
	selectedSet := make(map[string]bool, len(selected))
	for _, tag := range selected {
		selectedSet[tag] = true
	}
	if !actual.APIReachable || !actual.RoutingReachable || !actual.ObservatoryReachable || actual.Balancer.Override != "" || !selectedSet[actual.Balancer.NativeSelected] || !s.Reader.ProbeReachable(readCtx) {
		s.applyOutcome("inspection-required", "native-selection-readback-unavailable", true)
		return
	}
	s.mu.Lock()
	s.status.ActivePoolCount = len(selected)
	s.status.ActivePool = append([]string(nil), selected...)
	s.status.ActivePoolState = "verified-after-apply"
	s.status.AppliedPool = append([]string(nil), selected...)
	observeNativeSelection(&s.status, actual, time.Now().UTC())
	s.mu.Unlock()
	s.applyOutcome("applied", "", false)
}
