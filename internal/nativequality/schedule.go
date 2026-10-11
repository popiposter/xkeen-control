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
	// DefaultWindow is the local time of day the nightly maintenance starts.
	DefaultWindow = 4 * time.Hour
	// settleDelay lets a restarted Xray load its outbounds before probes.
	settleDelay = time.Minute
	// readyWait bounds waiting for the native API after a restart.
	readyWait = 5 * time.Minute
	// reviewStartAttempts bounds busy retries of the nightly review start.
	reviewStartAttempts = 30
	// geodataWait bounds the native geodata update (its own limit is 15 min).
	geodataWait = 16 * time.Minute
)

// MaintenanceStatus is the last nightly maintenance window (revision
// 2026-10-11): subscriptions, settle, review, geodata.
type MaintenanceStatus struct {
	Phase         string    `json:"phase,omitempty"`
	StartedAt     time.Time `json:"startedAt,omitzero"`
	FinishedAt    time.Time `json:"finishedAt,omitzero"`
	ReviewStarted bool      `json:"reviewStarted"`
	ReviewReason  string    `json:"reviewReason,omitempty"`
	GeodataState  string    `json:"geodataState,omitempty"`
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
	// Location is the router's local time zone; nil means time.Local.
	Location *time.Location
	// Window is the local time of day the maintenance starts.
	Window time.Duration
	// settle is the pause after the native API answers; tests shorten it.
	settle time.Duration
}

func NewSchedule(service *Service) *Schedule {
	return &Schedule{service: service, Window: DefaultWindow, settle: settleDelay}
}

// NextWindow returns the next maintenance start after now.
func (s *Schedule) NextWindow(now time.Time) time.Time {
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	return nextWindow(now, loc, s.Window)
}

func nextWindow(now time.Time, loc *time.Location, offset time.Duration) time.Time {
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	at := day.Add(offset)
	if !at.After(local) {
		at = day.AddDate(0, 0, 1).Add(offset)
	}
	return at
}

func (s *Schedule) Run(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	go s.runRecovery(ctx)
	for {
		next := s.NextWindow(time.Now())
		s.service.mu.Lock()
		s.service.status.NextDueAt = next
		s.service.mu.Unlock()
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.runNight(ctx)
	}
}

// runNight performs the window strictly in order. Each step is bounded and a
// failed step does not skip the later ones: geodata still updates when the
// review could not run.
func (s *Schedule) runNight(ctx context.Context) {
	svc := s.service
	svc.mu.Lock()
	svc.maintenance = MaintenanceStatus{Phase: "subscriptions", StartedAt: time.Now().UTC()}
	svc.mu.Unlock()
	phase := func(name string) {
		svc.mu.Lock()
		svc.maintenance.Phase = name
		svc.mu.Unlock()
	}
	if s.Subscriptions != nil {
		s.Subscriptions(ctx)
	}
	phase("settle")
	s.waitNativeReady(ctx)
	if !sleepCtx(ctx, s.settle) {
		return
	}
	phase("review")
	var reason string
	started := false
	for attempt := 0; attempt < reviewStartAttempts && ctx.Err() == nil; attempt++ {
		err := svc.startSweep(ctx, "nightly")
		if err == nil {
			started = true
			svc.waitReview(ctx)
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
		if !sleepCtx(ctx, busyRetry) {
			return
		}
	}
	svc.mu.Lock()
	svc.maintenance.ReviewStarted = started
	svc.maintenance.ReviewReason = reason
	svc.mu.Unlock()
	phase("geodata")
	geodata := "skipped"
	if s.Geodata != nil {
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
	svc.mu.Unlock()
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
		if !sleepCtx(ctx, busyRetry) {
			return "canceled", ctx.Err()
		}
	}
	if err != nil {
		return "refused", err
	}
	wait, cancel := context.WithTimeout(ctx, geodataWait)
	defer cancel()
	tick := time.NewTicker(2 * time.Second)
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
