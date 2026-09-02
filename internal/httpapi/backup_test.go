package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

type httpBackupApplianceSource struct{ value appliance.Appliance }

func (s httpBackupApplianceSource) Snapshot() (appliance.Appliance, error) { return s.value, nil }

type httpBackupRegistrySource struct{ value nodes.Registry }

func (s httpBackupRegistrySource) Snapshot(context.Context) (nodes.Registry, error) {
	return s.value, nil
}

type httpBackupReader struct{ next byte }

func (r *httpBackupReader) Read(value []byte) (int, error) {
	for index := range value {
		value[index] = r.next
		r.next++
	}
	return len(value), nil
}

func httpBackupAppliance(t *testing.T) appliance.Appliance {
	t.Helper()
	value := appliance.Appliance{
		SchemaVersion: appliance.SchemaVersion,
		DNS:           appliance.DNSPolicy{Servers: []appliance.DNSServer{{Address: "localhost"}}, QueryStrategy: "UseIPv4", ServeStale: true, ServeExpiredTTL: 3600, DisableFallbackIfMatch: true, EnableParallelQuery: true, UseSystemHosts: true},
		Routing: appliance.RoutingPolicy{
			DomainStrategy: "IPIfNonMatch", DomainMatcher: "hybrid",
			Rules: []appliance.RoutingRule{
				{Type: "field", InboundTag: []string{"api"}, Action: appliance.RuleAction{OutboundTag: "api"}},
				{Type: "field", InboundTag: []string{"tproxy"}, Action: appliance.RuleAction{BalancerTag: "bal-proxy"}},
			},
			Balancers: []appliance.Balancer{{Tag: "bal-proxy", Selector: []string{"proxy-node-aaaaaaaa"}, FallbackTag: "block", Strategy: appliance.BalancerStrategy{Type: "leastPing"}}},
		},
		Observatory: appliance.ObservatoryPolicy{SubjectSelector: []string{"proxy-node-aaaaaaaa"}, ProbeInterval: "5m"},
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	return value
}

func httpBackupService(t *testing.T, derive backup.KeyDeriver) *backup.Service {
	return httpBackupServiceWithRegistry(t, derive, nodes.NewRegistry())
}

func httpBackupServiceWithRegistry(t *testing.T, derive backup.KeyDeriver, registry nodes.Registry) *backup.Service {
	t.Helper()
	return backup.NewService(backup.Config{
		Appliance: httpBackupApplianceSource{value: httpBackupAppliance(t)},
		Nodes:     httpBackupRegistrySource{value: registry},
		Build:     buildinfo.Info{Product: "xkeen-control", Version: "1.2.3", SourceCommit: strings.Repeat("b", 40), Channel: "stable"},
		Now:       func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
		Random:    &httpBackupReader{}, DeriveKey: derive, GOOS: "linux", GOARCH: "arm64",
	})
}

func fastHTTPBackupDeriver(password, salt []byte, _, _ uint32, _ uint8, keyBytes uint32) []byte {
	input := append(append([]byte(nil), password...), salt...)
	digest := sha256.Sum256(input)
	return append([]byte(nil), digest[:keyBytes]...)
}

func TestBackupHTTPAuthOriginAndDownloadBoundary(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-current-password"
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{
		Auth:   auth.NewManager(auth.Config{HashPath: hashPath}),
		Backup: httpBackupService(t, fastHTTPBackupDeriver),
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/api/v1/backup/export")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated safe export = %d", response.StatusCode)
	}
	response.Body.Close()

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	if login.CSRFToken == "" {
		t.Fatal("login did not return csrf token")
	}

	response, err = client.Get(server.URL + "/api/v1/backup/export")
	if err != nil {
		t.Fatal(err)
	}
	safeBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != backup.BackupMediaType || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Content-Disposition") != `attachment; filename="xkeen-control-backup.json"` {
		t.Fatalf("safe download = %d headers=%v", response.StatusCode, response.Header)
	}
	if _, err := backup.ParseBundle(safeBody); err != nil {
		t.Fatalf("safe response bundle = %v", err)
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

	crossOriginRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backup/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	crossOriginRequest.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(crossOriginRequest)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin safe export = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestSecretExportHTTPReturnsSafeLockoutResponse(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-current-password"
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: hashPath, LockoutAfter: 2, LockoutFor: time.Hour})
	server := httptest.NewServer(New(Config{Auth: manager, Backup: httpBackupService(t, fastHTTPBackupDeriver)}))
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
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: hashPath})
	derive := func(passwordBytes, salt []byte, memoryKiB, iterations uint32, parallelism uint8, keyBytes uint32) []byte {
		manager.InvalidateAll()
		return fastHTTPBackupDeriver(passwordBytes, salt, memoryKiB, iterations, parallelism, keyBytes)
	}
	server := httptest.NewServer(New(Config{Auth: manager, Backup: httpBackupService(t, derive)}))
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
