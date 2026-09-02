package c1

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrBenchmarkBusy = errors.New("benchmark is already running or lifecycle is busy")
var ErrLifecycleBusy = errors.New("runtime lifecycle is busy")

type BenchmarkStatus struct {
	Enabled             bool                        `json:"enabled"`
	Running             bool                        `json:"running"`
	State               string                      `json:"state"`
	LastResult          string                      `json:"lastResult"`
	LastCompletedAt     time.Time                   `json:"lastCompletedAt"`
	LastEligibleNodes   int                         `json:"lastEligibleNodes"`
	LastValidSamples    int                         `json:"lastValidSamples"`
	LastAggregateBytes  int64                       `json:"lastAggregateBytes"`
	LastDurationMS      int64                       `json:"lastDurationMs"`
	PayloadBytes        int64                       `json:"payloadBytes"`
	TotalBudgetBytes    int64                       `json:"totalBudgetBytes"`
	MinimumPayloadBytes int64                       `json:"minimumPayloadBytes"`
	PerNodeTimeoutMS    int64                       `json:"perNodeTimeoutMs"`
	MaximumWallSeconds  int64                       `json:"maximumWallSeconds"`
	Schedule            string                      `json:"schedule"`
	NextRunAt           time.Time                   `json:"nextRunAt"`
	Generation          uint64                      `json:"generation"`
	CleanupPending      bool                        `json:"cleanupPending"`
	SwitchAllowed       bool                        `json:"switchAllowed"`
	Samples             map[string]ThroughputStatus `json:"samples,omitempty"`
}

type ThroughputStatus struct {
	Valid          bool    `json:"valid"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
}

type Status struct {
	Selection SelectionStatus `json:"selection"`
	Benchmark BenchmarkStatus `json:"benchmark"`
}

type Coordinator struct {
	policy         Policy
	supervisor     *Supervisor
	runner         *BenchmarkRunner
	nodes          NodeReader
	lifecycle      chan struct{}
	supervisorWake chan struct{}

	mu               sync.Mutex
	benchmarkCancel  context.CancelFunc
	benchmarkDone    chan struct{}
	supervisorCancel context.CancelFunc
	supervisorDone   chan struct{}
	benchmark        BenchmarkStatus
	applyWaiters     int
	applyActive      bool
	// maintenance is set when an interrupted appliance import cannot yet prove
	// recovery. It is deliberately process-wide for every lifecycle mutation;
	// only BeginRecovery may enter while it is set.
	maintenance bool
	// Test-only synchronization point used to force the Apply admission
	// interleaving covered by coordinator concurrency regressions.
	beforeApplyAcquire func()
	started            bool
	stop               context.CancelFunc
	wait               sync.WaitGroup
}

func NewCoordinator(policy Policy, supervisor *Supervisor, runner *BenchmarkRunner, nodes NodeReader) *Coordinator {
	policy = policy.normalized()
	c := &Coordinator{policy: policy, supervisor: supervisor, runner: runner, nodes: nodes, lifecycle: make(chan struct{}, 1), supervisorWake: make(chan struct{}, 1)}
	c.lifecycle <- struct{}{}
	c.benchmark = BenchmarkStatus{Enabled: policy.Enabled, State: "idle", Schedule: policy.Schedule, TotalBudgetBytes: policy.TotalBudgetBytes, MinimumPayloadBytes: policy.MinimumPayloadBytes, PerNodeTimeoutMS: policy.PerNodeTimeout.Milliseconds(), Samples: make(map[string]ThroughputStatus)}
	if runner != nil && runner.Store.Path != "" {
		if snapshot, err := runner.Store.Load(); err == nil && snapshot.ResultClass != "" {
			c.benchmark.LastResult = snapshot.ResultClass
			c.benchmark.State = snapshot.ResultClass
			c.benchmark.LastCompletedAt = snapshot.CompletedAt
			c.benchmark.LastEligibleNodes = snapshot.EligibleNodes
			c.benchmark.LastValidSamples = snapshot.ValidSamples
			c.benchmark.LastAggregateBytes = snapshot.AggregateBytes
			c.benchmark.LastDurationMS = snapshot.DurationMS
			c.benchmark.PayloadBytes = snapshot.PayloadBytes
			c.benchmark.PerNodeTimeoutMS = snapshot.PerNodeTimeoutMS
			c.benchmark.Generation = snapshot.Generation
			c.benchmark.Samples = snapshot.Samples
		}
	}
	return c
}

func (c *Coordinator) Start(parent context.Context) {
	if c == nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		cancel()
		return
	}
	c.started = true
	c.stop = cancel
	c.mu.Unlock()
	if c.supervisor != nil {
		c.wait.Add(1)
		go func() {
			defer c.wait.Done()
			c.supervisorLoop(ctx)
		}()
	}
	c.wait.Add(1)
	go func() {
		defer c.wait.Done()
		c.schedule(ctx)
	}()
}

func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	stop := c.stop
	cancel := c.benchmarkCancel
	done := c.benchmarkDone
	supervisorCancel := c.supervisorCancel
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
	if cancel != nil {
		cancel()
	}
	if supervisorCancel != nil {
		supervisorCancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	c.wait.Wait()
}

// TriggerBenchmark accepts a typed full-run request and returns immediately.
// The work is single-flight and always uses the fixed repository policy.
func (c *Coordinator) TriggerBenchmark() error {
	if c == nil || c.runner == nil || !c.policy.Enabled {
		return errors.New("benchmark unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.mu.Lock()
	// Apply admission and benchmark ownership are decided under the same
	// mutex. Once an Apply caller has entered BeginApply, no benchmark may
	// acquire the lifecycle token in the gap before Apply starts waiting for
	// it.
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.benchmarkCancel != nil {
		c.mu.Unlock()
		cancel()
		return ErrBenchmarkBusy
	}
	select {
	case <-c.lifecycle:
	default:
		c.mu.Unlock()
		cancel()
		return ErrBenchmarkBusy
	}
	c.benchmarkCancel, c.benchmarkDone = cancel, done
	c.benchmark.Running = true
	c.benchmark.State = "running"
	c.mu.Unlock()
	go c.runBenchmark(ctx, done)
	return nil
}

func (c *Coordinator) runBenchmark(ctx context.Context, done chan struct{}) {
	defer func() {
		c.mu.Lock()
		c.benchmarkCancel = nil
		c.benchmarkDone = nil
		c.benchmark.Running = false
		c.mu.Unlock()
		close(done)
		c.lifecycle <- struct{}{}
	}()
	nodes := []NodeState(nil)
	if c.nodes != nil {
		nodes = c.nodes(ctx)
	}
	current := ""
	if c.supervisor != nil {
		current = c.supervisor.currentTarget()
	}
	result := c.runner.Run(ctx, nodes, current)
	if result.SwitchAllowed && c.supervisor != nil {
		if err := c.supervisor.ApplyBenchmark(ctx, result); err != nil {
			result.SwitchAllowed = false
		}
	}
	c.mu.Lock()
	c.benchmark.LastResult = result.ResultClass
	c.benchmark.State = result.ResultClass
	c.benchmark.LastCompletedAt = result.CompletedAt
	c.benchmark.LastEligibleNodes = result.EligibleNodes
	c.benchmark.LastValidSamples = result.ValidSamples
	c.benchmark.LastAggregateBytes = result.AggregateBytes
	c.benchmark.LastDurationMS = result.Duration.Milliseconds()
	c.benchmark.PayloadBytes = result.PayloadBytes
	c.benchmark.MaximumWallSeconds = int64(result.MaximumWallTime.Seconds())
	c.benchmark.Generation = result.Generation
	c.benchmark.CleanupPending = result.CleanupPending
	c.benchmark.SwitchAllowed = result.SwitchAllowed
	c.benchmark.Samples = throughputStatuses(result.Samples)
	c.mu.Unlock()
}

func throughputStatuses(samples map[string]ThroughputSample) map[string]ThroughputStatus {
	result := make(map[string]ThroughputStatus, len(samples))
	for tag, sample := range samples {
		result[tag] = ThroughputStatus{Valid: sample.Valid, BytesPerSecond: sample.BytesPerSecond}
	}
	return result
}

// BeginApply gives an explicit operator mutation priority over managed runtime
// work. It prevents new benchmark/supervisor work from starting, cancels and
// drains any active benchmark and active supervisor operation (including probe
// cleanup), then holds the lifecycle token across the node transaction.
func (c *Coordinator) BeginApply(ctx context.Context) (func(), error) {
	return c.beginApply(ctx, false)
}

// BeginRecovery admits the bounded startup recovery path while maintenance is
// active. It is intentionally separate from BeginApply so an unresolved
// journal cannot be bypassed by an ordinary lifecycle mutation.
func (c *Coordinator) BeginRecovery(ctx context.Context) (func(), error) {
	return c.beginApply(ctx, true)
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
	supervisorCancel, supervisorDone := c.supervisorCancel, c.supervisorDone
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
	if supervisorCancel != nil {
		supervisorCancel()
	}
	if err := waitForManagedWork(ctx, done); err != nil {
		clearPending()
		return nil, err
	}
	if err := waitForManagedWork(ctx, supervisorDone); err != nil {
		clearPending()
		return nil, err
	}
	// A cancelled liveness tick may have observed context cancellation as a
	// probe failure or partially advanced RAM-only RTT persistence before it
	// drained. An explicit lifecycle mutation invalidates that transient
	// evidence anyway, so clear it before the transaction starts.
	c.resetSupervisorTransientState()
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
				c.requestSupervisorReconcile()
			})
		}, nil
	case <-ctx.Done():
		clearPending()
		return nil, ctx.Err()
	}
}

func (c *Coordinator) resetSupervisorTransientState() {
	if c == nil || c.supervisor == nil {
		return
	}
	c.supervisor.policyMu.Lock()
	c.supervisor.failures = 0
	c.supervisor.engine.ResetEvidence()
	c.supervisor.policyMu.Unlock()
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

func (c *Coordinator) requestSupervisorReconcile() {
	if c == nil || c.supervisor == nil || c.supervisorWake == nil {
		return
	}
	select {
	case c.supervisorWake <- struct{}{}:
	default:
	}
}

// SetManualOverride is an explicit operator mutation and therefore uses the
// same lifecycle barrier as node Apply. It may cancel a sustained benchmark;
// automatic latency/throughput work must never race the operator choice.
func (c *Coordinator) SetManualOverride(ctx context.Context, target string) error {
	if c == nil || c.supervisor == nil {
		return errors.New("selection unavailable")
	}
	release, err := c.BeginApply(ctx)
	if err != nil {
		return err
	}
	defer release()
	return c.supervisor.SetManualOverride(ctx, target)
}

// runSupervisorOperation registers one cancellable supervisor operation under
// the coordinator. Benchmarks deliberately do not block admission here: the
// supervisor may interleave liveness probes between benchmark samples through
// ProbeRouter's lease. Apply admission, however, blocks new operations and can
// cancel/drain the current one before Xray restart/rollback begins.
func (c *Coordinator) runSupervisorOperation(parent context.Context, operation func(context.Context) error) error {
	if c == nil || operation == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	c.mu.Lock()
	if c.maintenance || c.applyWaiters > 0 || c.applyActive || c.supervisorCancel != nil {
		c.mu.Unlock()
		cancel()
		return ErrLifecycleBusy
	}
	c.supervisorCancel, c.supervisorDone = cancel, done
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.supervisorDone == done {
			c.supervisorCancel = nil
			c.supervisorDone = nil
		}
		c.mu.Unlock()
		cancel()
		close(done)
	}()
	return operation(ctx)
}

func (c *Coordinator) supervisorLoop(ctx context.Context) {
	if c == nil || c.supervisor == nil {
		return
	}
	ticker := time.NewTicker(c.policy.ProbeInterval)
	defer ticker.Stop()
	reconciled := c.supervisor.probe == nil
	for {
		if !reconciled {
			if err := c.runSupervisorOperation(ctx, c.supervisor.probe.Reconcile); err == nil {
				reconciled = true
			}
		}
		if reconciled {
			_ = c.runSupervisorOperation(ctx, c.supervisor.Tick)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-c.supervisorWake:
		}
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
		return Status{Selection: SelectionStatus{State: "unavailable"}, Benchmark: BenchmarkStatus{State: "unavailable"}}
	}
	c.mu.Lock()
	benchmark := c.benchmark
	c.mu.Unlock()
	if c.supervisor != nil {
		return Status{Selection: c.supervisor.Snapshot(), Benchmark: benchmark}
	}
	return Status{Benchmark: benchmark}
}

func (c *Coordinator) schedule(ctx context.Context) {
	for {
		next := NextRunAt(time.Now(), time.Local)
		c.mu.Lock()
		c.benchmark.NextRunAt = next
		c.mu.Unlock()
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			_ = c.TriggerBenchmark()
		}
	}
}
