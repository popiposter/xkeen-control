package c1

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"
)

// QualityMetrics describes HTTP response timings through the isolated outbound.
// Request failure ratio is deliberately not called UDP packet loss.
type QualityMetrics struct {
	Idle            LatencyDistribution `json:"idle"`
	DownloadLatency LatencyDistribution `json:"downloadLatency"`
	UploadLatency   LatencyDistribution `json:"uploadLatency"`
	Download        RateDistribution    `json:"download"`
	Upload          RateDistribution    `json:"upload"`
	Requests        int                 `json:"requests"`
	Failures        int                 `json:"failures"`
}

type LatencyDistribution struct {
	Samples  int     `json:"samples"`
	MedianMS float64 `json:"medianMs"`
	P95MS    float64 `json:"p95Ms"`
	JitterMS float64 `json:"jitterMs"`
}

type RateDistribution struct {
	Measurements []DetailedTransferSample `json:"measurements,omitempty"`
	Samples      int                      `json:"samples"`
	MedianBPS    float64                  `json:"medianBps"`
	P10BPS       float64                  `json:"p10Bps"`
	P90BPS       float64                  `json:"p90Bps"`
	ShortSamples bool                     `json:"shortSamples"`
}

type DetailedTransferSample struct {
	Bytes      int64   `json:"bytes"`
	DurationMS float64 `json:"durationMs"`
	BPS        float64 `json:"bps"`
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	x := fraction * float64(len(v)-1)
	lo := int(x)
	hi := min(lo+1, len(v)-1)
	return v[lo] + (v[hi]-v[lo])*(x-float64(lo))
}

func latencyDistribution(points []float64) LatencyDistribution {
	d := LatencyDistribution{Samples: len(points), MedianMS: percentile(points, .5), P95MS: percentile(points, .95)}
	for i := 1; i < len(points); i++ {
		d.JitterMS += math.Abs(points[i] - points[i-1])
	}
	if len(points) > 1 {
		d.JitterMS /= float64(len(points) - 1)
	}
	return d
}

func measureLatency(ctx context.Context, t ManualMeasurementTransport, metrics *QualityMetrics, points *[]float64) {
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	d, err := t.Latency(probe)
	if ctx.Err() != nil {
		return
	}
	metrics.Requests++
	if err != nil || d <= 0 {
		metrics.Failures++
		return
	}
	*points = append(*points, float64(d)/float64(time.Millisecond))
}

func (e *adaptiveExecution) runDetailed(ctx context.Context) error {
	t, ok := e.transport.(ManualMeasurementTransport)
	if !ok {
		return errors.New("detailed latency transport unavailable")
	}
	m := &QualityMetrics{}
	e.metrics = m
	// Warm up each direction; these bytes count but rates do not.
	for _, transfer := range []func(context.Context, int64) (ManualTransfer, error){t.Download, t.Upload} {
		if _, err := e.detailedTransfer(ctx, MiB, transfer); err != nil {
			return err
		}
	}
	var idle []float64
	for i := 0; i < 8 && ctx.Err() == nil; i++ {
		measureLatency(ctx, t, m, &idle)
	}
	m.Idle = latencyDistribution(idle)
	if len(idle) < 4 {
		return errors.New("insufficient idle samples")
	}
	var err error
	m.Download, m.DownloadLatency, err = e.detailedDirection(ctx, t, []int64{4 * MiB, 4 * MiB, 4 * MiB, 16 * MiB, 16 * MiB}, t.Download)
	if err != nil {
		return err
	}
	e.downloadBPS = m.Download.MedianBPS
	m.Upload, m.UploadLatency, err = e.detailedDirection(ctx, t, []int64{2 * MiB, 2 * MiB, 2 * MiB, 8 * MiB, 8 * MiB}, t.Upload)
	if err != nil {
		return err
	}
	e.downloadBPS = m.Download.MedianBPS
	e.uploadBPS = m.Upload.MedianBPS
	return nil
}

func (e *adaptiveExecution) detailedTransfer(ctx context.Context, payload int64, transfer func(context.Context, int64) (ManualTransfer, error)) (ManualTransfer, error) {
	stage, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r, err := transfer(stage, payload)
	// Failed upload cannot always report bytes already sent: reserve its entire
	// payload, rather than claiming zero and exceeding the generation budget.
	e.bytes += payload
	e.metrics.Requests++
	if err != nil || r.Bytes != payload || r.Duration <= 0 {
		e.metrics.Failures++
		return r, errors.New("incomplete detailed transfer")
	}
	return r, nil
}

func (e *adaptiveExecution) detailedDirection(ctx context.Context, t ManualMeasurementTransport, payloads []int64, transfer func(context.Context, int64) (ManualTransfer, error)) (RateDistribution, LatencyDistribution, error) {
	var rates, points []float64
	var measurements []DetailedTransferSample
	var previousPayload int64
	short := false
	for _, payload := range payloads {
		// Exactly one sampler accompanies a transfer. Join it before leaving the
		// route lease, even on timeout/failure. No background probe survives cleanup.
		loaded, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		local := QualityMetrics{}
		var observed []float64
		go func() {
			defer close(done)
			for loaded.Err() == nil && len(observed) < 32 && local.Requests < 32 {
				measureLatency(loaded, t, &local, &observed)
				timer := time.NewTimer(200 * time.Millisecond)
				select {
				case <-loaded.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
		r, err := e.detailedTransfer(ctx, payload, transfer)
		cancel()
		<-done
		e.metrics.Requests += local.Requests
		e.metrics.Failures += local.Failures
		points = append(points, observed...)
		if err != nil {
			return RateDistribution{}, latencyDistribution(points), err
		}
		if payload != previousPayload {
			rates = nil
			short = false
			previousPayload = payload
		}
		rates = append(rates, float64(r.Bytes)/r.Duration.Seconds())
		measurements = append(measurements, DetailedTransferSample{Bytes: payload, DurationMS: float64(r.Duration) / float64(time.Millisecond), BPS: rates[len(rates)-1]})
		short = short || r.Duration < 250*time.Millisecond
		// Three repeated small transfers suffice on slow paths; fast paths ramp
		// up for two more sustained samples within the hard byte ceiling.
		if len(rates) == 3 && r.Duration >= time.Second {
			break
		}
	}
	return RateDistribution{Measurements: measurements, Samples: len(rates), MedianBPS: percentile(rates, .5), P10BPS: percentile(rates, .1), P90BPS: percentile(rates, .9), ShortSamples: short}, latencyDistribution(points), nil
}

func detailedQualityPenalty(m *QualityMetrics) float64 {
	if m == nil {
		return 1
	}
	failure := float64(m.Failures) / float64(max(1, m.Requests))
	loaded := max(m.DownloadLatency.P95MS, m.UploadLatency.P95MS)
	growth := max(0, loaded-m.Idle.MedianMS)
	jitter := max(m.Idle.JitterMS, max(m.DownloadLatency.JitterMS, m.UploadLatency.JitterMS))
	variation := 0.0
	for _, r := range []RateDistribution{m.Download, m.Upload} {
		if r.MedianBPS > 0 {
			variation = max(variation, (r.P90BPS-r.P10BPS)/r.MedianBPS)
		}
	}
	return 1 + min(9, 5*failure+growth/200+jitter/100+variation/2)
}
