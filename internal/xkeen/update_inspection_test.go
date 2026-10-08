package xkeen

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestStable21IdentityDiscoveryIsReadOnlyAndRejectsAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name, variables string
		known           bool
	}{
		{"stable", "xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\nbuild_timestamp=\"2026-10-06 10:58:45 MSK\"\n", true},
		{"beta", "xkeen_current_version=\"2.0.1\"\nxkeen_build=\"Beta\"\n", true},
		{"dev", "xkeen_current_version=\"2.0.1\"\nxkeen_build=\"Dev\"\n", true},
		{"duplicate", "xkeen_current_version=\"2.1\"\nxkeen_current_version=\"2.0.1\"\nxkeen_build=\"Stable\"\n", false},
		{"malformed", "xkeen_current_version=\"$(private-command)\"\nxkeen_build=\"Stable\"\n", false},
		{"missing-channel", "xkeen_current_version=\"2.1\"\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := nativeFixture(t)
			writeNativeFixture(t, d, "opt/sbin/.xkeen/01_info/01_info_variable.sh", test.variables)
			before, _ := os.ReadFile(d.path("opt/etc/xkeen/xkeen.json"))
			facts := d.Inspect(context.Background())
			snapshot := d.updateSnapshot(context.Background())
			if (snapshot.release != nil) != test.known || (facts.Installation == CapabilityAvailable) != test.known {
				t.Fatalf("identity=%+v installation=%s", snapshot.release, facts.Installation)
			}
			if test.name == "stable" && (facts.Version != "2.1" || facts.Channel != "stable" || snapshot.release.BuildTimestamp != "2026-10-06 10:58:45 MSK") {
				t.Fatal(facts, snapshot.release)
			}
			after, _ := os.ReadFile(d.path("opt/etc/xkeen/xkeen.json"))
			if string(before) != string(after) {
				t.Fatal("readback changed configuration")
			}
		})
	}
}

func TestUpdateProcessObservationDoesNotTurnUnavailableIntoStopped(t *testing.T) {
	d := nativeFixture(t)
	writeNativeFixture(t, d, "proc/123/comm", "xray\n")
	if got := d.updateSnapshot(context.Background()).xrayProcess; got != "running" {
		t.Fatal(got)
	}
	if err := os.RemoveAll(d.path("proc")); err != nil {
		t.Fatal(err)
	}
	if got := d.updateSnapshot(context.Background()).xrayProcess; got != "unknown" {
		t.Fatal("unreadable proc reported stopped", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := d.updateSnapshot(ctx); got.release != nil || got.xrayProcess != "unknown" {
		t.Fatal(got)
	}
	encoded, _ := json.Marshal(updateResult(updateSnapshot{}, d.updateSnapshot(ctx)))
	if string(encoded) != `{"change":"unknown","xrayProcess":"unknown"}` {
		t.Fatal("unknown observation contains unsupported facts", string(encoded))
	}
}

func TestUpdateIdentityRejectsUnsupportedReassignments(t *testing.T) {
	const baseline = "xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\nbuild_timestamp=\"2026-10-06 10:58:45 MSK\"\n"
	for _, field := range []string{"xkeen_current_version", "xkeen_build", "build_timestamp"} {
		for _, suffix := range []string{"=\"bogus\"", "='bogus'", "=\"$(private-command)\"", "=bogus", " = bogus"} {
			for _, prefix := range []string{"", "  ", "export ", "true; "} {
				t.Run(field+prefix+suffix, func(t *testing.T) {
					d := nativeFixture(t)
					writeNativeFixture(t, d, "opt/sbin/.xkeen/01_info/01_info_variable.sh", baseline+prefix+field+suffix+"\n")
					snapshot := d.updateSnapshot(context.Background())
					if snapshot.release != nil {
						t.Fatal("ambiguous installed identity was published", snapshot.release)
					}
					result := updateResult(updateSnapshot{release: &NativeRelease{Version: "2.1", Channel: "stable"}}, snapshot)
					if result.Change != "unknown" || result.After != nil {
						t.Fatal("ambiguous identity became a verified update result", result)
					}
				})
			}
		}
	}
	for _, build := range []string{"", "build_timestamp=\"\"\n", "# build_timestamp='ignored'\n"} {
		d := nativeFixture(t)
		writeNativeFixture(t, d, "opt/sbin/.xkeen/01_info/01_info_variable.sh", "xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\n"+build)
		if got := d.updateSnapshot(context.Background()); got.release == nil || got.release.BuildTimestamp != "" {
			t.Fatal("missing or empty stock build stamp rejected", got)
		}
	}
}
