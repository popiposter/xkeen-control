package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/components"
)

type componentPolicyHTTPStub struct {
	status components.ComponentPolicyStatus
}

func (stub *componentPolicyHTTPStub) Status() components.ComponentPolicyStatus {
	return stub.status
}

func (stub *componentPolicyHTTPStub) SetPolicy(policy components.ComponentPolicy) (components.ComponentPolicyStatus, error) {
	scheduler := components.SchedulerStatus{State: "disabled", NotificationState: "idle"}
	if policy.Mode == components.ComponentPolicyModeNotify {
		scheduler.Enabled = true
		scheduler.State = "waiting"
	}
	stub.status = components.ComponentPolicyStatus{
		SchemaVersion:       policy.SchemaVersion,
		Mode:                policy.Mode,
		CheckCadenceMinutes: policy.CheckCadenceMinutes,
		Scheduler:           scheduler,
	}
	return stub.status, nil
}

func policyRawPost(t *testing.T, client *http.Client, target, body, contentType, csrf, origin string) *http.Response {
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

func TestComponentPolicyRoutesAreAuthenticatedStrictAndSanitized(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	policy := &componentPolicyHTTPStub{status: components.ComponentPolicyStatus{
		SchemaVersion:       components.ComponentPolicySchemaVersion,
		Mode:                components.ComponentPolicyModeManual,
		CheckCadenceMinutes: components.DefaultComponentPolicyCadenceMinutes,
		Scheduler:           components.SchedulerStatus{State: "disabled", NotificationState: "idle"},
	}}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), ComponentPolicy: policy}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/api/v1/components/policy")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated policy = %d", response.StatusCode)
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

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/components/policy", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin policy GET = %d", response.StatusCode)
	}
	response.Body.Close()

	response, err = client.Get(server.URL + "/api/v1/components/policy")
	if err != nil {
		t.Fatal(err)
	}
	contents := readBody(response)
	if response.StatusCode != http.StatusOK || !strings.Contains(contents, `"mode":"manual"`) || !strings.Contains(contents, `"checkCadenceMinutes":1440`) {
		t.Fatalf("default policy = %d %s", response.StatusCode, contents)
	}
	for _, forbidden := range []string{"/opt/", "password.bcrypt", "https://", "secret"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("policy response contains %q: %s", forbidden, contents)
		}
	}

	response = postJSON(t, client, server.URL+"/api/v1/components/policy", components.ComponentPolicy{SchemaVersion: 1, Mode: components.ComponentPolicyModeNotify, CheckCadenceMinutes: 60}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("policy without csrf = %d", response.StatusCode)
	}
	response.Body.Close()

	validBody := `{"schemaVersion":1,"mode":"notify","checkCadenceMinutes":60}`
	response = policyRawPost(t, client, server.URL+"/api/v1/components/policy", validBody, "application/json; charset=utf-8", login.CSRFToken, "")
	if response.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("parameterized policy content type = %d body=%s", response.StatusCode, readBody(response))
	}

	for _, body := range []string{
		`{"schemaVersion":1,"mode":"notify","checkCadenceMinutes":60,"url":"https://secret.example"}`,
		`{"schemaVersion":1,"mode":"notify","mode":"manual","checkCadenceMinutes":60}`,
		validBody + `{}`,
		`{"schemaVersion":1,"mode":"notify","checkCadenceMinutes":59}`,
	} {
		response = policyRawPost(t, client, server.URL+"/api/v1/components/policy", body, "application/json", login.CSRFToken, "")
		contents = readBody(response)
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(contents, `"code":"invalid-request"`) {
			t.Fatalf("invalid policy body = %d %s", response.StatusCode, contents)
		}
		if strings.Contains(contents, "secret.example") || strings.Contains(contents, "https://") {
			t.Fatalf("invalid policy detail leaked: %s", contents)
		}
	}

	response = policyRawPost(t, client, server.URL+"/api/v1/components/policy?mode=notify", validBody, "application/json", login.CSRFToken, "")
	contents = readBody(response)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(contents, `"code":"invalid-request"`) {
		t.Fatalf("policy query = %d %s", response.StatusCode, contents)
	}

	response = policyRawPost(t, client, server.URL+"/api/v1/components/policy", strings.Repeat("x", maxComponentPolicyBody+1), "application/json", login.CSRFToken, "")
	contents = readBody(response)
	if response.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(contents, `"code":"invalid-request"`) {
		t.Fatalf("oversized policy = %d %s", response.StatusCode, contents)
	}

	response = policyRawPost(t, client, server.URL+"/api/v1/components/policy", validBody, "application/json", login.CSRFToken, "http://evil.example")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin policy POST = %d", response.StatusCode)
	}
	response.Body.Close()

	response = policyRawPost(t, client, server.URL+"/api/v1/components/policy", validBody, "application/json", login.CSRFToken, "")
	contents = readBody(response)
	if response.StatusCode != http.StatusOK || !strings.Contains(contents, `"mode":"notify"`) || !strings.Contains(contents, `"checkCadenceMinutes":60`) {
		t.Fatalf("valid policy POST = %d %s", response.StatusCode, contents)
	}
	var status components.ComponentPolicyStatus
	if err := json.Unmarshal([]byte(contents), &status); err != nil {
		t.Fatal(err)
	}
	if status.Mode != components.ComponentPolicyModeNotify || !status.Scheduler.Enabled || status.Scheduler.State != "waiting" {
		t.Fatalf("saved policy status = %+v", status)
	}

	request, err = http.NewRequest(http.MethodPut, server.URL+"/api/v1/components/policy", strings.NewReader(validBody))
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodGet+", "+http.MethodPost {
		t.Fatalf("PUT policy = %d allow=%q body=%s", response.StatusCode, response.Header.Get("Allow"), readBody(response))
	}
}

func TestComponentPolicyDisabledCheckErrorIsProjected(t *testing.T) {
	passwordPath := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	checker := &componentCheckHTTPStub{err: components.ErrComponentPolicyDisabled}
	server := httptest.NewServer(New(Config{
		Auth:            auth.NewManager(auth.Config{HashPath: passwordPath}),
		ComponentChecks: checker,
	}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	response := postJSON(t, client, server.URL+"/api/v1/components/check", map[string]string{"component": "xray", "channel": "stable"}, login.CSRFToken)
	contents := readBody(response)
	if response.StatusCode != http.StatusConflict || !strings.Contains(contents, `"code":"policy-disabled"`) {
		t.Fatalf("disabled check = %d body=%s", response.StatusCode, contents)
	}
	if strings.Contains(contents, "/opt/") || strings.Contains(contents, "component-policy.json") {
		t.Fatalf("policy check response leaked local detail: %s", contents)
	}
}

var _ ComponentPolicyService = (*components.PolicyManager)(nil)
var _ ComponentPolicyService = (*componentPolicyHTTPStub)(nil)
var _ components.CheckService = (*componentCheckHTTPStub)(nil)
