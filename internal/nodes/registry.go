package nodes

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	SchemaVersion       = 1
	MaxNodes            = 256
	MaxSubscriptions    = 32
	MaxNameLength       = 128
	MaxIdentifierLength = 64
)

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{7,63}$`)

// Registry is the versioned, local-only secret source of truth. It must never
// be marshalled into an API response; use PublicNode for the safe projection.
type Registry struct {
	SchemaVersion int            `json:"schemaVersion"`
	Nodes         []Node         `json:"nodes"`
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
}

type Node struct {
	ID          string `json:"id"`
	OutboundTag string `json:"outboundTag"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Source      Source `json:"source"`
	VLESS       VLESS  `json:"vless"`
	SourceKey   string `json:"sourceKey"`
	Stale       bool   `json:"stale,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
}

type Source struct {
	Type           string `json:"type"`
	SubscriptionID string `json:"subscriptionId,omitempty"`
}

type Subscription struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// UnmarshalJSON keeps subscriptions created before the enabled flag was
// introduced active. A missing flag is the backwards-compatible enabled
// state; an explicit false remains disabled.
func (s *Subscription) UnmarshalJSON(data []byte) error {
	var record struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		URL     string `json:"url"`
		Enabled *bool  `json:"enabled"`
	}
	if err := decodeStrictJSON(data, &record); err != nil {
		return err
	}
	s.ID, s.Name, s.URL = record.ID, record.Name, record.URL
	s.Enabled = record.Enabled == nil || *record.Enabled
	return nil
}

// VLESS contains only the explicitly supported client-side VLESS/REALITY
// transport fields. The UUID and key fields are intentionally kept inside the
// domain package and are never copied into PublicNode.
type VLESS struct {
	UUID        string     `json:"uuid"`
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Encryption  string     `json:"encryption"`
	Flow        string     `json:"flow,omitempty"`
	Security    string     `json:"security"`
	ServerName  string     `json:"serverName"`
	Fingerprint string     `json:"fingerprint"`
	PublicKey   string     `json:"publicKey"`
	ShortID     string     `json:"shortId"`
	SpiderX     string     `json:"spiderX,omitempty"`
	Network     string     `json:"network"`
	Path        string     `json:"path,omitempty"`
	HostHeader  string     `json:"hostHeader,omitempty"`
	ServiceName string     `json:"serviceName,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	FinalMask   *FinalMask `json:"finalMask,omitempty"`
}

// FinalMask is intentionally limited to the fragment shape currently emitted
// by supported VLESS share links. Unknown mask types and fields fail closed.
type FinalMask struct {
	Fragment FinalMaskFragment `json:"fragment"`
}

type FinalMaskFragment struct {
	Packets  string `json:"packets"`
	Length   string `json:"length"`
	Delay    string `json:"delay"`
	MaxSplit string `json:"maxSplit,omitempty"`
}

type PublicNode struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	Address          string `json:"address"`
	CountryCode      string `json:"countryCode,omitempty"`
	OutboundTag      string `json:"outboundTag"`
	Enabled          bool   `json:"enabled"`
	SourceType       string `json:"sourceType"`
	SubscriptionName string `json:"subscriptionName,omitempty"`
	Stale            bool   `json:"stale,omitempty"`
	Missing          bool   `json:"missing,omitempty"`
}

type PublicSubscription struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	NodeCount  int    `json:"nodeCount"`
	StaleCount int    `json:"staleCount"`
}

func NewRegistry() Registry {
	return Registry{SchemaVersion: SchemaVersion, Nodes: []Node{}}
}

func CanonicalTag(id string) string { return "proxy-" + id }

func NewNode(profile VLESS, name string, source Source) (Node, error) {
	id, err := randomIdentifier()
	if err != nil {
		return Node{}, errors.New("unable to allocate node identity")
	}
	return NewNodeWithID(profile, name, source, id)
}

func NewNodeWithID(profile VLESS, name string, source Source, id string) (Node, error) {
	if err := profile.Validate(); err != nil {
		return Node{}, err
	}
	if !validIdentifier(id) {
		return Node{}, errors.New("invalid node identity")
	}
	name = safeName(name, "Imported node")
	if source.Type == "" {
		source.Type = "manual"
	}
	if source.Type != "manual" && source.Type != "subscription" {
		return Node{}, errors.New("unsupported node source")
	}
	if source.Type == "subscription" && !validSubscriptionID(source.SubscriptionID) {
		return Node{}, errors.New("invalid subscription identity")
	}
	return Node{
		ID:          id,
		OutboundTag: CanonicalTag(id),
		Name:        name,
		Enabled:     true,
		Source:      source,
		VLESS:       profile,
		SourceKey:   nodeSourceKey(profile, name, source),
	}, nil
}

func (r Registry) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return errors.New("unsupported registry schema")
	}
	if len(r.Nodes) > MaxNodes || len(r.Subscriptions) > MaxSubscriptions {
		return errors.New("registry exceeds bounded size")
	}
	subscriptions := make(map[string]struct{}, len(r.Subscriptions))
	for _, subscription := range r.Subscriptions {
		if !validSubscriptionID(subscription.ID) || !validDisplay(subscription.Name, MaxNameLength) || subscription.URL == "" {
			return errors.New("invalid subscription record")
		}
		if _, exists := subscriptions[subscription.ID]; exists {
			return errors.New("duplicate subscription identity")
		}
		subscriptions[subscription.ID] = struct{}{}
	}
	ids := make(map[string]struct{}, len(r.Nodes))
	tags := make(map[string]struct{}, len(r.Nodes))
	keys := make(map[string]struct{}, len(r.Nodes))
	for _, node := range r.Nodes {
		if !validIdentifier(node.ID) || node.OutboundTag != CanonicalTag(node.ID) || !validDisplay(node.Name, MaxNameLength) {
			return errors.New("invalid node identity or display name")
		}
		if _, exists := ids[node.ID]; exists {
			return errors.New("duplicate node identity")
		}
		if _, exists := tags[node.OutboundTag]; exists {
			return errors.New("duplicate outbound tag")
		}
		if node.Source.Type != "manual" && node.Source.Type != "subscription" {
			return errors.New("unsupported node source")
		}
		if node.Source.Type == "subscription" {
			if !validSubscriptionID(node.Source.SubscriptionID) {
				return errors.New("invalid node subscription identity")
			}
			if _, exists := subscriptions[node.Source.SubscriptionID]; !exists {
				return errors.New("node references unknown subscription")
			}
		}
		if err := node.VLESS.Validate(); err != nil {
			return err
		}
		if node.SourceKey != nodeSourceKey(node.VLESS, node.Name, node.Source) {
			return errors.New("invalid node source key")
		}
		if node.Missing && !node.Stale {
			return errors.New("missing node must be stale")
		}
		ids[node.ID] = struct{}{}
		tags[node.OutboundTag] = struct{}{}
		if node.Source.Type == "subscription" {
			if _, exists := keys[node.SourceKey]; exists {
				return errors.New("ambiguous subscription source identity")
			}
			keys[node.SourceKey] = struct{}{}
		}
	}
	return nil
}

func (r Registry) SortedNodes() []Node {
	result := append([]Node(nil), r.Nodes...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].OutboundTag == result[j].OutboundTag {
			return result[i].ID < result[j].ID
		}
		return result[i].OutboundTag < result[j].OutboundTag
	})
	return result
}

func (r Registry) PublicNodes() []PublicNode {
	labels := make(map[string]string, len(r.Subscriptions))
	for _, subscription := range r.Subscriptions {
		labels[subscription.ID] = subscription.Name
	}
	nodes := r.SortedNodes()
	result := make([]PublicNode, 0, len(nodes))
	for _, node := range nodes {
		displayName, countryCode := nodeDisplayName(node.Name, node.VLESS.Host)
		item := PublicNode{ID: node.ID, Name: node.Name, DisplayName: displayName, Address: displayAddress(node.VLESS.Host, node.VLESS.Port), CountryCode: countryCode, OutboundTag: node.OutboundTag, Enabled: node.Enabled, SourceType: node.Source.Type, Stale: node.Stale, Missing: node.Missing}
		if node.Source.Type == "subscription" {
			item.SubscriptionName = labels[node.Source.SubscriptionID]
		}
		result = append(result, item)
	}
	return result
}

func (r Registry) PublicSubscriptions() []PublicSubscription {
	result := make([]PublicSubscription, 0, len(r.Subscriptions))
	for _, subscription := range r.Subscriptions {
		item := PublicSubscription{ID: subscription.ID, Name: subscription.Name, Enabled: subscription.Enabled}
		for _, node := range r.Nodes {
			if node.Source.Type == "subscription" && node.Source.SubscriptionID == subscription.ID {
				item.NodeCount++
				if node.Stale || node.Missing {
					item.StaleCount++
				}
			}
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (v VLESS) Validate() error {
	if !uuidLike(v.UUID) || !validHost(v.Host) || v.Port < 1 || v.Port > 65535 {
		return errors.New("invalid VLESS endpoint")
	}
	if v.Encryption != "none" || v.Security != "reality" {
		return errors.New("unsupported VLESS security mode")
	}
	if v.Flow != "" && v.Flow != "xtls-rprx-vision" {
		return errors.New("unsupported VLESS flow")
	}
	if !validHost(v.ServerName) || !validToken(v.Fingerprint, 1, 32) || !validPublicKey(v.PublicKey) || !validShortID(v.ShortID) {
		return errors.New("invalid REALITY parameters")
	}
	if v.Network == "" {
		return errors.New("missing VLESS transport")
	}
	switch v.Network {
	case "tcp":
		if v.Path != "" || v.HostHeader != "" || v.ServiceName != "" || v.Mode != "" {
			return errors.New("invalid TCP transport fields")
		}
	case "ws", "http":
		if v.Path == "" || !strings.HasPrefix(v.Path, "/") || len(v.Path) > 256 || !validDisplay(v.Path, 256) {
			return errors.New("invalid web transport path")
		}
		if v.HostHeader != "" && (!validHost(v.HostHeader) || len(v.HostHeader) > 253) {
			return errors.New("invalid web transport host")
		}
		if v.ServiceName != "" || v.Mode != "" {
			return errors.New("invalid web transport fields")
		}
	case "xhttp", "splithttp":
		if v.Path == "" || !strings.HasPrefix(v.Path, "/") || len(v.Path) > 256 || !validDisplay(v.Path, 256) {
			return errors.New("invalid XHTTP transport path")
		}
		if v.HostHeader != "" && (!validHost(v.HostHeader) || len(v.HostHeader) > 253) {
			return errors.New("invalid XHTTP transport host")
		}
		if v.ServiceName != "" || !validXHTTPMode(v.Mode) {
			return errors.New("invalid XHTTP transport fields")
		}
	case "grpc":
		if !validDisplay(v.ServiceName, 128) || v.Path != "" || v.HostHeader != "" {
			return errors.New("invalid gRPC transport fields")
		}
	default:
		return errors.New("unsupported VLESS transport")
	}
	if v.SpiderX != "" && (!strings.HasPrefix(v.SpiderX, "/") || len(v.SpiderX) > 256 || !validDisplay(v.SpiderX, 256)) {
		return errors.New("invalid REALITY spider path")
	}
	if v.FinalMask != nil {
		fragment := v.FinalMask.Fragment
		if fragment.Packets != "tlshello" || !validIntRange(fragment.Length, 1, 65535) || !validIntRange(fragment.Delay, 0, 60000) || (fragment.MaxSplit != "" && !validIntRange(fragment.MaxSplit, 1, 1024)) {
			return errors.New("invalid Finalmask fragment")
		}
	}
	return nil
}

func (v VLESS) SourceKey() string { return sourceKey(v) }

func sourceKey(v VLESS) string {
	canonical := fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", strings.ToLower(v.Host), v.Port, v.Security, v.ServerName, v.Fingerprint, v.PublicKey, v.ShortID, v.SpiderX, v.Network, v.Path, v.HostHeader, v.ServiceName, v.Mode)
	digest := sha256.Sum256([]byte(canonical))
	return "sha256-" + hex.EncodeToString(digest[:])
}

func subscriptionSourceKey(v VLESS, name string) string {
	canonical := sourceKey(v) + "|" + strings.ToLower(strings.TrimSpace(name))
	digest := sha256.Sum256([]byte(canonical))
	return "sha256-" + hex.EncodeToString(digest[:])
}

func nodeSourceKey(v VLESS, name string, source Source) string {
	if source.Type == "subscription" {
		return subscriptionSourceKey(v, name)
	}
	return sourceKey(v)
}

func randomIdentifier() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "node-" + hex.EncodeToString(raw[:]), nil
}

func validIdentifier(value string) bool {
	return len(value) <= MaxIdentifierLength && identifierPattern.MatchString(value)
}

func validSubscriptionID(value string) bool {
	return len(value) >= 8 && len(value) <= MaxIdentifierLength && identifierPattern.MatchString(value)
}

func validDisplay(value string, max int) bool {
	if value == "" || len([]rune(value)) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return false
		}
	}
	return true
}

func safeName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if !validDisplay(value, MaxNameLength) {
		return fallback
	}
	return value
}

func uuidLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func validHost(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "\r\n/\\@") {
		return false
	}
	if value == "localhost" || strings.HasSuffix(value, ".localhost") {
		return false
	}
	if strings.Contains(value, ":") {
		// IPv6 literals are accepted only in their parsed canonical form.
		for _, r := range value {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == ':') {
				return false
			}
		}
		return true
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

func validToken(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}

func validPublicKey(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validShortID(value string) bool {
	if len(value) == 0 || len(value) > 16 || len(value)%2 != 0 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func validXHTTPMode(value string) bool {
	switch value {
	case "", "auto", "packet-up", "stream-up", "stream-one":
		return true
	default:
		return false
	}
}

func validIntRange(value string, minimum, maximum int) bool {
	parts := strings.Split(value, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	from, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	to := from
	if len(parts) == 2 {
		to, err = strconv.Atoi(parts[1])
		if err != nil {
			return false
		}
	}
	return from >= minimum && from <= to && to <= maximum
}
