package nodes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransactionNativePreservationAndConcurrentEdit(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		dir := t.TempDir()
		active := filepath.Join(dir, "04_outbounds.json")
		initial := []byte(`{"extension":true,"outbounds":[{"tag":"custom","protocol":"freedom"}]}`)
		if err := os.WriteFile(active, initial, 0600); err != nil {
			t.Fatal(err)
		}
		store := Store{Path: filepath.Join(dir, "secrets", "nodes.json")}
		if err := store.Save(NewRegistry()); err != nil {
			t.Fatal(err)
		}
		next := NewRegistry()
		next.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
		activator := &fakeActivator{}
		changed := []byte(`{"outbounds":[{"tag":"operator-change","protocol":"freedom"}]}`)
		if concurrent {
			activator.validate = func(context.Context) error { return os.WriteFile(active, changed, 0600) }
		}
		tx := Transaction{Store: store, ActiveOutboundsPath: active, ConfigDir: dir, PreviousDir: filepath.Join(t.TempDir(), "previous"), Activator: activator}
		err := tx.Apply(context.Background(), next)
		got, _ := os.ReadFile(active)
		if concurrent {
			if err == nil || activator.restarts != 0 || string(got) != string(changed) {
				t.Fatalf("concurrent edit overwritten: %v", err)
			}
			registry, loadErr := store.Load()
			if loadErr != nil || len(registry.Nodes) != 0 {
				t.Fatal("registry changed after rejected validation")
			}
		} else if err != nil || !strings.Contains(string(got), `"custom"`) || !strings.Contains(string(got), `"extension": true`) || !strings.Contains(string(got), "proxy-node-11111111") {
			t.Fatalf("native transaction lost data: %v", err)
		}
	}
}

func TestRenderNativePreservesExternalOrderAndFields(t *testing.T) {
	previous := NewRegistry()
	previous.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	next := NewRegistry()
	next.Nodes = []Node{testNode(t, syntheticProfileTwo, "node-22222222", true)}
	input := []byte(`{// native comment
	"extension":{"number":9007199254740993},"outbounds":[
	{"tag":"custom","protocol":"freedom","settings":{"domainStrategy":"UseIP"}},
	{"tag":"proxy-node-11111111","protocol":"vless"},
	{"tag":"direct","protocol":"freedom","extra":true}]}`)
	result, err := RenderNative(input, previous, next)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Outbounds []struct {
			Tag      string
			Settings json.RawMessage
			Extra    bool
		}
		Extension json.RawMessage
	}
	if err := json.Unmarshal(result, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Outbounds) != 3 || document.Outbounds[0].Tag != "custom" || document.Outbounds[1].Tag != "direct" || document.Outbounds[2].Tag != "proxy-node-22222222" || !document.Outbounds[1].Extra || !strings.Contains(string(document.Outbounds[0].Settings), "UseIP") || !strings.Contains(string(document.Extension), "9007199254740993") {
		t.Fatalf("native fields/order lost: %s", result)
	}
	next.Nodes[0].Enabled = false
	result, err = RenderNative(result, next, next)
	if err != nil || strings.Contains(string(result), "proxy-node-") {
		t.Fatalf("disabled node retained: %v", err)
	}
}

func TestRenderNativeRejectsExternalCollisionAndAmbiguousDocuments(t *testing.T) {
	next := NewRegistry()
	next.Nodes = []Node{testNode(t, syntheticProfile, "node-11111111", true)}
	for _, input := range []string{
		`{"outbounds":[{"tag":"proxy-node-11111111","protocol":"freedom"}]}`,
		`{"outbounds":[{"tag":"external"},{"tag":"external"}]}`,
		`{"outbounds":null}`, `{"outbounds":{}}`, `{"outbounds":[null]}`,
		`{"outbounds":[{"tag":42}]}`, `{"outbounds":[],"outbounds":[]}`,
	} {
		if _, err := RenderNative([]byte(input), NewRegistry(), next); err == nil {
			t.Fatalf("accepted ambiguous input %s", input)
		}
	}
}

func TestRenderNativeFreshTemplate(t *testing.T) {
	result, err := RenderNative([]byte("{/* native empty template */}"), NewRegistry(), NewRegistry())
	if err != nil || !strings.Contains(string(result), `"outbounds": []`) {
		t.Fatalf("empty native template: %s %v", result, err)
	}
}
