package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nativequality"
)

type qualityInspectorStub struct {
	calls int
	allow bool
}

func (*qualityInspectorStub) Read() nativequality.Status {
	return nativequality.Status{State: "idle", InspectionRequired: true}
}
func (*qualityInspectorStub) Start(context.Context) error { return nil }
func (*qualityInspectorStub) Stage(context.Context, string) (string, error) {
	return "", nativequality.ErrUnavailable
}
func (s *qualityInspectorStub) InspectAndResolve(context.Context) error {
	s.calls++
	if !s.allow {
		return nativequality.ErrUnavailable
	}
	return nil
}

func TestQualityInspectionRequiresSessionCSRFAndExplicitEmptyRequest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := setHTTPTestPassword(p, []byte("synthetic-quality-password")); err != nil {
		t.Fatal(err)
	}
	stub := &qualityInspectorStub{}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: p}), NativeQuality: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	endpoint := server.URL + "/api/v1/performance/quality/inspect"
	r := postJSON(t, client, endpoint, map[string]any{}, "")
	if r.StatusCode != 401 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()
	r = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-quality-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, r, &login)
	r = postJSON(t, client, endpoint, map[string]any{}, "")
	if r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()
	r = postJSON(t, client, endpoint, map[string]any{"acknowledge": true}, login.CSRFToken)
	if r.StatusCode != 400 || stub.calls != 0 {
		t.Fatal("untyped acknowledgement accepted", r.StatusCode, stub.calls)
	}
	r.Body.Close()
	r = postJSON(t, client, endpoint, map[string]any{}, login.CSRFToken)
	if r.StatusCode != 409 || stub.calls != 1 {
		t.Fatal("uninspected state accepted", r.StatusCode, stub.calls)
	}
	r.Body.Close()
	stub.allow = true
	r = postJSON(t, client, endpoint, map[string]any{}, login.CSRFToken)
	if r.StatusCode != 200 || stub.calls != 2 {
		t.Fatal("explicit inspected request rejected", r.StatusCode, stub.calls)
	}
	r.Body.Close()
}
