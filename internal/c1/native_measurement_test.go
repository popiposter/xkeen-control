package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
