package appliance

import (
	"reflect"
	"strings"
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
			value.DNS.DisableFallbackIfMatch = !value.DNS.DisableFallbackIfMatch
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

func TestCustomDomainExtExpressionsAreBoundToManagedGeodata(t *testing.T) {
	managed := []CustomRule{{Name: "managed geosite", Domains: []string{"ext:geosite_v2fly.dat:discord"}, Action: CustomRuleProxy}}
	if _, err := CompileCustomRules(ProductDefault(), managed); err != nil {
		t.Fatalf("managed geodata expression rejected: %v", err)
	}
	for _, expression := range []string{
		"ext:/tmp/operator-controlled.dat:tag",
		"ext:../../operator-controlled.dat:tag",
		"ext:geosite_v2fly.dat:../tag",
		"ext:geoip_v2fly.dat:ru",
		"ext:geosite_v2fly.dat:tag/with-slash",
	} {
		rules := []CustomRule{{Name: "invalid geosite", Domains: []string{expression}, Action: CustomRuleProxy}}
		if err := ValidateCustomRules(rules); err == nil {
			t.Fatalf("path-like or unmanaged geodata expression accepted: %q", expression)
		}
	}
}

func TestNormalizeCustomRuleCanonicalizesOnlyMatchMemberOrder(t *testing.T) {
	rule := CustomRule{
		Name:      "ordered rule",
		Domains:   []string{"domain:b.example", "domain:a.example"},
		IPs:       []string{"192.0.2.0/24", "10.0.0.0/8"},
		Protocols: []string{"tls", "http"},
		Networks:  []string{"udp", "tcp"},
		Ports:     []PortRange{{From: 443, To: 443}, {From: 80, To: 80}},
		Action:    CustomRuleDirect,
	}
	normalized := normalizeCustomRule(rule)
	if got, want := normalized.Domains, []string{"domain:a.example", "domain:b.example"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized domains = %v, want %v", got, want)
	}
	if got, want := normalized.IPs, []string{"10.0.0.0/8", "192.0.2.0/24"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized IPs = %v, want %v", got, want)
	}
	if got, want := normalized.Protocols, []string{"http", "tls"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized protocols = %v, want %v", got, want)
	}
	if got, want := normalized.Networks, []string{"tcp", "udp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized networks = %v, want %v", got, want)
	}
	if got, want := normalized.Ports, []PortRange{{From: 80, To: 80}, {From: 443, To: 443}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized ports = %v, want %v", got, want)
	}
	if normalized.Name != rule.Name || normalized.Action != rule.Action {
		t.Fatalf("ordered rule identity/action changed: %+v", normalized)
	}
}

func TestManagedPolicyDecompilesClosedDNSAndObservatoryDefaults(t *testing.T) {
	managed, err := DecompileManagedPolicy(ProductDefault())
	if err != nil {
		t.Fatal(err)
	}
	catalog := DNSResolverCatalog()
	if len(catalog) != len(managed.DNS.ProxyResolverIDs) || len(catalog) < 1 {
		t.Fatalf("resolver catalog = %+v settings = %+v", catalog, managed.DNS)
	}
	if managed.DNS.FallbackMode != FallbackModeSystem || !managed.DNS.CacheEnabled || !managed.DNS.ServeStale || managed.DNS.StaleTTLSeconds != 3600 || !managed.DNS.ParallelQueries {
		t.Fatalf("default DNS settings = %+v", managed.DNS)
	}
	if managed.Observatory.ProbeIntervalMinutes != 5 {
		t.Fatalf("default Observatory settings = %+v", managed.Observatory)
	}
	for _, option := range catalog {
		if option.ID == "" || option.Label == "" || strings.Contains(option.ID, "https") || strings.Contains(option.Label, "dns-query") {
			t.Fatalf("unsafe resolver catalog option = %+v", option)
		}
	}
}

func TestDNSObservatoryCompileSupportsOneResolverAndPreservesRouting(t *testing.T) {
	base := ProductDefault()
	catalog := DNSResolverCatalog()
	settings := DNSSettings{
		ProxyResolverIDs: []string{catalog[0].ID},
		FallbackMode:     FallbackModeDisabled,
		CacheEnabled:     true,
		ServeStale:       true,
		StaleTTLSeconds:  120,
		ParallelQueries:  false,
	}
	observatory := ObservatorySettings{ProbeIntervalMinutes: 2}
	withDNS, err := CompileDNSObservatory(base, settings, observatory)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := DecompileManagedPolicy(withDNS)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(managed.DNS, settings) || !reflect.DeepEqual(managed.Observatory, observatory) {
		t.Fatalf("compiled settings = DNS %+v Observatory %+v", managed.DNS, managed.Observatory)
	}
	if len(withDNS.DNS.Servers) != 2 || withDNS.DNS.Servers[1].Address != "localhost" {
		t.Fatalf("DNS server shape = %+v", withDNS.DNS.Servers)
	}
	rules := []CustomRule{{Name: "preserve proxy", Domains: []string{"domain:preserve.example"}, Action: CustomRuleProxy}}
	withRouting, err := CompileCustomRules(withDNS, rules)
	if err != nil {
		t.Fatal(err)
	}
	afterRouting, err := DecompileManagedPolicy(withRouting)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterRouting.DNS, settings) || !reflect.DeepEqual(afterRouting.Observatory, observatory) {
		t.Fatalf("routing compile changed DNS/Observatory: DNS %+v Observatory %+v", afterRouting.DNS, afterRouting.Observatory)
	}
	if afterRouting.ProxyDNSDerivedDomainCount != 1 || len(withRouting.DNS.Servers[0].Domains) <= len(base.DNS.Servers[0].Domains) {
		t.Fatalf("routing-derived DNS domains = %d servers = %+v", afterRouting.ProxyDNSDerivedDomainCount, withRouting.DNS.Servers)
	}
	withDNSAgain, err := CompileDNSObservatory(withRouting, settings, observatory)
	if err != nil {
		t.Fatal(err)
	}
	routingBefore := withRouting.Routing
	routingAfter, err := DecompileManagedPolicy(withDNSAgain)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(routingBefore, withDNSAgain.Routing) || !reflect.DeepEqual(routingAfter.Rules, afterRouting.Rules) {
		t.Fatal("DNS compile changed custom routing rules")
	}
}

func TestDNSObservatoryClosedValidation(t *testing.T) {
	catalog := DNSResolverCatalog()
	valid := DNSSettings{ProxyResolverIDs: []string{catalog[0].ID}, FallbackMode: FallbackModeSystem, CacheEnabled: true, ServeStale: true, StaleTTLSeconds: 60, ParallelQueries: true}
	invalid := []DNSSettings{
		{ProxyResolverIDs: []string{}, FallbackMode: FallbackModeSystem},
		{ProxyResolverIDs: []string{catalog[0].ID, catalog[0].ID}, FallbackMode: FallbackModeSystem},
		{ProxyResolverIDs: []string{"operator-url"}, FallbackMode: FallbackModeSystem},
		{ProxyResolverIDs: []string{catalog[0].ID}, FallbackMode: FallbackModeSystem, CacheEnabled: false, ServeStale: true},
		{ProxyResolverIDs: []string{catalog[0].ID}, FallbackMode: FallbackModeSystem, CacheEnabled: true, ServeStale: false, StaleTTLSeconds: 1},
		{ProxyResolverIDs: []string{catalog[0].ID}, FallbackMode: FallbackModeSystem, CacheEnabled: true, ServeStale: true, StaleTTLSeconds: 86401},
	}
	for _, value := range invalid {
		if err := ValidateDNSSettings(value); err == nil {
			t.Fatalf("invalid DNS settings accepted: %+v", value)
		}
	}
	if err := ValidateDNSSettings(valid); err != nil {
		t.Fatal(err)
	}
	for _, minutes := range []int{0, 6} {
		if err := ValidateObservatorySettings(ObservatorySettings{ProbeIntervalMinutes: minutes}); err == nil {
			t.Fatalf("invalid Observatory interval accepted: %d", minutes)
		}
	}
}
