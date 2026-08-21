package c1

import (
	"testing"
	"time"
)

func TestSelectionStaleGapResetsPersistenceWithoutFreshObservation(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	evidence := testEvidence("proxy-current", "proxy-candidate", 300, 10)

	for i := 0; i < 4; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Minute)
		changed := engine.Observe(at, []Observation{
			{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at},
			{Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at},
		})
		if decision := engine.LatencyDecision(at, "proxy-current", evidence, changed, base.Add(-time.Hour)); decision.Changed {
			t.Fatalf("switched before the configured persistence threshold: %+v", decision)
		}
	}
	if engine.pending != "proxy-candidate" || engine.badWindows != 2 {
		t.Fatalf("pre-gap persistence state = pending %q badWindows %d", engine.pending, engine.badWindows)
	}

	later := base.Add(31 * time.Minute)
	changed := engine.Observe(later, nil)
	decision := engine.LatencyDecision(later, "proxy-current", evidence, changed, base.Add(-time.Hour))
	if decision.Changed || engine.pending != "" || engine.badWindows != 0 {
		t.Fatalf("stale gap retained quality persistence: decision=%+v pending=%q badWindows=%d", decision, engine.pending, engine.badWindows)
	}
	if engine.SampleCount("proxy-current") != 0 || engine.SampleCount("proxy-candidate") != 0 {
		t.Fatalf("stale RTT samples survived the gap: current=%d candidate=%d", engine.SampleCount("proxy-current"), engine.SampleCount("proxy-candidate"))
	}

	for i := 0; i < 3; i++ {
		at := later.Add(time.Duration(i) * 5 * time.Minute)
		changed = engine.Observe(at, []Observation{
			{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at},
			{Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at},
		})
		decision = engine.LatencyDecision(at, "proxy-current", evidence, changed, base.Add(-time.Hour))
		if decision.Changed {
			t.Fatalf("fresh evidence inherited the pre-gap streak at window %d: %+v", i, decision)
		}
	}
	if engine.pending != "proxy-candidate" || engine.badWindows != 1 {
		t.Fatalf("fresh persistence did not restart from one window: pending=%q badWindows=%d", engine.pending, engine.badWindows)
	}
}

func TestSelectionStalePendingCandidateResetsWithFreshUnrelatedWindow(t *testing.T) {
	policy := testSelectionPolicy()
	engine := NewPolicyEngine(policy)
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Minute)
		engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at}, {Tag: "proxy-candidate", Alive: true, DelayMS: 10, LastTry: at}})
	}
	engine.pending = "proxy-candidate"
	engine.badWindows = 2
	for i := 0; i < 3; i++ {
		at := base.Add(16*time.Minute + time.Duration(i)*5*time.Minute)
		engine.Observe(at, []Observation{{Tag: "proxy-current", Alive: true, DelayMS: 300, LastTry: at}, {Tag: "proxy-alternate", Alive: true, DelayMS: 10, LastTry: at}})
	}

	later := base.Add(31 * time.Minute)
	decision := engine.LatencyDecision(later, "proxy-current", map[string]Evidence{
		"proxy-current":   {Tag: "proxy-current", Enabled: true, Alive: true},
		"proxy-candidate": {Tag: "proxy-candidate", Enabled: true, Alive: true},
		"proxy-alternate": {Tag: "proxy-alternate", Enabled: true, Alive: true},
	}, engine.Observe(later, nil), base.Add(-time.Hour))
	if decision.Changed || engine.pending != "" || engine.badWindows != 0 {
		t.Fatalf("stale pending candidate survived without a relevant update: decision=%+v pending=%q badWindows=%d", decision, engine.pending, engine.badWindows)
	}
}
