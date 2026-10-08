//go:build linux

package xkeen

import (
	"context"
	"github.com/popiposter/xkeen-control/internal/authority"
	"os"
	"os/exec"
	"testing"
	"time"
)

func waitSetupJob(t *testing.T, m *Jobs, id string) JobView {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		v, e := m.Read("initial-setup", id, 0)
		if e != nil {
			t.Fatal(e)
		}
		if v.State != "running" {
			return v
		}
		select {
		case <-ctx.Done():
			t.Fatal("setup job timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestFreshPortPreparationThenOneConfirmedFullGenerationStart(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.ProcRoot = t.TempDir()
	e.Lease = authority.NewLease()
	before, _ := e.Snapshot(context.Background())
	digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
	if err != nil {
		t.Fatal(err)
	}
	m := testNativeJobs(t, "case \"$1 $2\" in '-ape 53'|'-start ') exit 0;; *) exit 9;; esac\n")
	m.Lease = e.Lease
	starts, prepares := 0, 0
	m.startTerminal = func(c *exec.Cmd, interactive bool) (*os.File, error) {
		if c.Args[1] == "-ape" {
			prepares++
		} else {
			starts++
			configProcessFixture(t, e, "102", "600")
			stockConfigProcessEnvironment(t, e, "102", "XRAY_LOCATION_CONFDIR="+e.Dir+"\x00")
		}
		return startNativeTerminal(c, interactive)
	}
	p, err := m.PrepareSetupDNSExemption(func(ctx context.Context) error { return e.VerifySetupStopped(ctx, digest) })
	if err != nil {
		t.Fatal(err)
	}
	if waitSetupJob(t, m, p.ID).State != "completed" || prepares != 1 || starts != 0 || e.VerifySetupStopped(context.Background(), digest) != nil {
		t.Fatal("preparation was activation")
	}
	pending, err := e.readPending()
	if err != nil || pending == nil || pending.ApplyID != "" {
		t.Fatal("preparation stole Apply outcome")
	}
	start, err := m.StartSetupConfigs(e, digest, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	v := waitSetupJob(t, m, start.ID)
	if v.ConfigurationState != "applied" || starts != 1 {
		t.Fatal("one complete activation missing", v.ConfigurationState)
	}
}
func TestSetupPolicyFailureAndUnexpectedProcessNeverStart(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.ProcRoot = t.TempDir()
	e.Lease = authority.NewLease()
	s, _ := e.Snapshot(context.Background())
	digest, err := e.SaveField(context.Background(), s.Digest, "dns", "queryStrategy", "UseIPv4")
	if err != nil {
		t.Fatal(err)
	}
	m := testNativeJobs(t, "exit 0\n")
	m.Lease = e.Lease
	calls := 0
	m.startTerminal = func(*exec.Cmd, bool) (*os.File, error) { calls++; return nil, ErrJob }
	if _, err := m.StartSetupConfigs(e, digest, func(context.Context) error { return ErrConfig }); err == nil || calls != 0 {
		t.Fatal("activation without policy")
	}
	configProcessFixture(t, e, "103", "700")
	stockConfigProcessEnvironment(t, e, "103", "XRAY_LOCATION_CONFDIR="+e.Dir+"\x00")
	if _, err := m.PrepareSetupDNSExemption(func(ctx context.Context) error { return e.VerifySetupStopped(ctx, digest) }); err == nil || calls != 0 {
		t.Fatal("unexpected process bypassed fence")
	}
}
