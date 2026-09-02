package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/restore"
)

const (
	httpRestoreUUIDMarker              = "11111111-1111-4111-8111-111111111111"
	httpRestoreRealityPublicKeyMarker  = "AAAAAAAAAAAAAAAA"
	httpRestoreRealityShortIDMarker    = "0123456789abcdef"
	httpRestoreSubscriptionTokenMarker = "synthetic-token"
	httpRestoreSubscriptionURLMarker   = "https://subscription.example/" + httpRestoreSubscriptionTokenMarker
)

func newHTTPRealRestoreServer(t *testing.T, root string) (*httptest.Server, *http.Client, string) {
	t.Helper()
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	applianceBytes, err := appliance.MarshalCanonical(httpBackupAppliance(t))
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := nodes.MarshalCanonical(nodes.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	writeHTTPRestorePrivateFile(t, appliancePath, applianceBytes)
	writeHTTPRestorePrivateFile(t, nodesPath, registryBytes)

	service := restore.NewService(restore.Config{
		AppliancePath:  appliancePath,
		NodesPath:      nodesPath,
		AuthorityLease: authority.NewLease(),
		Now:            func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
	})
	if err := service.Ready(); err != nil {
		t.Fatal(err)
	}

	const password = "synthetic-control-password"
	hashPath := filepath.Join(root, "auth", "password.bcrypt")
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: hashPath})
	server := httptest.NewServer(New(Config{Auth: manager, Restore: service}))
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readBody(loginResponse))
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	return server, client, login.CSRFToken
}

func writeHTTPRestorePrivateFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func realHTTPEncryptedBundle(t *testing.T) []byte {
	t.Helper()
	contents, err := httpBackupServiceWithRegistry(t, nil, httpRestoreSecretRegistry(t)).ExportSecret(context.Background(), "correct synthetic passphrase")
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func httpRestoreSecretRegistry(t *testing.T) nodes.Registry {
	t.Helper()
	profile := nodes.VLESS{
		UUID: httpRestoreUUIDMarker, Host: "node.example.com", Port: 443,
		Encryption: "none", Security: "reality", ServerName: "node.example.com", Fingerprint: "chrome",
		PublicKey: httpRestoreRealityPublicKeyMarker, ShortID: httpRestoreRealityShortIDMarker, Network: "tcp",
	}
	const subscriptionID = "sub-httpmarker"
	node, err := nodes.NewNodeWithID(profile, "Synthetic encrypted HTTP node", nodes.Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-httpmarker")
	if err != nil {
		t.Fatal(err)
	}
	registry := nodes.NewRegistry()
	registry.Subscriptions = []nodes.Subscription{{
		ID: subscriptionID, Name: "Synthetic encrypted HTTP provider", URL: httpRestoreSubscriptionURLMarker, Enabled: true,
	}}
	registry.Nodes = []nodes.Node{node}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func httpRestoreSecretMarkers() []string {
	return []string{
		httpRestoreUUIDMarker,
		httpRestoreRealityPublicKeyMarker,
		httpRestoreRealityShortIDMarker,
		httpRestoreSubscriptionURLMarker,
		httpRestoreSubscriptionTokenMarker,
	}
}

func tamperHTTPEncryptedBundle(t *testing.T, source []byte) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(source, &envelope); err != nil {
		t.Fatal(err)
	}
	ciphertext, ok := envelope["ciphertext"].(string)
	if !ok {
		t.Fatal("encrypted fixture has no ciphertext")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil || len(decoded) == 0 {
		t.Fatalf("encrypted fixture ciphertext = %v", err)
	}
	decoded[0] ^= 1
	envelope["ciphertext"] = base64.RawURLEncoding.EncodeToString(decoded)
	mutated, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return append(mutated, '\n')
}

func unsupportedKDFHTTPEncryptedBundle(t *testing.T, source []byte) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(source, &envelope); err != nil {
		t.Fatal(err)
	}
	kdf, ok := envelope["kdf"].(map[string]any)
	if !ok {
		t.Fatal("encrypted fixture has no KDF")
	}
	kdf["memoryKiB"] = float64(backup.Argon2MemoryKiB + 1)
	mutated, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return append(mutated, '\n')
}

func TestRestoreHTTPUsesRealBundleParserAndLeavesNoUploadFiles(t *testing.T) {
	root := t.TempDir()
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	t.Setenv("TMP", tempRoot)
	t.Setenv("TEMP", tempRoot)
	server, client, csrf := newHTTPRealRestoreServer(t, root)
	defer server.Close()

	safe := restoreFixtureBundle(t)
	encrypted := realHTTPEncryptedBundle(t)
	request := newRestoreMultipartRequest(t, restorePreviewURL(server), safe, "", "../../hostile-upload-name.json", csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("real safe preview = %d %s", response.StatusCode, body)
	}
	var safePreview restore.Preview
	if err := json.Unmarshal([]byte(body), &safePreview); err != nil {
		t.Fatal(err)
	}
	if safePreview.Token == "" || safePreview.ContainsSecrets || safePreview.Mode != restore.SettingsOnly {
		t.Fatalf("real safe preview = %+v", safePreview)
	}
	assertHTTPRestoreResponseHasNoSecretMarkers(t, body, httpRestoreSecretMarkers()...)

	request = newRestoreMultipartRequest(t, restorePreviewURL(server), encrypted, "correct synthetic passphrase", "../../hostile-upload-name.json", csrf)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body = readBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("real encrypted preview = %d %s", response.StatusCode, body)
	}
	var encryptedPreview restore.Preview
	if err := json.Unmarshal([]byte(body), &encryptedPreview); err != nil {
		t.Fatal(err)
	}
	if encryptedPreview.Token == "" || !encryptedPreview.ContainsSecrets || encryptedPreview.Mode != restore.SettingsOnly {
		t.Fatalf("real encrypted preview = %+v", encryptedPreview)
	}
	assertHTTPRestoreResponseHasNoSecretMarkers(t, body, httpRestoreSecretMarkers()...)

	for _, test := range []struct {
		name       string
		bundle     []byte
		passphrase string
	}{
		{name: "wrong passphrase", bundle: encrypted, passphrase: "wrong synthetic passphrase"},
		{name: "tampered ciphertext", bundle: tamperHTTPEncryptedBundle(t, encrypted), passphrase: "correct synthetic passphrase"},
		{name: "unsupported KDF", bundle: unsupportedKDFHTTPEncryptedBundle(t, encrypted), passphrase: "correct synthetic passphrase"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := newRestoreMultipartRequest(t, restorePreviewURL(server), test.bundle, test.passphrase, "../../hostile-upload-name.json", csrf)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body := readBody(response)
			if response.StatusCode != http.StatusBadRequest || strings.TrimSpace(body) != `{"error":"restore request rejected"}` {
				t.Fatalf("real %s rejection = %d %s", test.name, response.StatusCode, body)
			}
			assertHTTPRestoreResponseHasNoSecretMarkers(t, body, httpRestoreSecretMarkers()...)
		})
	}

	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("hostile multipart filename left temp/upload paths: %v", names)
	}
}

func assertHTTPRestoreResponseHasNoSecretMarkers(t *testing.T, body string, markers ...string) {
	t.Helper()
	for _, marker := range markers {
		if strings.Contains(body, marker) {
			t.Fatalf("restore HTTP response exposed secret marker %q: %s", marker, body)
		}
	}
}
