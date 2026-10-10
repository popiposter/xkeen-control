package c1

import (
	"context"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

func evidenceSnapshot(at time.Time, delay int64) xrayapi.Snapshot {
	return xrayapi.Snapshot{ObservatoryReachable: true, OutboundHealth: []xrayapi.OutboundHealth{{Tag: "proxy-alpha", Alive: true, DelayMS: delay, LastTry: at, LastSeen: at}}}
}

func TestNativeQualityEvidenceCountsUniqueObservationsAndResetsOnApply(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	clock := now
	c := NewCoordinator(DefaultPolicy(), nil)
	c.clock = func() time.Time { return clock }
	for i, delay := range []int64{100, 120, 110} {
		at := now.Add(time.Duration(i) * time.Minute)
		clock = at
		if i < 2 {
			if got := c.NativeQualityEvidence(evidenceSnapshot(at, delay)); len(got) != 0 {
				t.Fatalf("evidence before three samples = %+v", got)
			}
			// Re-reading the same Observatory timestamp must not count again.
			if got := c.NativeQualityEvidence(evidenceSnapshot(at, delay)); len(got) != 0 {
				t.Fatalf("duplicate read advanced evidence = %+v", got)
			}
			continue
		}
		got := c.NativeQualityEvidence(evidenceSnapshot(at, delay))
		if item, ok := got["proxy-alpha"]; !ok || item.Samples != 3 || item.RTTMS != 110 {
			t.Fatalf("evidence after three unique samples = %+v", got)
		}
	}
	// Samples older than the 15-minute window are pruned: one fresh sample
	// after a long gap is not enough evidence on its own.
	clock = now.Add(2*time.Minute + DefaultLatencyWindow + time.Minute)
	if got := c.NativeQualityEvidence(evidenceSnapshot(clock, 105)); len(got) != 0 {
		t.Fatalf("stale samples survived the window = %+v", got)
	}
	if got := c.NativeQualityEvidence(xrayapi.Snapshot{}); len(got) != 0 {
		t.Fatalf("unreachable Observatory produced evidence = %+v", got)
	}
	release, err := c.BeginApply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if got := c.NativeQualityEvidence(evidenceSnapshot(clock, 110)); len(got) != 0 {
		t.Fatalf("Apply did not reset transient evidence = %+v", got)
	}
}
