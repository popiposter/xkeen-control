package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nativebackup"
)

type nativeTransferStub struct {
	previewCalls, stageCalls, cancelCalls, invalidates int
	owner, canceled                                    string
	mapping                                            map[string]string
	afterPreview                                       func()
}

func (s *nativeTransferStub) Preview(_ context.Context, owner string, archive []byte, passphrase string, mapping map[string]string) (nativebackup.TransferPreview, error) {
	s.previewCalls++
	s.owner = owner
	s.mapping = mapping
	if string(archive) != "synthetic encrypted archive" || passphrase != "synthetic transfer passphrase" {
		panic("wrong private upload")
	}
	if s.afterPreview != nil {
		s.afterPreview()
	}
	return nativebackup.TransferPreview{Token: strings.Repeat("a", 32), Files: []string{"05_routing.json"}, Nodes: 1}, nil
}
func (s *nativeTransferStub) Stage(_ context.Context, owner, token string, checked bool) (string, error) {
	s.stageCalls++
	s.owner = owner
	if token != strings.Repeat("a", 32) || !checked {
		panic("incorrect stage")
	}
	return strings.Repeat("b", 64), nil
}
func (s *nativeTransferStub) Cancel(owner, token string) {
	s.cancelCalls++
	s.owner = owner
	s.canceled = token
}
func (s *nativeTransferStub) Invalidate(string) { s.invalidates++ }
func (s *nativeTransferStub) InvalidateAll()    { s.invalidates++ }

func transferHTTP(t *testing.T, stub *nativeTransferStub) (*httptest.Server, *http.Client, *auth.Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-current-password"
	if err := setHTTPTestPassword(path, []byte(password)); err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.Config{HashPath: path})
	server := httptest.NewServer(New(Config{Auth: manager, NativeTransfer: stub}))
	t.Cleanup(server.Close)
	client := &http.Client{Jar: mustCookieJar(t)}
	r := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, r, &login)
	return server, client, manager, login.CSRFToken
}

func transferUpload(t *testing.T, server *httptest.Server, client *http.Client, csrf string, fields [][2]string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	writer.Close()
	r, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/xkeen/transfer/preview", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Origin", server.URL)
	r.Header.Set("X-CSRF-Token", csrf)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestNativeTransferHTTPPrivatePreviewStageAndSessionRetirement(t *testing.T) {
	stub := &nativeTransferStub{}
	server, client, manager, csrf := transferHTTP(t, stub)
	fields := [][2]string{{"bundle", "synthetic encrypted archive"}, {"passphrase", "synthetic transfer passphrase"}, {"mapping", `{"old":"new"}`}}
	r := transferUpload(t, server, client, "", fields)
	r.Body.Close()
	if r.StatusCode != 403 || stub.previewCalls != 0 {
		t.Fatal("CSRF bypass")
	}
	r = transferUpload(t, server, &http.Client{}, csrf, fields)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("unauthenticated upload accepted")
	}
	r = transferUpload(t, server, client, csrf, fields)
	var preview nativebackup.TransferPreview
	decodeResponse(t, r, &preview)
	if r.StatusCode != 200 || preview.Token != strings.Repeat("a", 32) || stub.owner != csrf || stub.mapping["old"] != "new" || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("private preview boundary")
	}
	for _, body := range []string{
		`{"token":"` + preview.Token + `"}`, `{"token":"` + preview.Token + `","nativeSettingsChecked":false}`,
		`{"token":"` + preview.Token + `","token":"` + preview.Token + `","nativeSettingsChecked":true}`,
		`{"token":"` + preview.Token + `","nativeSettingsChecked":true,"extra":true}`,
	} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/xkeen/transfer/stage", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", server.URL)
		request.Header.Set("X-CSRF-Token", csrf)
		r, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 || stub.stageCalls != 0 {
			t.Fatal("invalid stage accepted")
		}
	}
	r = postJSON(t, client, server.URL+"/api/v1/xkeen/transfer/stage", map[string]any{"token": preview.Token, "nativeSettingsChecked": true}, csrf)
	var staged map[string]any
	decodeResponse(t, r, &staged)
	if r.StatusCode != 200 || stub.stageCalls != 1 || staged["restartRequired"] != true {
		t.Fatal("stage did not save without command")
	}
	stub.afterPreview = manager.InvalidateAll
	r = transferUpload(t, server, client, csrf, fields)
	r.Body.Close()
	if r.StatusCode != 401 || stub.canceled != preview.Token || stub.cancelCalls != 1 {
		t.Fatal("late session preview retained/disclosed")
	}
}

func TestNativeTransferUploadRejectsDuplicatesUnknownMappingAndOversize(t *testing.T) {
	stub := &nativeTransferStub{}
	server, client, _, csrf := transferHTTP(t, stub)
	base := [][2]string{{"bundle", "synthetic encrypted archive"}, {"passphrase", "synthetic transfer passphrase"}}
	for _, extra := range [][2]string{{"bundle", "duplicate"}, {"extra", "unknown"}, {"mapping", `{"old":"new","old":"other"}`}, {"mapping", `{"old":null}`}, {"mapping", `{} {}`}, {"passphrase", strings.Repeat("x", 257)}} {
		fields := append(append([][2]string{}, base...), extra)
		r := transferUpload(t, server, client, csrf, fields)
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		if r.StatusCode != 400 && r.StatusCode != 413 {
			t.Fatalf("invalid multipart accepted: %s %d", extra[0], r.StatusCode)
		}
	}
	if stub.previewCalls != 0 {
		t.Fatal("invalid upload reached decryption")
	}
}
