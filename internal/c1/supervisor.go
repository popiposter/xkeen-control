package c1

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type NodeReader func(context.Context) []NodeState
type ActiveProbe func(context.Context, string, int64) error

type SelectionStatus struct {
	Enabled               bool      `json:"enabled"`
	State                 string    `json:"state"`
	StableTarget          string    `json:"stableTarget"`
	ManualOverride        string    `json:"manualOverride"`
	NativeTarget          string    `json:"nativeTarget"`
	OverrideTarget        string    `json:"overrideTarget"`
	EffectiveTarget       string    `json:"effectiveTarget"`
	FallbackReason        string    `json:"fallbackReason"`
	LastSwitchReason      string    `json:"lastSwitchReason"`
	LastSwitchAt          time.Time `json:"lastSwitchAt"`
	LastRuntimeAction     string    `json:"lastRuntimeAction"`
	LastRuntimeActionAt   time.Time `json:"lastRuntimeActionAt"`
	StableSince           time.Time `json:"stableSince"`
	DwellRemainingSeconds int64     `json:"dwellRemainingSeconds"`
	RollingLatencyMS      int64     `json:"rollingLatencyMs"`
	LatencyEvidence       int       `json:"latencyEvidence"`
	PendingCandidate      string    `json:"pendingCandidate"`
	ConsecutiveBadWindows int       `json:"consecutiveBadWindows"`
	LivenessFailures      int       `json:"livenessFailures"`
	ProbeCleanupPending   bool      `json:"probeCleanupPending"`
}

type Supervisor struct {
	policy      Policy
	xray        xrayapi.Reader
	api         xrayapi.RoutingController
	nodes       NodeReader
	probe       *ProbeRouter
	selection   SelectionStore
	engine      *PolicyEngine
	activeProbe ActiveProbe
	clock       func() time.Time

	// policyMu serializes policy-engine state, liveness counters and runtime
	// selection decisions. Benchmark samples use the same ProbeRouter lease,
	// but must not race the independent supervisor loop after the benchmark
	// starts running.
	policyMu sync.Mutex
	writeMu  sync.Mutex
	mu       sync.Mutex
	record   SelectionRecord
	status   SelectionStatus
	failures int
	started  bool
}

func NewSupervisor(policy Policy, reader xrayapi.Reader, api xrayapi.RoutingController, nodes NodeReader, probe *ProbeRouter, store SelectionStore) *Supervisor {
	policy = policy.normalized()
	if store.Path == "" {
		store.Path = DefaultSelectionPath
	}
	record, _ := store.Load()
	return &Supervisor{
		policy: policy, xray: reader, api: api, nodes: nodes, probe: probe, selection: store,
		engine: NewPolicyEngine(policy), record: record, clock: func() time.Time { return time.Now().UTC() },
		status: SelectionStatus{Enabled: policy.Enabled, State: "starting", StableTarget: record.Target, ManualOverride: record.ManualOverride, StableSince: record.StableSince, LastSwitchReason: record.LastSwitchReason, LastSwitchAt: record.LastSwitchAt},
	}
}

func (s *Supervisor) Start(ctx context.Context) {
	if s == nil || !s.policy.Enabled {
		return
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go func() {
		ticker := time.NewTicker(s.policy.ProbeInterval)
		defer ticker.Stop()
		// Remove any C.1 rule left by a prior process before the first probe.
		// Failure keeps the probe gate closed until a later explicit reconcile.
		_ = s.probe.Reconcile(ctx)
		_ = s.Tick(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Tick(ctx)
			}
		}
	}()
}

func (s *Supervisor) Tick(ctx context.Context) error {
	if s == nil || !s.policy.Enabled {
		return nil
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	now := s.clock()
	if s.probe != nil && s.probe.Blocked() {
		s.mu.Lock()
		s.status.State = "cleanup-pending"
		s.status.ProbeCleanupPending = true
		s.mu.Unlock()
		return ErrProbeBlocked
	}
	if s.xray == nil || s.api == nil {
		return errors.New("selection runtime unavailable")
	}
	snapshot := s.xray.Snapshot(ctx)
	if !snapshot.APIReachable || !snapshot.RoutingReachable {
		s.mu.Lock()
		s.status.State = "fallback-leastping"
		s.status.FallbackReason = "xray-unavailable"
		s.mu.Unlock()
		return errors.New("Xray routing unavailable")
	}
	nodes := s.readNodes(ctx, snapshot)
	evidence := make(map[string]Evidence, len(nodes))
	for _, node := range nodes {
		evidence[node.Tag] = Evidence{Tag: node.Tag, Enabled: node.Enabled}
	}
	observations := make([]Observation, 0, len(snapshot.OutboundHealth))
	for _, item := range snapshot.OutboundHealth {
		if !validTag(item.Tag) {
			continue
		}
		itemEvidence := evidence[item.Tag]
		itemEvidence.Alive = item.Alive
		itemEvidence.DelayMS = item.DelayMS
		evidence[item.Tag] = itemEvidence
		observations = append(observations, Observation{Tag: item.Tag, Alive: item.Alive, DelayMS: item.DelayMS, LastTry: item.LastTry, LastSeen: item.LastSeen})
	}
	stable, stableSince := s.currentRecord()
	override := safeTag(snapshot.Balancer.Override)
	native := safeTag(snapshot.Balancer.NativeSelected)
	if manual := s.currentManualOverride(); manual != "" {
		return s.tickManualOverride(ctx, snapshot, evidence, native, manual, now)
	}
	current := override
	if current == "" {
		current = stable
	}
	if current == "" {
		current = native
	}
	if current == "" {
		s.updateStatus(snapshot, stable, native, override, "fallback-leastping", now)
		return nil
	}
	validatedThisTick := false
	if stable != "" && current == stable && override == "" && evidence[current].Enabled {
		if err := s.validateAndReapply(ctx, current); err == nil {
			override = current
			validatedThisTick = true
		} else if stable == current {
			// A stale persisted target is not blindly re-applied. Native
			// leastPing remains the emergency fallback until recovery chooses a
			// fresh validated candidate.
			current = native
		}
	}
	if current == "" {
		s.updateStatus(snapshot, stable, native, override, ReasonFallback, now)
		return nil
	}
	if !evidence[current].Enabled {
		return s.recover(ctx, snapshot, evidence, native, current, now)
	}
	if override == "" && stable == "" {
		if err := s.validateTarget(ctx, current); err != nil {
			// Native leastPing is already the emergency path. Try current
			// evidence for a first validated stable target without pinning a
			// target that failed its active probe.
			return s.recover(ctx, snapshot, evidence, native, current, now)
		}
		if err := s.changeTarget(ctx, current, ReasonStartup, now, false); err != nil {
			return err
		}
		override = current
		stable, stableSince = current, now
		validatedThisTick = true
	}
	if override != "" {
		if !validatedThisTick {
			if err := s.liveness(ctx, current); err != nil {
				s.failures++
			} else {
				s.failures = 0
			}
		}
		if s.failures >= s.policy.FailureThreshold {
			return s.recover(ctx, snapshot, evidence, native, current, now)
		}
		if stable == "" {
			if err := s.changeRecord(SelectionRecord{Target: current, StableSince: now, LastSwitchReason: ReasonStartup, LastSwitchAt: now}, false); err != nil {
				return err
			}
			stable, stableSince = current, now
		}
	}
	observationsChanged := s.engine.Observe(now, observations)
	decision := s.engine.LatencyDecision(now, current, evidence, observationsChanged, stableSince)
	if decision.Changed && decision.Target != current {
		if err := s.changeTarget(ctx, decision.Target, decision.Reason, now, false); err != nil {
			return err
		}
		stable, stableSince, override, current = decision.Target, now, decision.Target, decision.Target
	}
	s.updateStatus(snapshot, stable, native, override, "", now)
	return nil
}

func (s *Supervisor) tickManualOverride(ctx context.Context, snapshot xrayapi.Snapshot, evidence map[string]Evidence, native, manual string, now time.Time) error {
	item, known := evidence[manual]
	if !known || !item.Enabled {
		stable, _ := s.currentRecord()
		runtime := safeTag(snapshot.Balancer.Override)
		if stable != manual && runtime != manual {
			s.failures = s.policy.FailureThreshold
			s.updateStatus(snapshot, stable, native, runtime, ReasonManualFallback, now)
			return nil
		}
		s.failures = s.policy.FailureThreshold
	} else if err := s.liveness(ctx, manual); err != nil {
		s.failures++
	} else {
		s.failures = 0
	}
	if s.failures >= s.policy.FailureThreshold {
		if err := s.recover(ctx, snapshot, evidence, native, manual, now); err != nil {
			return err
		}
		fallback, _ := s.currentRecord()
		s.updateStatus(snapshot, fallback, native, fallback, ReasonManualFallback, now)
		return nil
	}
	if s.failures == 0 {
		stable, _ := s.currentRecord()
		if stable != manual || safeTag(snapshot.Balancer.Override) != manual {
			if err := s.changeTarget(ctx, manual, ReasonManualOverride, now, false); err != nil {
				return err
			}
		}
		s.updateStatus(snapshot, manual, native, manual, "", now)
		return nil
	}
	s.updateStatus(snapshot, manual, native, safeTag(snapshot.Balancer.Override), "", now)
	return nil
}

func (s *Supervisor) validateAndReapply(ctx context.Context, target string) error {
	if err := s.validateTarget(ctx, target); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.api.OverrideBalancerTarget(ctx, "bal-proxy", target); err != nil {
		return err
	}
	s.mu.Lock()
	s.status.LastRuntimeAction = ReasonReapply
	s.status.LastRuntimeActionAt = s.clock()
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) validateTarget(ctx context.Context, target string) error {
	if s.activeProbe == nil {
		return nil
	}
	if s.probe == nil {
		return errors.New("probe router unavailable")
	}
	return s.probe.WithTarget(ctx, "liveness", target, func(probeContext context.Context) error {
		return s.activeProbe(probeContext, target, s.policy.ProbePayloadBytes)
	})
}

func (s *Supervisor) liveness(ctx context.Context, target string) error {
	if s.activeProbe == nil {
		return nil
	}
	if s.probe == nil {
		return errors.New("probe router unavailable")
	}
	return s.probe.WithTarget(ctx, "liveness", target, func(probeContext context.Context) error {
		return s.activeProbe(probeContext, target, s.policy.ProbePayloadBytes)
	})
}

func (s *Supervisor) recover(ctx context.Context, snapshot xrayapi.Snapshot, evidence map[string]Evidence, native, failed string, now time.Time) error {
	previous := s.currentRecordRecord()
	previousStable := previous.Target
	recoveryReason := ReasonHealthFailover
	if previousStable == "" {
		recoveryReason = ReasonStartup
	}
	s.writeMu.Lock()
	if err := s.api.OverrideBalancerTarget(ctx, "bal-proxy", ""); err != nil {
		s.writeMu.Unlock()
		return err
	}
	s.writeMu.Unlock()
	if stable, _ := s.currentRecord(); stable != "" {
		if err := s.changeRecord(SelectionRecord{Target: "", ManualOverride: previous.ManualOverride, LastSwitchReason: ReasonFallback, LastSwitchAt: now, LastBenchmarkGeneration: previous.LastBenchmarkGeneration}, true); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.status.State = ReasonFallback
	s.status.FallbackReason = ReasonFallback
	s.status.LastRuntimeAction = ReasonFallback
	s.status.LastRuntimeActionAt = now
	s.mu.Unlock()
	type candidate struct {
		tag   string
		delay int64
	}
	candidates := make([]candidate, 0, len(evidence))
	for tag, item := range evidence {
		if tag == failed || !item.Enabled || !item.Alive || tag == "" {
			continue
		}
		candidates = append(candidates, candidate{tag: tag, delay: item.DelayMS})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].delay < candidates[j].delay || (candidates[i].delay == candidates[j].delay && candidates[i].tag < candidates[j].tag)
	})
	for _, item := range candidates {
		if err := s.validateTarget(ctx, item.tag); err != nil {
			continue
		}
		if err := s.changeTarget(ctx, item.tag, recoveryReason, now, false); err != nil {
			return err
		}
		s.failures = 0
		return nil
	}
	_ = snapshot
	return nil
}

func (s *Supervisor) changeTarget(ctx context.Context, target, reason string, now time.Time, reapply bool) error {
	if target == "" || !validTag(target) {
		return errors.New("selection target is invalid")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.api.OverrideBalancerTarget(ctx, "bal-proxy", target); err != nil {
		return err
	}
	if reapply {
		s.mu.Lock()
		s.status.LastRuntimeAction = ReasonReapply
		s.status.LastRuntimeActionAt = now
		s.mu.Unlock()
		return nil
	}
	previous := s.currentRecordRecord()
	record := SelectionRecord{Target: target, ManualOverride: previous.ManualOverride, StableSince: now, LastSwitchReason: reason, LastSwitchAt: now, LastBenchmarkGeneration: previous.LastBenchmarkGeneration}
	if _, err := s.selection.SaveIfChanged(previous, record); err != nil {
		// Do not leave a runtime target active when its durable counterpart
		// could not be committed. Native leastPing is the safe fallback.
		_ = s.api.OverrideBalancerTarget(ctx, "bal-proxy", "")
		return err
	}
	s.mu.Lock()
	s.record = record
	s.status.StableTarget = target
	s.status.StableSince = now
	s.status.LastSwitchReason = reason
	s.status.LastSwitchAt = now
	s.status.LastRuntimeAction = reason
	s.status.LastRuntimeActionAt = now
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) changeRecord(record SelectionRecord, allowEmpty bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !allowEmpty && record.Target == "" {
		return errors.New("selection target is empty")
	}
	previous := s.currentRecordRecord()
	if _, err := s.selection.SaveIfChanged(previous, record); err != nil {
		return err
	}
	s.mu.Lock()
	s.record = record
	s.mu.Unlock()
	return nil
}

// SetManualOverride persists the operator's explicit node choice. While it is
// set, latency and benchmark decisions are deliberately bypassed; Tick only
// leaves the choice after the node fails the bounded liveness policy.
func (s *Supervisor) SetManualOverride(ctx context.Context, target string) error {
	if s == nil || !s.policy.Enabled {
		return errors.New("selection unavailable")
	}
	if target != "" && !validTag(target) {
		return errors.New("manual override target is invalid")
	}
	if s.api == nil {
		return errors.New("selection runtime unavailable")
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if target != "" && s.nodes != nil {
		found := false
		for _, node := range s.nodes(ctx) {
			if node.Tag != target {
				continue
			}
			found = true
			if !node.Enabled {
				return errors.New("manual override target is disabled")
			}
			break
		}
		if !found {
			return errors.New("manual override target was not found")
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	previous := s.currentRecordRecord()
	now := s.clock()
	next := previous
	next.ManualOverride = target
	if target != "" {
		next.Target = target
		next.StableSince = now
		next.LastSwitchReason = ReasonManualOverride
		next.LastSwitchAt = now
	} else {
		if previous.ManualOverride != "" && previous.Target == previous.ManualOverride {
			next.Target = ""
			next.StableSince = time.Time{}
		}
		next.LastSwitchReason = "manual-cleared"
		next.LastSwitchAt = now
	}

	runtimeBefore := ""
	var snapshot xrayapi.Snapshot
	if s.xray != nil {
		snapshot = s.xray.Snapshot(ctx)
		runtimeBefore = safeTag(snapshot.Balancer.Override)
	}
	runtimeAfter := runtimeBefore
	if target != "" {
		runtimeAfter = target
	} else if runtimeBefore == previous.ManualOverride {
		runtimeAfter = ""
	}
	if runtimeAfter != runtimeBefore {
		if err := s.api.OverrideBalancerTarget(ctx, "bal-proxy", runtimeAfter); err != nil {
			return err
		}
	}
	if _, err := s.selection.SaveIfChanged(previous, next); err != nil {
		if runtimeAfter != runtimeBefore {
			_ = s.api.OverrideBalancerTarget(ctx, "bal-proxy", runtimeBefore)
		}
		return err
	}

	s.engine.ResetEvidence()
	s.mu.Lock()
	s.record = next
	s.failures = 0
	s.status.ManualOverride = target
	s.status.StableTarget = next.Target
	s.status.StableSince = next.StableSince
	s.status.NativeTarget = safeTag(snapshot.Balancer.NativeSelected)
	s.status.OverrideTarget = runtimeAfter
	s.status.EffectiveTarget = runtimeAfter
	if s.status.EffectiveTarget == "" {
		s.status.EffectiveTarget = s.status.NativeTarget
	}
	s.status.LastSwitchReason = next.LastSwitchReason
	s.status.LastSwitchAt = next.LastSwitchAt
	s.status.LastRuntimeAction = next.LastSwitchReason
	s.status.LastRuntimeActionAt = now
	s.status.FallbackReason = ""
	if target != "" {
		s.status.State = "manual"
	} else if runtimeAfter == "" {
		s.status.State = ReasonFallback
	} else {
		s.status.State = "stable"
	}
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) ApplyBenchmark(ctx context.Context, result BenchmarkResult) error {
	if s == nil {
		return nil
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if !result.SwitchAllowed || s.currentManualOverride() != "" {
		return nil
	}
	snapshot := s.xray.Snapshot(ctx)
	current := safeTag(snapshot.Balancer.Override)
	if current == "" {
		current = s.currentTarget()
	}
	if current == "" {
		return nil
	}
	evidence := make(map[string]Evidence)
	for _, node := range s.readNodes(ctx, snapshot) {
		if validTag(node.Tag) {
			evidence[node.Tag] = Evidence{Tag: node.Tag, Enabled: node.Enabled}
		}
	}
	for _, item := range snapshot.OutboundHealth {
		if validTag(item.Tag) {
			itemEvidence := evidence[item.Tag]
			itemEvidence.Tag = item.Tag
			itemEvidence.Alive = item.Alive
			itemEvidence.DelayMS = item.DelayMS
			evidence[item.Tag] = itemEvidence
		}
	}
	decision := s.engine.ThroughputDecision(current, evidence, result.Samples, s.currentStableSince(), s.clock())
	if !decision.Changed || decision.Target == current {
		return nil
	}
	return s.changeTarget(ctx, decision.Target, decision.Reason, s.clock(), false)
}

func (s *Supervisor) Snapshot() SelectionStatus {
	if s == nil {
		return SelectionStatus{State: "unavailable"}
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.status
	if !result.StableSince.IsZero() {
		remaining := s.policy.MinimumDwell - s.clock().Sub(result.StableSince)
		if remaining > 0 {
			result.DwellRemainingSeconds = int64(remaining.Seconds())
		}
	}
	result.LatencyEvidence = s.engine.SampleCount(result.StableTarget)
	result.PendingCandidate = s.engine.pending
	result.ConsecutiveBadWindows = s.engine.badWindows
	result.LivenessFailures = s.failures
	result.ProbeCleanupPending = s.probe != nil && s.probe.Blocked()
	return result
}

func (s *Supervisor) SetActiveProbe(probe ActiveProbe) { s.activeProbe = probe }

func (s *Supervisor) SetClock(clock func() time.Time) {
	if clock != nil {
		s.clock = clock
	}
}

func (s *Supervisor) currentRecord() (string, time.Time) {
	record := s.currentRecordRecord()
	return record.Target, record.StableSince
}

func (s *Supervisor) currentRecordRecord() SelectionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record
}

func (s *Supervisor) currentManualOverride() string { return s.currentRecordRecord().ManualOverride }

func (s *Supervisor) currentTarget() string {
	target, _ := s.currentRecord()
	return target
}

func (s *Supervisor) currentStableSince() time.Time {
	_, since := s.currentRecord()
	return since
}

func (s *Supervisor) readNodes(ctx context.Context, snapshot xrayapi.Snapshot) []NodeState {
	if s.nodes != nil {
		return s.nodes(ctx)
	}
	result := make([]NodeState, 0, len(snapshot.OutboundHealth))
	for _, item := range snapshot.OutboundHealth {
		if validTag(item.Tag) {
			result = append(result, NodeState{Tag: item.Tag, Enabled: true})
		}
	}
	return result
}

func (s *Supervisor) updateStatus(snapshot xrayapi.Snapshot, stable, native, override, fallback string, now time.Time) {
	effective := override
	if effective == "" {
		effective = native
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Enabled = s.policy.Enabled
	s.status.StableTarget = stable
	s.status.ManualOverride = s.record.ManualOverride
	s.status.NativeTarget = native
	s.status.OverrideTarget = override
	s.status.EffectiveTarget = effective
	s.status.StableSince = s.record.StableSince
	s.status.LastSwitchReason = s.record.LastSwitchReason
	s.status.LastSwitchAt = s.record.LastSwitchAt
	s.status.FallbackReason = fallback
	if s.record.ManualOverride != "" {
		if fallback != "" || override != s.record.ManualOverride {
			s.status.State = "manual-fallback"
		} else {
			s.status.State = "manual"
		}
	} else if fallback != "" || override == "" {
		s.status.State = ReasonFallback
	} else {
		s.status.State = "stable"
	}
	if snapshot.ObservatoryReachable {
		s.status.ProbeCleanupPending = s.probe != nil && s.probe.Blocked()
	}
	_ = now
}

func safeTag(tag string) string {
	if validTag(tag) {
		return tag
	}
	return ""
}
