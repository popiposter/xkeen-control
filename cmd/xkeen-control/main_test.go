package main

import (
	"testing"
)

func TestUpdateInspectionAllowlistIsExact(t *testing.T) {
	for _, args := range [][]string{{"version", "--json"}, {"setup", "guard"}, {"self-update", "inspect"}, {"self-update", "inspect-installed"}, {"nodes", "recovery", "inspect"}} {
		if !updateReadOnlyCommand(args) {
			t.Fatalf("read-only inspection blocked: %v", args)
		}
	}
	for _, args := range [][]string{{"self-update", "--apply"}, {"self-update", "inspect", "--apply"}, {"nodes", "recovery", "restore"}, {"setup", "run"}, {"native", "start"}, {"password", "set"}} {
		if updateReadOnlyCommand(args) {
			t.Fatalf("mutation bypassed intent: %v", args)
		}
	}
}

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
