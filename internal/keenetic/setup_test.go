package keenetic

import (
	"context"
	"encoding/json"
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
Interface, name = "0-SyntheticWiFi"
                id: WifiMaster0/AccessPoint0
    interface-name: 0-SyntheticWiFi
             state: down
         connected: no
`
const policyTree = `           policy, name = Policy1, description = xkeen:
                 mark: ffffad00
               table4: 41
               route4:
                    route:
                       destination: 0.0.0.0/0
                         interface: Internet
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
	for _, live := range []string{
		strings.ReplaceAll(interfaceTree, `"Internet"`, `"Other"`),
		interfaceTree + "Interface, name = \"Internet\"\n                id: ISP1\n    interface-name: Internet\n",
		interfaceTree + "Interface, name = \"ISP0\"\n                id: ISP1\n    interface-name: ISP0\n",
	} {
		a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
			if c == running {
				return []byte(cfg), nil
			}
			if c == interfaces {
				return []byte(live), nil
			}
			return prior(ctx, c, x)
		}
		if a.VerifyPolicy(context.Background(), p, true) == nil {
			t.Fatal("unknown or ambiguous live WAN alias admitted")
		}
	}
}

func TestDigitLeadingAliasInCompleteConfiguration(t *testing.T) {
	a, _ := fixture(t)
	prior := a.run
	a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
		if c == running || c == startup {
			return []byte(freshConfig + "interface WifiMaster0/AccessPoint0\n    rename 0-SyntheticWiFi\n!\n"), nil
		}
		return prior(ctx, c, x)
	}
	if _, err := a.Discover(context.Background()); err != nil {
		t.Fatal("data-only digit-leading alias rejected", err)
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

func TestAssignHOMEPolicyDoesNotConfigureDNS(t *testing.T) {
	a, initial := fixture(t)
	p, err := a.Plan(initial)
	if err != nil {
		t.Fatal(err)
	}
	cfg := strings.Replace(freshConfig, "ip hotspot\n", "ip policy Policy1\n    description xkeen\n    permit global Internet\n!\nip hotspot\n", 1)
	prior := a.run
	commands := []string{}
	a.run = func(ctx context.Context, c command, commandText string) ([]byte, error) {
		switch c {
		case running, startup:
			return []byte(cfg), nil
		case mutation:
			commands = append(commands, commandText)
			switch commandText {
			case "ip hotspot policy Bridge0 Policy1":
				cfg = strings.Replace(cfg, "    auto-register", "    auto-register\n    policy Bridge0 Policy1", 1)
			case "system configuration save":
			default:
				t.Fatal("unexpected mutation", commandText)
			}
			return nil, nil
		}
		return prior(ctx, c, commandText)
	}
	before, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, err := a.AssignHOMEPolicy(context.Background(), before, p, func(Intent) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || after.HomePolicy != p.PolicyID || after.HomeProfile != before.HomeProfile || after.Engine != before.Engine {
		t.Fatal("DNS-independent transition", commands, err)
	}
}

func TestSchemaTwoPolicyInverseNeverRemovesDNS(t *testing.T) {
	for _, drift := range []bool{false, true} {
		a, original := fixture(t)
		p, _ := a.Plan(original)
		// Exercise the real private baseline persistence boundary: config is
		// intentionally unexported and must not be needed by the inverse.
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var persisted Snapshot
		if json.Unmarshal(encoded, &persisted) != nil || persisted.config != nil || persisted.DNSHash == "" {
			t.Fatal("baseline projection")
		}
		original = persisted

		policyBlock := "ip policy Policy1\n    description xkeen\n    permit global Internet\n!\n"
		cfg := strings.Replace(freshConfig, "ip hotspot\n", policyBlock+"ip hotspot\n", 1)
		cfg = strings.Replace(cfg, "    auto-register", "    auto-register\n    policy Bridge0 Policy1", 1)
		if drift {
			cfg = strings.Replace(cfg, "    cache-size 100", "    cache-size 100\n    filter engine public", 1)
		}
		prior := a.run
		commands := []string{}
		a.run = func(ctx context.Context, c command, x string) ([]byte, error) {
			switch c {
			case running, startup:
				return []byte(cfg), nil
			case mutation:
				commands = append(commands, x)
				switch x {
				case "no ip hotspot policy Bridge0":
					cfg = strings.Replace(cfg, "    policy Bridge0 Policy1\n", "", 1)
				case "no ip policy Policy1":
					cfg = strings.Replace(cfg, policyBlock, "", 1)
				case "system configuration save":
				default:
					t.Fatal("unexpected inverse", x)
				}
				return nil, nil
			}
			return prior(ctx, c, x)
		}
		actual, e := a.current(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		missing := original
		missing.DNSHash = ""
		if _, err := a.RestorePolicyOwned(context.Background(), missing, p, actual.Hash, func(Intent) error { return nil }); err == nil || len(commands) != 0 {
			t.Fatal("missing persisted DNS proof admitted inverse")
		}
		restored, e := a.RestorePolicyOwned(context.Background(), original, p, actual.Hash, func(Intent) error { return nil })
		if drift {
			if e == nil || len(commands) != 0 {
				t.Fatal("DNS drift admitted inverse", e, commands)
			}
		} else if e != nil || restored.Hash != original.Hash || len(commands) != 3 {
			t.Fatal("policy inverse", e, commands)
		}
	}
}
