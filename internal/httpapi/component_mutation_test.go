package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/components"
)

type f1ComponentMutationHTTPStub struct {
	previews       atomic.Int32
	applies        atomic.Int32
	rollbacks      atomic.Int32
	cancels        atomic.Int32
	invalidates    atomic.Int32
	invalidatesAll atomic.Int32
	applyErr       error
}

func (stub *f1ComponentMutationHTTPStub) Preview(_ context.Context, _ string, request components.MutationRequest) (components.MutationPreview, error) {
	stub.previews.Add(1)
	return components.MutationPreview{
		SchemaVersion: components.MutationSchemaVersion,
		PreviewToken:  "synthetic-preview-token",
		Component:     request.Component,
		Operation:     request.Operation,
		Channel:       request.Channel,
		ExpiresAt:     time.Date(2026, time.September, 5, 13, 0, 0, 0, time.UTC),
	}, nil
}

func (stub *f1ComponentMutationHTTPStub) Apply(_ context.Context, _, _ string) (components.MutationResult, error) {
	stub.applies.Add(1)
	if stub.applyErr != nil {
		return components.MutationResult{}, stub.applyErr
	}
	return components.MutationResult{SchemaVersion: components.MutationSchemaVersion, Component: components.KindXray, Operation: components.MutationOperationUpdate, Channel: components.MutationChannelStable, State: "applied"}, nil
}

func (stub *f1ComponentMutationHTTPStub) Rollback(_ context.Context, _, _ string) (components.MutationResult, error) {
	stub.rollbacks.Add(1)
	return components.MutationResult{SchemaVersion: components.MutationSchemaVersion, Component: components.KindXray, Operation: components.MutationOperationRollback, State: "rolled-back"}, nil
}

func (stub *f1ComponentMutationHTTPStub) Cancel(_, _ string) { stub.cancels.Add(1) }
func (stub *f1ComponentMutationHTTPStub) Invalidate(string)  { stub.invalidates.Add(1) }
func (stub *f1ComponentMutationHTTPStub) InvalidateAll()     { stub.invalidatesAll.Add(1) }

func TestComponentMutationRoutesAreStrictAuthenticatedAndInvalidateSessions(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	mutations := &f1ComponentMutationHTTPStub{}
	server := httptest.NewServer(New(Config{
		Auth:               auth.NewManager(auth.Config{HashPath: passwordPath}),
		ComponentMutations: mutations,
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	body := `{"component":"xray","operation":"update","channel":"stable"}`
	response := componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", body, "application/json", "", "")
	if response.StatusCode != http.StatusUnauthorized || mutations.previews.Load() != 0 {
		t.Fatalf("unauthenticated preview = %d calls=%d", response.StatusCode, mutations.previews.Load())
	}
	response.Body.Close()

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	if login.CSRFToken == "" {
		t.Fatal("login did not return csrf token")
	}

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", body, "application/json", "", "")
	if response.StatusCode != http.StatusForbidden || mutations.previews.Load() != 0 {
		t.Fatalf("preview without csrf = %d calls=%d", response.StatusCode, mutations.previews.Load())
	}
	response.Body.Close()

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", body, "application/json; charset=utf-8", login.CSRFToken, "")
	if response.StatusCode != http.StatusUnsupportedMediaType || mutations.previews.Load() != 0 {
		t.Fatalf("parameterized content type = %d calls=%d", response.StatusCode, mutations.previews.Load())
	}
	response.Body.Close()

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", `{"component":"xray","operation":"update","channel":"stable","url":"https://evil.example"}`, "application/json", login.CSRFToken, "")
	if response.StatusCode != http.StatusBadRequest || mutations.previews.Load() != 0 {
		t.Fatalf("unknown mutation field = %d calls=%d", response.StatusCode, mutations.previews.Load())
	}
	var invalidBody struct {
		Code string `json:"code"`
	}
	decodeResponse(t, response, &invalidBody)
	if invalidBody.Code != "invalid-request" {
		t.Fatalf("unknown mutation field code = %q", invalidBody.Code)
	}

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", body+`{}`, "application/json", login.CSRFToken, "")
	if response.StatusCode != http.StatusBadRequest || mutations.previews.Load() != 0 {
		t.Fatalf("trailing mutation JSON = %d calls=%d", response.StatusCode, mutations.previews.Load())
	}
	response.Body.Close()

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", body, "application/json", login.CSRFToken, "http://evil.example")
	if response.StatusCode != http.StatusForbidden || mutations.previews.Load() != 0 {
		t.Fatalf("cross-origin preview = %d calls=%d", response.StatusCode, mutations.previews.Load())
	}
	response.Body.Close()

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/preview", body, "application/json", login.CSRFToken, "")
	if response.StatusCode != http.StatusOK || mutations.previews.Load() != 1 {
		t.Fatalf("valid preview = %d calls=%d body=%s", response.StatusCode, mutations.previews.Load(), readBody(response))
	}
	response.Body.Close()

	tokenBody := `{"previewToken":"synthetic-preview-token"}`
	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/apply", tokenBody, "application/json", login.CSRFToken, "")
	if response.StatusCode != http.StatusOK || mutations.applies.Load() != 1 {
		t.Fatalf("valid apply = %d calls=%d body=%s", response.StatusCode, mutations.applies.Load(), readBody(response))
	}
	response.Body.Close()

	response = componentMutationRawPost(t, client, server.URL+"/api/v1/components/cancel", tokenBody, "application/json", login.CSRFToken, "")
	if response.StatusCode != http.StatusOK || mutations.cancels.Load() != 1 {
		t.Fatalf("valid cancel = %d calls=%d body=%s", response.StatusCode, mutations.cancels.Load(), readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || mutations.invalidates.Load() != 1 {
		t.Fatalf("logout invalidation = %d invalidates=%d", response.StatusCode, mutations.invalidates.Load())
	}
	response.Body.Close()

	loginResponse = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	decodeResponse(t, loginResponse, &login)
	response = postJSON(t, client, server.URL+"/api/v1/session/password", map[string]string{"newPassword": "synthetic-new-password"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || mutations.invalidatesAll.Load() != 1 {
		t.Fatalf("password invalidation = %d invalidates-all=%d body=%s", response.StatusCode, mutations.invalidatesAll.Load(), readBody(response))
	}
	response.Body.Close()
}

func TestComponentMutationRouteSanitizesBackendFailure(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	mutations := &f1ComponentMutationHTTPStub{applyErr: components.ErrMutationTransactionFailed}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), ComponentMutations: mutations}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	preview := postJSON(t, client, server.URL+"/api/v1/components/preview", map[string]string{"component": "xray", "operation": "update", "channel": "stable"}, login.CSRFToken)
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview = %d %s", preview.StatusCode, readBody(preview))
	}
	preview.Body.Close()
	response := postJSON(t, client, server.URL+"/api/v1/components/apply", map[string]string{"previewToken": "synthetic-preview-token"}, login.CSRFToken)
	contents := readBody(response)
	if response.StatusCode != http.StatusInternalServerError || !strings.Contains(contents, "previous generation restored") || strings.Contains(contents, "password.bcrypt") {
		t.Fatalf("sanitized failure = %d %s", response.StatusCode, contents)
	}
}

func TestComponentMutationRouteDoesNotClaimRestoreForUnprovenFailure(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	mutations := &f1ComponentMutationHTTPStub{applyErr: components.ErrMutationTransactionUnproven}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), ComponentMutations: mutations}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string
	}
	decodeResponse(t, loginResponse, &login)
	csrf := login.CSRFToken
	if csrf == "" {
		t.Fatal("login did not return csrf token")
	}
	preview := postJSON(t, client, server.URL+"/api/v1/components/preview", map[string]string{"component": "xray", "operation": "update", "channel": "stable"}, csrf)
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview = %d %s", preview.StatusCode, readBody(preview))
	}
	preview.Body.Close()
	response := postJSON(t, client, server.URL+"/api/v1/components/apply", map[string]string{"previewToken": "synthetic-preview-token"}, csrf)
	contents := readBody(response)
	if response.StatusCode != http.StatusInternalServerError || !strings.Contains(contents, "outcome is not proven") || strings.Contains(contents, "previous generation restored") {
		t.Fatalf("unproven failure = %d %s", response.StatusCode, contents)
	}
}

func TestComponentMutationErrorsExposeStableAllowlistedCodes(t *testing.T) {
	tests := []struct {
		err     error
		status  int
		code    string
		message string
	}{
		{components.ErrInvalidMutationRequest, http.StatusBadRequest, "invalid-request", "invalid component mutation request"},
		{components.ErrMutationOperationMismatch, http.StatusBadRequest, "invalid-request", "invalid component mutation request"},
		{components.ErrMutationBusy, http.StatusConflict, "busy", "component mutation busy"},
		{components.ErrMutationPreviewExpired, http.StatusConflict, "preview-expired", "component mutation preview expired or invalid"},
		{components.ErrMutationPreviewStale, http.StatusConflict, "preview-stale", "component mutation preview is stale"},
		{components.ErrMutationNoPrevious, http.StatusConflict, "no-previous", "previous component generation unavailable"},
		{components.ErrMutationMetadataUnavailable, http.StatusBadGateway, "metadata-unavailable", "component metadata unavailable"},
		{components.ErrMutationCandidateRejected, http.StatusBadGateway, "candidate-rejected", "component candidate rejected"},
		{components.ErrMutationTransactionFailed, http.StatusInternalServerError, "transaction-restored", "component transaction failed; previous generation restored"},
		{components.ErrMutationTransactionUnproven, http.StatusInternalServerError, "transaction-unproven", "component transaction failed; outcome is not proven"},
		{components.ErrMutationRollbackUnproven, http.StatusServiceUnavailable, "rollback-unproven", "component rollback or recovery is not proven"},
		{components.ErrMutationMaintenance, http.StatusServiceUnavailable, "maintenance", "component mutation unavailable during maintenance"},
		{components.ErrMutationUnavailable, http.StatusServiceUnavailable, "unavailable", "component mutation unavailable"},
		{errors.New("synthetic unknown backend failure"), http.StatusServiceUnavailable, "unavailable", "component mutation unavailable"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		writeComponentMutationError(recorder, test.err)
		var body struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %v: %v", test.err, err)
		}
		if recorder.Code != test.status || body.Code != test.code || body.Error != test.message {
			t.Fatalf("mapping %v = status %d body %+v", test.err, recorder.Code, body)
		}
		if strings.Contains(recorder.Body.String(), "synthetic unknown backend failure") {
			t.Fatalf("backend detail leaked for %v: %s", test.err, recorder.Body.String())
		}
	}
}

func componentMutationRawPost(t *testing.T, client *http.Client, target, body, contentType, csrf, origin string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	if csrf != "" {
		request.Header.Set(auth.CSRFHeader, csrf)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

var _ ComponentMutationService = (*f1ComponentMutationHTTPStub)(nil)
