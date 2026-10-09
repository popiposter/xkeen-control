package nativequality

import (
	"context"
	"testing"
	"time"
)

func TestComparisonDueThrottlesRefreshAndManualStarts(t *testing.T) {
	now := time.Now()
	if got := comparisonDue(now, now.Add(2*time.Minute), now.Add(-time.Hour)); !got.Equal(now.Add(5 * time.Hour)) {
		t.Fatal("refresh bypassed minimum comparison interval")
	}
	if got := comparisonDue(now, now.Add(2*time.Minute), time.Time{}); !got.Equal(now.Add(2 * time.Minute)) {
		t.Fatal("initial refresh not scheduled")
	}
	if got := comparisonDue(now, now.Add(-time.Minute), now.Add(-7*time.Hour)); !got.Equal(now) {
		t.Fatal("overdue comparison not eligible")
	}
}

func TestRefreshDueHonorsStartupAndManualCooldown(t *testing.T) {
	now := time.Now().UTC()
	floor := now.Add(10 * time.Minute)
	if due := refreshDue(now, floor, now.Add(12*time.Hour)); !due.Equal(floor) {
		t.Fatal("refresh bypassed startup floor", due)
	}
	later := now.Add(11 * time.Minute)
	requested := refreshDue(later, floor, now.Add(12*time.Hour))
	if !requested.Equal(later.Add(2 * time.Minute)) {
		t.Fatal("refresh not coalesced after two minutes", requested)
	}
	manualStart := now.Add(9 * time.Minute)
	if due := comparisonDue(now, requested, manualStart); !due.Equal(manualStart.Add(6 * time.Hour)) {
		t.Fatal("manual test cooldown bypassed", due)
	}
}

func TestScheduleCoalescesRefreshAndStopsBeforeMeasurement(t *testing.T) {
	s := NewSchedule(&Service{})
	for i := 0; i < 100; i++ {
		s.NotifyRefresh()
	}
	if len(s.refresh) != 1 {
		t.Fatal("refresh storm not coalesced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	if !s.service.lastStartedAt.IsZero() {
		t.Fatal("measurement started after shutdown")
	}
}
