//go:build linux

package xkeen

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func TestStable21UpdatePromptReadbackAndNoReplay(t *testing.T) {
	for _, mode := range []string{"declined", "eof", "no-update", "changed", "build-only", "stopped-service", "busy", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			d := nativeFixture(t)
			writeNativeFixture(t, d, "opt/sbin/.xkeen/01_info/01_info_variable.sh", "xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\nbuild_timestamp=\"2026-10-06 10:58:45 MSK\"\n")
			writeNativeFixture(t, d, "proc/123/comm", "xray\n")
			t.Setenv("NATIVE_TEST_ROOT", d.Root)
			t.Setenv("NATIVE_TEST_MODE", mode)
			// Model the native confirmation/exit semantics, not the installer.
			script := `#!/bin/sh
[ "$#" = 1 ] && [ "$1" = -uk ] && [ "$XKEEN_FOREGROUND" = 1 ] || exit 9
printf 'run\n' >> "$NATIVE_TEST_ROOT/invocations"
case "$1" in
        -uk)
            printf '1. Confirm\n0. Cancel\nYour choice: '
            read -r answer || answer=0
            [ "$answer" = 1 ] || exit 0
            case "$NATIVE_TEST_MODE" in
                changed) printf 'xkeen_current_version="2.2"\nxkeen_build="Stable"\n' > "$NATIVE_TEST_ROOT/opt/sbin/.xkeen/01_info/01_info_variable.sh" ;;
                build-only) printf 'xkeen_current_version="2.1"\nxkeen_build="Stable"\nbuild_timestamp="2026-10-07 10:58:45 MSK"\n' > "$NATIVE_TEST_ROOT/opt/sbin/.xkeen/01_info/01_info_variable.sh" ;;
                stopped-service) rm "$NATIVE_TEST_ROOT/proc/123/comm"; rmdir "$NATIVE_TEST_ROOT/proc/123" ;;
                busy) exit 3 ;;
                incomplete) rm "$NATIVE_TEST_ROOT/opt/sbin/.xkeen/01_info/01_info_variable.sh" ;;
            esac
            printf 'private native output\n'
        ;;
esac
exit 0
`
			writeNativeFixture(t, d, "opt/sbin/xkeen", script)
			m := NewJobs(d.path("opt/sbin/xkeen"), authority.NewLease())
			m.ConfigureUpdateInspection(d)
			m.RequireInstalledCommands()
			callback := false
			m.AfterCommand = func(context.Context, string) {
				callback = true
				if release, err := m.Lease.TryAcquire(); err == nil {
					release()
					t.Error("readback/reconciliation lost panel lease")
				}
			}
			view, err := m.Start("owner", CommandRequest{Action: "update-xkeen"})
			if err != nil || !view.Interactive {
				t.Fatal(view, err)
			}
			if _, err = m.Start("owner", CommandRequest{Action: "update-xkeen"}); !errors.Is(err, authority.ErrBusy) {
				t.Fatal("second invocation admitted", err)
			}
			// Reattaching and reading cannot answer or restart this prompt.
			if _, err = m.Read("owner", "", 0); err != nil {
				t.Fatal(err)
			}
			answer := "1\n"
			if mode == "declined" {
				answer = "0\n"
			}
			if mode == "eof" {
				answer = "\x04"
			}
			if err = m.Input("owner", view.ID, answer); err != nil {
				t.Fatal(err)
			}
			final := waitNativeJob(t, m, view.ID)
			if final.Update == nil || final.Update.Before.Version != "2.1" || final.Update.Before.Channel != "stable" {
				t.Fatal("missing baseline", final.Update)
			}
			wantChange, wantProcess, wantState := "unchanged", "running", "completed"
			if mode == "changed" || mode == "build-only" {
				wantChange = "changed"
			}
			if mode == "stopped-service" {
				wantProcess = "stopped"
			}
			if mode == "incomplete" {
				wantChange = "unknown"
			}
			if mode == "busy" {
				wantState = "failed"
			}
			if final.State != wantState || final.Update.Change != wantChange || final.Update.XrayProcess != wantProcess || callback != (mode != "busy") {
				t.Fatal(final.State, final.Update, callback)
			}
			if final.ExitCode == nil || mode != "busy" && *final.ExitCode != 0 || mode == "busy" && *final.ExitCode != 3 {
				t.Fatal(final.ExitCode)
			}
			data, _ := json.Marshal(final.Update)
			if strings.Contains(string(data), "private") || strings.Contains(string(data), "NATIVE_TEST") {
				t.Fatal("update facts leaked console/environment")
			}
			calls, _ := os.ReadFile(d.path("invocations"))
			if string(calls) != "run\n" || m.Input("owner", view.ID, "1\n") == nil {
				t.Fatal("replayed command or accepted post-exit input")
			}
			if _, _, err := commandArguments(CommandRequest{Action: "update-xkeen", Parameter: "auto"}); err != ErrCommand {
				t.Fatal("quiet update introduced", err)
			}
		})
	}
}

func TestStable21UnansweredRemoteUpdateStaysPrivateAndUnknown(t *testing.T) {
	editor := editorFixture(t, "exit 0\n")
	m := testNativeJobs(t, "[ \"$#\" = 1 ] && [ \"$1\" = -uk ] || exit 9\nprintf 'Confirmation: '\nread -r answer\n")
	m.Lease = editor.Lease
	m.ConfigureUpdateInspection(nativeFixture(t))
	m.idleTimeout = 80 * time.Millisecond
	view, err := m.StartRemote("update-xkeen", editor)
	if err != nil || !view.Interactive {
		t.Fatal(view, err)
	}
	if _, err := m.Read("local-session", view.ID, 0); err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, m, view.ID)
	if final.State != "unknown" || final.Update.Change != "unknown" || final.Update.After != nil || final.Update.XrayProcess != "unknown" {
		t.Fatal(final)
	}
	if _, err := m.StartRemote("update-xkeen", editor); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("unanswered update replayed", err)
	}
}

func TestUpdateReceiptFailureRetiresAfterObservation(t *testing.T) {
	d := nativeFixture(t)
	m := testNativeJobs(t, "exit 0\n")
	m.ConfigureUpdateInspection(d)
	m.AfterCommand = func(context.Context, string) {
		// Saving the completed receipt must fail. The observed release must
		// not survive as verified after-state for this unknown operation.
		m.receiptPath = t.TempDir()
	}
	view, err := m.Start("owner", CommandRequest{Action: "update-xkeen"})
	if err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, m, view.ID)
	if final.State != "unknown" || final.Update.Change != "unknown" || final.Update.After != nil || final.ExitCode != nil {
		t.Fatal(final)
	}
	if release, err := m.Lease.TryAcquire(); !errors.Is(err, authority.ErrBlocked) {
		if release != nil {
			release()
		}
		t.Fatal("receipt failure did not block mutations", err)
	}
}
