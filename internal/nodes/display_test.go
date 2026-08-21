package nodes

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicNodeUsesFriendlyNameAndSafeAddress(t *testing.T) {
	parsed, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	parsed.VLESS.Host = "undef-fin-31b.undef.network"
	node, err := NewNodeWithID(parsed.VLESS, "Node 1", Source{Type: "manual"}, "node-55555555")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Nodes = []Node{node}
	public := registry.PublicNodes()
	if len(public) != 1 || public[0].DisplayName != "🇫🇮 Finland · undef-fin-31b" || public[0].Address != "undef-fin-31b.undef.network:443" || public[0].CountryCode != "FI" {
		t.Fatalf("public display = %+v", public)
	}
	encoded, _ := json.Marshal(public)
	for _, secret := range []string{parsed.VLESS.UUID, parsed.VLESS.PublicKey, parsed.VLESS.ShortID} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("public node exposed credential material")
		}
	}
}

func TestPublicNodePreservesProviderFlag(t *testing.T) {
	name, code := nodeDisplayName("🇩🇪 Germany XHTTP", "203.0.113.10")
	if name != "🇩🇪 Germany XHTTP" || code != "DE" {
		t.Fatalf("provider display = %q %q", name, code)
	}
}
