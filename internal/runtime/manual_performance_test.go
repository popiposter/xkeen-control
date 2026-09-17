package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configview"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type countingRuntimeXray struct {
	snapshots atomic.Int32
	probes    atomic.Int32
	snapshot  xrayapi.Snapshot
}

func (x *countingRuntimeXray) Snapshot(context.Context) xrayapi.Snapshot {
	x.snapshots.Add(1)
	return x.snapshot
}
func (x *countingRuntimeXray) ProbeReachable(context.Context) bool {
	x.probes.Add(1)
	return true
}

type countingRuntimeConfig struct{ reads atomic.Int32 }

func (c *countingRuntimeConfig) Read(context.Context) configview.Summary {
	c.reads.Add(1)
	return configview.Summary{Available: true}
}

type runtimeProbeController struct{}

func (runtimeProbeController) OverrideBalancerTarget(context.Context, string, string) error {
	return nil
}
func (runtimeProbeController) AddRule(context.Context, xrayapi.Rule, bool) error { return nil }
func (runtimeProbeController) RemoveRule(context.Context, string) error          { return nil }
func (runtimeProbeController) ListRules(context.Context) ([]xrayapi.Rule, error) { return nil, nil }

type runtimeBlockingTransport struct{ started chan struct{} }

func (r *runtimeBlockingTransport) Latency(ctx context.Context) (time.Duration, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-ctx.Done()
	return 0, ctx.Err()
}
func (r *runtimeBlockingTransport) Download(ctx context.Context, _ int64) (c1.ManualTransfer, error) {
	<-ctx.Done()
	return c1.ManualTransfer{}, ctx.Err()
}
func (r *runtimeBlockingTransport) Upload(ctx context.Context, _ int64) (c1.ManualTransfer, error) {
	<-ctx.Done()
	return c1.ManualTransfer{}, ctx.Err()
}

func TestPerformanceSnapshotOverlaysManualRAMWithoutRefreshingHeavyCollectors(t *testing.T) {
	tag := "proxy-node-00000001"
	xray := &countingRuntimeXray{snapshot: xrayapi.Snapshot{
		APIReachable: true, RoutingReachable: true, ObservatoryReachable: true,
		Balancer:       xrayapi.BalancerState{NativeSelected: tag},
		OutboundHealth: []xrayapi.OutboundHealth{{Tag: tag, Alive: true}},
	}}
	config := &countingRuntimeConfig{}
	transport := &runtimeBlockingTransport{started: make(chan struct{})}
	coordinator := c1.NewCoordinator(c1.DefaultPolicy(), nil, &c1.BenchmarkRunner{}, func(context.Context) []c1.NodeState {
		return []c1.NodeState{{ID: "node-00000001", Tag: tag, Enabled: true}}
	})
	coordinator.SetManualRunner(&c1.ManualNodeRunner{Probe: c1.NewProbeRouter(runtimeProbeController{}), Transport: transport})
	collector := NewCollector("test", time.Now().UTC(), Dependencies{
		Xray: xray, Xkeen: fakeXkeen{snapshot: xkeen.Snapshot{XrayRunning: true, XkeenRunning: true}}, Config: config, C1: coordinator,
		OutboundTags: func(string) ([]string, error) { return []string{tag}, nil },
	})
	collector.SetCacheTTL(time.Nanosecond)
	_ = collector.Snapshot(context.Background())
	baseSnapshots, baseProbes, baseReads := xray.snapshots.Load(), xray.probes.Load(), config.reads.Load()
	if err := coordinator.TriggerManualNode("node-00000001"); err != nil {
		t.Fatal(err)
	}
	<-transport.started
	for index := 0; index < 3; index++ {
		performance := collector.PerformanceSnapshot(context.Background())
		if performance.Manual.State != "running" || performance.Manual.TargetNodeID != "node-00000001" {
			t.Fatalf("manual performance projection = %+v", performance.Manual)
		}
	}
	if xray.snapshots.Load() != baseSnapshots || xray.probes.Load() != baseProbes || config.reads.Load() != baseReads {
		t.Fatalf("manual polling refreshed heavy collectors: snapshots %d/%d probes %d/%d config %d/%d", xray.snapshots.Load(), baseSnapshots, xray.probes.Load(), baseProbes, config.reads.Load(), baseReads)
	}
	applyContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := coordinator.BeginApply(applyContext)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
