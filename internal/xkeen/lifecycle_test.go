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
