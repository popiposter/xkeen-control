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

func TestReportLabelNeverUsesTheEndpoint(t *testing.T) {
	for _, c := range []struct {
		node PublicNode
		want string
	}{
		{PublicNode{Name: "Imported node", DisplayName: "🇩🇪 Germany · 203.0.113.7", Address: "203.0.113.7", CountryCode: "DE"}, "🇩🇪 Germany · Node 3"},
		{PublicNode{Name: "node 2", DisplayName: "edge-fra", Address: "edge-fra.example.net"}, "Node 3"},
		{PublicNode{Name: "Frankfurt", CountryCode: "DE"}, "🇩🇪 Frankfurt"},
		{PublicNode{Name: "🇩🇪 Frankfurt", CountryCode: "DE"}, "🇩🇪 Frankfurt"},
		// Provider fragments are kept unless they could carry an endpoint
		// or a credential.
		{PublicNode{Name: "🇫🇮 [FI] Финляндия-2", CountryCode: "FI"}, "🇫🇮 [FI] Финляндия-2"},
		{PublicNode{Name: "🇳🇱 Netherlands(W) XH Vless", CountryCode: "NL"}, "🇳🇱 Netherlands(W) XH Vless"},
		{PublicNode{Name: "Fast 203.0.113.7 node", CountryCode: "DE"}, "🇩🇪 Germany · Node 3"},
		{PublicNode{Name: "edge [2001:db8::7]"}, "Node 3"},
		{PublicNode{Name: "FRA edge-fra.example.net:443"}, "Node 3"},
		{PublicNode{Name: "vless://user@host"}, "Node 3"},
		{PublicNode{Name: "id 2f0c6e2a-1b2c-4d5e-8f90-a1b2c3d4e5f6"}, "Node 3"},
		{PublicNode{Name: "sid 6ba85179e30d4fc2"}, "Node 3"},
		{PublicNode{Name: "key Zm9vYmFyYmF6cXV4cXV1eHF1dXhxdXV4"}, "Node 3"},
		{PublicNode{Name: "Paris edgehost", Address: "EdgeHost"}, "Node 3"},
	} {
		got := ReportLabel(c.node, 3)
		if got != c.want || strings.Contains(got, "203.0.113") || strings.Contains(got, "edge-fra") {
			t.Fatalf("ReportLabel(%+v) = %q, want %q", c.node, got, c.want)
		}
	}
}
