package c1

import (
	"context"
	"errors"
	"time"
)

const (
	RTTMode = "rtt"
	// RTTProbeTimeout bounds one targeted latency probe, including the probe
	// rule installation and the zero-byte HTTPS request through the outbound.
	RTTProbeTimeout = 10 * time.Second
)

// RTTSample is one targeted probe result. Valid is false when the request did
// not complete through the requested outbound within RTTProbeTimeout.
type RTTSample struct {
	Tag       string
	RTTMS     int64
	Valid     bool
	SampledAt time.Time
}

// LatencyTransport is the zero-byte request used for a targeted RTT probe.
type LatencyTransport interface {
	Latency(context.Context) (time.Duration, error)
}

// SetRTTTransport replaces the fixed latency transport; it is a test seam.
func (c *Coordinator) SetRTTTransport(transport LatencyTransport) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.rttTransport = transport
	c.mu.Unlock()
}

// MeasureRTT probes each tag once, sequentially, through the loopback probe
// inbound using the same fixed endpoint and timeout for every tag, so the
// results are comparable with each other. It holds the shared lifecycle token,
// so a node or config Apply cancels it. A failed probe yields an invalid
// sample; a probe-rule cleanup failure stops the run and returns its error.
func (c *Coordinator) MeasureRTT(ctx context.Context, tags []string) ([]RTTSample, error) {
	if c == nil {
		return nil, ErrManualUnavailable
	}
	if len(tags) == 0 || len(tags) > MaxRegistryNodes {
		return nil, ErrManualInvalidTarget
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !validTag(tag) || seen[tag] {
			return nil, ErrManualInvalidTarget
		}
		seen[tag] = true
	}
	c.mu.Lock()
	if c.adaptiveRunner != nil {
		probe := c.adaptiveRunner.Probe
		c.mu.Unlock()
		// Retry a closed probe gate before refusing; nothing else would.
		probe.Recover(ctx)
		c.mu.Lock()
	}
	if !c.policy.Enabled || c.adaptiveRunner == nil || c.adaptiveRunner.Probe == nil {
		c.mu.Unlock()
		return nil, ErrManualUnavailable
	}
	probe := c.adaptiveRunner.Probe
	transport := c.rttTransport
	if probe.Blocked() {
		c.mu.Unlock()
		return nil, ErrManualCleanupPending
	}
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil {
		c.mu.Unlock()
		return nil, ErrManualBusy
	}
	var token struct{}
	select {
	case token = <-c.lifecycle:
	default:
		c.mu.Unlock()
		return nil, ErrManualBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.benchmarkCancel, c.benchmarkDone = cancel, done
	c.performanceMode = RTTMode
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
	if transport == nil {
		transport = newFixedMeasurementTransport()
	}
	results := make([]RTTSample, 0, len(tags))
	for _, tag := range tags {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		sample := RTTSample{Tag: tag}
		probeContext, stop := context.WithTimeout(ctx, RTTProbeTimeout)
		var elapsed time.Duration
		err := probe.WithTarget(probeContext, RTTMode, tag, func(requestContext context.Context) error {
			started := time.Now()
			measured, requestErr := transport.Latency(requestContext)
			elapsed = measured
			if elapsed <= 0 {
				elapsed = time.Since(started)
			}
			return requestErr
		})
		stop()
		sample.SampledAt = time.Now().UTC()
		if errors.Is(err, ErrProbeCleanup) || errors.Is(err, ErrProbeBlocked) {
			return results, err
		}
		if err == nil && elapsed > 0 {
			sample.Valid = true
			sample.RTTMS = max(int64(1), elapsed.Milliseconds())
		}
		results = append(results, sample)
	}
	return results, nil
}
