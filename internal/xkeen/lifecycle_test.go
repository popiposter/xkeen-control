package xkeen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNativeLifecycleWaitsForForegroundCompletionAndDoesNotReplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native process fixture requires Linux")
	}
	dir := t.TempDir()
	binary, marker := filepath.Join(dir, "xkeen"), filepath.Join(dir, "done")
	script := "#!/bin/sh\n[ \"$XKEEN_FOREGROUND\" = 1 ] || exit 9\n[ \"$1\" = -restart ] || exit 8\nsleep 0.15\nprintf completed > \"$NATIVE_TEST_MARKER\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NATIVE_TEST_MARKER", marker)
	t.Setenv("XKEEN_FOREGROUND", "0")
	lifecycle := Lifecycle{Binary: binary, Timeout: time.Second}
	if err := lifecycle.Run(context.Background(), Restart); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "completed" {
		t.Fatal("returned before native completion")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	lifecycle.Timeout = 20 * time.Millisecond
	if err := lifecycle.Run(context.Background(), Restart); !errors.Is(err, ErrLifecycleUnknown) {
		t.Fatalf("timeout outcome = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("timeout replayed native operation")
	}
	if err := lifecycle.Run(context.Background(), LifecycleAction("restart; echo invalid")); !errors.Is(err, ErrLifecycleFailed) {
		t.Fatal("accepted arbitrary argv")
	}
}

func TestNativeInitLifecycleUsesServiceArgumentsOnly(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native process fixture requires Linux")
	}
	dir := t.TempDir()
	init, marker := filepath.Join(dir, "S05xkeen"), filepath.Join(dir, "calls")
	script := "#!/bin/sh\n[ $# = 2 ] && [ \"$2\" = on ] || exit 8\ncase \"$1\" in start|stop|restart) ;; *) exit 9;; esac\nprintf '%s\\n' \"$1\" >> \"$NATIVE_INIT_MARKER\"\n"
	if err := os.WriteFile(init, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NATIVE_INIT_MARKER", marker)
	for _, action := range []LifecycleAction{Start, Stop, Restart} {
		if err := (Lifecycle{InitPath: init, Timeout: time.Second}).Run(context.Background(), action); err != nil {
			t.Fatalf("native init %s: %v", action, err)
		}
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "start\nstop\nrestart\n" {
		t.Fatalf("native calls: %q %v", data, err)
	}
	if err := (Lifecycle{InitPath: init, Binary: init}).Run(context.Background(), Start); !errors.Is(err, ErrLifecycleFailed) {
		t.Fatalf("ambiguous executable accepted: %v", err)
	}
	if data, _ := os.ReadFile(marker); string(data) != "start\nstop\nrestart\n" {
		t.Fatal("rejected configuration executed")
	}
}
