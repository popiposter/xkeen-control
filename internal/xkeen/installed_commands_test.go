package xkeen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func TestNativeInstalledCatalogDoesNotExecuteSelfHeal(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "xkeen")
	script := "#!/bin/sh\nexit 99\ncase \"$1\" in\n -h|-help) ;;\n -start) ;;\n -ugc) ;;\nesac\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := NewJobs(binary, authority.NewLease())
	m.RequireInstalledCommands()
	commands := m.InstalledCatalog()
	if len(commands) != 3 {
		t.Fatal("wrong installed feature count", commands)
	}
	if _, err := m.Start("owner", CommandRequest{Action: "update-xkeen"}); err != ErrCommand {
		t.Fatal("unsupported installed flag accepted", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(binary), "receipt")); !os.IsNotExist(err) {
		t.Fatal("catalog inspection wrote files")
	}
}
