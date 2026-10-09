package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	panelupdate "github.com/popiposter/xkeen-control/internal/update"
)

func TestCapabilityInspectionReleasesAdmissionAndReportsFailures(t *testing.T) {
	for _, mode := range []string{"pass", "error", "cancel", "admission"} {
		t.Run(mode, func(t *testing.T) {
			closed, called := false, false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			admit := func() (func(), error) {
				if mode == "admission" {
					return nil, errors.New("unavailable")
				}
				return func() { closed = true }, nil
			}
			inspect := func(ctx context.Context) (panelupdate.CapabilityReport, error) {
				called = true
				if mode == "pass" {
					return panelupdate.CapabilityReport{Ready: true}, nil
				}
				if ctx.Err() != nil {
					return panelupdate.CapabilityReport{Reason: "probe-canceled"}, ctx.Err()
				}
				return panelupdate.CapabilityReport{Reason: "flock-unsupported"}, errors.New("unsupported")
			}
			var out bytes.Buffer
			err := runCapabilityInspection(ctx, &out, admit, inspect)
			var r panelupdate.CapabilityReport
			if json.Unmarshal(out.Bytes(), &r) != nil {
				t.Fatal("missing structured output", out.String())
			}
			if mode == "admission" {
				if called || closed || err == nil {
					t.Fatal("admission bypass")
				}
				return
			}
			if !closed || !called {
				t.Fatal("admission leaked or inspection skipped")
			}
			if (mode == "pass") != (err == nil) || r.Ready != (mode == "pass") {
				t.Fatal(r, err)
			}
		})
	}
}

func TestUpdateInspectionAllowlistIsExact(t *testing.T) {
	for _, args := range [][]string{{"version", "--json"}, {"setup", "guard"}, {"self-update", "inspect"}, {"self-update", "inspect-installed"}, {"self-update", "inspect-capabilities"}, {"nodes", "recovery", "inspect"}} {
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
