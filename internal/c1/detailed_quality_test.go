package c1

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDetailedDistributionsKeepSequenceJitterAndQuantiles(t *testing.T) {
	d := latencyDistribution([]float64{10, 30, 20, 40})
	if d.MedianMS != 25 || d.P95MS != 38.5 || d.JitterMS != 50.0/3 || d.Samples != 4 {
		t.Fatal(d)
	}
	if latencyDistribution(nil).Samples != 0 {
		t.Fatal("missing metrics invented")
	}
}

func TestDetailedWarmupAndRepeatedTransfers(t *testing.T) {
	stub := &adaptiveTransportStub{duration: 300 * time.Millisecond, failedDownload: -1, failedUpload: -1}
	e := &adaptiveExecution{transport: stub}
	if err := e.runDetailed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.bytes != 68*MiB || e.metrics.Idle.Samples != 8 || e.metrics.Download.Samples != 2 || e.metrics.Upload.Samples != 2 || e.metrics.Failures != 0 || len(e.metrics.Download.Measurements) != 5 || e.metrics.Download.P90BPS != e.metrics.Download.P10BPS {
		t.Fatalf("missing repeated evidence: %+v bytes=%d", e.metrics, e.bytes)
	}
}

func TestDetailedFailedTransferChargesFullReservation(t *testing.T) {
	stub := &adaptiveTransportStub{duration: time.Second, failedDownload: 0, failedUpload: -1}
	e := &adaptiveExecution{transport: stub}
	if e.runDetailed(context.Background()) == nil || e.bytes != MiB || e.metrics.Failures != 1 {
		t.Fatal("failed transfer not counted", e)
	}
}

type cancelledLatencyTransport struct {
	adaptiveTransportStub
	started chan struct{}
	exited  chan struct{}
}

func (s *cancelledLatencyTransport) Latency(ctx context.Context) (time.Duration, error) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-ctx.Done()
	close(s.exited)
	return 0, ctx.Err()
}

func TestLoadedSamplerJoinsBeforeCleanupAndCancellationIsNotFailure(t *testing.T) {
	tx := &cancelledLatencyTransport{started: make(chan struct{}), exited: make(chan struct{})}
	e := &adaptiveExecution{transport: tx, metrics: &QualityMetrics{}}
	_, _, err := e.detailedDirection(context.Background(), tx, []int64{MiB}, func(ctx context.Context, p int64) (ManualTransfer, error) {
		<-tx.started
		return ManualTransfer{}, errors.New("offline failure")
	})
	if err == nil || e.metrics.Failures != 1 || e.metrics.Requests != 1 {
		t.Fatal("sampler cancellation misclassified", e.metrics)
	}
	select {
	case <-tx.exited:
	default:
		t.Fatal("sampler survived route cleanup")
	}
}

func TestDetailedPenaltyFavorsStableNodeOverBurstAndSaturatesSpeed(t *testing.T) {
	now := time.Now()
	stable := &QualityMetrics{Requests: 20, Idle: LatencyDistribution{MedianMS: 50}}
	unstable := &QualityMetrics{Requests: 20, Failures: 3, Idle: LatencyDistribution{MedianMS: 50}, DownloadLatency: LatencyDistribution{P95MS: 600, JitterMS: 80}}
	r := AdaptiveResult{NativeQuality: true, BroadSample: true, State: "completed", Generation: 1, StartedAt: now.Add(-time.Minute), CompletedAt: now, ShortlistCount: 2, Candidates: []AdaptiveCandidateResult{
		{Tag: "proxy-stable", RTTMS: 50, Valid: true, DownloadBPS: 150e6 / 8, UploadBPS: 40e6 / 8, Metrics: stable},
		{Tag: "proxy-burst", RTTMS: 50, Valid: true, DownloadBPS: 500e6 / 8, UploadBPS: 100e6 / 8, Metrics: unstable},
	}}
	costs, err := NativeQualityCosts(r, now, []string{"proxy-stable", "proxy-burst"})
	if err != nil {
		t.Fatal(err)
	}
	if NativeQualityRanking(r, costs)[0] != "proxy-stable" {
		t.Fatal("burst displaced stability", costs)
	}
	r.Candidates[1].Metrics = stable
	costs, err = NativeQualityCosts(r, now, []string{"proxy-stable", "proxy-burst"})
	if err != nil || costs[0].Value != costs[1].Value {
		t.Fatal("speed benefit failed to saturate", costs, err)
	}
}
