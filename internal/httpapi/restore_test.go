package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/restore"
)

type httpRestoreServiceStub struct {
	mu                 sync.Mutex
	preview            restore.Preview
	previewErr         error
	previewBinding     string
	previewMode        restore.Mode
	previewContents    []byte
	previewPassphrase  string
	applyResult        restore.ApplyResult
	applyErr           error
	applyBinding       string
	applyToken         string
	cancelBinding      string
	cancelToken        string
	previewStarted     chan struct{}
	releasePreview     chan struct{}
	previewCalls       atomic.Int32
	invalidateAllCalls atomic.Int32
	invalidateStarted  chan struct{}
	releaseInvalidate  chan struct{}
	invalidateOnce     sync.Once
}

func (stub *httpRestoreServiceStub) PreviewBundle(_ context.Context, binding string, contents []byte, passphrase string, mode restore.Mode) (restore.Preview, error) {
	stub.mu.Lock()
	stub.previewBinding = binding
	stub.previewMode = mode
	stub.previewContents = contents
	stub.previewPassphrase = passphrase
	preview := stub.preview
	err := stub.previewErr
	started := stub.previewStarted
	release := stub.releasePreview
	call := stub.previewCalls.Add(1)
	stub.mu.Unlock()
	if started != nil && call == 1 {
		close(started)
	}
	if release != nil {
		<-release
	}
	if preview.Mode == "" {
		preview.Mode = mode
	}
	return preview, err
}

func (stub *httpRestoreServiceStub) Apply(_ context.Context, binding, token string) (restore.ApplyResult, error) {
	stub.mu.Lock()
	stub.applyBinding = binding
	stub.applyToken = token
	result := stub.applyResult
	err := stub.applyErr
	stub.mu.Unlock()
	return result, err
}

func (stub *httpRestoreServiceStub) Cancel(binding, token string) {
	stub.mu.Lock()
	stub.cancelBinding = binding
	stub.cancelToken = token
	stub.mu.Unlock()
}

func (stub *httpRestoreServiceStub) Invalidate(binding string) {
	stub.mu.Lock()
	stub.cancelBinding = binding
	started := stub.invalidateStarted
	release := stub.releaseInvalidate
	stub.mu.Unlock()
	if started != nil {
		stub.invalidateOnce.Do(func() { close(started) })
	}
	if release != nil {
		<-release
	}
}

func (stub *httpRestoreServiceStub) InvalidateAll() {
	stub.invalidateAllCalls.Add(1)
}

func newHTTPRestoreServer(t *testing.T, stub *httpRestoreServiceStub) (*httptest.Server, *http.Client, *auth.Manager, string) {
	t.Helper()
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-control-password"
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: hashPath})
	server := httptest.NewServer(New(Config{Auth: manager, Restore: stub}))
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readBody(loginResponse))
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	return server, client, manager, login.CSRFToken
}

func restoreFixtureBundle(t *testing.T) []byte {
	t.Helper()
	contents, err := httpBackupService(t, fastHTTPBackupDeriver).Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func newRestoreMultipartRequest(t *testing.T, target string, bundle []byte, passphrase, filename, csrf string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("bundle", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bundle); err != nil {
		t.Fatal(err)
	}
	if passphrase != "" {
		if err := writer.WriteField("passphrase", passphrase); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set(auth.CSRFHeader, csrf)
	return request
}

func restorePreviewURL(server *httptest.Server) string {
	return server.URL + "/api/v1/backup/import/preview?mode=settings-only"
}

func TestRestoreApplyCancelAuthOriginAndCSRF(t *testing.T) {
	stub := &httpRestoreServiceStub{}
	server, client, _, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	unauthenticated := &http.Client{Jar: mustCookieJar(t)}
	for _, endpoint := range []string{"apply", "cancel"} {
		target := server.URL + "/api/v1/backup/import/" + endpoint
		response := postJSON(t, unauthenticated, target, map[string]string{"previewToken": "synthetic-preview-token"}, csrf)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s = %d", endpoint, response.StatusCode)
		}
		response.Body.Close()

		response = postJSON(t, client, target, map[string]string{"previewToken": "synthetic-preview-token"}, "")
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s without csrf = %d", endpoint, response.StatusCode)
		}
		response.Body.Close()

		request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(`{"previewToken":"synthetic-preview-token"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(auth.CSRFHeader, csrf)
		request.Header.Set("Origin", "http://evil.example")
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin %s = %d", endpoint, response.StatusCode)
		}
		response.Body.Close()
	}
}

func TestRestorePreviewHTTPAuthQueryAndMultipartLimits(t *testing.T) {
	stub := &httpRestoreServiceStub{preview: restore.Preview{
		Token:     "synthetic-preview-token",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}}
	server, client, _, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	bundle := restoreFixtureBundle(t)

	unauthenticated, err := http.NewRequest(http.MethodPost, restorePreviewURL(server), strings.NewReader("not multipart"))
	if err != nil {
		t.Fatal(err)
	}
	unauthenticated.Header.Set("Content-Type", "text/plain")
	unauthenticatedResponse, err := http.DefaultClient.Do(unauthenticated)
	if err != nil {
		t.Fatal(err)
	}
	if unauthenticatedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated preview = %d", unauthenticatedResponse.StatusCode)
	}
	unauthenticatedResponse.Body.Close()

	withoutCSRF := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "../../hostile-name.json", "")
	response, err := client.Do(withoutCSRF)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("preview without csrf = %d", response.StatusCode)
	}
	response.Body.Close()

	crossOrigin := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "../../hostile-name.json", csrf)
	crossOrigin.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(crossOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin preview = %d", response.StatusCode)
	}
	response.Body.Close()

	wrongMedia, err := http.NewRequest(http.MethodPost, restorePreviewURL(server), strings.NewReader(`{"bundle":"not accepted"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrongMedia.Header.Set("Content-Type", "application/json")
	wrongMedia.Header.Set(auth.CSRFHeader, csrf)
	wrongMediaResponse, err := client.Do(wrongMedia)
	if err != nil {
		t.Fatal(err)
	}
	if wrongMediaResponse.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong preview media type = %d", wrongMediaResponse.StatusCode)
	}
	wrongMediaResponse.Body.Close()

	malformedMedia, err := http.NewRequest(http.MethodPost, restorePreviewURL(server), strings.NewReader("malformed"))
	if err != nil {
		t.Fatal(err)
	}
	malformedMedia.Header.Set("Content-Type", `multipart/form-data; boundary="unterminated`)
	malformedMedia.Header.Set(auth.CSRFHeader, csrf)
	malformedResponse, err := client.Do(malformedMedia)
	if err != nil {
		t.Fatal(err)
	}
	if malformedResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed multipart media type = %d", malformedResponse.StatusCode)
	}
	malformedResponse.Body.Close()

	for _, target := range []string{
		server.URL + "/api/v1/backup/import/preview?mode=settings-only&mode=merge-registry",
		server.URL + "/api/v1/backup/import/preview?mode=settings-only&unknown=value",
		server.URL + "/api/v1/backup/import/preview?mode=unknown",
	} {
		request := newRestoreMultipartRequest(t, target, bundle, "", "bundle.json", csrf)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("strict preview query %q = %d", target, response.StatusCode)
		}
		response.Body.Close()
	}

	request := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "synthetic passphrase", "../../hostile-name.json", csrf)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var preview restore.Preview
	decodeResponse(t, response, &preview)
	if response.StatusCode != http.StatusOK || preview.Token != "synthetic-preview-token" || stub.previewMode != restore.SettingsOnly || stub.previewPassphrase != "synthetic passphrase" || stub.previewBinding != csrf {
		t.Fatalf("valid preview = %d %+v binding=%q mode=%q passphrase=%q", response.StatusCode, preview, stub.previewBinding, stub.previewMode, stub.previewPassphrase)
	}
	if len(stub.previewContents) == 0 {
		t.Fatal("preview did not receive bundle")
	}
	for index, value := range stub.previewContents {
		if value != 0 {
			t.Fatalf("preview upload byte %d was not cleared", index)
		}
	}

	extraParameter := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "bundle.json", csrf)
	extraParameter.Header.Set("Content-Type", extraParameter.Header.Get("Content-Type")+"; charset=utf-8")
	response, err = client.Do(extraParameter)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("multipart with extra parameter = %d", response.StatusCode)
	}
	response.Body.Close()

	oversizedBundle := newRestoreMultipartRequest(t, restorePreviewURL(server), bytes.Repeat([]byte("x"), backup.MaxEncryptedEnvelope+1), "", "bundle.json", csrf)
	response, err = client.Do(oversizedBundle)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized bundle = %d", response.StatusCode)
	}
	response.Body.Close()

	oversizedPassphrase := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, strings.Repeat("p", backup.MaxPassphraseBytes+1), "bundle.json", csrf)
	response, err = client.Do(oversizedPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized passphrase = %d", response.StatusCode)
	}
	response.Body.Close()

	tooLargeTotal, err := http.NewRequest(http.MethodPost, restorePreviewURL(server), bytes.NewReader(bytes.Repeat([]byte("x"), maxRestoreRequestBody+1)))
	if err != nil {
		t.Fatal(err)
	}
	tooLargeTotal.Header.Set("Content-Type", `multipart/form-data; boundary=synthetic-boundary`)
	tooLargeTotal.Header.Set(auth.CSRFHeader, csrf)
	response, err = client.Do(tooLargeTotal)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized total request = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestRestorePreviewRejectsPartShapeAndSingleFlight(t *testing.T) {
	stub := &httpRestoreServiceStub{preview: restore.Preview{Token: "synthetic-preview-token"}}
	server, client, _, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	bundle := restoreFixtureBundle(t)

	makeShapeRequest := func(parts func(*multipart.Writer) error) *http.Request {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := parts(writer); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(http.MethodPost, restorePreviewURL(server), &body)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set(auth.CSRFHeader, csrf)
		return request
	}

	shapeCases := []struct {
		name  string
		parts func(*multipart.Writer) error
	}{
		{name: "missing bundle", parts: func(writer *multipart.Writer) error { return writer.WriteField("passphrase", "synthetic passphrase") }},
		{name: "unknown part", parts: func(writer *multipart.Writer) error { return writer.WriteField("unexpected", "synthetic") }},
		{name: "duplicate bundle", parts: func(writer *multipart.Writer) error {
			for count := 0; count < 2; count++ {
				part, err := writer.CreateFormFile("bundle", "bundle.json")
				if err != nil {
					return err
				}
				if _, err := part.Write(bundle); err != nil {
					return err
				}
			}
			return nil
		}},
		{name: "more than two parts", parts: func(writer *multipart.Writer) error {
			part, err := writer.CreateFormFile("bundle", "bundle.json")
			if err != nil {
				return err
			}
			if _, err := part.Write(bundle); err != nil {
				return err
			}
			if err := writer.WriteField("passphrase", "synthetic passphrase"); err != nil {
				return err
			}
			return writer.WriteField("unexpected", "synthetic")
		}},
	}
	for _, test := range shapeCases {
		t.Run(test.name, func(t *testing.T) {
			response, err := client.Do(makeShapeRequest(test.parts))
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("part shape = %d", response.StatusCode)
			}
			response.Body.Close()
		})
	}

	stub.previewStarted = make(chan struct{})
	stub.releasePreview = make(chan struct{})
	firstDone := make(chan *http.Response, 1)
	go func() {
		request := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "bundle.json", csrf)
		response, err := client.Do(request)
		if err != nil {
			firstDone <- nil
			return
		}
		firstDone <- response
	}()
	select {
	case <-stub.previewStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("preview did not enter single-flight service")
	}
	secondRequest := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "bundle.json", csrf)
	secondResponse, err := client.Do(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if secondResponse.StatusCode != http.StatusConflict {
		t.Fatalf("concurrent preview = %d", secondResponse.StatusCode)
	}
	secondResponse.Body.Close()
	close(stub.releasePreview)
	firstResponse := <-firstDone
	if firstResponse == nil {
		t.Fatal("first preview request failed")
	}
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("admitted preview = %d %s", firstResponse.StatusCode, readBody(firstResponse))
	}
	firstResponse.Body.Close()
}

func TestRestorePreviewSessionInvalidationRaceCancelsToken(t *testing.T) {
	stub := &httpRestoreServiceStub{
		preview:        restore.Preview{Token: "synthetic-preview-token"},
		previewStarted: make(chan struct{}),
		releasePreview: make(chan struct{}),
	}
	server, client, manager, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	bundle := restoreFixtureBundle(t)
	result := make(chan *http.Response, 1)
	go func() {
		request := newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "bundle.json", csrf)
		response, err := client.Do(request)
		if err != nil {
			result <- nil
			return
		}
		result <- response
	}()
	select {
	case <-stub.previewStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("preview did not enter service")
	}
	manager.InvalidateAll()
	close(stub.releasePreview)
	response := <-result
	if response == nil {
		t.Fatal("preview request failed")
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("preview after session invalidation = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	stub.mu.Lock()
	canceledBinding, canceledToken := stub.cancelBinding, stub.cancelToken
	stub.mu.Unlock()
	if canceledBinding != csrf || canceledToken != "synthetic-preview-token" {
		t.Fatalf("orphaned preview cancellation binding=%q token=%q", canceledBinding, canceledToken)
	}
}

func TestRestorePreviewLogoutOrderingCancelsInFlightToken(t *testing.T) {
	stub := &httpRestoreServiceStub{
		preview:           restore.Preview{Token: "synthetic-preview-token"},
		previewStarted:    make(chan struct{}),
		releasePreview:    make(chan struct{}),
		invalidateStarted: make(chan struct{}),
		releaseInvalidate: make(chan struct{}),
	}
	server, client, manager, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	bundle := restoreFixtureBundle(t)
	var releasePreviewOnce sync.Once
	releasePreview := func() { releasePreviewOnce.Do(func() { close(stub.releasePreview) }) }
	defer releasePreview()
	var releaseLogoutOnce sync.Once
	releaseLogout := func() { releaseLogoutOnce.Do(func() { close(stub.releaseInvalidate) }) }
	defer releaseLogout()

	previewDone := make(chan *http.Response, 1)
	go func() {
		response, err := client.Do(newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "bundle.json", csrf))
		if err != nil {
			previewDone <- nil
			return
		}
		previewDone <- response
	}()
	select {
	case <-stub.previewStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("preview did not enter service")
	}

	logoutRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/session/logout", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	logoutRequest.Header.Set("Content-Type", "application/json")
	logoutRequest.Header.Set(auth.CSRFHeader, csrf)
	logoutDone := make(chan *http.Response, 1)
	go func() {
		response, requestErr := client.Do(logoutRequest)
		if requestErr != nil {
			logoutDone <- nil
			return
		}
		logoutDone <- response
	}()
	select {
	case <-stub.invalidateStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not enter synchronous restore invalidation")
	}

	checkRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	cookieURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range client.Jar.Cookies(cookieURL) {
		checkRequest.AddCookie(cookie)
	}
	if _, active := manager.SessionFromRequest(checkRequest); active {
		t.Fatal("logout entered restore invalidation before removing the auth session")
	}

	releasePreview()
	previewResponse := <-previewDone
	if previewResponse == nil {
		t.Fatal("in-flight preview request failed")
	}
	if previewResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("in-flight preview after logout = %d %s", previewResponse.StatusCode, readBody(previewResponse))
	}
	previewResponse.Body.Close()
	stub.mu.Lock()
	canceledBinding, canceledToken := stub.cancelBinding, stub.cancelToken
	stub.mu.Unlock()
	if canceledBinding != csrf || canceledToken != "synthetic-preview-token" {
		t.Fatalf("logout race cancellation binding=%q token=%q", canceledBinding, canceledToken)
	}

	releaseLogout()
	logoutResponse := <-logoutDone
	if logoutResponse == nil {
		t.Fatal("logout request failed")
	}
	if logoutResponse.StatusCode != http.StatusOK {
		t.Fatalf("logout after preview race = %d %s", logoutResponse.StatusCode, readBody(logoutResponse))
	}
	logoutResponse.Body.Close()
}

func TestRestoreApplyCancelBodiesAndSafeErrorMappings(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "expired", err: restore.ErrPreviewExpired, status: http.StatusConflict},
		{name: "stale", err: restore.ErrPreviewStale, status: http.StatusConflict},
		{name: "compatibility", err: restore.ErrCompatibilityBlocked, status: http.StatusConflict},
		{name: "candidate", err: restore.ErrCandidateInvalid, status: http.StatusBadRequest},
		{name: "unavailable", err: restore.ErrUnavailable, status: http.StatusServiceUnavailable},
		{name: "recovery", err: restore.ErrRecoveryRequired, status: http.StatusServiceUnavailable},
		{name: "recovery failure", err: restore.ErrRecoveryFailed, status: http.StatusServiceUnavailable},
		{name: "authority", err: restore.ErrAuthorityBusy, status: http.StatusServiceUnavailable},
		{name: "apply rollback", err: restore.ErrApplyFailed, status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &httpRestoreServiceStub{applyErr: test.err}
			server, client, _, csrf := newHTTPRestoreServer(t, stub)
			defer server.Close()
			response := postJSON(t, client, server.URL+"/api/v1/backup/import/apply", map[string]string{"previewToken": "synthetic-preview-token"}, csrf)
			body := readBody(response)
			if response.StatusCode != test.status {
				t.Fatalf("apply status = %d, want %d, body=%s", response.StatusCode, test.status, body)
			}
			if strings.Contains(body, "synthetic") && test.name != "" {
				t.Fatalf("apply response exposed fixture detail: %s", body)
			}
			stub.mu.Lock()
			binding, token := stub.applyBinding, stub.applyToken
			stub.mu.Unlock()
			if binding != csrf || token != "synthetic-preview-token" {
				t.Fatalf("apply token binding=%q token=%q", binding, token)
			}
		})
	}

	stub := &httpRestoreServiceStub{applyResult: restore.ApplyResult{Mode: restore.SettingsOnly, Classification: "applied"}}
	server, client, _, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	valid := postJSON(t, client, server.URL+"/api/v1/backup/import/apply", map[string]string{"previewToken": "synthetic-preview-token"}, csrf)
	if valid.StatusCode != http.StatusOK {
		t.Fatalf("valid apply = %d %s", valid.StatusCode, readBody(valid))
	}
	valid.Body.Close()
	for _, body := range []string{
		`{"previewToken":"synthetic-preview-token","mode":"settings-only"}`,
		`{"previewToken":"synthetic-preview-token"} {}`,
	} {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/backup/import/apply", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(auth.CSRFHeader, csrf)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("strict apply body %q = %d", body, response.StatusCode)
		}
		response.Body.Close()
	}
	queryRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/backup/import/apply?mode=merge-registry", strings.NewReader(`{"previewToken":"synthetic-preview-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	queryRequest.Header.Set("Content-Type", "application/json")
	queryRequest.Header.Set(auth.CSRFHeader, csrf)
	queryResponse, err := client.Do(queryRequest)
	if err != nil {
		t.Fatal(err)
	}
	if queryResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("apply with mode query = %d", queryResponse.StatusCode)
	}
	queryResponse.Body.Close()
	largeBody := `{"previewToken":"` + strings.Repeat("x", maxRestoreJSONBody) + `"}`
	largeRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/backup/import/apply", strings.NewReader(largeBody))
	if err != nil {
		t.Fatal(err)
	}
	largeRequest.Header.Set("Content-Type", "application/json")
	largeRequest.Header.Set(auth.CSRFHeader, csrf)
	largeResponse, err := client.Do(largeRequest)
	if err != nil {
		t.Fatal(err)
	}
	if largeResponse.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("large apply body = %d", largeResponse.StatusCode)
	}
	largeResponse.Body.Close()

	cancel := postJSON(t, client, server.URL+"/api/v1/backup/import/cancel", map[string]string{"previewToken": "synthetic-preview-token"}, csrf)
	body, _ := io.ReadAll(cancel.Body)
	cancel.Body.Close()
	if cancel.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"canceled":true}` {
		t.Fatalf("cancel = %d %q", cancel.StatusCode, body)
	}
	if stub.cancelBinding != csrf || stub.cancelToken != "synthetic-preview-token" {
		t.Fatalf("cancel binding=%q token=%q", stub.cancelBinding, stub.cancelToken)
	}
}

func TestRestorePreviewInvalidatesOnLogoutAndPasswordReplacement(t *testing.T) {
	stub := &httpRestoreServiceStub{preview: restore.Preview{Token: "synthetic-preview-token"}}
	server, client, _, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	bundle := restoreFixtureBundle(t)
	response, err := client.Do(newRestoreMultipartRequest(t, restorePreviewURL(server), bundle, "", "bundle.json", csrf))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preview before logout = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	logout := postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, csrf)
	if logout.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d %s", logout.StatusCode, readBody(logout))
	}
	logout.Body.Close()
	stub.mu.Lock()
	logoutBinding := stub.cancelBinding
	stub.mu.Unlock()
	if logoutBinding != csrf {
		t.Fatalf("logout did not invalidate restore binding %q", logoutBinding)
	}

	stub = &httpRestoreServiceStub{preview: restore.Preview{Token: "synthetic-preview-token"}}
	server2, client2, _, csrf2 := newHTTPRestoreServer(t, stub)
	defer server2.Close()
	response, err = client2.Do(newRestoreMultipartRequest(t, restorePreviewURL(server2), bundle, "", "bundle.json", csrf2))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	passwordChange := postJSON(t, client2, server2.URL+"/api/v1/session/password", map[string]string{"newPassword": "synthetic-new-password"}, csrf2)
	if passwordChange.StatusCode != http.StatusOK {
		t.Fatalf("password replacement = %d %s", passwordChange.StatusCode, readBody(passwordChange))
	}
	passwordChange.Body.Close()
	if stub.invalidateAllCalls.Load() != 1 {
		t.Fatalf("password replacement restore invalidations = %d", stub.invalidateAllCalls.Load())
	}
}

func TestRestorePreviewErrorResponsesDoNotExposeCause(t *testing.T) {
	stub := &httpRestoreServiceStub{previewErr: errors.New("synthetic secret-bearing parser detail")}
	server, client, _, csrf := newHTTPRestoreServer(t, stub)
	defer server.Close()
	request := newRestoreMultipartRequest(t, restorePreviewURL(server), restoreFixtureBundle(t), "", "bundle.json", csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(response)
	if response.StatusCode != http.StatusBadRequest || strings.Contains(body, "synthetic secret-bearing parser detail") {
		t.Fatalf("unsafe preview error = %d %s", response.StatusCode, body)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
}
