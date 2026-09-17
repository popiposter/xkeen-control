package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/c1"
)

type manualHTTPStub struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (s *manualHTTPStub) TriggerManualNode(nodeID string) error {
	s.mu.Lock()
	s.ids = append(s.ids, nodeID)
	err := s.err
	s.mu.Unlock()
	return err
}

func TestManualNodeRouteIsAuthenticatedCSRFBoundAndClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	manual := &manualHTTPStub{}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Manual: manual}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	unauthenticated := postJSON(t, client, server.URL+"/api/v1/performance/manual-node", map[string]string{"nodeId": "node-00000001"}, "")
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated manual route = %d", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/performance/manual-node", map[string]string{"nodeId": "node-00000001"}, "")
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("manual route without csrf = %d", withoutCSRF.StatusCode)
	}
	withoutCSRF.Body.Close()

	accepted := postJSON(t, client, server.URL+"/api/v1/performance/manual-node", map[string]string{"nodeId": "node-00000001"}, login.CSRFToken)
	if accepted.StatusCode != http.StatusAccepted || readBody(accepted) != "{\"accepted\":true,\"state\":\"accepted\"}\n" {
		t.Fatalf("manual accepted response = %d", accepted.StatusCode)
	}
	manual.mu.Lock()
	ids := append([]string(nil), manual.ids...)
	manual.mu.Unlock()
	if len(ids) != 1 || ids[0] != "node-00000001" {
		t.Fatalf("manual target forwarding = %v", ids)
	}

	unknown := postJSON(t, client, server.URL+"/api/v1/performance/manual-node", map[string]any{"nodeId": "node-00000001", "url": "https://secret.invalid"}, login.CSRFToken)
	if unknown.StatusCode != http.StatusBadRequest || len(ids) != 1 {
		unknownBody := readBody(unknown)
		t.Fatalf("unknown manual field = %d %s", unknown.StatusCode, unknownBody)
	}
	unknown.Body.Close()

	trailingRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/manual-node", strings.NewReader(`{"nodeId":"node-00000001"}{}`))
	if err != nil {
		t.Fatal(err)
	}
	trailingRequest.Header.Set("Content-Type", "application/json")
	trailingRequest.Header.Set(auth.CSRFHeader, login.CSRFToken)
	trailing, err := client.Do(trailingRequest)
	if err != nil {
		t.Fatal(err)
	}
	if trailing.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing manual JSON = %d %s", trailing.StatusCode, readBody(trailing))
	}
	trailing.Body.Close()

	wrongTypeRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/manual-node", strings.NewReader(`{"nodeId":"node-00000001"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrongTypeRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	wrongTypeRequest.Header.Set(auth.CSRFHeader, login.CSRFToken)
	wrongType, err := client.Do(wrongTypeRequest)
	if err != nil {
		t.Fatal(err)
	}
	wrongType.Body.Close()
	if wrongType.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong manual content type = %d", wrongType.StatusCode)
	}

	for _, body := range []string{`{}`, `{"nodeId":""}`, `{"nodeId":"not-a-node"}`} {
		invalidRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/manual-node", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		invalidRequest.Header.Set("Content-Type", "application/json")
		invalidRequest.Header.Set(auth.CSRFHeader, login.CSRFToken)
		invalid, err := client.Do(invalidRequest)
		if err != nil {
			t.Fatal(err)
		}
		if invalid.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid manual body %s = %d %s", body, invalid.StatusCode, readBody(invalid))
		}
		invalid.Body.Close()
	}
	manual.mu.Lock()
	defer manual.mu.Unlock()
	if len(manual.ids) != 1 {
		t.Fatalf("invalid manual bodies reached service: %v", manual.ids)
	}
}

type chunkedManualBody struct {
	reader *strings.Reader
}

func (b *chunkedManualBody) Read(value []byte) (int, error) {
	return b.reader.Read(value)
}

func TestManualNodeRouteRejectsCrossOriginQueryAndChunkedOversizeBeforeService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	manual := &manualHTTPStub{}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Manual: manual}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	crossOriginRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/manual-node", strings.NewReader(`{"nodeId":"node-00000001"}`))
	if err != nil {
		t.Fatal(err)
	}
	crossOriginRequest.Header.Set("Content-Type", "application/json")
	crossOriginRequest.Header.Set(auth.CSRFHeader, login.CSRFToken)
	crossOriginRequest.Header.Set("Origin", "http://evil.example")
	crossOrigin, err := client.Do(crossOriginRequest)
	if err != nil {
		t.Fatal(err)
	}
	if crossOrigin.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin manual route = %d %s", crossOrigin.StatusCode, readBody(crossOrigin))
	}
	crossOrigin.Body.Close()

	queryRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/manual-node?bytes=1", strings.NewReader(`{"nodeId":"node-00000001"}`))
	if err != nil {
		t.Fatal(err)
	}
	queryRequest.Header.Set("Content-Type", "application/json")
	queryRequest.Header.Set(auth.CSRFHeader, login.CSRFToken)
	queryResponse, err := client.Do(queryRequest)
	if err != nil {
		t.Fatal(err)
	}
	if queryResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("manual query = %d %s", queryResponse.StatusCode, readBody(queryResponse))
	}
	queryResponse.Body.Close()

	oversizedBody := `{"nodeId":"` + strings.Repeat("a", maxManualNodeBody) + `"}`
	chunkedRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/performance/manual-node", &chunkedManualBody{reader: strings.NewReader(oversizedBody)})
	if err != nil {
		t.Fatal(err)
	}
	if chunkedRequest.ContentLength != 0 {
		t.Fatalf("oversized regression was not chunked: content length=%d", chunkedRequest.ContentLength)
	}
	chunkedRequest.Header.Set("Content-Type", "application/json")
	chunkedRequest.Header.Set(auth.CSRFHeader, login.CSRFToken)
	chunkedResponse, err := client.Do(chunkedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if chunkedResponse.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked oversized manual body = %d %s", chunkedResponse.StatusCode, readBody(chunkedResponse))
	}
	chunkedResponse.Body.Close()

	manual.mu.Lock()
	defer manual.mu.Unlock()
	if len(manual.ids) != 0 {
		t.Fatalf("rejected manual boundary requests reached service: %v", manual.ids)
	}
}

func TestManualNodeRouteMapsBusyCleanupAndUnavailableToClosedStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		err  error
		code int
		body string
	}{
		{name: "busy", err: c1.ErrManualBusy, code: http.StatusConflict, body: `{"accepted":false,"state":"busy"}`},
		{name: "cleanup", err: c1.ErrManualCleanupPending, code: http.StatusConflict, body: `{"accepted":false,"state":"cleanup-pending"}`},
		{name: "invalid", err: c1.ErrManualInvalidTarget, code: http.StatusBadRequest, body: `{"accepted":false,"state":"invalid-target"}`},
		{name: "unavailable", err: c1.ErrManualUnavailable, code: http.StatusServiceUnavailable, body: `{"accepted":false,"state":"unavailable"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			manual := &manualHTTPStub{err: test.err}
			server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Manual: manual}))
			defer server.Close()
			client := &http.Client{Jar: mustCookieJar(t)}
			loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
			var login struct {
				CSRFToken string `json:"csrfToken"`
			}
			decodeResponse(t, loginResponse, &login)
			response := postJSON(t, client, server.URL+"/api/v1/performance/manual-node", map[string]string{"nodeId": "node-00000001"}, login.CSRFToken)
			if response.StatusCode != test.code || strings.TrimSpace(readBody(response)) != test.body {
				t.Fatalf("manual %s = %d %s", test.name, response.StatusCode, readBody(response))
			}
		})
	}
}

func TestManualPerformanceRouteRequiresGETAndDoesNotExposeProviderError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	manual := &manualHTTPStub{err: errors.New("secret provider URL and UUID")}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Manual: manual}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/api/v1/performance/manual-node", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("manual GET = %d", response.StatusCode)
	}
	response.Body.Close()

	accepted := postJSON(t, client, server.URL+"/api/v1/performance/manual-node", map[string]string{"nodeId": "node-00000001"}, login.CSRFToken)
	body := readBody(accepted)
	if strings.Contains(body, "secret provider") || strings.Contains(body, "UUID") {
		t.Fatalf("raw manual error crossed HTTP boundary: %s", body)
	}
}
