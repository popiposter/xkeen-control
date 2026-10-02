package httpapi

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
)

func withLocalAddress(r *http.Request, local string) *http.Request {
	addr, err := net.ResolveTCPAddr("tcp", local)
	if err != nil {
		panic(err)
	}
	return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, addr))
}

func TestSocketAuthorityBeforeEveryDispatch(t *testing.T) {
	cases := []struct {
		local, host string
		allowed     bool
	}{
		{"127.0.0.1:8787", "127.0.0.1:8787", true},
		{"127.0.0.1:8787", "localhost:8787", true},
		{"[::1]:8787", "LOCALHOST:8787", true},
		{"10.0.0.4:8787", "10.0.0.4:8787", true},
		{"[fd00::4]:8787", "[fd00:0:0:0:0:0:0:4]:8787", true},
		{"127.0.0.1:80", "localhost", true},
		{"10.0.0.4:80", "10.0.0.4", true},
		{"[fd00::4]:80", "[fd00::4]", true},
		{"10.0.0.4:8787", "localhost:8787", false},
		{"[fd00::4]:8787", "localhost:8787", false},
		{"127.0.0.1:8787", "localhost.evil:8787", false},
		{"127.0.0.1:8787", "panel.example:8787", false},
		{"127.0.0.1:8787", "10.0.0.4:8787", false},
		{"127.0.0.1:8787", "127.0.0.1:8788", false},
		{"127.0.0.1:8787", "127.0.0.1", false},
		{"127.0.0.1:8787", "127.0.0.1:", false},
		{"127.0.0.1:8787", "[127.0.0.1]:8787", false},
		{"127.0.0.1:8787", "[localhost]:8787", false},
		{"[fd00::4]:8787", "fd00::4:8787", false},
		{"[fd00::4]:8787", "[fd00::4%zone]:8787", false},
		{"0.0.0.0:8787", "0.0.0.0:8787", false},
		{"8.8.8.8:8787", "8.8.8.8:8787", false},
		{"127.0.0.1:8787", "", false},
		{"", "127.0.0.1:8787", false},
	}
	for _, tc := range cases {
		for _, path := range []string{"/", "/assets/test.js", "/api/v1/session", "/healthz"} {
			t.Run(fmt.Sprint(tc.local, "/", tc.host, path), func(t *testing.T) {
				called := false
				s := New(Config{Assets: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) })})
				r := httptest.NewRequest("GET", "http://127.0.0.1:8787"+path, nil)
				r.Host = tc.host
				if tc.local != "" {
					r = withLocalAddress(r, tc.local)
				}
				r.Header.Set("Forwarded", "host=127.0.0.1:8787")
				r.Header.Set("X-Forwarded-Host", "127.0.0.1:8787")
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if !tc.allowed && (w.Code != 403 || called || w.Body.String() != "{\"error\":\"forbidden\"}\n") {
					t.Fatalf("guard bypass: %d %s", w.Code, w.Body.String())
				}
				if tc.allowed && (w.Code == 403 || path == "/healthz" && w.Code != 200) {
					t.Fatalf("valid authority blocked: %d", w.Code)
				}
				for header, expected := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer", "Cross-Origin-Opener-Policy": "same-origin", "Cross-Origin-Resource-Policy": "same-origin", "Permissions-Policy": "camera=(), microphone=(), geolocation=()"} {
					if w.Header().Get(header) != expected {
						t.Fatalf("header %s missing", header)
					}
				}
				if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Cross-Origin-Embedder-Policy") != "" {
					t.Fatal("CSP/isolation regressed")
				}
				if (strings.HasPrefix(path, "/api/") || path == "/healthz") && w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("no-store missing")
				}
			})
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8787/healthz", nil)
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.UnixAddr{Name: "not-tcp", Net: "unix"}))
	w := httptest.NewRecorder()
	New(Config{}).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("unparseable local context accepted")
	}
}

func TestRealSocketHealthProbeCompatibility(t *testing.T) {
	for _, address := range []string{"127.0.0.1:0", "[::1]:0"} {
		t.Run(address, func(t *testing.T) {
			listener, err := net.Listen("tcp", address)
			if err != nil {
				t.Skip("loopback family unavailable")
			}
			s := httptest.NewUnstartedServer(New(Config{}))
			s.Listener.Close()
			s.Listener = listener
			s.Start()
			defer s.Close()
			response, err := s.Client().Get(s.URL + "/healthz")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("numeric updater-style health rejected")
			}
		})
	}
}

func TestPasswordRoutesRejectNonExactObjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := setHTTPTestPassword(path, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	m := auth.NewManager(auth.Config{HashPath: path})
	session, token, err := m.Login("127.0.0.1", "synthetic-panel-password")
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Auth: m})
	for _, route := range []struct {
		path, field string
		limit       int
	}{
		{"/api/v1/session/login", "password", maxLoginBody},
		{"/api/v1/session/password", "newPassword", maxLoginBody},
		{"/api/v1/backup/export-secret", "currentPassword", backup.MaxSecretRequestBody},
	} {
		valid := fmt.Sprintf(`{"%s":"secret-sentinel"}`, route.field)
		if route.field == "currentPassword" {
			valid = fmt.Sprintf(`{"%s":"secret-sentinel","passphrase":"synthetic-passphrase"}`, route.field)
		}
		for _, bad := range []struct {
			name, body, query string
			media             []string
		}{
			{"missing", "{}", "", []string{"application/json"}},
			{"null", strings.Replace(valid, `"secret-sentinel"`, `null`, 1), "", []string{"application/json"}},
			{"wrong-case", strings.Replace(valid, route.field, strings.ToUpper(route.field), 1), "", []string{"application/json"}},
			{"duplicate", strings.TrimSuffix(valid, "}") + fmt.Sprintf(`,"%s":"secret-sentinel"}`, route.field), "", []string{"application/json"}},
			{"unknown", strings.TrimSuffix(valid, "}") + `,"unknown":"secret-sentinel"}`, "", []string{"application/json"}},
			{"trailing", valid + ` {}`, "", []string{"application/json"}},
			{"array", "[" + valid + "]", "", []string{"application/json"}},
			{"query", valid, "?password=secret-sentinel", []string{"application/json"}},
			{"empty-query", valid, "?", []string{"application/json"}},
			{"no-type", valid, "", nil},
			{"wrong-type", valid, "", []string{"text/plain"}},
			{"parameters", valid, "", []string{"application/json; charset=utf-8"}},
			{"multiple-types", valid, "", []string{"application/json", "application/json"}},
			{"combined-types", valid, "", []string{"application/json, application/json"}},
			{"oversize", valid + strings.Repeat(" ", route.limit), "", []string{"application/json"}},
		} {
			t.Run(route.path+"/"+bad.name, func(t *testing.T) {
				r := withLocalAddress(httptest.NewRequest("POST", "http://127.0.0.1:8787"+route.path+bad.query, strings.NewReader(bad.body)), "127.0.0.1:8787")
				for _, v := range bad.media {
					r.Header.Add("Content-Type", v)
				}
				r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
				r.Header.Set(auth.CSRFHeader, session.CSRFToken)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if w.Code != 400 || strings.Contains(w.Body.String(), "secret-sentinel") {
					t.Fatalf("unsafe body result: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestLoginLockoutUsesTCPRemoteAndIgnoresProxyHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := setHTTPTestPassword(path, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	s := New(Config{Auth: auth.NewManager(auth.Config{HashPath: path, LockoutAfter: 1})})
	for i := 0; i < 2; i++ {
		r := withLocalAddress(httptest.NewRequest("POST", "http://127.0.0.1:8787/api/v1/session/login", strings.NewReader(`{"password":"wrong"}`)), "127.0.0.1:8787")
		r.RemoteAddr = "10.0.0.2:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", fmt.Sprint("10.0.1.", i))
		r.Header.Set("Forwarded", fmt.Sprint("for=10.0.2.", i))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := 401
		if i == 1 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("remote identity changed: %d", w.Code)
		}
		_, _ = io.Copy(io.Discard, w.Result().Body)
	}
}
