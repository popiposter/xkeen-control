package appliance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/nodes"
)

func TestProductDefaultMatchesCheckedInPolicy(t *testing.T) {
	if err := ValidateProductDefault(); err != nil {
		t.Fatalf("embedded product default: %v", err)
	}
	got, err := RenderPolicyFiles(ProductDefault())
	if err != nil {
		t.Fatalf("render product default: %v", err)
	}
	for _, name := range []string{"02_dns.json", "05_routing.json", "07_observatory.json"} {
		want, err := os.ReadFile(filepath.Join("..", "..", "config", "xray", name))
		if err != nil {
			t.Fatalf("read checked-in %s: %v", name, err)
		}
		if !SemanticJSONEqual(got["xray/"+name], want) {
			t.Fatalf("product default %s differs semantically from checked-in policy", name)
		}
	}
	files, err := RenderCandidateFiles(ProductDefault(), nodes.NewRegistry())
	if err != nil {
		t.Fatalf("render empty product candidate: %v", err)
	}
	if len(files) != 9 {
		t.Fatalf("candidate file count = %d, want 9", len(files))
	}
}
