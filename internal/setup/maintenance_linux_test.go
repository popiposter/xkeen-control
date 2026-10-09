//go:build linux

package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceSubprocessHelper(t *testing.T) {
	path := os.Getenv("XKEEN_TEST_MAINTENANCE_LOCK")
	if path == "" {
		return
	}
	release, err := acquireLock(path, os.Getenv("XKEEN_TEST_MAINTENANCE_MODE") == "exclusive")
	if err != nil {
		os.Exit(9)
	}
	defer release()
	fmt.Println("locked")
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
}

func TestMaintenanceExcludesLiveProcessesAndCrashReleasesKernelLock(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		t.Run(fmt.Sprint(exclusive), func(t *testing.T) {
			path := filepath.Join(privateTemp(t), "guard.lock")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMaintenanceSubprocessHelper$")
			mode := "shared"
			if exclusive {
				mode = "exclusive"
			}
			cmd.Env = append(os.Environ(), "XKEEN_TEST_MAINTENANCE_LOCK="+path, "XKEEN_TEST_MAINTENANCE_MODE="+mode)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatal(line, err)
			}
			if close, err := acquireExistingLock(path, true); !errors.Is(err, ErrBusy) {
				if close != nil {
					close()
				}
				t.Fatal("exclusive overlapped live process", err)
			}
			if exclusive {
				if close, err := acquireLock(path, false); !errors.Is(err, ErrBusy) {
					if close != nil {
						close()
					}
					t.Fatal("normal overlapped maintenance", err)
				}
			}
			before, _ := os.Lstat(path)
			if !exclusive {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if close, err := acquireExistingLock(path, true); !errors.Is(err, ErrState) {
					if close != nil {
						close()
					}
					t.Fatal("maintenance recreated deleted live lock", err)
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("deleted lock recreated", err)
				}
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			if !exclusive {
				return
			}
			release, err := acquireExistingLock(path, true)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			after, _ := os.Lstat(path)
			if !os.SameFile(before, after) {
				t.Fatal("lock inode replaced")
			}
		})
	}
}

func TestMaintenanceMissingAndReplacedLock(t *testing.T) {
	root := privateTemp(t)
	for _, path := range []string{filepath.Join(root, "missing.lock"), filepath.Join(root, "missing", "guard.lock")} {
		if close, err := acquireExistingLock(path, true); !errors.Is(err, ErrState) {
			if close != nil {
				close()
			}
			t.Fatal("missing admission accepted", err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("missing admission created", err)
		}
	}
	path := filepath.Join(root, "guard.lock")
	f, err := acquireLockFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !lockPathMatches(path, f) {
		t.Fatal("original identity rejected")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if lockPathMatches(path, f) {
		t.Fatal("replacement identity accepted")
	}
}
