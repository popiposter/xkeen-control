package nodes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandActivatorVerifiesEmptySetupBaseline(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "04_outbounds.json")
	routing := filepath.Join(dir, "05_routing.json")
	if err := os.WriteFile(active, []byte(`{"outbounds":[{"tag":"api"},{"tag":"block"},{"tag":"direct"},{"tag":"dns-out"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routing, []byte(`{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"strategy":{"type":"leastPing"}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeCalls := 0
	activator := CommandActivator{ActiveOutboundsPath: active, RoutingPath: routing, RuntimeVerifier: func(_ context.Context, _ string, balancer string, expected []string) error {
		runtimeCalls++
		if balancer != "bal-proxy" || len(expected) != 0 {
			return errors.New("unexpected empty setup runtime request")
		}
		return nil
	}}
	if err := activator.VerifyEmptyOutboundTags(context.Background()); err != nil {
		t.Fatalf("empty setup baseline rejected: %v", err)
	}
	if runtimeCalls != 1 {
		t.Fatalf("runtime verifier calls = %d, want 1", runtimeCalls)
	}

	if err := os.WriteFile(active, []byte(`{"outbounds":[{"tag":"api"},{"tag":"block"},{"tag":"direct"},{"tag":"dns-out"},{"tag":"proxy-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := activator.VerifyEmptyOutboundTags(context.Background()); err == nil {
		t.Fatal("unexpected proxy outbound accepted for empty setup")
	}
}
