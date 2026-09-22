package c1

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPerformancePolicyCommitRecomputesNextRunWithoutStartingWorkOrWritingSelection(t *testing.T) {
	policy := DefaultPolicy()
	coordinator := NewCoordinator(policy, nil, nil, nil)
	appliedAt := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	candidate := DefaultPerformancePolicy()
	candidate.ProbeIntervalSeconds = 120
	candidate.FailureThreshold = 4
	candidate.AdaptiveCadenceMinutes = 240
	candidate.AdaptiveChallengerLimit = 3
	candidate.MinimumDwellMinutes = 60
	candidate.QualityHysteresisPercent = 25
	persisted := 0
	if err := coordinator.CommitPerformancePolicy(context.Background(), candidate, appliedAt, true, func() error { persisted++; return nil }); err != nil {
		t.Fatal(err)
	}
	if persisted != 1 || coordinator.performancePolicy != candidate || coordinator.policy.ProbeInterval != 120*time.Second || coordinator.policy.FailureThreshold != 4 || coordinator.policy.MinimumDwell != time.Hour {
		t.Fatalf("runtime policy = persisted=%d performance=%+v base=%+v", persisted, coordinator.performancePolicy, coordinator.policy)
	}
	if !coordinator.adaptive.NextRunAt.Equal(appliedAt.Add(240*time.Minute)) || coordinator.adaptiveGeneration != 0 || coordinator.benchmarkCancel != nil || coordinator.benchmark.Running {
		t.Fatalf("Apply started or mis-scheduled work: adaptive=%+v benchmark=%+v", coordinator.adaptive, coordinator.benchmark)
	}
	if coordinator.performanceMode != "" {
		t.Fatalf("Apply acquired performance mode %q", coordinator.performanceMode)
	}
}

func TestPerformancePolicyCommitRejectsActiveGenerationAndFreezesOldPolicy(t *testing.T) {
	coordinator := NewCoordinator(DefaultPolicy(), nil, nil, nil)
	oldPolicy := coordinator.performancePolicy
	coordinator.mu.Lock()
	coordinator.benchmarkCancel = func() {}
	coordinator.benchmarkDone = make(chan struct{})
	coordinator.performanceMode = AdaptiveMode
	coordinator.mu.Unlock()
	candidate := oldPolicy
	candidate.AdaptiveCadenceMinutes = 360
	persisted := false
	err := coordinator.CommitPerformancePolicy(context.Background(), candidate, time.Now(), true, func() error { persisted = true; return nil })
	if !errors.Is(err, ErrBenchmarkBusy) || persisted || coordinator.performancePolicy != oldPolicy {
		t.Fatalf("active generation commit = err=%v persisted=%v policy=%+v", err, persisted, coordinator.performancePolicy)
	}
}

func TestPerformancePolicyRetainsRTTEvidenceResetsCandidateAndBoundsShortlist(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	tags := []string{"proxy-current", "proxy-a", "proxy-b", "proxy-c", "proxy-d", "proxy-e", "proxy-f"}
	delays := []int64{500, 10, 20, 30, 40, 50, 60}
	supervisor, _, _, _ := adaptiveEvidenceFixture(t, "proxy-current", now.Add(-time.Hour), supervisorPolicy(), tags, delays, now)
	seedAdaptiveEvidence(supervisor, tags, now, delays)
	supervisor.engine.pending = "proxy-a"
	supervisor.engine.badWindows = 2
	candidate := DefaultPerformancePolicy()
	candidate.AdaptiveChallengerLimit = 2
	supervisor.applyPerformancePolicy(candidate)
	if supervisor.engine.SampleCount("proxy-a") != 3 || supervisor.engine.pending != "" || supervisor.engine.badWindows != 0 {
		t.Fatalf("policy reset lost evidence or retained streak: samples=%d pending=%q windows=%d", supervisor.engine.SampleCount("proxy-a"), supervisor.engine.pending, supervisor.engine.badWindows)
	}
	generation, reason := supervisor.PrepareAdaptiveGeneration(context.Background(), 1)
	if reason != "" {
		t.Fatalf("preparation reason = %q", reason)
	}
	want := []string{"proxy-a", "proxy-b", "proxy-current"}
	got := make([]string, 0, len(generation.Candidates))
	for _, item := range generation.Candidates {
		got = append(got, item.Tag)
	}
	if !reflect.DeepEqual(got, want) || len(got) > AdaptiveMaxCandidates {
		t.Fatalf("bounded shortlist = %v want=%v", got, want)
	}

	fastCurrentDelays := []int64{5, 10, 20, 30, 40, 50, 60}
	fastCurrent, _, _, _ := adaptiveEvidenceFixture(t, "proxy-current", now.Add(-time.Hour), supervisorPolicy(), tags, fastCurrentDelays, now)
	seedAdaptiveEvidence(fastCurrent, tags, now, fastCurrentDelays)
	policy := DefaultPerformancePolicy()
	policy.AdaptiveChallengerLimit = 1
	fastCurrent.applyPerformancePolicy(policy)
	generation, reason = fastCurrent.PrepareAdaptiveGeneration(context.Background(), 2)
	if reason != "" || len(generation.Candidates) != 2 || generation.Candidates[0].Tag != "proxy-a" || generation.Candidates[1].Tag != "proxy-current" {
		t.Fatalf("one challenger plus current = %+v reason=%q", generation, reason)
	}
}

func TestPerformancePolicyFailureThresholdAffectsSubsequentLivenessCycles(t *testing.T) {
	path := t.TempDir() + "/selection.json"
	when := time.Now().UTC().Add(-time.Hour)
	store := SelectionStore{Path: path}
	if _, err := store.SaveIfChanged(SelectionRecord{}, SelectionRecord{Target: "proxy-main-01", StableSince: when, LastSwitchReason: ReasonStartup, LastSwitchAt: when}); err != nil {
		t.Fatal(err)
	}
	reader := &supervisorReader{snapshot: supervisorSnapshot("proxy-main-01", "proxy-main-01", "proxy-main-02")}
	api := &supervisorAPI{reader: reader}
	supervisor := NewSupervisor(supervisorPolicy(), reader, api, func(context.Context) []NodeState {
		return []NodeState{{Tag: "proxy-main-01", Enabled: true}, {Tag: "proxy-main-02", Enabled: true}}
	}, NewProbeRouter(api), store)
	supervisor.SetActiveProbe(func(_ context.Context, target string, _ int64) error {
		if target == "proxy-main-01" {
			return errors.New("synthetic failure")
		}
		return nil
	})
	policy := DefaultPerformancePolicy()
	policy.FailureThreshold = 5
	supervisor.applyPerformancePolicy(policy)
	for cycle := 1; cycle <= 4; cycle++ {
		if err := supervisor.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if reader.snapshot.Balancer.Override != "proxy-main-01" {
			t.Fatalf("cycle %d switched before threshold: %q", cycle, reader.snapshot.Balancer.Override)
		}
	}
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.snapshot.Balancer.Override != "proxy-main-02" {
		t.Fatalf("fifth failure did not switch: %q", reader.snapshot.Balancer.Override)
	}
}

func TestPerformancePolicyDwellAndHysteresisCanOnlyBecomeMoreConservative(t *testing.T) {
	stableSince := time.Date(2026, 9, 18, 9, 50, 0, 0, time.UTC)
	supervisor, _, api, _, generation, result := adaptiveApplyFixture(t, stableSince)
	policy := DefaultPerformancePolicy()
	policy.MinimumDwellMinutes = 1440
	supervisor.applyPerformancePolicy(policy)
	decision, err := supervisor.ApplyAdaptive(context.Background(), generation, result)
	if err != nil || decision.Applied || decision.ReasonCode != AdaptiveReasonMinimumDwell || len(api.override) != 0 {
		t.Fatalf("configured dwell = %+v err=%v writes=%v", decision, err, api.override)
	}

	stableSince = time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	supervisor, _, api, _, generation, result = adaptiveApplyFixture(t, stableSince)
	policy = DefaultPerformancePolicy()
	policy.QualityHysteresisPercent = 50
	supervisor.applyPerformancePolicy(policy)
	result.Candidates[1].DownloadBPS = 20
	result.Candidates[1].UploadBPS = 20
	decision, err = supervisor.ApplyAdaptive(context.Background(), generation, result)
	if err != nil || decision.Applied || decision.ReasonCode != AdaptiveReasonHysteresis || len(api.override) != 0 {
		t.Fatalf("configured hysteresis = %+v err=%v writes=%v", decision, err, api.override)
	}
}
