package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/performancepolicy"
)

type httpPerformancePolicyStub struct {
	projection        performancepolicy.Projection
	preview           performancepolicy.Preview
	apply             performancepolicy.ApplyResult
	previewPolicy     c1.PerformancePolicy
	previewBinding    string
	applyBinding      string
	applyToken        string
	cancelBinding     string
	cancelToken       string
	invalidateBinding string
	invalidateAll     int
}

func (stub *httpPerformancePolicyStub) Read(context.Context) (performancepolicy.Projection, error) {
	return stub.projection, nil
}

func (stub *httpPerformancePolicyStub) Preview(_ context.Context, binding string, policy c1.PerformancePolicy) (performancepolicy.Preview, error) {
	stub.previewBinding = binding
	stub.previewPolicy = policy
	return stub.preview, nil
}

func (stub *httpPerformancePolicyStub) Apply(_ context.Context, binding, token string) (performancepolicy.ApplyResult, error) {
	stub.applyBinding = binding
	stub.applyToken = token
	return stub.apply, nil
}

func (stub *httpPerformancePolicyStub) Cancel(binding, token string) {
	stub.cancelBinding = binding
	stub.cancelToken = token
}

func (stub *httpPerformancePolicyStub) Invalidate(binding string) { stub.invalidateBinding = binding }
func (stub *httpPerformancePolicyStub) InvalidateAll()            { stub.invalidateAll++ }

func TestPerformancePolicyHTTPIsClosedAuthenticatedAndSessionBound(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-control-password"
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	policy := c1.DefaultPerformancePolicy()
	stub := &httpPerformancePolicyStub{
		projection: performancepolicy.Projection{
			Policy: policy, Source: performancepolicy.SourceDefault,
			HardCeilings: performancepolicy.HardCeilings{MaxCandidates: 6, CandidateDownloadMiB: 16, CandidateUploadMiB: 8, CandidateMaxSeconds: 30, GenerationMaxMiB: 144, GenerationMaxSeconds: 180, TransportIdentity: "source-owned", RTTGuard: "source-owned", Scoring: "source-owned"},
			Adaptive:     c1.AdaptivePerformanceStatus{State: "waiting"},
		},
		preview: performancepolicy.Preview{Token: "synthetic-performance-token", ExpiresAt: time.Now().UTC().Add(time.Minute), Before: policy, After: policy, Noop: true},
		apply:   performancepolicy.ApplyResult{Policy: policy, Source: performancepolicy.SourceDefault, Noop: true},
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: hashPath}), PerformancePolicy: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/api/v1/performance/policy")
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	response, err = client.Get(server.URL + "/api/v1/performance/policy")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d, %v", response.StatusCode, err)
	}
	body := readBody(response)
	if strings.Contains(strings.ToLower(body), "https://") || strings.Contains(strings.ToLower(body), "schedule") || strings.Contains(strings.ToLower(body), "timeout") || !strings.Contains(body, `"transportIdentity":"source-owned"`) {
		t.Fatalf("unsafe or incomplete projection: %s", body)
	}
	response, err = client.Get(server.URL + "/api/v1/performance/policy?raw=true")
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET query = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/performance/policy/preview", policy, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("preview without CSRF = %d", response.StatusCode)
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/performance/policy/preview", policy, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.previewBinding != login.CSRFToken || stub.previewPolicy != policy {
		t.Fatalf("preview = %d binding=%q policy=%+v body=%s", response.StatusCode, stub.previewBinding, stub.previewPolicy, readBody(response))
	}
	response.Body.Close()

	invalidBodies := []string{
		`{"schemaVersion":1,"probeIntervalSeconds":60,"failureThreshold":2,"adaptiveCadenceMinutes":180,"adaptiveChallengerLimit":5,"minimumDwellMinutes":30,"qualityHysteresisPercent":10,"url":"https://example.invalid"}`,
		`{"schemaVersion":1,"probeIntervalSeconds":60,"failureThreshold":2,"failureThreshold":3,"adaptiveCadenceMinutes":180,"adaptiveChallengerLimit":5,"minimumDwellMinutes":30,"qualityHysteresisPercent":10}`,
		`{"schemaVersion":1,"probeIntervalSeconds":60,"failureThreshold":2,"adaptiveCadenceMinutes":180,"adaptiveChallengerLimit":5,"minimumDwellMinutes":30,"qualityHysteresisPercent":10} {}`,
	}
	for _, invalid := range invalidBodies {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/policy/preview", strings.NewReader(invalid))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(auth.CSRFHeader, login.CSRFToken)
		response, err = client.Do(request)
		if err != nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid body accepted = %d err=%v body=%s", response.StatusCode, err, readBody(response))
		}
	}

	response = postJSON(t, client, server.URL+"/api/v1/performance/policy/apply", map[string]any{"previewToken": "synthetic-performance-token", "policy": policy}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("apply accepted non-token payload = %d body=%s", response.StatusCode, readBody(response))
	}
	response = postJSON(t, client, server.URL+"/api/v1/performance/policy/apply", map[string]string{"previewToken": "synthetic-performance-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.applyBinding != login.CSRFToken || stub.applyToken != "synthetic-performance-token" {
		t.Fatalf("apply = %d binding=%q token=%q body=%s", response.StatusCode, stub.applyBinding, stub.applyToken, readBody(response))
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/performance/policy/cancel", map[string]string{"previewToken": "synthetic-performance-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.cancelBinding != login.CSRFToken || stub.cancelToken != "synthetic-performance-token" {
		t.Fatalf("cancel = %d binding=%q token=%q body=%s", response.StatusCode, stub.cancelBinding, stub.cancelToken, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.invalidateBinding != login.CSRFToken {
		t.Fatalf("logout invalidation = %d binding=%q body=%s", response.StatusCode, stub.invalidateBinding, readBody(response))
	}
}

func TestPerformancePolicyHTTPErrorMappingAndBodyLimit(t *testing.T) {
	for _, body := range []string{
		`{"previewToken":"a","previewToken":"b"}`,
		`{"previewToken":"a","policy":{}}`,
		`{"previewToken":"a"} {}`,
	} {
		if _, err := decodePerformancePolicyToken([]byte(body)); err == nil {
			t.Fatalf("accepted invalid token body: %s", body)
		}
	}
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{performancepolicy.ErrInvalidRequest, http.StatusBadRequest, "invalid-request"},
		{performancepolicy.ErrPreviewExpired, http.StatusConflict, "preview-expired"},
		{performancepolicy.ErrPreviewStale, http.StatusConflict, "preview-stale"},
		{performancepolicy.ErrBusy, http.StatusConflict, "busy"},
		{performancepolicy.ErrSave, http.StatusInternalServerError, "save-failed"},
		{performancepolicy.ErrUnavailable, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		writePerformancePolicyError(recorder, test.err)
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("error %v = %d %s", test.err, recorder.Code, recorder.Body.String())
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/performance/policy/preview", strings.NewReader(strings.Repeat("x", maxPerformancePolicyBody+1)))
	request.Header.Set("Content-Type", "application/json")
	if _, ok := decodePerformancePolicyBody(recorder, request); ok || recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body = ok=%v status=%d", ok, recorder.Code)
	}
}
