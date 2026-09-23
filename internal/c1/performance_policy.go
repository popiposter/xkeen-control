package c1

import (
	"context"
	"errors"
	"time"
)

const PerformancePolicySchemaVersion = 1

const (
	MinProbeIntervalSeconds     = 60
	MaxProbeIntervalSeconds     = 300
	MinFailureThreshold         = 2
	MaxFailureThreshold         = 5
	MinAdaptiveCadenceMinutes   = 180
	MaxAdaptiveCadenceMinutes   = 1440
	MinAdaptiveChallengerLimit  = 1
	MaxAdaptiveChallengerLimit  = 5
	MinMinimumDwellMinutes      = 30
	MaxMinimumDwellMinutes      = 1440
	MinQualityHysteresisPercent = 10
	MaxQualityHysteresisPercent = 50
)

var ErrInvalidPerformancePolicy = errors.New("performance policy is invalid")

// PerformancePolicy is the complete operator-tunable C.1 policy. Transport
// identity, stage sizes, byte/time ceilings, RTT guards and score weights are
// deliberately absent and remain source-owned.
type PerformancePolicy struct {
	SchemaVersion            int `json:"schemaVersion"`
	ProbeIntervalSeconds     int `json:"probeIntervalSeconds"`
	FailureThreshold         int `json:"failureThreshold"`
	AdaptiveCadenceMinutes   int `json:"adaptiveCadenceMinutes"`
	AdaptiveChallengerLimit  int `json:"adaptiveChallengerLimit"`
	MinimumDwellMinutes      int `json:"minimumDwellMinutes"`
	QualityHysteresisPercent int `json:"qualityHysteresisPercent"`
}

func DefaultPerformancePolicy() PerformancePolicy {
	return PerformancePolicy{
		SchemaVersion:            PerformancePolicySchemaVersion,
		ProbeIntervalSeconds:     int(DefaultProbeInterval / time.Second),
		FailureThreshold:         DefaultFailureThreshold,
		AdaptiveCadenceMinutes:   int(AdaptiveCadence / time.Minute),
		AdaptiveChallengerLimit:  AdaptiveShortlistLimit,
		MinimumDwellMinutes:      int(DefaultMinimumDwell / time.Minute),
		QualityHysteresisPercent: int((AdaptiveQualityHysteresis - 1) * 100),
	}
}

func ValidatePerformancePolicy(policy PerformancePolicy) error {
	if policy.SchemaVersion != PerformancePolicySchemaVersion ||
		policy.ProbeIntervalSeconds < MinProbeIntervalSeconds || policy.ProbeIntervalSeconds > MaxProbeIntervalSeconds || policy.ProbeIntervalSeconds%30 != 0 ||
		policy.FailureThreshold < MinFailureThreshold || policy.FailureThreshold > MaxFailureThreshold ||
		policy.AdaptiveCadenceMinutes < MinAdaptiveCadenceMinutes || policy.AdaptiveCadenceMinutes > MaxAdaptiveCadenceMinutes || policy.AdaptiveCadenceMinutes%30 != 0 ||
		policy.AdaptiveChallengerLimit < MinAdaptiveChallengerLimit || policy.AdaptiveChallengerLimit > MaxAdaptiveChallengerLimit ||
		policy.MinimumDwellMinutes < MinMinimumDwellMinutes || policy.MinimumDwellMinutes > MaxMinimumDwellMinutes ||
		policy.QualityHysteresisPercent < MinQualityHysteresisPercent || policy.QualityHysteresisPercent > MaxQualityHysteresisPercent {
		return ErrInvalidPerformancePolicy
	}
	return nil
}

func (policy PerformancePolicy) probeInterval() time.Duration {
	return time.Duration(policy.ProbeIntervalSeconds) * time.Second
}

func (policy PerformancePolicy) adaptiveCadence() time.Duration {
	return time.Duration(policy.AdaptiveCadenceMinutes) * time.Minute
}

func (policy PerformancePolicy) minimumDwell() time.Duration {
	return time.Duration(policy.MinimumDwellMinutes) * time.Minute
}

func (policy PerformancePolicy) qualityHysteresis() float64 {
	return 1 + float64(policy.QualityHysteresisPercent)/100
}

// InitializePerformancePolicy installs the startup snapshot before the
// Coordinator starts. It performs no persistence or runtime work.
func (c *Coordinator) InitializePerformancePolicy(policy PerformancePolicy, initializedAt time.Time) error {
	if c == nil || ValidatePerformancePolicy(policy) != nil {
		return ErrInvalidPerformancePolicy
	}
	c.mu.Lock()
	if c.started || c.benchmarkCancel != nil || c.applyActive || c.applyWaiters > 0 {
		c.mu.Unlock()
		return ErrLifecycleBusy
	}
	c.performancePolicy = policy
	c.applyPerformancePolicyLocked(policy, initializedAt)
	c.mu.Unlock()
	if c.supervisor != nil {
		c.supervisor.applyPerformancePolicy(policy)
	}
	return nil
}

// CommitPerformancePolicy owns the runtime admission boundary for the policy
// manager. Persist runs only while no manual/adaptive/legacy performance owner
// or lifecycle mutation can start. An in-flight liveness tick may finish with
// its old snapshot; the new values affect subsequent cycles.
func (c *Coordinator) CommitPerformancePolicy(ctx context.Context, policy PerformancePolicy, appliedAt time.Time, changed bool, persist func() error) error {
	if c == nil || ValidatePerformancePolicy(policy) != nil || persist == nil {
		return ErrInvalidPerformancePolicy
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil {
		c.mu.Unlock()
		return ErrBenchmarkBusy
	}
	select {
	case token := <-c.lifecycle:
		c.applyActive = true
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.applyActive = false
			c.mu.Unlock()
			c.lifecycle <- token
		}()
	default:
		c.mu.Unlock()
		return ErrBenchmarkBusy
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := persist(); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if appliedAt.IsZero() {
		appliedAt = c.now()
	}
	c.mu.Lock()
	c.performancePolicy = policy
	c.applyPerformancePolicyLocked(policy, appliedAt)
	c.mu.Unlock()
	if c.supervisor != nil {
		c.supervisor.applyPerformancePolicy(policy)
	}
	notifyPolicyChange(c.supervisorPolicyChanged)
	notifyPolicyChange(c.adaptivePolicyChanged)
	return nil
}

func (c *Coordinator) applyPerformancePolicyLocked(policy PerformancePolicy, appliedAt time.Time) {
	if appliedAt.IsZero() {
		appliedAt = time.Now().UTC()
	}
	c.policy.ProbeInterval = policy.probeInterval()
	c.policy.FailureThreshold = policy.FailureThreshold
	c.policy.MinimumDwell = policy.minimumDwell()
	c.adaptive.NextRunAt = appliedAt.UTC().Add(policy.adaptiveCadence())
}

func (c *Coordinator) currentPerformancePolicy() PerformancePolicy {
	if c == nil {
		return DefaultPerformancePolicy()
	}
	c.mu.Lock()
	policy := c.performancePolicy
	c.mu.Unlock()
	if ValidatePerformancePolicy(policy) != nil {
		return DefaultPerformancePolicy()
	}
	return policy
}

func notifyPolicyChange(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (s *Supervisor) applyPerformancePolicy(policy PerformancePolicy) {
	if s == nil || ValidatePerformancePolicy(policy) != nil {
		return
	}
	s.policyMu.Lock()
	s.performancePolicy = policy
	s.policy.ProbeInterval = policy.probeInterval()
	s.policy.FailureThreshold = policy.FailureThreshold
	s.policy.MinimumDwell = policy.minimumDwell()
	s.engine.policy.FailureThreshold = policy.FailureThreshold
	s.engine.policy.MinimumDwell = policy.minimumDwell()
	s.engine.resetCandidate()
	s.policyMu.Unlock()
}
