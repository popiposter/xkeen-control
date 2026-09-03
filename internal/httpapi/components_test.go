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

var _ components.ReadOnlyService = (*componentsHTTPStub)(nil)
