package nativequality

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
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

// A review that met a busy panel (for example the recovery check firing at
// the same instant) retries within a minute and keeps its trigger; other
// refusals keep the ten-minute retry.
func TestBusyReviewRetriesSoonAndRecoveryIsOffset(t *testing.T) {
	now := time.Now()
	if next, keep := nextAfterStart(now, fmt.Errorf("wrapped: %w", c1.ErrManualBusy), reviewCadence); !next.Equal(now.Add(time.Minute)) || !keep {
		t.Fatal("busy retry", next.Sub(now), keep)
	}
	if next, keep := nextAfterStart(now, ErrUnavailable, reviewCadence); !next.Equal(now.Add(10*time.Minute)) || keep {
		t.Fatal("refusal retry", next.Sub(now), keep)
	}
	if next, keep := nextAfterStart(now, nil, reviewCadence); !next.Equal(now.Add(reviewCadence)) || keep {
		t.Fatal("started review", next.Sub(now), keep)
	}
	// Reviews first run ten minutes after start; recovery must not tick then.
	if recoveryFirstCheck%recoveryInterval == 0 || recoveryFirstCheck <= 10*time.Minute {
		t.Fatal("recovery checks align with the review schedule", recoveryFirstCheck)
	}
}
