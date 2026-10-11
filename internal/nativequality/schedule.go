package nativequality

import (
	"context"
	"errors"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

// qualityCadence is the minimum gap between any two review starts (REQ-006).
const qualityCadence = 6 * time.Hour

// reviewCadence is the daily due review (REQ-006). The quota allows one
// automatic review per rolling 24 hours on both profiles.
const reviewCadence = 24 * time.Hour

// Schedule coalesces refreshes and admits at most one bounded review owner.
type Schedule struct {
	service *Service
	refresh chan struct{}
}

func NewSchedule(service *Service) *Schedule {
	return &Schedule{service: service, refresh: make(chan struct{}, 1)}
}

func (s *Schedule) NotifyRefresh() {
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

// comparisonDue applies the profile's minimum gap after the last start of any
// review, manual or automatic, including reviews pulled forward by a refresh.
func comparisonDue(now, requested, lastStarted time.Time, minGap time.Duration) time.Time {
	if earliest := lastStarted.Add(minGap); !lastStarted.IsZero() && requested.Before(earliest) {
		requested = earliest
	}
	if requested.Before(now) {
		return now
	}
	return requested
}

func refreshDue(now, startupFloor, next time.Time) time.Time {
	requested := now.Add(2 * time.Minute)
	if requested.Before(startupFloor) {
		requested = startupFloor
	}
	if requested.Before(next) {
		return requested
	}
	return next
}

func (s *Schedule) Run(ctx context.Context) {
	if !s.service.profile().Automatic || s.service.AutomaticDisabled {
		return
	}
	go s.runRecovery(ctx)
	startupFloor := time.Now().Add(10 * time.Minute)
	next := startupFloor
	trigger := "startup"
	// One policy for both profiles: a daily due review, the six-hour gap after
	// any start, and the rolling one-review quota (REQ-006).
	cadence, minGap := reviewCadence, qualityCadence
	for {
		s.service.mu.Lock()
		last := s.service.lastStartedAt
		s.service.mu.Unlock()
		// Both profiles record every start in the receipt, so a panel restart
		// does not reset the gap.
		if q, err := quotaState(s.service.QuotaPath, time.Now().UTC(), s.service.profile().Review().Bytes); err == nil && q.LastStartedAt.After(last) {
			last = q.LastStartedAt
			s.service.mu.Lock()
			s.service.lastStartedAt = last
			s.service.mu.Unlock()
		}
		next = comparisonDue(time.Now(), next, last, minGap)
		if next.Before(startupFloor) {
			next = startupFloor
		}
		// Show the real start: a review inside the quota window waits for it.
		if retry, ok := s.service.quotaRetryAt(time.Now()); ok && retry.After(next) {
			next = retry
		}
		s.service.mu.Lock()
		s.service.status.NextDueAt = next
		s.service.mu.Unlock()
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.refresh:
			timer.Stop()
			requested := refreshDue(time.Now(), startupFloor, next)
			if requested.Before(next) {
				next = requested
				trigger = "subscription-refresh"
			}
		case <-timer.C:
			// Recheck manual starts that happened while the timer was waiting.
			s.service.mu.Lock()
			last = s.service.lastStartedAt
			s.service.mu.Unlock()
			if q, err := quotaState(s.service.QuotaPath, time.Now().UTC(), s.service.profile().Review().Bytes); err == nil && q.LastStartedAt.After(last) {
				last = q.LastStartedAt
			} else if err != nil {
				next = time.Now().Add(10 * time.Minute)
				continue
			}
			now := time.Now()
			if due := comparisonDue(now, next, last, minGap); due.After(now) {
				next = due
				continue
			}
			if ctx.Err() != nil {
				return
			}
			// A trigger inside the quota window waits for the next allowed slot
			// instead of repeatedly reading configuration and Xray.
			if retry, ok := s.service.quotaRetryAt(now); ok && retry.After(now) {
				next = retry
				continue
			}
			var keep bool
			next, keep = nextAfterStart(now, s.service.startSweep(ctx, trigger), cadence)
			if !keep {
				trigger = "periodic"
			}
		}
	}
}

// busyRetry is how soon a review that found the panel busy (another review,
// an Apply or a recovery check) tries again; it keeps its trigger.
const busyRetry = time.Minute

// nextAfterStart returns the next review time after one start attempt and
// whether the trigger is kept. A busy panel is transient: retrying on the
// ten-minute cadence would meet the recovery check again at the same instant.
func nextAfterStart(now time.Time, err error, cadence time.Duration) (time.Time, bool) {
	switch {
	case err == nil:
		return now.Add(cadence), false
	case errors.Is(err, c1.ErrManualBusy):
		return now.Add(busyRetry), true
	default:
		return now.Add(10 * time.Minute), false
	}
}

// recoveryFirstCheck offsets recovery checks by half an interval from the
// review schedule, which also starts ten minutes after panel start.
const recoveryFirstCheck = recoveryInterval + recoveryInterval/2

// runRecovery checks every recoveryInterval whether the pool still has a
// healthy member and, if not, runs one bounded REQ-009 attempt.
func (s *Schedule) runRecovery(ctx context.Context) {
	first := time.NewTimer(recoveryFirstCheck)
	defer first.Stop()
	select {
	case <-ctx.Done():
		return
	case <-first.C:
		s.service.recoverPool(ctx)
	}
	tick := time.NewTicker(recoveryInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.service.recoverPool(ctx)
		}
	}
}
