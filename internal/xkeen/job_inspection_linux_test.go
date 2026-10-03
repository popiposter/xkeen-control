//go:build linux

package xkeen

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestNativeResolveRequiresInspectionAndDoesNotClearNodeIntent(t *testing.T) {
	d := nativeFixture(t)
	m := NewJobs(d.path("opt/sbin/xkeen"), nil)
	m.job = &nativeJob{id: "0123456789abcdef0123456789abcdef", action: "start", state: "unknown", owner: "owner"}
	m.Lease.Block()
	pending := d.path("opt/etc/xkeen-control/previous/.pending")
	m.ConfigureRecovery(d, pending)
	writeNativeFixture(t, d, "opt/etc/xkeen-control/previous/.pending", "retained node intent")
	if _, err := m.ResolveInspection(context.Background(), "owner", m.job.id); err == nil {
		t.Fatal("cleared node intent")
	}
	if err := os.Remove(pending); err != nil {
		t.Fatal(err)
	}
	writeNativeFixture(t, d, "proc/12/cmdline", "/bin/sh\x00"+m.Binary+"\x00-start\x00")
	if _, err := m.ResolveInspection(context.Background(), "owner", m.job.id); err == nil {
		t.Fatal("ignored active native process")
	}
	if err := os.RemoveAll(d.path("proc/12")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveInspection(context.Background(), "other", m.job.id); err == nil {
		t.Fatal("foreign session cleared unknown")
	}
	v, err := m.ResolveInspection(context.Background(), "owner", m.job.id)
	if err != nil || v.State != "inspected" || v.ExitCode != nil {
		t.Fatal(v, err)
	}
	release, err := m.Lease.TryAcquire()
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := m.ResolveInspection(context.Background(), "owner", m.job.id); !errors.Is(err, ErrJob) {
		t.Fatal("replayed inspection", err)
	}
}
