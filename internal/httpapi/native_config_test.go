package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestPrivateNativeDocumentResponseDoesNotUseStatusLimit(t *testing.T) {
	raw := strings.Repeat("\x00", 300<<10)
	w := httptest.NewRecorder()
	writePrivateConfigJSON(w, http.StatusOK, map[string]any{"document": xkeen.EditorDocument{Text: raw, Draft: &raw}})
	if w.Code != http.StatusOK || w.Body.Len() <= maxJSONResponse {
		t.Fatal("private bound incompatible with draft size", w.Code, w.Body.Len())
	}
	status := httptest.NewRecorder()
	writeJSON(status, http.StatusOK, map[string]string{"text": raw})
	if status.Code != http.StatusInternalServerError {
		t.Fatal("enlarged unrelated status response bound")
	}
}

func TestNativeConfigRequiresSessionCSRFAndTypedFields(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := setHTTPTestPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), NativeConfig: &xkeen.ConfigEditor{}}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	response := postJSON(t, client, server.URL+"/api/v1/xkeen/config/save", map[string]any{}, "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, response, &login)
	request := map[string]any{"digest": "a", "area": "dns", "field": "disableCache", "value": true}
	response = postJSON(t, client, server.URL+"/api/v1/xkeen/config/save", request, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	request["field"] = "../../raw-file"
	response = postJSON(t, client, server.URL+"/api/v1/xkeen/config/save", request, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
}
