package nativequality

import (
	"context"
	"time"
)

// qualityCadence is the minimum gap between any two review starts (REQ-006).
const qualityCadence = 6 * time.Hour

// constrainedCadence paces the automatic constrained review; standardCadence
// paces the measure-only standard review, which reserves no quota, to one
// full review per 24 hours.
const constrainedCadence = 12 * time.Hour
const standardCadence = 24 * time.Hour

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
	startupFloor := time.Now().Add(10 * time.Minute)
	next := startupFloor
	trigger := "startup"
	// The measure-only standard review reserves no quota, so its minimum gap is
	// the full 24 hours; the constrained automatic review keeps the shared
	// six-hour gap and its daily quota.
	cadence, minGap := standardCadence, standardCadence
	if s.service.profile().Constrained {
		cadence, minGap = constrainedCadence, qualityCadence
	}
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
			var startErr error
			if s.service.profile().Constrained {
				startErr = s.service.startSweep(ctx, trigger)
			} else {
				// The standard profile measures only; applying is the operator's
				// explicit Stage until both profiles share automatic Apply.
				startErr = s.service.startReview(ctx, trigger, true)
			}
			if startErr != nil {
				next = now.Add(10 * time.Minute)
			} else {
				next = now.Add(cadence)
			}
			trigger = "periodic"
		}
	}
}
