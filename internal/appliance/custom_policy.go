package appliance

import (
	"encoding/hex"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// CustomRuleTagPrefix is the only ruleTag namespace that the editable
	// projection owns. The name is encoded as UTF-8 hex so the identity is
	// bounded, deterministic and reversible without accepting arbitrary tags.
	CustomRuleTagPrefix = "xkeen-custom-v1:"
	MaxCustomRules      = 64
	MaxCustomNameBytes  = (MaxRuleTag - len(CustomRuleTagPrefix)) / 2
	MaxCustomNameRunes  = 48
)

var (
	ErrCustomPolicyDrift   = errors.New("custom routing policy drift detected")
	ErrInvalidCustomPolicy = errors.New("custom routing policy is invalid")
)

var (
	customDomainExtFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	customDomainExtTagPattern      = regexp.MustCompile(`^[A-Za-z0-9._!-]{1,128}$`)
	customDomainExtAssetsOnce      sync.Once
	customDomainExtAssets          map[string]struct{}
)

type CustomRuleAction string

const (
	CustomRuleDirect CustomRuleAction = "direct"
	CustomRuleProxy  CustomRuleAction = "proxy"
	CustomRuleBlock  CustomRuleAction = "block"
)

// CustomRule is the deliberately narrow request/response projection for one
// client-routing rule. It contains no Xray type, inbound, outbound, balancer
// or ruleTag field; those are compiled by the server.
type CustomRule struct {
	Name      string           `json:"name"`
	Domains   []string         `json:"domains"`
	IPs       []string         `json:"ips"`
	Protocols []string         `json:"protocols"`
	Networks  []string         `json:"networks"`
	Ports     []PortRange      `json:"ports"`
	Action    CustomRuleAction `json:"action"`
}

// CustomPolicy is the safe result of classifying an adopted appliance
// authority. Its facts are bounded and contain no raw generated policy.
type CustomPolicy struct {
	Rules                       []CustomRule
	ProtectedRuleCount          int
	ProtectedPrefixRuleCount    int
	CustomRegionRuleCount       int
	ProxyDNSResolverCount       int
	ProxyDNSBaselineDomainCount int
	ProxyDNSDerivedDomainCount  int
}

type customRuleEnvelope struct {
	typeName    string
	inboundTags []string
	direct      string
	proxy       string
	block       string
}

// ValidateCustomRules validates the complete editable list before any
// candidate compilation or persistence work occurs.
func ValidateCustomRules(rules []CustomRule) error {
	if len(rules) > MaxCustomRules {
		return ErrInvalidCustomPolicy
	}
	seenNames := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if err := validateCustomRule(rule); err != nil {
			return ErrInvalidCustomPolicy
		}
		if _, exists := seenNames[rule.Name]; exists {
			return ErrInvalidCustomPolicy
		}
		seenNames[rule.Name] = struct{}{}
	}
	return nil
}

// DecompileCustomPolicy accepts only the current source-owned envelope. Any
// protected drift, manual extra rule, malformed reserved tag or DNS
// asymmetry is rejected rather than normalized into editable state.
func DecompileCustomPolicy(value Appliance) (CustomPolicy, error) {
	if err := value.Validate(); err != nil {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	baseline := ProductDefault()
	if err := baseline.Validate(); err != nil {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	if value.SchemaVersion != baseline.SchemaVersion ||
		value.Routing.DomainStrategy != baseline.Routing.DomainStrategy ||
		value.Routing.DomainMatcher != baseline.Routing.DomainMatcher ||
		!reflect.DeepEqual(value.Routing.Balancers, baseline.Routing.Balancers) {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	envelope, err := customRuleEnvelopeFor(baseline)
	if err != nil {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}

	if len(baseline.Routing.Rules) < 1 {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	prefixCount := len(baseline.Routing.Rules) - 1
	if len(value.Routing.Rules) < prefixCount+1 {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	for index := 0; index < prefixCount; index++ {
		if !reflect.DeepEqual(value.Routing.Rules[index], baseline.Routing.Rules[index]) {
			return CustomPolicy{}, ErrCustomPolicyDrift
		}
	}
	if !reflect.DeepEqual(value.Routing.Rules[len(value.Routing.Rules)-1], baseline.Routing.Rules[len(baseline.Routing.Rules)-1]) {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}

	rules := make([]CustomRule, 0, len(value.Routing.Rules)-prefixCount-1)
	for _, runtimeRule := range value.Routing.Rules[prefixCount : len(value.Routing.Rules)-1] {
		decoded, err := decodeCustomRuntimeRule(runtimeRule, envelope)
		if err != nil {
			return CustomPolicy{}, ErrCustomPolicyDrift
		}
		rules = append(rules, decoded)
	}
	if err := ValidateCustomRules(rules); err != nil {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}

	proxyResolvers, baselineDomains, derivedDomains, err := validateCustomDNS(value.DNS, baseline.DNS, rules)
	if err != nil {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	return CustomPolicy{
		Rules:                       cloneCustomRules(rules),
		ProtectedRuleCount:          len(baseline.Routing.Rules),
		ProtectedPrefixRuleCount:    prefixCount,
		CustomRegionRuleCount:       len(rules),
		ProxyDNSResolverCount:       proxyResolvers,
		ProxyDNSBaselineDomainCount: baselineDomains,
		ProxyDNSDerivedDomainCount:  derivedDomains,
	}, nil
}

// CompileCustomRules replaces only the authorized custom region and rebuilds
// proxy-DNS domains from the embedded baseline plus the complete candidate
// list. It never incrementally patches the currently observed DNS list.
func CompileCustomRules(value Appliance, rules []CustomRule) (Appliance, error) {
	if err := ValidateCustomRules(rules); err != nil {
		return Appliance{}, err
	}
	if _, err := DecompileCustomPolicy(value); err != nil {
		return Appliance{}, err
	}
	baseline := ProductDefault()
	envelope, err := customRuleEnvelopeFor(baseline)
	if err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	contents, err := MarshalCanonical(value)
	if err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	candidate, err := Parse(contents)
	if err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	normalized := normalizeCustomRules(rules)

	prefixCount := len(baseline.Routing.Rules) - 1
	routingRules := make([]RoutingRule, 0, prefixCount+len(normalized)+1)
	routingRules = append(routingRules, cloneRoutingRules(baseline.Routing.Rules[:prefixCount])...)
	for _, rule := range normalized {
		routingRules = append(routingRules, compileCustomRuntimeRule(rule, envelope))
	}
	routingRules = append(routingRules, cloneRoutingRules(baseline.Routing.Rules[prefixCount:])...)
	candidate.Routing.DomainStrategy = baseline.Routing.DomainStrategy
	candidate.Routing.DomainMatcher = baseline.Routing.DomainMatcher
	candidate.Routing.Rules = routingRules
	candidate.Routing.Balancers = cloneBalancers(baseline.Routing.Balancers)
	candidate.DNS = rebuildCustomDNS(baseline.DNS, normalized)
	candidate = normalize(candidate)
	if err := candidate.Validate(); err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	if _, err := DecompileCustomPolicy(candidate); err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	return candidate, nil
}

func validateCustomRule(rule CustomRule) error {
	if rule.Name == "" || strings.TrimSpace(rule.Name) != rule.Name || !validText(rule.Name) ||
		!utf8.ValidString(rule.Name) || len([]byte(rule.Name)) > MaxCustomNameBytes ||
		utf8.RuneCountInString(rule.Name) > MaxCustomNameRunes {
		return ErrInvalidCustomPolicy
	}
	if len(rule.Domains) > MaxListItems {
		return ErrInvalidCustomPolicy
	}
	if err := validateUniqueValues(rule.Domains, validateCustomDomainExpression); err != nil {
		return err
	}
	if len(rule.IPs) > MaxListItems {
		return ErrInvalidCustomPolicy
	}
	if err := validateUniqueValues(rule.IPs, func(value string) error { return validateIPExpression(value) }); err != nil {
		return err
	}
	if len(rule.Protocols) > MaxSelectors {
		return ErrInvalidCustomPolicy
	}
	if err := validateUniqueValues(rule.Protocols, func(value string) error {
		switch value {
		case "bittorrent", "http", "tls", "quic", "utp":
			return nil
		default:
			return ErrInvalidCustomPolicy
		}
	}); err != nil {
		return err
	}
	if len(rule.Networks) > 2 {
		return ErrInvalidCustomPolicy
	}
	if err := validateUniqueValues(rule.Networks, func(value string) error {
		if value != "tcp" && value != "udp" {
			return ErrInvalidCustomPolicy
		}
		return nil
	}); err != nil {
		return err
	}
	if len(rule.Ports) > MaxPortRanges {
		return ErrInvalidCustomPolicy
	}
	seenPorts := make(map[PortRange]struct{}, len(rule.Ports))
	for _, port := range rule.Ports {
		if port.To == 0 {
			port.To = port.From
		}
		if port.From < 1 || port.To < port.From || port.To > 65535 {
			return ErrInvalidCustomPolicy
		}
		if _, exists := seenPorts[port]; exists {
			return ErrInvalidCustomPolicy
		}
		seenPorts[port] = struct{}{}
	}
	switch rule.Action {
	case CustomRuleDirect, CustomRuleProxy, CustomRuleBlock:
	default:
		return ErrInvalidCustomPolicy
	}
	return nil
}

func validateCustomDomainExpression(value string) error {
	if !strings.HasPrefix(value, "ext:") {
		return validateMatchExpression(value, "domain")
	}
	parts := strings.Split(strings.TrimPrefix(value, "ext:"), ":")
	if len(parts) != 2 || !customDomainExtFilenamePattern.MatchString(parts[0]) || !customDomainExtTagPattern.MatchString(parts[1]) {
		return ErrInvalidCustomPolicy
	}
	if _, ok := managedCustomDomainExtAssets()[parts[0]]; !ok {
		return ErrInvalidCustomPolicy
	}
	return nil
}

func managedCustomDomainExtAssets() map[string]struct{} {
	customDomainExtAssetsOnce.Do(func() {
		// The embedded product default is the appliance policy's source-owned
		// managed geosite catalog. Deriving the names from it keeps this broker
		// closed to operator-selected files without duplicating component data.
		assets := make(map[string]struct{})
		baseline := ProductDefault()
		add := func(expression string) {
			if !strings.HasPrefix(expression, "ext:") {
				return
			}
			parts := strings.Split(strings.TrimPrefix(expression, "ext:"), ":")
			if len(parts) == 2 && customDomainExtFilenamePattern.MatchString(parts[0]) && customDomainExtTagPattern.MatchString(parts[1]) {
				assets[parts[0]] = struct{}{}
			}
		}
		for _, rule := range baseline.Routing.Rules {
			for _, expression := range rule.Domain {
				add(expression)
			}
		}
		for _, server := range baseline.DNS.Servers {
			for _, expression := range server.Domains {
				add(expression)
			}
		}
		customDomainExtAssets = assets
	})
	return customDomainExtAssets
}

func validateUniqueValues(values []string, validate func(string) error) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validate(value); err != nil {
			return ErrInvalidCustomPolicy
		}
		if _, exists := seen[value]; exists {
			return ErrInvalidCustomPolicy
		}
		seen[value] = struct{}{}
	}
	return nil
}

func customRuleEnvelopeFor(value Appliance) (customRuleEnvelope, error) {
	if len(value.Routing.Rules) == 0 || len(value.Routing.Balancers) != 1 {
		return customRuleEnvelope{}, ErrCustomPolicyDrift
	}
	final := value.Routing.Rules[len(value.Routing.Rules)-1]
	balancer := value.Routing.Balancers[0]
	if final.Type == "" || len(final.InboundTag) == 0 || final.Action.OutboundTag == "" ||
		balancer.Tag == "" || balancer.FallbackTag == "" {
		return customRuleEnvelope{}, ErrCustomPolicyDrift
	}
	return customRuleEnvelope{
		typeName:    final.Type,
		inboundTags: append([]string{}, final.InboundTag...),
		direct:      final.Action.OutboundTag,
		proxy:       balancer.Tag,
		block:       balancer.FallbackTag,
	}, nil
}

func decodeCustomRuntimeRule(rule RoutingRule, envelope customRuleEnvelope) (CustomRule, error) {
	name, err := decodeCustomRuleName(rule.RuleTag)
	if err != nil {
		return CustomRule{}, err
	}
	result := CustomRule{
		Name:      name,
		Domains:   append([]string(nil), rule.Domain...),
		IPs:       append([]string(nil), rule.IP...),
		Protocols: append([]string(nil), rule.Protocol...),
		Networks:  append([]string(nil), rule.Network...),
		Ports:     append([]PortRange(nil), rule.Ports...),
	}
	switch {
	case rule.Action.OutboundTag == envelope.direct && rule.Action.BalancerTag == "":
		result.Action = CustomRuleDirect
	case rule.Action.OutboundTag == envelope.block && rule.Action.BalancerTag == "":
		result.Action = CustomRuleBlock
	case rule.Action.OutboundTag == "" && rule.Action.BalancerTag == envelope.proxy:
		result.Action = CustomRuleProxy
	default:
		return CustomRule{}, ErrCustomPolicyDrift
	}
	result = normalizeCustomRule(result)
	if err := validateCustomRule(result); err != nil {
		return CustomRule{}, err
	}
	expected := compileCustomRuntimeRule(result, envelope)
	if !reflect.DeepEqual(rule, expected) {
		return CustomRule{}, ErrCustomPolicyDrift
	}
	return result, nil
}

func compileCustomRuntimeRule(rule CustomRule, envelope customRuleEnvelope) RoutingRule {
	rule = normalizeCustomRule(rule)
	result := RoutingRule{
		Type:       envelope.typeName,
		InboundTag: append([]string{}, envelope.inboundTags...),
		IP:         append([]string{}, rule.IPs...),
		Domain:     append([]string{}, rule.Domains...),
		Protocol:   append([]string{}, rule.Protocols...),
		Network:    append([]string{}, rule.Networks...),
		Ports:      append([]PortRange{}, rule.Ports...),
		RuleTag:    encodeCustomRuleName(rule.Name),
	}
	switch rule.Action {
	case CustomRuleDirect:
		result.Action.OutboundTag = envelope.direct
	case CustomRuleProxy:
		result.Action.BalancerTag = envelope.proxy
	case CustomRuleBlock:
		result.Action.OutboundTag = envelope.block
	}
	return result
}

func encodeCustomRuleName(name string) string {
	return CustomRuleTagPrefix + hex.EncodeToString([]byte(name))
}

func decodeCustomRuleName(tag string) (string, error) {
	if !strings.HasPrefix(tag, CustomRuleTagPrefix) {
		return "", ErrCustomPolicyDrift
	}
	encoded := strings.TrimPrefix(tag, CustomRuleTagPrefix)
	if encoded == "" || len(encoded)%2 != 0 {
		return "", ErrCustomPolicyDrift
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil || !utf8.Valid(decoded) {
		return "", ErrCustomPolicyDrift
	}
	name := string(decoded)
	if err := validateCustomRule(CustomRule{Name: name, Action: CustomRuleDirect}); err != nil {
		return "", ErrCustomPolicyDrift
	}
	if encodeCustomRuleName(name) != tag {
		return "", ErrCustomPolicyDrift
	}
	return name, nil
}

func validateCustomDNS(current, baseline DNSPolicy, rules []CustomRule) (int, int, int, error) {
	if len(current.Servers) != len(baseline.Servers) {
		return 0, 0, 0, ErrCustomPolicyDrift
	}
	currentWithoutServers := current
	currentWithoutServers.Servers = nil
	baselineWithoutServers := baseline
	baselineWithoutServers.Servers = nil
	if !reflect.DeepEqual(currentWithoutServers, baselineWithoutServers) {
		return 0, 0, 0, ErrCustomPolicyDrift
	}
	derived := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Action != CustomRuleProxy {
			continue
		}
		for _, domain := range rule.Domains {
			derived[domain] = struct{}{}
		}
	}
	baselineDomainSet := make(map[string]struct{})
	proxyTag := ""
	for _, server := range baseline.Servers {
		if server.Tag == "" {
			continue
		}
		if proxyTag != "" && proxyTag != server.Tag {
			return 0, 0, 0, ErrCustomPolicyDrift
		}
		proxyTag = server.Tag
		for _, domain := range server.Domains {
			if _, exists := baselineDomainSet[domain]; exists {
				continue
			}
			baselineDomainSet[domain] = struct{}{}
		}
	}
	if proxyTag == "" {
		return 0, 0, 0, ErrCustomPolicyDrift
	}
	proxyResolvers := 0
	for index, expected := range baseline.Servers {
		actual := current.Servers[index]
		if expected.Tag != proxyTag {
			if !reflect.DeepEqual(actual, expected) {
				return 0, 0, 0, ErrCustomPolicyDrift
			}
			continue
		}
		proxyResolvers++
		expectedDomains := append([]string(nil), expected.Domains...)
		additional := make([]string, 0, len(derived))
		for domain := range derived {
			if _, isBaseline := baselineDomainSet[domain]; !isBaseline {
				additional = append(additional, domain)
			}
		}
		sort.Strings(additional)
		expectedDomains = append(expectedDomains, additional...)
		withoutDomains := expected
		withoutDomains.Domains = nil
		actualWithoutDomains := actual
		actualWithoutDomains.Domains = nil
		if !reflect.DeepEqual(actualWithoutDomains, withoutDomains) || !reflect.DeepEqual(actual.Domains, expectedDomains) {
			return 0, 0, 0, ErrCustomPolicyDrift
		}
	}
	if proxyResolvers == 0 {
		return 0, 0, 0, ErrCustomPolicyDrift
	}
	derivedCount := 0
	for domain := range derived {
		if _, isBaseline := baselineDomainSet[domain]; !isBaseline {
			derivedCount++
		}
	}
	return proxyResolvers, len(baselineDomainSet), derivedCount, nil
}

func rebuildCustomDNS(baseline DNSPolicy, rules []CustomRule) DNSPolicy {
	result := cloneDNSPolicy(baseline)
	derived := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Action != CustomRuleProxy {
			continue
		}
		for _, domain := range rule.Domains {
			derived[domain] = struct{}{}
		}
	}
	baselineDomainSet := make(map[string]struct{})
	proxyTag := ""
	for _, server := range baseline.Servers {
		if server.Tag == "" {
			continue
		}
		proxyTag = server.Tag
		for _, domain := range server.Domains {
			baselineDomainSet[domain] = struct{}{}
		}
	}
	for index, server := range result.Servers {
		if server.Tag != proxyTag {
			continue
		}
		additional := make([]string, 0, len(derived))
		for domain := range derived {
			if _, exists := baselineDomainSet[domain]; !exists {
				additional = append(additional, domain)
			}
		}
		sort.Strings(additional)
		result.Servers[index].Domains = append(append([]string(nil), server.Domains...), additional...)
	}
	return result
}

func normalizeCustomRules(rules []CustomRule) []CustomRule {
	result := make([]CustomRule, len(rules))
	for index, rule := range rules {
		result[index] = normalizeCustomRule(rule)
	}
	return result
}

func normalizeCustomRule(rule CustomRule) CustomRule {
	rule.Domains = append([]string{}, rule.Domains...)
	rule.IPs = append([]string{}, rule.IPs...)
	rule.Protocols = append([]string{}, rule.Protocols...)
	rule.Networks = append([]string{}, rule.Networks...)
	rule.Ports = append([]PortRange{}, rule.Ports...)
	sort.Strings(rule.Domains)
	sort.Strings(rule.IPs)
	sort.Strings(rule.Protocols)
	sort.Strings(rule.Networks)
	for index := range rule.Ports {
		if rule.Ports[index].To == 0 {
			rule.Ports[index].To = rule.Ports[index].From
		}
	}
	sort.Slice(rule.Ports, func(left, right int) bool {
		if rule.Ports[left].From != rule.Ports[right].From {
			return rule.Ports[left].From < rule.Ports[right].From
		}
		return rule.Ports[left].To < rule.Ports[right].To
	})
	return rule
}

func cloneCustomRules(rules []CustomRule) []CustomRule {
	return normalizeCustomRules(rules)
}

func cloneRoutingRules(rules []RoutingRule) []RoutingRule {
	result := make([]RoutingRule, len(rules))
	for index, rule := range rules {
		result[index] = rule
		result[index].InboundTag = append([]string{}, rule.InboundTag...)
		result[index].IP = append([]string{}, rule.IP...)
		result[index].Domain = append([]string{}, rule.Domain...)
		result[index].Protocol = append([]string{}, rule.Protocol...)
		result[index].Network = append([]string{}, rule.Network...)
		result[index].Ports = append([]PortRange{}, rule.Ports...)
	}
	return result
}

func cloneBalancers(values []Balancer) []Balancer {
	result := make([]Balancer, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Selector = append([]string{}, value.Selector...)
	}
	return result
}

func cloneDNSPolicy(value DNSPolicy) DNSPolicy {
	result := value
	result.Servers = make([]DNSServer, len(value.Servers))
	for index, server := range value.Servers {
		result.Servers[index] = server
		result.Servers[index].Domains = append([]string{}, server.Domains...)
	}
	return result
}
