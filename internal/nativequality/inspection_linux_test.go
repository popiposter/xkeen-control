//go:build linux

package nativequality

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type inspectionControl struct {
	rules       []xrayapi.Rule
	unavailable bool
	removeFails bool
	removeNoop  bool
	removals    int
}

func (*inspectionControl) OverrideBalancerTarget(context.Context, string, string) error { return nil }
func (c *inspectionControl) AddRule(_ context.Context, rule xrayapi.Rule, _ bool) error {
	c.rules = append(c.rules, rule)
	return nil
}
func (c *inspectionControl) RemoveRule(_ context.Context, tag string) error {
	if c.removeFails {
		return ErrUnavailable
	}
	c.removals++
	if c.removeNoop {
		return nil
	}
	for i, rule := range c.rules {
		if rule.RuleTag == tag {
			c.rules = append(c.rules[:i], c.rules[i+1:]...)
			return nil
		}
	}
	return nil
}
func (c *inspectionControl) ListRules(context.Context) ([]xrayapi.Rule, error) {
	if c.unavailable {
		return nil, ErrUnavailable
	}
	return append([]xrayapi.Rule(nil), c.rules...), nil
}

func configureSyntheticJobRecovery(t *testing.T, s *Service, dir string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"opt/sbin/xkeen": "#!/bin/sh\nexit 0\n",
		"opt/sbin/.xkeen/01_info/01_info_variable.sh": "xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\n",
		"opt/etc/init.d/S05xkeen":                     "#!/bin/sh\nname_client=\"xray\"\n",
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	configDir := filepath.Join(root, "opt", "etc", "xray", "configs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"04_outbounds.json", "05_routing.json", "07_observatory.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0700); err != nil {
		t.Fatal(err)
	}
	s.Jobs.ConfigureRecovery(xkeen.Discovery{Root: root}, filepath.Join(root, "node-pending"))
}

func TestExplicitQualityInspectionRequiresConcreteReadbacks(t *testing.T) {
	s, _, _, dir := sweepFixture(t, false)
	marker := setupNativeApply(t, s, dir)
	s.Reader.(*sweepReader).failAfterFile = marker
	control := &inspectionControl{}
	s.Control = control
	probe := c1.NewProbeRouter(control)
	s.Probe = probe
	if err := s.startSweep(context.Background(), "periodic"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not settle")
	}
	if !s.Read().InspectionRequired {
		t.Fatal("missing ambiguity intent")
	}
	configureSyntheticJobRecovery(t, s, dir)
	restarted := &Service{Editor: s.Editor, Lease: s.Lease, Reader: s.Reader, Nodes: s.Nodes, Measurement: s.Measurement, Probe: probe, Control: control, Resources: s.Resources, Jobs: s.Jobs, QuotaPath: s.QuotaPath}
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("unhealthy Xray cleared inspection")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err != nil || !q.InspectionRequired {
		t.Fatal("failed inspection lost fence", err)
	}
	s.Reader.(*sweepReader).failAfterFile = ""
	control.removeFails = true
	if err := probe.WithTarget(context.Background(), "adaptive", "proxy-a", nil); err == nil || !probe.Blocked() {
		t.Fatal("failed cleanup did not block shared probe router")
	}
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("failed probe cleanup cleared inspection")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err != nil || !q.InspectionRequired {
		t.Fatal("failed cleanup lost fence", err)
	}
	control.removeFails = false
	control.removeNoop = true
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("reported successful but ineffective probe removal cleared inspection")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err != nil || !q.InspectionRequired {
		t.Fatal("ineffective removal lost fence", err)
	}
	control.removeNoop = false
	control.rules = append(control.rules, xrayapi.Rule{RuleTag: "xkeen-control-probe-unknown"})
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("unknown panel probe rule cleared inspection")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err != nil || !q.InspectionRequired {
		t.Fatal("unknown panel probe rule lost fence", err)
	}
	control.rules = nil // Simulate separate inspected removal of the unknown rule.
	if err := restarted.InspectAndResolve(context.Background()); err != nil {
		t.Fatal("inspected native state did not settle", err)
	}
	if control.removals == 0 || len(control.rules) != 0 || probe.Blocked() {
		t.Fatal("shared probe router was not reconciled")
	}
	if err := probe.WithTarget(context.Background(), "adaptive", "proxy-a", nil); err != nil {
		t.Fatal("next same-process measurement remained blocked", err)
	}
	q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes)
	if err != nil || q.InspectionRequired || q.ReviewsUsed != 1 || q.LastStartedAt.IsZero() {
		t.Fatal("settlement reset cadence or quota", q, err)
	}
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("settlement replay accepted")
	}
}
