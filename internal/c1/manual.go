package c1

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	ManualMode                      = "manual-node"
	ManualMaxDownloadBytes    int64 = 32 * MiB
	ManualMaxUploadBytes      int64 = 16 * MiB
	ManualMaxWallTime               = 45 * time.Second
	ManualCleanupReserve            = 3 * time.Second
	ManualWorkTimeout               = ManualMaxWallTime - ManualCleanupReserve
	ManualStageTimeout              = 8 * time.Second
	ManualLatencyTimeout            = 2 * time.Second
	ManualLatencySamples            = 3
	ManualMinimumRateDuration       = 250 * time.Millisecond
	ManualEarlyStopDuration         = time.Second
	manualUploadResponseLimit       = 64 * KiB
	ManualPlannedStages             = ManualLatencySamples + 4 + 4
)

var (
	ErrManualBusy           = errors.New("manual performance is already running or lifecycle is busy")
	ErrManualUnavailable    = errors.New("manual performance unavailable")
	ErrManualInvalidTarget  = errors.New("manual performance target is invalid")
	ErrManualCleanupPending = errors.New("manual performance cleanup is pending")
)

var manualDownloadStages = [...]int64{1 * MiB, 3 * MiB, 8 * MiB, 20 * MiB}
var manualUploadStages = [...]int64{1 * MiB, 3 * MiB, 4 * MiB, 8 * MiB}

func idleManualPerformanceStatus() ManualPerformanceStatus {
	return ManualPerformanceStatus{
		Mode:          ManualMode,
		State:         "idle",
		Phase:         "done",
		PlannedStages: ManualPlannedStages,
		BytesPlanned:  ManualMaxDownloadBytes + ManualMaxUploadBytes,
	}
}

func DefaultManualPerformanceStatus() ManualPerformanceStatus { return idleManualPerformanceStatus() }

// ManualTransfer is the bounded result of one fixed transfer stage. Bytes are
// measured from the local request/response path and never represent a
// caller-supplied payload or endpoint.
type ManualTransfer struct {
	Bytes    int64
	Duration time.Duration
}

// ManualMeasurementTransport is deliberately a tiny internal seam. The
// production implementation is fixedManualTransport; tests inject a synthetic
// adapter without contacting the public provider.
type ManualMeasurementTransport interface {
	Latency(context.Context) (time.Duration, error)
	Download(context.Context, int64) (ManualTransfer, error)
	Upload(context.Context, int64) (ManualTransfer, error)
}

// ManualPerformanceStatus is the bounded RAM-only projection for one manual
// diagnostic. It contains only a safe node identity and fixed-plan metrics.
type ManualPerformanceStatus struct {
	Mode             string    `json:"mode"`
	State            string    `json:"state"`
	Phase            string    `json:"phase"`
	TargetNodeID     string    `json:"targetNodeId,omitempty"`
	TargetTag        string    `json:"targetTag,omitempty"`
	StartedAt        time.Time `json:"startedAt,omitempty"`
	ElapsedMS        int64     `json:"elapsedMs"`
	PlannedStages    int       `json:"plannedStages"`
	CompletedStages  int       `json:"completedStages"`
	CurrentStage     string    `json:"currentStage,omitempty"`
	BytesPlanned     int64     `json:"bytesPlanned"`
	BytesTransferred int64     `json:"bytesTransferred"`
	LatencyMS        *int64    `json:"latencyMs,omitempty"`
	DownloadBPS      *float64  `json:"downloadBps,omitempty"`
	UploadBPS        *float64  `json:"uploadBps,omitempty"`
	ErrorCode        string    `json:"errorCode,omitempty"`
}

// ManualNodeRunner executes the fixed one-node diagnostic under one existing
// ProbeRouter lease. The coordinator owns admission and lifecycle cancellation.
type ManualNodeRunner struct {
	Probe     *ProbeRouter
	Transport ManualMeasurementTransport
	Now       func() time.Time
}

func NewManualNodeRunner(probe *ProbeRouter) *ManualNodeRunner {
	return &ManualNodeRunner{Probe: probe, Transport: newFixedManualTransport()}
}

func (r *ManualNodeRunner) Run(parent context.Context, node NodeState, publish func(ManualPerformanceStatus)) ManualPerformanceStatus {
	if parent == nil {
		parent = context.Background()
	}
	now := time.Now
	if r != nil && r.Now != nil {
		now = r.Now
	}
	started := now().UTC()
	validTarget := validManualNode(node)
	status := ManualPerformanceStatus{
		Mode:          ManualMode,
		State:         "running",
		Phase:         "latency",
		StartedAt:     started,
		PlannedStages: ManualPlannedStages,
		BytesPlanned:  ManualMaxDownloadBytes + ManualMaxUploadBytes,
	}
	if validTarget {
		status.TargetNodeID = node.ID
		status.TargetTag = node.Tag
	}
	emit := func() {
		if publish != nil {
			publish(status)
		}
	}
	emit()

	if !validTarget {
		return manualTerminal(status, "failed", "done", "invalid-target", emit)
	}
	if r == nil || r.Probe == nil {
		return manualTerminal(status, "failed", "done", "probe-unavailable", emit)
	}
	transport := r.Transport
	if transport == nil {
		transport = newFixedManualTransport()
	}
	workContext, cancel := context.WithTimeout(parent, ManualWorkTimeout)
	defer cancel()

	execution := &manualExecution{status: &status, emit: emit, transport: transport}
	err := r.Probe.WithTarget(workContext, "manual-node", node.Tag, func(probeContext context.Context) error {
		return execution.run(probeContext)
	})
	if r.Probe.Blocked() || errors.Is(err, ErrProbeCleanup) || errors.Is(err, ErrProbeBlocked) {
		status.Phase = "cleanup"
		status.CurrentStage = ""
		status.State = "cleanup-pending"
		status.ErrorCode = "probe-cleanup"
		status.ElapsedMS = elapsedMilliseconds(started, now())
		emit()
		return status
	}
	if err != nil && status.State == "running" {
		state, code := manualContextOutcome(err, workContext)
		status.State = state
		status.Phase = "cleanup"
		status.CurrentStage = ""
		status.ErrorCode = code
		status.ElapsedMS = elapsedMilliseconds(started, now())
		emit()
		return status
	}
	if status.State == "running" {
		status.State = "failed"
		status.Phase = "done"
		status.ErrorCode = "probe-unavailable"
	}
	status.ElapsedMS = elapsedMilliseconds(started, now())
	emit()
	return status
}

type manualExecution struct {
	status    *ManualPerformanceStatus
	emit      func()
	transport ManualMeasurementTransport
	firstErr  string
	latencies []time.Duration
	download  []manualCompleteStage
	upload    []manualCompleteStage
}

type manualCompleteStage struct {
	bytes    int64
	duration time.Duration
}

func (e *manualExecution) run(ctx context.Context) error {
	if err := e.ensureActive(ctx); err != nil {
		return err
	}
	e.setPhase("latency")
	for index := 0; index < ManualLatencySamples; index++ {
		if err := e.ensureActive(ctx); err != nil {
			return err
		}
		e.status.CurrentStage = latencyStageName(index)
		e.emit()
		stageStarted := time.Now()
		stageContext, cancel, ok := boundedStageContext(ctx, ManualLatencyTimeout)
		if !ok {
			return e.ensureActive(ctx)
		}
		latency, err := e.transport.Latency(stageContext)
		stageContextErr := stageContext.Err()
		cancel()
		if latency <= 0 {
			latency = time.Since(stageStarted)
		}
		if err == nil && stageContextErr == nil && latency > 0 {
			e.latencies = append(e.latencies, latency)
			if len(e.latencies) >= 2 {
				value := medianDuration(e.latencies).Milliseconds()
				e.status.LatencyMS = int64Pointer(value)
			}
		}
		e.status.CompletedStages++
		e.status.CurrentStage = ""
		e.emit()
	}
	if len(e.latencies) < 2 {
		e.setFirstError("latency-failed")
	}

	if err := e.runDirection(ctx, "download", manualDownloadStages[:], func(stageContext context.Context, payload int64) (ManualTransfer, error) {
		return e.transport.Download(stageContext, payload)
	}); err != nil {
		return err
	}
	if err := e.runDirection(ctx, "upload", manualUploadStages[:], func(stageContext context.Context, payload int64) (ManualTransfer, error) {
		return e.transport.Upload(stageContext, payload)
	}); err != nil {
		return err
	}

	if e.firstErr != "" {
		e.status.State = "failed"
		e.status.ErrorCode = e.firstErr
	} else {
		e.status.State = "completed"
		e.status.ErrorCode = ""
	}
	e.status.Phase = "done"
	e.status.CurrentStage = ""
	e.emit()
	return nil
}

func (e *manualExecution) runDirection(ctx context.Context, direction string, stages []int64, transfer func(context.Context, int64) (ManualTransfer, error)) error {
	e.setPhase(direction)
	directionFailed := false
	var complete *[]manualCompleteStage
	if direction == "download" {
		complete = &e.download
	} else {
		complete = &e.upload
	}
	for index, payload := range stages {
		if err := e.ensureActive(ctx); err != nil {
			return err
		}
		e.status.CurrentStage = transferStageName(direction, index, payload)
		e.emit()
		stageStarted := time.Now()
		stageContext, cancel, ok := boundedStageContext(ctx, ManualStageTimeout)
		if !ok {
			return e.ensureActive(ctx)
		}
		result, err := transfer(stageContext, payload)
		stageContextErr := stageContext.Err()
		cancel()
		if err == nil && stageContextErr != nil {
			err = stageContextErr
		}
		if result.Duration <= 0 {
			result.Duration = time.Since(stageStarted)
		}
		reportedBytes := result.Bytes
		if reportedBytes < 0 {
			reportedBytes = 0
			err = errors.New("negative transfer result")
		}
		if reportedBytes > payload {
			reportedBytes = payload
			err = errors.New("oversized transfer result")
		}
		remaining := ManualMaxDownloadBytes + ManualMaxUploadBytes - e.status.BytesTransferred
		if reportedBytes > remaining {
			reportedBytes = maxInt64(remaining, 0)
			err = errors.New("manual transfer ceiling exceeded")
		}
		e.status.BytesTransferred += reportedBytes
		valid := err == nil && stageContextErr == nil && reportedBytes == payload && result.Duration > 0
		if !valid && ctx.Err() == nil {
			directionFailed = true
		}
		if valid {
			*complete = append(*complete, manualCompleteStage{bytes: payload, duration: result.Duration})
			e.projectDirection(direction, *complete)
		}
		e.status.CompletedStages++
		e.status.CurrentStage = ""
		e.emit()
		if err := e.ensureActive(ctx); err != nil {
			return err
		}
		if valid && result.Duration >= ManualEarlyStopDuration {
			break
		}
	}
	if len(*complete) == 0 {
		e.setFirstError(direction + "-failed")
	} else if directionFailed {
		e.setFirstError("transport-failure")
	}
	return nil
}

func (e *manualExecution) projectDirection(direction string, stages []manualCompleteStage) {
	rate, ok := aggregateManualRate(stages)
	if !ok {
		return
	}
	if direction == "download" {
		e.status.DownloadBPS = floatPointer(rate)
	} else {
		e.status.UploadBPS = floatPointer(rate)
	}
}

func aggregateManualRate(stages []manualCompleteStage) (float64, bool) {
	if len(stages) == 0 {
		return 0, false
	}
	eligible := make([]manualCompleteStage, 0, len(stages))
	for _, stage := range stages {
		if stage.bytes <= 0 || stage.duration <= 0 {
			continue
		}
		if stage.duration >= ManualMinimumRateDuration {
			eligible = append(eligible, stage)
		}
	}
	if len(eligible) == 0 {
		largest := stages[0]
		for _, stage := range stages[1:] {
			if stage.bytes > largest.bytes {
				largest = stage
			}
		}
		eligible = append(eligible, largest)
	}
	rates := make([]float64, 0, len(eligible))
	if len(eligible) == 1 && eligible[0].duration < ManualMinimumRateDuration {
		rate := float64(eligible[0].bytes) / eligible[0].duration.Seconds()
		return rate, finitePositive(rate)
	}
	for _, stage := range eligible {
		rate := float64(stage.bytes) / stage.duration.Seconds()
		if finitePositive(rate) {
			rates = append(rates, rate)
		}
	}
	if len(rates) == 0 {
		return 0, false
	}
	sort.Float64s(rates)
	return rates[len(rates)/2], finitePositive(rates[len(rates)/2])
}

func (e *manualExecution) ensureActive(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		e.status.Phase = "cleanup"
		e.status.CurrentStage = ""
		if errors.Is(err, context.DeadlineExceeded) {
			e.status.State = "failed"
			e.status.ErrorCode = "timeout"
		} else {
			e.status.State = "cancelled"
			e.status.ErrorCode = "cancelled"
		}
		e.emit()
		return err
	}
	return nil
}

func (e *manualExecution) setPhase(phase string) {
	e.status.Phase = phase
	e.status.CurrentStage = ""
	e.emit()
}

func (e *manualExecution) setFirstError(code string) {
	if e.firstErr == "" {
		e.firstErr = code
	}
}

func manualTerminal(status ManualPerformanceStatus, state, phase, code string, emit func()) ManualPerformanceStatus {
	status.State = state
	status.Phase = phase
	status.CurrentStage = ""
	status.ErrorCode = code
	status.ElapsedMS = elapsedMilliseconds(status.StartedAt, time.Now().UTC())
	if emit != nil {
		emit()
	}
	return status
}

func manualContextOutcome(err error, ctx context.Context) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "failed", "timeout"
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled", "cancelled"
	}
	return "failed", "probe-unavailable"
}

func boundedStageContext(parent context.Context, maximum time.Duration) (context.Context, context.CancelFunc, bool) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return parent, func() {}, false
	}
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return parent, func() {}, false
		}
		if remaining < maximum {
			maximum = remaining
		}
	}
	if maximum <= 0 {
		return parent, func() {}, false
	}
	ctx, cancel := context.WithTimeout(parent, maximum)
	return ctx, cancel, true
}

func validManualNode(node NodeState) bool {
	return validManualNodeID(node.ID) && node.Enabled && node.Tag == "proxy-"+node.ID && validTag(node.Tag)
}

// ValidManualNodeID keeps the HTTP boundary aligned with the Coordinator's
// safe target identity contract. Canonical tag and enabled-state validation
// still happen against the current authoritative node inside the Coordinator.
func ValidManualNodeID(value string) bool { return validManualNodeID(value) }

func validManualNodeID(value string) bool {
	if len(value) < len("node-00000000") || len(value) > 64 || !strings.HasPrefix(value, "node-") {
		return false
	}
	for _, character := range value[len("node-"):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func latencyStageName(index int) string {
	return "latency-" + formatInt64(int64(index+1))
}

func transferStageName(direction string, index int, payload int64) string {
	return direction + "-" + formatInt64(int64(index+1)) + "-" + formatInt64(payload/MiB) + "MiB"
}

func medianDuration(values []time.Duration) time.Duration {
	copyValues := append([]time.Duration(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	return copyValues[len(copyValues)/2]
}

func elapsedMilliseconds(started, now time.Time) int64 {
	if started.IsZero() || now.Before(started) {
		return 0
	}
	return now.Sub(started).Milliseconds()
}

func int64Pointer(value int64) *int64     { return &value }
func floatPointer(value float64) *float64 { return &value }
func maxInt64(value, fallback int64) int64 {
	if value < fallback {
		return fallback
	}
	return value
}
func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

type fixedManualTransport struct {
	roundTripper http.RoundTripper
}

func newFixedManualTransport() *fixedManualTransport { return &fixedManualTransport{} }

func newFixedManualTransportWithRoundTripper(roundTripper http.RoundTripper) *fixedManualTransport {
	return &fixedManualTransport{roundTripper: roundTripper}
}

func (t *fixedManualTransport) Latency(ctx context.Context) (time.Duration, error) {
	started := time.Now()
	response, closeTransport, err := t.request(ctx, http.MethodGet, fixedDownloadURL(0), nil, 0)
	if err != nil {
		return time.Since(started), err
	}
	defer closeTransport()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return time.Since(started), errors.New("fixed latency response status is not 200")
	}
	return time.Since(started), nil
}

func (t *fixedManualTransport) Download(ctx context.Context, payload int64) (ManualTransfer, error) {
	started := time.Now()
	response, closeTransport, err := t.request(ctx, http.MethodGet, fixedDownloadURL(payload), nil, 0)
	if err != nil {
		return ManualTransfer{Duration: time.Since(started)}, err
	}
	defer closeTransport()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ManualTransfer{Duration: time.Since(started)}, errors.New("fixed download response status is not 200")
	}
	bytesRead, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, payload))
	duration := time.Since(started)
	if readErr != nil {
		return ManualTransfer{Bytes: bytesRead, Duration: duration}, readErr
	}
	if bytesRead != payload {
		return ManualTransfer{Bytes: bytesRead, Duration: duration}, errors.New("fixed download body is incomplete")
	}
	return ManualTransfer{Bytes: bytesRead, Duration: duration}, nil
}

func (t *fixedManualTransport) Upload(ctx context.Context, payload int64) (ManualTransfer, error) {
	started := time.Now()
	body := io.LimitReader(zeroByteReader{}, payload)
	response, closeTransport, err := t.request(ctx, http.MethodPost, fixedUploadURL(), body, payload)
	if err != nil {
		return ManualTransfer{Duration: time.Since(started)}, err
	}
	defer closeTransport()
	defer response.Body.Close()
	duration := time.Since(started)
	if response.StatusCode != http.StatusOK {
		return ManualTransfer{Duration: duration}, errors.New("fixed upload response status is not 200")
	}
	responseBytes, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, manualUploadResponseLimit+1))
	if readErr != nil {
		return ManualTransfer{Duration: time.Since(started)}, readErr
	}
	if responseBytes > manualUploadResponseLimit {
		return ManualTransfer{Duration: time.Since(started)}, errors.New("fixed upload response is too large")
	}
	return ManualTransfer{Bytes: payload, Duration: time.Since(started)}, nil
}

func (t *fixedManualTransport) request(ctx context.Context, method, rawURL string, body io.Reader, contentLength int64) (*http.Response, func(), error) {
	request, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, func() {}, err
	}
	if contentLength > 0 {
		request.ContentLength = contentLength
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Connection", "close")
	client, closeTransport := t.client()
	response, err := client.Do(request)
	if err != nil {
		closeTransport()
		return nil, func() {}, err
	}
	return response, closeTransport, nil
}

func (t *fixedManualTransport) client() (*http.Client, func()) {
	if t != nil && t.roundTripper != nil {
		return &http.Client{
			Transport: t.roundTripper,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}, func() {}
	}
	proxyURL := &url.URL{Scheme: "http", Host: ProbeAddress}
	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		DisableKeepAlives: true,
		MaxIdleConns:      0,
		MaxConnsPerHost:   1,
		IdleConnTimeout:   0,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, transport.CloseIdleConnections
}

func fixedDownloadURL(payload int64) string {
	return "https://speed.cloudflare.com/__down?bytes=" + formatInt64(payload)
}

func fixedUploadURL() string { return "https://speed.cloudflare.com/__up" }

type zeroByteReader struct{}

func (zeroByteReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
