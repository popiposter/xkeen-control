package auth

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRememberedSessionRestartLogoutAndRotation(t *testing.T) {
	dir := t.TempDir()
	config := Config{HashPath: filepath.Join(dir, "password.bcrypt"), SessionPath: filepath.Join(dir, "sessions.json"), SessionTTL: 30 * 24 * time.Hour}
	if err := SetPassword(config.HashPath, []byte("synthetic-password-only")); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config)
	_, token, err := manager.Login("local", "synthetic-password-only")
	if err != nil {
		t.Fatal(err)
	}
	request := &http.Request{Header: make(http.Header)}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	stored, err := os.ReadFile(config.SessionPath)
	if err != nil || strings.Contains(string(stored), token) || strings.Contains(string(stored), "synthetic-password-only") {
		t.Fatal("session token or password stored in plain text", err)
	}
	restored := NewManager(config)
	if _, ok := restored.SessionFromRequest(request); !ok {
		t.Fatal("restart lost remembered session")
	}
	other := config
	other.SessionAudience = "other-listener"
	if _, ok := NewManager(other).SessionFromRequest(request); ok {
		t.Fatal("changed listener retained remembered session")
	}
	other = config
	other.SecureCookies = true
	if _, ok := NewManager(other).SessionFromRequest(request); ok {
		t.Fatal("changed transport retained remembered session")
	}
	if err := restored.Logout(request); err != nil {
		t.Fatal(err)
	}
	if _, ok := NewManager(config).SessionFromRequest(request); ok {
		t.Fatal("logged out session restored")
	}
	_, token, err = manager.Login("local", "synthetic-password-only")
	if err != nil {
		t.Fatal(err)
	}
	request.Header = make(http.Header)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	if err := SetPassword(config.HashPath, []byte("synthetic-changed-password")); err != nil {
		t.Fatal(err)
	}
	if _, ok := NewManager(config).SessionFromRequest(request); ok {
		t.Fatal("password authority change restored old session")
	}
}

func TestRememberedSessionExpiryAndUnsafePermissions(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	config := Config{HashPath: filepath.Join(dir, "password.bcrypt"), SessionPath: filepath.Join(dir, "sessions.json"), SessionTTL: time.Hour, Now: func() time.Time { return now }}
	if err := SetPassword(config.HashPath, []byte("synthetic-password-only")); err != nil {
		t.Fatal(err)
	}
	_, token, err := NewManager(config).Login("local", "synthetic-password-only")
	if err != nil {
		t.Fatal(err)
	}
	request := &http.Request{Header: make(http.Header)}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	if err := os.Chmod(config.SessionPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := NewManager(config).SessionFromRequest(request); ok {
		t.Fatal("unsafe session permissions admitted")
	}
	os.Chmod(config.SessionPath, 0600)
	now = now.Add(2 * time.Hour)
	if _, ok := NewManager(config).SessionFromRequest(request); ok {
		t.Fatal("expired session restored")
	}
}
