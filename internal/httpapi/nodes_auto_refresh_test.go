package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nodes"
	controlruntime "github.com/popiposter/xkeen-control/internal/runtime"
)

func TestNodesEndpointProjectsBoundedAutomaticRefreshStatusAndDisabledImmediately(t *testing.T) {
	dir := t.TempDir()
	passwordPath := filepath.Join(dir, "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	registry := syntheticSubscriptionHTTPRegistry(t)
	store := nodes.Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	manager := nodes.NewManager(nodes.Config{Store: store, Transaction: nodes.Transaction{Store: store, ActiveOutboundsPath: filepath.Join(dir, "xray", "04_outbounds.json")}})
	manager.SetAutoRefreshStatusProvider(func() map[string]nodes.AutoRefreshStatus {
		return map[string]nodes.AutoRefreshStatus{
			"sub-12345678": {
				State: "deferred", LastAttemptAt: "2026-09-17T12:00:00Z", LastSuccessAt: "2026-09-17T11:00:00Z",
				NextRunAt: "2026-09-17T12:05:00Z", LastResult: "noop", ErrorCode: "authority-busy",
			},
		}
	})
	collector := controlruntime.NewCollector("test", time.Now().UTC(), controlruntime.Dependencies{
		Xray: httpFakeXray{}, Xkeen: httpFakeXkeen{}, Config: httpFakeConfig{}, OutboundTags: func(string) ([]string, error) { return nil, nil },
	})
	server := httptest.NewServer(New(Config{Collector: collector, Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), Nodes: manager}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	login := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-panel-password"}, "")
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, login, &session)
	response, err := client.Get(server.URL + "/api/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("nodes status = %d %s", response.StatusCode, body)
	}
	var payload struct {
		Subscriptions []nodes.PublicSubscription `json:"subscriptions"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Subscriptions) != 1 || payload.Subscriptions[0].AutoRefresh == nil {
		t.Fatalf("automatic status projection = %+v", payload.Subscriptions)
	}
	status := payload.Subscriptions[0].AutoRefresh
	if status.State != "deferred" || status.NextRunAt == "" || status.LastResult != "noop" || status.ErrorCode != "authority-busy" {
		t.Fatalf("automatic status = %+v", status)
	}
	if strings.Contains(body, "subscription.example") || strings.Contains(body, "replacement-token") || strings.Contains(body, "11111111-1111-4111-8111-111111111111") {
		t.Fatalf("nodes response leaked provider material: %s", body)
	}

	registry.Subscriptions[0].Enabled = false
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(server.URL + "/api/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	body = readBody(response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("disabled nodes status = %d %s", response.StatusCode, body)
	}
	payload = struct {
		Subscriptions []nodes.PublicSubscription `json:"subscriptions"`
	}{}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Subscriptions) != 1 || payload.Subscriptions[0].AutoRefresh == nil {
		t.Fatalf("disabled automatic status projection = %+v", payload.Subscriptions)
	}
	status = payload.Subscriptions[0].AutoRefresh
	if status.State != "disabled" || status.LastAttemptAt != "" || status.LastSuccessAt != "" || status.NextRunAt != "" || status.LastResult != "" || status.ErrorCode != "" {
		t.Fatalf("disabled automatic status retained stale participation: %+v", status)
	}
}
