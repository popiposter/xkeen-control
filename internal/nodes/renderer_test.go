package nodes

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testNode(t *testing.T, profile string, id string, enabled bool) Node {
	t.Helper()
	parsed, err := ParseProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewNodeWithID(parsed.VLESS, parsed.Name, Source{Type: "manual"}, id)
	if err != nil {
		t.Fatal(err)
	}
	node.Enabled = enabled
	return node
}

func TestRenderIsDeterministicAndFiltersDisabledNodes(t *testing.T) {
	registry := NewRegistry()
	registry.Nodes = []Node{
		testNode(t, syntheticProfileTwo, "node-22222222", false),
		testNode(t, syntheticProfile, "node-11111111", true),
	}
	first, err := Render(registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(registry)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("render is not deterministic: %v", err)
	}
	if strings.Contains(string(first), "proxy-node-22222222") || strings.Contains(string(first), "edge-2.example.com") {
		t.Fatal("disabled node was rendered")
	}
	if !strings.Contains(string(first), "proxy-node-11111111") || !strings.Contains(string(first), `"tag": "direct"`) {
		t.Fatal("canonical node or fixed outbound missing")
	}
	var document struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(first, &document); err != nil || len(document.Outbounds) != 5 {
		t.Fatalf("rendered outbounds = %d, %v", len(document.Outbounds), err)
	}
}

func TestRenderXHTTPAndFinalMask(t *testing.T) {
	registry := NewRegistry()
	registry.Nodes = []Node{testNode(t, syntheticXHTTPFinalMaskProfile, "node-44444444", true)}
	contents, err := Render(registry)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{`"network": "xhttp"`, `"xhttpSettings"`, `"mode": "auto"`, `"finalmask"`, `"type": "fragment"`, `"delay": "10-20"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("rendered XHTTP Finalmask missing %s", expected)
		}
	}
	if strings.Contains(text, `"wsSettings"`) {
		t.Fatal("XHTTP profile rendered as WebSocket")
	}
}

func TestStoreAtomicPrivateRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "nodes.json")
	store := Store{Path: path}
	registry := NewRegistry()
	registry.Nodes = []Node{testNode(t, syntheticProfile, "node-33333333", true)}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Nodes) != 1 {
		t.Fatalf("loaded registry = %+v, %v", loaded, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("registry permissions are not private: %v", err)
		}
		if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("registry directory permissions are not private: %v", err)
		}
	}
}
