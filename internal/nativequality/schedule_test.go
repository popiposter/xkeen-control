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
