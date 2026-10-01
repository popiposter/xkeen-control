package nodes

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
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

func TestParseSubscriptionBodyRejectsEmptySnapshot(t *testing.T) {
	for _, body := range [][]byte{nil, []byte("\r\n \t")} {
		if _, err := ParseSubscriptionBody(body); err == nil {
			t.Fatalf("empty subscription snapshot was accepted: %q", body)
		}
	}
}

func subscriptionEncodings(raw string) map[string][]byte {
	result := map[string][]byte{"raw": []byte(raw)}
	for name, encoding := range map[string]*base64.Encoding{
		"std": base64.StdEncoding, "raw-std": base64.RawStdEncoding,
		"url": base64.URLEncoding, "raw-url": base64.RawURLEncoding,
	} {
		encoded := encoding.EncodeToString([]byte(raw))
		result[name] = []byte(" \n" + encoded[:len(encoded)/2] + "\r\n\t" + encoded[len(encoded)/2:] + "\n")
	}
	return result
}

func TestMixedSubscriptionImportsOnlyReality(t *testing.T) {
	tls := strings.Replace(syntheticProfile, "security=reality", "security=tls&alpn=h2&extra=unsupported", 1)
	raw := "trojan://synthetic@example.com:443\r\n" + syntheticProfile + "\nss://synthetic\n" + tls + "\n" + syntheticXHTTPFinalMaskProfile + "\nvmess://synthetic\n" + syntheticProfileTwo
	want, err := ParseProfiles(syntheticProfile + "\n" + syntheticXHTTPFinalMaskProfile + "\n" + syntheticProfileTwo)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range subscriptionEncodings(raw) {
		t.Run(name, func(t *testing.T) {
			got, err := ParseSubscriptionBody(body)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("mixed subscription did not retain exact supported profiles: count=%d error=%v", len(got), err)
			}
		})
	}
	if _, err := ParseProfiles(raw); err == nil {
		t.Fatal("manual import silently filtered unsupported profiles")
	}
}

func TestMixedSubscriptionRejectsInvalidEligibleSnapshot(t *testing.T) {
	for _, bad := range []string{
		"unexpected non-URI content",
		"vless:/truncated-profile",
		strings.Replace(syntheticProfile, "sid=abcd", "sid=xyz", 1),
		strings.Replace(syntheticProfile, "&sid=abcd", "&unknown=ignored&sid=abcd", 1),
		strings.Replace(syntheticProfile, "security=reality", "security=tls&security=reality", 1),
		strings.Replace(syntheticProfile, "security=reality", "security=tls&SECURITY=reality", 1),
		strings.Replace(syntheticProfile, "security=reality", "security=%ZZ", 1),
		strings.Replace(syntheticProfile, "security=reality", "security=", 1),
		strings.Replace(syntheticProfile, "security=reality&", "", 1),
		strings.Replace(syntheticProfile, "security=reality", "security=REALITY", 1),
		strings.Replace(syntheticProfile, "type=tcp", "type=unrecognized", 1),
	} {
		for name, body := range subscriptionEncodings(syntheticProfileTwo + "\nss://synthetic\n" + bad) {
			if got, err := ParseSubscriptionBody(body); err == nil || got != nil {
				t.Fatalf("invalid eligible snapshot returned partial membership for %s", name)
			}
		}
	}
}

func TestSubscriptionSubsetBoundsAndNoEmptyAuthority(t *testing.T) {
	unsupported := "ss://synthetic\ntrojan://synthetic@example.com:443\n" + strings.Replace(syntheticProfile, "security=reality", "security=tls", 1)
	for name, body := range subscriptionEncodings(unsupported) {
		if got, err := ParseSubscriptionBody(body); err == nil || got != nil {
			t.Fatalf("unsupported-only snapshot accepted for %s", name)
		}
	}
	raw := strings.Repeat("ss://synthetic\n", MaxProfileCount+1) + strings.Repeat(syntheticProfile+"\n", MaxProfileCount)
	if got, err := ParseSubscriptionBody([]byte(raw)); err != nil || len(got) != MaxProfileCount {
		t.Fatalf("eligible profile boundary rejected: count=%d error=%v", len(got), err)
	}
	for _, body := range [][]byte{[]byte(raw + syntheticProfile), []byte(strings.Repeat(" ", MaxProfileInput+1))} {
		if got, err := ParseSubscriptionBody(body); err == nil || got != nil {
			t.Fatal("oversize subscription returned partial membership")
		}
	}
}

func TestRealityShortIDCompatibility(t *testing.T) {
	for _, sid := range []string{"", "00", "ABCDEF0123456789"} {
		parsed, err := ParseProfile(strings.Replace(syntheticProfile, "sid=abcd", "sid="+sid, 1))
		if err != nil || parsed.VLESS.ShortID != sid {
			t.Fatalf("valid shortId rejected: %v", err)
		}
	}
	if parsed, err := ParseProfile(strings.Replace(syntheticProfile, "&sid=abcd", "", 1)); err != nil || parsed.VLESS.ShortID != "" {
		t.Fatalf("omitted shortId did not resolve to empty: %v", err)
	}
	for _, sid := range []string{"a", "abc", "gg", "012345678901234567", "00-0"} {
		if _, err := ParseProfile(strings.Replace(syntheticProfile, "sid=abcd", "sid="+sid, 1)); err == nil {
			t.Fatal("invalid shortId accepted")
		}
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
