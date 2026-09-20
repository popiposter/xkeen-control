package appliance

import (
	"reflect"
	"testing"
)

func TestProductDefaultIsEditableWithNoCustomRules(t *testing.T) {
	policy, err := DecompileCustomPolicy(ProductDefault())
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != 0 || policy.CustomRegionRuleCount != 0 {
		t.Fatalf("product default custom policy = %+v", policy)
	}
	if policy.ProxyDNSDerivedDomainCount != 0 || policy.ProxyDNSResolverCount == 0 || policy.ProxyDNSBaselineDomainCount == 0 {
		t.Fatalf("product default DNS facts = %+v", policy)
	}
}

func TestCustomRulesCompileRoundTripAndDeriveProxyDNS(t *testing.T) {
	base := ProductDefault()
	rules := []CustomRule{
		{Name: "Proxy work", Domains: []string{"domain:work.example", "full:portal.example"}, Action: CustomRuleProxy},
		{Name: "Block noisy UDP", Networks: []string{"udp"}, Ports: []PortRange{{From: 443, To: 443}}, Action: CustomRuleBlock},
		{Name: "Direct local app", IPs: []string{"10.10.0.0/16"}, Protocols: []string{"tls"}, Action: CustomRuleDirect},
	}
	candidate, err := CompileCustomRules(base, rules)
	if err != nil {
		t.Fatal(err)
	}
	classified, err := DecompileCustomPolicy(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(classified.Rules, normalizeCustomRules(rules)) {
		t.Fatalf("round-trip rules = %+v", classified.Rules)
	}
	if classified.ProxyDNSDerivedDomainCount != 2 {
		t.Fatalf("derived DNS count = %d", classified.ProxyDNSDerivedDomainCount)
	}
	for _, server := range candidate.DNS.Servers {
		if server.Tag == "dns-proxy" {
			if server.Domains[len(server.Domains)-2] != "domain:work.example" || server.Domains[len(server.Domains)-1] != "full:portal.example" {
				t.Fatalf("derived DNS order = %v", server.Domains[len(server.Domains)-2:])
			}
		}
	}
	if got := candidate.Routing.Rules[len(candidate.Routing.Rules)-4].RuleTag; got != encodeCustomRuleName("Proxy work") {
		t.Fatalf("custom tag = %q", got)
	}
	if candidate.Routing.Rules[len(candidate.Routing.Rules)-4].Action.BalancerTag != "bal-proxy" {
		t.Fatalf("proxy action = %+v", candidate.Routing.Rules[len(candidate.Routing.Rules)-4].Action)
	}
}

func TestCustomPolicyRejectsProtectedAndManualDrift(t *testing.T) {
	base := ProductDefault()
	tests := []struct {
		name   string
		mutate func(Appliance) Appliance
	}{
		{name: "protected deletion", mutate: func(value Appliance) Appliance {
			value.Routing.Rules = value.Routing.Rules[1:]
			return value
		}},
		{name: "protected reorder", mutate: func(value Appliance) Appliance {
			value.Routing.Rules[0], value.Routing.Rules[1] = value.Routing.Rules[1], value.Routing.Rules[0]
			return value
		}},
		{name: "balancer drift", mutate: func(value Appliance) Appliance {
			value.Routing.Balancers[0].FallbackTag = "direct"
			return value
		}},
		{name: "untagged extra", mutate: func(value Appliance) Appliance {
			value.Routing.Rules = append(value.Routing.Rules[:len(value.Routing.Rules)-1], RoutingRule{Type: fieldRuleType, InboundTag: []string{"redirect", "tproxy", "socks"}, Action: RuleAction{OutboundTag: "direct"}, RuleTag: "manual"}, value.Routing.Rules[len(value.Routing.Rules)-1])
			return value
		}},
		{name: "malformed reserved tag", mutate: func(value Appliance) Appliance {
			value.Routing.Rules = append(value.Routing.Rules[:len(value.Routing.Rules)-1], RoutingRule{Type: fieldRuleType, InboundTag: []string{"redirect", "tproxy", "socks"}, Action: RuleAction{OutboundTag: "direct"}, RuleTag: CustomRuleTagPrefix + "bad"}, value.Routing.Rules[len(value.Routing.Rules)-1])
			return value
		}},
		{name: "DNS safety drift", mutate: func(value Appliance) Appliance {
			value.DNS.DisableFallback = !value.DNS.DisableFallback
			return value
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecompileCustomPolicy(test.mutate(base)); err == nil {
				t.Fatal("drift was accepted")
			}
		})
	}
}

func TestCustomPolicyRejectsInvalidRequestValues(t *testing.T) {
	for _, invalid := range [][]CustomRule{
		{{Name: "", Action: CustomRuleDirect}},
		{{Name: "duplicate", Action: CustomRuleDirect}, {Name: "duplicate", Action: CustomRuleProxy}},
		{{Name: "unknown protocol", Protocols: []string{"ftp"}, Action: CustomRuleDirect}},
		{{Name: "target", Domains: []string{"domain:example"}, Action: CustomRuleAction("raw-outbound")}},
	} {
		if err := ValidateCustomRules(invalid); err == nil {
			t.Fatalf("invalid custom rule was accepted: %+v", invalid)
		}
	}
}
