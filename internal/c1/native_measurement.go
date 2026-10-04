package c1

import (
	"context"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

// NativeQualityEvidence retains unique upstream observations in the existing
// policy engine. It makes no routing call and starts no supervisor/poller.
func (c *Coordinator) NativeQualityEvidence(snapshot xrayapi.Snapshot) map[string]AdaptiveCandidateInput {
	result := map[string]AdaptiveCandidateInput{}
	if c == nil || c.supervisor == nil || !snapshot.ObservatoryReachable {
		return result
	}
	s := c.supervisor
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	now := s.clock()
	s.engine.Observe(now, adaptiveObservations(snapshot))
	cutoff := now.Add(-s.policy.LatencyWindow)
	for tag, samples := range s.engine.samples {
		median, count, latest := adaptiveRTTEvidence(samples, cutoff, now)
		if count < 3 {
			continue
		}
		result[tag] = AdaptiveCandidateInput{Tag: tag, RTTMS: median, Samples: count, LatestAt: latest, HealthPenalty: adaptiveWindowPenalty(samples, cutoff, now, median)}
	}
	return result
}

// MeasureNativeQuality uses the existing diagnostic lifecycle and fixed bounded
// transfer runner. It does not invoke the override-based adaptive supervisor.
func (c *Coordinator) MeasureNativeQuality(ctx context.Context, generation AdaptiveGeneration, publish func(AdaptivePerformanceStatus)) (AdaptiveResult, error) {
	c.mu.Lock()
	runner := c.adaptiveRunner
	if !c.policy.Enabled || runner == nil || runner.Probe == nil {
		c.mu.Unlock()
		return AdaptiveResult{}, ErrManualUnavailable
	}
	if runner.Probe.Blocked() {
		c.mu.Unlock()
		return AdaptiveResult{}, ErrManualCleanupPending
	}
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil {
		c.mu.Unlock()
		return AdaptiveResult{}, ErrManualBusy
	}
	var token struct{}
	select {
	case token = <-c.lifecycle:
	default:
		c.mu.Unlock()
		return AdaptiveResult{}, ErrManualBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.benchmarkCancel, c.benchmarkDone = cancel, done
	c.performanceMode = "native-quality"
	c.adaptive = AdaptivePerformanceStatus{NativeQuality: generation.NativeQuality, State: "running", Generation: generation.Generation, StartedAt: generation.StartedAt, CurrentTarget: generation.CurrentTarget, ShortlistCount: len(generation.Candidates)}
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		if c.benchmarkDone == done {
			c.benchmarkCancel, c.benchmarkDone, c.performanceMode = nil, nil, ""
		}
		c.mu.Unlock()
		close(done)
		c.lifecycle <- token
	}()
	return runner.Run(ctx, generation, func(progress AdaptivePerformanceStatus) {
		c.mu.Lock()
		c.adaptive = sanitizeAdaptiveStatus(progress)
		c.mu.Unlock()
		if publish != nil {
			publish(progress)
		}
	}), nil
}
