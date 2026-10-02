package xkeen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeFixture(t *testing.T) Discovery {
	t.Helper()
	d := Discovery{Root: t.TempDir()}
	files := map[string]string{
		"opt/sbin/xkeen": "#!/bin/sh\n# XKEEN_FOREGROUND=1\ncase \"$1\" in\n -sb) ;;\nesac\nexit 99\n",
		"opt/sbin/.xkeen/01_info/01_info_variable.sh": "xkeen_current_version=\"2.0.1\"\nxkeen_build=\"Beta\"\n",
		"opt/etc/init.d/S05xkeen":                     "#!/bin/sh\nname_client=\"xray\"\nexit 99\n",
		"opt/etc/xkeen/xkeen.json":                    "// Native config uses comments\n{\"xkeen\":{\"xray\":{\"speed_balancer\":{\"enabled\":false}}},\"unrelated\":{\"url\":\"https://fixture.invalid/x//y\"}}",
		"opt/etc/xray/configs/03_inbounds.json":       "{/* native interfaces */\"inbounds\":[{\"tag\":\"redirect\",\"port\":5000}]}",
		"opt/etc/xray/configs/04_outbounds.json":      "{\"outbounds\":[{\"tag\":\"direct\",\"protocol\":\"freedom\"}]}",
		"opt/etc/xray/configs/05_routing.json":        "{\"routing\":{\"rules\":[{\"inboundTag\":[\"redirect\"],\"outboundTag\":\"direct\"}]}}",
		"opt/var/spool/cron/crontabs/root":            "# */5 * * * * xkeen -ug\n23 4 * * * /opt/sbin/xkeen -ug\n",
		"opt/etc/xray/dat/geosite_v2fly.dat":          "synthetic",
		"proc/modules":                                "xt_TPROXY 123 0 - Live 0x0\n",
	}
	for name, contents := range files {
		writeNativeFixture(t, d, name, contents)
	}
	return d
}

func writeNativeFixture(t *testing.T, d Discovery, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(d.path(name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.path(name), []byte(data), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestNativeDiscoveryDoesNotExecuteOrRewriteInstalledFiles(t *testing.T) {
	d := nativeFixture(t)
	before, err := os.ReadFile(d.path("opt/etc/xkeen/xkeen.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := d.Inspect(context.Background())
	if r.Installation != CapabilityAvailable || r.Version != "2.0.1" || r.Channel != "beta" || r.Core != "xray" ||
		r.Lifecycle != CapabilityAvailable || r.Configuration != CapabilityAvailable || r.ConfigFiles != 3 ||
		r.GeodataFiles != 1 || r.GeodataCron != CapabilityAvailable || !r.NeedsOnboarding || r.APIConfigured || r.PoolConfigured ||
		r.XrayRunning || r.NativeHook != CapabilityMissing || r.SpeedBalancer != CapabilityAvailable || r.SpeedBalancerEnabled {
		t.Fatalf("native discovery = %+v", r)
	}
	after, _ := os.ReadFile(d.path("opt/etc/xkeen/xkeen.json"))
	if string(before) != string(after) {
		t.Fatal("discovery rewrote native configuration")
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "fixture.invalid") || strings.Contains(string(encoded), "unrelated") {
		t.Fatal("projection exposed raw native settings")
	}
}

func TestNativeDiscoveryRequiresAPIServiceLoopbackInboundAndRoute(t *testing.T) {
	d := nativeFixture(t)
	writeNativeFixture(t, d, "opt/etc/xray/configs/08_api.json", `{"api":{"tag":"api","services":["RoutingService"]},"inbounds":[{"tag":"api","protocol":"tunnel","settings":{"address":"127.0.0.1"},"listen":"127.0.0.1","port":10085}]}`)
	if d.Inspect(context.Background()).APIConfigured {
		t.Fatal("missing API routing was accepted")
	}
	writeNativeFixture(t, d, "opt/etc/xray/configs/05_routing.json", `{"routing":{"rules":[{"inboundTag":["api"],"outboundTag":"api"}],"balancers":[{"tag":"bal-proxy","selector":["proxy-"]}]}}`)
	r := d.Inspect(context.Background())
	if !r.APIConfigured || !r.PoolConfigured || r.NeedsOnboarding {
		t.Fatalf("onboarding facts = %+v", r)
	}
	writeNativeFixture(t, d, "opt/etc/xray/configs/08_api.json", `{"api":{"tag":"api","services":["RoutingService"]},"inbounds":[{"tag":"api","listen":"0.0.0.0","port":10085}]}`)
	if d.Inspect(context.Background()).APIConfigured {
		t.Fatal("public API considered supported")
	}
}

func TestNativeDiscoveryUnknownSettingsDoNotBlockKnownCapabilities(t *testing.T) {
	d := nativeFixture(t)
	writeNativeFixture(t, d, "opt/etc/xray/configs/09_external.json", `{"unknownFutureFeature":{"largeInteger":9007199254740993}}`)
	r := d.Inspect(context.Background())
	if r.Configuration != CapabilityAvailable || r.Lifecycle != CapabilityAvailable {
		t.Fatalf("unknown field blocked known capability: %+v", r)
	}
	writeNativeFixture(t, d, "opt/sbin/.xkeen/01_info/01_info_variable.sh", "xkeen_current_version=\"$(secret-command)\"\nxkeen_build=\"Beta\"\n")
	r = d.Inspect(context.Background())
	if r.Version != "" || r.Installation != CapabilityUnknown {
		t.Fatalf("executable version accepted: %+v", r)
	}
}

func TestNativeDiscoveryResourceAndParseFailuresAreLocal(t *testing.T) {
	for _, test := range []struct {
		name, data string
		state      CapabilityState
	}{
		{"malformed", `{"routing":`, CapabilityUnsupported},
		{"duplicates", `{"routing":{"rules":[],"rules":[]}}`, CapabilityUnsupported},
		{"oversized", strings.Repeat(" ", maxNativeConfig+1), CapabilityUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := nativeFixture(t)
			writeNativeFixture(t, d, "opt/etc/xray/configs/05_routing.json", test.data)
			r := d.Inspect(context.Background())
			if r.Configuration != test.state || r.Lifecycle != CapabilityAvailable || r.APIConfigured || r.NeedsOnboarding {
				t.Fatalf("failure projection=%+v", r)
			}
		})
	}
	d := nativeFixture(t)
	for i := 0; i < maxNativeConfigFiles; i++ {
		writeNativeFixture(t, d, "opt/etc/xray/configs/extra-"+strings.Repeat("x", i)+".json", "{}")
	}
	if d.Inspect(context.Background()).Configuration != CapabilityUnknown {
		t.Fatal("unbounded configuration directory")
	}
}

func TestNativeDiscoveryMissingCanceledAndSymlink(t *testing.T) {
	d := Discovery{Root: t.TempDir()}
	r := d.Inspect(context.Background())
	if r.Installation != CapabilityMissing || r.Configuration != CapabilityMissing || r.NeedsOnboarding {
		t.Fatalf("absent = %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d.Inspect(ctx).Installation != CapabilityUnknown {
		t.Fatal("canceled read reported absence")
	}
	d = nativeFixture(t)
	path := d.path("opt/etc/xray/configs/05_routing.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(d.path("opt/etc/xkeen/xkeen.json"), path); err != nil {
		t.Skip("host cannot create test symlink")
	}
	if d.Inspect(context.Background()).Configuration != CapabilityUnknown {
		t.Fatal("followed symlink config")
	}
}

func TestNativeCronDoesNotMatchCommentsOrArbitraryShellText(t *testing.T) {
	d := nativeFixture(t)
	writeNativeFixture(t, d, "opt/var/spool/cron/crontabs/root", "# 1 2 * * * xkeen -ug\n1 2 * * * echo xkeen -ug\n1 2 * * * /opt/sbin/xkeen -ugi\n")
	if d.Inspect(context.Background()).GeodataCron != CapabilityMissing {
		t.Fatal("matched unrelated cron text")
	}
}

func TestNativeFreshDefaultsAndDatDirectoryMatchRealInstaller(t *testing.T) {
	d := nativeFixture(t)
	writeNativeFixture(t, d, "opt/etc/xkeen/xkeen.json", "{}\n")
	for _, name := range []string{"geoip_refilter.dat", "geoip_v2fly.dat", "geoip_zkeenip.dat", "geosite_refilter.dat", "geosite_zkeen.dat"} {
		writeNativeFixture(t, d, "opt/etc/xray/dat/"+name, "synthetic")
	}
	r := d.Inspect(context.Background())
	if r.GeodataFiles != 6 || r.SpeedBalancer != CapabilityAvailable || r.SpeedBalancerEnabled {
		t.Fatalf("fresh native defaults = %+v", r)
	}
}

func TestNativeDiscoveryRejectsAmbiguousManagementDeclarations(t *testing.T) {
	const inbound = `{"tag":"api","protocol":"tunnel","settings":{"address":"127.0.0.1"},"listen":"127.0.0.1","port":10085}`
	const route = `{"inboundTag":["api"],"outboundTag":"api"}`
	for _, name := range []string{"overlapping-routing", "overlapping-api", "wrong-protocol", "conditional-route", "shadowed-route", "duplicate-inbound"} {
		t.Run(name, func(t *testing.T) {
			d := nativeFixture(t)
			inbounds, rules := inbound, route
			switch name {
			case "overlapping-routing":
				writeNativeFixture(t, d, "opt/etc/xray/configs/09_override.json", `{"routing":{"rules":[]}}`)
			case "overlapping-api":
				writeNativeFixture(t, d, "opt/etc/xray/configs/09_override.json", `{"api":{"tag":"other","services":[]}}`)
			case "wrong-protocol":
				inbounds = strings.Replace(inbound, `"tunnel"`, `"socks"`, 1)
			case "conditional-route":
				rules = `{"inboundTag":["api"],"outboundTag":"api","domain":["example.invalid"]}`
			case "shadowed-route":
				rules = `{"network":"tcp","outboundTag":"direct"},` + route
			case "duplicate-inbound":
				inbounds += "," + inbound
			}
			writeNativeFixture(t, d, "opt/etc/xray/configs/08_api.json", `{"api":{"tag":"api","services":["RoutingService"]},"inbounds":[`+inbounds+`]}`)
			writeNativeFixture(t, d, "opt/etc/xray/configs/05_routing.json", `{"routing":{"rules":[`+rules+`],"balancers":[{"tag":"bal-proxy","selector":["proxy-"]}]}}`)
			r := d.Inspect(context.Background())
			if r.Configuration != CapabilityAvailable || r.PanelIntegration != CapabilityUnknown || r.APIConfigured || r.PoolConfigured || r.NeedsOnboarding {
				t.Fatalf("ambiguous management accepted: %+v", r)
			}
		})
	}
}
