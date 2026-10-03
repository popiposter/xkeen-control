//go:build linux

package xkeen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetProjectionReportsTruncationBeforeQualityWeights(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	var targets []map[string]string
	for i := 0; i < 257; i++ {
		targets = append(targets, map[string]string{"tag": fmt.Sprintf("proxy-%03d", i), "protocol": "vless"})
	}
	raw, _ := json.Marshal(map[string]any{"outbounds": targets})
	if err := os.WriteFile(filepath.Join(e.Dir, "04_outbounds.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	w, err := e.Workspace(context.Background())
	if err != nil || len(w.Targets) != 256 || w.TargetsComplete {
		t.Fatal("truncated target projection appeared complete", err)
	}
}

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
