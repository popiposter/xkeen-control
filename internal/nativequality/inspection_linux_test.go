//go:build linux

package nativequality

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type inspectionControl struct {
	rules       []xrayapi.Rule
	unavailable bool
	removeFails bool
	removals    int
}

func (*inspectionControl) OverrideBalancerTarget(context.Context, string, string) error { return nil }
func (*inspectionControl) AddRule(context.Context, xrayapi.Rule, bool) error            { return nil }
func (c *inspectionControl) RemoveRule(_ context.Context, tag string) error {
	if c.removeFails {
		return ErrUnavailable
	}
	c.removals++
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
	restarted := &Service{Editor: s.Editor, Lease: s.Lease, Reader: s.Reader, Nodes: s.Nodes, Measurement: s.Measurement, Control: control, Resources: s.Resources, Jobs: s.Jobs, QuotaPath: s.QuotaPath}
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("unhealthy Xray cleared inspection")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC()); err != nil || !q.InspectionRequired {
		t.Fatal("failed inspection lost fence", err)
	}
	s.Reader.(*sweepReader).failAfterFile = ""
	control.rules = []xrayapi.Rule{{RuleTag: "xkeen-control-probe-adaptive"}}
	control.removeFails = true
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("failed probe cleanup cleared inspection")
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC()); err != nil || !q.InspectionRequired {
		t.Fatal("failed cleanup lost fence", err)
	}
	control.removeFails = false
	if err := restarted.InspectAndResolve(context.Background()); err != nil {
		t.Fatal("inspected native state did not settle", err)
	}
	if control.removals != 1 || len(control.rules) != 0 {
		t.Fatal("quality probe rule was not settled exactly once")
	}
	q, err := quotaState(s.QuotaPath, time.Now().UTC())
	if err != nil || q.InspectionRequired || q.ReviewsUsed != 1 || q.LastStartedAt.IsZero() {
		t.Fatal("settlement reset cadence or quota", q, err)
	}
	if err := restarted.InspectAndResolve(context.Background()); err == nil {
		t.Fatal("settlement replay accepted")
	}
}
