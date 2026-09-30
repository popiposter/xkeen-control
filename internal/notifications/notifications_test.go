package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fixtureToken = "123456:synthetic_notification_token_sentinel"
const fixtureChat = "-1234567890123"

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureService(t *testing.T) *Service {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return NewServiceForTest(filepath.Join(dir, "notifications.json"))
}
func assertSafe(t *testing.T, value any) {
	t.Helper()
	data, _ := json.Marshal(value)
	for _, secret := range []string{fixtureToken, fixtureChat, "botToken", "chatId", "synthetic-upstream-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("secret-bearing projection")
		}
	}
}
func TestAuthorityRoundtripAndDisabledExplicitTest(t *testing.T) {
	s := fixtureService(t)
	if status := s.Status(); status.AuthorityState != "unconfigured" || status.Configured || status.Enabled {
		t.Fatalf("absent state: %+v", status)
	}
	status, err := s.Configure(fixtureToken, fixtureChat)
	if err != nil || !status.Configured || status.Enabled {
		t.Fatalf("configure: %v %+v", err, status)
	}
	assertSafe(t, status)
	sends := 0
	s.transport.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		sends++
		if r.URL.Scheme != "https" || r.URL.Host != telegramAddress || r.URL.Path != "/bot"+fixtureToken+"/sendMessage" || r.Method != "POST" {
			t.Fatal("wrong transport boundary")
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]string
		json.Unmarshal(data, &body)
		if len(body) != 2 || body["chat_id"] != fixtureChat || body["text"] != TestAlert().text {
			t.Fatal("unexpected message fields")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"text":"synthetic-upstream-secret"}}`)), Header: http.Header{}}, nil
	})
	if err := s.Send(context.Background(), PanelAlert("1.2.3", strings.Repeat("a", 64), time.Now())); err != Error("disabled") {
		t.Fatalf("disabled send: %v", err)
	}
	if sends != 0 {
		t.Fatal("disabled network")
	}
	status, err = s.Test(context.Background())
	if err != nil || sends != 1 || status.Enabled || status.DeliveryState != "delivered" {
		t.Fatalf("test: %v count %d", err, sends)
	}
	assertSafe(t, status)
	status, err = s.SetEnabled(true)
	if err != nil || !status.Enabled {
		t.Fatal("enable failed")
	}
	assertSafe(t, status)
	status, err = s.SetEnabled(false)
	if err != nil || status.Enabled {
		t.Fatal("disable failed")
	}
	info, _ := os.Stat(s.path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatal("unsafe persisted permissions")
	}
	status, err = s.Clear()
	if err != nil || status.Configured || status.Enabled {
		t.Fatal("clear failed")
	}
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clear retained authority")
	}
	if _, err := s.SetEnabled(true); err != Error("unconfigured") {
		t.Fatal("enable without authority")
	}
}

func TestAuthorityMalformedAndUnsafeFailClosedWithoutRewriteOrNetwork(t *testing.T) {
	good, _ := json.Marshal(authority{1, "telegram", true, fixtureToken, fixtureChat})
	fixtures := map[string][]byte{
		"malformed":      []byte(`{"botToken":"` + fixtureToken + `"`),
		"duplicate":      []byte(strings.Replace(string(good), `"enabled":true`, `"enabled":false,"enabled":true`, 1)),
		"unknown":        []byte(strings.Replace(string(good), `"enabled":true`, `"enabled":true,"extra":1`, 1)),
		"case":           []byte(strings.Replace(string(good), `"enabled"`, `"Enabled"`, 1)),
		"trailing":       append(append([]byte{}, good...), []byte(`{}`)...),
		"oversize":       []byte(strings.Repeat(" ", MaxAuthorityBytes+1)),
		"missing":        []byte(`{"schemaVersion":1}`),
		"null":           []byte(strings.Replace(string(good), `"enabled":true`, `"enabled":null`, 1)),
		"wrong-provider": []byte(strings.Replace(string(good), "telegram", "webhook", 1)),
		"url-token":      []byte(strings.Replace(string(good), fixtureToken, "https://unsafe.invalid", 1)),
	}
	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			s := fixtureService(t)
			if os.WriteFile(s.path, data, 0o600) != nil {
				t.Fatal("fixture write")
			}
			s.transport.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("unsafe network"); return nil, nil })
			before, _ := os.ReadFile(s.path)
			assertSafe(t, s.Status())
			if status := s.Status(); status.AuthorityState != "unavailable" || status.Configured || status.Enabled {
				t.Fatal("did not fail closed")
			}
			if s.Send(context.Background(), TestAlert()) == nil {
				t.Fatal("unsafe send accepted")
			}
			after, _ := os.ReadFile(s.path)
			if !bytes.Equal(before, after) {
				t.Fatal("read normalized authority")
			}
		})
	}
	for _, kind := range []string{"symlink", "directory", "permissions", "parent-permissions", "parent-symlink", "wrong-owner", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			if runtime.GOOS == "windows" && kind != "directory" {
				t.Skip("Unix fixture")
			}
			s := fixtureService(t)
			switch kind {
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				os.WriteFile(target, good, 0o600)
				if err := os.Symlink(target, s.path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				os.Mkdir(s.path, 0o700)
			case "permissions":
				os.WriteFile(s.path, good, 0o644)
			case "parent-permissions":
				os.WriteFile(s.path, good, 0o600)
				os.Chmod(filepath.Dir(s.path), 0o755)
			case "parent-symlink":
				actual := t.TempDir()
				os.Chmod(actual, 0o700)
				os.WriteFile(filepath.Join(actual, "notifications.json"), good, 0o600)
				link := filepath.Join(t.TempDir(), "link")
				os.Symlink(actual, link)
				s.path = filepath.Join(link, "notifications.json")
			case "fifo":
				makeFIFO(t, s.path)
			case "wrong-owner":
				os.WriteFile(s.path, good, 0o600)
				if err := os.Chown(s.path, 1, 0); err != nil {
					t.Skip("owner fixture unavailable")
				}
			}
			if s.Status().AuthorityState != "unavailable" {
				t.Fatal("unsafe authority not unavailable")
			}
			if s.Send(context.Background(), TestAlert()) == nil {
				t.Fatal("unsafe authority sent")
			}
		})
	}
}

func TestTelegramDNSConnectBoundary(t *testing.T) {
	unsafe := []string{"0.0.0.0", "10.1.2.3", "100.64.0.1", "127.0.0.1", "169.254.169.254", "172.16.1.1", "192.0.0.9", "192.0.2.1", "192.168.1.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "2001::1", "2002:0808:0808::", "3fff::1", "64:ff9b::a00:1"}
	for _, ip := range unsafe {
		t.Run(ip, func(t *testing.T) {
			calls := 0
			dial := telegramDial(func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP(ip)}}, nil
			}, func(context.Context, string, string) (net.Conn, error) { calls++; return nil, nil })
			if _, err := dial(context.Background(), "tcp", telegramAddress); err != Error("unsafe-destination") || calls != 0 {
				t.Fatal("unsafe answer dialed")
			}
		})
	}
	calls := 0
	dial := telegramDial(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != telegramHost {
			t.Fatal("arbitrary DNS")
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, func(_ context.Context, network, address string) (net.Conn, error) {
		calls++
		if address != "8.8.8.8:443" || network != "tcp" {
			t.Fatal("second DNS lookup")
		}
		return nil, nil
	})
	if _, err := dial(context.Background(), "tcp", telegramAddress); err != nil || calls != 1 {
		t.Fatal("safe dial rejected")
	}
	for _, address := range []string{"api.telegram.org:80", "other.invalid:443", "127.0.0.1:443"} {
		if _, err := dial(context.Background(), "tcp", address); err != Error("unsafe-destination") {
			t.Fatal("arbitrary host")
		}
	}
	if calls != 1 {
		t.Fatal("wrong-host connection")
	}
	dial = telegramDial(func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("synthetic-upstream-secret")
	}, nil)
	if _, err := dial(context.Background(), "tcp", telegramAddress); err != Error("dns-unavailable") {
		t.Fatal("raw DNS error")
	}
}

func TestTelegramTimeoutLimitsRedirectAndSanitizedProviderFailure(t *testing.T) {
	client := newTelegram().client
	transport := client.Transport.(*http.Transport)
	if client.Timeout > 5*time.Second || transport.Proxy != nil || transport.TLSClientConfig.ServerName != telegramHost || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unsafe defaults")
	}
	for _, fixture := range []struct {
		name   string
		status int
		body   string
		err    error
		code   Error
	}{
		{"provider", 400, "synthetic-upstream-secret", nil, "provider-rejected"},
		{"not-ok", 200, `{"ok":false,"description":"synthetic-upstream-secret"}`, nil, "provider-rejected"},
		{"oversize", 200, strings.Repeat("x", maxResponseBytes+1), nil, "provider-rejected"},
		{"malformed", 200, `{`, nil, "provider-rejected"},
		{"native", 0, "", errors.New(fixtureToken + fixtureChat), "transport-failed"},
		{"timeout", 0, "", context.DeadlineExceeded, "timeout"},
		{"unsafe", 0, "", Error("unsafe-destination"), "unsafe-destination"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			s := fixtureService(t)
			s.Configure(fixtureToken, fixtureChat)
			var logs bytes.Buffer
			old := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(old)
			s.transport.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				if fixture.err != nil {
					return nil, fixture.err
				}
				return &http.Response{StatusCode: fixture.status, Body: io.NopCloser(strings.NewReader(fixture.body))}, nil
			})
			_, err := s.Test(context.Background())
			if err != fixture.code {
				t.Fatalf("safe code %v, want %v", err, fixture.code)
			}
			assertSafe(t, s.Status())
			assertSafe(t, err.Error())
			assertSafe(t, logs.String())
		})
	}
	redirects := 0
	client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		redirects++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.invalid/leak"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := (&telegram{client: client}).send(context.Background(), authority{BotToken: fixtureToken, ChatID: fixtureChat}, TestAlert().text); err != Error("provider-rejected") || redirects != 1 {
		t.Fatal("redirect followed")
	}
}

func TestClosedAlertContent(t *testing.T) {
	alert := ComponentAlert("xray", "stable", strings.Repeat("a", 64), "update-available", time.Unix(1, 0))
	if alert.text == "" || len(alert.text) > 512 || strings.Contains(alert.text, strings.Repeat("a", 64)) {
		t.Fatal("unsafe alert")
	}
	for _, alert := range []Alert{ComponentAlert("node", "stable", strings.Repeat("a", 64), "changed", time.Now()), ComponentAlert("xray", "stable", fixtureToken, "changed", time.Now()), ComponentAlert("xray", "beta", strings.Repeat("a", 64), "changed", time.Now()), PanelAlert(fixtureToken, strings.Repeat("a", 64), time.Now())} {
		if alert.text != "" {
			t.Fatal("arbitrary text admitted")
		}
	}
	assertSafe(t, alert.text)
}
