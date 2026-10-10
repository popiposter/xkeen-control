package c1

import (
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

// NativeQualityEvidence retains unique upstream Observatory observations in
// RAM and returns per-tag RTT evidence for native quality review. It makes no
// routing call and starts no poller.
func (c *Coordinator) NativeQualityEvidence(snapshot xrayapi.Snapshot) map[string]AdaptiveCandidateInput {
	result := map[string]AdaptiveCandidateInput{}
	if c == nil || c.evidence == nil || !snapshot.ObservatoryReachable {
		return result
	}
	now := c.now()
	c.evidenceMu.Lock()
	defer c.evidenceMu.Unlock()
	c.evidence.Observe(now, adaptiveObservations(snapshot))
	cutoff := now.Add(-c.policy.LatencyWindow)
	for tag, samples := range c.evidence.samples {
		median, count, latest := adaptiveRTTEvidence(samples, cutoff, now)
		if count < 3 {
			continue
		}
		result[tag] = AdaptiveCandidateInput{Tag: tag, RTTMS: median, Samples: count, LatestAt: latest, HealthPenalty: adaptiveWindowPenalty(samples, cutoff, now, median)}
	}
	return result
}

// resetEvidence drops transient observations when a lifecycle mutation starts;
// the mutation invalidates them anyway.
func (c *Coordinator) resetEvidence() {
	if c == nil || c.evidence == nil {
		return
	}
	c.evidenceMu.Lock()
	c.evidence.ResetEvidence()
	c.evidenceMu.Unlock()
}

func adaptiveObservations(snapshot xrayapi.Snapshot) []Observation {
	result := make([]Observation, 0, len(snapshot.OutboundHealth))
	for _, item := range snapshot.OutboundHealth {
		if !validTag(item.Tag) {
			continue
		}
		result = append(result, Observation{Tag: item.Tag, Alive: item.Alive, DelayMS: item.DelayMS, LastTry: item.LastTry, LastSeen: item.LastSeen})
	}
	return result
}

func safeTag(tag string) string {
	if validTag(tag) {
		return tag
	}
	return ""
}
