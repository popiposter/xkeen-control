package keenetic

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

const freshConfig = "! $$$ synthetic\ninterface Bridge0\n    rename Home\n    security-level private\n    ip address 192.168.50.1 255.255.255.0\n!\ninterface ISP0\n    rename Internet\n    security-level public\n    ip global 100\n!\nip hotspot\n    auto-register\n!\ndns-proxy\n    cache-size 100\n!\n"
const interfaceTree = `Interface, name = "Home"
                id: Bridge0
    interface-name: Home
             state: up
         connected: yes
           address: 192.168.50.1
             port, name = SyntheticPort:
                 state: up
Interface, name = "Internet"
                id: ISP0
    interface-name: Internet
             state: up
         connected: yes
`
const policyTree = `           policy, name = Policy1, description = xkeen:
                 mark: ffffad00
               table4: 41
               route4:
                    route:
                       destination: 0.0.0.0/0
                         interface: ISP0
                         rejecting: no
               table6: 0
               route6:
`

func fixture(t *testing.T) (*Adapter, Snapshot) {
	t.Helper()
	a := &Adapter{run: func(_ context.Context, c command, _ string) ([]byte, error) {
		switch c {
		case version:
			return []byte(" model: Ultra (KN-1811)\n release: 5.01.C.6.0-1\n arch: aarch64\n"), nil
		case interfaces:
			return []byte(interfaceTree), nil
		case running, startup:
			return []byte(freshConfig), nil
		case policies:
			return []byte(policyTree), nil
		}
		return nil, ErrCapability
	}}
	s, e := a.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return a, s
}
func TestObservedTextFamilyAliasesAndHexPolicy(t *testing.T) {
	a, s := fixture(t)
	p, e := a.Plan(s)
	if e != nil || p.Home != "Bridge0" || p.WAN != "ISP0" {
		t.Fatal(p, e)
	}
	cfg := strings.Replace(freshConfig, "ip hotspot\n", "ip policy Policy1\n    description xkeen\n    permit global Internet\n!\nip hotspot\n    policy Home Policy1\n", 1)
	prior := a.run
	a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
		if c == running {
			return []byte(cfg), nil
		}
		return prior(ctx, c, x)
	}
	if e = a.VerifyPolicy(context.Background(), p, false); e == nil {
		t.Fatal("assigned alias was treated as unassigned")
	}
	mark, e := a.Mark(context.Background(), p, true)
	if e != nil || mark != 0xffffad00 {
		t.Fatal("bare hex policy mark", e)
	}
}
func TestFreshRefusesHostOverridesMultipleHomeAndUnsavedState(t *testing.T) {
	for _, edit := range []string{
		strings.Replace(freshConfig, "    auto-register", "    host 02:00:00:00:00:01 permit", 1),
		strings.Replace(freshConfig, "    auto-register", "    host 02:00:00:00:00:01 policy Policy2", 1),
		freshConfig + "interface Bridge1\n    security-level private\n    ip address 192.168.51.1 255.255.255.0\n!\n",
	} {
		a, _ := fixture(t)
		prior := a.run
		a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
			if c == running || c == startup {
				return []byte(edit), nil
			}
			return prior(ctx, c, x)
		}
		if _, e := a.Discover(context.Background()); !errors.Is(e, ErrCapability) {
			t.Fatal("unsupported membership accepted", e)
		}
	}
	a, _ := fixture(t)
	prior := a.run
	a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
		if c == startup {
			return []byte(strings.Replace(freshConfig, "cache-size 100", "cache-size 101", 1)), nil
		}
		return prior(ctx, c, x)
	}
	if _, e := a.Discover(context.Background()); e == nil {
		t.Fatal("unsaved firmware admitted")
	}
}
func TestMutationPersistsBeforeDispatchAndRejectsUnrelatedDrift(t *testing.T) {
	for _, drift := range []bool{false, true} {
		a, s := fixture(t)
		p, _ := a.Plan(s)
		cfg := freshConfig
		intent := false
		prior := a.run
		a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
			if c == running {
				return []byte(cfg), nil
			}
			if c == mutation {
				if !intent {
					t.Fatal("command before durable intent")
				}
				cfg = strings.Replace(cfg, "ip hotspot\n", "ip policy Policy1\n    description xkeen\n!\nip hotspot\n", 1)
				if drift {
					cfg = strings.Replace(cfg, "cache-size 100", "cache-size 101", 1)
				}
				return []byte("ok"), nil
			}
			return prior(ctx, c, x)
		}
		confirmed := false
		_, e := a.change(context.Background(), s, p, "policy-create", "ip policy Policy1 description xkeen", func(i Intent) error { intent = true; confirmed = i.AfterHash != ""; return nil }, func(v Snapshot) bool { return v.Policies[p.PolicyID].Description == "xkeen" })
		if drift && (e == nil || confirmed) {
			t.Fatal("external DNS drift adopted")
		}
		if !drift && (e != nil || !confirmed) {
			t.Fatal(e)
		}
	}
}
func TestSaveNeedsIndependentStartupReadback(t *testing.T) {
	a, s := fixture(t)
	p, _ := a.Plan(s)
	reads := 0
	saved := false
	intent := false
	prior := a.run
	a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
		if c == mutation {
			if !intent || x != "system configuration save" {
				t.Fatal("unbounded save")
			}
			saved = true
			return nil, nil
		}
		if c == startup && saved {
			reads++
			if reads == 1 {
				return []byte(strings.Replace(freshConfig, "cache-size 100", "cache-size 99", 1)), nil
			}
		}
		return prior(ctx, c, x)
	}
	_, e := a.SaveAndInspect(context.Background(), s, p, func(i Intent) error { intent = true; return nil })
	if e != nil || reads < 2 {
		t.Fatal("save exit substituted for readback", e, reads)
	}
}
func TestInvalidPlanNeverDispatches(t *testing.T) {
	a, s := fixture(t)
	calls := 0
	a.run = func(context.Context, command, string) ([]byte, error) { calls++; return nil, nil }
	p := Plan{Home: "Home;reboot", WAN: "ISP0", PolicyID: "Policy1", Address: netip.MustParseAddr("192.168.50.1"), ProfileID: 1}
	if _, e := a.change(context.Background(), s, p, "home-policy", "ignored", func(Intent) error { return nil }, func(Snapshot) bool { return true }); e == nil || calls != 0 {
		t.Fatal("caller command admitted")
	}
}
