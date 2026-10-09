package httpapi

import (
	"context"
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/splitdns"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitDNSRequiresSessionAndCSRFAndRejectsPaths(t *testing.T) {
	password := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := setHTTPTestPassword(password, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	dnsDir := t.TempDir()
	calls := 0
	service := &splitdns.Service{Dir: dnsDir, Lease: authority.NewLease(), ReadNative: func(context.Context) (map[string][]byte, error) { calls++; return nil, nil }, Restart: func(context.Context) error { calls++; return nil }}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: password}), SplitDNS: service}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	response, err := client.Get(server.URL + "/api/v1/dns/split")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 401 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, response, &session)
	response, err = client.Get(server.URL + "/api/v1/dns/split")
	if err != nil {
		t.Fatal(err)
	}
	var status struct{ State string }
	decodeResponse(t, response, &status)
	if status.State != "unconfigured" {
		t.Fatal(status)
	}
	// Retired POST must not enter the resolver owner even when configured.
	if err := os.WriteFile(filepath.Join(dnsDir, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		body map[string]string
		csrf string
		code int
	}{
		{map[string]string{}, "", 403}, {map[string]string{"path": "/tmp/custom"}, session.CSRFToken, 400}, {map[string]string{}, session.CSRFToken, 410},
	} {
		response = postJSON(t, client, server.URL+"/api/v1/dns/split/sync", item.body, item.csrf)
		if response.StatusCode != item.code {
			t.Fatal(response.StatusCode, item.code)
		}
		response.Body.Close()
	}
	if calls != 0 {
		t.Fatal("retired API entered resolver owner", calls)
	}
	if b, err := os.ReadFile(filepath.Join(dnsDir, "config.json")); err != nil || string(b) != "{}" {
		t.Fatal("retired API changed resolver config")
	}
	response, err = client.Get(server.URL + "/api/v1/dns/split?path=custom")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
}
