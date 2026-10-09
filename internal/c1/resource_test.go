package c1

import (
	"context"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	"testing"
	"time"
)

func quietConstrainedGuard() *resourcepolicy.Guard {
	s := resourcepolicy.Sample{At: time.Now(), TotalKiB: 254472, AvailableKiB: 100000}
	return &resourcepolicy.Guard{Profile: resourcepolicy.ForPlatform("mipsle", 254472), Interval: time.Millisecond, Read: func() (resourcepolicy.Sample, error) {
		s.At = s.At.Add(time.Second)
		s.Total += 100
		s.Idle += 95
		return s, nil
	}}
}

func TestConstrainedManualBytesAndCleanup(t *testing.T) {
	for _, failed := range []int{-1, 0} {
		api := &benchmarkProbeAPI{}
		runner := NewManualNodeRunner(NewProbeRouter(api))
		runner.Resources = quietConstrainedGuard()
		runner.Transport = &manualTransportStub{stageDuration: 300 * time.Millisecond, failedDownload: failed}
		result := runner.Run(context.Background(), validManualTestNode(), nil)
		if result.BytesPlanned != 4*MiB || result.PlannedStages != 7 || result.BytesTransferred > 4*MiB || len(api.removes) != 1 {
			t.Fatal(result, api.removes)
		}
		want := int64(4 * MiB)
		if failed == 0 {
			want -= MiB / 4
		}
		if result.BytesTransferred != want {
			t.Fatal("partial bytes lost", result.BytesTransferred, want)
		}
	}
}

func TestConstrainedComparisonFailuresConsumeBudgetAndStopAtFourAttempts(t *testing.T) {
	api := &benchmarkProbeAPI{}
	runner := NewAdaptiveRunner(NewProbeRouter(api))
	runner.Resources = quietConstrainedGuard()
	runner.Transport = &adaptiveTransportStub{duration: 300 * time.Millisecond, failedDownload: 0, failedUpload: -1}
	g := AdaptiveGeneration{Generation: 1, NativeQuality: true, BroadSample: true}
	for i, tag := range []string{"proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e"} {
		c := AdaptiveCandidateInput{Tag: tag, RTTMS: 20}
		if i < 3 {
			g.Candidates = append(g.Candidates, c)
		} else {
			g.Fallbacks = append(g.Fallbacks, c)
		}
	}
	result := runner.Run(context.Background(), g, nil)
	if result.State != "completed" || len(result.Candidates) != 4 || result.ValidCount != 3 || result.AggregateBytes != 18*MiB+MiB/2 || len(api.removes) != 4 {
		t.Fatal(result, api.removes)
	}
}

func TestResourceCancellationRetainsProbeCleanup(t *testing.T) {
	api := &benchmarkProbeAPI{}
	runner := NewAdaptiveRunner(NewProbeRouter(api))
	g := quietConstrainedGuard()
	read := g.Read
	calls := 0
	g.Read = func() (resourcepolicy.Sample, error) {
		s, e := read()
		calls++
		if calls > 3 {
			s.AvailableKiB = 1
		}
		return s, e
	}
	runner.Resources = g
	runner.Transport = &adaptiveTransportStub{failedDownload: -1, failedUpload: -1, block: make(chan struct{})}
	result := runner.Run(context.Background(), AdaptiveGeneration{Generation: 1, NativeQuality: true, Candidates: []AdaptiveCandidateInput{{Tag: "proxy-a", RTTMS: 20}, {Tag: "proxy-b", RTTMS: 30}}}, nil)
	if result.ReasonCode != "resource-pressure" || len(api.removes) != 1 || runner.Probe.Blocked() {
		t.Fatal(result, api.removes)
	}
}

func TestManualAdmissionCancellationIsNotPressure(t *testing.T) {
	api := &benchmarkProbeAPI{}
	runner := NewManualNodeRunner(NewProbeRouter(api))
	runner.Resources = quietConstrainedGuard()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runner.Run(ctx, validManualTestNode(), nil)
	if result.ErrorCode != "cancelled" || len(api.adds) != 0 {
		t.Fatal(result, api.adds)
	}
}
