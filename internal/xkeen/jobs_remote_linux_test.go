//go:build linux

package xkeen

import (
	"context"
	"errors"
	"github.com/popiposter/xkeen-control/internal/authority"
	"testing"
)

func TestRemoteNativeCommandUsesLeaseAndRefusesPendingBeforeExec(t *testing.T) {
	editor := editorFixture(t, "exit 0\n")
	editor.Lease = authority.NewLease()
	jobs := testNativeJobs(t, "exit 0\n")
	jobs.Lease = editor.Lease
	before, err := editor.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = editor.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4"); err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.StartRemote("restart", editor); err == nil || jobs.job != nil {
		t.Fatal("remote applied pending config")
	}
	if _, err = jobs.StartRemote("help", editor); !errors.Is(err, ErrCommand) {
		t.Fatal("expanded remote vocabulary")
	}
	if _, err = jobs.StartRemote("stop", editor); err == nil {
		t.Fatal("remote stop ignored pending guard")
	}
	snapshot, _ := editor.Snapshot(context.Background())
	if _, err = editor.RestoreSaved(context.Background(), snapshot.Digest); err != nil {
		t.Fatal(err)
	}
	release, err := editor.Lease.TryAcquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.StartRemote("stop", editor); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("bypassed shared lease", err)
	}
	release()
	start, err := jobs.StartRemote("start", editor)
	if err != nil {
		t.Fatal("clean remote start refused", err)
	}
	if waitNativeJob(t, jobs, start.ID).State != "completed" {
		t.Fatal("clean start")
	}
	view, err := jobs.StartRemote("stop", editor)
	if err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, jobs, view.ID)
	if final.State != "completed" {
		t.Fatal(final.State)
	}
	if _, err = jobs.Read("local-session", view.ID, 0); err != nil {
		t.Fatal("operator cannot inspect bot job", err)
	}
	if _, err = jobs.Read("", view.ID, 0); err == nil {
		t.Fatal("empty owner leaked console")
	}
}
