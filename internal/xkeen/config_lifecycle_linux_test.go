//go:build linux

package xkeen

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func TestConfigSetValidatesTogetherAndDiscardRestoresAllOriginals(t *testing.T) {
	e := editorFixture(t, "grep -q UseIPv4 \"$4/02_dns.json\" || exit 4\ngrep -q IPOnDemand \"$4/05_routing.json\" || exit 5\nexit 0\n")
	before, _ := e.Snapshot(context.Background())
	texts := map[string]string{"02_dns.json": `{"dns":{"queryStrategy":"UseIPv4"}}`, "05_routing.json": `{"routing":{"domainStrategy":"IPOnDemand"}}`}
	if _, err := e.SaveTexts(context.Background(), before.Digest, texts); err != nil {
		t.Fatal("cross-file candidate was not validated together", err)
	}
	// Restore validates against the current core too; switch the synthetic
	// validator to accept the original set before the explicit discard.
	if err := os.WriteFile(e.XrayBinary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	saved, _ := e.Snapshot(context.Background())
	if _, err := e.RestoreSaved(context.Background(), saved.Digest); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Snapshot(context.Background())
	if after.Digest != before.Digest {
		t.Fatal("discard did not restore complete original config")
	}
	if pending, err := e.readPending(); err != nil || pending != nil {
		t.Fatal("discard retained pending set", err)
	}
}

func configProcessFixture(t *testing.T, e *ConfigEditor, pid, start string) {
	t.Helper()
	dir := filepath.Join(e.ProcRoot, pid)
	if os.MkdirAll(dir, 0700) != nil {
		t.Fatal("proc fixture")
	}
	fields := make([]string, 20)
	for index := range fields {
		fields[index] = "0"
	}
	fields[0] = "S"
	fields[19] = start
	files := map[string]string{"comm": "xray\n", "cmdline": e.XrayBinary + "\x00run\x00-confdir\x00" + e.Dir + "\x00", "stat": pid + " (xray) " + strings.Join(fields, " ")}
	for name, text := range files {
		if os.WriteFile(filepath.Join(dir, name), []byte(text), 0600) != nil {
			t.Fatal("proc data")
		}
	}
	_ = os.Remove(filepath.Join(dir, "exe"))
	if os.Symlink(e.XrayBinary, filepath.Join(dir, "exe")) != nil {
		t.Fatal("proc executable")
	}
}

func TestNativeConfigApplyExecutesOnceVerifiesFreshProcessAndOffersPreviousRestore(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.ProcRoot = t.TempDir()
	e.Lease = authority.NewLease()
	configProcessFixture(t, e, "101", "500")
	before, _ := e.Snapshot(context.Background())
	digest, err := e.SaveTexts(context.Background(), before.Digest, map[string]string{"02_dns.json": `{"dns":{"queryStrategy":"UseIPv4"}}`, "05_routing.json": `{"routing":{"domainStrategy":"IPOnDemand"}}`})
	if err != nil {
		t.Fatal(err)
	}
	m := testNativeJobs(t, "[ \"$1\" = -restart ] || exit 9\nprintf 'native restart fixture completed'\n")
	m.Lease = e.Lease
	starts := 0
	m.startTerminal = func(command *exec.Cmd, interactive bool) (*os.File, error) {
		starts++
		terminal, err := startNativeTerminal(command, interactive)
		if err == nil {
			if os.RemoveAll(filepath.Join(e.ProcRoot, "101")) != nil {
				t.Fatal("old proc")
			}
			configProcessFixture(t, e, "102", "600")
		}
		return terminal, err
	}
	job, err := m.ApplyConfigs("owner", e, digest)
	if err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, m, job.ID)
	if final.State != "completed" || final.ConfigurationState != "applied" || starts != 1 {
		t.Fatal("Apply not independently confirmed", final, starts)
	}
	w, err := e.Workspace(context.Background())
	if err != nil || w.Pending != nil || !w.HasPrevious {
		t.Fatal("previous generation unavailable", err)
	}
	if _, err := e.RestorePrevious(context.Background(), digest); err != nil {
		t.Fatal(err)
	}
	restored, _ := e.Snapshot(context.Background())
	if restored.Digest != before.Digest || starts != 1 {
		t.Fatal("restore restarted or lost original configs")
	}
	w, _ = e.Workspace(context.Background())
	if w.Pending == nil || !w.Pending.RestartRequired {
		t.Fatal("restore did not await explicit Apply")
	}
}

func TestConfigApplyExitZeroWithSameProcessIsUnknownAndNeverAutomaticallyRollsBack(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.ProcRoot = t.TempDir()
	e.Lease = authority.NewLease()
	configProcessFixture(t, e, "101", "500")
	before, _ := e.Snapshot(context.Background())
	digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
	if err != nil {
		t.Fatal(err)
	}
	m := testNativeJobs(t, "exit 0\n")
	m.Lease = e.Lease
	job, err := m.ApplyConfigs("owner", e, digest)
	if err != nil {
		t.Fatal(err)
	}
	final := waitNativeJob(t, m, job.ID)
	if final.State != "completed" || final.ConfigurationState != "unknown" {
		t.Fatal("exit zero treated as verified Apply", final)
	}
	after, _ := e.Snapshot(context.Background())
	if after.Digest != digest {
		t.Fatal("forced rollback")
	}
	if _, err := m.ApplyConfigs("owner", e, digest); err == nil {
		t.Fatal("unknown Apply replayed")
	}
	if _, err := e.RestoreSaved(context.Background(), digest); err != nil {
		t.Fatal("optional pre-apply restore unavailable", err)
	}
	w, _ := e.Workspace(context.Background())
	if w.Pending == nil || !w.Pending.RestartRequired {
		t.Fatal("restored failed set did not require explicit restart")
	}
}

func TestConfigApplyRejectsAlternateConfigSourceAndUnreadableProcess(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.ProcRoot = t.TempDir()
	configProcessFixture(t, e, "101", "500")
	path := filepath.Join(e.ProcRoot, "101", "cmdline")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(f, "-config\x00/extra.json\x00")
	_ = f.Close()
	if _, err := e.configProcess(context.Background()); err == nil {
		t.Fatal("untracked config source accepted")
	}
	if os.Remove(filepath.Join(e.ProcRoot, "101", "comm")) != nil {
		t.Fatal("proc fixture")
	}
	if _, err := e.configProcess(context.Background()); err == nil {
		t.Fatal("unreadable process treated as absent")
	}
}

func TestConfigApplyCannotConfirmValidatorOrDumpProcess(t *testing.T) {
	for _, mode := range []string{"-test", "-test=true", "--test=true", "-dump", "-dump=true"} {
		t.Run(mode, func(t *testing.T) {
			e := editorFixture(t, "exit 0\n")
			e.ProcRoot = t.TempDir()
			before, _ := e.Snapshot(context.Background())
			digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.beginApply(context.Background(), digest, strings.Repeat("1", 32)); err != nil {
				t.Fatal(err)
			}
			configProcessFixture(t, e, "102", "600")
			path := filepath.Join(e.ProcRoot, "102", "cmdline")
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(f, mode+"\x00")
			_ = f.Close()
			if result := e.finishApply(context.Background(), strings.Repeat("1", 32), "completed"); result != "unknown" {
				t.Fatal("validator accepted as service", result)
			}
			if pending, _ := e.readPending(); pending == nil || pending.ApplyState != "unknown" {
				t.Fatal("pending set lost")
			}
		})
	}
}

func TestConfigApplyRefusesDriftDuringValidationBeforeNativeExec(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.ProcRoot = t.TempDir()
	before, _ := e.Snapshot(context.Background())
	digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic external writer changes the live file during candidate validation.
	script := "#!/bin/sh\nprintf '%s' '{\"dns\":{\"queryStrategy\":\"UseIPv6\"}}' > " + strconv.Quote(filepath.Join(e.Dir, "02_dns.json")) + "\nexit 0\n"
	if os.WriteFile(e.XrayBinary, []byte(script), 0700) != nil {
		t.Fatal("validator fixture")
	}
	m := testNativeJobs(t, "exit 0\n")
	m.Lease = e.Lease
	starts := 0
	m.startTerminal = func(command *exec.Cmd, interactive bool) (*os.File, error) {
		starts++
		return startNativeTerminal(command, interactive)
	}
	if _, err := m.ApplyConfigs("owner", e, digest); err == nil || starts != 0 {
		t.Fatal("drift allowed native exec", err, starts)
	}
	pending, err := e.readPending()
	if err != nil || pending == nil || pending.ApplyID != "" {
		t.Fatal("drift published Apply intent", err)
	}
}
