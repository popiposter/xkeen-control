package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnifiedOutboundTagsUsesOnlySafeProjection(t *testing.T) {
	contents := []byte(`{"outbounds":[
        {"tag":"proxy-us-02","protocol":"vless","settings":{"vnext":[{"address":"vpn.example.invalid","users":[{"id":"UUID-SENTINEL","encryption":"none"}]}]},"streamSettings":{"realitySettings":{"privateKey":"PRIVATE-SENTINEL","shortId":"SHORT-SENTINEL"}}},
        {"tag":"proxy-main-01","settings":{"id":"ID-SENTINEL","subscription":"https://secret.invalid/token"}},
        {"tag":"direct","protocol":"freedom"},
        {"tag":"proxy-main-bad space","settings":{"id":"IGNORED"}}
    ]}`)

	tags, err := UnifiedOutboundTagsJSON(contents)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(tags, ","), "proxy-main-01,proxy-us-02"; got != want {
		t.Fatalf("tags = %q, want %q", got, want)
	}
	serialized, err := json.Marshal(tags)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "SENTINEL") {
		t.Fatal("credential-bearing fixture content reached the projection")
	}
}

func TestSanitizeErrorReturnsBoundedClass(t *testing.T) {
	cases := map[string]string{
		"context deadline exceeded for vpn.example.invalid:443": "timeout",
		"connection refused":              "connection-refused",
		"remote TLS certificate mismatch": "tls",
		"no such host in DNS resolver":    "dns",
		"unexpected probe response":       "probe-failed",
	}
	for input, want := range cases {
		if got := SanitizeError(input); got != want {
			t.Errorf("SanitizeError(%q) = %q, want %q", input, got, want)
		}
	}
}
