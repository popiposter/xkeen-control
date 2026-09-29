package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/panellistener"
	panelupdate "github.com/popiposter/xkeen-control/internal/update"
)

type panelListenerHTTPStub struct {
	mu          sync.Mutex
	previewHost string
	applied     chan string
	canceled    int
	invalidated int
}

func (stub *panelListenerHTTPStub) Read(context.Context) (panellistener.Projection, error) {
	return panellistener.Projection{
		Host: "127.0.0.1", Port: 8787, Source: panellistener.SourceDefault,
		Editability: panellistener.EditabilityEditable, AllowedHosts: []string{"127.0.0.1", "10.0.0.4"},
	}, nil
}

func (stub *panelListenerHTTPStub) Preview(_ context.Context, _ string, host string) (panellistener.Preview, error) {
	if host != "10.0.0.4" {
		return panellistener.Preview{}, errors.New("unexpected host")
	}
	stub.mu.Lock()
	stub.previewHost = host
	stub.mu.Unlock()
	return panellistener.Preview{
		Token: "listener-preview-token", Before: panellistener.Address{Host: "127.0.0.1", Port: 8787},
		After: panellistener.Address{Host: host, Port: 8787}, ReconnectClassification: "loopback-to-lan",
		RestartRequired: true, SessionInvalidated: true, LoginRequired: true,
	}, nil
}

func (stub *panelListenerHTTPStub) Apply(_ context.Context, _ string, token string) (panellistener.ApplyResult, error) {
	if token != "listener-preview-token" {
		return panellistener.ApplyResult{}, errors.New("unexpected token")
	}
	stub.applied <- token
	return panellistener.ApplyResult{
		Accepted: true, State: "rebind-started", Before: panellistener.Address{Host: "127.0.0.1", Port: 8787},
		After: panellistener.Address{Host: "10.0.0.4", Port: 8787}, ReconnectClassification: "loopback-to-lan",
		RestartRequired: true, SessionInvalidated: true, LoginRequired: true,
	}, nil
}

func (stub *panelListenerHTTPStub) Cancel(_ string, token string) {
	if token == "listener-preview-token" {
		stub.mu.Lock()
		stub.canceled++
		stub.mu.Unlock()
	}
}

func (stub *panelListenerHTTPStub) Invalidate(string) {
	stub.mu.Lock()
	stub.invalidated++
	stub.mu.Unlock()
}

func (stub *panelListenerHTTPStub) InvalidateAll() {
	stub.mu.Lock()
	stub.invalidated++
	stub.mu.Unlock()
}

func TestPanelListenerRoutesAreAuthenticatedCSRFBoundAndExact(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	listener := &panelListenerHTTPStub{applied: make(chan string, 1)}
	server := httptest.NewServer(New(Config{
		Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), Listener: listener,
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/api/v1/panel/listener")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated listener read = %d", response.StatusCode)
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

	response = postJSON(t, client, server.URL+"/api/v1/panel/listener/preview", map[string]string{"host": "10.0.0.4"}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("listener Preview without csrf = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/panel/listener/preview", map[string]string{"host": "10.0.0.4", "unexpected": "field"}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("listener Preview with unknown field = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/panel/listener/preview", map[string]string{"host": "10.0.0.4"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || listener.previewHost != "10.0.0.4" {
		t.Fatalf("listener Preview = %d host=%q body=%s", response.StatusCode, listener.previewHost, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/panel/listener/apply", map[string]string{"previewToken": "listener-preview-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("listener Apply = %d %s", response.StatusCode, readBody(response))
	}
	var applied struct {
		Accepted bool   `json:"accepted"`
		State    string `json:"state"`
	}
	decodeResponse(t, response, &applied)
	if !applied.Accepted || applied.State != "rebind-started" {
		t.Fatalf("listener Apply response = %+v", applied)
	}
	select {
	case token := <-listener.applied:
		if token != "listener-preview-token" {
			t.Fatalf("applied token = %q", token)
		}
	default:
		t.Fatal("listener Apply did not reach the typed owner")
	}

	response = postJSON(t, client, server.URL+"/api/v1/panel/listener/cancel", map[string]string{"previewToken": "listener-preview-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("listener Cancel = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	listener.mu.Lock()
	invalidated := listener.invalidated
	listener.mu.Unlock()
	if invalidated != 1 {
		t.Fatalf("listener invalidations after logout = %d", invalidated)
	}

	loginResponse = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	decodeResponse(t, loginResponse, &login)
	response = postJSON(t, client, server.URL+"/api/v1/panel/listener/preview", map[string]string{"host": "10.0.0.4"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("listener Preview before password replacement = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/session/password", map[string]string{"newPassword": "synthetic-new-password"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("password replacement = %d %s", response.StatusCode, readBody(response))
	}
	var passwordResult struct {
		Authenticated bool   `json:"authenticated"`
		State         string `json:"state"`
	}
	decodeResponse(t, response, &passwordResult)
	if passwordResult.Authenticated || passwordResult.State != "reauthentication-required" {
		t.Fatalf("password replacement response = %+v", passwordResult)
	}
	listener.mu.Lock()
	invalidated = listener.invalidated
	listener.mu.Unlock()
	if invalidated != 2 {
		t.Fatalf("listener invalidations after password replacement = %d", invalidated)
	}
	response, err = client.Get(server.URL + "/api/v1/panel/listener")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-password listener read = %d", response.StatusCode)
	}
	response.Body.Close()
}

type checkedUpdateHTTPStub struct {
	checked         atomicBool
	claimed         atomicBool
	rollbackClaimed atomicBool
	applied         chan [2]string
}

type atomicBool struct {
	mu    sync.Mutex
	value bool
}

func (value *atomicBool) Load() bool {
	value.mu.Lock()
	defer value.mu.Unlock()
	return value.value
}

func (value *atomicBool) Store(next bool) {
	value.mu.Lock()
	value.value = next
	value.mu.Unlock()
}

func (value *atomicBool) compareAndSwap(old, next bool) bool {
	value.mu.Lock()
	defer value.mu.Unlock()
	if value.value != old {
		return false
	}
	value.value = next
	return true
}

func (stub *checkedUpdateHTTPStub) Status(context.Context) panelupdate.Status {
	status := panelupdate.Status{
		Channel: "stable", Policy: panelupdate.Policy{Channel: "stable", Mode: "manual", CheckCadenceMinutes: 360},
	}
	if stub.checked.Load() {
		status.LatestCompatible = "1.2.3"
		status.LatestChannel = "stable"
		status.LatestSource = "github-release"
	}
	return status
}

func (stub *checkedUpdateHTTPStub) Check(context.Context, string, string) (panelupdate.Status, error) {
	stub.checked.Store(true)
	return stub.Status(context.Background()), nil
}

func (stub *checkedUpdateHTTPStub) SetPolicy(policy panelupdate.Policy) (panelupdate.Status, error) {
	return panelupdate.Status{Channel: policy.Channel, Policy: policy}, nil
}

func (stub *checkedUpdateHTTPStub) Apply(context.Context, string, string) error { return nil }

func (stub *checkedUpdateHTTPStub) ApplyChecked(_ context.Context, channel, version string) error {
	if !stub.claimed.compareAndSwap(false, true) {
		return errors.New("checked candidate already claimed")
	}
	stub.checked.Store(false)
	stub.applied <- [2]string{channel, version}
	return nil
}

func (stub *checkedUpdateHTTPStub) ValidateChecked(_ context.Context, channel, version string) error {
	if !stub.checked.Load() || channel != "stable" || version != "1.2.3" {
		return errors.New("not explicitly checked")
	}
	return nil
}

func (stub *checkedUpdateHTTPStub) Rollback(context.Context) error {
	if !stub.rollbackClaimed.compareAndSwap(false, true) {
		return errors.New("rollback outcome requires verification")
	}
	return nil
}

func TestUpdateApplyRequiresExactCheckedCandidateAndReturnsHandoff202(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	updates := &checkedUpdateHTTPStub{applied: make(chan [2]string, 1)}
	server := httptest.NewServer(New(Config{
		Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), Updates: updates,
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	response := postJSON(t, client, server.URL+"/api/v1/update/apply", map[string]string{"channel": "stable", "version": "1.2.3"}, login.CSRFToken)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unchecked update Apply = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/update/check", map[string]string{"channel": "beta"}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("beta Check without version = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/update/check", map[string]string{"channel": "stable"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stable Check = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/update/apply", map[string]string{"channel": "stable", "version": "1.2.4"}, login.CSRFToken)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("wrong checked update Apply = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/update/apply", map[string]string{"channel": "stable", "version": "1.2.3"}, login.CSRFToken)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("checked update Apply = %d %s", response.StatusCode, readBody(response))
	}
	var accepted struct {
		Accepted bool   `json:"accepted"`
		State    string `json:"state"`
	}
	decodeResponse(t, response, &accepted)
	if !accepted.Accepted || accepted.State != "update-attempt-started" {
		t.Fatalf("checked update response = %+v", accepted)
	}
	select {
	case applied := <-updates.applied:
		if applied != [2]string{"stable", "1.2.3"} {
			t.Fatalf("applied candidate = %v", applied)
		}
	default:
		t.Fatal("checked update did not reach ApplyChecked")
	}

	response = postJSON(t, client, server.URL+"/api/v1/update/apply", map[string]string{"channel": "stable", "version": "1.2.3"}, login.CSRFToken)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("replayed checked update Apply = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	select {
	case applied := <-updates.applied:
		t.Fatalf("replayed checked candidate reached ApplyChecked: %v", applied)
	default:
	}

	response = postJSON(t, client, server.URL+"/api/v1/update/rollback", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("rollback = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/update/rollback", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("replayed rollback = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
}

var _ PanelListenerService = (*panelListenerHTTPStub)(nil)
var _ panelupdate.Service = (*checkedUpdateHTTPStub)(nil)
