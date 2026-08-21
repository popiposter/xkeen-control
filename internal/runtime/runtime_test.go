package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-keenetic/internal/c1"
	"github.com/popiposter/xkeen-keenetic/internal/configview"
	"github.com/popiposter/xkeen-keenetic/internal/xkeen"
	"github.com/popiposter/xkeen-keenetic/internal/xrayapi"
)

type fakeXray struct {
	snapshot xrayapi.Snapshot
	probe    bool
}

func (f fakeXray) Snapshot(context.Context) xrayapi.Snapshot { return f.snapshot }
func (f fakeXray) ProbeReachable(context.Context) bool       { return f.probe }

type fakeXkeen struct{ snapshot xkeen.Snapshot }

func (f fakeXkeen) Snapshot(context.Context) xkeen.Snapshot { return f.snapshot }

type fakeConfig struct{ summary configview.Summary }

func (f fakeConfig) Read(context.Context) configview.Summary { return f.summary }

func TestCollectorBuildsUnifiedReadOnlyView(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tags := make([]string, 0, 13)
	health := make([]xrayapi.OutboundHealth, 0, 13)
	throughput := make(map[string]float64, 13)
	throughputAt := make(map[string]time.Time, 13)
	throughputError := make(map[string]string, 1)
	for i := 1; i <= 13; i++ {
		prefix := "proxy-main-"
		if i > 7 {
			prefix = "proxy-us-"
		}
		tag := prefix + string(rune('a'+i))
		tags = append(tags, tag)
		health = append(health, xrayapi.OutboundHealth{Tag: tag, Alive: i != 13, DelayMS: int64(i * 10), LastSeen: now, LastTry: now, LastError: "connection refused for UUID-SENTINEL"})
		throughput[tag] = float64(i * 100)
		throughputAt[tag] = now
		if i == 13 {
			throughput[tag] = 0
			throughputError[tag] = "code-000"
		}
	}
	collector := NewCollector("test", now, Dependencies{
		Xray: fakeXray{snapshot: xrayapi.Snapshot{
			APIReachable:         true,
			RoutingReachable:     true,
			ObservatoryReachable: true,
			Balancer:             xrayapi.BalancerState{NativeSelected: tags[1], Override: tags[8]},
			OutboundHealth:       health,
		}, probe: true},
		Xkeen: fakeXkeen{snapshot: xkeen.Snapshot{
			XrayRunning: true, XkeenRunning: true,
			Speed:     xkeen.SpeedBalancer{Enabled: true, IntervalMin: 1440, Hysteresis: 20, Balancer: "bal-proxy", EligibleNodes: 128, PayloadBytes: 20971520, NodeSeconds: 10, MaxBytes: (20 << 20) * 128, MaxSeconds: 1280},
			Benchmark: xkeen.Benchmark{InstalledSchedule: "17 4 * * *", LastRunAt: now, ThroughputKBps: throughput, ThroughputAt: throughputAt, ThroughputError: throughputError},
			Watchdog:  xkeen.Watchdog{Installed: true, Enabled: true},
		}},
		Config: fakeConfig{summary: configview.Summary{
			Available:     true,
			Observatory:   configview.ObservatorySummary{ProbeInterval: "5m", SubjectSelectors: []string{"proxy-main-", "proxy-us-"}},
			SpeedBalancer: configview.SpeedBalancerSummary{Enabled: true, IntervalMin: 1440, Hysteresis: 20, Balancer: "bal-proxy", EligibleNodes: 128, PayloadBytes: 20971520, NodeSeconds: 10, MaxBytes: (20 << 20) * 128, MaxSeconds: 1280},
		}},
		OutboundTags: func(string) ([]string, error) { return tags, nil },
	})
	collector.SetCacheTTL(time.Minute)
	view := collector.Snapshot(context.Background())
	if len(view.Nodes) != 13 || view.Status.Observatory.Healthy != 12 {
		t.Fatalf("node aggregation = total %d healthy %d", len(view.Nodes), view.Status.Observatory.Healthy)
	}
	if view.Status.Balancer.NativeSelected != tags[1] || view.Status.Balancer.Override != tags[8] || view.Status.Balancer.Effective != tags[8] {
		t.Fatalf("selection model = %+v", view.Status.Balancer)
	}
	if !view.Nodes[1].IsNativeSelected || !view.Nodes[8].IsOverride || !view.Nodes[8].IsEffective {
		t.Fatalf("selection flags missing: native=%+v override=%+v", view.Nodes[1], view.Nodes[8])
	}
	if view.Nodes[12].LastError != "connection-refused" {
		t.Fatalf("unsanitized error = %q", view.Nodes[12].LastError)
	}
	if len(view.Performance.Nodes) != 13 || view.Performance.Nodes[12].ThroughputKBps != 0 || view.Performance.Nodes[12].Error != "code-000" || view.Performance.Nodes[12].LastBenchmarkAt == "" {
		t.Fatalf("performance projection = %+v", view.Performance.Nodes)
	}
	if view.Status.Benchmark.EligibleNodes != 128 || view.Status.Benchmark.MaxTransferBytes != (20<<20)*128 || view.Status.Benchmark.MaxWallSeconds != 1280 {
		t.Fatalf("benchmark budget projection = %+v", view.Status.Benchmark)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "UUID-SENTINEL") || strings.Contains(string(encoded), "privateKey") {
		t.Fatal("credential-bearing value reached runtime JSON")
	}
}

func TestCollectorProjectsPersistedControlPlaneThroughputIntoNodeList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tag := "proxy-main-throughput"
	benchmarkPath := t.TempDir() + "/benchmark.json"
	store := c1.BenchmarkStore{Path: benchmarkPath}
	if err := store.Save(c1.BenchmarkSnapshot{
		CompletedAt:  now,
		ResultClass:  "completed",
		Samples:      map[string]c1.ThroughputStatus{tag: {Valid: true, BytesPerSecond: 10240}},
		ValidSamples: 1,
	}); err != nil {
		t.Fatal(err)
	}
	coordinator := c1.NewCoordinator(c1.DefaultPolicy(), nil, c1.NewBenchmarkRunner(c1.DefaultPolicy(), nil, store), nil)
	collector := NewCollector("test", now, Dependencies{
		Xray: fakeXray{snapshot: xrayapi.Snapshot{
			APIReachable: true, RoutingReachable: true, ObservatoryReachable: true,
			Balancer:       xrayapi.BalancerState{NativeSelected: tag},
			OutboundHealth: []xrayapi.OutboundHealth{{Tag: tag, Alive: true}},
		}, probe: true},
		C1:           coordinator,
		OutboundTags: func(string) ([]string, error) { return []string{tag}, nil },
	})
	view := collector.Snapshot(context.Background())
	if len(view.Nodes) != 1 || view.Nodes[0].ThroughputKBps != 10 || view.Nodes[0].LastBenchmarkAt != now.Format(time.RFC3339) {
		t.Fatalf("control-plane throughput projection = %+v", view.Nodes)
	}
}
