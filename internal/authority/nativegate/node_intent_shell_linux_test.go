//go:build linux

package nativegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual verifier parser against a proof emitted by Go, rather
// than teaching a shell fixture a second copy of the record encoder.
func TestGoNodeIntentProofMatchesNativeVerifierParser(t *testing.T) {
	root := testRoot(t)
	protected, err := os.MkdirTemp("/opt", "native-intent-interop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(protected) })
	pending := filepath.Join(protected, ".pending")
	f, err := os.OpenFile(pending, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("node-operation-pending\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	lease, err := Acquire(root, ConfigChange)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.BindNodeIntent(f); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../../scripts/native-admission-verify.sh")
	if err != nil {
		t.Fatal(err)
	}
	parser, _, found := strings.Cut(string(source), "_nv_input_file() {")
	if !found || !strings.Contains(parser, "_nv_pending() {") {
		t.Fatal("verifier parser boundary changed")
	}
	parser = strings.NewReplacer(
		"/opt/etc/xkeen-control/secrets/previous/.pending", filepath.Join(protected, "fallback.pending"),
		"/opt/etc/xkeen-control/previous/.pending", pending,
		"/tmp/.xkeen-admission", root,
	).Replace(parser)
	run := func(want int) {
		t.Helper()
		cmd := shell(t, parser+"\nnative_gate_join \"$XKEEN_GATE_ROOT\" \"$XKEEN_GATE_TOKEN\" || exit $?\n_nv_pending\n", lease.ChildEnvironment(nil))
		out, err := cmd.CombinedOutput()
		got := cmd.ProcessState.ExitCode()
		if got != want {
			t.Fatalf("actual verifier parser status %d, want %d: %v %s", got, want, err, out)
		}
	}
	run(0)
	// Keep the original descriptor open, so same bytes cannot conceal a newly
	// created intent or benefit from inode reuse.
	if err := os.Rename(pending, pending+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("node-operation-pending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(75)
	if err := os.Remove(pending); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(pending+".original", pending); err != nil {
		t.Fatal(err)
	}
	run(0)
	if err := os.WriteFile(filepath.Join(protected, "fallback.pending"), []byte("node-operation-pending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(75)
}
