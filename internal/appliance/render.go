package appliance

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/popiposter/xkeen-control/internal/nodes"
)

// Compatibility templates are embedded because the appliance binary must be
// able to render a complete candidate on a router without a repository
// checkout. They are fixed product inputs, not editable appliance state.
//
//go:embed templates/01_log.json templates/03_inbounds.json templates/06_policy.json templates/08_api.json templates/xkeen.json
var compatibilityTemplates embed.FS

var fixedTemplatePaths = []string{
	"xray/01_log.json",
	"xray/03_inbounds.json",
	"xray/06_policy.json",
	"xray/08_api.json",
	"xkeen/xkeen.json",
}

type dnsDocument struct {
	DNS dnsRuntime `json:"dns"`
}

type dnsRuntime struct {
	Servers                []DNSServer `json:"servers"`
	QueryStrategy          string      `json:"queryStrategy"`
	DisableCache           bool        `json:"disableCache"`
	ServeStale             bool        `json:"serveStale"`
	ServeExpiredTTL        int         `json:"serveExpiredTTL"`
	DisableFallback        bool        `json:"disableFallback"`
	DisableFallbackIfMatch bool        `json:"disableFallbackIfMatch"`
	EnableParallelQuery    bool        `json:"enableParallelQuery"`
	UseSystemHosts         bool        `json:"useSystemHosts"`
}

type routingDocument struct {
	Routing routingRuntime `json:"routing"`
}

type routingRuntime struct {
	DomainStrategy string               `json:"domainStrategy"`
	DomainMatcher  string               `json:"domainMatcher,omitempty"`
	Rules          []routingRuleRuntime `json:"rules"`
	Balancers      []balancerRuntime    `json:"balancers"`
}

type routingRuleRuntime struct {
	Type        string   `json:"type,omitempty"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	Network     string   `json:"network,omitempty"`
	Port        string   `json:"port,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
	RuleTag     string   `json:"ruleTag,omitempty"`
}

type balancerRuntime struct {
	Tag         string           `json:"tag"`
	Selector    []string         `json:"selector"`
	FallbackTag string           `json:"fallbackTag"`
	Strategy    BalancerStrategy `json:"strategy"`
}

type observatoryDocument struct {
	Observatory observatoryRuntime `json:"observatory"`
}

type observatoryRuntime struct {
	SubjectSelector   []string `json:"subjectSelector"`
	ProbeURL          string   `json:"probeUrl"`
	ProbeInterval     string   `json:"probeInterval"`
	EnableConcurrency bool     `json:"enableConcurrency"`
}

func parseActivePolicy(configDir string) (Appliance, error) {
	dns, err := readRegularFile(filepath.Join(configDir, "02_dns.json"), MaxDocumentSize)
	if err != nil {
		return Appliance{}, errors.New("active DNS policy is unavailable")
	}
	routing, err := readRegularFile(filepath.Join(configDir, "05_routing.json"), MaxDocumentSize)
	if err != nil {
		return Appliance{}, errors.New("active routing policy is unavailable")
	}
	observatory, err := readRegularFile(filepath.Join(configDir, "07_observatory.json"), MaxDocumentSize)
	if err != nil {
		return Appliance{}, errors.New("active Observatory policy is unavailable")
	}
	return parseActivePolicyBytes(dns, routing, observatory)
}

func parseActivePolicyBytes(dns, routing, observatory []byte) (Appliance, error) {
	var dnsValue dnsDocument
	if err := decodeStrict(dns, &dnsValue); err != nil {
		return Appliance{}, errors.New("active DNS policy is invalid")
	}
	var routingValue routingDocument
	if err := decodeStrict(routing, &routingValue); err != nil {
		return Appliance{}, errors.New("active routing policy is invalid")
	}
	var observatoryValue observatoryDocument
	if err := decodeStrict(observatory, &observatoryValue); err != nil {
		return Appliance{}, errors.New("active Observatory policy is invalid")
	}

	probeURL, enableConcurrency := defaultObservatoryFields()
	if observatoryValue.Observatory.ProbeURL != probeURL || observatoryValue.Observatory.EnableConcurrency != enableConcurrency {
		return Appliance{}, errors.New("active Observatory compatibility fields differ")
	}

	result := Appliance{
		SchemaVersion: SchemaVersion,
		DNS: DNSPolicy{
			Servers:                dnsValue.DNS.Servers,
			QueryStrategy:          dnsValue.DNS.QueryStrategy,
			DisableCache:           dnsValue.DNS.DisableCache,
			ServeStale:             dnsValue.DNS.ServeStale,
			ServeExpiredTTL:        dnsValue.DNS.ServeExpiredTTL,
			DisableFallback:        dnsValue.DNS.DisableFallback,
			DisableFallbackIfMatch: dnsValue.DNS.DisableFallbackIfMatch,
			EnableParallelQuery:    dnsValue.DNS.EnableParallelQuery,
			UseSystemHosts:         dnsValue.DNS.UseSystemHosts,
		},
		Routing: RoutingPolicy{
			DomainStrategy: routingValue.Routing.DomainStrategy,
			DomainMatcher:  routingValue.Routing.DomainMatcher,
			Rules:          make([]RoutingRule, 0, len(routingValue.Routing.Rules)),
			Balancers:      make([]Balancer, 0, len(routingValue.Routing.Balancers)),
		},
		Observatory: ObservatoryPolicy{
			SubjectSelector: observatoryValue.Observatory.SubjectSelector,
			ProbeInterval:   normalizeProbeInterval(observatoryValue.Observatory.ProbeInterval),
		},
	}
	for _, rule := range routingValue.Routing.Rules {
		ports, err := parsePortGrammar(rule.Port)
		if err != nil {
			return Appliance{}, errors.New("active routing port grammar is invalid")
		}
		network, err := parseNetworkGrammar(rule.Network)
		if err != nil {
			return Appliance{}, errors.New("active routing network grammar is invalid")
		}
		result.Routing.Rules = append(result.Routing.Rules, RoutingRule{
			Type:       rule.Type,
			InboundTag: append([]string(nil), rule.InboundTag...),
			IP:         append([]string(nil), rule.IP...),
			Domain:     append([]string(nil), rule.Domain...),
			Protocol:   append([]string(nil), rule.Protocol...),
			Network:    network,
			Ports:      ports,
			Action:     RuleAction{OutboundTag: rule.OutboundTag, BalancerTag: rule.BalancerTag},
			RuleTag:    rule.RuleTag,
		})
	}
	for _, balancer := range routingValue.Routing.Balancers {
		result.Routing.Balancers = append(result.Routing.Balancers, Balancer{
			Tag:         balancer.Tag,
			Selector:    append([]string(nil), balancer.Selector...),
			FallbackTag: balancer.FallbackTag,
			Strategy:    balancer.Strategy,
		})
	}
	result = normalize(result)
	if err := result.Validate(); err != nil {
		return Appliance{}, errors.New("active policy is outside appliance schema")
	}
	return result, nil
}

func parseNetworkGrammar(value string) ([]string, error) {
	if value == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if part != "tcp" && part != "udp" {
			return nil, errors.New("unsupported network")
		}
		if _, exists := seen[part]; exists {
			return nil, errors.New("duplicate network")
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result, nil
}

func parsePortGrammar(value string) ([]PortRange, error) {
	if value == "" {
		return []PortRange{}, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > MaxPortRanges {
		return nil, errors.New("too many ports")
	}
	result := make([]PortRange, 0, len(parts))
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part {
			return nil, errors.New("invalid port")
		}
		rangeParts := strings.Split(part, "-")
		if len(rangeParts) > 2 || rangeParts[0] == "" {
			return nil, errors.New("invalid port range")
		}
		from, err := strconv.Atoi(rangeParts[0])
		if err != nil {
			return nil, errors.New("invalid port")
		}
		to := from
		if len(rangeParts) == 2 {
			if rangeParts[1] == "" {
				return nil, errors.New("invalid port range")
			}
			to, err = strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, errors.New("invalid port range")
			}
		}
		if from < 1 || to < from || to > 65535 {
			return nil, errors.New("invalid port range")
		}
		result = append(result, PortRange{From: from, To: to})
	}
	return result, nil
}

func renderPolicyFiles(value Appliance) (map[string][]byte, error) {
	value = normalize(value)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	dns, err := json.MarshalIndent(dnsDocument{DNS: dnsRuntime{
		Servers: value.DNS.Servers, QueryStrategy: value.DNS.QueryStrategy,
		DisableCache: value.DNS.DisableCache, ServeStale: value.DNS.ServeStale,
		ServeExpiredTTL: value.DNS.ServeExpiredTTL, DisableFallback: value.DNS.DisableFallback,
		DisableFallbackIfMatch: value.DNS.DisableFallbackIfMatch,
		EnableParallelQuery:    value.DNS.EnableParallelQuery, UseSystemHosts: value.DNS.UseSystemHosts,
	}}, "", "  ")
	if err != nil {
		return nil, errors.New("unable to render DNS policy")
	}
	routingRules := make([]routingRuleRuntime, 0, len(value.Routing.Rules))
	for _, rule := range value.Routing.Rules {
		routingRules = append(routingRules, routingRuleRuntime{
			Type: rule.Type, InboundTag: rule.InboundTag, IP: rule.IP, Domain: rule.Domain,
			Protocol: rule.Protocol, Network: joinNetworkGrammar(rule.Network), Port: joinPortGrammar(rule.Ports),
			OutboundTag: rule.Action.OutboundTag, BalancerTag: rule.Action.BalancerTag, RuleTag: rule.RuleTag,
		})
	}
	routingBalancers := make([]balancerRuntime, 0, len(value.Routing.Balancers))
	for _, balancer := range value.Routing.Balancers {
		routingBalancers = append(routingBalancers, balancerRuntime{Tag: balancer.Tag, Selector: balancer.Selector, FallbackTag: balancer.FallbackTag, Strategy: balancer.Strategy})
	}
	routing, err := json.MarshalIndent(routingDocument{Routing: routingRuntime{
		DomainStrategy: value.Routing.DomainStrategy, DomainMatcher: value.Routing.DomainMatcher,
		Rules: routingRules, Balancers: routingBalancers,
	}}, "", "  ")
	if err != nil {
		return nil, errors.New("unable to render routing policy")
	}
	probeURL, enableConcurrency := defaultObservatoryFields()
	observatory, err := json.MarshalIndent(observatoryDocument{Observatory: observatoryRuntime{
		SubjectSelector: value.Observatory.SubjectSelector, ProbeURL: probeURL,
		ProbeInterval: value.Observatory.ProbeInterval, EnableConcurrency: enableConcurrency,
	}}, "", "  ")
	if err != nil {
		return nil, errors.New("unable to render Observatory policy")
	}
	dns, err = boundedJSONWithNewline(dns)
	if err != nil {
		return nil, errors.New("DNS policy exceeds bounded size")
	}
	routing, err = boundedJSONWithNewline(routing)
	if err != nil {
		return nil, errors.New("routing policy exceeds bounded size")
	}
	observatory, err = boundedJSONWithNewline(observatory)
	if err != nil {
		return nil, errors.New("Observatory policy exceeds bounded size")
	}
	return map[string][]byte{
		"xray/02_dns.json":         dns,
		"xray/05_routing.json":     routing,
		"xray/07_observatory.json": observatory,
	}, nil
}

func boundedJSONWithNewline(contents []byte) ([]byte, error) {
	if len(contents)+1 > MaxDocumentSize {
		return nil, errors.New("JSON document exceeds bounded size")
	}
	return append(contents, '\n'), nil
}

func joinNetworkGrammar(values []string) string {
	return strings.Join(values, ",")
}

func joinPortGrammar(values []PortRange) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value.From == value.To {
			parts = append(parts, strconv.Itoa(value.From))
			continue
		}
		parts = append(parts, strconv.Itoa(value.From)+"-"+strconv.Itoa(value.To))
	}
	return strings.Join(parts, ",")
}

func renderFiles(value Appliance, registry nodes.Registry) (map[string][]byte, error) {
	files, err := renderPolicyFiles(value)
	if err != nil {
		return nil, err
	}
	for _, path := range fixedTemplatePaths {
		contents, err := compatibilityTemplate(path)
		if err != nil {
			return nil, err
		}
		files[path] = contents
	}
	outbounds, err := nodes.Render(registry)
	if err != nil {
		return nil, errors.New("node outbound render failed")
	}
	files["xray/04_outbounds.json"] = outbounds
	return files, nil
}

func compatibilityTemplate(path string) ([]byte, error) {
	templatePath := strings.TrimPrefix(path, "xray/")
	if strings.HasPrefix(path, "xkeen/") {
		templatePath = "xkeen.json"
	}
	contents, err := compatibilityTemplates.ReadFile("templates/" + templatePath)
	if err != nil {
		return nil, errors.New("compatibility template is unavailable")
	}
	return bytes.Clone(contents), nil
}

func canonicalEqual(left, right Appliance) bool {
	leftJSON, leftErr := MarshalCanonical(left)
	rightJSON, rightErr := MarshalCanonical(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func semanticJSONEqual(left, right []byte) bool {
	leftValue, leftErr := decodeJSONValue(left)
	rightValue, rightErr := decodeJSONValue(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftValue, rightValue)
}

func decodeJSONValue(data []byte) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}
