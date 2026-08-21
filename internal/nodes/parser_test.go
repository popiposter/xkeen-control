package nodes

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const syntheticProfile = "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=front.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&spx=%2F&type=tcp#Primary"

const syntheticProfileTwo = "vless://22222222-2222-4222-8222-222222222222@edge-2.example.com:8443?encryption=none&security=reality&sni=front-2.example.com&fp=firefox&pbk=BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB&sid=beef&type=tcp#Secondary"

const syntheticXHTTPFinalMaskProfile = "vless://33333333-3333-4333-8333-333333333333@edge-3.example.com:8443?encryption=none&security=reality&sni=front-3.example.com&fp=chrome&pbk=CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC&sid=cafe&type=xhttp&path=%2Ftunnel&host=front-3.example.com&mode=auto&fm=%7B%22fragment%22%3A%7B%22packets%22%3A%22tlshello%22%2C%22length%22%3A%2250-100%22%2C%22interval%22%3A%2210-20%22%7D%7D#%F0%9F%87%A9%F0%9F%87%AA%20Germany%20XHTTP"

func TestParseProfileStrictVLESSReality(t *testing.T) {
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.VLESS.Security != "reality" || parsed.VLESS.PublicKey == "" || parsed.VLESS.SpiderX != "/" || parsed.Name != "Primary" {
		t.Fatalf("parsed profile = %+v", parsed)
	}
	for _, invalid := range []string{
		"https://example.com/profile",
		strings.Replace(syntheticProfile, "security=reality", "security=tls", 1),
		strings.Replace(syntheticProfile, "&spx=%2F", "&privateKey=SECRET", 1),
		strings.Replace(syntheticProfile, "&sid=abcd", "&sid=abcd&sid=beef", 1),
		strings.Replace(syntheticProfile, "11111111-1111-4111-8111-111111111111", "not-a-uuid", 1),
	} {
		if _, err := ParseProfile(invalid); err == nil {
			t.Fatalf("invalid profile accepted: %q", invalid[:min(len(invalid), 60)])
		}
	}
}

func TestParseProfileStrictXHTTPFinalMask(t *testing.T) {
	parsed, err := ParseProfile(syntheticXHTTPFinalMaskProfile)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.VLESS.Network != "xhttp" || parsed.VLESS.Mode != "auto" || parsed.VLESS.FinalMask == nil || parsed.VLESS.FinalMask.Fragment.Delay != "10-20" || parsed.Name != "🇩🇪 Germany XHTTP" {
		t.Fatalf("XHTTP Finalmask profile = %+v", parsed)
	}
	for _, invalid := range []string{
		strings.Replace(syntheticXHTTPFinalMaskProfile, `%22interval%22`, `%22interval%22%3A%2210-20%22%2C%22unknown%22`, 1),
		strings.Replace(syntheticXHTTPFinalMaskProfile, `%2250-100%22`, `%220-100%22`, 1),
		strings.Replace(syntheticXHTTPFinalMaskProfile, "mode=auto", "mode=unbounded", 1),
	} {
		if _, err := ParseProfile(invalid); err == nil {
			t.Fatal("invalid XHTTP Finalmask profile accepted")
		}
	}
}

func TestParseSubscriptionBodyRawAndBase64(t *testing.T) {
	raw := syntheticProfile + "\n" + syntheticProfileTwo + "\n"
	if got, err := ParseSubscriptionBody([]byte(raw)); err != nil || len(got) != 2 {
		t.Fatalf("raw subscription = %d, %v", len(got), err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	if got, err := ParseSubscriptionBody([]byte(encoded)); err != nil || len(got) != 2 {
		t.Fatalf("base64 subscription = %d, %v", len(got), err)
	}
}

func TestMigrateLegacyCreatesNeutralStableNodes(t *testing.T) {
	legacy := `{"outbounds":[
{"tag":"direct","protocol":"freedom"},
{"tag":"proxy-main-01","protocol":"vless","settings":{"vnext":[{"address":"edge.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"front.example.com","fingerprint":"chrome","publicKey":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","shortId":"abcd","spiderX":"/"}}},
{"tag":"proxy-us-01","protocol":"vless","settings":{"vnext":[{"address":"edge-2.example.com","port":8443,"users":[{"id":"22222222-2222-4222-8222-222222222222","encryption":"none"}]}]},"streamSettings":{"network":"tcp","realitySettings":{"serverName":"front-2.example.com","fingerprint":"firefox","publicKey":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB","shortId":"beef"}}}
]}`
	registry, err := MigrateLegacy([]byte(legacy))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Nodes) != 2 {
		t.Fatalf("migrated nodes = %d", len(registry.Nodes))
	}
	for _, node := range registry.Nodes {
		if !strings.HasPrefix(node.OutboundTag, "proxy-") || strings.HasPrefix(node.OutboundTag, "proxy-main-") || strings.HasPrefix(node.OutboundTag, "proxy-us-") {
			t.Fatalf("non-neutral tag: %q", node.OutboundTag)
		}
		if node.Name == "proxy-main-01" || node.Name == "proxy-us-01" {
			t.Fatalf("historical region leaked into name: %q", node.Name)
		}
	}
	first := registry.Nodes[0].OutboundTag
	if _, err := json.Marshal(registry); err != nil || first == "" {
		t.Fatal("registry is not serializable")
	}
}

func TestMigrateLegacyAcceptsCurrentFlatVLESSSettings(t *testing.T) {
	legacy := `{"outbounds":[
{"tag":"proxy-main-01","protocol":"vless","settings":{"address":"edge.example.com","port":443,"id":"11111111-1111-4111-8111-111111111111","encryption":"none","flow":"xtls-rprx-vision"},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"front.example.com","fingerprint":"chrome","publicKey":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","shortId":"abcd","spiderX":"/"}}}
]}`
	registry, err := MigrateLegacy([]byte(legacy))
	if err != nil || len(registry.Nodes) != 1 {
		t.Fatalf("flat legacy settings were rejected: %v", err)
	}
	if registry.Nodes[0].OutboundTag == "proxy-main-01" || registry.Nodes[0].VLESS.Host != "edge.example.com" {
		t.Fatalf("flat legacy node was not normalized: %+v", registry.Nodes[0])
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
