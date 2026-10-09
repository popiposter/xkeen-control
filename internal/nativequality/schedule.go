package nativequality

import (
	"context"
	"time"
)

const qualityCadence = 6 * time.Hour

// Schedule only runs bounded measurements. It never stages config, restarts the
// native service or changes selection. Refresh notifications are coalesced.
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

func (s *Schedule) Run(ctx context.Context) {
	if !s.service.profile().Automatic {
		return
	}
	next := time.Now().Add(10 * time.Minute)
	for {
		s.service.mu.Lock()
		last := s.service.lastStartedAt
		s.service.mu.Unlock()
		next = comparisonDue(time.Now(), next, last)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.refresh:
			timer.Stop()
			requested := time.Now().Add(2 * time.Minute)
			if requested.Before(next) {
				next = requested
			}
		case <-timer.C:
			// Recheck manual starts that happened while the timer was waiting.
			s.service.mu.Lock()
			last = s.service.lastStartedAt
			s.service.mu.Unlock()
			now := time.Now()
			if due := comparisonDue(now, next, last); due.After(now) {
				next = due
				continue
			}
			if ctx.Err() != nil {
				return
			}
			if s.service.start(ctx, false) != nil {
				next = now.Add(10 * time.Minute)
			} else {
				next = now.Add(qualityCadence)
			}
		}
	}
}
