package nodes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoadRetainsSchemaV1CompatibilityWhileCanonicalParserIsStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.json")
	contents := []byte(`{"schemaVersion":1,"nodes":[],"subscriptions":[{"id":"sub-11111111","name":"Synthetic provider","url":"https://subscription.example/token","futureField":"accepted"}],"futureRoot":true}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := (Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("compatible Store.Load rejected schema-v1 fixture: %v", err)
	}
	if len(loaded.Subscriptions) != 1 || !loaded.Subscriptions[0].Enabled {
		t.Fatalf("compatibility defaults changed: %+v", loaded.Subscriptions)
	}
	if _, err := ParseCanonical(contents); err == nil {
		t.Fatal("strict backup parser accepted additive registry fields")
	}

	canonical, err := MarshalCanonical(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCanonical(canonical); err != nil {
		t.Fatalf("strict parser rejected canonical registry: %v", err)
	}
}
