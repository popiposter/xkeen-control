package nativequality

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func TestNextWindowIsTheNextLocalFourOClock(t *testing.T) {
	loc := time.FixedZone("router", 3*3600)
	for _, c := range []struct{ now, want time.Time }{
		{time.Date(2026, 10, 11, 1, 30, 0, 0, loc), time.Date(2026, 10, 11, 4, 0, 0, 0, loc)},
		{time.Date(2026, 10, 11, 4, 0, 0, 0, loc), time.Date(2026, 10, 12, 4, 0, 0, 0, loc)},
		{time.Date(2026, 10, 11, 23, 59, 0, 0, loc), time.Date(2026, 10, 12, 4, 0, 0, 0, loc)},
		// 00:30 UTC is 03:30 at the router: the window is half an hour away.
		{time.Date(2026, 10, 11, 0, 30, 0, 0, time.UTC), time.Date(2026, 10, 11, 4, 0, 0, 0, loc)},
	} {
		if got := nextWindow(c.now, loc, DefaultWindow); !got.Equal(c.want) {
			t.Fatal(c.now, got, c.want)
		}
	}
}

func TestScheduleStopsBeforeMeasurement(t *testing.T) {
	s := NewSchedule(&Service{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	if !s.service.lastStartedAt.IsZero() {
		t.Fatal("measurement started after shutdown")
	}
}

// The window runs its steps in order, and a review that cannot run does not
// skip the geodata update.
func TestNightRunsStepsInOrderAndAlwaysUpdatesGeodata(t *testing.T) {
	svc := &Service{}
	s := NewSchedule(svc)
	s.settle = 0
	var steps []string
	s.Subscriptions = func(context.Context) {
		svc.mu.Lock()
		phase := svc.maintenance.Phase
		svc.mu.Unlock()
		steps = append(steps, "subscriptions:"+phase)
	}
	s.Geodata = func(context.Context) (string, error) {
		svc.mu.Lock()
		phase := svc.maintenance.Phase
		svc.mu.Unlock()
		steps = append(steps, "geodata:"+phase)
		return "succeeded", nil
	}
	s.runNight(context.Background())
	if fmt.Sprint(steps) != "[subscriptions:subscriptions geodata:geodata]" {
		t.Fatal("steps", steps)
	}
	m := svc.maintenance
	if m.Phase != "" || m.StartedAt.IsZero() || m.FinishedAt.Before(m.StartedAt) || m.ReviewStarted || m.ReviewReason != "review-unavailable" || m.GeodataState != "succeeded" {
		t.Fatalf("maintenance status %+v", m)
	}
}

// A nightly review that met a busy panel (an Apply or the recovery check)
// retries within a minute; other refusals are final for the night.
func TestBusyReviewRetriesAndRecoveryIsOffset(t *testing.T) {
	if !busyStart(fmt.Errorf("wrapped: %w", c1.ErrManualBusy)) || !busyStart(errQuotaBusy) {
		t.Fatal("busy start not retried")
	}
	if busyStart(errQuota) || busyStart(ErrUnavailable) || busyStart(nil) {
		t.Fatal("final refusal retried")
	}
	if recoveryFirstCheck%recoveryInterval == 0 || recoveryFirstCheck <= 10*time.Minute {
		t.Fatal("recovery checks align with other ten-minute work", recoveryFirstCheck)
	}
}
