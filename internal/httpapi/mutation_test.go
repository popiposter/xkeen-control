package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nodes"
	controlruntime "github.com/popiposter/xkeen-control/internal/runtime"
)

func TestMutationRoutesRequireCSRFAndReturnSanitizedPreview(t *testing.T) {
	dir := t.TempDir()
	passwordPath := filepath.Join(dir, "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	store := nodes.Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	manager := nodes.NewManager(nodes.Config{Store: store, Transaction: nodes.Transaction{
		Store: store, ActiveOutboundsPath: filepath.Join(dir, "xray", "04_outbounds.json"), PreviousDir: filepath.Join(dir, "previous"),
	}})
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
	if session.CSRFToken == "" {
		t.Fatal("missing CSRF token")
	}
	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/nodes/import/preview", map[string]string{"profiles": syntheticHTTPProfile}, "")
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("mutation without CSRF = %d", withoutCSRF.StatusCode)
	}
	withoutCSRF.Body.Close()
	previewResponse := postJSON(t, client, server.URL+"/api/v1/nodes/import/preview", map[string]string{"profiles": syntheticHTTPProfile}, session.CSRFToken)
	body := readBody(previewResponse)
	if previewResponse.StatusCode != http.StatusOK || strings.Contains(body, "11111111-1111-4111-8111-111111111111") || strings.Contains(body, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") || strings.Contains(body, "abcd") {
		t.Fatalf("preview response = %d %s", previewResponse.StatusCode, body)
	}
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("API preview persisted registry")
	}
	var preview nodes.Preview
	if err := json.Unmarshal([]byte(body), &preview); err != nil || preview.Token == "" {
		t.Fatalf("preview decode = %v", err)
	}
	apply := postJSON(t, client, server.URL+"/api/v1/node-changes/apply", map[string]any{"previewToken": preview.Token}, session.CSRFToken)
	applyBody := readBody(apply)
	if apply.StatusCode != http.StatusOK || strings.Contains(applyBody, "11111111-1111-4111-8111-111111111111") || strings.Contains(applyBody, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") || strings.Contains(applyBody, "abcd") || !strings.Contains(applyBody, "edge.example.com:443") {
		t.Fatalf("apply response = %d %s", apply.StatusCode, applyBody)
	}
	cancelPreviewResponse := postJSON(t, client, server.URL+"/api/v1/nodes/import/preview", map[string]string{"profiles": syntheticHTTPProfile}, session.CSRFToken)
	var cancelPreview nodes.Preview
	decodeResponse(t, cancelPreviewResponse, &cancelPreview)
	cancel := postJSON(t, client, server.URL+"/api/v1/node-changes/cancel", map[string]string{"previewToken": cancelPreview.Token}, session.CSRFToken)
	if cancel.StatusCode != http.StatusOK {
		t.Fatalf("cancel response = %d %s", cancel.StatusCode, readBody(cancel))
	}
	cancel.Body.Close()
	canceledApply := postJSON(t, client, server.URL+"/api/v1/node-changes/apply", map[string]any{"previewToken": cancelPreview.Token}, session.CSRFToken)
	if canceledApply.StatusCode != http.StatusConflict {
		t.Fatalf("canceled preview apply = %d %s", canceledApply.StatusCode, readBody(canceledApply))
	}
	canceledApply.Body.Close()
}

func TestBatchMutationRoutesAreStrictAtomicAndKeepSubscriptionRecords(t *testing.T) {
	dir := t.TempDir()
	passwordPath := filepath.Join(dir, "password.bcrypt")
	if err := auth.SetPassword(passwordPath, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	registry := syntheticBatchHTTPRegistry(t)
	store := nodes.Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	manager := nodes.NewManager(nodes.Config{Store: store, Transaction: nodes.Transaction{
		Store: store, ActiveOutboundsPath: filepath.Join(dir, "xray", "04_outbounds.json"), PreviousDir: filepath.Join(dir, "previous"),
	}})
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: passwordPath}), Nodes: manager}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	login := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-panel-password"}, "")
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, login, &session)

	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/nodes/batch/state/preview", map[string]any{"nodeIds": []string{"node-11111111"}, "enabled": false}, "")
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("batch mutation without CSRF = %d", withoutCSRF.StatusCode)
	}
	withoutCSRF.Body.Close()

	unknownField := postRawJSON(t, client, server.URL+"/api/v1/nodes/batch/state/preview", `{"nodeIds":["node-11111111"],"enabled":false,"extra":true}`, session.CSRFToken)
	if unknownField.StatusCode != http.StatusBadRequest || strings.Contains(readBody(unknownField), "edge.example.com") {
		t.Fatalf("batch unknown field = %d", unknownField.StatusCode)
	}
	trailing := postRawJSON(t, client, server.URL+"/api/v1/nodes/batch/state/preview", `{"nodeIds":["node-11111111"],"enabled":false}{}`, session.CSRFToken)
	trailingBody := readBody(trailing)
	if trailing.StatusCode != http.StatusBadRequest {
		t.Fatalf("batch trailing JSON = %d %s", trailing.StatusCode, trailingBody)
	}

	invalid := postJSON(t, client, server.URL+"/api/v1/nodes/batch/state/preview", map[string]any{"nodeIds": []string{"node-99999999"}, "enabled": false}, session.CSRFToken)
	invalidBody := readBody(invalid)
	if invalid.StatusCode != http.StatusBadRequest || invalidBody != `{"error":"node batch rejected"}`+"\n" {
		t.Fatalf("batch invalid selection = %d %s", invalid.StatusCode, invalidBody)
	}

	previewResponse := postJSON(t, client, server.URL+"/api/v1/nodes/batch/state/preview", map[string]any{"nodeIds": []string{"node-33333333", "node-11111111"}, "enabled": false}, session.CSRFToken)
	var preview nodes.Preview
	decodeResponse(t, previewResponse, &preview)
	if previewResponse.StatusCode != http.StatusOK || preview.Operation != "batch-disable" || len(preview.Changes) != 2 || preview.Token == "" {
		t.Fatalf("batch state preview = %d %+v", previewResponse.StatusCode, preview)
	}
	apply := postJSON(t, client, server.URL+"/api/v1/node-changes/apply", map[string]any{"previewToken": preview.Token}, session.CSRFToken)
	applyBody := readBody(apply)
	if apply.StatusCode != http.StatusOK || strings.Contains(applyBody, "11111111-1111-4111-8111-111111111111") || strings.Contains(applyBody, "subscription.example/token") {
		t.Fatalf("batch state apply = %d %s", apply.StatusCode, applyBody)
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if updated.Nodes[0].Enabled || updated.Nodes[2].Enabled || updated.Subscriptions[0].Enabled != true {
		t.Fatalf("batch state applied outside selected set: %+v", updated)
	}

	removeResponse := postJSON(t, client, server.URL+"/api/v1/nodes/batch/remove/preview", map[string]any{"nodeIds": []string{"node-11111111", "node-33333333"}}, session.CSRFToken)
	var removePreview nodes.Preview
	decodeResponse(t, removeResponse, &removePreview)
	if removeResponse.StatusCode != http.StatusOK || removePreview.Operation != "batch-remove" || len(removePreview.Changes) != 2 {
		t.Fatalf("batch remove preview = %d %+v", removeResponse.StatusCode, removePreview)
	}
	removeApply := postJSON(t, client, server.URL+"/api/v1/node-changes/apply", map[string]any{"previewToken": removePreview.Token}, session.CSRFToken)
	removeApplyBody := readBody(removeApply)
	if removeApply.StatusCode != http.StatusOK {
		t.Fatalf("batch remove apply = %d %s", removeApply.StatusCode, removeApplyBody)
	}
	updated, err = store.Load()
	if err != nil || len(updated.Subscriptions) != 1 || len(updated.Nodes) != 1 || updated.Nodes[0].ID != "node-22222222" {
		t.Fatalf("batch remove deleted unrelated records: %+v, %v", updated, err)
	}

	applyWithExtra := postRawJSON(t, client, server.URL+"/api/v1/node-changes/apply", `{"previewToken":"opaque","nodeIds":["node-22222222"]}`, session.CSRFToken)
	if applyWithExtra.StatusCode != http.StatusBadRequest {
		t.Fatalf("apply accepted non-token payload = %d %s", applyWithExtra.StatusCode, readBody(applyWithExtra))
	}
}

func syntheticBatchHTTPRegistry(t *testing.T) nodes.Registry {
	t.Helper()
	parsed, err := nodes.ParseProfile(syntheticHTTPProfile)
	if err != nil {
		t.Fatal(err)
	}
	manualOne, err := nodes.NewNodeWithID(parsed.VLESS, "Manual one", nodes.Source{Type: "manual"}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	manualTwo, err := nodes.NewNodeWithID(parsed.VLESS, "Manual two", nodes.Source{Type: "manual"}, "node-22222222")
	if err != nil {
		t.Fatal(err)
	}
	manualTwo.Enabled = false
	subscriptionNode, err := nodes.NewNodeWithID(parsed.VLESS, "Provider node", nodes.Source{Type: "subscription", SubscriptionID: "sub-12345678"}, "node-33333333")
	if err != nil {
		t.Fatal(err)
	}
	return nodes.Registry{
		SchemaVersion: nodes.SchemaVersion,
		Nodes:         []nodes.Node{manualOne, manualTwo, subscriptionNode},
		Subscriptions: []nodes.Subscription{{ID: "sub-12345678", Name: "Provider", URL: "https://subscription.example/token", Enabled: true}},
	}
}

func postRawJSON(t *testing.T, client *http.Client, target, body, csrf string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
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

const syntheticHTTPProfile = "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=reality&sni=front.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&type=tcp#Synthetic"
