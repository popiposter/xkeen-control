package benchmarkpolicy

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryPolicyIsBoundedForMaximumRegistry(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "xkeen", "xkeen.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy := Parse(raw)
	if policy.EligibleNodes != MaxEligibleNodes || policy.PayloadBytes != MaxPayloadBytes || policy.NodeSeconds != MaxNodeSeconds {
		t.Fatalf("repository benchmark policy = %+v", policy)
	}
	if policy.MaxBytes != (20<<20)*128 || policy.MaxSeconds != 1280 || policy.EligibleNodes > MaxRegistryNodes {
		t.Fatalf("repository benchmark budget is not bounded: %+v", policy)
	}
	if !bytes.Contains(raw, []byte(`"outbounds_file": "../../../../tmp/xkeen-control/benchmark-outbounds.json"`)) {
		t.Fatal("repository policy does not point XKeen at the tmpfs tag-only benchmark manifest")
	}
}

func TestPolicyRejectsUnboundedValues(t *testing.T) {
	for _, raw := range []string{
		`{"xkeen":{"xray":{"speed_balancer":{"max_nodes":129,"max_time":10,"test_url":"https://speed.example/down?bytes=20971520"}}}}`,
		`{"xkeen":{"xray":{"speed_balancer":{"max_nodes":128,"max_time":11,"test_url":"https://speed.example/down?bytes=20971520"}}}}`,
		`{"xkeen":{"xray":{"speed_balancer":{"max_nodes":128,"max_time":10,"test_url":"https://speed.example/down?bytes=20971521"}}}}`,
	} {
		if got := Parse([]byte(raw)); got != (Policy{}) {
			t.Fatalf("unbounded policy accepted: %+v", got)
		}
	}
}
