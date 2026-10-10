package c1

import (
	"context"
)

// MeasureNativeQuality uses the existing diagnostic lifecycle and fixed bounded
// transfer runner.
func (c *Coordinator) MeasureNativeQuality(ctx context.Context, generation AdaptiveGeneration, publish func(AdaptivePerformanceStatus)) (AdaptiveResult, error) {
	c.mu.Lock()
	if c.adaptiveRunner != nil {
		probe := c.adaptiveRunner.Probe
		c.mu.Unlock()
		// Retry a closed probe gate before refusing; nothing else would.
		probe.Recover(ctx)
		c.mu.Lock()
	}
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
	c.adaptive = AdaptivePerformanceStatus{NativeQuality: generation.NativeQuality, BroadSample: generation.BroadSample, State: "running", Generation: generation.Generation, StartedAt: generation.StartedAt, CurrentTarget: generation.CurrentTarget, ShortlistCount: len(generation.Candidates)}
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
