package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"testing"
	"time"
)

func TestPasswordHashAndSessionLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	password := []byte("synthetic-panel-password")
	if err := SetPassword(path, password); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if stdruntime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("password hash mode = %o, want 600", info.Mode().Perm())
	}

	manager := NewManager(Config{HashPath: path, SessionTTL: time.Hour})
	session, token, err := manager.Login("127.0.0.1", string(password))
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || session.CSRFToken == "" || token == session.CSRFToken {
		t.Fatal("session identifiers were not independently generated")
	}

	request := httptest.NewRequest("GET", "http://127.0.0.1:8787/api/v1/session", nil)
	response := httptest.NewRecorder()
	manager.SetSessionCookie(response, token, session.ExpiresAt)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Secure {
		t.Fatalf("session cookie flags = %+v", cookies)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	got, ok := manager.SessionFromRequest(request)
	if !ok || got.CSRFToken != session.CSRFToken {
		t.Fatal("session was not recovered from HttpOnly cookie")
	}
	if ValidateCSRF(httptest.NewRequest("POST", request.URL.String(), nil), got) {
		// The no-header case must fail; this branch documents the negative check.
		t.Fatal("empty CSRF header unexpectedly accepted")
	}
}

func TestCSRFMustMatchAndRateLimitIsInRAM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := SetPassword(path, []byte("another-synthetic-password")); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{HashPath: path, LockoutAfter: 2, LockoutFor: time.Hour})
	if _, _, err := manager.Login("127.0.0.1", "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("first failure = %v", err)
	}
	if _, _, err := manager.Login("127.0.0.1", "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("second failure = %v", err)
	}
	if _, _, err := manager.Login("127.0.0.1", "another-synthetic-password"); err != ErrLocked {
		t.Fatalf("locked login = %v, want ErrLocked", err)
	}

	session := Session{CSRFToken: "csrf-token"}
	request := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/v1/session/logout", nil)
	request.Header.Set(CSRFHeader, "wrong")
	if ValidateCSRF(request, session) {
		t.Fatal("wrong CSRF token accepted")
	}
	request.Header.Set(CSRFHeader, session.CSRFToken)
	if !ValidateCSRF(request, session) {
		t.Fatal("correct CSRF token rejected")
	}
}

func TestOriginPolicyRejectsCrossOrigin(t *testing.T) {
	manager := NewManager(Config{})
	request := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/v1/session/logout", nil)
	request.Host = "127.0.0.1:8787"
	request.Header.Set("Origin", "http://evil.example")
	if manager.OriginAllowed(request) {
		t.Fatal("cross-origin request accepted")
	}
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	if !manager.OriginAllowed(request) {
		t.Fatal("same-origin request rejected")
	}
}
