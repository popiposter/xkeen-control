package xkeen

import (
	"context"
	"os"
	"testing"
)

func TestQualitySettlementNeverConsumesUnknownNativeJob(t *testing.T) {
	d := nativeFixture(t)
	m := NewJobs(d.path("opt/sbin/xkeen"), nil)
	m.ConfigureRecovery(d, d.path("opt/etc/xkeen-control/previous/.pending"))
	m.job = &nativeJob{state: "unknown", action: "restart"}
	if err := m.InspectQualitySettlement(context.Background()); err == nil {
		t.Fatal("unknown job treated as settled")
	}
	m.job.state = "completed"
	writeNativeFixture(t, d, "proc/12/cmdline", "/bin/sh\x00"+m.Binary+"\x00-restart\x00")
	if err := m.InspectQualitySettlement(context.Background()); err == nil {
		t.Fatal("active native child treated as settled")
	}
	if err := os.RemoveAll(d.path("proc/12")); err != nil {
		t.Fatal(err)
	}
	if err := m.InspectQualitySettlement(context.Background()); err != nil {
		t.Fatal("completed job with no child not inspectable", err)
	}
}
