package appliance

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SchemaVersion = 1

	// The appliance document is deliberately much smaller than the existing
	// node/legacy limits. The current checked-in policy is only a few KiB, and
	// this ceiling prevents an appliance authority from becoming a raw config
	// transport.
	MaxDocumentSize = 1 << 20
	MaxResolvers    = 16
	MaxRules        = 256
	MaxBalancers    = 8
	MaxSelectors    = 16
	MaxListItems    = 512
	MaxStringLength = 512
	MaxRuleTag      = 128
	MaxPortRanges   = 64

	fieldRuleType            = "field"
	defaultProbeURL          = "https://www.google.com/generate_204"
	defaultEnableConcurrency = true
)

// Appliance is the portable, non-secret, schema-versioned policy authority.
// It intentionally contains no node credentials, panel credentials, listener
// address, runtime counters or arbitrary JSON fragments.
type Appliance struct {
	SchemaVersion int               `json:"schemaVersion"`
	DNS           DNSPolicy         `json:"dns"`
	Routing       RoutingPolicy     `json:"routing"`
	Observatory   ObservatoryPolicy `json:"observatory"`
}

type DNSPolicy struct {
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

// DNSServer is the typed subset of an Xray DNS server definition supported by
// appliance v1. The simple localhost form is accepted and rendered as the
// current Xray string form.
type DNSServer struct {
	Address       string   `json:"address"`
	Domains       []string `json:"domains,omitempty"`
	SkipFallback  bool     `json:"skipFallback,omitempty"`
	Tag           string   `json:"tag,omitempty"`
	QueryStrategy string   `json:"queryStrategy,omitempty"`
}

// UnmarshalJSON accepts both Xray's string localhost server and its typed
// object form while keeping object fields strictly allowlisted.
func (s *DNSServer) UnmarshalJSON(data []byte) error {
	var address string
	if err := json.Unmarshal(data, &address); err == nil {
		s.Address = address
		s.Domains = nil
		s.SkipFallback = false
		s.Tag = ""
		s.QueryStrategy = ""
		return nil
	}
	var value struct {
		Address       string   `json:"address"`
		Domains       []string `json:"domains"`
		SkipFallback  bool     `json:"skipFallback"`
		Tag           string   `json:"tag"`
		QueryStrategy string   `json:"queryStrategy"`
	}
	if err := decodeStrict(data, &value); err != nil {
		return errors.New("invalid DNS server")
	}
	s.Address = value.Address
	s.Domains = value.Domains
	s.SkipFallback = value.SkipFallback
	s.Tag = value.Tag
	s.QueryStrategy = value.QueryStrategy
	return nil
}

// MarshalJSON keeps the existing Xray localhost representation while all
// appliance state remains represented by this explicit typed struct.
func (s DNSServer) MarshalJSON() ([]byte, error) {
	if s.Address == "localhost" && len(s.Domains) == 0 && !s.SkipFallback && s.Tag == "" && s.QueryStrategy == "" {
		return json.Marshal(s.Address)
	}
	type dnsServer DNSServer
	return json.Marshal(dnsServer(s))
}

type RoutingPolicy struct {
	DomainStrategy string        `json:"domainStrategy"`
	DomainMatcher  string        `json:"domainMatcher"`
	Rules          []RoutingRule `json:"rules"`
	Balancers      []Balancer    `json:"balancers"`
}

// RoutingRule retains order because Xray evaluates rules first-match. Network
// and Ports are typed values in appliance state; they are rendered into the
// compact Xray grammar only at the runtime boundary.
type RoutingRule struct {
	Type       string      `json:"type"`
	InboundTag []string    `json:"inboundTag,omitempty"`
	IP         []string    `json:"ip,omitempty"`
	Domain     []string    `json:"domain,omitempty"`
	Protocol   []string    `json:"protocol,omitempty"`
	Network    []string    `json:"network,omitempty"`
	Ports      []PortRange `json:"ports,omitempty"`
	Action     RuleAction  `json:"action"`
	RuleTag    string      `json:"ruleTag,omitempty"`
}

type PortRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type RuleAction struct {
	OutboundTag string `json:"outboundTag,omitempty"`
	BalancerTag string `json:"balancerTag,omitempty"`
}

type Balancer struct {
	Tag         string           `json:"tag"`
	Selector    []string         `json:"selector"`
	FallbackTag string           `json:"fallbackTag"`
	Strategy    BalancerStrategy `json:"strategy"`
}

type BalancerStrategy struct {
	Type string `json:"type"`
}

type ObservatoryPolicy struct {
	SubjectSelector []string `json:"subjectSelector"`
	ProbeInterval   string   `json:"probeInterval"`
}

// Parse strictly decodes an appliance authority and validates every supported
// field. Unknown JSON fields are rejected before validation.
func Parse(data []byte) (Appliance, error) {
	if len(data) == 0 || len(data) > MaxDocumentSize {
		return Appliance{}, errors.New("appliance document exceeds bounded size")
	}
	var value Appliance
	if err := decodeStrict(data, &value); err != nil {
		return Appliance{}, errors.New("invalid appliance JSON")
	}
	value = normalize(value)
	if err := value.Validate(); err != nil {
		return Appliance{}, err
	}
	return value, nil
}

// MarshalCanonical returns deterministic, newline-terminated appliance JSON.
func MarshalCanonical(value Appliance) ([]byte, error) {
	value = normalize(value)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, errors.New("unable to encode appliance")
	}
	if len(contents)+1 > MaxDocumentSize {
		return nil, errors.New("appliance document exceeds bounded size")
	}
	return append(contents, '\n'), nil
}

func (a Appliance) Validate() error {
	if a.SchemaVersion != SchemaVersion {
		return errors.New("unsupported appliance schema")
	}
	if err := a.DNS.validate(); err != nil {
		return err
	}
	if err := a.Routing.validate(); err != nil {
		return err
	}
	if err := a.Observatory.validate(); err != nil {
		return err
	}
	return nil
}

func (p DNSPolicy) validate() error {
	if len(p.Servers) == 0 || len(p.Servers) > MaxResolvers {
		return errors.New("invalid DNS resolver count")
	}
	if !validQueryStrategy(p.QueryStrategy) {
		return errors.New("unsupported DNS query strategy")
	}
	if p.ServeExpiredTTL < 0 || p.ServeExpiredTTL > 7*24*60*60 {
		return errors.New("invalid DNS expired TTL")
	}
	for _, server := range p.Servers {
		if err := server.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s DNSServer) validate() error {
	if err := validateDNSAddress(s.Address); err != nil {
		return err
	}
	if len(s.Domains) > MaxListItems {
		return errors.New("DNS resolver domains exceed bounded size")
	}
	for _, domain := range s.Domains {
		if err := validateMatchExpression(domain, "domain"); err != nil {
			return errors.New("invalid DNS resolver domain")
		}
	}
	if s.Tag != "" && s.Tag != "dns-proxy" {
		return errors.New("unsupported DNS resolver tag")
	}
	if !validQueryStrategy(s.QueryStrategy) {
		return errors.New("unsupported DNS resolver query strategy")
	}
	return nil
}

func (p RoutingPolicy) validate() error {
	switch p.DomainStrategy {
	case "AsIs", "IPIfNonMatch", "IPOnDemand":
	default:
		return errors.New("unsupported routing domain strategy")
	}
	switch p.DomainMatcher {
	case "", "linear", "mph", "hybrid":
	default:
		return errors.New("unsupported routing domain matcher")
	}
	if len(p.Rules) == 0 || len(p.Rules) > MaxRules {
		return errors.New("invalid routing rule count")
	}
	for _, rule := range p.Rules {
		if err := rule.validate(); err != nil {
			return err
		}
	}
	if len(p.Balancers) == 0 || len(p.Balancers) > MaxBalancers {
		return errors.New("invalid balancer count")
	}
	foundManaged := false
	for _, balancer := range p.Balancers {
		if err := balancer.validate(); err != nil {
			return err
		}
		if balancer.Tag == "bal-proxy" {
			if foundManaged {
				return errors.New("duplicate managed balancer")
			}
			foundManaged = true
		}
	}
	if !foundManaged {
		return errors.New("managed balancer is missing")
	}
	return nil
}

func (r RoutingRule) validate() error {
	if r.Type != fieldRuleType {
		return errors.New("unsupported routing rule type")
	}
	if len(r.InboundTag) > MaxSelectors || len(r.IP) > MaxListItems || len(r.Domain) > MaxListItems || len(r.Protocol) > MaxSelectors || len(r.Network) > 2 || len(r.Ports) > MaxPortRanges {
		return errors.New("routing rule exceeds bounded size")
	}
	for _, tag := range r.InboundTag {
		switch tag {
		case "api", "probe", "dns-proxy", "redirect", "tproxy", "socks":
		default:
			return errors.New("unsupported routing inbound tag")
		}
	}
	for _, expression := range r.IP {
		if err := validateIPExpression(expression); err != nil {
			return err
		}
	}
	for _, expression := range r.Domain {
		if err := validateMatchExpression(expression, "domain"); err != nil {
			return err
		}
	}
	for _, protocol := range r.Protocol {
		switch protocol {
		case "bittorrent", "http", "tls", "quic", "utp":
		default:
			return errors.New("unsupported routing protocol")
		}
	}
	seenNetworks := make(map[string]struct{}, len(r.Network))
	for _, network := range r.Network {
		if network != "tcp" && network != "udp" {
			return errors.New("unsupported routing network")
		}
		if _, exists := seenNetworks[network]; exists {
			return errors.New("duplicate routing network")
		}
		seenNetworks[network] = struct{}{}
	}
	for _, port := range r.Ports {
		if port.From < 1 || port.To < port.From || port.To > 65535 {
			return errors.New("invalid routing port range")
		}
	}
	if len([]rune(r.RuleTag)) > MaxRuleTag || !validText(r.RuleTag) {
		return errors.New("invalid routing rule tag")
	}
	if err := r.Action.validate(); err != nil {
		return err
	}
	return nil
}

func (a RuleAction) validate() error {
	if (a.OutboundTag == "") == (a.BalancerTag == "") {
		return errors.New("routing action must select exactly one target")
	}
	if a.OutboundTag != "" {
		switch a.OutboundTag {
		case "api", "block", "direct", "dns-out":
		default:
			return errors.New("unsupported routing outbound")
		}
	}
	if a.BalancerTag != "" && a.BalancerTag != "bal-proxy" {
		return errors.New("unsupported routing balancer")
	}
	return nil
}

func (b Balancer) validate() error {
	if b.Tag != "bal-proxy" || len(b.Selector) == 0 || len(b.Selector) > MaxSelectors || b.FallbackTag != "block" || b.Strategy.Type != "leastPing" {
		return errors.New("invalid managed balancer")
	}
	for _, selector := range b.Selector {
		if !validTag(selector) || !strings.HasPrefix(selector, "proxy-") {
			return errors.New("invalid balancer selector")
		}
	}
	return nil
}

func (p ObservatoryPolicy) validate() error {
	if len(p.SubjectSelector) == 0 || len(p.SubjectSelector) > MaxSelectors {
		return errors.New("invalid Observatory selector count")
	}
	for _, selector := range p.SubjectSelector {
		if !validTag(selector) || !strings.HasPrefix(selector, "proxy-") {
			return errors.New("invalid Observatory selector")
		}
	}
	if len(p.ProbeInterval) == 0 || len(p.ProbeInterval) > 32 {
		return errors.New("invalid Observatory probe interval")
	}
	interval, err := time.ParseDuration(p.ProbeInterval)
	if err != nil || interval < time.Second || interval > 24*time.Hour {
		return errors.New("invalid Observatory probe interval")
	}
	return nil
}

func normalize(value Appliance) Appliance {
	if value.DNS.Servers == nil {
		value.DNS.Servers = []DNSServer{}
	}
	for index := range value.DNS.Servers {
		if value.DNS.Servers[index].Domains == nil {
			value.DNS.Servers[index].Domains = []string{}
		}
	}
	if value.Routing.Rules == nil {
		value.Routing.Rules = []RoutingRule{}
	}
	for index := range value.Routing.Rules {
		rule := &value.Routing.Rules[index]
		if rule.Type == "" {
			rule.Type = fieldRuleType
		}
		if rule.InboundTag == nil {
			rule.InboundTag = []string{}
		}
		if rule.IP == nil {
			rule.IP = []string{}
		}
		if rule.Domain == nil {
			rule.Domain = []string{}
		}
		if rule.Protocol == nil {
			rule.Protocol = []string{}
		}
		if rule.Network == nil {
			rule.Network = []string{}
		}
		if rule.Ports == nil {
			rule.Ports = []PortRange{}
		}
		for portIndex := range rule.Ports {
			if rule.Ports[portIndex].To == 0 {
				rule.Ports[portIndex].To = rule.Ports[portIndex].From
			}
		}
	}
	if value.Routing.Balancers == nil {
		value.Routing.Balancers = []Balancer{}
	}
	for index := range value.Routing.Balancers {
		if value.Routing.Balancers[index].Selector == nil {
			value.Routing.Balancers[index].Selector = []string{}
		}
	}
	if value.Observatory.SubjectSelector == nil {
		value.Observatory.SubjectSelector = []string{}
	}
	return value
}

func decodeStrict(data []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateDNSAddress(value string) error {
	if value == "localhost" {
		return nil
	}
	if len(value) == 0 || len(value) > MaxStringLength || !validText(value) || strings.ContainsAny(value, "\\@") {
		return errors.New("invalid DNS resolver address")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.Hostname() == "" || !validHost(parsed.Hostname()) {
			return errors.New("invalid DNS resolver URL")
		}
		if parsed.Port() != "" {
			port, err := strconv.Atoi(parsed.Port())
			if err != nil || port < 1 || port > 65535 {
				return errors.New("invalid DNS resolver port")
			}
		}
		return nil
	}
	if !validHost(value) {
		return errors.New("invalid DNS resolver host")
	}
	return nil
}

func validateMatchExpression(value, kind string) error {
	if kind == "domain" && len(value) > MaxStringLength {
		return errors.New("domain expression exceeds bounded size")
	}
	if value == "" || !validText(value) || strings.ContainsAny(value, "\\\t") {
		return errors.New("invalid match expression")
	}
	for _, prefix := range []string{"domain:", "full:", "keyword:", "regexp:", "geosite:", "ext:"} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
			return nil
		}
	}
	return errors.New("unsupported match expression")
}

func validateIPExpression(value string) error {
	if value == "" || len(value) > MaxStringLength || !validText(value) {
		return errors.New("invalid IP match expression")
	}
	if _, _, err := net.ParseCIDR(value); err == nil {
		return nil
	}
	for _, prefix := range []string{"geoip:", "ext:"} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix) && validToken(value[len(prefix):], true) {
			return nil
		}
	}
	return errors.New("unsupported IP match expression")
}

func validToken(value string, allowBang bool) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) || (allowBang && r == '!') {
			continue
		}
		return false
	}
	return true
}

func validQueryStrategy(value string) bool {
	switch value {
	case "", "UseIPv4", "UseIPv6", "UseIP", "UseIPv4v6":
		return true
	default:
		return false
	}
}

func validTag(value string) bool {
	if value == "" || len(value) > MaxStringLength {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func validText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return false
		}
	}
	return true
}

func validHost(value string) bool {
	if value == "localhost" {
		return true
	}
	if net.ParseIP(value) != nil {
		return true
	}
	if len(value) == 0 || len(value) > 253 || strings.ContainsAny(value, "@/\\") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return true
}

func defaultObservatoryFields() (string, bool) {
	return defaultProbeURL, defaultEnableConcurrency
}

func normalizeProbeInterval(value string) string {
	return strings.TrimSpace(value)
}
