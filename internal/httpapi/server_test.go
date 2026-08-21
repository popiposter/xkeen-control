package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/popiposter/xkeen-control/internal/configview"
	"github.com/popiposter/xkeen-control/internal/nodes"
	controlruntime "github.com/popiposter/xkeen-control/internal/runtime"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

func TestNodeActivationErrorsExposeOnlyConfirmedRecoveryState(t *testing.T) {
	tests := []struct {
		err     error
		message string
	}{
		{err: &nodes.RollbackError{Cause: errors.New("synthetic activation"), Recovery: errors.New("synthetic recovery")}, message: "node activation failed; rollback failed"},
		{err: errors.New("Xray restart failed; previous generation restored"), message: "node activation failed; previous generation restored"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		new(Server).writeNodeOperationError(recorder, test.err)
		if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), test.message) {
			t.Fatalf("activation response = %d %s", recorder.Code, recorder.Body.String())
		}
	}
}

type httpFakeXray struct{}

func (httpFakeXray) Snapshot(context.Context) xrayapi.Snapshot {
	return xrayapi.Snapshot{
		APIReachable: true, RoutingReachable: true, ObservatoryReachable: true,
		Balancer:       xrayapi.BalancerState{NativeSelected: "proxy-main-01"},
		OutboundHealth: []xrayapi.OutboundHealth{{Tag: "proxy-main-01", Alive: true, DelayMS: 42}},
	}
}
func (httpFakeXray) ProbeReachable(context.Context) bool { return true }

type httpFakeXkeen struct{}

func (httpFakeXkeen) Snapshot(context.Context) xkeen.Snapshot {
	return xkeen.Snapshot{XrayRunning: true, XkeenRunning: true, Speed: xkeen.SpeedBalancer{IntervalMin: 1440}, Benchmark: xkeen.Benchmark{InstalledSchedule: "17 4 * * *", ThroughputKBps: map[string]float64{"proxy-main-01": 100}}}
}

type httpFakeConfig struct{}

func (httpFakeConfig) Read(context.Context) configview.Summary {
	return configview.Summary{Available: true, Observatory: configview.ObservatorySummary{ProbeInterval: "5m"}}
}

type benchmarkRequestStub struct{ calls atomic.Int32 }

func (stub *benchmarkRequestStub) TriggerBenchmark() error {
	stub.calls.Add(1)
	return nil
}

type selectionRequestStub struct{ target string }

func (stub *selectionRequestStub) SetManualOverride(_ context.Context, target string) error {
	stub.target = target
	return nil
}

func TestManualOverrideRouteRequiresCSRFAndPersistsTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	selection := &selectionRequestStub{}
	handler := New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Selection: selection})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/selection/override", map[string]string{"target": "proxy-main-01"}, "")
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("manual override without csrf = %d", withoutCSRF.StatusCode)
	}
	withoutCSRF.Body.Close()
	response := postJSON(t, client, server.URL+"/api/v1/selection/override", map[string]string{"target": "proxy-main-01"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || selection.target != "proxy-main-01" {
		t.Fatalf("manual override request = %d target=%q body=%s", response.StatusCode, selection.target, readBody(response))
	}
}

func TestBenchmarkRunRequiresCSRFAndIsSingleFlightRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	benchmark := &benchmarkRequestStub{}
	handler := New(Config{Auth: auth.NewManager(auth.Config{HashPath: path}), Benchmark: benchmark})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readBody(loginResponse))
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/benchmark/run", map[string]string{}, "")
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("benchmark without csrf = %d", withoutCSRF.StatusCode)
	}

	accepted := postJSON(t, client, server.URL+"/api/v1/benchmark/run", map[string]string{}, login.CSRFToken)
	if accepted.StatusCode != http.StatusAccepted || benchmark.calls.Load() != 1 {
		t.Fatalf("benchmark request = %d calls=%d body=%s", accepted.StatusCode, benchmark.calls.Load(), readBody(accepted))
	}
}

func TestServerAuthReadOnlyRoutesAndHealthBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.bcrypt")
	if err := auth.SetPassword(path, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	collector := controlruntime.NewCollector("test", time.Now().UTC(), controlruntime.Dependencies{
		Xray: httpFakeXray{}, Xkeen: httpFakeXkeen{}, Config: httpFakeConfig{},
		OutboundTags: func(string) ([]string, error) { return []string{"proxy-main-01"}, nil },
	})
	handler := New(Config{Collector: collector, Auth: auth.NewManager(auth.Config{HashPath: path})})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Fatalf("healthz = %d %q", response.StatusCode, body)
	}
	if strings.Contains(string(body), "xray") {
		t.Fatal("healthz exposed runtime details")
	}
	if response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}

	response, err = client.Get(server.URL + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	response.Body.Close()

	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.StatusCode, readBody(loginResponse))
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)
	if login.CSRFToken == "" {
		t.Fatal("login did not return CSRF token")
	}

	response, err = client.Get(server.URL + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	var status controlruntime.Status
	decodeResponse(t, response, &status)
	if status.Balancer.NativeSelected != "proxy-main-01" || !status.Xray.APIReachable {
		t.Fatalf("status = %+v", status)
	}

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d %s", response.StatusCode, readBody(response))
	}
	response.Body.Close()
	response, err = client.Get(server.URL + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d", response.StatusCode)
	}
	response.Body.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/session/login", strings.NewReader(`{"password":"synthetic-control-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin login = %d", response.StatusCode)
	}
	response.Body.Close()
}

func mustCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func postJSON(t *testing.T, client *http.Client, target string, value any, csrf string) *http.Response {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(string(contents)))
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

func decodeResponse(t *testing.T, response *http.Response, value any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(value); err != nil {
		t.Fatal(err)
	}
}

func readBody(response *http.Response) string {
	defer response.Body.Close()
	contents, _ := io.ReadAll(response.Body)
	return string(contents)
}
