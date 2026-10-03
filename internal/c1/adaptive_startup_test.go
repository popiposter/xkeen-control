package c1

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveInitialRunWaitsForIndependentFreshEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		count  int
		stale  bool
		manual bool
		want   bool
	}{
		{"duplicate reads", 1, false, false, false}, {"two independent", 2, false, false, false},
		{"three stale", 3, true, false, false}, {"manual", 3, false, true, false},
		{"failed RTT", 3, false, false, false}, {"zero RTT", 3, false, false, false}, {"three fresh", 3, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			tags := []string{"proxy-current", "proxy-challenger"}
			s, _, _, _ := adaptiveEvidenceFixture(t, tags[0], now.Add(-time.Hour), supervisorPolicy(), tags, []int64{100, 100}, now)
			s.SetActiveProbe(func(context.Context, string, int64) error { return nil })
			if tc.count == 1 {
				s.policy.LatencyObservations = 1
			}
			for i := 0; i < tc.count; i++ {
				at := now.Add(-time.Duration(tc.count-i) * time.Minute)
				if tc.stale {
					at = at.Add(-time.Hour)
				}
				delay := int64(100)
				if tc.name == "zero RTT" && i == 0 {
					delay = 0
				}
				alive := tc.name != "failed RTT" || i != 0
				s.engine.Observe(now, []Observation{{Tag: tags[0], Alive: alive, DelayMS: delay, LastTry: at}, {Tag: tags[1], Alive: alive, DelayMS: delay, LastTry: at}})
			}
			if tc.manual {
				s.record.ManualOverride = tags[0]
			}
			transport := &adaptiveTransportStub{duration: time.Second, failedDownload: -1, failedUpload: -1, started: make(chan struct{})}
			c := NewCoordinator(supervisorPolicy(), s, nil, nil)
			c.SetAdaptiveRunner(&AdaptiveRunner{Probe: s.probe, Transport: transport})
			c.Start(context.Background())
			defer c.Stop()
			// Wakeups read identical upstream LastTry and must not manufacture samples.
			for i := 0; i < 4; i++ {
				c.requestSupervisorReconcile()
			}
			select {
			case <-transport.started:
				if !tc.want {
					t.Fatal("initial transfer without sufficient independent fresh evidence")
				}
			case <-time.After(500 * time.Millisecond):
				if tc.want {
					t.Fatal("initial quality still waits the full cadence after three fresh samples")
				}
			}
			if !tc.want {
				return
			}
			deadline := time.Now().Add(time.Second)
			for c.IsLifecycleBusy() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if c.IsLifecycleBusy() {
				t.Fatal("initial generation did not finish")
			}
			count := len(transport.callList())
			if count != 4 {
				t.Fatalf("bounded two-candidate early-stop transfers=%d want4", count)
			}
			c.requestSupervisorReconcile()
			time.Sleep(30 * time.Millisecond)
			if got := len(transport.callList()); got != count {
				t.Fatalf("initial opportunity repeated: %d -> %d", count, got)
			}
			if !c.AdaptiveSnapshot().NextRunAt.After(now.Add(2 * time.Hour)) {
				t.Fatal("normal cadence not retained")
			}
		})
	}
}

func TestAdaptiveInitialFailureConsumesOpportunityAndBusyDoesNot(t *testing.T) {
	now := time.Now().UTC()
	tags := []string{"proxy-current", "proxy-challenger"}
	delays := []int64{100, 100}
	s, _, _, _ := adaptiveEvidenceFixture(t, tags[0], now.Add(-time.Hour), supervisorPolicy(), tags, delays, now)
	seedAdaptiveEvidence(s, tags, now, delays)
	transport := &adaptiveTransportStub{duration: time.Second, failedDownload: 0, failedUpload: -1}
	c := NewCoordinator(supervisorPolicy(), s, nil, nil)
	c.SetAdaptiveRunner(&AdaptiveRunner{Probe: s.probe, Transport: transport})
	c.EnterMaintenance()
	c.runAdaptiveAdmission(context.Background(), true)
	if len(transport.callList()) != 0 {
		t.Fatal("busy startup transferred")
	}
	c.ExitMaintenance()
	c.runAdaptiveAdmission(context.Background(), true)
	c.mu.Lock()
	done := c.benchmarkDone
	c.mu.Unlock()
	if done == nil {
		t.Fatal("busy consumed initial opportunity")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("generation stuck")
	}
	calls := len(transport.callList())
	if calls != 3 {
		t.Fatalf("failed candidate did not consume bounded transfer: %d", calls)
	}
	c.runAdaptiveAdmission(context.Background(), true)
	c.mu.Lock()
	running := c.benchmarkDone != nil
	c.mu.Unlock()
	if running || len(transport.callList()) != calls {
		t.Fatal("failed initial generation retried before cadence")
	}
}
