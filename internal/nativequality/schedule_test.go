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

// The window starts only from its planned time until the grace period; any
// clock jump or zone change re-plans it from the current clock instead.
func TestDueFollowsClockJumpsAndZoneChanges(t *testing.T) {
	zone := time.FixedZone("router", 3*3600)
	s := NewSchedule(&Service{})
	s.Location = func() *time.Location { return zone }
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	next := at(11, 4, 0)
	if run, planned := s.due(at(11, 4, 0).Add(time.Second), next); !run || !planned.Equal(next) {
		t.Fatal("on-time wake did not run", run, planned)
	}
	// Booted at 01:00 by a wrong clock; NTP then set 13:00: skip to tomorrow.
	if run, planned := s.due(at(11, 13, 0), next); run || !planned.Equal(at(12, 4, 0)) {
		t.Fatal("forward jump ran in the day", run, planned)
	}
	// Planned at a wrong 13:00 for tomorrow; NTP then set 01:00 today.
	if run, planned := s.due(at(11, 1, 0), at(12, 4, 0)); run || !planned.Equal(at(11, 4, 0)) {
		t.Fatal("backward jump kept a window a day late", run, planned)
	}
	// The operator moved the zone two hours east: 04:00 is earlier in UTC.
	zone = time.FixedZone("router", 5*3600)
	if run, planned := s.due(next.Add(-3*time.Hour), next); run || !planned.Equal(time.Date(2026, 10, 11, 4, 0, 0, 0, zone)) || !planned.Before(next) {
		t.Fatal("zone change ignored", run, planned)
	}
}

func TestNextWindowKeepsWallTimeAcrossDaylightSaving(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no zone database", err)
	}
	// 2026-10-25 03:00 CEST becomes 02:00 CET.
	got := nextWindow(time.Date(2026, 10, 24, 23, 0, 0, 0, loc), loc, DefaultWindow)
	if got.Hour() != 4 || got.Minute() != 0 || got.Day() != 25 {
		t.Fatal("window drifted across the DST change", got)
	}
}

// A busy nightly start is retried; a final refusal is not, and geodata still
// runs in both cases.
func TestNightRetriesBusyReviewStart(t *testing.T) {
	svc := &Service{}
	s := NewSchedule(svc)
	s.settle, s.retry = 0, 0
	calls := 0
	s.start = func(context.Context) (<-chan struct{}, error) {
		calls++
		if calls < 3 {
			return nil, fmt.Errorf("wrapped: %w", c1.ErrManualBusy)
		}
		done := make(chan struct{})
		close(done)
		return done, nil
	}
	geodata := 0
	s.Geodata = func(context.Context) (string, error) { geodata++; return "completed", nil }
	s.runNight(context.Background())
	if calls != 3 || !svc.maintenance.ReviewStarted || geodata != 1 {
		t.Fatal("busy start not retried", calls, svc.maintenance, geodata)
	}
	calls = 0
	s.start = func(context.Context) (<-chan struct{}, error) { calls++; return nil, ErrUnavailable }
	s.runNight(context.Background())
	if calls != 1 || svc.maintenance.ReviewStarted || svc.maintenance.ReviewReason != "review-unavailable" || geodata != 2 {
		t.Fatal("final refusal retried or geodata skipped", calls, svc.maintenance, geodata)
	}
}
