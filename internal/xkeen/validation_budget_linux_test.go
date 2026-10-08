//go:build linux

package xkeen

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/validationbudget"
)

func TestConfigValidationBudgetAndCallerCancellation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		validator string
		budget    time.Duration
		cancel    bool
		pass      bool
	}{
		{"slow-success", "exec sleep 0.06\n", 400 * time.Millisecond, false, true},
		{"deadline", "exec sleep 1\n", 30 * time.Millisecond, false, false},
		{"caller-cancel", "exec sleep 1\n", time.Second, true, false},
		{"invalid", "exit 1\n", time.Second, false, false},
		{"drift", "printf '{}' > \"$DRIFT_PATH\"\nexit 0\n", time.Second, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := editorFixture(t, tc.validator)
			if e.validationTimeout() != validationbudget.Candidate {
				t.Fatal("target default not used")
			}
			e.ValidationTimeout = tc.budget
			t.Setenv("DRIFT_PATH", filepath.Join(e.Dir, "05_routing.json"))
			before, _ := e.Snapshot(context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				timer := time.AfterFunc(20*time.Millisecond, cancel)
				defer timer.Stop()
			}
			_, err := e.SaveField(ctx, before.Digest, "dns", "queryStrategy", "UseIPv4")
			if (err == nil) != tc.pass {
				t.Fatalf("save result: %v", err)
			}
			if !tc.pass {
				data, _ := os.ReadFile(filepath.Join(e.Dir, "02_dns.json"))
				if string(data) != string(before.files["02_dns.json"]) {
					t.Fatal("failed candidate promoted")
				}
				if pending, err := e.readPending(); err != nil || pending != nil {
					t.Fatal("failed candidate persisted pending")
				}
			}
		})
	}
}

func TestConfiguredPreparationRetainsBudgetAndRejectsExpiredValidation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		e := editorFixture(t, "exit 0\n")
		e.ProcRoot = t.TempDir()
		before, _ := e.Snapshot(context.Background())
		digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
		if err != nil {
			t.Fatal(err)
		}
		e.ValidationTimeout = 400 * time.Millisecond
		validator := "#!/bin/sh\nexec sleep 0.06\n"
		if expired {
			e.ValidationTimeout = 30 * time.Millisecond
			validator = "#!/bin/sh\nexec sleep 1\n"
		}
		if err := os.WriteFile(e.XrayBinary, []byte(validator), 0700); err != nil {
			t.Fatal(err)
		}
		m := testNativeJobs(t, "exit 0\n")
		m.Lease = e.Lease
		starts := 0
		m.startTerminal = func(command *exec.Cmd, interactive bool) (*os.File, error) {
			starts++
			return startNativeTerminal(command, interactive)
		}
		job, err := m.StartConfigured("owner", CommandRequest{Action: "start"}, e, digest)
		if expired {
			if err == nil || starts != 0 {
				t.Fatal("expired validation dispatched native command", err, starts)
			}
		} else {
			if err != nil || starts != 1 {
				t.Fatal("valid slow preparation failed", err, starts)
			}
			waitNativeJob(t, m, job.ID)
		}
	}
}
