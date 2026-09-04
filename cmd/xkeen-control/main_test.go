package main

import (
	"testing"

	"github.com/popiposter/xkeen-control/internal/components"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

func TestListenAddressAllowsOnlyLoopbackOrExactPrivateLAN(t *testing.T) {
	t.Setenv("XKEEN_CONTROL_LISTEN", "127.0.0.1:8787")
	if got, err := listenAddressFromEnv(); err != nil || got != "127.0.0.1:8787" {
		t.Fatalf("loopback address = %q, %v", got, err)
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "0.0.0.0:8787")
	if _, err := listenAddressFromEnv(); err == nil {
		t.Fatal("wildcard listen address accepted")
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "192.168.1.1:8787")
	if got, err := listenAddressFromEnv(); err != nil || got != "192.168.1.1:8787" {
		t.Fatalf("private LAN address = %q, %v", got, err)
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "8.8.8.8:8787")
	if _, err := listenAddressFromEnv(); err == nil {
		t.Fatal("public listen address accepted")
	}
	t.Setenv("XKEEN_CONTROL_LISTEN", "not-an-address")
	if _, err := listenAddressFromEnv(); err == nil {
		t.Fatal("malformed listen address accepted")
	}
}

func TestHTTPWriteWindowExceedsNodeTransactionBudget(t *testing.T) {
	if httpWriteTimeout <= nodes.DefaultTransactionTimeout+nodes.DefaultApplyGateWaitTimeout {
		t.Fatalf("HTTP write timeout %s does not exceed gate plus transaction budget %s", httpWriteTimeout, nodes.DefaultTransactionTimeout+nodes.DefaultApplyGateWaitTimeout)
	}
	if httpWriteTimeout <= components.DefaultXKeenTransactionTimeout+components.DefaultMutationWaitTimeout {
		t.Fatalf("HTTP write timeout %s does not exceed XKeen transaction plus admission budget %s", httpWriteTimeout, components.DefaultXKeenTransactionTimeout+components.DefaultMutationWaitTimeout)
	}
	if got := httpWriteTimeout - components.DefaultMutationOperationTimeout; got != componentMutationResponseGrace {
		t.Fatalf("HTTP response grace = %s", got)
	}
}
