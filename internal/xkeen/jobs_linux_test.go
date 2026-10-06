//go:build linux

package xkeen

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/popiposter/xkeen-control/internal/authority"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testNativeJobs(t *testing.T, script string) *Jobs {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xkeen")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return NewJobs(path, authority.NewLease())
}

func TestNativeUpdateConsoleCanAnswerConditionalPrompt(t *testing.T) {
	m := testNativeJobs(t, "[ \"$1\" = -ug ] || exit 9\nprintf 'Answer: '\nread -r answer\nprintf 'Got:%s' \"$answer\"\n")
	v, err := m.Start("owner", CommandRequest{Action: "update-geodata"})
	if err != nil || !v.Interactive {
		t.Fatal("update prompt unavailable", err)
	}
	if err = m.Input("owner", v.ID, "yes\n"); err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, m, v.ID)
	output, _ := base64.StdEncoding.DecodeString(final.Output)
	if final.State != "completed" || !strings.Contains(string(output), "Got:yes") {
		t.Fatal("native update prompt was not answered")
	}
}
func waitNativeJob(t *testing.T, m *Jobs, id string) JobView {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		v, err := m.Read("owner", id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v.State != "running" {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native job failed to finish")
	return JobView{}
}
func TestNativeTerminalOwnsOneProcessAndSession(t *testing.T) {
	m := testNativeJobs(t, "[ \"$1\" = -ugc ] || exit 9\nprintf 'Answer: '\nread -r answer\nprintf 'Got:%s' \"$answer\"\n")
	v, err := m.Start("owner", CommandRequest{Action: "geodata-schedule"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start("other", CommandRequest{Action: "status"}); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("accepted concurrent command", err)
	}
	if _, err = m.Read("other", v.ID, 0); err == nil {
		t.Fatal("leaked output to another session")
	}
	if err = m.Input("other", v.ID, "secret\n"); err == nil {
		t.Fatal("accepted foreign input")
	}
	if err = m.Resize("owner", v.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err = m.Input("owner", v.ID, "yes\n"); err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, m, v.ID)
	output, _ := base64.StdEncoding.DecodeString(final.Output)
	if final.State != "completed" || !strings.Contains(string(output), "Got:yes") {
		t.Fatalf("result=%+v output=%q", final, output)
	}
	if err = m.Input("owner", v.ID, "again\n"); err == nil {
		t.Fatal("post-exit input accepted")
	}
}

func TestNativeIdlePromptBecomesUnknownWithoutReplay(t *testing.T) {
	m := testNativeJobs(t, "printf 'Answer: '\nread -r answer\n")
	m.idleTimeout = 80 * time.Millisecond
	v, err := m.Start("owner", CommandRequest{Action: "geodata-schedule"})
	if err != nil {
		t.Fatal(err)
	}
	// Console inspection is not native activity and cannot extend the prompt.
	final := waitNativeJob(t, m, v.ID)
	if final.State != "unknown" {
		t.Fatalf("idle result=%+v", final)
	}
	if _, err := m.Start("owner", CommandRequest{Action: "status"}); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("idle command was replayable", err)
	}
}
func TestNativeCommandRejectsShellAndInvalidParameters(t *testing.T) {
	if _, _, err := commandArguments(CommandRequest{Action: "ports-add", Parameter: "+80"}); err == nil {
		t.Fatal("accepted signed port absent in native grammar")
	}
	for _, v := range []string{"1.2.3-rc1", "1.2.3.4"} {
		if _, _, err := commandArguments(CommandRequest{Action: "update-xray", Parameter: v}); err == nil {
			t.Fatal("accepted unsupported native version", v)
		}
	}
	for _, r := range []CommandRequest{{Action: "-uk"}, {Action: "start", Parameter: ";reboot"}, {Action: "update-xray", Parameter: "$(evil)"}, {Action: "ports-add", Parameter: "443;evil"}, {Action: "ports-add", Parameter: "80:20"}, {Action: "dns-interception", Parameter: "toggle"}, {Action: "uk_post_update"}} {
		if _, _, err := commandArguments(r); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	_, args, err := commandArguments(CommandRequest{Action: "update-xray", Parameter: "1.2.3"})
	if err != nil || strings.Join(args, " ") != "-ux v1.2.3" {
		t.Fatal(args, err)
	}
}
func TestNativeOutputBoundedAndNoninteractiveReadOnly(t *testing.T) {
	m := testNativeJobs(t, "printf '%0300000d' 0\n")
	v, err := m.Start("owner", CommandRequest{Action: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Input("owner", v.ID, "input\n") == nil {
		t.Fatal("noninteractive input accepted")
	}
	final := waitNativeJob(t, m, v.ID)
	m.mu.Lock()
	size := len(m.job.output)
	m.mu.Unlock()
	if size > maxJobOutput || !final.Truncated {
		t.Fatalf("unbounded output: %d %+v", size, final)
	}
}
func TestNativeCancelDoesNotReplayAndRetainsUnknown(t *testing.T) {
	m := testNativeJobs(t, "sleep 60\n")
	v, err := m.Start("owner", CommandRequest{Action: "geodata-schedule"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Cancel("other", v.ID) == nil {
		t.Fatal("foreign cancellation accepted")
	}
	if err = m.Cancel("owner", v.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitNativeJob(t, m, v.ID); got.State != "unknown" {
		t.Fatal(got)
	}
	if _, err = m.Start("owner", CommandRequest{Action: "geodata-schedule"}); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("replayed unknown job", err)
	}
	if release, err := m.Lease.TryAcquire(); !errors.Is(err, authority.ErrBlocked) {
		if release != nil {
			release()
		}
		t.Fatal("unknown command released shared panel fence", err)
	}
}

func TestNativeJobExitPreservesBackgroundService(t *testing.T) {
	for _, action := range []string{"start", "geodata-schedule"} {
		t.Run(action, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "child-result")
			t.Setenv("NATIVE_CHILD_MARKER", marker)
			m := testNativeJobs(t, "(trap 'printf hup > \"$NATIVE_CHILD_MARKER\"; exit 3' HUP; sleep .4; printf survived > \"$NATIVE_CHILD_MARKER\") &\nsleep .05\n")
			v, err := m.Start("owner", CommandRequest{Action: action})
			if err != nil {
				t.Fatal(err)
			}
			if got := waitNativeJob(t, m, v.ID); got.State != "completed" {
				t.Fatal(got)
			}
			time.Sleep(500 * time.Millisecond)
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "survived" {
				t.Fatalf("background service signalled: %q %v", data, err)
			}
		})
	}
}

func TestNativeCompletionCallbackKeepsCommandResultAndLease(t *testing.T) {
	for _, exit := range []string{"0", "3"} {
		t.Run(exit, func(t *testing.T) {
			jobs := testNativeJobs(t, "exit "+exit+"\n")
			called := false
			jobs.AfterCommand = func(ctx context.Context, action string) {
				called = true
				if action != "status" || ctx.Err() != nil {
					t.Error("bad callback")
				}
				if release, err := jobs.Lease.TryAcquire(); err == nil {
					release()
					t.Error("native lease escaped before derived synchronization")
				}
				// A DNS failure is projected separately; it cannot rewrite native exit.
			}
			started, err := jobs.Start("owner", CommandRequest{Action: "status"})
			if err != nil {
				t.Fatal(err)
			}
			final := waitNativeJob(t, jobs, started.ID)
			if exit == "0" {
				if !called || final.State != "completed" || final.ExitCode == nil || *final.ExitCode != 0 {
					t.Fatal(final, called)
				}
			} else if called || final.State != "failed" {
				t.Fatal(final, called)
			}
		})
	}
}
