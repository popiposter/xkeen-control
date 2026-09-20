package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/routingpolicy"
)

type httpRoutingPolicyStub struct {
	projection        routingpolicy.Projection
	preview           routingpolicy.Preview
	apply             routingpolicy.ApplyResult
	previewRules      []appliance.CustomRule
	previewBinding    string
	applyBinding      string
	applyToken        string
	cancelBinding     string
	cancelToken       string
	invalidateBinding string
	invalidateAll     int
}

func (stub *httpRoutingPolicyStub) Read(context.Context) (routingpolicy.Projection, error) {
	return stub.projection, nil
}

func (stub *httpRoutingPolicyStub) Preview(_ context.Context, binding string, rules []appliance.CustomRule) (routingpolicy.Preview, error) {
	stub.previewBinding = binding
	stub.previewRules = rules
	return stub.preview, nil
}

func (stub *httpRoutingPolicyStub) Apply(_ context.Context, binding, token string) (routingpolicy.ApplyResult, error) {
	stub.applyBinding = binding
	stub.applyToken = token
	return stub.apply, nil
}

func (stub *httpRoutingPolicyStub) Cancel(binding, token string) {
	stub.cancelBinding = binding
	stub.cancelToken = token
}

func (stub *httpRoutingPolicyStub) Invalidate(binding string) { stub.invalidateBinding = binding }
func (stub *httpRoutingPolicyStub) InvalidateAll()            { stub.invalidateAll++ }

func TestRoutingPolicyHTTPIsTypedAuthenticatedCSRFBoundAndSessionInvalidated(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-control-password"
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	stub := &httpRoutingPolicyStub{
		projection: routingpolicy.Projection{Editability: routingpolicy.EditabilityEditable, SchemaVersion: appliance.SchemaVersion, Rules: []appliance.CustomRule{}},
		preview:    routingpolicy.Preview{Token: "synthetic-policy-token", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		apply:      routingpolicy.ApplyResult{Classification: "applied"},
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: hashPath}), Policy: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/api/v1/appliance/policy")
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated policy GET = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	response, err = client.Get(server.URL + "/api/v1/appliance/policy")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("policy GET = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/appliance/policy/preview", map[string]any{"rules": []any{}}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("policy preview without csrf = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/appliance/policy/preview", map[string]any{
		"rules": []map[string]any{{"name": "typed", "action": "direct", "ruleTag": "forbidden"}},
	}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(response), `"code":"invalid-request"`) {
		t.Fatalf("policy unknown-field response = %d", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/appliance/policy/preview", strings.NewReader(`{"rules":[]} {}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(auth.CSRFHeader, login.CSRFToken)
	response, err = client.Do(request)
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("policy trailing JSON response = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()

	validBody := map[string]any{"rules": []map[string]any{{
		"name": "typed", "domains": []string{"domain:example.com"}, "ips": []string{}, "protocols": []string{}, "networks": []string{}, "ports": []map[string]int{}, "action": "proxy",
	}}}
	response = postJSON(t, client, server.URL+"/api/v1/appliance/policy/preview", validBody, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.previewBinding != login.CSRFToken || len(stub.previewRules) != 1 || stub.previewRules[0].Name != "typed" {
		t.Fatalf("policy preview = %d binding=%q rules=%+v body=%s", response.StatusCode, stub.previewBinding, stub.previewRules, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/appliance/policy/apply", map[string]string{"previewToken": "synthetic-policy-token"}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("policy apply without csrf = %d", response.StatusCode)
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/appliance/policy/apply", map[string]string{"previewToken": "synthetic-policy-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.applyBinding != login.CSRFToken || stub.applyToken != "synthetic-policy-token" {
		t.Fatalf("policy apply = %d binding=%q token=%q body=%s", response.StatusCode, stub.applyBinding, stub.applyToken, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.invalidateBinding != login.CSRFToken {
		t.Fatalf("policy logout invalidation = %d binding=%q body=%s", response.StatusCode, stub.invalidateBinding, readBody(response))
	}
	response.Body.Close()

	encoded, _ := json.Marshal(stub.previewRules)
	if strings.Contains(string(encoded), "ruleTag") || strings.Contains(string(encoded), "outboundTag") {
		t.Fatal("test projection unexpectedly contains raw routing fields")
	}
}
