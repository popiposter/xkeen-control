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
	} {
		got := ReportLabel(c.node, 3)
		if got != c.want || strings.Contains(got, "203.0.113") || strings.Contains(got, "edge-fra") {
			t.Fatalf("ReportLabel(%+v) = %q, want %q", c.node, got, c.want)
		}
	}
}
