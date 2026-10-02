package nodes

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNativeInitRestartFailureDoesNotBecomeStart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native init fixture requires Linux")
	}
	dir := t.TempDir()
	init, calls := filepath.Join(dir, "S05xkeen"), filepath.Join(dir, "calls")
	t.Setenv("NATIVE_INIT_CALLS", calls)
	if err := os.WriteFile(init, []byte("#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\" >> \"$NATIVE_INIT_CALLS\"\n[ \"$1\" != restart ]\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := CommandActivator{NativeLifecycleInit: init, RestartTimeout: time.Second}
	if err := a.Restart(context.Background()); err == nil {
		t.Error("failed native restart reported success")
	}
	if data, err := os.ReadFile(calls); err != nil || string(data) != "restart on\n" {
		t.Fatalf("native calls=%q err=%v", data, err)
	}
}
