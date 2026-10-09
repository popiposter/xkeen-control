//go:build linux

package update

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"golang.org/x/sys/unix"
)

func validReceipt() Receipt {
	b := buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("a", 40), Channel: "stable"}
	c := b
	c.Version = "1.1.0"
	return Receipt{Schema: 1, ID: strings.Repeat("1", 32), Action: "install", ManifestDigest: strings.Repeat("2", 64), PreviousDigest: strings.Repeat("3", 64), Architecture: "arm64", Previous: b, Candidate: c, Phase: "prepared", StartedAt: "2026-10-09T00:00:00Z", UpdatedAt: "2026-10-09T00:00:00Z"}
}

func putReceipt(t *testing.T, path string, r Receipt) {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestMalformedTerminalNeverAdmits(t *testing.T) {
	for name, mutate := range map[string]func(*Receipt){
		"action": func(r *Receipt) { r.Action = "unknown" }, "phase": func(r *Receipt) { r.Phase = "done" },
		"architecture": func(r *Receipt) { r.Architecture = "amd64" }, "digest": func(r *Receipt) { r.ManifestDigest = "bad" },
		"previous-digest": func(r *Receipt) { r.PreviousDigest = "bad" }, "timestamp": func(r *Receipt) { r.UpdatedAt = "yesterday" },
		"time-order": func(r *Receipt) { r.UpdatedAt = "2020-01-01T00:00:00Z" }, "reason": func(r *Receipt) { r.Reason = "secret\ntext" },
		"wrong-verified": func(r *Receipt) { r.Verified = &r.Previous }, "missing-verified": func(r *Receipt) { r.Verified = nil },
	} {
		t.Run(name, func(t *testing.T) {
			d := t.TempDir()
			os.Chmod(d, 0700)
			r := validReceipt()
			r.Phase = "installed-verified"
			r.Verified = &r.Candidate
			mutate(&r)
			path := filepath.Join(d, "panel-update-result.json")
			putReceipt(t, path, r)
			m := NewManager(Config{Paths: Paths{MarkerPath: filepath.Join(d, "installed-release.json")}})
			if _, e := m.Receipt(); e == nil {
				t.Fatal("malformed terminal admitted")
			}
		})
	}
}

func TestTerminalBeforeIntentCleanupStaysFenced(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	r := validReceipt()
	active := filepath.Join(d, "panel-update-active.json")
	putReceipt(t, active, r)
	r.Phase = "installed-verified"
	r.Verified = &r.Candidate
	putReceipt(t, filepath.Join(d, "panel-update-result.json"), r)
	m := NewManager(Config{Paths: Paths{MarkerPath: filepath.Join(d, "installed-release.json")}})
	if result, e := m.Receipt(); e == nil || result == nil || result.Phase != "installed-verified" {
		t.Fatal("cleanup crash lost evidence/fence", result, e)
	}
	if e := os.Remove(active); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Receipt(); e != nil {
		t.Fatal(e)
	}
}

func TestInheritedUpdaterLockDowngradesWithoutOwnershipGap(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned production lock fixture")
	}
	d := t.TempDir()
	os.Chmod(d, 0700)
	path := filepath.Join(d, "initial-setup.lock")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile("../../scripts/xkeen-control-updater")
	if e != nil {
		t.Fatal(e)
	}
	source := string(b)
	start := strings.Index(source, "update_lock() {")
	end := strings.Index(source, "daemon_identity() {")
	if start < 0 || end <= start {
		t.Fatal("production lock functions missing")
	}
	script := source[start:end] + "\nupdate_lock || exit 11\nprintf 'exclusive\\n'\nread answer\nupdate_allow_daemon || exit 12\nprintf 'shared\\n'\nread answer\n"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.Env = append(os.Environ(), "TEST_MODE=0", "SETUP_LOCK="+path, "XKEEN_CONTROL_UPDATE_LOCK_FD=3")
	cmd.ExtraFiles = []*os.File{f}
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	in, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	f.Close()
	r := bufio.NewReader(out)
	if line, e := r.ReadString('\n'); e != nil || line != "exclusive\n" {
		t.Fatal(line, e)
	}
	probe := func(shared bool) bool {
		flag := "-x"
		if shared {
			flag = "-s"
		}
		return exec.CommandContext(ctx, "flock", "-n", flag, path, "true").Run() == nil
	}
	if probe(false) || probe(true) {
		t.Fatal("inherited EX did not exclude independent processes")
	}
	in.Write([]byte("downgrade\n"))
	if line, e := r.ReadString('\n'); e != nil || line != "shared\n" {
		t.Fatal(line, e)
	}
	if probe(false) || !probe(true) {
		t.Fatal("SH handoff did not admit daemon and exclude maintenance")
	}
	in.Write([]byte("terminal\n"))
	in.Close()
	if e = cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	if !probe(false) {
		t.Fatal("terminal process exit retained lock")
	}
}

func TestProtectedHashRejectsSymlinkAndWritablePayload(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "payload")
	os.WriteFile(p, []byte("candidate"), 0600)
	if _, e := protectedHash(p); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(d, "link")
	os.Symlink(p, link)
	if _, e := protectedHash(link); e == nil {
		t.Fatal("symlink admitted")
	}
	os.Chmod(p, 0666)
	if _, e := protectedHash(p); e == nil {
		t.Fatal("writable payload admitted")
	}
}

func TestBuildProbeCannotOutliveSharedDeadline(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "panel")
	if e := os.WriteFile(bin, []byte("#!/bin/sh\ntrap '' TERM\nsleep 30\n"), 0755); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile("../../scripts/xkeen-control-updater")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	start := strings.Index(s, "verify_build() {")
	end := strings.Index(s, "verify_marker() {")
	if start < 0 || end <= start {
		t.Fatal("build probe missing")
	}
	script := s[start:end] + "\nreadiness_deadline=$(( $(date +%s) + 3 ))\nverify_build 1.0.0 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa stable\n"
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.Env = append(os.Environ(), "BIN="+bin)
	started := time.Now()
	e = cmd.Run()
	if e == nil || ctx.Err() != nil || time.Since(started) > 5*time.Second {
		t.Fatal("unbounded or accepted blocked build probe", e, ctx.Err())
	}
}

func TestCapabilityFailurePrecedesReservation(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	r := validReceipt()
	m := newFixtureManager(t, Config{Current: r.Previous, Paths: Paths{MarkerPath: filepath.Join(d, "installed-release.json")}})
	tools := t.TempDir()
	if e := os.WriteFile(filepath.Join(tools, "sync"), []byte("#!/bin/sh\nexit 7\n"), 0755); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", tools)
	if e := m.reserveRecord(r); e == nil {
		t.Fatal("unsupported sync admitted")
	}
	if _, e := os.Lstat(filepath.Join(d, "panel-update-active.json")); !os.IsNotExist(e) {
		t.Fatal("prerequisite failure stranded active intent", e)
	}
}
