package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/notifications"
	"github.com/popiposter/xkeen-control/internal/release"
)

type Discovery struct {
	Version  string
	Identity string
	Newer    bool
}

// DiscoverStable is independent of the explicit Check owner. It verifies only
// the signed manifest, never downloads artifact bodies and never reads/writes
// the checked candidate, lastCheck or lastResult used by ApplyChecked.
func (m *Manager) DiscoverStable(ctx context.Context) (Discovery, error) {
	manifest, err := m.client.Check(ctx, "stable", "")
	if err != nil {
		return Discovery{}, errors.New("discovery-failed")
	}
	// Current signed release manifests declare supported state schema zero.
	c := manifest.Compatibility
	if c.StateSchemaMin != 0 || c.ManualMigrationRequired || !c.RollbackCompatible {
		return Discovery{}, errors.New("incompatible")
	}
	data, err := manifest.MarshalDeterministic()
	if err != nil {
		return Discovery{}, errors.New("discovery-failed")
	}
	digest := sha256.Sum256(data)
	return Discovery{Version: manifest.Version, Identity: hex.EncodeToString(digest[:]), Newer: newerStable(manifest.Version, m.current.Version)}, nil
}

func newerStable(candidate, installed string) bool {
	prerelease := func(v string) bool { return strings.Contains(strings.SplitN(v, "+", 2)[0], "-") }
	if release.ValidateVersion(candidate) != nil || release.ValidateVersion(installed) != nil || prerelease(candidate) {
		return false
	}
	clean := func(v string) []string {
		v = strings.TrimPrefix(v, "v")
		v = strings.SplitN(v, "+", 2)[0]
		v = strings.SplitN(v, "-", 2)[0]
		return strings.Split(v, ".")
	}
	a, b := clean(candidate), clean(installed)
	for i := 0; i < 3; i++ {
		left, _ := new(big.Int).SetString(a[i], 10)
		right, _ := new(big.Int).SetString(b[i], 10)
		if comparison := left.Cmp(right); comparison != 0 {
			return comparison > 0
		}
	}
	return prerelease(installed)
}

type NotifyStatus struct {
	State             string     `json:"state"`
	NotificationState string     `json:"notificationState"`
	NextDueAt         *time.Time `json:"nextDueAt,omitempty"`
	LastCheckAt       *time.Time `json:"lastCheckAt,omitempty"`
	LastCycleAt       *time.Time `json:"lastCycleAt,omitempty"`
	LastSkipReason    string     `json:"lastSkipReason,omitempty"`
	ErrorCode         string     `json:"errorCode,omitempty"`
}

func notifyPolicyStatus(policy Policy) NotifyStatus {
	state := "disabled"
	if policy.Mode == "auto-stable" {
		state = "unsupported-mode"
	} else if policy.Mode == "notify" {
		if policy.Channel == "stable" {
			state = "waiting"
		} else {
			state = "unsupported-channel"
		}
	}
	return NotifyStatus{State: state, NotificationState: "idle"}
}

type NotifySchedulerConfig struct {
	Manager   *Manager
	Lifecycle func() (maintenance, applying, available bool)
	Send      func(context.Context, notifications.Alert) error
}
type NotifyScheduler struct {
	manager   *Manager
	lifecycle func() (bool, bool, bool)
	send      func(context.Context, notifications.Alert) error
	mu        sync.Mutex
	policy    Policy
	revision  uint64
	due       time.Time
	notified  string
	status    NotifyStatus
	running   bool
	cancel    context.CancelFunc
	done      chan struct{}

	beforeNotificationAdmission func() // In-package concurrency test seam; nil in production.
}

func NewNotifyScheduler(config NotifySchedulerConfig) *NotifyScheduler {
	s := &NotifyScheduler{manager: config.Manager, lifecycle: config.Lifecycle, send: config.Send, status: NotifyStatus{State: "disabled", NotificationState: "idle"}}
	if config.Manager != nil {
		config.Manager.mu.Lock()
		config.Manager.notify = s
		config.Manager.mu.Unlock()
	}
	return s
}
func (s *NotifyScheduler) statusFor(policy Policy) NotifyStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if notifyPolicyStatus(policy).State != "waiting" || s.policy != policy {
		return notifyPolicyStatus(policy)
	}
	status := s.status
	if status.NextDueAt != nil {
		v := *status.NextDueAt
		status.NextDueAt = &v
	}
	if status.LastCheckAt != nil {
		v := *status.LastCheckAt
		status.LastCheckAt = &v
	}
	if status.LastCycleAt != nil {
		v := *status.LastCycleAt
		status.LastCycleAt = &v
	}
	return status
}
func (s *NotifyScheduler) policySnapshot() (Policy, uint64) {
	s.manager.policyMu.Lock()
	defer s.manager.policyMu.Unlock()
	policy := s.manager.readPolicy()
	revision := s.manager.policyRevision
	return policy, revision
}

// startNotifyDelivery admits and launches one delivery atomically with policy
// mutation. The policy mutex protects only this short admission boundary, not
// the network work or its result, and does not acquire lifecycle/global owners.
func (m *Manager) startNotifyDelivery(ctx context.Context, policy Policy, revision uint64, send func(context.Context, notifications.Alert) error, alert notifications.Alert) <-chan error {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	if policy.Mode != "notify" || policy.Channel != "stable" || m.readPolicy() != policy || m.policyRevision != revision || ctx.Err() != nil {
		return nil
	}
	result := make(chan error, 1)
	go func() { result <- send(ctx, alert) }()
	return result
}
func (s *NotifyScheduler) Start(parent context.Context) {
	if s == nil || s.manager == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()
	go func() {
		defer close(s.done)
		// A bounded read-only rescan observes external policy changes; no catch-up.
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		s.step(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.manager.policyChanges:
				s.step(ctx)
			case <-ticker.C:
				s.step(ctx)
			}
		}
	}()
}
func (s *NotifyScheduler) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func (s *NotifyScheduler) step(ctx context.Context) {
	policy, revision := s.policySnapshot()
	now := s.manager.now().UTC()
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	if s.policy != policy || s.revision != revision {
		s.policy, s.revision = policy, revision
		s.due = time.Time{}
		s.status = notifyPolicyStatus(policy)
		if s.status.State == "waiting" {
			s.due = now.Add(time.Duration(policy.CheckCadenceMinutes) * time.Minute)
			due := s.due
			s.status.NextDueAt = &due
		}
		s.mu.Unlock()
		return
	}
	if s.status.State == "unsupported-mode" || s.status.State == "unsupported-channel" || s.due.IsZero() || now.Before(s.due) {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.status.State = "running"
	s.status.NextDueAt = nil
	s.mu.Unlock()
	result := NotifyStatus{State: "completed", NotificationState: "idle", LastCycleAt: &now}
	defer func() {
		completed := s.manager.now().UTC()
		s.mu.Lock()
		s.running = false
		s.due = completed.Add(time.Duration(policy.CheckCadenceMinutes) * time.Minute)
		due := s.due
		result.NextDueAt = &due
		s.status = result
		s.mu.Unlock()
	}()
	if s.lifecycle == nil {
		result.State = "skipped"
		result.LastSkipReason = "lifecycle-unavailable"
		return
	}
	maintenance, applying, available := s.lifecycle()
	if !available || maintenance || applying {
		result.State = "skipped"
		result.LastSkipReason = "lifecycle-unavailable"
		if maintenance {
			result.LastSkipReason = "maintenance"
		} else if applying {
			result.LastSkipReason = "applying"
		}
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	discovery, err := s.manager.DiscoverStable(checkCtx)
	checkedAt := s.manager.now().UTC()
	result.LastCheckAt = &checkedAt
	if err != nil {
		result.State = "failed"
		result.ErrorCode = "discovery-failed"
		return
	}
	currentPolicy, currentRevision := s.policySnapshot()
	if currentPolicy != policy || currentRevision != revision {
		result.State = "skipped"
		result.LastSkipReason = "policy-changed"
		return
	}
	maintenance, applying, available = s.lifecycle()
	if !available || maintenance || applying {
		result.State = "skipped"
		result.LastSkipReason = "lifecycle-changed"
		return
	}
	if !discovery.Newer {
		s.mu.Lock()
		s.notified = ""
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	duplicate := s.notified == discovery.Identity
	if !duplicate {
		s.notified = ""
	}
	s.mu.Unlock()
	if duplicate {
		result.NotificationState = "notified"
		return
	}
	if s.send == nil {
		result.NotificationState = "unconfigured"
		return
	}
	if s.beforeNotificationAdmission != nil {
		s.beforeNotificationAdmission()
	}
	deliveryCtx, cancelDelivery := context.WithTimeout(checkCtx, 5*time.Second)
	defer cancelDelivery()
	delivery := s.manager.startNotifyDelivery(deliveryCtx, policy, revision, s.send, notifications.PanelAlert(discovery.Version, discovery.Identity, now))
	if delivery == nil {
		result.State = "skipped"
		result.LastSkipReason = "policy-changed"
		return
	}
	select {
	case err = <-delivery:
	case <-deliveryCtx.Done():
		err = deliveryCtx.Err()
	}
	if err != nil {
		result.NotificationState = "failed"
		result.ErrorCode = "delivery-failed"
		if state, ok := err.(interface{ NotificationState() string }); ok {
			switch state.NotificationState() {
			case "unconfigured", "disabled":
				result.NotificationState = state.NotificationState()
				result.ErrorCode = ""
			}
		}
		return
	}
	s.mu.Lock()
	s.notified = discovery.Identity
	s.mu.Unlock()
	result.NotificationState = "notified"
}
