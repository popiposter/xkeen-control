//go:build linux

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestAbortUnknownStopReadbackNeverCallsFirmwareInverse(t *testing.T) {
	dir := t.TempDir()
	for name, data := range templates(t) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	xray := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(xray, []byte("synthetic"), 0700); err != nil {
		t.Fatal(err)
	}
	editor := &xkeen.ConfigEditor{Dir: dir, XrayBinary: xray, ProcRoot: t.TempDir()}
	s, err := editor.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inverses := 0
	restore := func() error { inverses++; return nil }
	if restoreAfterStop(context.Background(), editor, s.Digest, restore) != nil || inverses != 1 {
		t.Fatal("positive absence refused")
	}
	inverses = 0
	if err := os.Mkdir(filepath.Join(editor.ProcRoot, "123"), 0700); err != nil {
		t.Fatal(err)
	}
	if restoreAfterStop(context.Background(), editor, s.Digest, restore) == nil || inverses != 0 {
		t.Fatal("unreadable existing process admitted firmware inverse")
	}
	editor.ProcRoot = filepath.Join(t.TempDir(), "unavailable")
	if restoreAfterStop(context.Background(), editor, s.Digest, restore) == nil || inverses != 0 {
		t.Fatal("unknown process directory admitted firmware inverse")
	}
}
