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
}

func (r *sweepReader) Snapshot(context.Context) xrayapi.Snapshot {
	healthy := r.healthy
	if r.failAfterFile != "" {
		if _, err := os.Stat(r.failAfterFile); err == nil {
			healthy = false
		}
	}
	snapshot := xrayapi.Snapshot{APIReachable: healthy, RoutingReachable: healthy, ObservatoryReachable: healthy, Balancer: xrayapi.BalancerState{NativeSelected: "proxy-00"}}
	for i := 0; i < 14; i++ {
		snapshot.OutboundHealth = append(snapshot.OutboundHealth, xrayapi.OutboundHealth{Tag: fmt.Sprintf("proxy-%02d", i), Alive: true, DelayMS: int64(100 + i), LastTry: time.Now().UTC().Add(-time.Second)})
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
	if v.ReviewTrigger != "subscription-refresh" || v.EligibleCount != len(g.Candidates) || v.AttemptedCount != 14 || v.BatchCount != 5 || v.AppliedState != "no-op" {
		t.Fatal("admission/coverage mismatch", v)
	}
	if release, err := acquireQuotaLock(first.QuotaPath); err != nil {
		t.Fatal("quota lock leaked", err)
	} else {
		release()
	}
}

func sweepFixture(t *testing.T, noop bool) (*Service, c1.AdaptiveGeneration, string, string) {
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
	for i := 0; i < 14; i++ {
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
	s := &Service{Editor: e, Lease: lease, Reader: &sweepReader{healthy: true}, Nodes: func(context.Context) []c1.NodeState {
		result := make([]c1.NodeState, 0, len(pool))
		for _, tag := range pool {
			result = append(result, c1.NodeState{Tag: tag, Enabled: true})
		}
		return result
	}, Measurement: m, Resources: guard, Jobs: xkeen.NewJobs(filepath.Join(dir, "missing-xkeen"), lease), QuotaPath: filepath.Join(t.TempDir(), "quota", "receipt.json"), pool: pool, batchPause: time.Millisecond, status: Status{State: "running", Digest: w.Digest, EligibleCount: 14, ActivePoolCount: len(selector), AppliedState: "not-attempted"}}
	return s, c1.AdaptiveGeneration{Generation: 1, NativeQuality: true, Candidates: candidates}, w.Digest, dir
}

func runSweepFixture(t *testing.T, s *Service, g c1.AdaptiveGeneration, digest string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan struct{})
	s.runSweep(ctx, cancel, done, g, g.Candidates, digest, nil)
	select {
	case <-done:
	default:
		t.Fatal("sweep did not settle")
	}
}

func TestSweepFourteenNodesFiveBatchesNoopAndConsumed(t *testing.T) {
	s, g, digest, _ := sweepFixture(t, true)
	runSweepFixture(t, s, g, digest)
	m := s.Measurement.(*sweepMeasurement)
	if len(m.calls) != 5 || len(m.calls[0]) != 3 || len(m.calls[4]) != 2 {
		t.Fatal("wrong bounded batch plan", m.calls)
	}
	v := s.Read()
	if v.State != "completed" || v.AppliedState != "no-op" || v.AttemptedCount != 14 || v.ValidCount != 14 || v.BatchCount != 5 || len(v.Progress.Candidates) != 14 || v.CanStage || v.ActivePoolCount != 6 || v.AggregateBytes > maxSweepBytes {
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
				m.validLimit = 11
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
	q, err := quotaState(s.QuotaPath, time.Now().UTC())
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
	q, err := quotaState(s.QuotaPath, time.Now().UTC())
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
	q, err := quotaState(manual.QuotaPath, time.Now().UTC())
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
	q, err = quotaState(auto.QuotaPath, time.Now().UTC())
	if err != nil || q.LastStartedAt.IsZero() || q.InspectionRequired {
		t.Fatal("automatic start not settled durably", q, err)
	}
	restarted.QuotaPath = auto.QuotaPath
	if err := restarted.startSweep(context.Background(), "periodic"); err == nil {
		t.Fatal("restart bypassed automatic six-hour floor")
	}
}
