package nativequality

import (
	"os"
	"testing"
)

func TestProbeRouteShadowedRequiresInboundScopedRules(t *testing.T) {
	for name, routing := range map[string]string{
		"no rules":    `{"routing":{"rules":[]}}`,
		"all scoped":  `{"routing":{"rules":[{"inboundTag":["api"],"outboundTag":"api"},{"inboundTag":["redirect","tproxy"],"domain":["geosite:x"],"balancerTag":"bal-proxy"}]}}`,
		"with prefix": "// native comment\n" + `{"routing":{"rules":[{"inboundTag":["socks"],"outboundTag":"direct"}]}}`,
	} {
		if probeRouteShadowed(routing) {
			t.Fatalf("%s: inbound-scoped rules reported as shadowing", name)
		}
	}
	for name, routing := range map[string]string{
		"unscoped rule": `{"routing":{"rules":[{"inboundTag":["api"],"outboundTag":"api"},{"domain":["geosite:x"],"balancerTag":"bal-proxy"}]}}`,
		"empty inbound": `{"routing":{"rules":[{"inboundTag":[],"outboundTag":"direct"}]}}`,
		"probe inbound": `{"routing":{"rules":[{"inboundTag":["probe"],"outboundTag":"direct"}]}}`,
		"unparseable":   `{"routing":`,
	} {
		if !probeRouteShadowed(routing) {
			t.Fatalf("%s: shadowing rule admitted", name)
		}
	}
}

func TestShippedRoutingDoesNotShadowProbes(t *testing.T) {
	for _, path := range []string{"../../config/xray/05_routing.json", "../../config/presets/compact-selective-v1.json", "../../config/presets/ru-selective-v1.json"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if probeRouteShadowed(string(raw)) {
			t.Fatalf("%s shadows the probe inbound", path)
		}
	}
}
