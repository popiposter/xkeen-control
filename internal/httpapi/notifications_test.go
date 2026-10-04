package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/notifications"
)

func TestNotificationsExactRoutesAndSecretlessRoundtrip(t *testing.T) {
	dir := t.TempDir()
	secretDir := filepath.Join(dir, "secrets")
	os.Mkdir(secretDir, 0o700)
	path := filepath.Join(secretDir, "notifications.json")
	password := filepath.Join(dir, "password.bcrypt")
	if err := setHTTPTestPassword(password, []byte("synthetic-control-password")); err != nil {
		t.Fatal(err)
	}
	service := notifications.NewServiceForTest(path)
	server := httptest.NewServer(New(Config{Auth: auth.NewManager(auth.Config{HashPath: password}), Notifications: service}))
	defer server.Close()
	client := &http.Client{Jar: mustCookieJar(t)}
	response, _ := client.Get(server.URL + "/api/v1/notifications")
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated GET")
	}
	response.Body.Close()
	login := postJSON(t, client, server.URL+"/api/v1/session/login", map[string]string{"password": "synthetic-control-password"}, "")
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeResponse(t, login, &session)
	const token = "123456:synthetic_notification_token_sentinel"
	const chat = "-1234567890123"
	request := func(path, body, csrf, contentType, origin string) int {
		t.Helper()
		r, _ := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("X-CSRF-Token", csrf)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		for _, secret := range []string{token, chat, "botToken", "chatId"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("notification API leaked secret")
			}
		}
		return response.StatusCode
	}
	const configure = `{"botToken":"` + token + `","chatId":"` + chat + `"}`
	if request("/api/v1/notifications/configure", configure, "", "application/json", server.URL) != 403 {
		t.Fatal("CSRF bypass")
	}
	if request("/api/v1/notifications/configure", configure, session.CSRFToken, "application/json", "https://other.invalid") != 403 {
		t.Fatal("origin bypass")
	}
	for _, body := range []string{`null`, `{}`, `{"botToken":"x"}`, `{"botToken":"x","botToken":"y","chatId":"1"}`, configure + `{}`, strings.Replace(configure, `"botToken"`, `"BotToken"`, 1), strings.Replace(configure, `"chatId"`, `"unknown"`, 1), `{"botToken":null,"chatId":"1"}`, `{"botToken":1,"chatId":"1"}`} {
		if request("/api/v1/notifications/configure", body, session.CSRFToken, "application/json", server.URL) != 400 {
			t.Fatal("nonexact body accepted")
		}
	}
	if request("/api/v1/notifications/configure", strings.Repeat(" ", 4097), session.CSRFToken, "application/json", server.URL) != 413 {
		t.Fatal("oversize accepted")
	}
	if request("/api/v1/notifications/configure", configure, session.CSRFToken, "text/plain", server.URL) != 415 {
		t.Fatal("media type accepted")
	}
	if request("/api/v1/notifications/configure?url=https://other.invalid", configure, session.CSRFToken, "application/json", server.URL) != 400 {
		t.Fatal("query accepted")
	}
	if request("/api/v1/notifications/test", `{}`, session.CSRFToken, "application/json", server.URL) != 503 {
		t.Fatal("unconfigured test accepted")
	}
	if request("/api/v1/notifications/configure", configure, session.CSRFToken, "application/json", server.URL) != 200 || service.Status().Enabled {
		t.Fatal("configure enabled delivery")
	}
	for _, csrf := range []string{"", session.CSRFToken} {
		code := request("/api/v1/notifications/control", `{"enabled":true,"allowedUserId":"12345"}`, csrf, "application/json", server.URL)
		if csrf == "" && code != 403 || csrf != "" && code != 200 {
			t.Fatal("control auth/CSRF", code)
		}
	}
	if request("/api/v1/notifications/control", `{"enabled":true,"allowedUserId":"12345","command":"reboot"}`, session.CSRFToken, "application/json", server.URL) != 400 {
		t.Fatal("generic command accepted")
	}
	if request("/api/v1/notifications/control", `{"enabled":true,"allowedUserId":"12345"}`, session.CSRFToken, "application/json", "https://other.invalid") != 403 {
		t.Fatal("foreign bot control origin")
	}
	response, _ = client.Get(server.URL + "/api/v1/notifications")
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	for _, secret := range []string{token, chat, "botToken", "chatId"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("GET leaked")
		}
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":true,"enabled":false}`, `{"enabled":"true"}`, `{"Enabled":true}`} {
		if request("/api/v1/notifications/enabled", body, session.CSRFToken, "application/json", server.URL) != 400 {
			t.Fatal("invalid enabled request")
		}
	}
	for _, body := range []string{`{"enabled":true}`, `{"enabled":false}`} {
		if request("/api/v1/notifications/enabled", body, session.CSRFToken, "application/json", server.URL) != 200 {
			t.Fatal("enable/disable rejected")
		}
	}
	for _, path := range []string{"/api/v1/notifications/test", "/api/v1/notifications/clear"} {
		if request(path, `{"message":"arbitrary"}`, session.CSRFToken, "application/json", server.URL) != 400 {
			t.Fatal("free form API")
		}
	}
	if request("/api/v1/notifications/clear", `{}`, session.CSRFToken, "application/json", server.URL) != 200 || service.Status().Configured {
		t.Fatal("clear failed")
	}
}
