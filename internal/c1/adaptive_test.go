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

func TestAdaptiveScoreUsesFiniteFormulaAndAbsoluteRTTCeiling(t *testing.T) {
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
		{Tag: "proxy-too-slow", RTTMS: 751, DownloadBPS: 1000, UploadBPS: 1000, Valid: true},
	}
	winner, _, _, challenger = scoreAdaptiveResults(guarded, "proxy-current")
	if winner != "proxy-current" || challenger {
		t.Fatalf("RTT guard accepted ineligible challenger = winner=%q challenger=%v results=%+v", winner, challenger, guarded)
	}
}

func waitAdaptiveState(t *testing.T, coordinator *Coordinator, want string) AdaptivePerformanceStatus {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		status := coordinator.AdaptiveSnapshot()
		if status.State == want {
			return status
		}
		select {
		case <-deadline.C:
			t.Fatalf("adaptive state = %+v, want %q", status, want)
		case <-ticker.C:
		}
	}
}

func waitCoordinatorApplying(t *testing.T, coordinator *Coordinator) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot := coordinator.Snapshot()
		if snapshot.Lifecycle != nil && snapshot.Lifecycle.Applying {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("Coordinator never projected waiting operator Apply: %+v", snapshot.Lifecycle)
		case <-ticker.C:
		}
	}
}
