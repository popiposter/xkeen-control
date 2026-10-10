package c1

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrLifecycleBusy = errors.New("runtime lifecycle is busy")

// NodeReader returns the current registry projection used to resolve targets.
type NodeReader func(context.Context) []NodeState

type Status struct {
	Lifecycle *LifecycleStatus `json:"lifecycle,omitempty"`
}

// LifecycleStatus is the compact read-only projection used by the UI. It is
// deliberately derived under Coordinator.mu rather than becoming another
// operation owner. Applying includes admitted waiters and the active lifecycle
// mutation; a performance measurement is intentionally not applying.
type LifecycleStatus struct {
	Maintenance bool `json:"maintenance"`
	Applying    bool `json:"applying"`
}

// Coordinator owns the single panel lifecycle token shared by node/config
// Apply, startup recovery, the manual one-node diagnostic and native quality
// measurement. It starts no background work of its own.
type Coordinator struct {
	policy         Policy
	manualRunner   *ManualNodeRunner
	adaptiveRunner *AdaptiveRunner
	nodes          NodeReader
	lifecycle      chan struct{}

	// evidence keeps RAM-only Observatory RTT history for native quality
	// review; evidenceMu guards it separately from the lifecycle state.
	evidenceMu sync.Mutex
	evidence   *PolicyEngine

	mu sync.Mutex
	// benchmarkCancel/Done are the shared performance owner for the manual
	// diagnostic and native quality measurement.
	benchmarkCancel context.CancelFunc
	benchmarkDone   chan struct{}
	performanceMode string
	manual          ManualPerformanceStatus
	adaptive        AdaptivePerformanceStatus
	applyWaiters    int
	applyActive     bool
	// maintenance is set when an interrupted transaction cannot yet prove
	// recovery. It is deliberately process-wide for every lifecycle mutation;
	// only BeginRecovery may enter while it is set.
	maintenance bool
	// Test-only synchronization point used to force the Apply admission
	// interleaving covered by coordinator concurrency regressions.
	beforeApplyAcquire func()
	clock              func() time.Time
}

func NewCoordinator(policy Policy, nodes NodeReader) *Coordinator {
	policy = policy.normalized()
	c := &Coordinator{policy: policy, nodes: nodes, lifecycle: make(chan struct{}, 1), evidence: NewPolicyEngine(policy), clock: func() time.Time { return time.Now().UTC() }}
	c.lifecycle <- struct{}{}
	c.manual = idleManualPerformanceStatus()
	c.adaptive = idleAdaptivePerformanceStatus()
	return c
}

// SetManualRunner installs the fixed one-node diagnostic implementation. It
// is called during process wiring; the Coordinator remains the sole owner of
// admission, cancellation, progress and completion state.
func (c *Coordinator) SetManualRunner(runner *ManualNodeRunner) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.manualRunner = runner
	if c.manual.State == "idle" {
		limits := runner.Limits()
		c.manual.BytesPlanned = limits.Bytes
		c.manual.PlannedStages = ManualLatencySamples + len(limits.Download) + len(limits.Upload)
		c.manual.MaxWallSeconds = limits.Seconds
	}
	c.mu.Unlock()
}

// SetAdaptiveRunner installs the fixed adaptive implementation. It is a
// process-wiring seam for offline synthetic tests; the Coordinator retains
// sole ownership of admission, cancellation and status.
func (c *Coordinator) SetAdaptiveRunner(runner *AdaptiveRunner) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.adaptiveRunner = runner
	c.mu.Unlock()
}

// SetClock is a test seam for the process-local adaptive scheduler and status
// decisions. It does not create a configurable runtime cadence.
func (c *Coordinator) SetClock(clock func() time.Time) {
	if c == nil || clock == nil {
		return
	}
	c.mu.Lock()
	c.clock = clock
	c.mu.Unlock()
}

func (c *Coordinator) now() time.Time {
	if c == nil {
		return time.Now().UTC()
	}
	c.mu.Lock()
	clock := c.clock
	c.mu.Unlock()
	if clock == nil {
		return time.Now().UTC()
	}
	return clock().UTC()
}

// Stop cancels an active manual diagnostic or quality measurement and waits a
// bounded time for its cleanup.
func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	cancel := c.benchmarkCancel
	done := c.benchmarkDone
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// TriggerManualNode admits one safe node ID into the same performance
// single-flight used by native quality measurement. Target resolution happens
// after that shared ownership is acquired, so an Apply cannot mutate the
// authoritative registry between selection validation and the diagnostic.
func (c *Coordinator) TriggerManualNode(nodeID string) error {
	if c == nil {
		return ErrManualUnavailable
	}
	if !validManualNodeID(nodeID) {
		return ErrManualInvalidTarget
	}
	c.mu.Lock()
	if c.manualRunner != nil {
		probe := c.manualRunner.Probe
		c.mu.Unlock()
		// Retry a closed probe gate before refusing; nothing else would.
		probe.Recover(context.Background())
		c.mu.Lock()
	}
	runner := c.manualRunner
	if runner == nil || !c.policy.Enabled {
		c.mu.Unlock()
		return ErrManualUnavailable
	}
	if runner.Probe == nil {
		c.mu.Unlock()
		return ErrManualUnavailable
	}
	if runner.Probe.Blocked() {
		c.mu.Unlock()
		return ErrManualCleanupPending
	}
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil {
		c.mu.Unlock()
		return ErrManualBusy
	}
	select {
	case token := <-c.lifecycle:
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		started := time.Now().UTC()
		c.benchmarkCancel, c.benchmarkDone = cancel, done
		c.performanceMode = ManualMode
		c.manual = ManualPerformanceStatus{
			Mode:          ManualMode,
			State:         "running",
			Phase:         "latency",
			TargetNodeID:  nodeID,
			StartedAt:     started,
			PlannedStages: ManualLatencySamples + len(runner.Limits().Download) + len(runner.Limits().Upload),
			BytesPlanned:  runner.Limits().Bytes,
		}
		c.mu.Unlock()

		node, ok := c.resolveManualNode(context.Background(), nodeID)
		if !ok {
			c.finishManualAdmission(cancel, done, token, ManualPerformanceStatus{
				Mode:          ManualMode,
				State:         "failed",
				Phase:         "done",
				TargetNodeID:  nodeID,
				StartedAt:     started,
				ElapsedMS:     elapsedMilliseconds(started, time.Now().UTC()),
				PlannedStages: ManualLatencySamples + len(runner.Limits().Download) + len(runner.Limits().Upload),
				BytesPlanned:  runner.Limits().Bytes,
				ErrorCode:     "invalid-target",
			})
			return ErrManualInvalidTarget
		}

		c.mu.Lock()
		c.manual.TargetTag = node.Tag
		c.mu.Unlock()
		go c.runManual(ctx, done, token, runner, node)
		return nil
	default:
		c.mu.Unlock()
		return ErrManualBusy
	}
}

func (c *Coordinator) resolveManualNode(ctx context.Context, nodeID string) (NodeState, bool) {
	if c == nil || c.nodes == nil {
		return NodeState{}, false
	}
	var result NodeState
	found := 0
	for _, node := range c.nodes(ctx) {
		if node.ID != nodeID {
			continue
		}
		found++
		result = node
	}
	return result, found == 1 && validManualNode(result)
}

func (c *Coordinator) runManual(ctx context.Context, done chan struct{}, token struct{}, runner *ManualNodeRunner, node NodeState) {
	defer func() {
		c.mu.Lock()
		if c.benchmarkDone == done {
			c.benchmarkCancel = nil
			c.benchmarkDone = nil
			c.performanceMode = ""
		}
		c.mu.Unlock()
		close(done)
		c.lifecycle <- token
	}()
	status := runner.Run(ctx, node, func(progress ManualPerformanceStatus) {
		c.mu.Lock()
		c.manual = progress
		c.mu.Unlock()
	})
	c.mu.Lock()
	c.manual = status
	c.mu.Unlock()
}

func (c *Coordinator) finishManualAdmission(cancel context.CancelFunc, done chan struct{}, token struct{}, status ManualPerformanceStatus) {
	c.mu.Lock()
	c.manual = status
	if c.benchmarkDone == done {
		c.benchmarkCancel = nil
		c.benchmarkDone = nil
		c.performanceMode = ""
	}
	c.mu.Unlock()
	cancel()
	close(done)
	c.lifecycle <- token
}

// BeginApply gives an explicit operator mutation priority over managed runtime
// work. It prevents new performance/supervisor work from starting, cancels and
// drains any active benchmark or manual diagnostic and active supervisor
// operation (including probe cleanup), then holds the lifecycle token across
// the node transaction.
func (c *Coordinator) BeginApply(ctx context.Context) (func(), error) {
	return c.beginApply(ctx, false)
}

// BeginRecovery admits the bounded startup recovery path while maintenance is
// active. It is intentionally separate from BeginApply so an unresolved
// journal cannot be bypassed by an ordinary lifecycle mutation.
func (c *Coordinator) BeginRecovery(ctx context.Context) (func(), error) {
	return c.beginApply(ctx, true)
}

// TryBeginManagedApply admits one bounded background mutation only when the
// runtime is completely idle. Unlike BeginApply, this admission is
// deliberately non-preemptive: it never marks an operator waiter, cancels
// benchmark work or drains a supervisor operation. The returned release owns
// the same lifecycle token as an explicit Apply until the caller finishes its
// transaction or recovery.
func (c *Coordinator) TryBeginManagedApply() (func(), error) {
	if c == nil {
		return nil, ErrLifecycleBusy
	}
	c.mu.Lock()
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil {
		c.mu.Unlock()
		return nil, ErrLifecycleBusy
	}
	select {
	case token := <-c.lifecycle:
		// Keep the state transition under the same mutex as all other lifecycle
		// admissions. Once applyActive is visible, an operator may wait for
		// this bounded transaction but no managed operation can start beside it.
		c.applyActive = true
		c.mu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				c.mu.Lock()
				c.applyActive = false
				c.mu.Unlock()
				c.lifecycle <- token
			})
		}, nil
	default:
		c.mu.Unlock()
		return nil, ErrLifecycleBusy
	}
}

// EnterMaintenance makes the retained-journal boundary fail closed for every
// normal lifecycle mutation until a recovery path proves the journal resolved.
func (c *Coordinator) EnterMaintenance() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.maintenance = true
	c.mu.Unlock()
}

// ExitMaintenance reopens normal lifecycle operations after durable recovery.
func (c *Coordinator) ExitMaintenance() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.maintenance = false
	c.mu.Unlock()
}

func (c *Coordinator) beginApply(ctx context.Context, recovery bool) (func(), error) {
	if c == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.maintenance && !recovery {
		c.mu.Unlock()
		return nil, ErrLifecycleBusy
	}
	// Mark Apply as pending before observing/cancelling managed work. This is
	// the admission point that closes both Apply-vs-benchmark and
	// Apply-vs-supervisor races.
	c.applyWaiters++
	cancel, done := c.benchmarkCancel, c.benchmarkDone
	hook := c.beforeApplyAcquire
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	clearPending := func() {
		c.mu.Lock()
		if c.applyWaiters > 0 {
			c.applyWaiters--
		}
		c.mu.Unlock()
	}
	if cancel != nil {
		cancel()
	}
	if err := waitForManagedWork(ctx, done); err != nil {
		clearPending()
		return nil, err
	}
	// An explicit lifecycle mutation invalidates transient RTT evidence, so
	// clear it before the transaction starts.
	c.resetEvidence()
	select {
	case token := <-c.lifecycle:
		c.mu.Lock()
		if c.maintenance && !recovery {
			c.mu.Unlock()
			c.lifecycle <- token
			clearPending()
			return nil, ErrLifecycleBusy
		}
		c.applyWaiters--
		c.applyActive = true
		c.mu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				c.mu.Lock()
				c.applyActive = false
				c.mu.Unlock()
				c.lifecycle <- token
			})
		}, nil
	case <-ctx.Done():
		clearPending()
		return nil, ctx.Err()
	}
}

func waitForManagedWork(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Coordinator) IsLifecycleBusy() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	pending := c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil
	c.mu.Unlock()
	if pending {
		return true
	}
	select {
	case token := <-c.lifecycle:
		c.lifecycle <- token
		return false
	default:
		return true
	}
}

func (c *Coordinator) Snapshot() Status {
	if c == nil {
		return Status{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{Lifecycle: &LifecycleStatus{Maintenance: c.maintenance, Applying: c.applyWaiters > 0 || c.applyActive}}
}

// ManualSnapshot returns the current or last manual diagnostic without reading
// Xray, XKeen or any persistent state. It is the cheap overlay used by the
// performance status route while a diagnostic is active.
func (c *Coordinator) ManualSnapshot() ManualPerformanceStatus {
	if c == nil {
		return idleManualPerformanceStatus()
	}
	c.mu.Lock()
	result := c.manual
	if result.State == "running" {
		result.ElapsedMS = elapsedMilliseconds(result.StartedAt, time.Now().UTC())
	}
	c.mu.Unlock()
	return result
}

// AdaptiveSnapshot returns only bounded Coordinator-owned RAM state. It never
// reads Xray, the node registry or the persisted legacy benchmark snapshot.
func (c *Coordinator) AdaptiveSnapshot() AdaptivePerformanceStatus {
	if c == nil {
		return idleAdaptivePerformanceStatus()
	}
	c.mu.Lock()
	result := sanitizeAdaptiveStatus(c.adaptive)
	c.mu.Unlock()
	return result
}
