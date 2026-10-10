package c1

import (
	"time"
)

type Observation struct {
	Tag      string
	Alive    bool
	DelayMS  int64
	LastTry  time.Time
	LastSeen time.Time
}

// PolicyEngine keeps a bounded RAM-only window of unique Observatory
// observations per outbound for native quality RTT evidence.
type PolicyEngine struct {
	policy  Policy
	samples map[string][]sample
	lastTry map[string]time.Time
}

type sample struct {
	at    time.Time
	delay int64
	alive bool
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
		values = append(values, sample{at: item.LastTry, delay: item.DelayMS, alive: item.Alive})
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

func (e *PolicyEngine) ResetEvidence() {
	if e == nil {
		return
	}
	e.samples = make(map[string][]sample)
	e.lastTry = make(map[string]time.Time)
}

func validTag(tag string) bool {
	return len(tag) > len("proxy-") && tag[:len("proxy-")] == "proxy-" && len(tag) <= 128
}
