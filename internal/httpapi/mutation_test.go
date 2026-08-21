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

const syntheticHTTPProfile = "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=reality&sni=front.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&type=tcp#Synthetic"
