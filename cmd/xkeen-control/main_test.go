package main

import (
	"testing"
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
