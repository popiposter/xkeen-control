package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReauthenticateSharesLockoutAccountingWithoutCreatingSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	const password = "synthetic-current-password"
	if err := SetPassword(path, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{HashPath: path, LockoutAfter: 2, LockoutFor: time.Hour})
	if err := manager.Reauthenticate("127.0.0.1", "wrong-password"); !errors.Is(err, ErrReauthenticationFailed) {
		t.Fatalf("first reauthentication = %v", err)
	}
	if err := manager.Reauthenticate("127.0.0.1", "wrong-password"); !errors.Is(err, ErrReauthenticationFailed) {
		t.Fatalf("second reauthentication = %v", err)
	}
	if err := manager.Reauthenticate("127.0.0.1", password); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked reauthentication = %v", err)
	}
	if len(manager.sessions) != 0 {
		t.Fatal("reauthentication created a session")
	}

	manager = NewManager(Config{HashPath: path})
	if err := manager.Reauthenticate("127.0.0.1", password); err != nil {
		t.Fatal(err)
	}
	if len(manager.sessions) != 0 || len(manager.attempts) != 0 {
		t.Fatal("successful reauthentication left session or failure state")
	}
}
