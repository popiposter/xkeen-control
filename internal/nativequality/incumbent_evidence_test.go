package nativequality

import (
	"fmt"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

func TestIncumbentEvidenceAdmission(t *testing.T) {
	now := time.Now().UTC()
	for _, count := range []int{2, 6} {
		t.Run(fmt.Sprintf("pool-%d", count), func(t *testing.T) {
			var active []string
			var eligible []c1.AdaptiveCandidateInput
			snapshot := xrayapi.Snapshot{}
			criteria := observationCriteria{maxRTT: 750, freshness: 2 * time.Minute, observed: map[string]bool{}}
			for i := 0; i < count; i++ {
				tag := fmt.Sprintf("proxy-%02d", i)
				active = append(active, tag)
				eligible = append(eligible, c1.AdaptiveCandidateInput{Tag: tag, RTTMS: 100})
				snapshot.OutboundHealth = append(snapshot.OutboundHealth, xrayapi.OutboundHealth{Tag: tag, Alive: true, DelayMS: 100, LastTry: now.Add(-time.Second)})
				criteria.observed[tag] = true
			}
			check := func(want bool) {
				t.Helper()
				if got := incumbentsComparable(active, eligible, snapshot, criteria, now); got != want {
					t.Fatalf("comparable=%t, want %t", got, want)
				}
			}
			check(true)
			last := count - 1
			original := snapshot.OutboundHealth[last]
			snapshot.OutboundHealth = snapshot.OutboundHealth[:last]
			check(false)
			snapshot.OutboundHealth = append(snapshot.OutboundHealth, original)
			eligible = eligible[:last]
			check(false)
			eligible = append(eligible, c1.AdaptiveCandidateInput{Tag: active[last], RTTMS: 100})
			snapshot.OutboundHealth[last].LastTry = now.Add(-3 * time.Minute)
			check(false)
			snapshot.OutboundHealth[last] = original
			snapshot.OutboundHealth[last].DelayMS = 800
			check(false)
			snapshot.OutboundHealth[last] = original
			snapshot.OutboundHealth[last].Alive = false
			eligible = eligible[:last]
			check(true) // an explicitly fresh Unhealthy member may be replaced later
			snapshot.OutboundHealth[last].LastTry = now.Add(-3 * time.Minute)
			check(false)
			snapshot.OutboundHealth[last] = original
			eligible = append(eligible, c1.AdaptiveCandidateInput{Tag: active[last], RTTMS: 100})
			criteria.observed[active[last]] = false
			check(false)
			criteria.observed[active[last]] = true
			snapshot.OutboundHealth = append(snapshot.OutboundHealth, original)
			check(false)
		})
	}
}

func TestBroadPoolKeepsFirstInitializationAdmission(t *testing.T) {
	active := []string{"a", "b", "c", "d", "e", "f", "g"}
	if !incumbentsComparable(active, nil, xrayapi.Snapshot{}, observationCriteria{}, time.Now()) {
		t.Fatal("broad first initialization was blocked")
	}
}
