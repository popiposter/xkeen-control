package c1

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type adaptiveTransportStub struct {
	mu             sync.Mutex
	calls          []string
	duration       time.Duration
	failedDownload int
	failedUpload   int
	started        chan struct{}
	block          <-chan struct{}
}

func (s *adaptiveTransportStub) Download(ctx context.Context, payload int64) (ManualTransfer, error) {
	index := s.record("download-" + formatInt64(payload))
	if index == 0 && s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ManualTransfer{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return ManualTransfer{}, err
	}
	if index == s.failedDownload {
		return ManualTransfer{Bytes: payload / 2, Duration: s.duration}, errors.New("synthetic adaptive download failure")
	}
	return ManualTransfer{Bytes: payload, Duration: s.duration}, nil
}

func (s *adaptiveTransportStub) Upload(ctx context.Context, payload int64) (ManualTransfer, error) {
	index := s.record("upload-" + formatInt64(payload))
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ManualTransfer{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return ManualTransfer{}, err
	}
	if index == s.failedUpload {
		return ManualTransfer{Bytes: payload / 2, Duration: s.duration}, errors.New("synthetic adaptive upload failure")
	}
	return ManualTransfer{Bytes: payload, Duration: s.duration}, nil
}

func (s *adaptiveTransportStub) record(call string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := 0
	if strings.HasPrefix(call, "download-") {
		for _, previous := range s.calls {
			if strings.HasPrefix(previous, "download-") {
				index++
			}
		}
	} else {
		for _, previous := range s.calls {
			if strings.HasPrefix(previous, "upload-") {
				index++
			}
		}
	}
	s.calls = append(s.calls, call)
	return index
}

func (s *adaptiveTransportStub) callList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func adaptiveTestGeneration(tags ...string) AdaptiveGeneration {
	inputs := make([]AdaptiveCandidateInput, 0, len(tags))
	for index, tag := range tags {
		inputs = append(inputs, AdaptiveCandidateInput{Tag: tag, RTTMS: int64(100 + index), Samples: 3, LatestAt: time.Now().UTC()})
	}
	return AdaptiveGeneration{Generation: 1, CurrentTarget: tags[0], StableSince: time.Now().UTC().Add(-time.Hour), Candidates: inputs}
}

func TestAdaptiveRunnerUsesFixedSequentialDownUpPlanWithoutLatencyPhase(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &adaptiveTransportStub{duration: 300 * time.Millisecond, failedDownload: -1, failedUpload: -1}
	runner := &AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport}
	result := runner.Run(context.Background(), adaptiveTestGeneration("proxy-current", "proxy-challenger"), nil)
	if result.State != "completed" || result.ValidCount != 2 || !result.CurrentValid || result.ShortlistCount != 2 {
		t.Fatalf("adaptive result = %+v", result)
	}
	if result.AggregateBytes != 2*(AdaptiveMaxDownloadBytes+AdaptiveMaxUploadBytes) || len(result.Candidates) != 2 {
		t.Fatalf("adaptive byte/result bounds = %+v", result)
	}
	want := []string{
		"download-1048576", "download-3145728", "download-4194304", "download-8388608",
		"upload-1048576", "upload-3145728", "upload-4194304",
		"download-1048576", "download-3145728", "download-4194304", "download-8388608",
		"upload-1048576", "upload-3145728", "upload-4194304",
	}
	if got := transport.callList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("adaptive fixed stage plan = %v, want %v", got, want)
	}
	if len(api.adds) != 2 || len(api.removes) != 2 || api.adds[0].RuleTag != AdaptiveRuleTag || api.adds[1].RuleTag != AdaptiveRuleTag {
		t.Fatalf("adaptive probe ownership = adds=%+v removes=%v", api.adds, api.removes)
	}
}

func TestAdaptiveRunnerEarlyStopFailureContinuationAndCleanupAbort(t *testing.T) {
	t.Run("early stop", func(t *testing.T) {
		transport := &adaptiveTransportStub{duration: AdaptiveEarlyStopDuration, failedDownload: -1, failedUpload: -1}
		result := (&AdaptiveRunner{Probe: NewProbeRouter(&benchmarkProbeAPI{}), Transport: transport}).Run(context.Background(), adaptiveTestGeneration("proxy-current", "proxy-challenger"), nil)
		if result.State != "completed" || result.ValidCount != 2 || result.AggregateBytes != 2*(MiB+MiB) {
			t.Fatalf("early-stop result = %+v", result)
		}
		if calls := transport.callList(); len(calls) != 4 || calls[0] != "download-1048576" || calls[1] != "upload-1048576" {
			t.Fatalf("early-stop calls = %v", calls)
		}
	})

	t.Run("failed candidate continues", func(t *testing.T) {
		api := &benchmarkProbeAPI{}
		transport := &adaptiveTransportStub{duration: 300 * time.Millisecond, failedDownload: 0, failedUpload: -1}
		result := (&AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport}).Run(context.Background(), adaptiveTestGeneration("proxy-failed", "proxy-valid"), nil)
		if result.State != "completed" || result.ValidCount != 1 || len(result.Candidates) != 2 || result.Candidates[0].Valid || !result.Candidates[1].Valid {
			t.Fatalf("failed-candidate result = %+v", result)
		}
		if len(api.adds) != 2 || len(api.removes) != 2 {
			t.Fatalf("failed-candidate cleanup/continuation = adds=%d removes=%d", len(api.adds), len(api.removes))
		}
	})

	t.Run("cleanup aborts generation", func(t *testing.T) {
		api := &benchmarkProbeAPI{failRemove: true}
		transport := &adaptiveTransportStub{duration: 300 * time.Millisecond, failedDownload: -1, failedUpload: -1}
		result := (&AdaptiveRunner{Probe: NewProbeRouter(api), Transport: transport}).Run(context.Background(), adaptiveTestGeneration("proxy-current", "proxy-challenger"), nil)
		if result.State != "cleanup-pending" || result.ReasonCode != AdaptiveReasonCleanupPending || len(result.Candidates) != 0 {
			t.Fatalf("cleanup result = %+v", result)
		}
		if len(api.adds) != 1 {
			t.Fatalf("cleanup failure started later candidate: adds=%d", len(api.adds))
		}
	})
}

func TestAdaptiveScoreUsesFiniteFormulaAndExactRTTGuards(t *testing.T) {
	results := []AdaptiveCandidateResult{
		{Tag: "proxy-current", RTTMS: 100, DownloadBPS: 10, UploadBPS: 10, Valid: true},
		{Tag: "proxy-challenger", RTTMS: 110, DownloadBPS: 30, UploadBPS: 20, Valid: true},
	}
	winner, winnerScore, currentScore, challenger := scoreAdaptiveResults(results, "proxy-current")
	if winner != "proxy-challenger" || !challenger || !(winnerScore > currentScore) || !finitePositive(winnerScore) || !finitePositive(currentScore) {
		t.Fatalf("adaptive score decision = winner=%q winner=%v current=%v challenger=%v results=%+v", winner, winnerScore, currentScore, challenger, results)
	}
	if math.IsNaN(results[0].Score) || math.IsInf(results[0].Score, 0) || math.IsNaN(results[1].Score) || math.IsInf(results[1].Score, 0) {
		t.Fatalf("non-finite adaptive score = %+v", results)
	}

	guarded := []AdaptiveCandidateResult{
		{Tag: "proxy-current", RTTMS: 100, DownloadBPS: 10, UploadBPS: 10, Valid: true},
		{Tag: "proxy-too-slow", RTTMS: 176, DownloadBPS: 1000, UploadBPS: 1000, Valid: true},
	}
	winner, _, _, challenger = scoreAdaptiveResults(guarded, "proxy-current")
	if winner != "proxy-current" || challenger {
		t.Fatalf("RTT guard accepted ineligible challenger = winner=%q challenger=%v results=%+v", winner, challenger, guarded)
	}
}

func adaptiveEvidenceFixture(t *testing.T, current string, stableSince time.Time, policy Policy, tags []string, delays []int64, now time.Time) (*Supervisor, *supervisorReader, *supervisorAPI, SelectionStore) {
	t.Helper()
	health := make([]xrayapi.OutboundHealth, 0, len(tags))
	for index, tag := range tags {
		health = append(health, xrayapi.OutboundHealth{Tag: tag, Alive: true, DelayMS: delays[index], LastTry: now.Add(-time.Minute), LastSeen: now.Add(-time.Minute)})
	}
	reader := &supervisorReader{snapshot: xrayapi.Snapshot{
		APIReachable: true, RoutingReachable: true, ObservatoryReachable: true,
		Balancer: xrayapi.BalancerState{NativeSelected: current, Override: current}, OutboundHealth: health,
	}}
	api := &supervisorAPI{reader: reader}
	store := SelectionStore{Path: t.TempDir() + "/selection.json"}
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: current, StableSince: stableSince, LastSwitchReason: ReasonStartup, LastSwitchAt: stableSince}); err != nil {
		t.Fatal(err)
	}
	nodes := make([]NodeState, 0, len(tags))
	for index, tag := range tags {
		nodes = append(nodes, NodeState{ID: "node-" + formatInt64(int64(index+1)), Tag: tag, Enabled: true})
	}
	supervisor := NewSupervisor(policy, reader, api, func(context.Context) []NodeState { return append([]NodeState(nil), nodes...) }, NewProbeRouter(api), store)
	supervisor.SetClock(func() time.Time { return now })
	return supervisor, reader, api, store
}

func seedAdaptiveEvidence(supervisor *Supervisor, tags []string, now time.Time, delays []int64) {
	for index := 0; index < 3; index++ {
		at := now.Add(-time.Duration(3-index) * time.Minute)
		observations := make([]Observation, 0, len(tags))
		for item, tag := range tags {
			observations = append(observations, Observation{Tag: tag, Alive: true, DelayMS: delays[item], LastTry: at, LastSeen: at})
		}
		supervisor.engine.Observe(at, observations)
	}
}

func TestSupervisorAdaptiveSnapshotUsesFreshUniqueRTTAndLowestFivePlusCurrent(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tags := []string{"proxy-current", "proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f", "proxy-g"}
	delays := []int64{500, 20, 20, 30, 40, 50, 60, 70}
	policy := supervisorPolicy()
	policy.LatencyWindow = 15 * time.Minute
	supervisor, _, _, _ := adaptiveEvidenceFixture(t, "proxy-current", now.Add(-time.Hour), policy, tags, delays, now)
	seedAdaptiveEvidence(supervisor, tags, now, delays)
	generation, reason := supervisor.PrepareAdaptiveGeneration(context.Background(), 1)
	if reason != "" {
		t.Fatalf("adaptive preparation reason = %q", reason)
	}
	if generation.Generation != 1 || generation.CurrentTarget != "proxy-current" || len(generation.Candidates) != AdaptiveMaxCandidates {
		t.Fatalf("adaptive generation = %+v", generation)
	}
	want := []string{"proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-current"}
	got := make([]string, 0, len(generation.Candidates))
	for _, candidate := range generation.Candidates {
		got = append(got, candidate.Tag)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adaptive shortlist = %v, want %v", got, want)
	}
	if supervisor.engine.SampleCount("proxy-current") != 3 || supervisor.engine.SampleCount("proxy-a") != 3 {
		t.Fatalf("duplicate LastTry inflated evidence: current=%d a=%d", supervisor.engine.SampleCount("proxy-current"), supervisor.engine.SampleCount("proxy-a"))
	}
}

func TestSupervisorAdaptivePreparationSkipsManualNativeAndIneligibleStates(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tags := []string{"proxy-current", "proxy-challenger"}
	delays := []int64{100, 90}
	policy := supervisorPolicy()
	supervisor, reader, api, _ := adaptiveEvidenceFixture(t, "proxy-current", now.Add(-time.Hour), policy, tags, delays, now)
	seedAdaptiveEvidence(supervisor, tags, now, delays)
	if _, reason := supervisor.PrepareAdaptiveGeneration(context.Background(), 1); reason != "" {
		t.Fatalf("eligible preparation reason = %q", reason)
	}
	reader.snapshot.Balancer.Override = ""
	if _, reason := supervisor.PrepareAdaptiveGeneration(context.Background(), 2); reason != AdaptiveReasonNoCurrentTarget {
		t.Fatalf("native fallback preparation reason = %q", reason)
	}
	reader.snapshot.Balancer.Override = "proxy-current"
	if err := supervisor.SetManualOverride(context.Background(), "proxy-current"); err != nil {
		t.Fatal(err)
	}
	if _, reason := supervisor.PrepareAdaptiveGeneration(context.Background(), 3); reason != AdaptiveReasonManualOverride {
		t.Fatalf("manual preparation reason = %q", reason)
	}
	if len(api.adds) != 0 {
		t.Fatalf("adaptive preparation installed probe rules: %+v", api.adds)
	}
}

func TestSupervisorTickCollectsRTTButNeverHealthyLatencySwitches(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tags := []string{"proxy-current", "proxy-challenger"}
	delays := []int64{300, 1}
	policy := supervisorPolicy()
	policy.MinimumDwell = time.Nanosecond
	supervisor, reader, _, _ := adaptiveEvidenceFixture(t, "proxy-current", now.Add(-time.Hour), policy, tags, delays, now)
	supervisor.SetActiveProbe(func(context.Context, string, int64) error { return nil })
	for index := 0; index < 3; index++ {
		at := now.Add(-time.Duration(3-index) * time.Minute)
		reader.snapshot.OutboundHealth[0].LastTry = at
		reader.snapshot.OutboundHealth[1].LastTry = at
		if err := supervisor.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if reader.snapshot.Balancer.Override != "proxy-current" || supervisor.engine.SampleCount("proxy-challenger") != 3 {
		t.Fatalf("healthy Tick changed target or lost RTT evidence: override=%q samples=%d", reader.snapshot.Balancer.Override, supervisor.engine.SampleCount("proxy-challenger"))
	}
}

func adaptiveApplyFixture(t *testing.T, stableSince time.Time) (*Supervisor, *supervisorReader, *supervisorAPI, SelectionStore, AdaptiveGeneration, AdaptiveResult) {
	t.Helper()
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	policy := supervisorPolicy()
	policy.MinimumDwell = 30 * time.Minute
	tags := []string{"proxy-current", "proxy-challenger"}
	supervisor, reader, api, store := adaptiveEvidenceFixture(t, "proxy-current", stableSince, policy, tags, []int64{100, 100}, now)
	generation := AdaptiveGeneration{Generation: 7, CurrentTarget: "proxy-current", StableSince: stableSince, Candidates: []AdaptiveCandidateInput{
		{Tag: "proxy-current", RTTMS: 100}, {Tag: "proxy-challenger", RTTMS: 100},
	}}
	result := AdaptiveResult{Generation: 7, State: "completed", CurrentTarget: "proxy-current", CurrentValid: true, Candidates: []AdaptiveCandidateResult{
		{Tag: "proxy-current", RTTMS: 100, DownloadBPS: 10, UploadBPS: 10, Valid: true},
		{Tag: "proxy-challenger", RTTMS: 100, DownloadBPS: 30, UploadBPS: 30, Valid: true},
	}}
	return supervisor, reader, api, store, generation, result
}

func TestSupervisorApplyAdaptiveUsesExistingSelectionWriteAndStaleGuard(t *testing.T) {
	stableSince := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	supervisor, reader, api, store, generation, result := adaptiveApplyFixture(t, stableSince)
	decision, err := supervisor.ApplyAdaptive(context.Background(), generation, result)
	if err != nil || !decision.Applied || decision.Target != "proxy-challenger" || decision.ReasonCode != AdaptiveReasonAdaptiveQuality {
		t.Fatalf("adaptive apply = %+v err=%v", decision, err)
	}
	if reader.snapshot.Balancer.Override != "proxy-challenger" || len(api.override) != 1 || api.override[0] != "proxy-challenger" {
		t.Fatalf("adaptive runtime target = override=%q trace=%v", reader.snapshot.Balancer.Override, api.override)
	}
	record, err := store.Load()
	if err != nil || record.Target != "proxy-challenger" || record.LastSwitchReason != AdaptiveReasonAdaptiveQuality {
		t.Fatalf("adaptive selection record = %+v err=%v", record, err)
	}

	reader.snapshot.Balancer.Override = "proxy-current"
	stale, err := supervisor.ApplyAdaptive(context.Background(), generation, result)
	if err != nil || stale.Applied || stale.ReasonCode != AdaptiveReasonStaleGeneration {
		t.Fatalf("stale adaptive generation = %+v err=%v", stale, err)
	}
}

func TestSupervisorApplyAdaptiveEnforcesDwellAndHysteresisWithoutWrite(t *testing.T) {
	stableSince := time.Date(2026, 9, 18, 9, 50, 0, 0, time.UTC)
	supervisor, reader, api, store, generation, result := adaptiveApplyFixture(t, stableSince)
	decision, err := supervisor.ApplyAdaptive(context.Background(), generation, result)
	if err != nil || decision.Applied || decision.ReasonCode != AdaptiveReasonMinimumDwell || len(api.override) != 0 {
		t.Fatalf("dwell guard = %+v err=%v trace=%v", decision, err, api.override)
	}
	if reader.snapshot.Balancer.Override != "proxy-current" {
		t.Fatalf("dwell guard changed runtime target: %q", reader.snapshot.Balancer.Override)
	}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}

	stableSince = time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	supervisor, reader, api, _, generation, result = adaptiveApplyFixture(t, stableSince)
	result.Candidates[1].DownloadBPS = 11
	result.Candidates[1].UploadBPS = 11
	decision, err = supervisor.ApplyAdaptive(context.Background(), generation, result)
	if err != nil || decision.Applied || decision.ReasonCode != AdaptiveReasonHysteresis || len(api.override) != 0 || reader.snapshot.Balancer.Override != "proxy-current" {
		t.Fatalf("hysteresis guard = %+v err=%v trace=%v override=%q", decision, err, api.override, reader.snapshot.Balancer.Override)
	}
}

func TestCoordinatorAdaptiveIsOnePerformanceOwnerAndApplyCancelsIt(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	policy := supervisorPolicy()
	policy.MinimumDwell = time.Nanosecond
	tags := []string{"proxy-current", "proxy-challenger"}
	supervisor, _, api, _ := adaptiveEvidenceFixture(t, "proxy-current", now.Add(-time.Hour), policy, tags, []int64{100, 90}, now)
	seedAdaptiveEvidence(supervisor, tags, now, []int64{100, 90})
	probe := NewProbeRouter(api)
	blocking := &adaptiveTransportStub{started: make(chan struct{}), block: make(chan struct{}), duration: 300 * time.Millisecond, failedDownload: -1, failedUpload: -1}
	coordinator := NewCoordinator(policy, supervisor, NewBenchmarkRunner(policy, probe, BenchmarkStore{Path: t.TempDir() + "/benchmark.json"}), func(context.Context) []NodeState {
		return []NodeState{{ID: "node-1", Tag: "proxy-current", Enabled: true}, {ID: "node-2", Tag: "proxy-challenger", Enabled: true}}
	})
	coordinator.SetAdaptiveRunner(&AdaptiveRunner{Probe: probe, Transport: blocking})
	coordinator.SetManualRunner(&ManualNodeRunner{Probe: probe, Transport: &manualTransportStub{stageDuration: time.Millisecond, failedDownload: -1}})
	coordinator.runScheduledAdaptive(context.Background())
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("adaptive generation did not start")
	}
	if err := coordinator.TriggerBenchmark(); !errors.Is(err, ErrBenchmarkBusy) {
		t.Fatalf("legacy benchmark admitted beside adaptive = %v", err)
	}
	if err := coordinator.TriggerManualNode("node-00000001"); !errors.Is(err, ErrManualBusy) {
		t.Fatalf("manual diagnostic admitted beside adaptive = %v", err)
	}
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed apply admitted beside adaptive = %v", err)
	}
	applyContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := coordinator.BeginApply(applyContext)
	if err != nil {
		t.Fatalf("Apply did not cancel/drain adaptive = %v", err)
	}
	release()
	status := coordinator.AdaptiveSnapshot()
	if status.State != "cancelled" || status.ReasonCode != AdaptiveReasonCancelled {
		t.Fatalf("adaptive cancellation status = %+v", status)
	}
	if len(api.rules) != 0 || coordinator.IsLifecycleBusy() {
		t.Fatalf("adaptive ownership leaked after Apply: rules=%v busy=%v", api.rules, coordinator.IsLifecycleBusy())
	}
}

func TestCoordinatorStartsFreshAdaptiveCadenceAndDoesNotExposeLegacyNextRun(t *testing.T) {
	start := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	coordinator := NewCoordinator(DefaultPolicy(), nil, nil, nil)
	coordinator.SetClock(func() time.Time { return start })
	coordinator.Start(context.Background())
	status := coordinator.AdaptiveSnapshot()
	if !status.NextRunAt.Equal(start.Add(AdaptiveCadence)) || !coordinator.Snapshot().Benchmark.NextRunAt.IsZero() || coordinator.Snapshot().Benchmark.Schedule != ExplicitBenchmarkSchedule {
		t.Fatalf("scheduler projection = adaptive=%s legacy=%s", status.NextRunAt, coordinator.Snapshot().Benchmark.NextRunAt)
	}
	coordinator.Stop()
}
