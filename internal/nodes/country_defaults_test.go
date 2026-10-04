package nodes

import (
	"context"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSubscriptionCountryHints(t *testing.T) {
	for _, tc := range []struct{ name, host, country string }{
		{"🇷🇺 Edge", "edge.example.com", "RU"},
		{"🇧🇾 Edge", "edge.example.com", "BY"},
		{"РОССИЯ", "edge.example.com", "RU"},
		{"Белоруссия", "edge.example.com", "BY"},
		{"Беларусь", "edge.example.com", "BY"},
		{"РФ", "edge.example.com", "RU"},
		{"RUS", "edge.example.com", "RU"},
		{"Belarusian edge", "edge.example.com", "BY"},
		{"Russian edge", "edge.example.com", "RU"},
		{"Belarus", "edge.example.com", "BY"},
		{"BLR", "edge.example.com", "BY"},
		{"BY", "edge.example.com", "BY"},
		{"[BY] Edge", "edge.example.com", "BY"},
		{"BY-1", "edge.example.com", "BY"},
		{"edge-by-01", "edge.example.com", "BY"},
		{"edge_ru_01", "edge.example.com", "RU"},
		{"RU", "edge.example.com", "RU"},
		{"RU-1", "edge.example.com", "RU"},
		{"Edge", "edge-by.example.com", "BY"},
		{"Edge", "edge.by.example.com", "BY"},
		{"Edge", "edge-blr.example.com", "BY"},
		{"Edge", "edge.ru.example.com", "RU"},
		{"Hosted by Provider", "edge.example.com", ""},
		{"Powered by Example", "edge.example.com", ""},
		{"by", "edge.example.com", ""},
		{"By Example", "edge.example.com", ""},
		{"Hosted bY Provider", "edge.example.com", ""},
		{"ru", "edge.example.com", ""},
		{"Stand by Provider", "edge.example.com", ""},
		{"by-example", "edge.example.com", ""},
		{"ruby-1", "edge.example.com", ""},
		{"bypass-1", "edge.example.com", ""},
		{"🇩🇪 Edge", "edge.ru.example.com", "DE"},
		{"🇷🇺 Edge", "edge-deu.example.com", "RU"},
		{"Brussels ruby bypass", "edge.example.com", ""},
	} {
		t.Run(tc.name+tc.host, func(t *testing.T) {
			_, code := nodeDisplayName(tc.name, tc.host)
			if code != tc.country {
				t.Fatalf("country = %q, want %q", code, tc.country)
			}
			if got := subscriptionCountryDisabledByDefault(tc.name, tc.host); got != (code == "RU" || code == "BY") {
				t.Fatal("subscription default differs from public country projection")
			}
		})
	}
}

var countrySubscriptionFixtures = []struct {
	name    string
	enabled bool
}{
	{"🇷🇺 Edge", false}, {"Беларусь", false}, {"BY-1", false}, {"RU-1", false},
	{"Germany", true}, {"Unknown", true}, {"Hosted by Provider", true}, {"Powered by Example", true},
	{"Germany WL Mobile Vless", false}, {"Poland wl Mobile Vless", false}, {"Newland", true},
}

func TestWLNameDefaultsAndSavedRefreshChoices(t *testing.T) {
	for name, want := range map[string]bool{"Germany WL Mobile Vless": true, "Poland wl Mobile Vless": true, "WL-Mobile": true, "Newland": false, "SWL": false, "ordinary": false} {
		if nodeNameWL(name) != want {
			t.Fatalf("WL token mismatch: %q", name)
		}
	}
	profile, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	target := Subscription{ID: "sub-12345678", Name: "Provider", URL: "https://subscription.example/token", Enabled: true}
	for _, enabled := range []bool{false, true} {
		node, err := NewNodeWithID(profile.VLESS, "Germany WL Mobile Vless", Source{Type: "subscription", SubscriptionID: target.ID}, "node-12345678")
		if err != nil {
			t.Fatal(err)
		}
		node.Enabled = enabled
		before := NewRegistry()
		before.Subscriptions = []Subscription{target}
		before.Nodes = []Node{node}
		candidate, err := buildSubscriptionCandidate(before, target, []ParsedProfile{{VLESS: profile.VLESS, Name: node.Name}})
		if err != nil || len(candidate.Nodes) != 1 || candidate.Nodes[0].Enabled != enabled {
			t.Fatalf("refresh lost explicit WL choice: %v", err)
		}
		target.Enabled = false
		candidate, err = buildSubscriptionCandidate(before, target, []ParsedProfile{{VLESS: profile.VLESS, Name: node.Name}})
		if err != nil || candidate.Nodes[0].Enabled {
			t.Fatalf("disabled parent did not gate WL choice: %v", err)
		}
		target.Enabled = true
	}
	manager, store, active := testManager(t, nil, nil)
	input := syntheticProfile + "\n" + strings.Replace(syntheticProfileTwo, "#Secondary", "#Germany%20WL%20Mobile%20Vless", 1)
	preview, err := manager.PreviewImport("csrf", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	registry, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	outbounds, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range registry.Nodes {
		if node.Enabled == nodeNameWL(node.Name) || strings.Contains(string(outbounds), node.OutboundTag) != node.Enabled {
			t.Fatal("WL import/default runtime mismatch")
		}
	}
}

func countrySubscriptionBody() []byte {
	var profiles []string
	for _, fixture := range countrySubscriptionFixtures {
		profiles = append(profiles, strings.Replace(syntheticProfile, "#Primary", "#"+url.PathEscape(fixture.name), 1))
	}
	return []byte(strings.Join(profiles, "\n"))
}

func assertCountryDefaults(t *testing.T, registry Registry, parentEnabled bool) {
	t.Helper()
	if len(registry.Nodes) != len(countrySubscriptionFixtures) {
		t.Fatalf("node count = %d", len(registry.Nodes))
	}
	wantByName := make(map[string]bool)
	for _, fixture := range countrySubscriptionFixtures {
		wantByName[fixture.name] = parentEnabled && fixture.enabled
	}
	for _, node := range registry.Nodes {
		want, known := wantByName[node.Name]
		if !known {
			t.Fatalf("unexpected fixture member %q", node.Name)
		}
		if node.Enabled != want {
			t.Fatalf("unexpected enabled state for %q", node.Name)
		}
	}
}

func TestCountryDefaultsCandidateRetainsChoicesAndOtherAuthorities(t *testing.T) {
	parsed, err := ParseSubscriptionBody(countrySubscriptionBody())
	if err != nil {
		t.Fatal(err)
	}
	target := Subscription{ID: "sub-12345678", Name: "Provider", URL: "https://subscription.example/token", Enabled: true}
	for _, enabled := range []bool{false, true} {
		target.Enabled = enabled
		candidate, err := buildSubscriptionCandidate(NewRegistry(), target, parsed)
		if err != nil {
			t.Fatal(err)
		}
		assertCountryDefaults(t, candidate, enabled)
	}
	target.Enabled = true
	before, err := buildSubscriptionCandidate(NewRegistry(), target, parsed)
	if err != nil {
		t.Fatal(err)
	}
	for i := range before.Nodes {
		if before.Nodes[i].Name == "Germany" {
			before.Nodes[i].Enabled = false
		}
		if strings.Contains(before.Nodes[i].Name, "🇷🇺") || before.Nodes[i].Name == "Беларусь" {
			before.Nodes[i].Enabled = true
		}
	}
	manual := testNode(t, syntheticProfileTwo, "node-98765432", true)
	manual.Name = "Russia manual"
	before.Nodes = append(before.Nodes, manual)
	after, err := buildSubscriptionCandidate(before, target, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("refresh changed saved choices, identities or unrelated manual member")
	}
	target.Enabled = false
	after, err = buildSubscriptionCandidate(before, target, parsed)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range after.Nodes {
		if node.Source.Type == "subscription" && node.Enabled {
			t.Fatal("disabled parent enabled a member")
		}
	}
	if !reflect.DeepEqual(after.Nodes[len(after.Nodes)-1], manual) {
		t.Fatal("parent gate changed manual member")
	}
}

func TestManualImportCountryDefaults(t *testing.T) {
	manager, _, _ := testManager(t, nil, nil)
	for _, tc := range countrySubscriptionFixtures {
		preview, err := manager.PreviewImport("country-default", strings.Replace(syntheticProfile, "#Primary", "#"+url.PathEscape(tc.name), 1))
		if err != nil {
			t.Fatal(err)
		}
		manager.mu.Lock()
		entry := manager.previews[preview.Token]
		manager.mu.Unlock()
		if len(entry.Registry.Nodes) != 1 || entry.Registry.Nodes[0].Enabled != tc.enabled {
			t.Fatalf("unexpected manual import default for %q", tc.name)
		}
		manager.Cancel("country-default", preview.Token)
	}
}

func TestCountryDefaultsManualPreviewApplyAndExplicitEnable(t *testing.T) {
	manager, store, active := testManager(t, nil, &fakeFetcher{body: countrySubscriptionBody()})
	preview, err := manager.PreviewRefresh(context.Background(), "csrf", "", "Provider", "https://subscription.example/token")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{store.Path, active} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("Preview wrote persistent state")
		}
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	registry, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	assertCountryDefaults(t, registry, true)
	rendered, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	var russianID string
	for _, node := range registry.Nodes {
		if node.Enabled != strings.Contains(string(rendered), node.OutboundTag) {
			t.Fatal("runtime outbounds differ from saved country defaults")
		}
		if strings.Contains(node.Name, "🇷🇺") {
			russianID = node.ID
		}
	}
	preview, err = manager.PreviewState("csrf", russianID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	preview, err = manager.PreviewRefresh(context.Background(), "csrf", registry.Subscriptions[0].ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Noop {
		t.Fatal("repeat refresh would overwrite explicit enabled choice")
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range updated.Nodes {
		if node.ID == russianID && !node.Enabled {
			t.Fatal("explicit node enable was lost")
		}
	}
}

func TestCountryDefaultsAutomaticRefresh(t *testing.T) {
	registry := refresherRegistry(t, true)
	manager, store, active := testManager(t, &registry, &countingSubscriptionFetcher{body: countrySubscriptionBody()})
	manager.managedCoordinator = &managedRefreshCoordinator{}
	result, err := manager.refreshSavedSubscription(context.Background(), "sub-12345678")
	if err != nil || result.LastResult != autoRefreshUpdated {
		t.Fatalf("refresh result = %+v, err=%v", result, err)
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	assertCountryDefaults(t, updated, true)
	rendered, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range updated.Nodes {
		if node.Enabled != strings.Contains(string(rendered), node.OutboundTag) {
			t.Fatal("automatic runtime outbounds differ from saved country defaults")
		}
	}
	result, err = manager.refreshSavedSubscription(context.Background(), "sub-12345678")
	if err != nil || result.LastResult != autoRefreshNoop {
		t.Fatalf("repeat result = %+v, err=%v", result, err)
	}
	for _, node := range updated.Nodes {
		if !strings.Contains(node.Name, "🇷🇺") {
			continue
		}
		preview, err := manager.PreviewState("csrf", node.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
			t.Fatal(err)
		}
	}
	result, err = manager.refreshSavedSubscription(context.Background(), "sub-12345678")
	if err != nil || result.LastResult != autoRefreshNoop {
		t.Fatal("automatic refresh would overwrite explicit enabled choice")
	}
}

func TestCountryDefaultsDoNotOverrideExplicitImportOrSubscriptionEnable(t *testing.T) {
	manager, store, _ := testManager(t, nil, &fakeFetcher{body: countrySubscriptionBody()})
	profile := strings.Replace(syntheticProfileTwo, "#Secondary", "#Russia", 1)
	preview, err := manager.PreviewImport("csrf", profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	registry, err := store.Load()
	if err != nil || len(registry.Nodes) != 1 || registry.Nodes[0].Enabled {
		t.Fatal("manual Russia import was not default-disabled")
	}
	preview, err = manager.PreviewState("csrf", registry.Nodes[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	preview, err = manager.PreviewRefresh(context.Background(), "csrf", "", "Provider", "https://subscription.example/token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	registry, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	preview, err = manager.PreviewSubscriptionState("csrf", registry.Subscriptions[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	registry, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range registry.Nodes {
		if !node.Enabled {
			t.Fatal("explicit subscription enable failed to enable member")
		}
	}
}
