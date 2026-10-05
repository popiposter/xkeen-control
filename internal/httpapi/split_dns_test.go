package httpapi

import (
	"github.com/popiposter/xkeen-control/internal/auth"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSplitDNSRequiresSessionAndCSRFAndRejectsPaths(t *testing.T) {
	password := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := setHTTPTestPassword(password, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: password})}))
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
	for _, item := range []struct {
		body map[string]string
		csrf string
		code int
	}{
		{map[string]string{}, "", 403}, {map[string]string{"path": "/tmp/custom"}, session.CSRFToken, 400}, {map[string]string{}, session.CSRFToken, 409},
	} {
		response = postJSON(t, client, server.URL+"/api/v1/dns/split/sync", item.body, item.csrf)
		if response.StatusCode != item.code {
			t.Fatal(response.StatusCode, item.code)
		}
		response.Body.Close()
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
