package keenetic

import (
	"strings"
	"testing"
)

const runtimeDNS = `      proxy-status:
        proxy-name: System
      proxy-config:
         dns_server = 198.51.100.20 .
        proxy-safe:
         profile 12 safe-access
         profile-set-server 12 192.168.50.1:15354
         set-profile mac_default 0
      proxy-status:
        proxy-name: Policy1
      proxy-config:
         dns_server = 198.51.100.20 .
`

func TestDNSInternalIDIsIndependentAndMappingUsesKernelIndex(t *testing.T) {
	a, s := fixture(t)
	p, _ := a.Plan(s)
	cfg := strings.Replace(freshConfig, "    cache-size 100", "    cache-size 100\n    filter profile 1 description xkeen-control\n    filter profile 1 dns53 upstream 192.168.50.1:15354\n    filter engine public", 1)
	lines, e := parseConfig([]byte(cfg))
	if e != nil {
		t.Fatal(e)
	}
	s, e = project(lines)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := parseSafeDNS([]byte(runtimeDNS))
	if e != nil || verifyDNSProjection(s, p, parsed, 27, false) != nil {
		t.Fatal("unassigned custom profile", e)
	}
	cfg = strings.Replace(cfg, "    filter engine public", "    filter engine public\n    filter assign interface profile Home 1", 1)
	lines, _ = parseConfig([]byte(cfg))
	s, e = project(lines)
	if e != nil {
		t.Fatal(e)
	}
	actual := strings.Replace(runtimeDNS, "         set-profile mac_default 0", "         set-profile mac_default 0\n         set-profile-interface 27 12", 1)
	parsed, e = parseSafeDNS([]byte(actual))
	if e != nil || verifyDNSProjection(s, p, parsed, 27, true) != nil {
		t.Fatal("observed mapping", e)
	}
	if verifyDNSProjection(s, p, parsed, 1, true) == nil {
		t.Fatal("external DNS profile id guessed as kernel index")
	}
}
func TestDNSRuntimeRejectsAlternativeServerHostOverrideAndAmbiguousShape(t *testing.T) {
	for _, text := range []string{strings.Replace(runtimeDNS, "         set-profile mac_default 0", "         profile-set-server 12 198.51.100.40:53\n         set-profile mac_default 0", 1), strings.Replace(runtimeDNS, "         set-profile mac_default 0", "         set-profile 02:00:00:00:00:01 12", 1), runtimeDNS + "        proxy-name: System\n        proxy-safe:\n         set-profile mac_default 0\n"} {
		parsed, e := parseSafeDNS([]byte(text))
		if e == nil && len(parsed.profiles) == 1 && len(parsed.profiles[12].servers) == 1 {
			t.Fatal("unsafe runtime shape admitted")
		}
	}
}
