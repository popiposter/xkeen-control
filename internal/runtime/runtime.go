package runtime

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configview"
	"github.com/popiposter/xkeen-control/internal/redact"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

const defaultCacheTTL = 3 * time.Second

type ConfigReader interface {
	Read(context.Context) configview.Summary
}

type OutboundTagReader func(string) ([]string, error)

type Dependencies struct {
	Xray  xrayapi.Reader
	Xkeen interface {
		Snapshot(context.Context) xkeen.Snapshot
	}
	Config           ConfigReader
	OutboundTags     OutboundTagReader
	OutboundTagsPath string
	C1               *c1.Coordinator
	Setup            func() SetupStatus
}

type Collector struct {
	version   string
	build     buildinfo.Info
	startedAt time.Time
	ttl       time.Duration
	deps      Dependencies

	mu      sync.Mutex
	value   View
	updated time.Time
	loading chan struct{}
}

type View struct {
	Status        Status
	Nodes         []Node
	Performance   Performance
	ConfigSummary ConfigSummary
}

type Status struct {
	ControlPlane ControlPlaneStatus `json:"controlPlane"`
	Xray         XrayStatus         `json:"xray"`
	Xkeen        XkeenStatus        `json:"xkeen"`
	Balancer     BalancerStatus     `json:"balancer"`
	Observatory  ObservatoryStatus  `json:"observatory"`
	Benchmark    BenchmarkStatus    `json:"benchmark"`
	Watchdog     WatchdogStatus     `json:"watchdog"`
	Selection    c1.SelectionStatus `json:"selection"`
	Setup        SetupStatus        `json:"setup"`
}

type ControlPlaneStatus struct {
	Version       string `json:"version"`
	SourceCommit  string `json:"sourceCommit"`
	Channel       string `json:"channel"`
	StartedAt     string `json:"startedAt"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

type SetupStatus struct {
	Panel         string `json:"panel"`
	Credential    string `json:"credential"`
	Xkeen         string `json:"xkeen"`
	Xray          string `json:"xray"`
	Configuration string `json:"configuration"`
	Runtime       string `json:"runtime"`
}

type XrayStatus struct {
	Running        bool `json:"running"`
	APIReachable   bool `json:"apiReachable"`
	ProbeReachable bool `json:"probeReachable"`
}

type XkeenStatus struct {
	Running bool `json:"running"`
}

type BalancerStatus struct {
	Tag            string `json:"tag"`
	NativeSelected string `json:"nativeSelected"`
	Override       string `json:"override"`
	Effective      string `json:"effective"`
}

type ObservatoryStatus struct {
	ConfiguredInterval string `json:"configuredInterval"`
	Healthy            int    `json:"healthy"`
	Total              int    `json:"total"`
	APIReachable       bool   `json:"apiReachable"`
}

type BenchmarkStatus struct {
	SemanticIntervalMinutes int                `json:"semanticIntervalMinutes"`
	InstalledSchedule       string             `json:"installedSchedule"`
	LastRunAt               string             `json:"lastRunAt"`
	EligibleNodes           int                `json:"eligibleNodes"`
	PayloadBytes            int64              `json:"payloadBytes"`
	PerNodeSeconds          int                `json:"perNodeSeconds"`
	MaxTransferBytes        int64              `json:"maxTransferBytes"`
	MaxWallSeconds          int                `json:"maxWallSeconds"`
	ControlPlane            c1.BenchmarkStatus `json:"controlPlane"`
}

type WatchdogStatus struct {
	Installed bool `json:"installed"`
	Enabled   bool `json:"enabled"`
}

type Node struct {
	Tag              string  `json:"tag"`
	Alive            bool    `json:"alive"`
	LatencyMS        int64   `json:"latencyMs"`
	LastSeen         string  `json:"lastSeen"`
	LastTry          string  `json:"lastTry"`
	LastError        string  `json:"lastError"`
	IsNativeSelected bool    `json:"isNativeSelected"`
	IsOverride       bool    `json:"isOverride"`
	IsEffective      bool    `json:"isEffective"`
	ThroughputKBps   float64 `json:"lastThroughputKBps"`
	LastBenchmarkAt  string  `json:"lastBenchmarkAt"`
	ThroughputError  string  `json:"lastThroughputError,omitempty"`
}

type Performance struct {
	SemanticIntervalMinutes int          `json:"semanticIntervalMinutes"`
	InstalledSchedule       string       `json:"installedSchedule"`
	LastBenchmarkAt         string       `json:"lastBenchmarkAt"`
	Nodes                   []Throughput `json:"nodes"`
}

type Throughput struct {
	Tag             string  `json:"tag"`
	ThroughputKBps  float64 `json:"throughputKBps"`
	LastBenchmarkAt string  `json:"lastBenchmarkAt"`
	Error           string  `json:"error,omitempty"`
}

type ConfigSummary struct {
	Available     bool                 `json:"available"`
	Routing       RoutingConfigSummary `json:"routing"`
	DNS           DNSConfigSummary     `json:"dns"`
	Observatory   ObservatoryConfig    `json:"observatory"`
	SpeedBalancer SpeedBalancerConfig  `json:"speedBalancer"`
	Benchmark     BenchmarkConfig      `json:"benchmark"`
}

type RoutingConfigSummary struct {
	RuleCount int      `json:"ruleCount"`
	RuleTags  []string `json:"ruleTags"`
	Balancers []string `json:"balancers"`
}

type DNSConfigSummary struct {
	Upstreams []DNSUpstream `json:"upstreams"`
}

type DNSUpstream struct {
	Host string `json:"host"`
	Tag  string `json:"tag"`
}

type ObservatoryConfig struct {
	SubjectSelectors []string `json:"subjectSelectors"`
	ProbeInterval    string   `json:"probeInterval"`
}

type SpeedBalancerConfig struct {
	Enabled     bool   `json:"enabled"`
	IntervalMin int    `json:"intervalMinutes"`
	Hysteresis  int    `json:"hysteresisPercent"`
	Balancer    string `json:"balancer"`
}

type BenchmarkConfig struct {
	InstalledSchedule string `json:"installedSchedule"`
	EligibleNodes     int    `json:"eligibleNodes"`
	PayloadBytes      int64  `json:"payloadBytes"`
	PerNodeSeconds    int    `json:"perNodeSeconds"`
	MaxTransferBytes  int64  `json:"maxTransferBytes"`
	MaxWallSeconds    int    `json:"maxWallSeconds"`
}

func NewCollector(version string, startedAt time.Time, deps Dependencies) *Collector {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if deps.OutboundTags == nil {
		deps.OutboundTags = redact.ReadUnifiedOutboundTags
	}
	if deps.OutboundTagsPath == "" {
		deps.OutboundTagsPath = "/opt/etc/xkeen-control/secrets/nodes.json"
	}
	return &Collector{version: version, build: buildinfo.Info{Product: "xkeen-control", Version: version, SourceCommit: "dev", Channel: "development"}, startedAt: startedAt, ttl: defaultCacheTTL, deps: deps}
}

func (c *Collector) SetBuildInfo(value buildinfo.Info) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.build = value
	c.version = value.Version
	c.mu.Unlock()
}

func (c *Collector) SetCacheTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	c.mu.Lock()
	c.ttl = ttl
	c.mu.Unlock()
}

func (c *Collector) Snapshot(ctx context.Context) View {
	for {
		c.mu.Lock()
		if !c.updated.IsZero() && time.Since(c.updated) < c.ttl {
			value := c.value
			c.mu.Unlock()
			return value
		}
		if c.loading != nil {
			wait := c.loading
			last := c.value
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return last
			}
		}
		c.loading = make(chan struct{})
		loading := c.loading
		c.mu.Unlock()

		value := c.collect(ctx)

		c.mu.Lock()
		c.value = value
		c.updated = time.Now()
		c.loading = nil
		close(loading)
		c.mu.Unlock()
		return value
	}
}

func (c *Collector) collect(ctx context.Context) View {
	var xrayState xrayapi.Snapshot
	var probeReachable bool
	if c.deps.Xray != nil {
		xrayState = c.deps.Xray.Snapshot(ctx)
		probeReachable = c.deps.Xray.ProbeReachable(ctx)
	}
	var xkeenState xkeen.Snapshot
	if c.deps.Xkeen != nil {
		xkeenState = c.deps.Xkeen.Snapshot(ctx)
	}
	var configState configview.Summary
	if c.deps.Config != nil {
		configState = c.deps.Config.Read(ctx)
	}
	var c1State c1.Status
	if c.deps.C1 != nil {
		c1State = c.deps.C1.Snapshot()
	}

	tags := []string(nil)
	if c.deps.OutboundTags != nil {
		// The path is a fixed production boundary; fake readers may ignore it.
		if value, err := c.deps.OutboundTags(c.deps.OutboundTagsPath); err == nil {
			tags = value
		}
	}
	sort.Strings(tags)

	health := make(map[string]xrayapi.OutboundHealth, len(xrayState.OutboundHealth))
	for _, item := range xrayState.OutboundHealth {
		if !redact.IsUnifiedOutboundTag(item.Tag) {
			continue
		}
		health[item.Tag] = item
	}
	native := safeSelection(xrayState.Balancer.NativeSelected)
	override := safeSelection(xrayState.Balancer.Override)
	effective := override
	if effective == "" {
		effective = native
	}

	nodes := make([]Node, 0, len(tags))
	healthy := 0
	for _, tag := range tags {
		if !redact.IsUnifiedOutboundTag(tag) {
			continue
		}
		item := Node{
			Tag:              tag,
			IsNativeSelected: tag == native,
			IsOverride:       tag == override,
			IsEffective:      tag == effective,
		}
		if value, ok := health[tag]; ok {
			item.Alive = value.Alive
			item.LatencyMS = nonNegativeInt64(value.DelayMS)
			item.LastSeen = formatTime(value.LastSeen)
			item.LastTry = formatTime(value.LastTry)
			item.LastError = redact.SanitizeError(value.LastError)
		}
		if value, ok := c1State.Benchmark.Samples[tag]; ok {
			if value.Valid {
				item.ThroughputKBps = value.BytesPerSecond / 1024
				item.LastBenchmarkAt = formatTime(c1State.Benchmark.LastCompletedAt)
			}
		} else if value, ok := xkeenState.Benchmark.ThroughputKBps[tag]; ok {
			item.ThroughputKBps = value
			item.LastBenchmarkAt = formatTime(xkeenState.Benchmark.ThroughputAt[tag])
			item.ThroughputError = xkeenState.Benchmark.ThroughputError[tag]
		}
		if item.Alive {
			healthy++
		}
		nodes = append(nodes, item)
	}

	benchmarkInterval := configState.SpeedBalancer.IntervalMin
	if benchmarkInterval == 0 {
		benchmarkInterval = xkeenState.Speed.IntervalMin
	}
	benchmarkSchedule := xkeenState.Benchmark.InstalledSchedule
	lastRun := formatTime(xkeenState.Benchmark.LastRunAt)
	status := Status{
		ControlPlane: ControlPlaneStatus{
			Version:       c.version,
			SourceCommit:  c.build.SourceCommit,
			Channel:       c.build.Channel,
			StartedAt:     formatTime(c.startedAt),
			UptimeSeconds: nonNegativeInt64(int64(time.Since(c.startedAt).Seconds())),
		},
		Xray: XrayStatus{
			Running:        xkeenState.XrayRunning,
			APIReachable:   xrayState.APIReachable,
			ProbeReachable: probeReachable,
		},
		Xkeen: XkeenStatus{Running: xkeenState.XkeenRunning},
		Balancer: BalancerStatus{
			Tag:            "bal-proxy",
			NativeSelected: native,
			Override:       override,
			Effective:      effective,
		},
		Observatory: ObservatoryStatus{
			ConfiguredInterval: configState.Observatory.ProbeInterval,
			Healthy:            healthy,
			Total:              len(nodes),
			APIReachable:       xrayState.ObservatoryReachable,
		},
		Benchmark: BenchmarkStatus{
			SemanticIntervalMinutes: benchmarkInterval,
			InstalledSchedule:       benchmarkSchedule,
			LastRunAt:               lastRun,
			EligibleNodes:           firstPositive(configState.SpeedBalancer.EligibleNodes, xkeenState.Speed.EligibleNodes),
			PayloadBytes:            firstPositiveInt64(configState.SpeedBalancer.PayloadBytes, xkeenState.Speed.PayloadBytes),
			PerNodeSeconds:          firstPositive(configState.SpeedBalancer.NodeSeconds, xkeenState.Speed.NodeSeconds),
			MaxTransferBytes:        firstPositiveInt64(configState.SpeedBalancer.MaxBytes, xkeenState.Speed.MaxBytes),
			MaxWallSeconds:          firstPositive(configState.SpeedBalancer.MaxSeconds, xkeenState.Speed.MaxSeconds),
			ControlPlane:            c1State.Benchmark,
		},
		Watchdog:  WatchdogStatus{Installed: xkeenState.Watchdog.Installed, Enabled: xkeenState.Watchdog.Enabled},
		Selection: c1State.Selection,
	}
	status.Setup = c.setupStatus(status, xkeenState, xrayState, configState)

	performanceNodes := make([]Throughput, 0, len(nodes))
	for _, node := range nodes {
		_, c1Sample := c1State.Benchmark.Samples[node.Tag]
		if _, legacySample := xkeenState.Benchmark.ThroughputKBps[node.Tag]; !c1Sample && !legacySample {
			continue
		}
		performanceNodes = append(performanceNodes, Throughput{
			Tag:             node.Tag,
			ThroughputKBps:  node.ThroughputKBps,
			LastBenchmarkAt: node.LastBenchmarkAt,
			Error:           node.ThroughputError,
		})
	}

	return View{
		Status: status,
		Nodes:  nodes,
		Performance: Performance{
			SemanticIntervalMinutes: benchmarkInterval,
			InstalledSchedule:       benchmarkSchedule,
			LastBenchmarkAt:         lastRun,
			Nodes:                   performanceNodes,
		},
		ConfigSummary: buildConfigSummary(configState, benchmarkSchedule),
	}
}

func (c *Collector) setupStatus(status Status, xkeenState xkeen.Snapshot, xrayState xrayapi.Snapshot, configState configview.Summary) SetupStatus {
	result := SetupStatus{Panel: "ready", Xkeen: "missing", Xray: "missing", Configuration: "missing", Runtime: "setup"}
	if c.deps.Setup != nil {
		result = c.deps.Setup()
	}
	if xrayState.APIReachable || xkeenState.XrayRunning {
		result.Xray = "ready"
	}
	if xkeenState.XkeenRunning {
		result.Xkeen = "ready"
	}
	if configState.Available {
		result.Configuration = "ready"
	}
	if result.Xkeen == "ready" && result.Xray == "ready" && result.Configuration == "ready" {
		if xrayState.APIReachable && status.Observatory.APIReachable {
			result.Runtime = "running"
		} else {
			result.Runtime = "degraded"
		}
	} else {
		result.Runtime = "setup"
	}
	return result
}

func buildConfigSummary(source configview.Summary, schedule string) ConfigSummary {
	return ConfigSummary{
		Available: source.Available,
		Routing: RoutingConfigSummary{
			RuleCount: source.Routing.RuleCount,
			RuleTags:  append([]string(nil), source.Routing.RuleTags...),
			Balancers: append([]string(nil), source.Routing.Balancers...),
		},
		DNS: DNSConfigSummary{Upstreams: func() []DNSUpstream {
			result := make([]DNSUpstream, 0, len(source.DNS.Upstreams))
			for _, item := range source.DNS.Upstreams {
				result = append(result, DNSUpstream{Host: item.Host, Tag: item.Tag})
			}
			return result
		}()},
		Observatory: ObservatoryConfig{
			SubjectSelectors: append([]string(nil), source.Observatory.SubjectSelectors...),
			ProbeInterval:    source.Observatory.ProbeInterval,
		},
		SpeedBalancer: SpeedBalancerConfig{
			Enabled:     source.SpeedBalancer.Enabled,
			IntervalMin: source.SpeedBalancer.IntervalMin,
			Hysteresis:  source.SpeedBalancer.Hysteresis,
			Balancer:    source.SpeedBalancer.Balancer,
		},
		Benchmark: BenchmarkConfig{
			InstalledSchedule: schedule,
			EligibleNodes:     source.SpeedBalancer.EligibleNodes,
			PayloadBytes:      source.SpeedBalancer.PayloadBytes,
			PerNodeSeconds:    source.SpeedBalancer.NodeSeconds,
			MaxTransferBytes:  source.SpeedBalancer.MaxBytes,
			MaxWallSeconds:    source.SpeedBalancer.MaxSeconds,
		},
	}
}

func safeSelection(value string) string {
	if redact.IsUnifiedOutboundTag(value) {
		return value
	}
	return ""
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func nonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func firstPositive(primary, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}

func firstPositiveInt64(primary, fallback int64) int64 {
	if primary > 0 {
		return primary
	}
	return fallback
}
