package webassets

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentStyleNonceMatchesCSPAndIsNeverReused(t *testing.T) {
	handler := Handler()
	seen := map[string]bool{}
	for _, path := range []string{"/", "/index.html", "/"} {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'")
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("document not private: %d", w.Code)
		}
		prefix := `<meta name="style-nonce" content="`
		_, tail, ok := strings.Cut(w.Body.String(), prefix)
		if !ok {
			t.Fatal("nonce meta missing")
		}
		nonce, _, ok := strings.Cut(tail, `">`)
		if !ok || len(nonce) != 44 || seen[nonce] {
			t.Fatal("missing or reused nonce")
		}
		seen[nonce] = true
		policy := w.Header().Get("Content-Security-Policy")
		for _, expected := range []string{"style-src 'self' 'nonce-" + nonce + "'", "script-src 'self'", "frame-ancestors 'none'"} {
			if !strings.Contains(policy, expected) {
				t.Fatalf("directive changed: %s", expected)
			}
		}
		if strings.Contains(policy, "unsafe-inline") {
			t.Fatal("inline execution was enabled")
		}
	}
}
