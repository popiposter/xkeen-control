//go:build linux

package setup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectNormalNeverCreatesAndReusesSharedAdmission(t *testing.T) {
	d := privateTemp(t)
	lock := filepath.Join(d, "initial-setup.lock")
	receipt := filepath.Join(d, "initial-setup.json")
	if close, e := inspectNormal(lock, receipt); e == nil {
		close()
		t.Fatal("missing inode admitted")
	}
	entries, _ := os.ReadDir(d)
	if len(entries) != 0 {
		t.Fatal("inspection created persistent state")
	}
	exclusive, e := acquireLock(lock, true)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.Stat(lock)
	if close, e := inspectNormal(lock, receipt); !errors.Is(e, ErrBusy) {
		if close != nil {
			close()
		}
		t.Fatal("inspection overlapped EX", e)
	}
	exclusive()
	normal, e := acquireLock(lock, false)
	if e != nil {
		t.Fatal(e)
	}
	defer normal()
	close, e := inspectNormal(lock, receipt)
	if e != nil {
		t.Fatal(e)
	}
	close()
	after, _ := os.Stat(lock)
	if !os.SameFile(before, after) || before.Mode() != after.Mode() || before.ModTime() != after.ModTime() {
		t.Fatal("lock changed")
	}
	// A malformed or incomplete receipt must be refused without editing it.
	if e := writeReceipt(receipt, initialReceipt()); e != nil {
		t.Fatal(e)
	}
	if close, e := inspectNormal(lock, receipt); e == nil {
		close()
		t.Fatal("incomplete setup admitted")
	}
	if e := os.WriteFile(receipt, []byte(`{"schemaVersion":1,"phase":"unknown"}`), 0600); e != nil {
		t.Fatal(e)
	}
	contents, _ := os.ReadFile(receipt)
	if close, e := inspectNormal(lock, receipt); e == nil {
		close()
		t.Fatal("unsafe receipt admitted")
	}
	actual, _ := os.ReadFile(receipt)
	if string(actual) != string(contents) {
		t.Fatal("receipt repaired")
	}
	normal()
	if unlock, e := acquireExistingLock(lock, true); e != nil {
		t.Fatal("refusal leaked SH", e)
	} else {
		unlock()
	}
}
