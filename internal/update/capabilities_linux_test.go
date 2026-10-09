//go:build linux

package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDirectFlockSequenceAndCleanup(t *testing.T) {
	d := productionCapabilities()
	d.tempRoot = t.TempDir()
	var statuses []int
	var descriptions []*os.File
	d.run = func(ctx context.Context, path string, args []string, files []*os.File) (int, error) {
		if descriptions == nil {
			descriptions = append([]*os.File{}, files...)
		} else if descriptions[0] != files[0] || descriptions[1] != files[1] {
			t.Fatal("parent descriptions replaced")
		}
		status, e := runCapability(ctx, path, args, files)
		statuses = append(statuses, status)
		return status, e
	}
	if e := flockReady(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(statuses, []int{0, 1, 0, 0, 1}) {
		t.Fatal(statuses)
	}
	for _, f := range descriptions {
		if _, e := f.Stat(); !errors.Is(e, os.ErrClosed) {
			t.Fatal("descriptor retained", e)
		}
	}
	assertCapabilityCleanup(t, d.tempRoot)
}

func assertCapabilityCleanup(t *testing.T, root string) {
	t.Helper()
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("temporary evidence leaked", entries, e)
	}
}

func TestFlockRejectsNonConflictFailures(t *testing.T) {
	for name, code := range map[string]int{"noop": 0, "status127": 127, "other-status": 2, "signal": -1} {
		t.Run(name, func(t *testing.T) {
			d := productionCapabilities()
			d.tempRoot = t.TempDir()
			calls := 0
			d.run = func(context.Context, string, []string, []*os.File) (int, error) {
				calls++
				if calls == 1 {
					return 0, nil
				}
				return code, nil
			}
			if e := flockReady(context.Background(), d); e == nil {
				t.Fatal("non-conflict accepted")
			}
			if calls != 2 {
				t.Fatal(calls)
			}
			assertCapabilityCleanup(t, d.tempRoot)
		})
	}
	for _, missing := range []bool{false, true} {
		d := productionCapabilities()
		d.tempRoot = t.TempDir()
		if missing {
			d.lookup = func(string) (string, error) { return "", os.ErrNotExist }
		} else {
			d.run = func(context.Context, string, []string, []*os.File) (int, error) { return -1, os.ErrPermission }
		}
		if e := flockReady(context.Background(), d); e == nil {
			t.Fatal("lookup/launch failure accepted")
		}
		assertCapabilityCleanup(t, d.tempRoot)
	}
}

func TestFlockUsesOneDeadlineAndCleansOnTimeout(t *testing.T) {
	d := productionCapabilities()
	d.tempRoot = t.TempDir()
	var deadline time.Time
	calls := 0
	d.run = func(ctx context.Context, _ string, _ []string, _ []*os.File) (int, error) {
		got, _ := ctx.Deadline()
		if deadline.IsZero() {
			deadline = got
		} else if !deadline.Equal(got) {
			t.Fatal("deadline renewed")
		}
		calls++
		if calls == 1 {
			return 0, nil
		}
		<-ctx.Done()
		return -1, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if e := flockReady(ctx, d); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if time.Since(started) > time.Second || calls != 2 {
		t.Fatal("probe deadline not bounded", calls)
	}
	assertCapabilityCleanup(t, d.tempRoot)
}

func TestDroppedShellTailDefectDoesNotAffectDirectProbe(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "firmware-shell")
	if e := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec /bin/sh -c \"$2\"\n"), 0755); e != nil {
		t.Fatal(e)
	}
	flock, e := exec.LookPath("flock")
	if e != nil {
		t.Fatal(e)
	}
	status, e := runCapability(context.Background(), wrapper, []string{"-c", `"$1" -n -x 3`, "flock-capability", flock}, nil)
	if e != nil || status != 127 {
		t.Fatal("prior dropped-tail failure not reproduced", status, e)
	}
	d := productionCapabilities()
	d.tempRoot = t.TempDir()
	d.run = func(ctx context.Context, path string, args []string, files []*os.File) (int, error) {
		if path != flock || len(args) != 3 || args[0] != "-n" {
			t.Fatal("probe invoked a shell", path, args)
		}
		return runCapability(ctx, path, args, files)
	}
	if e := flockReady(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	assertCapabilityCleanup(t, d.tempRoot)
}

func TestDiagnosticSharedChecksOnlyTouchRAM(t *testing.T) {
	for _, mode := range []string{"pass", "error", "cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			d := productionCapabilities()
			d.tempRoot = t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			var cancelDeadline context.CancelFunc
			if mode == "timeout" {
				ctx, cancelDeadline = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancelDeadline()
			}
			var names []string
			lookup := d.lookup
			d.lookup = func(name string) (string, error) { names = append(names, name); return lookup(name) }
			d.run = func(ctx context.Context, path string, args []string, files []*os.File) (int, error) {
				if len(args) == 2 && args[0] == "-f" {
					rel, e := filepath.Rel(d.tempRoot, args[1])
					if e != nil || !filepath.IsLocal(rel) {
						t.Fatal("persistent sync path", args)
					}
					if mode == "error" {
						return 127, nil
					}
					if mode == "timeout" {
						<-ctx.Done()
						return -1, ctx.Err()
					}
				}
				return runCapability(ctx, path, args, files)
			}
			r, e := inspectCapabilities(ctx, d)
			if mode == "pass" {
				if e != nil || !r.Ready || !reflect.DeepEqual(names, []string{"sync", "timeout", "flock"}) {
					t.Fatal(r, e, names)
				}
			} else if e == nil || r.Ready {
				t.Fatal("failure accepted", r, e)
			}
			assertCapabilityCleanup(t, d.tempRoot)
		})
	}
}
