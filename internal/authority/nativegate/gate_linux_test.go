//go:build linux

package nativegate

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRoot(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Fatal("native gate fixtures require the supported root Linux container")
	}
	p, err := os.MkdirTemp("/tmp", "nativegate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(p) })
	return p
}

func shell(t *testing.T, script string, env []string) *exec.Cmd {
	t.Helper()
	path, err := filepath.Abs("../../../scripts/native-operation-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c := exec.CommandContext(ctx, "/bin/sh", "-c", ". \"$1\"; "+script, "fixture", path)
	c.Env = append(StripEnvironment(os.Environ()), env...)
	return c
}

func TestExclusiveAdmissionAndOwnTupleRelease(t *testing.T) {
	root := testRoot(t)
	first, err := Acquire(root, Start)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(root, Stop); !errors.Is(err, ErrBusy) {
		t.Fatalf("second owner: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(root, Stop)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("old release: %v", err)
	}
	if _, err := Acquire(root, Start); !errors.Is(err, ErrBusy) {
		t.Fatalf("old release removed current gate: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestIncompleteOwnerNeverReaped(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "operation.lock.d")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(root, Start); !errors.Is(err, ErrBusy) {
		t.Fatalf("incomplete owner: %v", err)
	}
	if err := shell(t, "native_gate_acquire \"$ROOT\" start; test $? -eq 75", []string{"ROOT=" + root}).Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("incomplete owner was removed")
	}
}

func TestGoOwnerShellBorrowAndContention(t *testing.T) {
	root := testRoot(t)
	lease, err := Acquire(root, ConfigChange)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	cmd := shell(t, `native_gate_join "$XKEEN_GATE_ROOT" "$XKEEN_GATE_TOKEN" || exit 10
native_gate_release; test $? -eq 77 || exit 11
native_gate_strip
test -z "${XKEEN_GATE_TOKEN+x}" && test -z "${XKEEN_GATE_ROOT+x}"`, lease.ChildEnvironment(nil))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("borrow: %v %s", err, out)
	}
	cmd = shell(t, `native_gate_acquire "$ROOT" stop; test $? -eq 75`, []string{"ROOT=" + root})
	if err := cmd.Run(); err != nil {
		t.Fatalf("shell contested Go owner: %v", err)
	}
}

func TestShellOwnerGoBorrowAndOwnerRelease(t *testing.T) {
	root := testRoot(t)
	c := shell(t, `native_gate_acquire "$ROOT" restart || exit 10
"$TEST_BINARY" -test.run '^TestGateChild$' || exit 11
native_gate_release || exit 12`, []string{"ROOT=" + root, "TEST_BINARY=" + os.Args[0], "NATIVE_GATE_CHILD=borrow"})
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("shell owner: %v %s", err, out)
	}
	lease, err := Acquire(root, Start)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestGateChild(t *testing.T) {
	if os.Getenv("NATIVE_GATE_CHILD") != "borrow" {
		return
	}
	lease, err := Join(os.Getenv("XKEEN_GATE_ROOT"), os.Getenv("XKEEN_GATE_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("borrower release: %v", err)
	}
}

func TestShellCrashRetainsGateAndRejectsSiblingBorrow(t *testing.T) {
	root := testRoot(t)
	cmd := shell(t, `native_gate_acquire "$ROOT" start || exit 10
printf '%s\n' "$XKEEN_GATE_TOKEN"
read answer`, []string{"ROOT=" + root})
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(out)
	if !scan.Scan() {
		t.Fatal("shell did not acquire gate")
	}
	token := scan.Text()
	if _, err := Join(root, token); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-descendant borrowed token: %v", err)
	}
	if _, err := Acquire(root, Stop); !errors.Is(err, ErrBusy) {
		t.Fatalf("Go contested shell owner: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if _, err := Join(root, token); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("dead owner joined: %v", err)
	}
	if _, err := Acquire(root, Stop); !errors.Is(err, ErrBusy) {
		t.Fatalf("crash gate was reaped: %v", err)
	}
}

func TestRejectUnsafeRoots(t *testing.T) {
	root := testRoot(t)
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{unsafe, link, root + "/../" + filepath.Base(root)} {
		if _, err := Acquire(path, Start); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unsafe %q: %v", path, err)
		}
		if err := shell(t, `native_gate_acquire "$ROOT" start; test $? -eq 76`, []string{"ROOT=" + path}).Run(); err != nil {
			t.Fatalf("shell unsafe root: %v", err)
		}
	}
}

func TestTamperedOwnerCannotReleaseOrJoin(t *testing.T) {
	root := testRoot(t)
	lease, err := Acquire(root, Start)
	if err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(root, "operation.lock.d", "owner")
	data, err := os.ReadFile(owner)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	fields[3] = "1" // wrong process starttime, even with live PID/token
	if err := os.WriteFile(owner, []byte(strings.Join(fields, " ")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Join(root, lease.Token()); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("changed starttime accepted: %v", err)
	}
	if err := lease.Release(); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("changed tuple released: %v", err)
	}
	if _, err := os.Stat(owner); err != nil {
		t.Fatal("changed owner removed")
	}
}

func TestEnvironmentProjectionIsExplicitAndStripsSpoofs(t *testing.T) {
	root := testRoot(t)
	lease, err := Acquire(root, Start)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	base := []string{"KEEP=value", "XKEEN_GATE_TOKEN=forged", "XKEEN_GATE_ROOT=/tmp/forged"}
	env := lease.ChildEnvironment(base)
	if strings.Join(StripEnvironment(env), "|") != "KEEP=value" {
		t.Fatal("background environment retained gate context")
	}
	if strings.Contains(strings.Join(env, "|"), "forged") {
		t.Fatal("inherited spoof survived projection")
	}
}

func TestShellSupportsRecordedBusyBoxStatProfile(t *testing.T) {
	root := testRoot(t)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	stat, err := exec.LookPath("stat")
	if err != nil {
		t.Fatal(err)
	}
	// Run real metadata reads, but reject flags absent from the appliance stat.
	shim := "#!/bin/sh\n[ \"$1\" = -t ] || exit 2\nexec " + stat + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "stat"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := shell(t, `native_gate_acquire "$ROOT" start || exit 10
native_gate_release || exit 11`, []string{"ROOT=" + root, "PATH=" + bin + ":" + os.Getenv("PATH")})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("recorded stat profile: %v %s", err, out)
	}
}

func TestShellSupportsLimitedBusyBoxODAndGoBorrow(t *testing.T) {
	root := testRoot(t)
	// Recorded appliance od supports -v/-b, but not GNU -A/-N/-t.
	// Keep real random bytes and od formatting; constrain only the flags.
	cmd := shell(t, `od() {
    for flag in "$@"; do
        case "$flag" in -v|-b) ;; *) return 2;; esac
    done
    command od "$@"
}
native_gate_acquire "$ROOT" restart || exit 10
"$TEST_BINARY" -test.run '^TestGateChild$' || exit 11
native_gate_release || exit 12`, []string{"ROOT=" + root, "TEST_BINARY=" + os.Args[0], "NATIVE_GATE_CHILD=borrow"})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("limited od profile: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "operation.lock.d")); !os.IsNotExist(err) {
		t.Fatal("limited od owner did not release")
	}
}

func TestShellRejectsTruncatedOrMalformedEntropyBeforeGateCreation(t *testing.T) {
	for name, shim := range map[string]string{
		"truncated":            `dd() { printf '12345678'; }`,
		"odd-truncated":        `dd() { printf '123456789012345'; }`,
		"oversized":            `dd() { printf '12345678901234567'; }`,
		"empty":                `dd() { return 1; }`,
		"malformed":            `od() { printf '%s\n' '0000000 zzzz zzzz zzzz zzzz zzzz zzzz zzzz zzzz' '0000020'; }`,
		"invalid-octal":        `od() { printf '%s\n' '0000000 008 001 002 003 004 005 006 007 010 011 012 013 014 015 016 017' '0000020'; }`,
		"oversized-octal-byte": `od() { printf '%s\n' '0000000 400 001 002 003 004 005 006 007 010 011 012 013 014 015 016 017' '0000020'; }`,
	} {
		t.Run(name, func(t *testing.T) {
			root := testRoot(t)
			cmd := shell(t, shim+`;
native_gate_acquire "$ROOT" start
test $? -eq 76`, []string{"ROOT=" + root})
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid entropy admitted: %v %s", err, out)
			}
			if _, err := os.Lstat(filepath.Join(root, "operation.lock.d")); !os.IsNotExist(err) {
				t.Fatal("invalid entropy created gate")
			}
		})
	}
}

func TestShellTokenPreservesAllSixteenBytes(t *testing.T) {
	root := testRoot(t)
	cmd := shell(t, `dd() { printf '\000\001\002\003\017\020\077\100\177\200\201\237\240\376\377\125'; }
native_gate_acquire "$ROOT" start || exit 10
test "$XKEEN_GATE_TOKEN" = 000102030f103f407f80819fa0feff55 || exit 11
native_gate_release || exit 12`, []string{"ROOT=" + root})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("byte-exact token conversion: %v %s", err, out)
	}
}

func TestShellSubshellCannotReleaseParentGate(t *testing.T) {
	root := testRoot(t)
	cmd := shell(t, `native_gate_acquire "$ROOT" start || exit 10
(native_gate_release; test $? -eq 77) || exit 11
native_gate_release || exit 12`, []string{"ROOT=" + root})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subshell ownership: %v %s", err, out)
	}
}

func TestOwnerSymlinkAndUnexpectedFilesRemainUntouched(t *testing.T) {
	root := testRoot(t)
	lease, err := Acquire(root, Start)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "operation.lock.d")
	owner := filepath.Join(path, "owner")
	if err := os.Rename(owner, filepath.Join(root, "saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "saved"), owner); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("symlink release: %v", err)
	}
	cmd := shell(t, `native_gate_join "$XKEEN_GATE_ROOT" "$XKEEN_GATE_TOKEN"; test $? -eq 77`, lease.ChildEnvironment(nil))
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(owner); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "saved"), owner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "unknown"), []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("unknown entry release: %v", err)
	}
	if _, err := os.Stat(owner); err != nil {
		t.Fatal("owner removed before detecting unknown entry")
	}
}

func TestCanonicalOwnerGrammarAcrossPeers(t *testing.T) {
	root := testRoot(t)
	lease, err := Acquire(root, Start)
	if err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(root, "operation.lock.d", "owner")
	data, err := os.ReadFile(owner)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	fields[1] = "-" + fields[1][1:]
	bad := strings.Join(fields, " ") + "\n"
	if err := os.WriteFile(owner, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwner(root); err == nil {
		t.Fatal("Go accepted malformed boot UUID")
	}
	cmd := shell(t, `_native_gate_read_owner "$ROOT"; test $? -eq 77`, []string{"ROOT=" + root})
	if err := cmd.Run(); err != nil {
		t.Fatal("shell accepted malformed boot UUID")
	}
	_ = lease // corrupted owner deliberately retained
}

func TestSimultaneousContendersHaveOneOwner(t *testing.T) {
	root := testRoot(t)
	const count = 16
	start := make(chan struct{})
	results := make(chan *Lease, count)
	errorsFound := make(chan error, count)
	var ready sync.WaitGroup
	ready.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			ready.Done()
			<-start
			lease, err := Acquire(root, Start)
			if err != nil && !errors.Is(err, ErrBusy) {
				errorsFound <- err
			}
			results <- lease
		}()
	}
	ready.Wait()
	close(start)
	var owners []*Lease
	for i := 0; i < count; i++ {
		if lease := <-results; lease != nil {
			owners = append(owners, lease)
		}
	}
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("unexpected contender result: %v", err)
	}
	if len(owners) != 1 {
		t.Fatalf("owners = %d", len(owners))
	}
	if err := owners[0].Release(); err != nil {
		t.Fatal(err)
	}
}
