//go:build linux

package xkeen

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func TestNativeInterruptedReceiptNeverReplays(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "last-job.json")
	raw := `{"id":"0123456789abcdef0123456789abcdef","action":"geodata-schedule","state":"running"}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := NewPersistentJobs("/nonexistent", authority.NewLease(), path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := m.Read("new-session", "", 0)
	if err != nil || v.State != "unknown" || v.Output != "" {
		t.Fatal("interrupted state was lost", v, err)
	}
	if _, err := m.Start("new-session", CommandRequest{Action: "start"}); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("replayed interrupted job", err)
	}
}

func TestNativeReceiptContainsOnlyFactsAndRestoresCompleted(t *testing.T) {
	m := testNativeJobs(t, "printf 'private-console-answer'\n")
	m.receiptPath = filepath.Join(t.TempDir(), "last-job.json")
	if err := os.Chmod(filepath.Dir(m.receiptPath), 0700); err != nil {
		t.Fatal(err)
	}
	v, err := m.Start("private-session-token", CommandRequest{Action: "status"})
	if err != nil {
		t.Fatal(err)
	}
	// Read waits using the job's actual owner, unlike the common helper.
	m.mu.Lock()
	m.job.owner = "owner"
	m.mu.Unlock()
	final := waitNativeJob(t, m, v.ID)
	if final.State != "completed" {
		t.Fatal(final.State)
	}
	data, err := os.ReadFile(m.receiptPath)
	if err != nil || strings.Contains(string(data), "private") {
		t.Fatal("receipt leaked private data", err)
	}
	restored, err := NewPersistentJobs(m.Binary, authority.NewLease(), m.receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	view, err := restored.Read("new-owner", "", 0)
	if err != nil || view.State != "completed" || view.Output != "" {
		t.Fatal(view, err)
	}
}

func TestNativeReceiptFailurePreventsProcessStart(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	m := testNativeJobs(t, "touch '"+marker+"'\n")
	m.receiptPath = filepath.Join(dir, "missing-parent", "last-job.json")
	if _, err := m.Start("owner", CommandRequest{Action: "start"}); err == nil {
		t.Fatal("accepted failed receipt")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("native executable ran before durable intent")
	}
	if _, err := m.Lease.TryAcquire(); !errors.Is(err, authority.ErrBlocked) {
		t.Fatal("failed intent not blocked", err)
	}
}

func TestNativeTerminalStartupErrorRetainsUnknown(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "native-wrote")
	m := testNativeJobs(t, "touch '"+marker+"'\n")
	m.startTerminal = func(command *exec.Cmd, interactive bool) (*os.File, error) {
		terminal, err := startNativeTerminal(command, interactive)
		if err != nil {
			return nil, err
		}
		_ = command.Wait()
		_ = terminal.Close()
		return nil, errors.New("simulated post-exec terminal setup failure")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m.receiptPath = filepath.Join(dir, "last-job.json")
	if _, err := m.Start("owner", CommandRequest{Action: "geodata-schedule"}); err == nil {
		t.Fatal("accepted terminal setup failure")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("post-exec failure not exercised", err)
	}
	v, err := m.Read("owner", "", 0)
	if err != nil || v.State != "unknown" {
		t.Fatal("startup error became replayable", v, err)
	}
	restored, err := NewPersistentJobs(m.Binary, authority.NewLease(), m.receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Start("owner", CommandRequest{Action: "start"}); !errors.Is(err, authority.ErrBusy) {
		t.Fatal("startup uncertainty lost on restart", err)
	}
}

func TestNativeReceiptRejectsMalformedOrLinkedState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "last-job.json")
	for _, data := range []string{`{}`, `{"id":"0123456789abcdef0123456789abcdef","action":"start","state":"completed","output":"private"}`, strings.Repeat("x", 4097)} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewPersistentJobs("/nonexistent", authority.NewLease(), path); err == nil {
			t.Fatal("accepted malformed receipt")
		}
	}
	os.Remove(path)
	if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentJobs("/nonexistent", authority.NewLease(), path); err == nil {
		t.Fatal("accepted linked receipt")
	}
}
