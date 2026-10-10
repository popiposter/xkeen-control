package nativequality

import (
	"context"
	"time"
)

const qualityCadence = 6 * time.Hour
const constrainedCadence = 12 * time.Hour

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

func comparisonDue(now, requested, lastStarted time.Time) time.Time {
	if earliest := lastStarted.Add(qualityCadence); !lastStarted.IsZero() && requested.Before(earliest) {
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
	cadence := qualityCadence
	if s.service.profile().Constrained {
		cadence = constrainedCadence
	}
	for {
		s.service.mu.Lock()
		last := s.service.lastStartedAt
		s.service.mu.Unlock()
		if s.service.profile().Constrained {
			if q, err := quotaState(s.service.QuotaPath, time.Now().UTC(), s.service.profile().Review().Bytes); err == nil && q.LastStartedAt.After(last) {
				last = q.LastStartedAt
				s.service.mu.Lock()
				s.service.lastStartedAt = last
				s.service.mu.Unlock()
			}
		}
		next = comparisonDue(time.Now(), next, last)
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
			if s.service.profile().Constrained {
				if q, err := quotaState(s.service.QuotaPath, time.Now().UTC(), s.service.profile().Review().Bytes); err == nil && q.LastStartedAt.After(last) {
					last = q.LastStartedAt
				} else if err != nil {
					next = time.Now().Add(10 * time.Minute)
					continue
				}
			}
			now := time.Now()
			if due := comparisonDue(now, next, last); due.After(now) {
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
