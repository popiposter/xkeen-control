package c1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBroadManualMeasuresTwelveAndKeepsTransferCleanupBounded(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &adaptiveTransportStub{duration: time.Second, failedDownload: 0, failedUpload: -1}
	runner := &AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport}
	g := AdaptiveGeneration{NativeQuality: true, BroadSample: true, Generation: 1, StartedAt: time.Now()}
	for i := 0; i < 18; i++ {
		c := AdaptiveCandidateInput{Tag: fmt.Sprintf("proxy-%02d", i), RTTMS: int64(100 + i*5)}
		if i < 12 {
			g.Candidates = append(g.Candidates, c)
		} else {
			g.Fallbacks = append(g.Fallbacks, c)
		}
	}
	var final AdaptivePerformanceStatus
	r := runner.Run(context.Background(), g, func(p AdaptivePerformanceStatus) { final = p })
	if r.State != "completed" || r.ValidCount != 12 || len(r.Candidates) != 13 || len(final.Candidates) != 13 || r.Candidates[12].Tag != "proxy-12" || r.AggregateBytes > NativeQualityBroadBytes || len(api.adds) != 13 || len(api.removes) != 13 || runner.Probe.Blocked() {
		t.Fatalf("broad sample or cleanup failed: %+v %+v", r, final)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if adaptiveCanAdmitCandidate(ctx, NativeQualityBroadBytes-AdaptiveMaxDownloadBytes-AdaptiveMaxUploadBytes+1, NativeQualityBroadBytes) {
		t.Fatal("broad byte ceiling exceeded")
	}
	g.BroadSample = false
	if got := runner.Run(context.Background(), g, nil); got.State != "failed" {
		t.Fatal("automatic twelve-node test admitted")
	}
}

func TestMeasureNativeQualitySharesAdmissionAndApplyCleanup(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &adaptiveTransportStub{started: make(chan struct{}), block: make(chan struct{}), failedDownload: -1, failedUpload: -1}
	c := NewCoordinator(DefaultPolicy(), nil, nil, nil)
	c.SetAdaptiveRunner(&AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport})
	done := make(chan error, 1)
	g := AdaptiveGeneration{Generation: 1, StartedAt: time.Now(), CurrentTarget: "proxy-a", Candidates: []AdaptiveCandidateInput{{Tag: "proxy-a", RTTMS: 20}, {Tag: "proxy-b", RTTMS: 30}}}
	go func() {
		r, err := c.MeasureNativeQuality(context.Background(), g, nil)
		if err == nil && r.State != "cancelled" {
			err = errors.New("generation not cancelled")
		}
		done <- err
	}()
	<-transport.started
	if c.AdaptiveSnapshot().State != "running" {
		t.Fatal("shared progress omitted")
	}
	if _, err := c.MeasureNativeQuality(context.Background(), g, nil); !errors.Is(err, ErrManualBusy) {
		t.Fatal("parallel diagnostic admitted", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := c.BeginApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c.IsLifecycleBusy() || c.AdaptiveSnapshot().State != "cancelled" {
		t.Fatal("cleanup/progress not settled")
	}
}

func TestNativeSpeedTestReplacesFailedCandidatesInLatencyOrder(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &adaptiveTransportStub{duration: time.Second, failedDownload: 0, failedUpload: -1}
	runner := &AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport}
	g := adaptiveTestGeneration("proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f")
	g.NativeQuality = true
	g.CurrentTarget = "proxy-not-in-shortlist"
	g.Fallbacks = []AdaptiveCandidateInput{{Tag: "proxy-g", RTTMS: 200}, {Tag: "proxy-h", RTTMS: 210}}
	var final AdaptivePerformanceStatus
	r := runner.Run(context.Background(), g, func(p AdaptivePerformanceStatus) { final = p })
	if r.State != "completed" || r.ValidCount != 6 || len(r.Candidates) != 7 || r.Candidates[0].Valid || r.Candidates[6].Tag != "proxy-g" || len(final.Candidates) != 7 || final.ValidCount != 6 {
		t.Fatalf("replacement failed: %+v %+v", r, final)
	}
	if len(api.adds) != 7 || len(api.removes) != 7 || runner.Probe.Blocked() {
		t.Fatal("diagnostic cleanup or attempt count")
	}
	pool := []string{"proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f", "proxy-g", "proxy-h"}
	if _, err := NativeQualityCosts(r, r.CompletedAt, pool); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSpeedTestReplacementDoesNotIncreaseTransferBudget(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &adaptiveTransportStub{duration: 300 * time.Millisecond, failedDownload: 0, failedUpload: -1}
	runner := &AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport}
	g := adaptiveTestGeneration("proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f")
	g.NativeQuality = true
	g.Fallbacks = []AdaptiveCandidateInput{{Tag: "proxy-g", RTTMS: 200}}
	r := runner.Run(context.Background(), g, nil)
	if r.State != "completed" || r.ReasonCode != AdaptiveReasonGenerationBudget || r.ValidCount != 5 || len(api.adds) != 6 || r.AggregateBytes > AdaptiveMaxGenerationBytes {
		t.Fatalf("budget exceeded or partial result hidden: %+v", r)
	}
}
