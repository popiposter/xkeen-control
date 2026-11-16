package nodes

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicNodeSourceGroupIsInternalOnly(t *testing.T) {
	r := NewRegistry()
	r.Subscriptions = []Subscription{{ID: "provider-a", Name: "Provider", Enabled: true}}
	r.Nodes = []Node{{ID: "n1", OutboundTag: "tag1", Name: "Node", Source: Source{Type: "subscription", SubscriptionID: "provider-a"}}, {ID: "n2", OutboundTag: "tag2", Name: "Manual", Source: Source{Type: "manual"}}}
	items := r.PublicNodes()
	if items[0].SourceGroup != "subscription:provider-a" || items[1].SourceGroup != "" {
		t.Fatalf("internal source grouping missing: %q, %q", items[0].SourceGroup, items[1].SourceGroup)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sourceGroup") || strings.Contains(string(encoded), "provider-a") {
		t.Fatal("private source group leaked into public node JSON")
	}
}
