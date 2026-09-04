package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/components"
)

type componentsHTTPStub struct {
	calls atomic.Int32
	value components.Inventory
}

func (stub *componentsHTTPStub) Snapshot(context.Context) components.Inventory {
	stub.calls.Add(1)
	return stub.value
}

type componentCheckHTTPStub struct {
	calls  atomic.Int32
	last   components.CheckRequest
	result components.CheckResult
	err    error
}

func (stub *componentCheckHTTPStub) Check(_ context.Context, request components.CheckRequest) (components.CheckResult, error) {
	stub.calls.Add(1)
	stub.last = request
	return stub.result, stub.err
}

func TestComponentsRouteUsesReadOnlyAuthOriginAndSafeProjection(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	stub := &componentsHTTPStub{value: components.Inventory{
		SchemaVersion: components.SchemaVersion,
		Panel: components.Component{
			Kind:       components.KindPanel,
			State:      components.StatePresent,
			Present:    true,
			Version:    "1.2.3",
			Capability: components.CapabilityInformational,
			ReasonCode: "panel-update-owned",
		},
		Xray: components.Component{
			Kind:           components.KindXray,
			State:          components.StateUnknown,
			VersionUnknown: true,
			Capability:     components.CapabilityUnsupported,
			ReasonCode:     "version-unavailable",
		},
		Geodata: components.GeodataComponent{
			Component: components.Component{
				Kind:       components.KindGeodata,
				State:      components.StateMissing,
				Capability: components.CapabilityUnsupported,
				ReasonCode: "required-files-missing",
			},
			Items: []components.GeodataItem{},
		},
	}}
	server := httptest.NewServer(New(Config{
		Auth:       auth.NewManager(auth.Config{HashPath: passwordPath}),
		Components: stub,
	}))
	defer server.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	response, err := client.Get(server.URL + "/api/v1/components")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated components = %d", response.StatusCode)
	}
	response.Body.Close()
	if stub.calls.Load() != 0 {
		t.Fatalf("unauthenticated request called inventory: %d", stub.calls.Load())
	}

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readBody(loginResponse))
	}
	loginResponse.Body.Close()

	response, err = client.Get(server.URL + "/api/v1/components")
	if err != nil {
		t.Fatal(err)
	}
	contents := readBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated components = %d %s", response.StatusCode, contents)
	}
	var inventory components.Inventory
	if err := json.Unmarshal([]byte(contents), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SchemaVersion != components.SchemaVersion || inventory.Panel.Version != "1.2.3" || inventory.Xray.ReasonCode != "version-unavailable" || stub.calls.Load() != 1 {
		t.Fatalf("inventory = %+v calls=%d", inventory, stub.calls.Load())
	}
	for _, forbidden := range []string{"/opt/", "command", "argv", "stderr", "password.bcrypt"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("component response contains %q: %s", forbidden, contents)
		}
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/components", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("POST components = %d allow=%q body=%s", response.StatusCode, response.Header.Get("Allow"), readBody(response))
	}
	if stub.calls.Load() != 1 {
		t.Fatalf("method rejection called inventory: %d", stub.calls.Load())
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/api/v1/components", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin components = %d", response.StatusCode)
	}
	response.Body.Close()
	if stub.calls.Load() != 1 {
		t.Fatalf("cross-origin request called inventory: %d", stub.calls.Load())
	}
}

func TestComponentsRouteFailsClosedWhenServiceUnavailable(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath})}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readBody(loginResponse))
	}
	loginResponse.Body.Close()
	response, err := client.Get(server.URL + "/api/v1/components")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unavailable components = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
}

func TestComponentsCheckRouteIsClosedCSRFBoundAndSafe(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	checker := &componentCheckHTTPStub{result: components.CheckResult{
		SchemaVersion:     components.CheckSchemaVersion,
		Component:         components.KindXray,
		Channel:           "stable",
		SourceID:          "github/XTLS/Xray-core",
		CheckedAt:         time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Candidate:         &components.CheckCandidate{Version: "26.3.27", AssetName: "Xray-linux-arm64-v8a.zip", SizeBytes: 1234, SHA256: strings.Repeat("a", 64)},
		InstalledState:    "update-available",
		Eligible:          true,
		MutationAvailable: false,
		ReasonCode:        "supported-for-preview",
	}}
	server := httptest.NewServer(New(Config{
		Auth:            auth.NewManager(auth.Config{HashPath: passwordPath}),
		ComponentChecks: checker,
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	requestBody := map[string]string{"component": "xray", "channel": "stable"}
	response := postJSON(t, client, server.URL+"/api/v1/components/check", requestBody, "")
	if response.StatusCode != http.StatusUnauthorized || checker.calls.Load() != 0 {
		t.Fatalf("unauthenticated check = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	if login.CSRFToken == "" {
		t.Fatal("login did not return csrf token")
	}

	response = postJSON(t, client, server.URL+"/api/v1/components/check", requestBody, "")
	if response.StatusCode != http.StatusForbidden || checker.calls.Load() != 0 {
		t.Fatalf("check without csrf = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/components/check", map[string]string{"component": "panel", "channel": "stable"}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest || checker.calls.Load() != 0 {
		t.Fatalf("invalid component check = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/components/check", map[string]string{"component": "xray", "channel": "stable", "force": "true"}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest || checker.calls.Load() != 0 {
		t.Fatalf("unknown check field = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/components/check", strings.NewReader(`{"component":"xray","channel":"stable"}{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(auth.CSRFHeader, login.CSRFToken)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest || checker.calls.Load() != 0 {
		t.Fatalf("trailing check JSON = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	request, err = http.NewRequest(http.MethodPost, server.URL+"/api/v1/components/check", strings.NewReader(`{"component":"xray","channel":"stable"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set(auth.CSRFHeader, login.CSRFToken)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnsupportedMediaType || checker.calls.Load() != 0 {
		t.Fatalf("wrong check content type = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	request, err = http.NewRequest(http.MethodGet, server.URL+"/api/v1/components/check", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodPost {
		t.Fatalf("GET check = %d allow=%q", response.StatusCode, response.Header.Get("Allow"))
	}
	response.Body.Close()

	request, err = http.NewRequest(http.MethodPost, server.URL+"/api/v1/components/check", strings.NewReader(`{"component":"xray","channel":"stable"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(auth.CSRFHeader, login.CSRFToken)
	request.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden || checker.calls.Load() != 0 {
		t.Fatalf("cross-origin check = %d calls=%d", response.StatusCode, checker.calls.Load())
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/components/check", requestBody, login.CSRFToken)
	contents := readBody(response)
	if response.StatusCode != http.StatusOK || checker.calls.Load() != 1 {
		t.Fatalf("valid component check = %d calls=%d body=%s", response.StatusCode, checker.calls.Load(), contents)
	}
	var result components.CheckResult
	if err := json.Unmarshal([]byte(contents), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Eligible || result.MutationAvailable || result.Candidate == nil || result.Candidate.AssetName != "Xray-linux-arm64-v8a.zip" {
		t.Fatalf("check result = %+v", result)
	}
	for _, forbidden := range []string{"browser_download_url", "release body", "command", "stderr", "/opt/", "password.bcrypt"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("check response contains %q: %s", forbidden, contents)
		}
	}

	response = postJSON(t, client, server.URL+"/api/v1/components/check", map[string]string{"component": "xkeen", "channel": "dev"}, login.CSRFToken)
	contents = readBody(response)
	if response.StatusCode != http.StatusOK || checker.calls.Load() != 2 || checker.last != (components.CheckRequest{Component: components.KindXKeen, Channel: "dev"}) {
		t.Fatalf("valid xkeen dev check = %d calls=%d request=%+v body=%s", response.StatusCode, checker.calls.Load(), checker.last, contents)
	}
}

var _ components.ReadOnlyService = (*componentsHTTPStub)(nil)
var _ components.CheckService = (*componentCheckHTTPStub)(nil)
