package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/components"
	"github.com/popiposter/xkeen-control/internal/httpapi"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

func TestListenAddressAllowsOnlyLoopbackOrExactPrivateLAN(t *testing.T) {
	t.Setenv("XKEEN_CONTROL_LISTEN", "127.0.0.1:8787")
	if got, err := listenAddressFromEnv(); err != nil || got != "127.0.0.1:8787" {
		t.Fatalf("loopback address = %q, %v", got, err)
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "0.0.0.0:8787")
	if _, err := listenAddressFromEnv(); err == nil {
		t.Fatal("wildcard listen address accepted")
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "192.168.1.1:8787")
	if got, err := listenAddressFromEnv(); err != nil || got != "192.168.1.1:8787" {
		t.Fatalf("private LAN address = %q, %v", got, err)
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "8.8.8.8:8787")
	if _, err := listenAddressFromEnv(); err == nil {
		t.Fatal("public listen address accepted")
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "not-an-address")
	if _, err := listenAddressFromEnv(); err == nil {
		t.Fatal("malformed listen address accepted")
	}
}

func TestHTTPWriteWindowExceedsNodeTransactionBudget(t *testing.T) {
	if httpWriteTimeout <= nodes.DefaultTransactionTimeout+nodes.DefaultApplyGateWaitTimeout {
		t.Fatalf("HTTP write timeout %s does not exceed gate plus transaction budget %s", httpWriteTimeout, nodes.DefaultTransactionTimeout+nodes.DefaultApplyGateWaitTimeout)
	}
	if components.DefaultMutationOperationTimeout != components.DefaultXKeenTransactionTimeout+components.DefaultXKeenAuthorityWaitTimeout {
		t.Fatalf("component operation budget = %s, want XKeen transaction plus authority budget %s", components.DefaultMutationOperationTimeout, components.DefaultXKeenTransactionTimeout+components.DefaultXKeenAuthorityWaitTimeout)
	}
	if components.DefaultMutationRecoveryTimeout != max(components.DefaultXrayRollbackTimeout, components.DefaultGeodataRollbackTimeout, components.DefaultXKeenRollbackTimeout) {
		t.Fatalf("component recovery budget = %s, want largest rollback budget", components.DefaultMutationRecoveryTimeout)
	}
	expected := componentHTTPWriteWindow(components.DefaultMutationWaitTimeout, components.DefaultMutationOperationTimeout, components.DefaultMutationRecoveryTimeout, componentMutationResponseGrace)
	if httpWriteTimeout != expected {
		t.Fatalf("HTTP write window = %s, want admission + operation + recovery + response grace %s", httpWriteTimeout, expected)
	}
}

type lateRecoveryComponentMutationStub struct {
	operation  time.Duration
	recovery   time.Duration
	postCommit atomic.Bool
	recovered  atomic.Bool
}

func (stub *lateRecoveryComponentMutationStub) Preview(_ context.Context, _ string, request components.MutationRequest) (components.MutationPreview, error) {
	return components.MutationPreview{
		SchemaVersion: components.MutationSchemaVersion,
		PreviewToken:  "scaled-late-recovery-preview-token",
		Component:     request.Component,
		Operation:     request.Operation,
		Channel:       request.Channel,
		ExpiresAt:     time.Now().UTC().Add(time.Minute),
	}, nil
}

func (stub *lateRecoveryComponentMutationStub) Apply(context.Context, string, string) (components.MutationResult, error) {
	time.Sleep(stub.operation)
	stub.postCommit.Store(true)
	time.Sleep(stub.recovery)
	stub.recovered.Store(true)
	return components.MutationResult{}, components.ErrMutationTransactionFailed
}

func (*lateRecoveryComponentMutationStub) Rollback(context.Context, string, string) (components.MutationResult, error) {
	return components.MutationResult{}, components.ErrMutationRollbackUnproven
}

func (*lateRecoveryComponentMutationStub) Cancel(string, string) {}
func (*lateRecoveryComponentMutationStub) Invalidate(string)     {}
func (*lateRecoveryComponentMutationStub) InvalidateAll()        {}

func TestComponentWriteWindowPreservesLateRecoveryHTTPResponse(t *testing.T) {
	const (
		admission = 20 * time.Millisecond
		operation = 100 * time.Millisecond
		recovery  = 200 * time.Millisecond
		grace     = 100 * time.Millisecond
	)
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	mutations := &lateRecoveryComponentMutationStub{operation: admission + operation, recovery: recovery}
	handler := httpapi.New(httpapi.Config{
		Auth:               auth.NewManager(auth.Config{HashPath: passwordPath}),
		ComponentMutations: mutations,
	})
	server := httptest.NewUnstartedServer(handler)
	// Configure the bounded response window before starting any connection
	// goroutine; a live http.Server configuration must not be mutated.
	server.Config.WriteTimeout = componentHTTPWriteWindow(admission, operation, recovery, grace)
	server.Start()
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	var err error
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Exercise the real login handler in-process so bcrypt stays outside the
	// scaled network write window, including under the race detector. Preview
	// and Apply below still use real HTTP with the resulting session and CSRF.
	loginRequest := httptest.NewRequest(http.MethodPost, server.URL+"/api/v1/session/login", strings.NewReader(`{"password":"synthetic-control-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	loginResponse := loginRecorder.Result()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readMainBody(loginResponse))
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&login); err != nil {
		loginResponse.Body.Close()
		t.Fatal(err)
	}
	loginResponse.Body.Close()
	if login.CSRFToken == "" {
		t.Fatal("login did not return csrf token")
	}
	client.Jar.SetCookies(loginRequest.URL, loginResponse.Cookies())

	preview := postMainJSON(t, client, server.URL+"/api/v1/components/preview", map[string]string{"component": "xray", "operation": "update", "channel": "stable"}, login.CSRFToken)
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview = %d %s", preview.StatusCode, readMainBody(preview))
	}
	preview.Body.Close()

	response := postMainJSON(t, client, server.URL+"/api/v1/components/apply", map[string]string{"previewToken": "scaled-late-recovery-preview-token"}, login.CSRFToken)
	contents := readMainBody(response)
	if response.StatusCode != http.StatusInternalServerError || !strings.Contains(contents, "previous generation restored") {
		t.Fatalf("late recovered failure = %d %s", response.StatusCode, contents)
	}
	if !mutations.postCommit.Load() || !mutations.recovered.Load() {
		t.Fatalf("late recovery stages = post-commit:%v recovered:%v", mutations.postCommit.Load(), mutations.recovered.Load())
	}
}

func postMainJSON(t *testing.T, client *http.Client, target string, value any, csrf string) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		request.Header.Set(auth.CSRFHeader, csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readMainBody(response *http.Response) string {
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body)
}

var _ httpapi.ComponentMutationService = (*lateRecoveryComponentMutationStub)(nil)
