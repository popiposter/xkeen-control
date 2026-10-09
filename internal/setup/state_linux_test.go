//go:build linux

package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
)

func initialReceipt() Receipt {
	return Receipt{Schema: 1, Release: buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("a", 40), Channel: "stable"}, Phase: "prepared"}
}
func TestSetupLockExcludesDaemonAndCompetingSetup(t *testing.T) {
	path := filepath.Join(privateTemp(t), "guard.lock")
	close, e := acquireLock(path, true)
	if e != nil {
		t.Fatal(e)
	}
	for _, exclusive := range []bool{false, true} {
		if release, e := acquireLock(path, exclusive); !errors.Is(e, ErrBusy) {
			if release != nil {
				release()
			}
			t.Fatalf("concurrent lock accepted: %v", e)
		}
	}
	close()
	normal, e := acquireLock(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer normal()
	second, e := acquireLock(path, false)
	if e != nil {
		t.Fatal(e)
	}
	second()
	if release, e := acquireLock(path, true); !errors.Is(e, ErrBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("setup entered running daemon: %v", e)
	}
}
func TestIncompleteReceiptPersistsAndUnknownNeverBecomesAbsent(t *testing.T) {
	path := filepath.Join(privateTemp(t), "initial-setup.json")
	if r, e := readReceipt(path); e != nil || r != nil {
		t.Fatal(r, e)
	}
	r := initialReceipt()
	if e := writeReceipt(path, r); e != nil {
		t.Fatal(e)
	}
	for _, phase := range []string{"native", "activation", "unknown", "aborted"} {
		r.Phase = phase
		if e := writeReceipt(path, r); e != nil {
			t.Fatal(e)
		}
		actual, e := readReceipt(path)
		if e != nil || actual == nil || actual.Phase != phase {
			t.Fatal(actual, e)
		}
	}
	r.Phase = "completed"
	if e := writeReceipt(path, r); !errors.Is(e, ErrState) {
		t.Fatal("unproven completion accepted", e)
	}
	for _, b := range []string{`{}`, `{"schemaVersion":1,"schemaVersion":1}`, `null`, strings.Repeat("x", maxReceipt+1)} {
		if e := os.WriteFile(path, []byte(b), 0600); e != nil {
			t.Fatal(e)
		}
		if r, e := readReceipt(path); r != nil || !errors.Is(e, ErrState) {
			t.Fatal("unsafe receipt accepted", r, e)
		}
	}
}
func TestSetupReceiptAndLockRejectUnsafePaths(t *testing.T) {
	d := privateTemp(t)
	path := filepath.Join(d, "state.json")
	target := filepath.Join(d, "target")
	if e := os.WriteFile(target, []byte("unchanged"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, path); e != nil {
		t.Fatal(e)
	}
	if _, e := readReceipt(path); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	if e := writeReceipt(path, initialReceipt()); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	if _, e := acquireLock(path, true); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(target); string(b) != "unchanged" {
		t.Fatal("symlink target changed")
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("{}"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := readReceipt(path); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	if e := os.Chmod(d, 0755); e != nil {
		t.Fatal(e)
	}
	if e := writeReceipt(path, initialReceipt()); !errors.Is(e, ErrState) {
		t.Fatal("nonprivate parent accepted", e)
	}
}
func TestCrashBeforePromotionRetainsInspectionFence(t *testing.T) {
	path := filepath.Join(privateTemp(t), "state.json")
	r := initialReceipt()
	if e := writeReceipt(path, r); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path+".new", []byte("partial"), 0600); e != nil {
		t.Fatal(e)
	}
	r.Phase = "native"
	if e := writeReceipt(path, r); !errors.Is(e, ErrState) {
		t.Fatal("stale candidate overwritten", e)
	}
	actual, e := readReceipt(path)
	if e != nil || actual.Phase != "prepared" {
		t.Fatal(actual, e)
	}
}

func privateTemp(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSchemaTwoAndLegacyPendingRemainFenced(t *testing.T) {
	for _, schema := range []int{1, 2} {
		root := privateTemp(t)
		receipt := filepath.Join(root, "receipt.json")
		lock := filepath.Join(root, "guard.lock")
		close, e := acquireLock(lock, false)
		if e != nil {
			t.Fatal(e)
		}
		close()
		r := initialReceipt()
		r.Schema = schema
		r.Phase = "activation"
		if e := writeReceipt(receipt, r); e != nil {
			t.Fatal(e)
		}
		if release, e := inspectNormal(lock, receipt); e == nil {
			release()
			t.Fatal("incomplete admitted", schema)
		}
	}
}
