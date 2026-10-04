package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/geodatareader"
)

func TestGeodataPrivateReadRoutesRequireSessionAndQueryCSRF(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := setHTTPTestPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), []byte{10, 6, 10, 4, 'T', 'E', 'S', 'T'}, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), Geodata: &geodatareader.Reader{Dir: dir}}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	response, err := client.Get(server.URL + "/api/v1/geodata")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.StatusCode)
	}
	response = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, response, &login)
	query := map[string]any{"file": "geosite.dat", "view": "categories"}
	response = postJSON(t, client, server.URL+"/api/v1/geodata/query", query, "")
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal(response.StatusCode)
	}
	response = postJSON(t, client, server.URL+"/api/v1/geodata/query", query, login.CSRFToken)
	var result geodatareader.Result
	decodeResponse(t, response, &result)
	if result.Total != 1 || result.Items[0].Category != "test" {
		t.Fatal(result)
	}
	query["path"] = "/etc/passwd"
	response = postJSON(t, client, server.URL+"/api/v1/geodata/query", query, login.CSRFToken)
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode)
	}
}
