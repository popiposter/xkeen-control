package c1

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type manualTransportStub struct {
	mu             sync.Mutex
	calls          []string
	stageDuration  time.Duration
	failedDownload int
}

func (s *manualTransportStub) Latency(context.Context) (time.Duration, error) {
	s.mu.Lock()
	s.calls = append(s.calls, "latency")
	s.mu.Unlock()
	return s.stageDuration, nil
}

func (s *manualTransportStub) Download(_ context.Context, payload int64) (ManualTransfer, error) {
	s.mu.Lock()
	index := 0
	for _, call := range s.calls {
		if strings.HasPrefix(call, "download") {
			index++
		}
	}
	s.calls = append(s.calls, "download-"+formatInt64(payload))
	s.mu.Unlock()
	if index == s.failedDownload {
		return ManualTransfer{Bytes: payload / 2, Duration: s.stageDuration}, errors.New("synthetic transfer failure")
	}
	return ManualTransfer{Bytes: payload, Duration: s.stageDuration}, nil
}

func (s *manualTransportStub) Upload(_ context.Context, payload int64) (ManualTransfer, error) {
	s.mu.Lock()
	s.calls = append(s.calls, "upload-"+formatInt64(payload))
	s.mu.Unlock()
	return ManualTransfer{Bytes: payload, Duration: s.stageDuration}, nil
}

func validManualTestNode() NodeState {
	return NodeState{ID: "node-00000001", Tag: "proxy-node-00000001", Enabled: true}
}

func TestManualRunnerUsesFixedPlanAndBoundedStreamingResults(t *testing.T) {
	if ManualWorkTimeout+ManualCleanupReserve != ManualMaxWallTime {
		t.Fatalf("manual timing envelope = work %s + cleanup %s != wall %s", ManualWorkTimeout, ManualCleanupReserve, ManualMaxWallTime)
	}
	api := &benchmarkProbeAPI{}
	transport := &manualTransportStub{stageDuration: 300 * time.Millisecond, failedDownload: -1}
	runner := &ManualNodeRunner{Probe: NewProbeRouter(api), Transport: transport}
	var progress []ManualPerformanceStatus
	result := runner.Run(context.Background(), validManualTestNode(), func(status ManualPerformanceStatus) {
		progress = append(progress, status)
	})
	if result.State != "completed" || result.Phase != "done" || result.ErrorCode != "" {
		t.Fatalf("manual result = %+v", result)
	}
	if result.CompletedStages != ManualPlannedStages || result.BytesTransferred != ManualMaxDownloadBytes+ManualMaxUploadBytes {
		t.Fatalf("manual progress totals = %+v", result)
	}
	if result.LatencyMS == nil || result.DownloadBPS == nil || result.UploadBPS == nil {
		t.Fatalf("manual metrics missing = %+v", result)
	}
	if result.PlannedStages != 11 || result.BytesPlanned != 48*MiB || len(progress) < 10 {
		t.Fatalf("manual fixed plan/progress = %+v updates=%d", result, len(progress))
	}
	if len(api.adds) != 1 || api.adds[0].RuleTag != ManualPerformanceRuleTag || len(api.removes) != 1 || api.removes[0] != ManualPerformanceRuleTag {
		t.Fatalf("manual probe ownership = adds=%+v removes=%v", api.adds, api.removes)
	}
	transport.mu.Lock()
	calls := append([]string(nil), transport.calls...)
	transport.mu.Unlock()
	if len(calls) != 11 || calls[0] != "latency" || calls[3] != "download-1048576" || calls[6] != "download-20971520" || calls[10] != "upload-8388608" {
		t.Fatalf("manual call plan = %v", calls)
	}
}

func TestManualRunnerEarlyStopsOnlyAfterUsefulCompleteStage(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &manualTransportStub{stageDuration: ManualEarlyStopDuration, failedDownload: -1}
	runner := &ManualNodeRunner{Probe: NewProbeRouter(api), Transport: transport}
	result := runner.Run(context.Background(), validManualTestNode(), nil)
	if result.State != "completed" || result.CompletedStages != ManualLatencySamples+2 || result.BytesTransferred != 2*MiB {
		t.Fatalf("early-stop result = %+v", result)
	}
	transport.mu.Lock()
	calls := append([]string(nil), transport.calls...)
	transport.mu.Unlock()
	if len(calls) != ManualLatencySamples+2 || calls[3] != "download-1048576" || calls[4] != "upload-1048576" {
		t.Fatalf("early-stop calls = %v", calls)
	}
}

func TestManualRunnerKeepsEarlierValidStagesAfterOneFailure(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &manualTransportStub{stageDuration: 300 * time.Millisecond, failedDownload: 0}
	runner := &ManualNodeRunner{Probe: NewProbeRouter(api), Transport: transport}
	result := runner.Run(context.Background(), validManualTestNode(), nil)
	if result.State != "failed" || result.ErrorCode != "transport-failure" || result.DownloadBPS == nil || result.UploadBPS == nil {
		t.Fatalf("failed-stage result = %+v", result)
	}
	if result.BytesTransferred != ManualMaxDownloadBytes+ManualMaxUploadBytes-(1*MiB/2) {
		t.Fatalf("failed-stage byte accounting = %+v", result)
	}
}

type recordingManualRoundTripper struct {
	mu             sync.Mutex
	requests       []manualRequestRecord
	status         int
	responseBytes  int64
	responseReader func(int64) io.Reader
}

type manualRequestRecord struct {
	method         string
	url            string
	rawQuery       string
	contentLength  int64
	bodyBytes      int64
	acceptEncoding string
	connection     string
}

func (r *recordingManualRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	record := manualRequestRecord{method: request.Method, url: request.URL.String(), rawQuery: request.URL.RawQuery, contentLength: request.ContentLength, acceptEncoding: request.Header.Get("Accept-Encoding"), connection: request.Header.Get("Connection")}
	if request.Body != nil {
		record.bodyBytes, _ = io.Copy(io.Discard, request.Body)
	}
	r.mu.Lock()
	r.requests = append(r.requests, record)
	r.mu.Unlock()
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	responseBytes := r.responseBytes
	if request.URL.Path == "/__down" && responseBytes == 0 {
		responseBytes, _ = strconv.ParseInt(request.URL.Query().Get("bytes"), 10, 64)
	}
	body := io.Reader(bytes.NewReader(nil))
	if r.responseReader != nil {
		body = r.responseReader(responseBytes)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(body), Request: request}, nil
}

func TestFixedManualTransportConstructsOnlyTheClosedHTTPContract(t *testing.T) {
	transportRecorder := &recordingManualRoundTripper{responseReader: func(size int64) io.Reader { return io.LimitReader(zeroByteReader{}, size) }}
	transport := newFixedManualTransportWithRoundTripper(transportRecorder)
	if _, err := transport.Latency(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Download(context.Background(), MiB); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Upload(context.Background(), MiB); err != nil {
		t.Fatal(err)
	}
	transportRecorder.mu.Lock()
	requests := append([]manualRequestRecord(nil), transportRecorder.requests...)
	transportRecorder.mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("fixed request count = %d", len(requests))
	}
	if requests[0].method != http.MethodGet || requests[0].url != "https://speed.cloudflare.com/__down?bytes=0" || requests[0].rawQuery != "bytes=0" {
		t.Fatalf("latency request = %+v", requests[0])
	}
	if requests[1].method != http.MethodGet || requests[1].url != "https://speed.cloudflare.com/__down?bytes=1048576" || requests[1].contentLength != 0 || requests[1].bodyBytes != 0 {
		t.Fatalf("download request = %+v", requests[1])
	}
	if requests[2].method != http.MethodPost || requests[2].url != "https://speed.cloudflare.com/__up" || requests[2].rawQuery != "" || requests[2].contentLength != MiB || requests[2].bodyBytes != MiB {
		t.Fatalf("upload request = %+v", requests[2])
	}
	for _, request := range requests {
		if request.acceptEncoding != "identity" || request.connection != "close" {
			t.Fatalf("fixed headers = %+v", request)
		}
	}
}

func TestFixedManualTransportRejectsRedirectsAndBoundsUploadResponse(t *testing.T) {
	redirect := newFixedManualTransportWithRoundTripper(&recordingManualRoundTripper{status: http.StatusFound})
	if _, err := redirect.Latency(context.Background()); err == nil {
		t.Fatal("redirect response was accepted")
	}
	response := &recordingManualRoundTripper{responseBytes: manualUploadResponseLimit + 1, responseReader: func(size int64) io.Reader { return io.LimitReader(zeroByteReader{}, size) }}
	bounded := newFixedManualTransportWithRoundTripper(response)
	if _, err := bounded.Upload(context.Background(), MiB); err == nil {
		t.Fatal("oversized upload response was accepted")
	}
}

func TestManualRateUsesMedianAndLargestFastFallback(t *testing.T) {
	median, ok := aggregateManualRate([]manualCompleteStage{{bytes: MiB, duration: 300 * time.Millisecond}, {bytes: 3 * MiB, duration: 600 * time.Millisecond}, {bytes: 8 * MiB, duration: 400 * time.Millisecond}})
	if !ok || median != float64(3*MiB)/(600*time.Millisecond).Seconds() {
		t.Fatalf("median rate = %v %v", median, ok)
	}
	fallback, ok := aggregateManualRate([]manualCompleteStage{{bytes: MiB, duration: 100 * time.Millisecond}, {bytes: 8 * MiB, duration: 100 * time.Millisecond}})
	if !ok || fallback != float64(8*MiB)/(100*time.Millisecond).Seconds() {
		t.Fatalf("fast fallback rate = %v %v", fallback, ok)
	}
}

type blockingManualTransport struct {
	started chan struct{}
}

func (b *blockingManualTransport) Latency(ctx context.Context) (time.Duration, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-ctx.Done()
	return 0, ctx.Err()
}
func (b *blockingManualTransport) Download(ctx context.Context, _ int64) (ManualTransfer, error) {
	<-ctx.Done()
	return ManualTransfer{}, ctx.Err()
}
func (b *blockingManualTransport) Upload(ctx context.Context, _ int64) (ManualTransfer, error) {
	<-ctx.Done()
	return ManualTransfer{}, ctx.Err()
}

func TestCoordinatorManualUsesLegacyPerformanceSingleFlightAndApplyCancellation(t *testing.T) {
	api := &benchmarkProbeAPI{}
	blocking := &blockingManualTransport{started: make(chan struct{})}
	runner := &ManualNodeRunner{Probe: NewProbeRouter(api), Transport: blocking}
	coordinator := NewCoordinator(DefaultPolicy(), nil, &BenchmarkRunner{}, func(context.Context) []NodeState { return []NodeState{validManualTestNode()} })
	coordinator.SetManualRunner(runner)
	if err := coordinator.TriggerManualNode(validManualTestNode().ID); err != nil {
		t.Fatal(err)
	}
	<-blocking.started
	if status := coordinator.ManualSnapshot(); status.State != "running" || status.TargetTag != validManualTestNode().Tag {
		t.Fatalf("manual running status = %+v", status)
	}
	if err := coordinator.TriggerBenchmark(); !errors.Is(err, ErrBenchmarkBusy) {
		t.Fatalf("legacy benchmark admission beside manual = %v", err)
	}
	if _, err := coordinator.TryBeginManagedApply(); !errors.Is(err, ErrLifecycleBusy) {
		t.Fatalf("managed apply admission beside manual = %v", err)
	}
	applyContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := coordinator.BeginApply(applyContext)
	if err != nil {
		t.Fatalf("Apply did not cancel manual diagnostic = %v", err)
	}
	status := coordinator.ManualSnapshot()
	if status.State != "cancelled" || status.ErrorCode != "cancelled" {
		t.Fatalf("manual cancellation status = %+v", status)
	}
	release()
	if coordinator.IsLifecycleBusy() {
		t.Fatal("shared lifecycle remained busy after Apply release")
	}
}

func TestCoordinatorManualRechecksEnabledCanonicalTargetBeforeProbe(t *testing.T) {
	api := &benchmarkProbeAPI{}
	transport := &manualTransportStub{stageDuration: 300 * time.Millisecond, failedDownload: -1}
	runner := &ManualNodeRunner{Probe: NewProbeRouter(api), Transport: transport}
	nodes := []NodeState{{ID: "node-00000001", Tag: "proxy-node-00000001", Enabled: false}}
	coordinator := NewCoordinator(DefaultPolicy(), nil, &BenchmarkRunner{}, func(context.Context) []NodeState { return nodes })
	coordinator.SetManualRunner(runner)
	if err := coordinator.TriggerManualNode(nodes[0].ID); !errors.Is(err, ErrManualInvalidTarget) {
		t.Fatalf("disabled target admission = %v", err)
	}
	if len(api.adds) != 0 || coordinator.ManualSnapshot().ErrorCode != "invalid-target" {
		t.Fatalf("invalid target side effects = adds=%d status=%+v", len(api.adds), coordinator.ManualSnapshot())
	}
	if err := coordinator.TriggerManualNode("node-00000002"); !errors.Is(err, ErrManualInvalidTarget) {
		t.Fatalf("unknown target admission = %v", err)
	}
}

func TestCoordinatorManualCleanupFailureBlocksUnsafeReuse(t *testing.T) {
	api := &benchmarkProbeAPI{failRemove: true}
	transport := &manualTransportStub{stageDuration: 300 * time.Millisecond, failedDownload: -1}
	coordinator := NewCoordinator(DefaultPolicy(), nil, &BenchmarkRunner{}, func(context.Context) []NodeState { return []NodeState{validManualTestNode()} })
	coordinator.SetManualRunner(&ManualNodeRunner{Probe: NewProbeRouter(api), Transport: transport})
	if err := coordinator.TriggerManualNode(validManualTestNode().ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if status := coordinator.ManualSnapshot(); status.State == "cleanup-pending" {
			if err := coordinator.TriggerManualNode(validManualTestNode().ID); !errors.Is(err, ErrManualCleanupPending) {
				t.Fatalf("reuse after cleanup failure = %v", err)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("cleanup did not become pending: %+v", coordinator.ManualSnapshot())
}
