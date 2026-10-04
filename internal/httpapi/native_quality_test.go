package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/nativequality"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

type qualityStub struct{ starts, stages int }

func (s *qualityStub) Read() nativequality.Status  { return nativequality.Status{State: "idle"} }
func (s *qualityStub) Start(context.Context) error { s.starts++; return nil }
func (s *qualityStub) Stage(context.Context, string) (string, error) {
	s.stages++
	return strings.Repeat("b", 64), nil
}

func TestNativeQualityAuthenticationClosedBodiesAndNoImplicitApply(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := setHTTPTestPassword(p, []byte("synthetic-quality-password")); err != nil {
		t.Fatal(err)
	}
	stub := &qualityStub{}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: p}), NativeQuality: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	r := postJSON(t, client, server.URL+"/api/v1/performance/quality/start", map[string]any{}, "")
	if r.StatusCode != 401 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()
	r = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-quality-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, r, &login)
	r = postJSON(t, client, server.URL+"/api/v1/performance/quality/start", map[string]any{}, "")
	if r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()
	for _, body := range []string{`{"digest":null}`, `{"nodeId":"arbitrary"}`, `{} {}`, `{"digest":"a","digest":"b"}`} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/quality/start", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		req.Header.Set("X-CSRF-Token", login.CSRFToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 400 {
			t.Fatal(body, resp.StatusCode)
		}
		resp.Body.Close()
	}
	r = postJSON(t, client, server.URL+"/api/v1/performance/quality/start", map[string]any{}, login.CSRFToken)
	if r.StatusCode != 202 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()
	r = postJSON(t, client, server.URL+"/api/v1/performance/quality/stage", map[string]any{"digest": strings.Repeat("a", 64)}, login.CSRFToken)
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()
	if stub.starts != 1 || stub.stages != 1 {
		t.Fatal(stub)
	}
	// Check Apply capability before saving a recommendation.
	r = postJSON(t, client, server.URL+"/api/v1/performance/quality/apply", map[string]any{"digest": strings.Repeat("a", 64)}, login.CSRFToken)
	if r.StatusCode != 503 || stub.stages != 1 {
		t.Fatal("saved with no Apply owner", r.StatusCode, stub.stages)
	}
	r.Body.Close()
}

func TestQualitySavedButRestartAdmissionFailureDoesNotReplay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := setHTTPTestPassword(p, []byte("synthetic-quality-password")); err != nil {
		t.Fatal(err)
	}
	stub := &qualityStub{}
	// Distinct leases deliberately reject existing ApplyConfigs admission.
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: p}), NativeQuality: stub, NativeConfig: &xkeen.ConfigEditor{Lease: authority.NewLease()}, NativeJobs: xkeen.NewJobs("synthetic", authority.NewLease())}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	r := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-quality-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, r, &login)
	r = postJSON(t, client, server.URL+"/api/v1/performance/quality/apply", map[string]any{"digest": strings.Repeat("a", 64)}, login.CSRFToken)
	if r.StatusCode != 409 {
		t.Fatal(r.StatusCode)
	}
	var result struct {
		Saved  bool
		Digest string
	}
	decodeResponse(t, r, &result)
	if !result.Saved || result.Digest != strings.Repeat("b", 64) || stub.stages != 1 || stub.starts != 0 {
		t.Fatal("lost saved state or automatic replay", result, stub)
	}
}
