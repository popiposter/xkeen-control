package c1

import (
	"testing"
	"time"
)

func testSelectionPolicy() Policy {
	policy := DefaultPolicy()
	policy.MinimumDwell = time.Minute
	policy.LatencyObservations = 3
	policy.BadWindows = 3
	return policy
}

func testEvidence(current, candidate string, currentDelay, candidateDelay int64) map[string]Evidence {
	return map[string]Evidence{
		current:   {Tag: current, Enabled: true, Alive: true, DelayMS: currentDelay},
		candidate: {Tag: candidate, Enabled: true, Alive: true, DelayMS: candidateDelay},
	}
}

func TestSelectionDeduplicatesObservatoryReadsAndIgnoresRTTNoise(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		at := now.Add(time.Duration(i) * 5 * time.Minute)
		changed := engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 100 + int64(i%2)*7, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 90 + int64(i%2)*7, LastTry: at}})
		if len(changed) == 0 {
			t.Fatal("unique observation was ignored")
		}
		if duplicate := engine.Observe(at.Add(time.Minute), []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 40, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 20, LastTry: at}}); len(duplicate) != 0 {
			t.Fatal("duplicate Observatory timestamp advanced evidence")
		}
		decision := engine.LatencyDecision(at, "proxy-current", testEvidence("proxy-current", "proxy-candidate", 100, 90), changed, now.Add(-time.Hour))
		if decision.Changed {
			t.Fatal("small RTT noise switched stable target")
		}
	}
}

func TestSelectionSwitchesAfterPersistentMaterialLatencyDegradation(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	evidence := testEvidence("proxy-current", "proxy-candidate", 200, 100)
	for i := 0; i < 5; i++ {
		at := now.Add(time.Duration(i) * 5 * time.Minute)
		changed := engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 200, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 100, LastTry: at}})
		decision := engine.LatencyDecision(at, "proxy-current", evidence, changed, now.Add(-time.Hour))
		if i < 4 && decision.Changed {
			t.Fatalf("switched before persistence window %d: %+v", i, decision)
		}
		if i == 4 && (decision.Target != "proxy-candidate" || decision.Reason != ReasonLatencyQuality) {
			t.Fatalf("persistent degradation decision = %+v", decision)
		}
	}
}

func TestSelectionStaleCurrentWindowFailsClosed(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Minute)
		engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at}})
	}
	for i := 0; i < 3; i++ {
		at := base.Add(31*time.Minute + time.Duration(i)*5*time.Minute)
		changed := engine.Observe(at, []Observation{{Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at}})
		decision := engine.LatencyDecision(at, "proxy-current", testEvidence("proxy-current", "proxy-candidate", 300, 10), changed, base.Add(-time.Hour))
		if decision.Changed {
			t.Fatalf("stale current RTT allowed a switch at %s: %+v", at, decision)
		}
	}
	if engine.SampleCount("proxy-current") != 0 || engine.SampleCount("proxy-candidate") != 3 {
		t.Fatalf("stale current samples were retained while candidate refreshed: current=%d candidate=%d", engine.SampleCount("proxy-current"), engine.SampleCount("proxy-candidate"))
	}
}

func TestSelectionStaleCandidateWindowFailsClosed(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Minute)
		engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at}})
	}
	for i := 0; i < 3; i++ {
		at := base.Add(31*time.Minute + time.Duration(i)*5*time.Minute)
		changed := engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at}})
		decision := engine.LatencyDecision(at, "proxy-current", testEvidence("proxy-current", "proxy-candidate", 300, 10), changed, base.Add(-time.Hour))
		if decision.Changed {
			t.Fatalf("stale candidate RTT allowed a switch at %s: %+v", at, decision)
		}
	}
	if engine.SampleCount("proxy-current") != 3 {
		t.Fatalf("fresh current samples were unexpectedly pruned: %d", engine.SampleCount("proxy-current"))
	}
}

func TestSelectionUnrelatedObservationDoesNotAdvanceStaleQualityStreak(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Minute)
		engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at}})
	}
	later := base.Add(31 * time.Minute)
	changed := engine.Observe(later, []Observation{{Tag: "proxy-unrelated", Alive: true, DelayMS: 1, LastTry: later}})
	decision := engine.LatencyDecision(later, "proxy-current", testEvidence("proxy-current", "proxy-candidate", 300, 10), changed, base.Add(-time.Hour))
	if decision.Changed || decision.BadWindows != 0 || engine.SampleCount("proxy-current") != 0 || engine.SampleCount("proxy-candidate") != 0 {
		t.Fatalf("unrelated fresh RTT advanced stale quality state: decision=%+v current=%d candidate=%d", decision, engine.SampleCount("proxy-current"), engine.SampleCount("proxy-candidate"))
	}
}

func TestSelectionCandidateChangeResetsQualityStreak(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i) * 5 * time.Minute)
		changed := engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 240, LastTry: at}, {Tag: "proxy-a", Alive: true, DelayMS: 100, LastTry: at}, {Tag: "proxy-b", Alive: true, DelayMS: 110, LastTry: at}})
		evidence := map[string]Evidence{"proxy-current": {Tag: "proxy-current", Enabled: true, Alive: true}, "proxy-a": {Tag: "proxy-a", Enabled: true, Alive: true}, "proxy-b": {Tag: "proxy-b", Enabled: true, Alive: true}}
		if i == 1 {
			// Make the second candidate the only best one for this window.
			engine.samples["proxy-a"][0].delay = 300
			engine.samples["proxy-a"][i].delay = 300
			engine.samples["proxy-b"][i].delay = 80
		}
		decision := engine.LatencyDecision(at, "proxy-current", evidence, changed, now.Add(-time.Hour))
		if i == 2 && (decision.Pending != "proxy-b" || decision.BadWindows != 1) {
			t.Fatalf("candidate streak was not reset: %+v", decision)
		}
	}
}

func TestThroughputHysteresisNeedsCurrentSampleAndLatencyGuardrail(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i) * 5 * time.Minute)
		engine.Observe(at, []Observation{{Tag: "proxy-current", DelayMS: 100, LastTry: at}, {Tag: "proxy-fast", DelayMS: 170, LastTry: at}})
	}
	evidence := map[string]Evidence{"proxy-current": {Enabled: true, Alive: true}, "proxy-fast": {Enabled: true, Alive: true}}
	throughput := map[string]ThroughputSample{"proxy-current": {Valid: true, BytesPerSecond: 10}, "proxy-fast": {Valid: true, BytesPerSecond: 20}}
	if decision := engine.ThroughputDecision("proxy-current", evidence, throughput, now.Add(-time.Hour), now.Add(time.Hour)); decision.Changed {
		t.Fatalf("latency guardrail accepted speed winner: %+v", decision)
	}
	throughput["proxy-fast"] = ThroughputSample{Valid: true, BytesPerSecond: 14}
	if decision := engine.ThroughputDecision("proxy-current", evidence, throughput, now.Add(-time.Hour), now.Add(time.Hour)); decision.Changed {
		t.Fatalf("marginal speed winner accepted: %+v", decision)
	}
}
