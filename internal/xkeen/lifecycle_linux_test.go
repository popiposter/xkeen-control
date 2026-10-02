//go:build linux

package xkeen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeLifecycleReturnsWhenForegroundExitsWithBackgroundService(t *testing.T) {
	for _, code := range []string{"0", "7"} {
		t.Run(code, func(t *testing.T) {
			dir := t.TempDir()
			binary, pidPath := filepath.Join(dir, "xkeen"), filepath.Join(dir, "child.pid")
			t.Setenv("NATIVE_TEST_CHILD_PID", pidPath)
			t.Setenv("NATIVE_TEST_EXIT", code)
			script := "#!/bin/sh\nsleep 30 &\nprintf '%s' \"$!\" > \"$NATIVE_TEST_CHILD_PID\"\necho synthetic-private-output\necho synthetic-private-error >&2\nexit \"$NATIVE_TEST_EXIT\"\n"
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			err := (Lifecycle{Binary: binary, Timeout: time.Second}).Run(context.Background(), Restart)
			data, readErr := os.ReadFile(pidPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil || pid <= 1 {
				t.Fatal("invalid fixture child")
			}
			defer syscall.Kill(pid, syscall.SIGKILL)
			if errors.Is(err, ErrLifecycleUnknown) {
				t.Fatal("foreground exited, but inherited background output caused a false timeout")
			}
			if code == "0" && err != nil {
				t.Fatal(err)
			}
			if code == "7" && !errors.Is(err, ErrLifecycleFailed) {
				t.Fatalf("exit failure lost: %v", err)
			}
			if err := syscall.Kill(pid, 0); err != nil {
				t.Fatal("completed lifecycle killed its background service")
			}
		})
	}
}
