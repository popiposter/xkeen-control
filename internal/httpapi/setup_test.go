package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/components"
)

type setupHTTPStub struct {
	previews     int
	applications int
	cancels      int
	invalidated  string
}

func (s *setupHTTPStub) Preview(context.Context, string) (components.SetupPreview, error) {
	s.previews++
	return components.SetupPreview{SchemaVersion: 1, Operation: components.SetupOperation, PreviewToken: "setup-token", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func (s *setupHTTPStub) Apply(context.Context, string, string) (components.SetupResult, error) {
	s.applications++
	return components.SetupResult{SchemaVersion: 1, Operation: components.SetupOperation, State: "ready", CompletedAt: time.Now().UTC()}, nil
}

func (s *setupHTTPStub) Cancel(_, _ string)        { s.cancels++ }
func (s *setupHTTPStub) Invalidate(binding string) { s.invalidated = binding }
func (s *setupHTTPStub) InvalidateAll()            { s.invalidated = "all" }
func (s *setupHTTPStub) Status() components.SetupProjection {
	return components.SetupProjection{State: "fresh", Eligible: true, ReasonCode: components.SetupReasonFresh}
}

func TestSetupRoutesAreClosedAuthenticatedAndSessionBound(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	setup := &setupHTTPStub{}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), Setup: setup}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	response := postJSON(t, client, server.URL+"/api/v1/setup/preview", map[string]any{}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("setup preview without csrf = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/setup/preview", map[string]any{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || setup.previews != 1 {
		t.Fatalf("setup preview = %d calls=%d body=%s", response.StatusCode, setup.previews, readBody(response))
	}

	response = postJSON(t, client, server.URL+"/api/v1/setup/preview?unexpected=1", map[string]any{}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("setup preview query = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/setup/preview", map[string]any{"component": "xray"}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("setup preview unknown field = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/setup/preview", "{"+string(make([]byte, maxSetupBody))+"}", login.CSRFToken)
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("setup preview oversized = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/setup/apply", map[string]any{"previewToken": "setup-token", "component": "xray"}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest || setup.applications != 0 {
		t.Fatalf("setup apply extra field = %d calls=%d body=%s", response.StatusCode, setup.applications, readBody(response))
	}

	response = postJSON(t, client, server.URL+"/api/v1/setup/apply", map[string]string{"previewToken": "setup-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || setup.applications != 1 {
		t.Fatalf("setup apply = %d calls=%d body=%s", response.StatusCode, setup.applications, readBody(response))
	}

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]any{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || setup.invalidated == "" {
		t.Fatalf("setup logout invalidation = %d binding=%q body=%s", response.StatusCode, setup.invalidated, readBody(response))
	}
}
