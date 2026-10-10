package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
)

// syntheticSecretExport stands in for nativebackup.Service. These tests cover
// the HTTP boundary only: session, CSRF, reauthentication, headers and the
// post-encryption session recheck. Envelope crypto is tested in package backup.
type syntheticSecretExport struct{ during func() }

func (e syntheticSecretExport) ExportSecret(context.Context, string) ([]byte, error) {
	if e.during != nil {
		e.during()
	}
	return []byte(`{"format":"xkeen-control-backup-encrypted","ciphertext":"synthetic"}` + "\n"), nil
}

func TestBackupHTTPAuthOriginAndDownloadBoundary(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-current-password"
	if err := setHTTPTestPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{
		Auth:   auth.NewManager(auth.Config{HashPath: hashPath}),
		Backup: syntheticSecretExport{},
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	unauthenticated := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{"currentPassword": password, "passphrase": "correct synthetic passphrase"}, "")
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated secret export = %d", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	if login.CSRFToken == "" {
		t.Fatal("login did not return csrf token")
	}

	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{"currentPassword": password, "passphrase": "correct synthetic passphrase"}, "")
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("secret export without csrf = %d", withoutCSRF.StatusCode)
	}
	withoutCSRF.Body.Close()

	wrongPassword := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{"currentPassword": "wrong synthetic password", "passphrase": "correct synthetic passphrase"}, login.CSRFToken)
	wrongBody, _ := io.ReadAll(wrongPassword.Body)
	wrongPassword.Body.Close()
	if wrongPassword.StatusCode != http.StatusUnauthorized || !bytes.Contains(wrongBody, []byte("reauthentication failed")) || bytes.Contains(wrongBody, []byte("wrong synthetic password")) {
		t.Fatalf("wrong reauthentication = %d %s", wrongPassword.StatusCode, wrongBody)
	}

	secret := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{"currentPassword": password, "passphrase": "correct synthetic passphrase"}, login.CSRFToken)
	secretBody, _ := io.ReadAll(secret.Body)
	secret.Body.Close()
	if secret.StatusCode != http.StatusOK || secret.Header.Get("Cache-Control") != "no-store" || secret.Header.Get("Content-Type") != backup.EncryptedBackupMediaType || secret.Header.Get("Content-Disposition") != `attachment; filename="xkeen-control-backup-encrypted.json"` || len(secretBody) == 0 {
		t.Fatalf("secret download = %d headers=%v body=%d", secret.StatusCode, secret.Header, len(secretBody))
	}

	unknown := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{"currentPassword": password, "passphrase": "correct synthetic passphrase", "unexpected": "field"}, login.CSRFToken)
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown secret request field = %d", unknown.StatusCode)
	}
	unknown.Body.Close()

	crossOriginRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/backup/export-secret", strings.NewReader(`{"currentPassword":"synthetic-current-password","passphrase":"correct synthetic passphrase"}`))
	if err != nil {
		t.Fatal(err)
	}
	crossOriginRequest.Header.Set("Content-Type", "application/json")
	crossOriginRequest.Header.Set("X-CSRF-Token", login.CSRFToken)
	crossOriginRequest.Header.Set("Origin", "http://evil.example")
	response, err := client.Do(crossOriginRequest)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin secret export = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestSecretExportHTTPReturnsSafeLockoutResponse(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-current-password"
	if err := setHTTPTestPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: hashPath, LockoutAfter: 2, LockoutFor: time.Hour})
	server := httptest.NewServer(New(Config{Auth: manager, Backup: syntheticSecretExport{}}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	for attempt := 0; attempt < 2; attempt++ {
		response := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{
			"currentPassword": "wrong synthetic password", "passphrase": "correct synthetic passphrase",
		}, login.CSRFToken)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized || bytes.Contains(body, []byte(password)) {
			t.Fatalf("failed reauthentication attempt = %d %s", response.StatusCode, body)
		}
	}

	locked := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{
		"currentPassword": password, "passphrase": "correct synthetic passphrase",
	}, login.CSRFToken)
	body, _ := io.ReadAll(locked.Body)
	locked.Body.Close()
	if locked.StatusCode != http.StatusTooManyRequests || !bytes.Contains(body, []byte("temporarily unavailable")) || bytes.Contains(body, []byte(password)) || bytes.Contains(body, []byte("correct synthetic passphrase")) {
		t.Fatalf("lockout response = %d %s", locked.StatusCode, body)
	}
}

func TestSecretExportDoesNotWriteAfterConcurrentSessionInvalidation(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-current-password"
	if err := setHTTPTestPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: hashPath})
	server := httptest.NewServer(New(Config{Auth: manager, Backup: syntheticSecretExport{during: manager.InvalidateAll}}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	response := postJSON(t, client, server.URL+"/api/v1/backup/export-secret", map[string]string{"currentPassword": password, "passphrase": "correct synthetic passphrase"}, login.CSRFToken)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || json.Valid(body) && bytes.Contains(body, []byte("ciphertext")) {
		t.Fatalf("invalidated session export = %d %s", response.StatusCode, body)
	}
}
