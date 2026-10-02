//go:build linux

package nativegate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func createdIntent(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(testRoot(t), ".pending"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err := f.WriteString("node-operation-pending\n"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNodeIntentProofRequiresOwnerAndExactCurrentFile(t *testing.T) {
	for _, attack := range []string{"normal", "borrower", "lifecycle", "same-bytes-new-inode", "changed-content", "replaced-proof", "owner-loss", "closed-descriptor"} {
		t.Run(attack, func(t *testing.T) {
			root := testRoot(t)
			action := ConfigChange
			if attack == "lifecycle" {
				action = Restart
			}
			lease, err := Acquire(root, action)
			if err != nil {
				t.Fatal(err)
			}
			f := createdIntent(t)
			binder := lease
			if attack == "borrower" {
				binder, err = Join(root, lease.Token())
				if err != nil {
					t.Fatal(err)
				}
			}
			binding, err := binder.BindNodeIntent(f)
			if attack == "borrower" || attack == "lifecycle" {
				if !errors.Is(err, ErrNotOwner) {
					t.Fatal("non-config owner bound intent", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			proof := filepath.Join(root, "operation.lock.d", "node-intent")
			switch attack {
			case "same-bytes-new-inode":
				if err := os.Rename(f.Name(), f.Name()+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.Name(), []byte("node-operation-pending\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-content":
				if _, err := f.WriteAt([]byte("X"), 0); err != nil {
					t.Fatal(err)
				}
			case "replaced-proof":
				data, err := os.ReadFile(proof)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(proof, proof+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(proof, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "owner-loss":
				if err := os.WriteFile(filepath.Join(root, "operation.lock.d", "owner"), []byte("invalid\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "closed-descriptor":
				f.Close()
			}
			if attack != "normal" {
				if !errors.Is(binding.Verify(), ErrNotOwner) {
					t.Fatal("invalid current binding accepted")
				}
				if _, err := os.Lstat(proof); err != nil {
					t.Fatal("evidence removed")
				}
				return
			}
			if err := binding.Verify(); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(binding.Clear(), ErrNotOwner) {
				t.Fatal("proof cleared before durable intent")
			}
			if !errors.Is(lease.Release(), ErrNotOwner) {
				t.Fatal("released outstanding binding")
			}
			if _, err := lease.BindNodeIntent(f); err == nil {
				t.Fatal("existing proof overwritten")
			}
			// The refused second bind leaves no replacement or new temporary file.
			if err := os.Remove(f.Name()); err != nil {
				t.Fatal(err)
			}
			if err := binding.Clear(); err != nil {
				t.Fatal(err)
			}
			if err := lease.Release(); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(binding.Verify(), ErrNotOwner) {
				t.Fatal("closed owner retained authority")
			}
		})
	}
}
