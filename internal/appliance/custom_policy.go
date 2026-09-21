package appliance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strconv"
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

	FallbackModeSystem   = "system"
	FallbackModeDisabled = "disabled"

	MinStaleTTLSeconds = 60
	MaxStaleTTLSeconds = 24 * 60 * 60

	MinObservatoryIntervalMinutes = 1
	MaxObservatoryIntervalMinutes = 5
)

var (
	ErrCustomPolicyDrift   = errors.New("custom routing policy drift detected")
	ErrInvalidCustomPolicy = errors.New("custom routing policy is invalid")
	ErrManagedPolicyDrift  = errors.New("managed appliance policy drift detected")
	ErrInvalidDNSSettings  = errors.New("DNS settings are invalid")
	ErrInvalidObservatory  = errors.New("Observatory settings are invalid")
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

// DNSSettings is the closed DNS projection accepted by the DNS/Observatory
// broker. Resolver IDs identify only source-owned ProductDefault definitions;
// addresses, tags and domains never cross the broker boundary.
type DNSSettings struct {
	ProxyResolverIDs []string `json:"proxyResolverIds"`
	FallbackMode     string   `json:"fallbackMode"`
	CacheEnabled     bool     `json:"cacheEnabled"`
	ServeStale       bool     `json:"serveStale"`
	StaleTTLSeconds  int      `json:"staleTTLSeconds"`
	ParallelQueries  bool     `json:"parallelQueries"`
}

// ObservatorySettings is the only editable Observatory projection in this
// slice. Runtime URL, selector and concurrency remain source-owned.
type ObservatorySettings struct {
	ProbeIntervalMinutes int `json:"probeIntervalMinutes"`
}

// DNSResolverOption is the safe selectable resolver catalog entry. The ID is
// an opaque request identifier and the label contains no endpoint material.
type DNSResolverOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ManagedPolicy is the common classified envelope shared by the Routing and
// DNS/Observatory brokers. It contains typed projections only; protected
// source-owned fields remain validated by the classifier and are not exposed
// as request data.
type ManagedPolicy struct {
	CustomPolicy
	DNS         DNSSettings
	Observatory ObservatorySettings
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

// DecompileCustomPolicy returns the routing projection from the shared managed
// appliance-policy envelope. Keeping this wrapper preserves the Issue #83 API
// while making DNS/Observatory compatibility common to both brokers.
func DecompileCustomPolicy(value Appliance) (CustomPolicy, error) {
	managed, err := DecompileManagedPolicy(value)
	if err != nil {
		return CustomPolicy{}, ErrCustomPolicyDrift
	}
	return managed.CustomPolicy, nil
}

// DecompileManagedPolicy accepts only the closed source-owned envelope. Any
// protected drift, manual extra rule, malformed reserved tag, DNS asymmetry or
// unsupported Observatory cadence is rejected rather than normalized into
// editable state.
func DecompileManagedPolicy(value Appliance) (ManagedPolicy, error) {
	if err := value.Validate(); err != nil {
		return ManagedPolicy{}, ErrManagedPolicyDrift
	}
	baseline := ProductDefault()
	if err := baseline.Validate(); err != nil {
		return ManagedPolicy{}, ErrManagedPolicyDrift
	}
	rules, prefixCount, err := decompileCustomRouting(value, baseline)
	if err != nil {
		return ManagedPolicy{}, err
	}
	proxyResolvers, baselineDomains, derivedDomains, err := validateCustomDNS(value.DNS, baseline.DNS, rules)
	if err != nil {
		return ManagedPolicy{}, ErrManagedPolicyDrift
	}
	dns, err := decompileDNSSettings(value.DNS, baseline.DNS, rules)
	if err != nil {
		return ManagedPolicy{}, ErrManagedPolicyDrift
	}
	observatory, err := decompileObservatorySettings(value.Observatory, baseline.Observatory)
	if err != nil {
		return ManagedPolicy{}, ErrManagedPolicyDrift
	}
	return ManagedPolicy{
		CustomPolicy: CustomPolicy{
			Rules:                       cloneCustomRules(rules),
			ProtectedRuleCount:          len(baseline.Routing.Rules),
			ProtectedPrefixRuleCount:    prefixCount,
			CustomRegionRuleCount:       len(rules),
			ProxyDNSResolverCount:       proxyResolvers,
			ProxyDNSBaselineDomainCount: baselineDomains,
			ProxyDNSDerivedDomainCount:  derivedDomains,
		},
		DNS:         dns,
		Observatory: observatory,
	}, nil
}

func decompileCustomRouting(value, baseline Appliance) ([]CustomRule, int, error) {
	if value.SchemaVersion != baseline.SchemaVersion ||
		value.Routing.DomainStrategy != baseline.Routing.DomainStrategy ||
		value.Routing.DomainMatcher != baseline.Routing.DomainMatcher ||
		!reflect.DeepEqual(value.Routing.Balancers, baseline.Routing.Balancers) {
		return nil, 0, ErrCustomPolicyDrift
	}
	envelope, err := customRuleEnvelopeFor(baseline)
	if err != nil {
		return nil, 0, ErrCustomPolicyDrift
	}

	if len(baseline.Routing.Rules) < 1 {
		return nil, 0, ErrCustomPolicyDrift
	}
	prefixCount := len(baseline.Routing.Rules) - 1
	if len(value.Routing.Rules) < prefixCount+1 {
		return nil, 0, ErrCustomPolicyDrift
	}
	for index := 0; index < prefixCount; index++ {
		if !reflect.DeepEqual(value.Routing.Rules[index], baseline.Routing.Rules[index]) {
			return nil, 0, ErrCustomPolicyDrift
		}
	}
	if !reflect.DeepEqual(value.Routing.Rules[len(value.Routing.Rules)-1], baseline.Routing.Rules[len(baseline.Routing.Rules)-1]) {
		return nil, 0, ErrCustomPolicyDrift
	}

	rules := make([]CustomRule, 0, len(value.Routing.Rules)-prefixCount-1)
	for _, runtimeRule := range value.Routing.Rules[prefixCount : len(value.Routing.Rules)-1] {
		decoded, err := decodeCustomRuntimeRule(runtimeRule, envelope)
		if err != nil {
			return nil, 0, ErrCustomPolicyDrift
		}
		rules = append(rules, decoded)
	}
	if err := ValidateCustomRules(rules); err != nil {
		return nil, 0, ErrCustomPolicyDrift
	}
	return rules, prefixCount, nil
}

// CompileCustomRules replaces only the authorized custom region and rebuilds
// proxy-DNS domains from the embedded baseline plus the complete candidate
// list. Supported DNS/Observatory settings remain unchanged.
func CompileCustomRules(value Appliance, rules []CustomRule) (Appliance, error) {
	if err := ValidateCustomRules(rules); err != nil {
		return Appliance{}, err
	}
	managed, err := DecompileManagedPolicy(value)
	if err != nil {
		return Appliance{}, ErrCustomPolicyDrift
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
	candidate.DNS, err = compileManagedDNS(baseline.DNS, managed.DNS, normalized)
	if err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	candidate = normalize(candidate)
	if err := candidate.Validate(); err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	if _, err := DecompileManagedPolicy(candidate); err != nil {
		return Appliance{}, ErrInvalidCustomPolicy
	}
	return candidate, nil
}

// CompileDNSObservatory preserves the complete classified custom-routing
// projection and changes only the closed DNS/Observatory editable fields.
func CompileDNSObservatory(value Appliance, dns DNSSettings, observatory ObservatorySettings) (Appliance, error) {
	if err := ValidateDNSSettings(dns); err != nil {
		return Appliance{}, err
	}
	if err := ValidateObservatorySettings(observatory); err != nil {
		return Appliance{}, err
	}
	managed, err := DecompileManagedPolicy(value)
	if err != nil {
		return Appliance{}, err
	}
	baseline := ProductDefault()
	candidate, err := cloneAppliance(value)
	if err != nil {
		return Appliance{}, ErrInvalidDNSSettings
	}
	candidate.DNS, err = compileManagedDNS(baseline.DNS, dns, managed.Rules)
	if err != nil {
		return Appliance{}, ErrInvalidDNSSettings
	}
	candidate.Observatory = ObservatoryPolicy{
		SubjectSelector: cloneStrings(baseline.Observatory.SubjectSelector),
		ProbeInterval:   strconv.Itoa(observatory.ProbeIntervalMinutes) + "m",
	}
	candidate = normalize(candidate)
	if err := candidate.Validate(); err != nil {
		return Appliance{}, ErrInvalidDNSSettings
	}
	if _, err := DecompileManagedPolicy(candidate); err != nil {
		return Appliance{}, ErrInvalidDNSSettings
	}
	return candidate, nil
}

func cloneAppliance(value Appliance) (Appliance, error) {
	contents, err := MarshalCanonical(value)
	if err != nil {
		return Appliance{}, err
	}
	return Parse(contents)
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

type managedResolverDefinition struct {
	option DNSResolverOption
	server DNSServer
}

func DNSResolverCatalog() []DNSResolverOption {
	definitions, err := managedResolverDefinitions(ProductDefault().DNS)
	if err != nil {
		return []DNSResolverOption{}
	}
	result := make([]DNSResolverOption, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition.option)
	}
	return result
}

// ResolverCatalog is an explicit alias for callers that do not need to know
// that the catalog is DNS-specific at the source boundary.
func ResolverCatalog() []DNSResolverOption {
	return DNSResolverCatalog()
}

func ValidateDNSSettings(settings DNSSettings) error {
	definitions, err := managedResolverDefinitions(ProductDefault().DNS)
	if err != nil {
		return ErrInvalidDNSSettings
	}
	return validateDNSSettingsAgainst(definitions, settings)
}

func ValidateObservatorySettings(settings ObservatorySettings) error {
	if settings.ProbeIntervalMinutes < MinObservatoryIntervalMinutes || settings.ProbeIntervalMinutes > MaxObservatoryIntervalMinutes {
		return ErrInvalidObservatory
	}
	return nil
}

func validateDNSSettingsAgainst(definitions []managedResolverDefinition, settings DNSSettings) error {
	if len(settings.ProxyResolverIDs) < 1 || len(settings.ProxyResolverIDs) > len(definitions) {
		return ErrInvalidDNSSettings
	}
	known := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		known[definition.option.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(settings.ProxyResolverIDs))
	for _, id := range settings.ProxyResolverIDs {
		if _, ok := known[id]; !ok {
			return ErrInvalidDNSSettings
		}
		if _, ok := seen[id]; ok {
			return ErrInvalidDNSSettings
		}
		seen[id] = struct{}{}
	}
	switch settings.FallbackMode {
	case FallbackModeSystem, FallbackModeDisabled:
	default:
		return ErrInvalidDNSSettings
	}
	if !settings.CacheEnabled {
		if settings.ServeStale || settings.StaleTTLSeconds != 0 {
			return ErrInvalidDNSSettings
		}
	} else if !settings.ServeStale {
		if settings.StaleTTLSeconds != 0 {
			return ErrInvalidDNSSettings
		}
	} else if settings.StaleTTLSeconds < MinStaleTTLSeconds || settings.StaleTTLSeconds > MaxStaleTTLSeconds {
		return ErrInvalidDNSSettings
	}
	return nil
}

func managedResolverDefinitions(baseline DNSPolicy) ([]managedResolverDefinition, error) {
	if len(baseline.Servers) < 2 {
		return nil, ErrManagedPolicyDrift
	}
	last := baseline.Servers[len(baseline.Servers)-1]
	if last.Address != "localhost" || len(last.Domains) != 0 || last.SkipFallback || last.Tag != "" || last.QueryStrategy != "" {
		return nil, ErrManagedPolicyDrift
	}
	definitions := make([]managedResolverDefinition, 0, len(baseline.Servers)-1)
	seenIDs := make(map[string]struct{}, len(baseline.Servers))
	for index, server := range baseline.Servers[:len(baseline.Servers)-1] {
		if server.Tag != "dns-proxy" || !server.SkipFallback || server.QueryStrategy != "UseIPv4" || server.Address == "localhost" {
			return nil, ErrManagedPolicyDrift
		}
		id := resolverID(server)
		if _, exists := seenIDs[id]; exists {
			return nil, ErrManagedPolicyDrift
		}
		seenIDs[id] = struct{}{}
		definitions = append(definitions, managedResolverDefinition{
			option: DNSResolverOption{ID: id, Label: "Proxy resolver " + strconv.Itoa(index+1)},
			server: cloneDNSServer(server),
		})
	}
	if len(definitions) == 0 {
		return nil, ErrManagedPolicyDrift
	}
	return definitions, nil
}

func resolverID(server DNSServer) string {
	digest := sha256.Sum256([]byte(server.Address + "\x00" + server.Tag))
	return "resolver-" + hex.EncodeToString(digest[:6])
}

func decompileDNSSettings(current, baseline DNSPolicy, rules []CustomRule) (DNSSettings, error) {
	settings, _, _, _, err := classifyManagedDNS(current, baseline, rules)
	return settings, err
}

func classifyManagedDNS(current, baseline DNSPolicy, rules []CustomRule) (DNSSettings, int, int, int, error) {
	if current.QueryStrategy != baseline.QueryStrategy || current.DisableFallbackIfMatch != baseline.DisableFallbackIfMatch || current.UseSystemHosts != baseline.UseSystemHosts {
		return DNSSettings{}, 0, 0, 0, ErrManagedPolicyDrift
	}
	definitions, err := managedResolverDefinitions(baseline)
	if err != nil {
		return DNSSettings{}, 0, 0, 0, err
	}
	if len(current.Servers) < 2 || len(current.Servers) > len(definitions)+1 {
		return DNSSettings{}, 0, 0, 0, ErrManagedPolicyDrift
	}
	if !reflect.DeepEqual(current.Servers[len(current.Servers)-1], baseline.Servers[len(baseline.Servers)-1]) {
		return DNSSettings{}, 0, 0, 0, ErrManagedPolicyDrift
	}
	baselineDomains, baselineDomainSet, err := baselineProxyDomains(definitions)
	if err != nil {
		return DNSSettings{}, 0, 0, 0, err
	}
	_, derivedDomainSet := customProxyDomains(rules)
	additional := make([]string, 0, len(derivedDomainSet))
	for domain := range derivedDomainSet {
		if _, exists := baselineDomainSet[domain]; !exists {
			additional = append(additional, domain)
		}
	}
	sort.Strings(additional)
	expectedDomains := append(append([]string{}, baselineDomains...), additional...)

	byIdentity := make(map[string]managedResolverDefinition, len(definitions))
	for _, definition := range definitions {
		byIdentity[resolverIdentity(definition.server)] = definition
	}
	selected := make([]string, 0, len(current.Servers)-1)
	seen := make(map[string]struct{}, len(current.Servers)-1)
	for _, actual := range current.Servers[:len(current.Servers)-1] {
		definition, exists := byIdentity[resolverIdentity(actual)]
		if !exists || !reflect.DeepEqual(actual.Domains, expectedDomains) {
			return DNSSettings{}, 0, 0, 0, ErrManagedPolicyDrift
		}
		if _, exists := seen[definition.option.ID]; exists {
			return DNSSettings{}, 0, 0, 0, ErrManagedPolicyDrift
		}
		seen[definition.option.ID] = struct{}{}
		selected = append(selected, definition.option.ID)
	}
	settings := DNSSettings{
		ProxyResolverIDs: selected,
		FallbackMode:     FallbackModeSystem,
		CacheEnabled:     !current.DisableCache,
		ServeStale:       current.ServeStale,
		StaleTTLSeconds:  current.ServeExpiredTTL,
		ParallelQueries:  current.EnableParallelQuery,
	}
	if current.DisableFallback {
		settings.FallbackMode = FallbackModeDisabled
	}
	if err := validateDNSSettingsAgainst(definitions, settings); err != nil {
		return DNSSettings{}, 0, 0, 0, ErrManagedPolicyDrift
	}
	derivedCount := 0
	for domain := range derivedDomainSet {
		if _, isBaseline := baselineDomainSet[domain]; !isBaseline {
			derivedCount++
		}
	}
	return settings, len(selected), len(baselineDomainSet), derivedCount, nil
}

func baselineProxyDomains(definitions []managedResolverDefinition) ([]string, map[string]struct{}, error) {
	ordered := make([]string, 0)
	seen := make(map[string]struct{})
	for _, definition := range definitions {
		for _, domain := range definition.server.Domains {
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			ordered = append(ordered, domain)
		}
	}
	if len(ordered) == 0 {
		return nil, nil, ErrManagedPolicyDrift
	}
	return ordered, seen, nil
}

func customProxyDomains(rules []CustomRule) ([]string, map[string]struct{}) {
	ordered := make([]string, 0)
	seen := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Action != CustomRuleProxy {
			continue
		}
		for _, domain := range rule.Domains {
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			ordered = append(ordered, domain)
		}
	}
	return ordered, seen
}

func validateCustomDNS(current, baseline DNSPolicy, rules []CustomRule) (int, int, int, error) {
	_, resolverCount, baselineCount, derivedCount, err := classifyManagedDNS(current, baseline, rules)
	return resolverCount, baselineCount, derivedCount, err
}

func compileManagedDNS(baseline DNSPolicy, settings DNSSettings, rules []CustomRule) (DNSPolicy, error) {
	definitions, err := managedResolverDefinitions(baseline)
	if err != nil {
		return DNSPolicy{}, ErrInvalidDNSSettings
	}
	if err := validateDNSSettingsAgainst(definitions, settings); err != nil {
		return DNSPolicy{}, err
	}
	baselineDomains, _, err := baselineProxyDomains(definitions)
	if err != nil {
		return DNSPolicy{}, ErrInvalidDNSSettings
	}
	_, derivedDomainSet := customProxyDomains(rules)
	additional := make([]string, 0, len(derivedDomainSet))
	baselineDomainSet := make(map[string]struct{}, len(baselineDomains))
	for _, domain := range baselineDomains {
		baselineDomainSet[domain] = struct{}{}
	}
	for domain := range derivedDomainSet {
		if _, exists := baselineDomainSet[domain]; !exists {
			additional = append(additional, domain)
		}
	}
	sort.Strings(additional)
	expectedDomains := append(append([]string{}, baselineDomains...), additional...)
	byID := make(map[string]managedResolverDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.option.ID] = definition
	}
	result := DNSPolicy{
		QueryStrategy:          baseline.QueryStrategy,
		DisableCache:           !settings.CacheEnabled,
		ServeStale:             settings.ServeStale,
		ServeExpiredTTL:        settings.StaleTTLSeconds,
		DisableFallback:        settings.FallbackMode == FallbackModeDisabled,
		DisableFallbackIfMatch: baseline.DisableFallbackIfMatch,
		EnableParallelQuery:    settings.ParallelQueries,
		UseSystemHosts:         baseline.UseSystemHosts,
		Servers:                make([]DNSServer, 0, len(settings.ProxyResolverIDs)+1),
	}
	for _, id := range settings.ProxyResolverIDs {
		definition, exists := byID[id]
		if !exists {
			return DNSPolicy{}, ErrInvalidDNSSettings
		}
		server := cloneDNSServer(definition.server)
		server.Domains = append([]string{}, expectedDomains...)
		result.Servers = append(result.Servers, server)
	}
	result.Servers = append(result.Servers, cloneDNSServer(baseline.Servers[len(baseline.Servers)-1]))
	return result, nil
}

func decompileObservatorySettings(current, baseline ObservatoryPolicy) (ObservatorySettings, error) {
	if !reflect.DeepEqual(current.SubjectSelector, baseline.SubjectSelector) {
		return ObservatorySettings{}, ErrManagedPolicyDrift
	}
	minutes, ok := parseObservatoryMinutes(current.ProbeInterval)
	if !ok {
		return ObservatorySettings{}, ErrManagedPolicyDrift
	}
	return ObservatorySettings{ProbeIntervalMinutes: minutes}, nil
}

func parseObservatoryMinutes(value string) (int, bool) {
	if len(value) < 2 || !strings.HasSuffix(value, "m") {
		return 0, false
	}
	minutes, err := strconv.Atoi(strings.TrimSuffix(value, "m"))
	if err != nil || strconv.Itoa(minutes)+"m" != value || minutes < MinObservatoryIntervalMinutes || minutes > MaxObservatoryIntervalMinutes {
		return 0, false
	}
	return minutes, true
}

func resolverIdentity(server DNSServer) string {
	return server.Address + "\x00" + server.Tag + "\x00" + strconv.FormatBool(server.SkipFallback) + "\x00" + server.QueryStrategy
}

func cloneDNSServer(server DNSServer) DNSServer {
	server.Domains = append([]string{}, server.Domains...)
	return server
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

func cloneStrings(values []string) []string {
	return append([]string{}, values...)
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
