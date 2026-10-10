//go:build linux

package nativequality

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type unusedMeasurement struct{}

func (unusedMeasurement) MeasureNativeQuality(context.Context, c1.AdaptiveGeneration, func(c1.AdaptivePerformanceStatus)) (c1.AdaptiveResult, error) {
	panic("measurement must not start")
}
func (unusedMeasurement) NativeQualityEvidence(xrayapi.Snapshot) map[string]c1.AdaptiveCandidateInput {
	panic("measurement must not start")
}
func (unusedMeasurement) MeasureRTT(context.Context, []string) ([]c1.RTTSample, error) {
	panic("measurement must not start")
}

func TestConstrainedNativeConflictIsVisibleWithoutActivation(t *testing.T) {
	s := &Service{Editor: &xkeen.ConfigEditor{}, Lease: authority.NewLease(), Reader: &pinRuntime{}, Nodes: func(context.Context) []c1.NodeState { return nil }, Measurement: unusedMeasurement{}, Resources: &resourcepolicy.Guard{Profile: resourcepolicy.ForPlatform("mipsle", 254472), Conflict: func() (bool, error) { return true, nil }}}
	if err := s.Start(context.Background()); err != resourcepolicy.ErrExternalBenchmark {
		t.Fatal(err)
	}
	v := s.Read()
	if v.StartReason != "native-speed-conflict" || v.AutomaticReason != "native-speed-conflict-or-unavailable" || v.State != "idle" {
		t.Fatal(v)
	}
}

func TestStageBroadSampleRestrictsOnlySelectorAndCostsAndKeepsFutureSampleBroad(t *testing.T) {
	dir := t.TempDir()
	validator := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	routing := `{/*keep*/"routing":{"rules":[{"domain":["example.invalid"],"outboundTag":"direct"}],"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"fallbackTag":"blocked","strategy":{"type":"leastLoad","settings":{"maxRTT":"10s","expected":2}}}]}}`
	var nodes []c1.NodeState
	var pool []string
	var outbounds []map[string]string
	now := time.Now().UTC()
	result := c1.AdaptiveResult{NativeQuality: true, BroadSample: true, Generation: 1, State: "completed", StartedAt: now.Add(-4 * time.Minute), CompletedAt: now, ShortlistCount: 9}
	for i := 0; i < 9; i++ {
		tag := fmt.Sprintf("proxy-%02d", i)
		pool = append(pool, tag)
		nodes = append(nodes, c1.NodeState{Tag: tag, Enabled: true})
		outbounds = append(outbounds, map[string]string{"tag": tag, "protocol": "vless"})
		rate := 10e6
		if i == 0 {
			rate = 1e6
		}
		result.Candidates = append(result.Candidates, c1.AdaptiveCandidateResult{Tag: tag, RTTMS: int64(150 + i*5), DownloadBPS: rate, UploadBPS: rate, Valid: true})
	}
	encoded, _ := json.Marshal(map[string]any{"outbounds": outbounds})
	for name, data := range map[string][]byte{"05_routing.json": []byte(routing), "04_outbounds.json": encoded} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := &xkeen.ConfigEditor{Dir: dir, XrayBinary: validator, Lease: authority.NewLease(), PreviousDir: filepath.Join(t.TempDir(), "previous")}
	w, err := e.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Editor: e, Nodes: func(context.Context) []c1.NodeState { return nodes }, pool: pool, result: result, status: Status{State: "completed", Digest: w.Digest}}
	if err := os.WriteFile(filepath.Join(dir, "04_outbounds.json"), append(append([]byte(nil), encoded...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if status := s.Read(); status.CanStage || status.StageReason != "configuration-changed" {
		t.Fatal("outbound drift was not explained", status)
	}
	if err := os.WriteFile(filepath.Join(dir, "04_outbounds.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(context.Background(), w.Digest); err != nil {
		t.Fatal(err)
	}
	after, err := e.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Routing struct {
			Balancers []struct {
				Selector []string
				Strategy struct {
					Settings struct{ Costs []c1.NativeQualityCost }
				}
			}
		}
	}
	if configjson.Decode([]byte(after.Documents["05_routing.json"].Text), &doc) != nil {
		t.Fatal("invalid saved JSONC")
	}
	selected := doc.Routing.Balancers[0].Selector
	actualOutbounds, readErr := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if len(selected) != 6 || selected[0] != "proxy-01" || selected[5] != "proxy-06" || len(doc.Routing.Balancers[0].Strategy.Settings.Costs) != 6 || readErr != nil || string(actualOutbounds) != string(encoded) || !strings.Contains(after.Documents["05_routing.json"].Text, `"fallbackTag":"blocked"`) || !strings.Contains(after.Documents["05_routing.json"].Text, "/*keep*/") {
		t.Fatal("wrong selected pool or unrelated write", selected)
	}
	if next, _, err := measurementPool(after.Documents["05_routing.json"].Text, nodes, after.Targets); err != nil || len(next) != 9 {
		t.Fatal("subsequent test cannot reconsider excluded nodes", next, err)
	}
}

type pinRuntime struct {
	override  string
	writes    int
	reachable bool
}

func (p *pinRuntime) Snapshot(context.Context) xrayapi.Snapshot {
	return xrayapi.Snapshot{APIReachable: p.reachable, RoutingReachable: p.reachable, Balancer: xrayapi.BalancerState{Override: p.override}}
}
func (p *pinRuntime) ProbeReachable(context.Context) bool { return p.reachable }
func (p *pinRuntime) OverrideBalancerTarget(_ context.Context, balancer, target string) error {
	if balancer != "bal-proxy" {
		return ErrUnavailable
	}
	p.override = target
	p.writes++
	return nil
}
func (*pinRuntime) AddRule(context.Context, xrayapi.Rule, bool) error { return ErrUnavailable }
func (*pinRuntime) RemoveRule(context.Context, string) error          { return ErrUnavailable }
func (*pinRuntime) ListRules(context.Context) ([]xrayapi.Rule, error) { return nil, ErrUnavailable }

func TestNativePinUsesEnabledPoolAndReadbackWithoutConfigWrites(t *testing.T) {
	dir := t.TempDir()
	validator := filepath.Join(t.TempDir(), "xray")
	os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700)
	routing := `{"routing":{"rules":[],"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"strategy":{"type":"leastLoad"}}]}}`
	os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(routing), 0600)
	os.WriteFile(filepath.Join(dir, "04_outbounds.json"), []byte(`{"outbounds":[{"tag":"proxy-a","protocol":"vless"},{"tag":"proxy-b","protocol":"vless"}]}`), 0600)
	lease := authority.NewLease()
	editor := &xkeen.ConfigEditor{Dir: dir, XrayBinary: validator, Lease: lease, PreviousDir: filepath.Join(t.TempDir(), "previous")}
	runtime := &pinRuntime{reachable: true}
	service := &Service{Editor: editor, Lease: lease, Reader: runtime, Control: runtime, Nodes: func(context.Context) []c1.NodeState {
		return []c1.NodeState{{Tag: "proxy-a", Enabled: true}, {Tag: "proxy-b", Enabled: true}, {Tag: "proxy-disabled"}}
	}}
	for _, invalid := range []string{"proxy-disabled", "foreign"} {
		if service.SetManualOverride(context.Background(), invalid) == nil {
			t.Fatal("invalid pool pin admitted")
		}
	}
	if runtime.writes != 0 {
		t.Fatal("invalid pin wrote runtime")
	}
	if service.SetManualOverride(context.Background(), "proxy-a") != nil || runtime.override != "proxy-a" {
		t.Fatal("native pin not read back")
	}
	if service.SetManualOverride(context.Background(), "") != nil || runtime.override != "" {
		t.Fatal("clear not read back")
	}
	content, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	if string(content) != routing {
		t.Fatal("pin changed native configuration")
	}
	current, err := editor.Workspace(context.Background())
	if err != nil || current.Pending != nil {
		t.Fatal("pin staged a configuration")
	}
	release, _ := lease.TryAcquire()
	if service.SetManualOverride(context.Background(), "proxy-a") == nil {
		t.Fatal("busy panel pin admitted")
	}
	release()
	runtime.reachable = false
	if service.SetManualOverride(context.Background(), "proxy-a") == nil {
		t.Fatal("unreachable runtime admitted")
	}
}

func TestStageUsesExistingPendingEditorPreservesOtherNativeBytes(t *testing.T) {
	dir := t.TempDir()
	validator := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	routing := `{/*retain*/"routing":{"future":9007199254740993,"rules":[],"balancers":[{"tag":"other","selector":["direct"]},{"tag":"bal-proxy","selector":["proxy-"],"fallbackTag":"blocked","strategy":{"type":"leastLoad","settings":{"maxRTT":"10s","expected":2}}}]}}`
	for name, text := range map[string]string{"05_routing.json": routing, "04_outbounds.json": `{"outbounds":[{"tag":"proxy-a","protocol":"vless"},{"tag":"proxy-b","protocol":"vless"}]}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := &xkeen.ConfigEditor{Dir: dir, XrayBinary: validator, Lease: authority.NewLease(), PreviousDir: filepath.Join(t.TempDir(), "previous")}
	w, err := e.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := &Service{Editor: e, Nodes: func(context.Context) []c1.NodeState {
		return []c1.NodeState{{Tag: "proxy-a", Enabled: true}, {Tag: "proxy-b", Enabled: true}}
	}, pool: []string{"proxy-a", "proxy-b"}, status: Status{State: "completed", Digest: w.Digest}, result: c1.AdaptiveResult{Generation: 1, StartedAt: now.Add(-time.Second), CompletedAt: now, State: "completed", ShortlistCount: 2, Candidates: []c1.AdaptiveCandidateResult{{Tag: "proxy-a", RTTMS: 20, Valid: true, DownloadBPS: 1e6, UploadBPS: 1e6}, {Tag: "proxy-b", RTTMS: 60, Valid: true, DownloadBPS: 100e6, UploadBPS: 10e6}}}}
	digest, err := s.Stage(context.Background(), w.Digest)
	if err != nil {
		t.Fatal(err)
	}
	after, err := e.Workspace(context.Background())
	if err != nil || after.Pending == nil || after.Digest != digest {
		t.Fatalf("not ordinary pending editor change: %+v %v", after.Pending, err)
	}
	text := after.Documents["05_routing.json"].Text
	for _, keep := range []string{"/*retain*/", `"future":9007199254740993`, `"fallbackTag":"blocked"`, `{"tag":"other","selector":["direct"]}`} {
		if !strings.Contains(text, keep) {
			t.Fatalf("lost native sibling %s", keep)
		}
	}
	if !strings.Contains(text, `"type":"leastLoad"`) {
		t.Fatal("no native strategy")
	}
	if _, err := s.Stage(context.Background(), w.Digest); err == nil {
		t.Fatal("consumed proposal replayed")
	}
}
