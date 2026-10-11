package nativequality

import (
	"context"
	"errors"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

const (
	// settleDelay lets a restarted Xray load its outbounds before probes.
	settleDelay = time.Minute
	// readyWait bounds waiting for the native API after a restart.
	readyWait = 5 * time.Minute
	// reviewStartAttempts bounds busy retries of the nightly review start.
	reviewStartAttempts = 30
	// geodataWait bounds the native geodata update (its own limit is 15 min).
	geodataWait = 16 * time.Minute
	// clockCheck is the longest the schedule sleeps before it re-reads the
	// wall clock, so an NTP correction or a zone change cannot move the
	// window into the day.
	clockCheck = 10 * time.Minute
	// windowGrace is how late a window may still start; a later wake-up
	// (for example after a forward clock jump) skips to the next night.
	windowGrace = time.Hour
)

// MaintenanceStatus is the last nightly maintenance window (revision
// 2026-10-11): subscriptions, settle, review, geodata.
type MaintenanceStatus struct {
	Phase                  string    `json:"phase,omitempty"`
	StartedAt              time.Time `json:"startedAt,omitzero"`
	FinishedAt             time.Time `json:"finishedAt,omitzero"`
	SubscriptionsRefreshed bool      `json:"subscriptionsRefreshed"`
	ReviewStarted          bool      `json:"reviewStarted"`
	ReviewReason           string    `json:"reviewReason,omitempty"`
	GeodataState           string    `json:"geodataState,omitempty"`
}

// Schedule runs the nightly maintenance window and the REQ-009 recovery
// checks. It is the only automatic owner of reviews.
type Schedule struct {
	service *Service
	// Subscriptions refreshes every enabled subscription once and returns
	// when every attempt has settled.
	Subscriptions func(context.Context)
	// Geodata runs the native geodata update, which restarts Xray, and
	// returns its terminal job state.
	Geodata func(context.Context) (string, error)
	// Finished receives the finished window and the review status read after
	// it, for the operator report; it must not block for long.
	Finished func(context.Context, MaintenanceStatus, Status)
	// Location returns the router's local time zone. It is read again for
	// every window so a zone change takes effect; nil means time.Local.
	Location func() *time.Location
	// settle is the pause after the native API answers and retry the pause
	// after a busy start; tests shorten them.
	settle, retry time.Duration
	// start starts the nightly review, applying its decision when apply is
	// set; tests replace it.
	start func(ctx context.Context, apply bool) (<-chan struct{}, error)
}

func NewSchedule(service *Service) *Schedule {
	return &Schedule{service: service, settle: settleDelay, retry: busyRetry, start: service.startNightly}
}

// NextWindow returns the next maintenance start after now at the configured
// local time, or the zero time when the window is turned off.
func (s *Schedule) NextWindow(now time.Time) time.Time {
	settings := s.service.settings()
	window, err := settings.Validate()
	if !settings.Enabled || err != nil {
		return time.Time{}
	}
	loc := time.Local
	if s.Location != nil {
		if l := s.Location(); l != nil {
			loc = l
		}
	}
	return nextWindow(now, loc, window)
}

// NextSubscriptionRefresh is when the window will next refresh
// subscriptions, or the zero time when that step or the window is off.
func (s *Schedule) NextSubscriptionRefresh(now time.Time) time.Time {
	if !s.service.settings().RefreshSubscriptions {
		return time.Time{}
	}
	return s.NextWindow(now)
}

func nextWindow(now time.Time, loc *time.Location, offset time.Duration) time.Time {
	local := now.In(loc)
	hour, minute := int(offset/time.Hour), int(offset%time.Hour/time.Minute)
	at := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !at.After(local) {
		at = time.Date(local.Year(), local.Month(), local.Day()+1, hour, minute, 0, 0, loc)
	}
	return at
}

// due decides, after a wake-up at now, whether the window planned for next
// starts: only from next until windowGrace after it. Otherwise it returns the
// window recomputed from the current clock and zone, so a backward or forward
// clock jump or a zone change moves the plan instead of the start time.
func (s *Schedule) due(now, next time.Time) (bool, time.Time) {
	if !next.IsZero() && !now.Before(next) && now.Sub(next) <= windowGrace {
		return true, next
	}
	return false, s.NextWindow(now)
}

func (s *Schedule) Run(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	go s.runRecovery(ctx)
	next := s.NextWindow(time.Now())
	for {
		s.service.mu.Lock()
		s.service.nextMaintenance = next
		s.service.mu.Unlock()
		wait := clockCheck
		if !next.IsZero() {
			wait = max(min(time.Until(next), clockCheck), time.Second)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.service.Settings.Changes():
			// A saved setting re-plans the window at once.
			timer.Stop()
			next = s.NextWindow(time.Now())
			continue
		case <-timer.C:
		}
		run, planned := s.due(time.Now(), next)
		next = planned
		if !run {
			continue
		}
		s.runNight(ctx)
		next = s.NextWindow(time.Now())
	}
}

// runNight performs the window strictly in order. Each step is bounded and a
// failed step does not skip the later ones: geodata still updates when the
// review could not run. A step the operator turned off is skipped.
func (s *Schedule) runNight(ctx context.Context) {
	svc := s.service
	settings := svc.settings()
	svc.mu.Lock()
	svc.maintenance = MaintenanceStatus{Phase: "subscriptions", StartedAt: time.Now().UTC()}
	svc.mu.Unlock()
	phase := func(name string) {
		svc.mu.Lock()
		svc.maintenance.Phase = name
		svc.mu.Unlock()
	}
	refreshed := settings.RefreshSubscriptions && s.Subscriptions != nil
	if refreshed {
		s.Subscriptions(ctx)
	}
	svc.mu.Lock()
	svc.maintenance.SubscriptionsRefreshed = refreshed
	svc.mu.Unlock()
	if settings.Review {
		phase("settle")
		s.waitNativeReady(ctx)
		if refreshed && !sleepCtx(ctx, s.settle) {
			return
		}
		phase("review")
	}
	var reason string
	started := false
	if !settings.Review {
		reason = "review-disabled"
	}
	for attempt := 0; settings.Review && attempt < reviewStartAttempts && ctx.Err() == nil; attempt++ {
		done, err := s.start(ctx, settings.AutoApply)
		if err == nil {
			started = true
			select {
			case <-ctx.Done():
			case <-done:
			}
			break
		}
		if !busyStart(err) {
			svc.mu.Lock()
			reason = svc.status.ReviewReason
			svc.mu.Unlock()
			if reason == "" {
				reason = "review-unavailable"
			}
			break
		}
		if !sleepCtx(ctx, s.retry) {
			return
		}
	}
	svc.mu.Lock()
	svc.maintenance.ReviewStarted = started
	svc.maintenance.ReviewReason = reason
	svc.mu.Unlock()
	phase("geodata")
	geodata := "disabled"
	if !settings.UpdateGeodata {
		// Off by the operator: nothing updates geodata automatically.
	} else if s.Geodata == nil {
		geodata = "skipped"
	} else {
		state, err := s.Geodata(ctx)
		geodata = state
		if err != nil && geodata == "" {
			geodata = "failed"
		}
	}
	svc.mu.Lock()
	svc.maintenance.GeodataState = geodata
	svc.maintenance.Phase = ""
	svc.maintenance.FinishedAt = time.Now().UTC()
	finished := svc.maintenance
	svc.mu.Unlock()
	if s.Finished != nil && settings.Report && ctx.Err() == nil {
		s.Finished(ctx, finished, svc.Read())
	}
}

// waitNativeReady polls the native API until it answers or readyWait passes.
func (s *Schedule) waitNativeReady(ctx context.Context) {
	if s.service.Reader == nil {
		return
	}
	deadline := time.Now().Add(readyWait)
	for time.Now().Before(deadline) {
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		snapshot := s.service.Reader.Snapshot(probe)
		cancel()
		if snapshot.APIReachable && snapshot.RoutingReachable {
			return
		}
		if !sleepCtx(ctx, 5*time.Second) {
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// busyRetry is how soon a nightly review that found the panel busy (an
// Apply or a recovery check) tries again.
const busyRetry = time.Minute

// busyStart reports whether a refused start was transient: another owner
// held the panel or the receipt lock. A broken receipt or an inspection
// fence is not retried.
func busyStart(err error) bool {
	return errors.Is(err, c1.ErrManualBusy) || errors.Is(err, errQuotaBusy)
}

// recoveryFirstCheck offsets recovery checks by half an interval from panel
// start so they do not meet other ten-minute work at the same instant.
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

// Enabled reports whether Run owns automatic work on this panel. When it
// does not, subscriptions keep their own refresh cadence.
func (s *Schedule) Enabled() bool {
	return s.service.profile().Automatic && !s.service.AutomaticDisabled
}

// UpdateGeodata runs the native geodata update through the shared job runner
// and waits for its terminal state. A busy runner is retried every minute.
func (s *Service) UpdateGeodata(ctx context.Context) (string, error) {
	return s.updateGeodata(ctx, busyRetry, 2*time.Second)
}

func (s *Service) updateGeodata(ctx context.Context, retry, poll time.Duration) (string, error) {
	if s.Jobs == nil || s.Editor == nil {
		return "unavailable", ErrUnavailable
	}
	var job xkeen.JobView
	var err error
	for attempt := 0; attempt < reviewStartAttempts; attempt++ {
		job, err = s.Jobs.StartMaintenance("update-geodata", s.Editor)
		if !errors.Is(err, authority.ErrBusy) {
			break
		}
		if !sleepCtx(ctx, retry) {
			return "canceled", ctx.Err()
		}
	}
	if err != nil {
		return "refused", err
	}
	wait, cancel := context.WithTimeout(ctx, geodataWait)
	defer cancel()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for job.State == "running" {
		select {
		case <-wait.Done():
			return "unknown", wait.Err()
		case <-tick.C:
			if job, err = s.Jobs.Read(xkeen.MaintenanceJobOwner, job.ID, 0); err != nil {
				return "unknown", err
			}
		}
	}
	return job.State, nil
}
