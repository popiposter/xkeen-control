package c1

import (
	"testing"
	"time"
)

func TestEvidenceDeduplicatesObservatoryReads(t *testing.T) {
	engine := NewPolicyEngine(DefaultPolicy())
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
	}
}
