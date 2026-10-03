package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

type nativeDiscoveryStub struct{ calls atomic.Int32 }

func (d *nativeDiscoveryStub) Inspect(context.Context) xkeen.Capabilities {
	d.calls.Add(1)
	return xkeen.Capabilities{Installation: xkeen.CapabilityAvailable, Version: "2.0.1", Channel: "beta", NeedsOnboarding: true}
}

func TestNativeDiscoveryRequiresAuthenticatedReadAndRejectsArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := setHTTPTestPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	stub := &nativeDiscoveryStub{}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Native: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	response, err := client.Get(server.URL + "/api/v1/xkeen")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || stub.calls.Load() != 0 {
		t.Fatal("unauthenticated discovery ran")
	}
	login := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	if login.StatusCode != http.StatusOK {
		t.Fatal("login failed")
	}
	login.Body.Close()
	response, err = client.Get(server.URL + "/api/v1/xkeen")
	if err != nil {
		t.Fatal(err)
	}
	var facts xkeen.Capabilities
	decodeResponse(t, response, &facts)
	if !facts.NeedsOnboarding || facts.Version != "2.0.1" || stub.calls.Load() != 1 {
		t.Fatalf("facts=%+v", facts)
	}
	response, err = client.Get(server.URL + "/api/v1/xkeen?command=restart")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || stub.calls.Load() != 1 {
		t.Fatal("discovery accepted parameters")
	}
	response = postJSON(t, client, server.URL+"/api/v1/xkeen", map[string]string{"action": "restart"}, "")
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed || stub.calls.Load() != 1 {
		t.Fatal("discovery accepted mutation")
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/xkeen", nil)
	request.Header.Set("Origin", "https://untrusted.invalid")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || stub.calls.Load() != 1 {
		t.Fatal("foreign origin discovery")
	}
}
