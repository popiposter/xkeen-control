package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nativequality"
)

type settingsQualityStub struct {
	qualityStub
	store *nativequality.SettingsStore
}

func (s *settingsQualityStub) MaintenanceSettings() nativequality.Settings { return s.store.Get() }
func (s *settingsQualityStub) SetMaintenanceSettings(v nativequality.Settings) (nativequality.Settings, error) {
	return s.store.Set(v)
}

func TestMaintenanceSettingsRequireSessionCSRFAndExactFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := setHTTPTestPassword(p, []byte("synthetic-settings-password")); err != nil {
		t.Fatal(err)
	}
	stub := &settingsQualityStub{store: nativequality.NewSettingsStore(filepath.Join(t.TempDir(), "state", "maintenance.json"))}
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: p}), NativeQuality: stub}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	url := server.URL + "/api/v1/performance/maintenance"
	if r, err := client.Get(url); err != nil || r.StatusCode != 401 {
		t.Fatal("unauthenticated read", err)
	}
	r := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-settings-password"}, "")
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, r, &login)
	var view maintenanceSettingsView
	r, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, r, &view)
	if view.Settings != nativequality.DefaultSettings() || view.Bounds["poolSize"] != [2]int{2, 6} {
		t.Fatalf("defaults view %+v", view)
	}
	changed := map[string]any{"enabled": true, "window": "03:30", "refreshSubscriptions": true, "review": true, "updateGeodata": false,
		"autoApply": false, "speedCandidates": 12, "poolSize": 4, "recovery": true, "report": false}
	if r := postJSON(t, client, url, changed, ""); r.StatusCode != 403 {
		t.Fatal("missing CSRF accepted", r.StatusCode)
	}
	send := func(body string) int {
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		req.Header.Set("X-CSRF-Token", login.CSRFToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	const rest = `"refreshSubscriptions":true,"review":true,"updateGeodata":true,"autoApply":true,"recovery":true,"report":true`
	for _, body := range []string{
		`{"enabled":true}`,
		`{"enabled":"yes","window":"03:30","speedCandidates":12,"poolSize":4,` + rest + `}`,
		`{"enabled":true,"window":"25:00","speedCandidates":12,"poolSize":4,` + rest + `}`,
		`{"enabled":true,"window":"03:30","speedCandidates":3,"poolSize":4,` + rest + `}`,
		`{"enabled":true,"window":"03:30","speedCandidates":12,"poolSize":7,` + rest + `}`,
		`{"enabled":true,"window":"03:30","speedCandidates":12,"poolSize":4,"extra":1,` + rest + `}`,
	} {
		if code := send(body); code != 400 {
			t.Fatal("accepted", body, code)
		}
	}
	if stub.store.Get() != nativequality.DefaultSettings() {
		t.Fatal("a rejected body changed the settings")
	}
	r = postJSON(t, client, url, changed, login.CSRFToken)
	decodeResponse(t, r, &view)
	want := nativequality.Settings{Enabled: true, Window: "03:30", RefreshSubscriptions: true, Review: true, SpeedCandidates: 12, PoolSize: 4, Recovery: true}
	if view.Settings != want || stub.store.Get() != want {
		t.Fatalf("saved %+v", view.Settings)
	}
	if reread := nativequality.NewSettingsStore(stub.store.Path).Get(); reread != want {
		t.Fatal("settings did not persist", reread)
	}
}
