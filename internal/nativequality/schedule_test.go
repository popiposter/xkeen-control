package nativequality

import (
	"context"
	"testing"
	"time"
)

func TestComparisonDueThrottlesRefreshAndManualStarts(t *testing.T) {
	now := time.Now()
	if got := comparisonDue(now, now.Add(2*time.Minute), now.Add(-time.Hour), qualityCadence); !got.Equal(now.Add(5 * time.Hour)) {
		t.Fatal("refresh bypassed minimum comparison interval")
	}
	if got := comparisonDue(now, now.Add(2*time.Minute), time.Time{}, qualityCadence); !got.Equal(now.Add(2 * time.Minute)) {
		t.Fatal("initial refresh not scheduled")
	}
	if got := comparisonDue(now, now.Add(-time.Minute), now.Add(-7*time.Hour), qualityCadence); !got.Equal(now) {
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
	if due := comparisonDue(now, requested, manualStart, qualityCadence); !due.Equal(manualStart.Add(6 * time.Hour)) {
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

func TestQuotaRetryAtWaitsForTheGapAndTheRollingDay(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	bytes := testReviewBytes
	if got := quotaRetryAt(quotaReceipt{Version: 1}, now, bytes); !got.Equal(now) {
		t.Fatalf("empty receipt retry = %s", got)
	}
	manual := quotaReceipt{Version: 1, LastComparisonStartedAt: now.Add(-2 * time.Hour)}
	if got := quotaRetryAt(manual, now, bytes); !got.Equal(now.Add(4 * time.Hour)) {
		t.Fatalf("six-hour gap retry = %s", got)
	}
	used := quotaReceipt{Version: 1, LastComparisonStartedAt: now.Add(-7 * time.Hour), Reservations: []quotaReservation{{At: now.Add(-7 * time.Hour), Bytes: bytes}}}
	if got := quotaRetryAt(used, now, bytes); !got.Equal(now.Add(17 * time.Hour)) {
		t.Fatalf("rolling-day retry = %s", got)
	}
	if quotaAdmits(used, now, bytes) {
		t.Fatal("a second automatic review was admitted inside 24 hours")
	}
	fenced := quotaReceipt{Version: 1, InspectionRequired: true}
	if got := quotaRetryAt(fenced, now, bytes); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("inspection-held retry = %s", got)
	}
}
