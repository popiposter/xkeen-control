package httpapi

import (
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRoutingExamplePrivateCSRFAndStrictBody(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := setHTTPTestPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), NativeConfig: &xkeen.ConfigEditor{}}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	body := map[string]any{"text": `{"routing":{"rules":[{"type":"field","domain":["full:example.test"],"outboundTag":"direct"}]}}`, "sample": map[string]any{"domain": "example.test"}}
	r := postJSON(t, client, server.URL+"/api/v1/xkeen/config/example", body, "")
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal(r.StatusCode)
	}
	r = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct{ CSRFToken string }
	decodeResponse(t, r, &login)
	r = postJSON(t, client, server.URL+"/api/v1/xkeen/config/example", body, "")
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	r = postJSON(t, client, server.URL+"/api/v1/xkeen/config/example", body, login.CSRFToken)
	var result xkeen.RoutingExample
	decodeResponse(t, r, &result)
	if result.State != "matched" || result.Target != "direct" {
		t.Fatal(result)
	}
	body["command"] = "forbidden"
	r = postJSON(t, client, server.URL+"/api/v1/xkeen/config/example", body, login.CSRFToken)
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatal(r.StatusCode)
	}
}
