package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/dnsobservatory"
)

type httpDNSObservatoryStub struct {
	projection         dnsobservatory.Projection
	preview            dnsobservatory.Preview
	apply              dnsobservatory.ApplyResult
	previewBinding     string
	previewDNS         appliance.DNSSettings
	previewObservatory appliance.ObservatorySettings
	applyBinding       string
	applyToken         string
	cancelBinding      string
	cancelToken        string
	invalidateBinding  string
	invalidateAll      int
}

func (stub *httpDNSObservatoryStub) Read(context.Context) (dnsobservatory.Projection, error) {
	return stub.projection, nil
}

func (stub *httpDNSObservatoryStub) Preview(_ context.Context, binding string, dns appliance.DNSSettings, observatory appliance.ObservatorySettings) (dnsobservatory.Preview, error) {
	stub.previewBinding = binding
	stub.previewDNS = dns
	stub.previewObservatory = observatory
	return stub.preview, nil
}

func (stub *httpDNSObservatoryStub) Apply(_ context.Context, binding, token string) (dnsobservatory.ApplyResult, error) {
	stub.applyBinding = binding
	stub.applyToken = token
	return stub.apply, nil
}

func (stub *httpDNSObservatoryStub) Cancel(binding, token string) {
	stub.cancelBinding = binding
	stub.cancelToken = token
}

func (stub *httpDNSObservatoryStub) Invalidate(binding string) { stub.invalidateBinding = binding }
func (stub *httpDNSObservatoryStub) InvalidateAll()            { stub.invalidateAll++ }

func TestDNSObservatoryHTTPIsTypedAuthenticatedCSRFBoundAndSessionInvalidated(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	const password = "synthetic-control-password"
	if err := auth.SetPassword(hashPath, []byte(password)); err != nil {
		t.Fatal(err)
	}
	stub := &httpDNSObservatoryStub{
		projection: dnsobservatory.Projection{
			Editability:   dnsobservatory.EditabilityEditable,
			SchemaVersion: appliance.SchemaVersion,
			DNS: dnsobservatory.DNSProjection{
				DNSSettings:     appliance.DNSSettings{ProxyResolverIDs: []string{"resolver-a"}, FallbackMode: appliance.FallbackModeSystem, CacheEnabled: true, ServeStale: true, StaleTTLSeconds: 3600, ParallelQueries: true},
				ResolverCatalog: []appliance.DNSResolverOption{{ID: "resolver-a", Label: "Proxy resolver 1"}},
				Locked:          dnsobservatory.LockedDNSFacts{QueryStrategy: "UseIPv4", LeakPreventionEnabled: true, SystemFallbackPresent: true},
			},
			Observatory: dnsobservatory.ObservatoryProjection{ProbeIntervalMinutes: 5, MinIntervalMinutes: 1, MaxIntervalMinutes: 5},
		},
		preview: dnsobservatory.Preview{Token: "synthetic-dns-observatory-token", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		apply:   dnsobservatory.ApplyResult{Classification: "applied"},
	}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: hashPath}), DNSObservatory: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	response, err := client.Get(server.URL + "/api/v1/appliance/dns-observatory")
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated DNS/Observatory GET = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()
	loginResponse := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, loginResponse, &login)

	response, err = client.Get(server.URL + "/api/v1/appliance/dns-observatory")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("DNS/Observatory GET = %d, %v", response.StatusCode, err)
	}
	body := readBody(response)
	if strings.Contains(body, "https://") || strings.Contains(body, "domain:") || strings.Contains(body, "dns-query") {
		t.Fatalf("DNS/Observatory projection contains raw endpoint/domain data: %s", body)
	}

	validBody := map[string]any{
		"dns": map[string]any{
			"proxyResolverIds": []string{"resolver-a"}, "fallbackMode": "system", "cacheEnabled": true,
			"serveStale": true, "staleTTLSeconds": 3600, "parallelQueries": true,
		},
		"observatory": map[string]any{"probeIntervalMinutes": 5},
	}
	response = postJSON(t, client, server.URL+"/api/v1/appliance/dns-observatory/preview", validBody, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("DNS/Observatory preview without CSRF = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/appliance/dns-observatory/preview", map[string]any{"dns": validBody["dns"], "observatory": validBody["observatory"], "unexpected": true}, login.CSRFToken)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("DNS/Observatory unknown field = %d", response.StatusCode)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/appliance/dns-observatory/preview", validBody, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.previewBinding != login.CSRFToken || stub.previewDNS.ProxyResolverIDs[0] != "resolver-a" || stub.previewObservatory.ProbeIntervalMinutes != 5 {
		t.Fatalf("DNS/Observatory preview = %d binding=%q dns=%+v observatory=%+v body=%s", response.StatusCode, stub.previewBinding, stub.previewDNS, stub.previewObservatory, readBody(response))
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/appliance/dns-observatory/apply", map[string]string{"previewToken": "synthetic-dns-observatory-token"}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("DNS/Observatory apply without CSRF = %d", response.StatusCode)
	}
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/appliance/dns-observatory/apply", map[string]string{"previewToken": "synthetic-dns-observatory-token"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.applyBinding != login.CSRFToken || stub.applyToken != "synthetic-dns-observatory-token" {
		t.Fatalf("DNS/Observatory apply = %d binding=%q token=%q body=%s", response.StatusCode, stub.applyBinding, stub.applyToken, readBody(response))
	}
	response.Body.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/appliance/dns-observatory/preview", strings.NewReader(`{"dns":{},"observatory":{}} {}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(auth.CSRFHeader, login.CSRFToken)
	response, err = client.Do(request)
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("DNS/Observatory trailing JSON = %d, %v", response.StatusCode, err)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/session/logout", map[string]string{}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.invalidateBinding != login.CSRFToken {
		t.Fatalf("DNS/Observatory logout invalidation = %d binding=%q body=%s", response.StatusCode, stub.invalidateBinding, readBody(response))
	}
	response.Body.Close()

	loginResponse = postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": password}, "")
	decodeResponse(t, loginResponse, &login)
	response = postJSON(t, client, server.URL+"/api/v1/session/password", map[string]string{"newPassword": "synthetic-new-control-password"}, login.CSRFToken)
	if response.StatusCode != http.StatusOK || stub.invalidateAll != 1 {
		t.Fatalf("DNS/Observatory password invalidation = %d count=%d body=%s", response.StatusCode, stub.invalidateAll, readBody(response))
	}
}
