//go:build linux

package xkeen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditorTargetMetadataDoesNotExposeOutboundCredentials(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	raw := `{"outbounds":[{"tag":"vpn","protocol":"vless","settings":{"vnext":[{"users":[{"id":"synthetic-private-node-credential"}]}]}},{"tag":"direct","protocol":"freedom"}]}`
	if err := os.WriteFile(filepath.Join(e.Dir, "04_outbounds.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := e.Workspace(context.Background())
	if err != nil || len(w.Targets) != 2 {
		t.Fatal(w, err)
	}
	if _, ok := w.Documents["04_outbounds.json"]; ok {
		t.Fatal("generated outbounds exposed as editable document")
	}
	encoded, _ := json.Marshal(w)
	if strings.Contains(string(encoded), "synthetic-private-node-credential") || strings.Contains(string(encoded), "vnext") {
		t.Fatal("target projection leaked credentials")
	}
}
