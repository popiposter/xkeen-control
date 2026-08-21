package c1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync/atomic"
	"time"
)

type BenchmarkNodeResult struct {
	Tag            string
	Valid          bool
	Bytes          int64
	Duration       time.Duration
	BytesPerSecond float64
	ErrorClass     string
}

type BenchmarkResult struct {
	Generation      uint64
	StartedAt       time.Time
	CompletedAt     time.Time
	EligibleNodes   int
	AttemptedNodes  int
	ValidSamples    int
	AggregateBytes  int64
	Duration        time.Duration
	PayloadBytes    int64
	PerNodeTimeout  time.Duration
	MaximumWallTime time.Duration
	ResultClass     string
	SwitchAllowed   bool
	CurrentValid    bool
	CleanupPending  bool
	Samples         map[string]ThroughputSample
	Nodes           []BenchmarkNodeResult
}

type BenchmarkRunner struct {
	Policy     Policy
	Probe      *ProbeRouter
	ProxyAddr  string
	Endpoint   string
	Store      BenchmarkStore
	Generation atomic.Uint64
	HTTPDo     func(context.Context, string, string, int64, time.Duration) (int64, time.Duration, error)
}

func NewBenchmarkRunner(policy Policy, probe *ProbeRouter, store BenchmarkStore) *BenchmarkRunner {
	policy = policy.normalized()
	return &BenchmarkRunner{Policy: policy, Probe: probe, ProxyAddr: ProbeAddress, Endpoint: policy.BenchmarkEndpoint, Store: store}
}

func (r *BenchmarkRunner) Run(ctx context.Context, nodes []NodeState, current string) BenchmarkResult {
	started := time.Now().UTC()
	result := BenchmarkResult{StartedAt: started, Samples: make(map[string]ThroughputSample)}
	if r == nil {
		result.ResultClass = "unavailable"
		return result
	}
	policy := r.Policy.normalized()
	eligible := EligibleTags(nodes, policy.RegistryMaximum)
	result.EligibleNodes = len(eligible)
	plan, planErr := policy.Plan(len(eligible))
	result.PayloadBytes, result.PerNodeTimeout, result.MaximumWallTime = plan.PayloadBytesPerNode, plan.PerNodeTimeout, plan.MaximumWallTime
	if planErr != nil {
		if errors.Is(planErr, ErrInsufficientBudget) {
			result.ResultClass = "insufficient-budget"
		} else {
			result.ResultClass = "policy-error"
		}
		result.CompletedAt = time.Now().UTC()
		return result
	}
	result.Generation = r.Generation.Add(1)
	if len(eligible) == 0 {
		result.ResultClass = "empty"
		result.CompletedAt = time.Now().UTC()
		return result
	}
	if r.Probe == nil {
		result.ResultClass = "unavailable"
		result.CompletedAt = time.Now().UTC()
		return result
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = policy.BenchmarkEndpoint
	}
	if !trustedEndpoint(endpoint) || endpoint != policy.BenchmarkEndpoint || endpoint != DefaultBenchmarkEndpoint {
		result.ResultClass = "policy-error"
		result.CompletedAt = time.Now().UTC()
		return result
	}
	for _, tag := range eligible {
		if err := ctx.Err(); err != nil {
			result.ResultClass = "cancelled"
			break
		}
		result.AttemptedNodes++
		startedNode := time.Now()
		var bytesRead int64
		var duration time.Duration
		var sampleErr error
		err := r.Probe.WithTarget(ctx, "benchmark", tag, func(sampleContext context.Context) error {
			if r.HTTPDo != nil {
				bytesRead, duration, sampleErr = r.HTTPDo(sampleContext, endpoint, r.ProxyAddr, plan.PayloadBytesPerNode, plan.PerNodeTimeout)
				return sampleErr
			}
			bytesRead, duration, sampleErr = streamSample(sampleContext, endpoint, r.ProxyAddr, plan.PayloadBytesPerNode, plan.PerNodeTimeout)
			return sampleErr
		})
		if duration <= 0 {
			duration = time.Since(startedNode)
		}
		nodeResult := BenchmarkNodeResult{Tag: tag, Bytes: bytesRead, Duration: duration}
		if err != nil {
			nodeResult.ErrorClass = classifyBenchmarkError(err)
		} else if bytesRead > 0 {
			nodeResult.Valid = true
			nodeResult.BytesPerSecond = float64(bytesRead) / duration.Seconds()
			result.ValidSamples++
			result.AggregateBytes += bytesRead
			result.Samples[tag] = ThroughputSample{Valid: true, BytesPerSecond: nodeResult.BytesPerSecond}
		} else {
			nodeResult.ErrorClass = "zero-bytes"
		}
		result.Nodes = append(result.Nodes, nodeResult)
		if err != nil && errors.Is(err, ErrProbeCleanup) {
			result.CleanupPending = true
			result.ResultClass = "cleanup-pending"
			break
		}
	}
	result.CurrentValid = result.Samples[current].Valid
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)
	if result.ResultClass == "" {
		if result.AttemptedNodes != result.EligibleNodes {
			result.ResultClass = "incomplete"
		} else {
			result.ResultClass = "completed"
		}
	}
	result.SwitchAllowed = result.ResultClass == "completed" && !result.CleanupPending && result.CurrentValid
	if result.ResultClass == "completed" && !result.CleanupPending {
		if err := r.Store.Save(BenchmarkSnapshot{Generation: result.Generation, CompletedAt: result.CompletedAt, EligibleNodes: result.EligibleNodes, ValidSamples: result.ValidSamples, AggregateBytes: result.AggregateBytes, DurationMS: result.Duration.Milliseconds(), PayloadBytes: result.PayloadBytes, PerNodeTimeoutMS: result.PerNodeTimeout.Milliseconds(), ResultClass: result.ResultClass, SelectionTarget: current, Samples: throughputStatuses(result.Samples)}); err != nil {
			result.ResultClass = "persistence-failure"
			result.SwitchAllowed = false
		}
	}
	return result
}

func streamSample(parent context.Context, endpoint, proxyAddr string, payload int64, timeout time.Duration) (int64, time.Duration, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	proxyURL, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		return 0, 0, err
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true, MaxIdleConns: 0, MaxConnsPerHost: 1, IdleConnTimeout: 0}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Connection", "close")
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, time.Since(started), err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, time.Since(started), errors.New("benchmark HTTP status is not 200")
	}
	bytesRead, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, payload))
	duration := time.Since(started)
	if bytesRead == 0 {
		if readErr != nil {
			return 0, duration, readErr
		}
		return 0, duration, errors.New("benchmark returned zero bytes")
	}
	// A bounded timeout with an HTTP 200 response and useful transferred bytes
	// is an intentionally valid partial sustained sample.
	if readErr != nil && ctx.Err() == nil {
		return bytesRead, duration, readErr
	}
	return bytesRead, duration, nil
}

// HTTPProbe is the bounded active-node probe used by the supervisor. It is
// deliberately not exposed through the browser/API.
func HTTPProbe(ctx context.Context, endpoint, proxyAddr string, payload int64, timeout time.Duration) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	query := parsed.Query()
	query.Set("bytes", formatInt64(payload))
	parsed.RawQuery = query.Encode()
	bytesRead, _, err := streamSample(ctx, parsed.String(), proxyAddr, payload, timeout)
	if err != nil {
		return err
	}
	if bytesRead <= 0 {
		return errors.New("probe returned zero bytes")
	}
	return nil
}

func formatInt64(value int64) string {
	if value <= 0 {
		return "0"
	}
	result := make([]byte, 0, 20)
	for value > 0 {
		result = append(result, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

func trustedEndpoint(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "speed.cloudflare.com" && parsed.Path == "/__down" && parsed.User == nil
}

func classifyBenchmarkError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "transport-failure"
}

func sortedSampleTags(samples map[string]ThroughputSample) []string {
	result := make([]string, 0, len(samples))
	for tag := range samples {
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}
