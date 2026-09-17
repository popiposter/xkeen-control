package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	// These are fixed v1 scheduler policy values. They are constants rather
	// than registry fields so an operator cannot create a per-subscription
	// refresh policy or a persistent scheduler configuration surface.
	DefaultSubscriptionRefreshCadence     = 6 * time.Hour
	DefaultSubscriptionRefreshStartupWait = 5 * time.Minute
	DefaultSubscriptionRefreshJitter      = 5 * time.Minute
	DefaultSubscriptionRefreshRescan      = 5 * time.Minute
)

var subscriptionRefreshBusyBackoff = [...]time.Duration{
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
}

const (
	autoRefreshWaiting    = "waiting"
	autoRefreshRunning    = "running"
	autoRefreshDeferred   = "deferred"
	autoRefreshFailed     = "failed"
	autoRefreshDisabled   = "disabled"
	autoRefreshUpdated    = "updated"
	autoRefreshNoop       = "noop"
	autoRefreshRuntime    = "runtime-busy"
	autoRefreshAuthority  = "authority-busy"
	autoRefreshStale      = "stale"
	autoRefreshFetch      = "fetch-failed"
	autoRefreshContent    = "content-rejected"
	autoRefreshDuplicate  = "duplicate"
	autoRefreshNode       = "node-rejected"
	autoRefreshCandidate  = "candidate-invalid"
	autoRefreshActivation = "activation-failed"
	autoRefreshRegistry   = "registry-unavailable"
)

// AutoRefreshStatus is the bounded, RAM-only subscription status exposed by
// the authenticated Nodes projection. It intentionally contains no provider
// URL, upstream error, registry material or scheduler history.
type AutoRefreshStatus struct {
	State         string `json:"state"`
	LastAttemptAt string `json:"lastAttemptAt,omitempty"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	NextRunAt     string `json:"nextRunAt,omitempty"`
	LastResult    string `json:"lastResult,omitempty"`
	ErrorCode     string `json:"errorCode,omitempty"`
}

type automaticRefreshResult struct {
	LastResult string
}

type automaticRefreshError struct {
	code      string
	retryable bool
	disabled  bool
}

func (e *automaticRefreshError) Error() string {
	if e == nil {
		return "automatic subscription refresh failed"
	}
	return e.code
}

func automaticError(code string, retryable, disabled bool) error {
	return &automaticRefreshError{code: code, retryable: retryable, disabled: disabled}
}

type subscriptionRefreshStatus struct {
	state         string
	lastAttemptAt time.Time
	lastSuccessAt time.Time
	lastResult    string
	errorCode     string
}

func (s subscriptionRefreshStatus) project(nextRunAt time.Time) AutoRefreshStatus {
	return AutoRefreshStatus{
		State:         s.state,
		LastAttemptAt: formatAutoRefreshTime(s.lastAttemptAt),
		LastSuccessAt: formatAutoRefreshTime(s.lastSuccessAt),
		NextRunAt:     formatAutoRefreshTime(nextRunAt),
		LastResult:    s.lastResult,
		ErrorCode:     s.errorCode,
	}
}

type subscriptionRefreshEntry struct {
	nextRunAt time.Time
	retry     int
	status    subscriptionRefreshStatus
}

// SubscriptionRefresher is the purpose-built in-process scheduler for saved
// subscriptions. It owns no durable state and runs at most one provider
// attempt at a time.
type SubscriptionRefresher struct {
	manager *Manager
	now     func() time.Time

	mu          sync.Mutex
	entries     map[string]*subscriptionRefreshEntry
	rescanAt    time.Time
	initialized bool
	started     bool
	stop        context.CancelFunc
	wait        sync.WaitGroup
}

func NewSubscriptionRefresher(manager *Manager) *SubscriptionRefresher {
	return newSubscriptionRefresher(manager, func() time.Time { return time.Now().UTC() })
}

// newSubscriptionRefresher is a clock-injection seam for deterministic
// scheduler tests. Production policy remains fixed by the constants above.
func newSubscriptionRefresher(manager *Manager, now func() time.Time) *SubscriptionRefresher {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SubscriptionRefresher{manager: manager, now: now, entries: make(map[string]*subscriptionRefreshEntry)}
}

// Start performs only the initial bounded registry rescan. It does not fetch
// a provider until the startup wait and deterministic ID-derived jitter elapse.
func (r *SubscriptionRefresher) Start(parent context.Context) {
	if r == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		cancel()
		return
	}
	r.started = true
	r.stop = cancel
	r.mu.Unlock()
	r.wait.Add(1)
	go func() {
		defer r.wait.Done()
		r.loop(ctx)
	}()
}

// Stop cancels and drains the in-flight fetch/transaction before returning.
// The caller must stop the refresher before stopping the shared Coordinator.
func (r *SubscriptionRefresher) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	stop := r.stop
	r.mu.Unlock()
	if stop != nil {
		stop()
	}
	r.wait.Wait()
}

// AutoRefreshStatuses returns a copy of the safe RAM-only status map for the
// Manager's existing subscription projection.
func (r *SubscriptionRefresher) AutoRefreshStatuses() map[string]AutoRefreshStatus {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]AutoRefreshStatus, len(r.entries))
	for id, entry := range r.entries {
		result[id] = entry.status.project(entry.nextRunAt)
	}
	return result
}

func (r *SubscriptionRefresher) loop(ctx context.Context) {
	_ = r.reconcile(r.clock())
	for {
		now := r.clock()
		r.mu.Lock()
		scanDue := r.rescanAt.IsZero() || !now.Before(r.rescanAt)
		id := ""
		if !scanDue {
			id = r.nextDueLocked(now)
		}
		next := r.nextWakeLocked(now)
		r.mu.Unlock()

		if scanDue {
			_ = r.reconcile(now)
			continue
		}
		if id != "" {
			runContext := ctx
			r.runAttempt(runContext, id)
			if ctx.Err() != nil {
				return
			}
			continue
		}

		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

func (r *SubscriptionRefresher) clock() time.Time {
	if r == nil || r.now == nil {
		return time.Now().UTC()
	}
	now := r.now()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now
}

func (r *SubscriptionRefresher) reconcile(now time.Time) error {
	if r == nil {
		return nil
	}
	now = normalizeAutoRefreshTime(now)
	r.mu.Lock()
	initial := !r.initialized
	r.initialized = true
	r.mu.Unlock()

	if r.manager == nil {
		r.mu.Lock()
		r.rescanAt = now.Add(DefaultSubscriptionRefreshRescan)
		r.mu.Unlock()
		return automaticError(autoRefreshRegistry, false, false)
	}
	registry, err := r.manager.current()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rescanAt = now.Add(DefaultSubscriptionRefreshRescan)
	if err != nil {
		return automaticError(autoRefreshRegistry, false, false)
	}

	seen := make(map[string]struct{}, len(registry.Subscriptions))
	for _, subscription := range registry.Subscriptions {
		seen[subscription.ID] = struct{}{}
		entry, exists := r.entries[subscription.ID]
		if !exists {
			entry = &subscriptionRefreshEntry{}
			r.entries[subscription.ID] = entry
			if subscription.Enabled {
				entry.nextRunAt = r.firstDue(now, subscription.ID, initial)
				entry.status.state = autoRefreshWaiting
			} else {
				entry.status.state = autoRefreshDisabled
			}
			continue
		}
		if !subscription.Enabled {
			entry.nextRunAt = time.Time{}
			entry.retry = 0
			entry.status.state = autoRefreshDisabled
			entry.status.errorCode = ""
			continue
		}
		if entry.status.state == autoRefreshDisabled {
			entry.nextRunAt = r.firstDue(now, subscription.ID, true)
			entry.retry = 0
			entry.status.state = autoRefreshWaiting
			entry.status.errorCode = ""
		}
	}
	for id := range r.entries {
		if _, exists := seen[id]; !exists {
			delete(r.entries, id)
		}
	}
	return nil
}

func (r *SubscriptionRefresher) firstDue(now time.Time, id string, startup bool) time.Time {
	if startup {
		return now.Add(DefaultSubscriptionRefreshStartupWait + subscriptionRefreshJitter(id))
	}
	return now.Add(DefaultSubscriptionRefreshCadence)
}

func subscriptionRefreshJitter(id string) time.Duration {
	if DefaultSubscriptionRefreshJitter <= 0 {
		return 0
	}
	digest := sha256.Sum256([]byte(id))
	value := binary.BigEndian.Uint64(digest[:8])
	return time.Duration(value % uint64(DefaultSubscriptionRefreshJitter))
}

func (r *SubscriptionRefresher) nextDueLocked(now time.Time) string {
	ids := make([]string, 0, len(r.entries))
	for id, entry := range r.entries {
		if entry.status.state == autoRefreshDisabled || entry.nextRunAt.IsZero() || now.Before(entry.nextRunAt) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func (r *SubscriptionRefresher) nextWakeLocked(now time.Time) time.Time {
	next := r.rescanAt
	if next.IsZero() {
		next = now
	}
	for _, entry := range r.entries {
		if entry.nextRunAt.IsZero() || (entry.status.state == autoRefreshDisabled) {
			continue
		}
		if next.IsZero() || entry.nextRunAt.Before(next) {
			next = entry.nextRunAt
		}
	}
	return next
}

func (r *SubscriptionRefresher) runAttempt(ctx context.Context, id string) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	started := r.clock()
	r.mu.Lock()
	entry, exists := r.entries[id]
	if !exists || entry.status.state == autoRefreshDisabled {
		r.mu.Unlock()
		return
	}
	entry.status.state = autoRefreshRunning
	entry.status.lastAttemptAt = started
	entry.status.errorCode = ""
	entry.nextRunAt = time.Time{}
	r.mu.Unlock()

	result, err := r.manager.refreshSavedSubscription(ctx, id)
	if ctx.Err() != nil {
		r.finishCanceled(id, r.clock())
		return
	}
	r.finish(id, r.clock(), result, err)
}

func (r *SubscriptionRefresher) finishCanceled(id string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.entries[id]
	if !exists {
		return
	}
	entry.retry = 0
	entry.nextRunAt = now.Add(DefaultSubscriptionRefreshCadence)
	entry.status.state = autoRefreshWaiting
	entry.status.errorCode = ""
}

func (r *SubscriptionRefresher) finish(id string, now time.Time, result automaticRefreshResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.entries[id]
	if !exists {
		return
	}
	if err == nil {
		entry.retry = 0
		entry.nextRunAt = now.Add(DefaultSubscriptionRefreshCadence)
		entry.status.state = autoRefreshWaiting
		entry.status.lastSuccessAt = now
		entry.status.lastResult = result.LastResult
		entry.status.errorCode = ""
		return
	}
	var automaticErr *automaticRefreshError
	if !errors.As(err, &automaticErr) {
		automaticErr = &automaticRefreshError{code: autoRefreshRegistry}
	}
	if automaticErr.disabled {
		entry.retry = 0
		entry.nextRunAt = time.Time{}
		entry.status.state = autoRefreshDisabled
		entry.status.errorCode = ""
		return
	}
	entry.status.errorCode = automaticErr.code
	if automaticErr.retryable && entry.retry < len(subscriptionRefreshBusyBackoff) {
		entry.nextRunAt = now.Add(subscriptionRefreshBusyBackoff[entry.retry])
		entry.retry++
		entry.status.state = autoRefreshDeferred
		return
	}
	entry.retry = 0
	entry.nextRunAt = now.Add(DefaultSubscriptionRefreshCadence)
	entry.status.state = autoRefreshFailed
}

func formatAutoRefreshTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func normalizeAutoRefreshTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value
}

func (m *Manager) refreshSavedSubscription(ctx context.Context, subscriptionID string) (automaticRefreshResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	before, target, err := m.savedEnabledSubscription(subscriptionID)
	if err != nil {
		return automaticRefreshResult{}, err
	}
	baseDigest := registryDigest(before)
	if err := validateSubscriptionURL(target.URL); err != nil {
		return automaticRefreshResult{}, automaticError(autoRefreshFetch, false, false)
	}
	if m.fetcher == nil {
		return automaticRefreshResult{}, automaticError(autoRefreshFetch, false, false)
	}
	body, err := m.fetcher.Fetch(ctx, target.URL)
	if err != nil {
		if ctx.Err() != nil {
			return automaticRefreshResult{}, ctx.Err()
		}
		return automaticRefreshResult{}, automaticError(autoRefreshFetch, false, false)
	}
	parsed, err := ParseSubscriptionBody(body)
	if err != nil {
		return automaticRefreshResult{}, automaticError(autoRefreshContent, false, false)
	}
	candidate, err := buildSubscriptionCandidate(before, target, parsed)
	if err != nil {
		return automaticRefreshResult{}, automaticCandidateError(err)
	}

	current, currentTarget, err := m.savedEnabledSubscription(subscriptionID)
	if err != nil {
		return automaticRefreshResult{}, err
	}
	if registryDigest(current) != baseDigest || currentTarget != target {
		return automaticRefreshResult{}, automaticError(autoRefreshStale, true, false)
	}
	if sameRegistry(before, candidate) {
		return automaticRefreshResult{LastResult: autoRefreshNoop}, nil
	}

	if m.managedCoordinator == nil {
		return automaticRefreshResult{}, automaticError(autoRefreshRuntime, true, false)
	}
	releaseCoordinator, err := m.managedCoordinator.TryBeginManagedApply()
	if err != nil {
		return automaticRefreshResult{}, automaticError(autoRefreshRuntime, true, false)
	}
	defer releaseCoordinator()
	if m.authority == nil {
		return automaticRefreshResult{}, automaticError(autoRefreshAuthority, true, false)
	}
	releaseAuthority, err := m.authority.TryAcquire()
	if err != nil {
		return automaticRefreshResult{}, automaticError(autoRefreshAuthority, true, false)
	}
	defer releaseAuthority()

	current, currentTarget, err = m.savedEnabledSubscription(subscriptionID)
	if err != nil {
		return automaticRefreshResult{}, err
	}
	if registryDigest(current) != baseDigest || currentTarget != target {
		return automaticRefreshResult{}, automaticError(autoRefreshStale, true, false)
	}
	applyContext, cancelApply := context.WithTimeout(ctx, m.tx.totalTimeout())
	defer cancelApply()
	if err := m.tx.Apply(applyContext, candidate); err != nil {
		if ctx.Err() != nil {
			return automaticRefreshResult{}, ctx.Err()
		}
		return automaticRefreshResult{}, automaticError(autoRefreshActivation, false, false)
	}
	return automaticRefreshResult{LastResult: autoRefreshUpdated}, nil
}

func (m *Manager) savedEnabledSubscription(subscriptionID string) (Registry, Subscription, error) {
	if m == nil || !validSubscriptionID(subscriptionID) {
		return Registry{}, Subscription{}, automaticError(autoRefreshRegistry, false, false)
	}
	registry, err := m.current()
	if err != nil {
		return Registry{}, Subscription{}, automaticError(autoRefreshRegistry, false, false)
	}
	for _, subscription := range registry.Subscriptions {
		if subscription.ID != subscriptionID {
			continue
		}
		if !subscription.Enabled {
			return registry, subscription, automaticError(autoRefreshDisabled, false, true)
		}
		return registry, subscription, nil
	}
	return registry, Subscription{}, automaticError(autoRefreshDisabled, false, true)
}

func automaticCandidateError(err error) error {
	switch {
	case errors.Is(err, ErrSubscriptionDuplicate):
		return automaticError(autoRefreshDuplicate, false, false)
	case errors.Is(err, ErrSubscriptionNode):
		return automaticError(autoRefreshNode, false, false)
	case errors.Is(err, ErrSubscriptionContent):
		return automaticError(autoRefreshContent, false, false)
	default:
		return automaticError(autoRefreshCandidate, false, false)
	}
}
