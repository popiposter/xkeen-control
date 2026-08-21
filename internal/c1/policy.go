package c1

import (
	"errors"
	"sort"
	"time"
)

const (
	MiB                          int64 = 1 << 20
	GiB                          int64 = 1 << 30
	MaxRegistryNodes                   = 256
	DefaultTargetPayload               = 20 * MiB
	DefaultTotalBudget                 = 2 * GiB
	DefaultMinimumPayload              = 4 * MiB
	DefaultPerNodeTimeout              = 10 * time.Second
	DefaultSchedule                    = "17 4 * * *"
	DefaultBenchmarkEndpoint           = "https://speed.cloudflare.com/__down?bytes=20971520"
	DefaultProbeInterval               = 60 * time.Second
	DefaultProbePayload                = 1 * KiB
	DefaultFailureThreshold            = 2
	DefaultLatencyWindow               = 15 * time.Minute
	DefaultLatencyObservations         = 3
	DefaultAbsoluteDegradation         = 50
	DefaultRelativeDegradation         = 1.5
	DefaultBadWindows                  = 3
	DefaultMinimumDwell                = 30 * time.Minute
	DefaultThroughputImprovement       = 0.30
	KiB                                = 1 << 10
)

var ErrInsufficientBudget = errors.New("benchmark budget cannot meet minimum payload")

// Policy is the C.1 bounded production policy. The hard ceilings are part of
// the type's validation contract; callers cannot turn a local settings file
// into a larger router traffic or wall-time envelope.
type Policy struct {
	Enabled               bool
	Schedule              string
	BenchmarkEndpoint     string
	TargetPayloadBytes    int64
	TotalBudgetBytes      int64
	MinimumPayloadBytes   int64
	PerNodeTimeout        time.Duration
	ProbeInterval         time.Duration
	ProbePayloadBytes     int64
	FailureThreshold      int
	LatencyWindow         time.Duration
	LatencyObservations   int
	AbsoluteDegradationMS int64
	RelativeDegradation   float64
	BadWindows            int
	MinimumDwell          time.Duration
	ThroughputImprovement float64
	LatencyGuardrailMS    int64
	LatencyGuardrailRatio float64
	RegistryMaximum       int
}

func DefaultPolicy() Policy {
	return Policy{
		Enabled:               true,
		Schedule:              DefaultSchedule,
		BenchmarkEndpoint:     DefaultBenchmarkEndpoint,
		TargetPayloadBytes:    DefaultTargetPayload,
		TotalBudgetBytes:      DefaultTotalBudget,
		MinimumPayloadBytes:   DefaultMinimumPayload,
		PerNodeTimeout:        DefaultPerNodeTimeout,
		ProbeInterval:         DefaultProbeInterval,
		ProbePayloadBytes:     DefaultProbePayload,
		FailureThreshold:      DefaultFailureThreshold,
		LatencyWindow:         DefaultLatencyWindow,
		LatencyObservations:   DefaultLatencyObservations,
		AbsoluteDegradationMS: DefaultAbsoluteDegradation,
		RelativeDegradation:   DefaultRelativeDegradation,
		BadWindows:            DefaultBadWindows,
		MinimumDwell:          DefaultMinimumDwell,
		ThroughputImprovement: DefaultThroughputImprovement,
		LatencyGuardrailMS:    DefaultAbsoluteDegradation,
		LatencyGuardrailRatio: DefaultRelativeDegradation,
		RegistryMaximum:       MaxRegistryNodes,
	}
}

func (p Policy) normalized() Policy {
	d := DefaultPolicy()
	if p.Schedule == "" {
		p.Schedule = d.Schedule
	}
	if p.BenchmarkEndpoint == "" {
		p.BenchmarkEndpoint = d.BenchmarkEndpoint
	}
	if p.TargetPayloadBytes <= 0 {
		p.TargetPayloadBytes = d.TargetPayloadBytes
	}
	if p.TotalBudgetBytes <= 0 {
		p.TotalBudgetBytes = d.TotalBudgetBytes
	}
	if p.MinimumPayloadBytes <= 0 {
		p.MinimumPayloadBytes = d.MinimumPayloadBytes
	}
	if p.PerNodeTimeout <= 0 {
		p.PerNodeTimeout = d.PerNodeTimeout
	}
	if p.ProbeInterval <= 0 {
		p.ProbeInterval = d.ProbeInterval
	}
	if p.ProbePayloadBytes <= 0 {
		p.ProbePayloadBytes = d.ProbePayloadBytes
	}
	if p.FailureThreshold <= 0 {
		p.FailureThreshold = d.FailureThreshold
	}
	if p.LatencyWindow <= 0 {
		p.LatencyWindow = d.LatencyWindow
	}
	if p.LatencyObservations <= 0 {
		p.LatencyObservations = d.LatencyObservations
	}
	if p.AbsoluteDegradationMS <= 0 {
		p.AbsoluteDegradationMS = d.AbsoluteDegradationMS
	}
	if p.RelativeDegradation <= 1 {
		p.RelativeDegradation = d.RelativeDegradation
	}
	if p.BadWindows <= 0 {
		p.BadWindows = d.BadWindows
	}
	if p.MinimumDwell <= 0 {
		p.MinimumDwell = d.MinimumDwell
	}
	if p.ThroughputImprovement <= 0 {
		p.ThroughputImprovement = d.ThroughputImprovement
	}
	if p.LatencyGuardrailMS <= 0 {
		p.LatencyGuardrailMS = d.LatencyGuardrailMS
	}
	if p.LatencyGuardrailRatio <= 1 {
		p.LatencyGuardrailRatio = d.LatencyGuardrailRatio
	}
	if p.RegistryMaximum <= 0 {
		p.RegistryMaximum = d.RegistryMaximum
	}
	return p
}

func (p Policy) Validate() error {
	p = p.normalized()
	if p.TargetPayloadBytes > DefaultTargetPayload || p.TotalBudgetBytes > DefaultTotalBudget || p.MinimumPayloadBytes < 1 || p.PerNodeTimeout > DefaultPerNodeTimeout || p.RegistryMaximum > MaxRegistryNodes {
		return errors.New("C.1 benchmark policy exceeds hard safety ceiling")
	}
	if p.FailureThreshold < 1 || p.LatencyObservations < 1 || p.BadWindows < 1 || p.RelativeDegradation <= 1 || p.LatencyGuardrailRatio <= 1 {
		return errors.New("C.1 benchmark or selection policy is invalid")
	}
	return nil
}

type BenchmarkPlan struct {
	EligibleNodes       int
	PayloadBytesPerNode int64
	TotalBudgetBytes    int64
	PerNodeTimeout      time.Duration
	MaximumWallTime     time.Duration
	InsufficientBudget  bool
}

func (p Policy) Plan(eligibleNodes int) (BenchmarkPlan, error) {
	p = p.normalized()
	if err := p.Validate(); err != nil {
		return BenchmarkPlan{}, err
	}
	if eligibleNodes < 0 {
		return BenchmarkPlan{}, errors.New("eligible node count is invalid")
	}
	if eligibleNodes > p.RegistryMaximum {
		eligibleNodes = p.RegistryMaximum
	}
	plan := BenchmarkPlan{EligibleNodes: eligibleNodes, TotalBudgetBytes: p.TotalBudgetBytes, PerNodeTimeout: p.PerNodeTimeout}
	if eligibleNodes == 0 {
		return plan, nil
	}
	payload := p.TargetPayloadBytes
	if budgetPayload := p.TotalBudgetBytes / int64(eligibleNodes); budgetPayload < payload {
		payload = budgetPayload
	}
	plan.PayloadBytesPerNode = payload
	plan.MaximumWallTime = time.Duration(eligibleNodes) * p.PerNodeTimeout
	if payload < p.MinimumPayloadBytes {
		plan.InsufficientBudget = true
		return plan, ErrInsufficientBudget
	}
	return plan, nil
}

type NodeState struct {
	Tag     string
	Enabled bool
}

func EligibleTags(nodes []NodeState, maximum int) []string {
	if maximum <= 0 || maximum > MaxRegistryNodes {
		maximum = MaxRegistryNodes
	}
	result := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if !node.Enabled || len(node.Tag) < len("proxy-") || node.Tag[:len("proxy-")] != "proxy-" {
			continue
		}
		if _, ok := seen[node.Tag]; ok {
			continue
		}
		seen[node.Tag] = struct{}{}
		result = append(result, node.Tag)
	}
	sort.Strings(result)
	if len(result) > maximum {
		result = result[:maximum]
	}
	return result
}

func NextRunAt(now time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	local := now.In(location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), 4, 17, 0, 0, location)
	if !candidate.After(local) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}
