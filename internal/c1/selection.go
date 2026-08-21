package c1

import (
	"sort"
	"time"
)

const (
	ReasonStartup        = "startup"
	ReasonHealthFailover = "health-failover"
	ReasonLatencyQuality = "latency-quality"
	ReasonThroughput     = "throughput-benchmark"
	ReasonFallback       = "fallback-leastping"
	ReasonReapply        = "reapply-after-restart"
	ReasonManualOverride = "manual-override"
	ReasonManualFallback = "manual-unavailable"
)

type Observation struct {
	Tag      string
	Alive    bool
	DelayMS  int64
	LastTry  time.Time
	LastSeen time.Time
}

type Evidence struct {
	Tag     string
	Enabled bool
	Alive   bool
	DelayMS int64
}

type ThroughputSample struct {
	Valid          bool
	BytesPerSecond float64
}

type Decision struct {
	Target         string
	Reason         string
	Clear          bool
	Changed        bool
	Reapply        bool
	Pending        string
	BadWindows     int
	CurrentRTTMS   int64
	CandidateRTTMS int64
}

type PolicyEngine struct {
	policy      Policy
	samples     map[string][]sample
	lastTry     map[string]time.Time
	pending     string
	badWindows  int
	lastEvalKey string
}

type sample struct {
	at    time.Time
	delay int64
}

func NewPolicyEngine(policy Policy) *PolicyEngine {
	policy = policy.normalized()
	return &PolicyEngine{policy: policy, samples: make(map[string][]sample), lastTry: make(map[string]time.Time)}
}

// Observe stores only unique upstream observations and returns the tags whose
// observation timestamp advanced. The 5-minute Observatory value is commonly
// read several times during a supervisor tick interval; a repeated LastTry
// timestamp must never count as another quality window.
func (e *PolicyEngine) Observe(now time.Time, observations []Observation) map[string]bool {
	if e == nil {
		return nil
	}
	changed := make(map[string]bool)
	cutoff := now.Add(-e.policy.LatencyWindow)
	// A node can stop producing Observatory updates. Prune every retained
	// window on every evaluation so stale evidence cannot remain eligible just
	// because an unrelated node produced a fresh sample.
	for tag, values := range e.samples {
		first := 0
		for first < len(values) && values[first].at.Before(cutoff) {
			first++
		}
		if first > 0 {
			values = append([]sample(nil), values[first:]...)
		}
		if len(values) == 0 {
			delete(e.samples, tag)
		} else {
			e.samples[tag] = values
		}
	}
	for _, item := range observations {
		if !validTag(item.Tag) || item.LastTry.IsZero() || item.DelayMS < 0 {
			continue
		}
		if previous, ok := e.lastTry[item.Tag]; ok && !item.LastTry.After(previous) {
			continue
		}
		e.lastTry[item.Tag] = item.LastTry
		values := e.samples[item.Tag]
		values = append(values, sample{at: item.LastTry, delay: item.DelayMS})
		first := 0
		for first < len(values) && values[first].at.Before(cutoff) {
			first++
		}
		if first > 0 {
			values = append([]sample(nil), values[first:]...)
		}
		if len(values) > e.policy.LatencyObservations*4 {
			values = append([]sample(nil), values[len(values)-e.policy.LatencyObservations*4:]...)
		}
		e.samples[item.Tag] = values
		changed[item.Tag] = true
	}
	return changed
}

func (e *PolicyEngine) SampleCount(tag string) int {
	if e == nil {
		return 0
	}
	return len(e.samples[tag])
}

func (e *PolicyEngine) RollingMedian(tag string) (int64, bool) {
	if e == nil {
		return 0, false
	}
	values := e.samples[tag]
	if len(values) == 0 {
		return 0, false
	}
	delays := make([]int64, 0, len(values))
	for _, item := range values {
		delays = append(delays, item.delay)
	}
	sort.Slice(delays, func(i, j int) bool { return delays[i] < delays[j] })
	return delays[len(delays)/2], true
}

func (e *PolicyEngine) LatencyDecision(now time.Time, current string, evidence map[string]Evidence, observationsChanged map[string]bool, stableSince time.Time) Decision {
	if e == nil {
		return Decision{}
	}
	decision := Decision{Target: current, Pending: e.pending, BadWindows: e.badWindows}
	if current == "" {
		e.resetCandidate()
		return decision
	}
	currentEvidence, ok := evidence[current]
	if !ok || !currentEvidence.Enabled || !currentEvidence.Alive || e.SampleCount(current) < e.policy.LatencyObservations {
		e.resetCandidate()
		return decision
	}
	currentRTT, ok := e.RollingMedian(current)
	if !ok {
		e.resetCandidate()
		return decision
	}
	decision.CurrentRTTMS = currentRTT
	if !stableSince.IsZero() && now.Sub(stableSince) < e.policy.MinimumDwell {
		e.resetCandidate()
		return decision
	}
	best := ""
	var bestRTT int64
	for tag, item := range evidence {
		if tag == current || !item.Enabled || !item.Alive || e.SampleCount(tag) < e.policy.LatencyObservations {
			continue
		}
		median, exists := e.RollingMedian(tag)
		if !exists || median+e.policy.AbsoluteDegradationMS > currentRTT || float64(median)*e.policy.RelativeDegradation > float64(currentRTT) {
			continue
		}
		if best == "" || median < bestRTT || (median == bestRTT && tag < best) {
			best, bestRTT = tag, median
		}
	}
	if best == "" {
		e.resetCandidate()
		return decision
	}
	if e.pending != "" {
		pendingEvidence, pendingOK := evidence[e.pending]
		if !pendingOK || !pendingEvidence.Enabled || !pendingEvidence.Alive || e.SampleCount(e.pending) < e.policy.LatencyObservations || e.pending != best {
			e.resetCandidate()
			decision.Pending, decision.BadWindows = e.pending, e.badWindows
		}
	}
	// No fresh upstream timestamp means this evaluation must not advance the
	// streak, but stale/incomplete windows above still reset it immediately.
	if len(observationsChanged) == 0 {
		return decision
	}
	// A fresh Observatory update from an unrelated node must not advance the
	// persistence streak. Both the current and selected candidate windows are
	// bounded by the active RTT window above; only a new relevant window can
	// make this decision progress.
	if !observationsChanged[current] && !observationsChanged[best] {
		return decision
	}
	if e.pending != best {
		e.pending = best
		e.badWindows = 1
	} else {
		e.badWindows++
	}
	decision.Pending, decision.BadWindows, decision.CandidateRTTMS = e.pending, e.badWindows, bestRTT
	if e.badWindows >= e.policy.BadWindows {
		decision.Target = best
		decision.Reason = ReasonLatencyQuality
		decision.Changed = true
		e.resetCandidate()
	}
	return decision
}

func (e *PolicyEngine) ThroughputDecision(current string, evidence map[string]Evidence, throughput map[string]ThroughputSample, stableSince, now time.Time) Decision {
	if e == nil {
		return Decision{}
	}
	decision := Decision{Target: current, Pending: e.pending, BadWindows: e.badWindows}
	if current == "" {
		return decision
	}
	currentEvidence, ok := evidence[current]
	currentSample, currentValid := throughput[current]
	if !ok || !currentEvidence.Enabled || !currentEvidence.Alive || !currentValid || !currentSample.Valid || currentSample.BytesPerSecond <= 0 || (!stableSince.IsZero() && now.Sub(stableSince) < e.policy.MinimumDwell) {
		return decision
	}
	currentRTT, currentRTTOk := e.RollingMedian(current)
	if !currentRTTOk || e.SampleCount(current) < e.policy.LatencyObservations {
		return decision
	}
	var best string
	var bestRate float64
	for tag, item := range evidence {
		candidate, valid := throughput[tag]
		if tag == current || !item.Enabled || !item.Alive || !valid || !candidate.Valid || candidate.BytesPerSecond <= currentSample.BytesPerSecond*(1+e.policy.ThroughputImprovement) || e.SampleCount(tag) < e.policy.LatencyObservations {
			continue
		}
		candidateRTT, ok := e.RollingMedian(tag)
		if !ok || candidateRTT > currentRTT+e.policy.LatencyGuardrailMS || float64(candidateRTT) > float64(currentRTT)*e.policy.LatencyGuardrailRatio {
			continue
		}
		if best == "" || candidate.BytesPerSecond > bestRate || (candidate.BytesPerSecond == bestRate && tag < best) {
			best, bestRate = tag, candidate.BytesPerSecond
		}
	}
	if best != "" {
		decision.Target = best
		decision.Reason = ReasonThroughput
		decision.Changed = true
		decision.CandidateRTTMS, _ = e.RollingMedian(best)
	}
	return decision
}

func (e *PolicyEngine) ResetEvidence() {
	if e == nil {
		return
	}
	e.samples = make(map[string][]sample)
	e.lastTry = make(map[string]time.Time)
	e.resetCandidate()
}

func (e *PolicyEngine) resetCandidate() {
	e.pending = ""
	e.badWindows = 0
}

func validTag(tag string) bool {
	return len(tag) > len("proxy-") && tag[:len("proxy-")] == "proxy-" && len(tag) <= 128
}
