//go:build linux

package nodes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recoveryRuntimeStub struct {
	identity string
	err      error
}

func TestProcessRecoveryUsesNativeEnvironmentIdentityAndRefusesCron(t *testing.T) {
	root, config := t.TempDir(), t.TempDir()
	binary := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(binary, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "123")
	os.Mkdir(p, 0700)
	put := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(p, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("comm", "xray\n")
	put("cmdline", binary+"\x00run\x00")
	put("environ", "XRAY_LOCATION_CONFDIR="+config+"\x00")
	put("stat", "123 (xray) S "+strings.Repeat("0 ", 18)+"100 0\n")
	if err := os.Symlink(binary, filepath.Join(p, "exe")); err != nil {
		t.Fatal(err)
	}
	r := ProcessRecoveryRuntime{ProcRoot: root, Binary: binary, ConfigDir: config}
	first, err := r.Snapshot(context.Background())
	if err != nil || len(first) != 64 {
		t.Fatal(first, err)
	}
	put("stat", "123 (xray) S "+strings.Repeat("0 ", 18)+"101 0\n")
	next, err := r.Snapshot(context.Background())
	if err != nil || next == first {
		t.Fatal("start identity not bound", err)
	}
	cron := filepath.Join(root, "124")
	os.Mkdir(cron, 0700)
	os.WriteFile(filepath.Join(cron, "comm"), []byte("crond\n"), 0600)
	os.WriteFile(filepath.Join(cron, "cmdline"), []byte("/opt/sbin/crond\x00"), 0600)
	if _, err := r.Snapshot(context.Background()); err == nil {
		t.Fatal("cron conflict ignored")
	}
	os.Remove(filepath.Join(cron, "cmdline"))
	if _, err := r.Snapshot(context.Background()); err == nil {
		t.Fatal("unknown process accepted")
	}
}

func (r *recoveryRuntimeStub) Snapshot(context.Context) (string, error) { return r.identity, r.err }

func recoveryFixture(t *testing.T) (*Manager, *fakeActivator, *recoveryRuntimeStub) {
	t.Helper()
	m, _, active := testManager(t, nil, nil)
	p, err := m.PreviewImport("owner", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(context.Background(), "owner", p.Token, false); err != nil {
		t.Fatal(err)
	}
	m.tx.ConfigDir = filepath.Dir(active)
	if err = os.WriteFile(filepath.Join(m.tx.ConfigDir, "05_routing.json"), []byte(`{"routing":{"rules":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	intent, err := acquireNodeIntent(context.Background(), m.recoveryDir())
	if err != nil {
		t.Fatal(err)
	}
	intent.Close()
	runtime := &recoveryRuntimeStub{identity: strings.Repeat("a", 64)}
	a := &fakeActivator{onRestart: func(int) { runtime.identity = strings.Repeat("b", 64) }}
	m.tx.Activator = a
	m.authority.Block()
	return m, a, runtime
}

func TestRecoveryOneActivationPreservesGenerationAndPrevious(t *testing.T) {
	m, a, r := recoveryFixture(t)
	s, err := m.recoverySnapshot(context.Background(), r)
	if err != nil || !s.view.CanActivate {
		t.Fatal(s.view, err)
	}
	if a.restarts != 0 || a.validatedPath != "" {
		t.Fatal("inspection mutated")
	}
	if err = m.RecoverCurrent(context.Background(), s.view.Digest, r); err != nil {
		t.Fatal(err)
	}
	if a.restarts != 1 || a.readyCalls != 1 || a.inventoryCalls != 1 || a.validatedPath == "" {
		t.Fatal(a)
	}
	receipt, err := readRecoveryReceipt(m.recoveryDir())
	if err != nil || receipt.Phase != "completed" || RecoveryNeedsInspection(m.recoveryDir()) {
		t.Fatal(receipt, err)
	}
	if _, err = os.Lstat(filepath.Join(m.recoveryDir(), ".pending")); !os.IsNotExist(err) {
		t.Fatal("marker retained", err)
	}
	v, err := m.InspectRecovery(context.Background(), r)
	if err != nil || v.Classification != "completed-receipt" || v.CanActivate {
		t.Fatal(v, err)
	}
	if err = m.RecoverCurrent(context.Background(), s.view.Digest, r); err == nil || a.restarts != 1 {
		t.Fatal("replayed", err, a.restarts)
	}
	for name, before := range s.configs {
		after, err := os.ReadFile(filepath.Join(m.tx.ConfigDir, name))
		if err != nil || string(after) != string(before) {
			t.Fatal("config changed", name)
		}
	}
}

func TestRecoveryRefusesDriftUnsafeStateAndReplay(t *testing.T) {
	for _, scenario := range []string{"mixed", "unsafe-marker", "digest-drift", "validation-drift", "post-activation-drift", "unchanged-process", "unknown-native", "old-intent", "missing-marker-unsettled"} {
		t.Run(scenario, func(t *testing.T) {
			m, a, r := recoveryFixture(t)
			v, err := m.InspectRecovery(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "mixed":
				os.WriteFile(m.tx.ActiveOutboundsPath, []byte(`{"outbounds":[]}`), 0600)
			case "unsafe-marker":
				os.Chmod(filepath.Join(m.recoveryDir(), ".pending"), 0644)
			case "digest-drift":
				os.WriteFile(filepath.Join(m.tx.ConfigDir, "05_routing.json"), []byte(`{"routing":{"rules":[{}]}}`), 0600)
			case "validation-drift":
				a.validate = func(context.Context) error {
					return os.WriteFile(filepath.Join(m.tx.ConfigDir, "05_routing.json"), []byte(`{"routing":{"rules":[{}]}}`), 0600)
				}
			case "post-activation-drift":
				a.onRestart = func(int) {
					r.identity = strings.Repeat("b", 64)
					os.WriteFile(filepath.Join(m.recoveryDir(), ".registry-absent"), []byte("drift"), 0600)
				}
			case "unchanged-process":
				a.onRestart = nil
			case "unknown-native":
				a.restartErr = errors.New("unknown native result")
			case "old-intent", "missing-marker-unsettled":
				a.restartErr = errors.New("unknown")
				if m.RecoverCurrent(context.Background(), v.Digest, r) == nil {
					t.Fatal("unknown accepted")
				}
				if scenario == "missing-marker-unsettled" {
					os.Remove(filepath.Join(m.recoveryDir(), ".pending"))
					if !RecoveryNeedsInspection(m.recoveryDir()) {
						t.Fatal("settlement crash not fenced")
					}
				}
				a.restartErr = nil
			}
			starts := a.restarts
			err = m.RecoverCurrent(context.Background(), v.Digest, r)
			if err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if scenario == "unknown-native" || scenario == "unchanged-process" || scenario == "post-activation-drift" {
				if a.restarts != 1 {
					t.Fatal(a.restarts)
				}
			} else if a.restarts != starts {
				t.Fatal("unexpected activation", a.restarts)
			}
			if a.restarts > 0 {
				fresh, _ := m.InspectRecovery(context.Background(), r)
				if fresh.CanActivate {
					t.Fatal("replay preview admitted", fresh)
				}
				if !RecoveryNeedsInspection(m.recoveryDir()) {
					t.Fatal("unknown not fenced")
				}
			}
		})
	}
}

func TestRecoveryRejectsSymlinkAndOversizedInput(t *testing.T) {
	m, _, r := recoveryFixture(t)
	target := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(target, []byte("node-operation-pending\n"), 0600)
	marker := filepath.Join(m.recoveryDir(), ".pending")
	os.Remove(marker)
	os.Symlink(target, marker)
	if _, err := m.InspectRecovery(context.Background(), r); err == nil {
		t.Fatal("followed marker symlink")
	}
	os.Remove(marker)
	os.WriteFile(marker, make([]byte, 129), 0600)
	if _, err := m.InspectRecovery(context.Background(), r); err == nil {
		t.Fatal("oversized marker")
	}
}
