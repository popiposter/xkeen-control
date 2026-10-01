package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordRotationRejectsInFlightOldAuthority(t *testing.T) {
	for _, operation := range []string{"login", "reauthenticate"} {
		t.Run(operation, func(t *testing.T) {
			const oldPassword = "synthetic-old-password"
			const newPassword = "synthetic-new-password"
			path := filepath.Join(t.TempDir(), "password.bcrypt")
			if err := SetPassword(path, []byte(oldPassword)); err != nil {
				t.Fatal(err)
			}
			manager := NewManager(Config{HashPath: path})
			_, admittedToken, err := manager.Login("admitted", oldPassword)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: admittedToken})
			if _, valid := manager.SessionFromRequest(request); !valid {
				t.Fatal("pre-rotation session was not admitted")
			}

			verified := make(chan error, 1)
			release := make(chan struct{})
			defer close(release)
			compare := func(hash, password []byte) error {
				err := bcrypt.CompareHashAndPassword(hash, password)
				verified <- err
				<-release
				return err
			}
			type result struct {
				session Session
				token   string
				err     error
			}
			finished := make(chan result, 1)
			go func() {
				if operation == "login" {
					s, token, err := manager.login("in-flight", oldPassword, compare)
					finished <- result{s, token, err}
				} else {
					finished <- result{err: manager.reauthenticate("in-flight", oldPassword, compare)}
				}
			}()
			select {
			case err := <-verified:
				if err != nil {
					t.Fatal("old password did not verify before rotation")
				}
			case <-time.After(20 * time.Second):
				t.Fatal("old authority comparison did not reach barrier")
			}
			// Rotation must finish while the old comparison remains paused.
			replaced := make(chan error, 1)
			go func() { replaced <- manager.ReplacePassword([]byte(newPassword)) }()
			select {
			case err := <-replaced:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("rotation blocked on in-flight bcrypt comparison")
			}
			if _, valid := manager.SessionFromRequest(request); valid {
				t.Fatal("rotation retained a previously admitted session")
			}
			release <- struct{}{}
			var got result
			select {
			case got = <-finished:
			case <-time.After(20 * time.Second):
				t.Fatal("old verification did not finish")
			}
			want := ErrInvalidCredentials
			if operation == "reauthenticate" {
				want = ErrReauthenticationFailed
			}
			if !errors.Is(got.err, want) || got.token != "" || got.session != (Session{}) {
				t.Fatal("old authority verification succeeded or returned a session after rotation")
			}
			manager.mu.Lock()
			count := len(manager.sessions)
			manager.mu.Unlock()
			if count != 0 {
				t.Fatal("old authority admitted a post-rotation session")
			}
			if _, _, err := manager.Login("new-login", newPassword); err != nil {
				t.Fatalf("new password login failed: %v", err)
			}
			if err := manager.Reauthenticate("new-reauth", newPassword); err != nil {
				t.Fatalf("new password reauthentication failed: %v", err)
			}
		})
	}
}

func TestPasswordRotationMarkerFailureRetiresOldAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := SetPassword(path, []byte("synthetic-old-password")); err != nil {
		t.Fatal(err)
	}
	// A nonempty marker directory deterministically fails removal after hash commit.
	marker := filepath.Join(filepath.Dir(path), "bootstrap-required")
	if err := os.Mkdir(marker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marker, "fixture"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{HashPath: path, BootstrapMarkerPath: marker})
	_, token, err := manager.Login("local", "synthetic-old-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ReplacePassword([]byte("too-short")); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("invalid password validation changed")
	}
	if manager.credentialGeneration != 0 || len(manager.sessions) != 1 {
		t.Fatal("invalid password retired an unchanged authority")
	}
	if err := manager.ReplacePassword([]byte("synthetic-new-password")); err == nil {
		t.Fatal("marker removal unexpectedly succeeded")
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	if _, valid := manager.SessionFromRequest(request); valid || manager.credentialGeneration != 1 {
		t.Fatal("failed cleanup retained old authority or session")
	}
	if err := manager.Reauthenticate("local", "synthetic-new-password"); err != nil {
		t.Fatal("committed new password was not usable")
	}
}
