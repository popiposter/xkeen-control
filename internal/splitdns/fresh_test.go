package splitdns

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestFreshResolverBindsOnlyExactHomeAndLoopback(t *testing.T) {
	home := netip.MustParseAddr("192.168.50.1")
	b, e := FreshConfig(home)
	if e != nil {
		t.Fatal(e)
	}
	var c struct{ Plugins []plugin }
	if json.Unmarshal(b, &c) != nil {
		t.Fatal("template")
	}
	listeners := 0
	for _, p := range c.Plugins {
		if p.Type == "tcp_server" || p.Type == "udp_server" {
			var a struct{ Listen string }
			_ = json.Unmarshal(p.Args, &a)
			endpoint, e := netip.ParseAddrPort(a.Listen)
			if e != nil || !endpoint.Addr().IsLoopback() && endpoint.Addr() != home {
				t.Fatal("wildcard or foreign listener")
			}
			listeners++
		}
	}
	if listeners != 8 {
		t.Fatal("missing readiness listeners")
	}
	for _, bad := range []string{"0.0.0.0", "203.0.113.1", "::1"} {
		if _, e := FreshConfig(netip.MustParseAddr(bad)); e == nil {
			t.Fatal("untrusted HOME accepted")
		}
	}
}
func TestRecoveryInspectionNeverRestartsDNS(t *testing.T) {
	s, _, count := fixture(t)
	ctx := context.Background()
	if s.Sync(ctx) != nil {
		t.Fatal("setup fixture")
	}
	before := *count
	fresh := &Service{Dir: s.Dir, AssetDir: s.AssetDir, Lease: s.Lease, ReadNative: s.ReadNative, Ready: s.Ready, Identity: s.Identity, Restart: func(context.Context) error { t.Fatal("recovery replay"); return nil }}
	if fresh.InspectOwned(ctx) != nil || fresh.Status(ctx).State != "synced" {
		t.Fatal("existing generation not observed")
	}
	if os.WriteFile(filepath.Join(s.Dir, "panel-manifest.json"), []byte(`{}`), 0600) != nil {
		t.Fatal("fixture")
	}
	if fresh.InspectOwned(ctx) == nil || *count != before {
		t.Fatal("drift caused restart or was accepted")
	}
}
