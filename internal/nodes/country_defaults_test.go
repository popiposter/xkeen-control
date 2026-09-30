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
		{"Russian edge", "edge.example.com", "RU"},
		{"Belarus", "edge.example.com", "BY"},
		{"RU-1", "edge.example.com", "RU"},
		{"Edge", "edge-blr.example.com", "BY"},
		{"Edge", "edge.ru.example.com", "RU"},
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

func countrySubscriptionBody() []byte {
	var profiles []string
	for _, name := range []string{"🇷🇺 Edge", "Беларусь", "Germany", "Unknown"} {
		profiles = append(profiles, strings.Replace(syntheticProfile, "#Primary", "#"+url.PathEscape(name), 1))
	}
	return []byte(strings.Join(profiles, "\n"))
}

func assertCountryDefaults(t *testing.T, registry Registry, parentEnabled bool) {
	t.Helper()
	if len(registry.Nodes) != 4 {
		t.Fatalf("node count = %d", len(registry.Nodes))
	}
	for _, node := range registry.Nodes {
		want := parentEnabled && !subscriptionCountryDisabledByDefault(node.Name, node.VLESS.Host)
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
		if strings.Contains(before.Nodes[i].Name, "🇷🇺") {
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
		if subscriptionCountryDisabledByDefault(node.Name, node.VLESS.Host) && strings.Contains(string(rendered), node.OutboundTag) {
			t.Fatal("default-disabled member entered runtime outbounds")
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
		if !node.Enabled && strings.Contains(string(rendered), node.OutboundTag) {
			t.Fatal("automatic refresh rendered disabled member")
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
	if err != nil || len(registry.Nodes) != 1 || !registry.Nodes[0].Enabled {
		t.Fatal("manual import was default-disabled")
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
