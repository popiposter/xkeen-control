package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func TestPerformanceSnapshotIncludesBoundedAdaptiveProjection(t *testing.T) {
	coordinator := c1.NewCoordinator(c1.DefaultPolicy(), nil, nil, nil)
	collector := NewCollector("test", time.Now().UTC(), Dependencies{C1: coordinator})
	performance := collector.PerformanceSnapshot(context.Background())
	// An unstarted Coordinator has no due time; the state must still be
	// present and truthful rather than being confused with legacy benchmark
	// scheduling.
	if performance.Adaptive.State != "waiting" || !performance.Adaptive.NextRunAt.IsZero() {
		t.Fatalf("adaptive idle projection = %+v", performance.Adaptive)
	}
	encoded, err := json.Marshal(performance)
	if err != nil {
		t.Fatal(err)
	}
	value := string(encoded)
	if !strings.Contains(value, `"adaptive"`) || !strings.Contains(value, `"state":"waiting"`) {
		t.Fatalf("adaptive projection missing from performance JSON: %s", value)
	}
	if strings.Contains(value, "speed.cloudflare.com") || strings.Contains(value, "provider") {
		t.Fatalf("provider material reached performance projection: %s", value)
	}
}
