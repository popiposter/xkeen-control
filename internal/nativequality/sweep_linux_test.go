//go:build linux

package nativequality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type sweepReader struct {
	healthy       bool
	failAfterFile string
	count         int
	mutate        func(*xrayapi.Snapshot)
	snapshotCalls atomic.Int64
}

func (r *sweepReader) Snapshot(context.Context) xrayapi.Snapshot {
	r.snapshotCalls.Add(1)
	healthy := r.healthy
	if r.failAfterFile != "" {
		if _, err := os.Stat(r.failAfterFile); err == nil {
			healthy = false
		}
	}
	snapshot := xrayapi.Snapshot{APIReachable: healthy, RoutingReachable: healthy, ObservatoryReachable: healthy, Balancer: xrayapi.BalancerState{NativeSelected: "proxy-00"}}
	count := r.count
	if count == 0 {
		count = 14
	}
	for i := 0; i < count; i++ {
		snapshot.OutboundHealth = append(snapshot.OutboundHealth, xrayapi.OutboundHealth{Tag: fmt.Sprintf("proxy-%02d", i), Alive: true, DelayMS: int64(100 + i), LastTry: time.Now().UTC().Add(-time.Second)})
	}
	if r.mutate != nil {
		r.mutate(&snapshot)
	}
	return snapshot
}
func (r *sweepReader) ProbeReachable(context.Context) bool {
	if r.failAfterFile != "" {
		if _, err := os.Stat(r.failAfterFile); err == nil {
			return false
		}
	}
	return r.healthy
}

type sweepMeasurement struct {
	rttCalls     [][]string
	rttDown      map[string]bool
	rttSlow      map[string]bool
	rttErr       error
	onRTT        func(int)
	calls        [][]string
	validLimit   int
	failBatch    int
	cleanupBatch int
	onBatch      func(int)
	hold         <-chan struct{}
}

func (*sweepMeasurement) NativeQualityEvidence(xrayapi.Snapshot) map[string]c1.AdaptiveCandidateInput {
	return nil
}

// MeasureRTT answers every tag in 100+index ms unless it is listed as down.
func (m *sweepMeasurement) MeasureRTT(_ context.Context, tags []string) ([]c1.RTTSample, error) {
	m.rttCalls = append(m.rttCalls, append([]string(nil), tags...))
	if m.rttErr != nil {
		return nil, m.rttErr
	}
	out := make([]c1.RTTSample, 0, len(tags))
	for _, tag := range tags {
		var index int
		_, _ = fmt.Sscanf(tag, "proxy-%d", &index)
		sample := c1.RTTSample{Tag: tag, SampledAt: time.Now().UTC()}
		if !m.rttDown[tag] {
			sample.Valid, sample.RTTMS = true, int64(100+index)
			if m.rttSlow[tag] {
				sample.RTTMS = 20000
			}
		}
		out = append(out, sample)
	}
	if m.onRTT != nil {
		m.onRTT(len(m.rttCalls))
	}
	return out, nil
}
func (m *sweepMeasurement) MeasureNativeQuality(_ context.Context, g c1.AdaptiveGeneration, publish func(c1.AdaptivePerformanceStatus)) (c1.AdaptiveResult, error) {
	if m.hold != nil {
		<-m.hold
	}
	index := len(m.calls) + 1
	r := c1.AdaptiveResult{NativeQuality: true, Generation: g.Generation, StartedAt: time.Now().UTC(), State: "completed", ShortlistCount: len(g.Candidates), AggregateBytes: int64(len(g.Candidates)) * 6 * c1.MiB}
	var tags []string
	for _, input := range g.Candidates {
		tags = append(tags, input.Tag)
		valid := m.validLimit == 0 || len(m.calls)*3+len(tags) <= m.validLimit
		r.Candidates = append(r.Candidates, c1.AdaptiveCandidateResult{Tag: input.Tag, SampledAt: time.Now().UTC(), RTTMS: input.RTTMS, Valid: valid, DownloadBPS: 1e6, UploadBPS: 1e6})
		if valid {
			r.ValidCount++
		}
	}
	m.calls = append(m.calls, tags)
	if m.failBatch == index {
		r.State = "failed"
		r.Candidates = r.Candidates[:1]
		r.ValidCount = 1
	}
	if m.cleanupBatch == index {
		r.State = "cleanup-pending"
		return r, errors.New("probe cleanup unknown")
	}
	r.CompletedAt = time.Now().UTC()
	if publish != nil {
		publish(c1.AdaptivePerformanceStatus{State: r.State, ShortlistCount: len(g.Candidates), ValidCount: r.ValidCount})
	}
	if m.onBatch != nil {
		m.onBatch(index)
	}
	return r, nil
}

func TestSweepAdmissionFreezesFourteenAndCrossProcessOwner(t *testing.T) {
	first, g, _, _ := sweepFixture(t, true)
	hold := make(chan struct{})
	first.Measurement.(*sweepMeasurement).hold = hold
	if err := first.startSweep(context.Background(), "subscription-refresh"); err != nil {
		t.Fatal("first admission", err, first.Read().ReviewReason)
	}
	second, _, _, _ := sweepFixture(t, true)
	second.QuotaPath = first.QuotaPath
	if err := second.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("parallel review escaped quota lock")
	}
	close(hold)
	select {
	case <-first.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not settle")
	}
	v := first.Read()
	if v.ReviewTrigger != "subscription-refresh" || v.EligibleCount != len(g.Candidates) || v.SelectedForSpeed != 12 || v.AttemptedCount != 12 || v.BatchCount != 4 || v.AppliedState != "no-op" {
		t.Fatal("admission/coverage mismatch", v)
	}
	if release, err := acquireQuotaLock(first.QuotaPath); err != nil {
		t.Fatal("quota lock leaked", err)
	} else {
		release()
	}
}

func TestSweepFortySixEligibleRunsBoundedSubsetWithoutApply(t *testing.T) {
	s, _, _, _ := sweepFixtureCount(t, true, 46)
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal("bounded admission", err, s.Read().ReviewReason)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("bounded review did not settle")
	}
	v := s.Read()
	if v.State != "completed" || v.AppliedState != "no-op" || v.TotalEligible != 46 || v.SelectedForSpeed != 12 || v.DeferredForFutureReview != 34 || v.AttemptedCount != 12 || v.ValidCount != 12 || v.BatchCount != 4 || len(v.Progress.Candidates) != 12 || v.SubsetState != "subset-complete" || v.PoolDecision != "pool-unchanged" {
		t.Fatal("bounded review/status mismatch", v)
	}
	if len(s.Measurement.(*sweepMeasurement).calls) != 4 {
		t.Fatal("wrong batch count")
	}
	w, err := s.Editor.Workspace(context.Background())
	if err != nil || w.Pending != nil || w.Digest != v.Digest {
		t.Fatal("no-op changed configuration", err)
	}
}

func TestSweepOverlappingNativeSelectorRefusesBeforeQuotaOrTransfer(t *testing.T) {
	s, _, _, dir := sweepFixtureCount(t, true, 46)
	text := `{"routing":{"rules":[],"balancers":[{"tag":"bal-proxy","selector":["proxy-","proxy-0"],"strategy":{"type":"leastLoad","settings":{"maxRTT":"10s"}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("overlapping selector admitted")
	}
	if len(s.Measurement.(*sweepMeasurement).calls) != 0 {
		t.Fatal("refused selector transferred data")
	}
	if _, err := os.Stat(s.QuotaPath); !os.IsNotExist(err) {
		t.Fatal("refused selector reserved quota", err)
	}
}

func waitSweep(t *testing.T, s *Service) Status {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not settle")
	}
	return s.Read()
}

func assertNoQuotaOrTransfer(t *testing.T, s *Service, digest string) {
	t.Helper()
	if len(s.Measurement.(*sweepMeasurement).calls) != 0 {
		t.Fatal("refused review transferred data")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err == nil && (q.ReviewsUsed != 0 || q.FairCursor != 0) {
		t.Fatal("refused review reserved quota or advanced the cursor", q)
	}
	w, err := s.Editor.Workspace(context.Background())
	if err != nil || w.Pending != nil || w.Digest != digest {
		t.Fatal("refused review altered the configuration", err)
	}
}

func TestSweepStaleObservatoryNoLongerDefersBecauseEveryCandidateIsProbed(t *testing.T) {
	s, _, _, _ := sweepFixtureCount(t, true, 46)
	s.Reader.(*sweepReader).mutate = func(snapshot *xrayapi.Snapshot) {
		for i := range snapshot.OutboundHealth {
			snapshot.OutboundHealth[i].LastTry = time.Now().UTC().Add(-time.Hour)
		}
	}
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal("stale Observatory evidence deferred the review", err, s.Read().ReviewReason)
	}
	v := waitSweep(t, s)
	m := s.Measurement.(*sweepMeasurement)
	if len(m.rttCalls) != 2 || len(m.rttCalls[0]) != 6 || len(m.rttCalls[1]) != 6 || v.RTTValidCount != 12 || v.State != "completed" || v.PoolDecision != "pool-unchanged" || v.ReviewPhase != "speed" {
		t.Fatal("RTT pre-phase did not freeze and probe the bounded set in chunks", len(m.rttCalls), v.RTTValidCount, v.State, v.PoolDecision)
	}
	for _, tag := range m.rttCalls[0][:6] {
		if tag < "proxy-00" || tag > "proxy-05" {
			t.Fatal("incumbents were not probed first", m.rttCalls[0])
		}
	}
}

func TestSweepRTTFailureOrTooFewAnswersSpendsNoQuota(t *testing.T) {
	for name, configure := range map[string]func(*sweepMeasurement){
		"probe error": func(m *sweepMeasurement) { m.rttErr = errors.New("synthetic probe failure") },
		"one answer": func(m *sweepMeasurement) {
			m.rttDown = map[string]bool{}
			for i := 1; i < 14; i++ {
				m.rttDown[fmt.Sprintf("proxy-%02d", i)] = true
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, digest, _ := sweepFixture(t, true)
			configure(s.Measurement.(*sweepMeasurement))
			if err := s.startSweep(context.Background(), "periodic"); err != nil {
				t.Fatal(err)
			}
			v := waitSweep(t, s)
			if v.State != "deferred" || (v.ReviewReason != "rtt-probe-unavailable" && v.ReviewReason != "rtt-candidates-insufficient") {
				t.Fatal("RTT refusal state", v.State, v.ReviewReason)
			}
			assertNoQuotaOrTransfer(t, s, digest)
		})
	}
}

func TestSweepShadowedProbeRouteRefusesBeforeProbesOrQuota(t *testing.T) {
	s, _, _, dir := sweepFixture(t, true)
	text := `{"routing":{"rules":[{"domain":["geosite:example"],"balancerTag":"bal-proxy"}],"balancers":[{"tag":"bal-proxy","selector":["proxy-00","proxy-01","proxy-02","proxy-03","proxy-04","proxy-05"],"strategy":{"type":"leastLoad","settings":{"maxRTT":"10s"}}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("shadowed probe route admitted")
	}
	m := s.Measurement.(*sweepMeasurement)
	if s.Read().ReviewReason != "probe-route-shadowed" || len(m.rttCalls) != 0 || len(m.calls) != 0 {
		t.Fatal("shadowed route probed or measured", s.Read().ReviewReason)
	}
	if _, err := os.Stat(s.QuotaPath); !os.IsNotExist(err) {
		t.Fatal("refused review touched quota", err)
	}
}

func TestSweepIncumbentFailingRTTIsUnhealthyAndReplaced(t *testing.T) {
	s, _, _, _ := sweepFixture(t, true)
	s.Measurement.(*sweepMeasurement).rttDown = map[string]bool{"proxy-05": true}
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	v := waitSweep(t, s)
	if v.RTTValidCount != 11 || v.PoolDecision != "unhealthy-incumbent-replaced" {
		t.Fatal("failed incumbent probe was not treated as unhealthy", v.RTTValidCount, v.PoolDecision, v.ReviewReason)
	}
	for _, calls := range s.Measurement.(*sweepMeasurement).calls {
		for _, tag := range calls {
			if tag == "proxy-05" {
				t.Fatal("RTT-failed candidate entered the speed phase")
			}
		}
	}
}

func TestSweepProbeGateFailureDefersWithoutAnInspectionFence(t *testing.T) {
	s, _, digest, _ := sweepFixture(t, true)
	s.Measurement.(*sweepMeasurement).rttErr = c1.ErrProbeCleanup
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	v := waitSweep(t, s)
	if v.State != "deferred" || v.ReviewReason != "probe-cleanup-pending" || v.InspectionRequired || v.ReviewPhase != "rtt" {
		t.Fatal("probe gate failure fenced the service", v.State, v.ReviewReason, v.InspectionRequired, v.ReviewPhase)
	}
	assertNoQuotaOrTransfer(t, s, digest)
	// The next review is admitted: nothing outside a restart could clear a fence.
	s.Measurement.(*sweepMeasurement).rttErr = nil
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal("review after a transient probe failure was refused", err, s.Read().ReviewReason)
	}
	waitSweep(t, s)
}

func TestSweepIncumbentAboveNativeMaxRTTIsUnhealthy(t *testing.T) {
	s, _, _, _ := sweepFixture(t, true)
	s.Measurement.(*sweepMeasurement).rttSlow = map[string]bool{"proxy-05": true}
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	v := waitSweep(t, s)
	if v.RTTValidCount != 11 || v.PoolDecision != "unhealthy-incumbent-replaced" {
		t.Fatal("over-maxRTT incumbent was kept as healthy", v.RTTValidCount, v.PoolDecision, v.ReviewReason)
	}
}

func TestSweepPrePhaseRechecksConfigurationBetweenChunks(t *testing.T) {
	s, _, digest, dir := sweepFixture(t, true)
	m := s.Measurement.(*sweepMeasurement)
	m.onRTT = func(calls int) {
		if calls == 1 {
			_ = os.WriteFile(filepath.Join(dir, "06_policy.json"), []byte(`{"policy":{}}`), 0600)
		}
	}
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	v := waitSweep(t, s)
	if v.State != "deferred" || v.ReviewReason != "configuration-changed-or-pending" || len(m.rttCalls) != 1 {
		t.Fatal("configuration drift between chunks was not detected", v.State, v.ReviewReason, len(m.rttCalls))
	}
	_ = digest
	if len(m.calls) != 0 {
		t.Fatal("drifted review transferred data")
	}
}

func TestQualityStatusPollingUsesCachedNativeSelection(t *testing.T) {
	s, _, _, _ := sweepFixture(t, true)
	reader := s.Reader.(*sweepReader)
	s.status.NativeSelected = "proxy-00"
	s.status.NativeSelectedState = "observed"
	s.status.NativeSelectedAt = time.Now().UTC()
	for i := 0; i < 3; i++ {
		v := s.Read()
		if v.NativeSelected != "proxy-00" || v.NativeSelectedState != "observed" {
			t.Fatal("recent observed target lost", v.NativeSelectedState)
		}
	}
	if got := reader.snapshotCalls.Load(); got != 0 {
		t.Fatal("status poll opened full Xray snapshot", got)
	}
	s.status.NativeSelectedAt = time.Now().UTC().Add(-3 * time.Minute)
	v := s.Read()
	if v.NativeSelected != "" || v.NativeSelectedState != "unavailable" {
		t.Fatal("stale native selection presented as current", v.NativeSelectedState)
	}
}

func sweepFixture(t *testing.T, noop bool) (*Service, c1.AdaptiveGeneration, string, string) {
	return sweepFixtureCount(t, noop, 14)
}

func sweepFixtureCount(t *testing.T, noop bool, count int) (*Service, c1.AdaptiveGeneration, string, string) {
	t.Helper()
	dir := t.TempDir()
	validator := filepath.Join(dir, "xray")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var outbounds []map[string]string
	var selected []string
	var costs []c1.NativeQualityCost
	var candidates []c1.AdaptiveCandidateInput
	var pool []string
	for i := 0; i < count; i++ {
		tag := fmt.Sprintf("proxy-%02d", i)
		outbounds = append(outbounds, map[string]string{"tag": tag, "protocol": "vless"})
		pool = append(pool, tag)
		candidates = append(candidates, c1.AdaptiveCandidateInput{Tag: tag, RTTMS: int64(100 + i)})
		if i < 6 {
			selected = append(selected, tag)
			costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: 1})
		}
	}
	selector := []string{"proxy-"}
	if noop {
		selector = selected
	}
	routing, _ := json.Marshal(map[string]any{"routing": map[string]any{"rules": []any{}, "balancers": []any{map[string]any{"tag": "bal-proxy", "selector": selector, "strategy": map[string]any{"type": "leastLoad", "settings": map[string]any{"maxRTT": "10s", "costs": costs}}}}}})
	encoded, _ := json.Marshal(map[string]any{"outbounds": outbounds})
	if os.WriteFile(filepath.Join(dir, "05_routing.json"), routing, 0600) != nil || os.WriteFile(filepath.Join(dir, "04_outbounds.json"), encoded, 0600) != nil {
		t.Fatal("config fixture")
	}
	if err := os.WriteFile(filepath.Join(dir, "07_observatory.json"), []byte(`{"observatory":{"subjectSelector":["proxy-"],"probeInterval":"10s","enableConcurrency":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	lease := authority.NewLease()
	e := &xkeen.ConfigEditor{Dir: dir, XrayBinary: validator, PreviousDir: filepath.Join(t.TempDir(), "previous"), Lease: lease}
	w, err := e.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var readCount uint64
	guard := &resourcepolicy.Guard{Profile: resourcepolicy.ForPlatform("mipsle", 254472), Interval: time.Millisecond, Read: func() (resourcepolicy.Sample, error) {
		n := atomic.AddUint64(&readCount, 1)
		return resourcepolicy.Sample{At: time.Now().Add(time.Duration(n) * time.Second), Total: n * 100, Idle: n * 50, TotalKiB: 254472, AvailableKiB: 150000, SwapSource: "vmstat", SwapUnitBytes: 4096}, nil
	}}
	m := &sweepMeasurement{}
	s := &Service{Editor: e, Lease: lease, Reader: &sweepReader{healthy: true, count: count}, Nodes: func(context.Context) []c1.NodeState {
		result := make([]c1.NodeState, 0, len(pool))
		for _, tag := range pool {
			result = append(result, c1.NodeState{Tag: tag, Enabled: true})
		}
		return result
	}, Measurement: m, Resources: guard, Jobs: xkeen.NewJobs(filepath.Join(dir, "missing-xkeen"), lease), QuotaPath: filepath.Join(t.TempDir(), "quota", "receipt.json"), pool: pool, batchPause: time.Millisecond, status: Status{State: "running", Digest: w.Digest, EligibleCount: count, ActivePoolCount: len(selector), AppliedState: "not-attempted"}}
	return s, c1.AdaptiveGeneration{Generation: 1, NativeQuality: true, Candidates: candidates}, w.Digest, dir
}

func runSweepFixture(t *testing.T, s *Service, g c1.AdaptiveGeneration, digest string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan struct{})
	var tags []string
	for _, candidate := range g.Candidates[:min(12, len(g.Candidates))] {
		tags = append(tags, candidate.Tag)
	}
	s.runSweep(ctx, cancel, done, g, sweepPlan{Candidates: tags, Active: s.status.ActivePool}, 10000, digest, s.QuotaPath, nil, false)
	select {
	case <-done:
	default:
		t.Fatal("sweep did not settle")
	}
}

func TestSweepFourteenNodesFourBatchesNoopAndConsumed(t *testing.T) {
	s, g, digest, _ := sweepFixture(t, true)
	runSweepFixture(t, s, g, digest)
	m := s.Measurement.(*sweepMeasurement)
	if len(m.calls) != 4 || len(m.calls[0]) != 3 || len(m.calls[3]) != 3 {
		t.Fatal("wrong bounded batch plan", m.calls)
	}
	v := s.Read()
	if v.State != "completed" || v.AppliedState != "no-op" || v.AttemptedCount != 12 || v.ValidCount != 12 || v.BatchCount != 4 || len(v.Progress.Candidates) != 12 || v.CanStage || v.ActivePoolCount != 6 || v.AggregateBytes > testReviewBytes {
		t.Fatal("untruthful completed review", v)
	}
	if _, err := s.Stage(context.Background(), digest); err == nil {
		t.Fatal("automatic no-op was stageable again")
	}
	w, err := s.Editor.Workspace(context.Background())
	if err != nil || w.Pending != nil {
		t.Fatal("no-op created pending config", err)
	}
}

func TestSweepIncompleteOrDriftNeverSaves(t *testing.T) {
	for _, mode := range []string{"coverage", "pressure", "drift"} {
		t.Run(mode, func(t *testing.T) {
			s, g, digest, dir := sweepFixture(t, false)
			m := s.Measurement.(*sweepMeasurement)
			switch mode {
			case "coverage":
				m.validLimit = 9 // 9 of 12 is below the 80% coverage rule
			case "pressure":
				m.failBatch = 2
			case "drift":
				m.onBatch = func(i int) {
					if i == 1 {
						p := filepath.Join(dir, "05_routing.json")
						b, _ := os.ReadFile(p)
						_ = os.WriteFile(p, append(b, '\n'), 0600)
					}
				}
			}
			runSweepFixture(t, s, g, digest)
			v := s.Read()
			w, err := s.Editor.Workspace(context.Background())
			if v.State != "failed" || v.AppliedState != "not-attempted" || err != nil || w.Pending != nil {
				t.Fatal("unsafe sweep attempted Apply", v, err)
			}
		})
	}
}

func TestSweepSavedApplyRefusalFencesWithoutReplay(t *testing.T) {
	s, g, digest, _ := sweepFixture(t, false)
	runSweepFixture(t, s, g, digest)
	v := s.Read()
	w, err := s.Editor.Workspace(context.Background())
	if v.State != "failed" || v.AppliedState != "inspection-required" || !v.InspectionRequired || err != nil || w.Pending == nil {
		t.Fatal("saved-but-unapplied state not fenced", v, err)
	}
	before := len(s.Measurement.(*sweepMeasurement).calls)
	if err := s.Start(context.Background()); err == nil || len(s.Measurement.(*sweepMeasurement).calls) != before {
		t.Fatal("inspection fence admitted new measurement")
	}
	if err := s.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("inspection fence admitted new sweep")
	}
	if _, err := s.Stage(context.Background(), digest); err == nil {
		t.Fatal("inspection fence replayed Stage")
	}
}

func shellQuoted(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func writeSweepProcess(t *testing.T, proc, pid, start, xray, dir string) {
	t.Helper()
	p := filepath.Join(proc, pid)
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[19] = "S", start
	for name, data := range map[string][]byte{
		"comm":    []byte("xray\n"),
		"cmdline": []byte(xray + "\x00run\x00-confdir\x00" + dir + "\x00"),
		"stat":    []byte(pid + " (xray) " + strings.Join(fields, " ") + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(p, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(xray, filepath.Join(p, "exe")); err != nil {
		t.Fatal(err)
	}
}

func TestSweepFourteenNodesOneVerifiedNativeApply(t *testing.T) {
	s, g, digest, dir := sweepFixture(t, false)
	callMarker := setupNativeApply(t, s, dir)
	runSweepFixture(t, s, g, digest)
	v := s.Read()
	calls, err := os.ReadFile(callMarker)
	w, readErr := s.Editor.Workspace(context.Background())
	if err != nil || string(calls) != "x" || readErr != nil || w.Pending != nil || v.State != "completed" || v.AppliedState != "applied" || v.ActivePoolCount != 6 || v.AppliedJobState != "completed" || v.AppliedConfigState != "applied" || v.InspectionRequired || v.CanStage {
		t.Fatalf("one verified Apply missing: calls=%q err=%v workspace=%v status=%+v", calls, err, readErr, v)
	}
}

func setupNativeApply(t *testing.T, s *Service, dir string) string {
	t.Helper()
	proc := t.TempDir()
	s.Editor.ProcRoot = proc
	xray := s.Editor.XrayBinary
	writeSweepProcess(t, proc, "101", "500", xray, dir)
	callMarker := filepath.Join(dir, "restart-calls")
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[19] = "S", "600"
	statLine := "102 (xray) " + strings.Join(fields, " ")
	script := filepath.Join(dir, "xkeen")
	body := "#!/bin/sh\n" +
		"[ \"$1\" = -restart ] || exit 9\n" +
		"printf x >> " + shellQuoted(callMarker) + "\n" +
		"mv " + shellQuoted(filepath.Join(proc, "101")) + " " + shellQuoted(filepath.Join(proc, "old")) + "\n" +
		"mkdir " + shellQuoted(filepath.Join(proc, "102")) + "\n" +
		"printf 'xray\\n' > " + shellQuoted(filepath.Join(proc, "102", "comm")) + "\n" +
		"printf '%s\\0' " + shellQuoted(xray) + " run -confdir " + shellQuoted(dir) + " > " + shellQuoted(filepath.Join(proc, "102", "cmdline")) + "\n" +
		"printf '%s\\n' " + shellQuoted(statLine) + " > " + shellQuoted(filepath.Join(proc, "102", "stat")) + "\n" +
		"ln -s " + shellQuoted(xray) + " " + shellQuoted(filepath.Join(proc, "102", "exe")) + "\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	s.Jobs = xkeen.NewJobs(script, s.Lease)
	return callMarker
}

func TestSweepPostApplyReadbackAmbiguitySurvivesPanelRestart(t *testing.T) {
	s, _, _, dir := sweepFixture(t, false)
	marker := setupNativeApply(t, s, dir)
	s.Reader.(*sweepReader).failAfterFile = marker
	if err := s.startSweep(context.Background(), "subscription-refresh"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not settle")
	}
	if !s.Read().InspectionRequired {
		t.Fatal("post-Apply readback ambiguity not fenced")
	}
	q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes)
	if err != nil || !q.InspectionRequired {
		t.Fatal("durable post-Apply fence missing", q, err)
	}
	restarted, _, _, _ := sweepFixture(t, false)
	restarted.QuotaPath = s.QuotaPath
	if err := restarted.startSweep(context.Background(), "periodic"); err == nil || !restarted.Read().InspectionRequired {
		t.Fatal("restart bypassed inspection", err)
	}
}

func TestSweepCleanupAmbiguitySurvivesPanelRestart(t *testing.T) {
	s, _, _, _ := sweepFixture(t, true)
	s.Measurement.(*sweepMeasurement).cleanupBatch = 1
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not settle")
	}
	q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes)
	if err != nil || !q.InspectionRequired {
		t.Fatal("cleanup ambiguity not durable", q, err)
	}
	restarted, _, _, _ := sweepFixture(t, true)
	restarted.QuotaPath = s.QuotaPath
	if err := restarted.Start(context.Background()); err == nil {
		t.Fatal("manual start bypassed durable cleanup fence")
	}
	if err := restarted.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("automatic start bypassed durable cleanup fence")
	}
}

func TestManualAndAutomaticStartsPersistSixHourFloor(t *testing.T) {
	manual, _, _, _ := sweepFixture(t, true)
	if err := manual.Start(context.Background()); err != nil {
		t.Fatal("manual start", err)
	}
	select {
	case <-manual.done:
	case <-time.After(5 * time.Second):
		t.Fatal("manual did not settle")
	}
	q, err := quotaState(manual.QuotaPath, time.Now().UTC(), testReviewBytes)
	if err != nil || q.LastStartedAt.IsZero() {
		t.Fatal("manual start not durable", q, err)
	}
	restarted, _, _, _ := sweepFixture(t, true)
	restarted.QuotaPath = manual.QuotaPath
	if err := restarted.startSweep(context.Background(), "subscription-refresh"); err == nil {
		t.Fatal("restart bypassed manual six-hour floor")
	}
	auto, _, _, _ := sweepFixture(t, true)
	if err := auto.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-auto.done:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic did not settle")
	}
	q, err = quotaState(auto.QuotaPath, time.Now().UTC(), testReviewBytes)
	if err != nil || q.LastStartedAt.IsZero() || q.InspectionRequired {
		t.Fatal("automatic start not settled durably", q, err)
	}
	restarted.QuotaPath = auto.QuotaPath
	if err := restarted.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("restart bypassed automatic six-hour floor")
	}
}

func TestSweepOrphanedSelectorIsAnUnhealthyMemberAndReplaced(t *testing.T) {
	s, _, _, dir := sweepFixture(t, true)
	var selector []string
	var costs []c1.NativeQualityCost
	for _, tag := range []string{"proxy-00", "proxy-01", "proxy-02", "proxy-03", "proxy-04", "proxy-gone"} {
		selector = append(selector, tag)
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: 1})
	}
	routing, _ := json.Marshal(map[string]any{"routing": map[string]any{"rules": []any{}, "balancers": []any{map[string]any{"tag": "bal-proxy", "selector": selector, "strategy": map[string]any{"type": "leastLoad", "settings": map[string]any{"maxRTT": "10s", "costs": costs}}}}}})
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), routing, 0600); err != nil {
		t.Fatal(err)
	}
	if v := s.Read(); v.ActivePoolState != "degraded-orphaned" || len(v.OrphanedPool) != 1 || v.OrphanedPool[0] != "proxy-gone" {
		t.Fatal("orphaned selector not reported", v.ActivePoolState, v.OrphanedPool)
	}
	if err := s.startSweep(context.Background(), "subscription-refresh"); err != nil {
		t.Fatal("orphaned selector blocked the review", err, s.Read().ReviewReason)
	}
	v := waitSweep(t, s)
	if v.PoolDecision != "unhealthy-incumbent-replaced" {
		t.Fatal("orphan was not replaced as an unhealthy member", v.PoolDecision, v.ReviewReason)
	}
	for _, calls := range s.Measurement.(*sweepMeasurement).rttCalls {
		for _, tag := range calls {
			if tag == "proxy-gone" {
				t.Fatal("orphan without an outbound was probed")
			}
		}
	}
	// Once 05 no longer carries the orphan (here a manual repair; the fixture's
	// native job cannot apply), status stops reporting it.
	repaired, _ := json.Marshal(map[string]any{"routing": map[string]any{"rules": []any{}, "balancers": []any{map[string]any{"tag": "bal-proxy", "selector": selector[:5], "strategy": map[string]any{"type": "leastLoad", "settings": map[string]any{"maxRTT": "10s", "costs": costs[:5]}}}}}})
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), repaired, 0600); err != nil {
		t.Fatal(err)
	}
	if after := s.Read(); len(after.OrphanedPool) != 0 || after.ActivePoolState == "degraded-orphaned" {
		t.Fatal("stale orphan list survived the repair", after.OrphanedPool, after.ActivePoolState)
	}
}

func TestManualReviewMeasuresWithoutApplyOrAutomaticQuota(t *testing.T) {
	for _, platform := range []string{"arm64", "mipsle"} {
		t.Run(platform, func(t *testing.T) {
			s, _, digest, _ := sweepFixture(t, false)
			s.Resources.Profile = resourcepolicy.ForPlatform(platform, 1<<20)
			if platform == "mipsle" {
				s.Resources.Profile = resourcepolicy.ForPlatform(platform, 254472)
			}
			if err := s.Start(context.Background()); err != nil {
				t.Fatal("manual review refused", err, s.Read().ReviewReason)
			}
			v := waitSweep(t, s)
			m := s.Measurement.(*sweepMeasurement)
			batch := s.profile().Review().BatchSize
			if !v.ManualSample || v.State != "completed" || v.AppliedState != "not-attempted" || !v.CanStage || len(m.calls) == 0 || len(m.calls[0]) != batch || v.SelectedForSpeed != 12 {
				t.Fatal("manual review state", v.ManualSample, v.State, v.AppliedState, v.CanStage, len(m.calls), v.SelectedForSpeed)
			}
			q, err := quotaState(s.QuotaPath, time.Now().UTC(), s.profile().Review().Bytes)
			if err != nil || q.ReviewsUsed != 0 || q.LastStartedAt.IsZero() || q.FairCursor != v.FairCursor {
				t.Fatal("manual review spent automatic quota or did not persist the rotation", q, err)
			}
			w, err := s.Editor.Workspace(context.Background())
			if err != nil || w.Pending != nil || w.Digest != digest {
				t.Fatal("manual review changed configuration before Stage", err)
			}
			if _, err := s.Stage(context.Background(), digest); err != nil {
				t.Fatal("manual recommendation is not stageable", err)
			}
		})
	}
}


func TestManualReviewFailureDoesNotFenceLaterReviews(t *testing.T) {
	s, _, _, _ := sweepFixture(t, false)
	s.Resources.Profile = resourcepolicy.ForPlatform("arm64", 1<<20)
	s.Measurement.(*sweepMeasurement).cleanupBatch = 1
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := waitSweep(t, s)
	if v.State != "failed" || v.InspectionRequired {
		t.Fatal("manual measurement failure fenced the service", v.State, v.InspectionRequired)
	}
	s.Measurement.(*sweepMeasurement).cleanupBatch = 0
	if err := s.Start(context.Background()); err != nil {
		t.Fatal("next manual review refused", err)
	}
	waitSweep(t, s)
}

func TestSuccessiveManualReviewsRotateCandidates(t *testing.T) {
	s, _, _, _ := sweepFixtureCount(t, false, 46)
	s.Resources.Profile = resourcepolicy.ForPlatform("arm64", 1<<20)
	var rounds [][]string
	for i := 0; i < 2; i++ {
		if err := s.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitSweep(t, s)
		m := s.Measurement.(*sweepMeasurement)
		var probed []string
		for _, chunk := range m.rttCalls {
			probed = append(probed, chunk...)
		}
		rounds = append(rounds, probed)
		m.rttCalls, m.calls = nil, nil
	}
	first := map[string]bool{}
	for _, tag := range rounds[0][1:] {
		first[tag] = true
	}
	for _, tag := range rounds[1][1:] {
		if first[tag] {
			t.Fatal("the second manual review repeated a rotating candidate", tag, rounds)
		}
	}
}
