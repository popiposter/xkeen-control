package httpapi

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetiredComponentSetupAndApplianceRestoreRoutesAreAbsent(t *testing.T) {
	server := httptest.NewServer(New(Config{}))
	defer server.Close()
	paths := []string{"components", "components/check", "components/policy", "components/preview", "components/apply", "components/rollback", "components/cancel", "setup/preview", "setup/apply", "setup/cancel", "backup/import/preview", "backup/import/apply", "backup/import/cancel"}
	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			request, _ := http.NewRequest(method, server.URL+"/api/v1/"+path, strings.NewReader(`{}`))
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("obsolete route %s %s reachable: %d", method, path, response.StatusCode)
			}
			media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if mediaErr != nil || media != "application/json" {
				t.Fatal("obsolete API fell through to HTML/assets")
			}
		}
	}
}
