package splitdns

import (
	"encoding/json"
	"net/netip"
)

// FreshConfig is the fixed initial resolver template. Compile/Service remain
// the native-policy derivation and activation owners. Protected DNS has no
// DIRECT fallback; additional verification listeners are loopback only.
func FreshConfig(home netip.Addr) ([]byte, error) {
	if !home.Is4() || !home.IsPrivate() {
		return nil, ErrPolicy
	}
	p := []plugin{
		{"direct", "forward", args(map[string]any{"upstreams": []map[string]string{{"addr": "https://8.8.8.8/dns-query"}}})},
		{"tunnel", "forward", args(map[string]any{"socks5": "127.0.0.1:5310", "upstreams": []map[string]string{{"addr": "https://1.1.1.1/dns-query"}}})},
		{"direct_only", "sequence", args([]map[string]string{{"exec": "$direct"}, {"exec": "accept"}})},
		{"vpn_only", "sequence", args([]map[string]string{{"exec": "$tunnel"}, {"exec": "accept"}})},
		{"main", "sequence", args([]map[string]string{{"exec": "goto direct_only"}})},
	}
	for _, v := range []struct{ tag, entry, listen string }{
		{"local", "main", "127.0.0.1:15354"}, {"home", "main", home.String() + ":15354"},
		{"verify_direct", "direct_only", "127.0.0.1:15355"}, {"verify_vpn", "vpn_only", "127.0.0.1:15356"},
	} {
		for _, proto := range []string{"tcp", "udp"} {
			p = append(p, plugin{v.tag + "_" + proto, proto + "_server", args(map[string]string{"entry": v.entry, "listen": v.listen})})
		}
	}
	return json.Marshal(map[string]any{"log": map[string]string{"level": "error", "file": "/dev/null"}, "plugins": p})
}
