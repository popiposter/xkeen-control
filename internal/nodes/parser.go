package nodes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
)

const (
	MaxProfileInput   = 256 << 10
	MaxProfileCount   = 128
	MaxLegacyDocument = 8 << 20
)

var supportedQueryKeys = map[string]struct{}{
	"encryption": {}, "flow": {}, "security": {}, "sni": {}, "servername": {},
	"fp": {}, "fingerprint": {}, "pbk": {}, "publickey": {}, "sid": {}, "shortid": {},
	"spx": {}, "spiderx": {}, "type": {}, "network": {}, "path": {}, "host": {},
	"servicename": {}, "mode": {}, "fm": {},
}

// ParseProfiles parses one or more newline-separated vless:// URIs. It does
// not accept arbitrary Xray JSON or silently discard unknown query fields.
func ParseProfiles(input string) ([]ParsedProfile, error) {
	if len(input) == 0 || len(input) > MaxProfileInput {
		return nil, errors.New("profile input exceeds bounded size")
	}
	result := make([]ParsedProfile, 0, 4)
	for _, line := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(result) >= MaxProfileCount {
			return nil, errors.New("profile count exceeds bounded limit")
		}
		profile, err := ParseProfile(line)
		if err != nil {
			return nil, err
		}
		result = append(result, profile)
	}
	if len(result) == 0 {
		return nil, errors.New("no VLESS profiles supplied")
	}
	return result, nil
}

type ParsedProfile struct {
	VLESS VLESS
	Name  string
}

func ParseProfile(raw string) (ParsedProfile, error) {
	if len(raw) > MaxProfileInput || !strings.HasPrefix(strings.ToLower(raw), "vless://") {
		return ParsedProfile{}, errors.New("invalid VLESS profile")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "vless") || parsed.User == nil || parsed.User.Username() == "" || parsed.Hostname() == "" {
		return ParsedProfile{}, errors.New("invalid VLESS profile")
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		return ParsedProfile{}, errors.New("invalid VLESS profile")
	}
	if len(parsed.RawQuery) > 8192 {
		return ParsedProfile{}, errors.New("VLESS query exceeds bounded size")
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return ParsedProfile{}, errors.New("invalid VLESS query")
	}
	normalized := make(url.Values, len(values))
	for key, items := range values {
		key = strings.ToLower(key)
		if _, ok := supportedQueryKeys[key]; !ok {
			return ParsedProfile{}, errors.New("unsupported VLESS parameter")
		}
		if len(items) != 1 || len(normalized[key]) != 0 {
			return ParsedProfile{}, errors.New("duplicate VLESS parameter")
		}
		normalized[key] = items
	}
	values = normalized
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || parsed.Port() == "" {
		return ParsedProfile{}, errors.New("invalid VLESS port")
	}
	profile := VLESS{
		UUID:        parsed.User.Username(),
		Host:        parsed.Hostname(),
		Port:        port,
		Encryption:  firstValue(values, "encryption", "none"),
		Flow:        firstValue(values, "flow", ""),
		Security:    firstAlias(values, "security"),
		ServerName:  firstAlias(values, "sni", "servername"),
		Fingerprint: firstAlias(values, "fp", "fingerprint"),
		PublicKey:   firstAlias(values, "pbk", "publickey"),
		ShortID:     firstAlias(values, "sid", "shortid"),
		SpiderX:     firstAlias(values, "spx", "spiderx"),
		Network:     firstAlias(values, "type", "network"),
		Path:        firstValue(values, "path", ""),
		HostHeader:  firstValue(values, "host", ""),
		ServiceName: firstValue(values, "servicename", ""),
		Mode:        firstValue(values, "mode", ""),
	}
	profile.FinalMask, err = parseFinalMask(firstValue(values, "fm", ""))
	if err != nil {
		return ParsedProfile{}, err
	}
	if err := profile.Validate(); err != nil {
		return ParsedProfile{}, err
	}
	name := strings.TrimSpace(parsed.Fragment)
	if !validDisplay(name, MaxNameLength) {
		name = "Imported node"
	}
	return ParsedProfile{VLESS: profile, Name: name}, nil
}

type sharedFinalMask struct {
	Fragment *struct {
		Packets  string `json:"packets"`
		Length   string `json:"length"`
		Interval string `json:"interval"`
		Delay    string `json:"delay"`
		MaxSplit string `json:"maxSplit"`
	} `json:"fragment"`
}

func parseFinalMask(raw string) (*FinalMask, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 8192 {
		return nil, errors.New("Finalmask exceeds bounded size")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var shared sharedFinalMask
	if err := decoder.Decode(&shared); err != nil || shared.Fragment == nil {
		return nil, errors.New("invalid Finalmask fragment")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("invalid Finalmask fragment")
	}
	if shared.Fragment.Interval != "" && shared.Fragment.Delay != "" {
		return nil, errors.New("ambiguous Finalmask delay")
	}
	delay := shared.Fragment.Delay
	if delay == "" {
		delay = shared.Fragment.Interval
	}
	result := &FinalMask{Fragment: FinalMaskFragment{
		Packets: shared.Fragment.Packets, Length: shared.Fragment.Length,
		Delay: delay, MaxSplit: shared.Fragment.MaxSplit,
	}}
	fragment := result.Fragment
	if fragment.Packets != "tlshello" || !validIntRange(fragment.Length, 1, 65535) || !validIntRange(fragment.Delay, 0, 60000) || (fragment.MaxSplit != "" && !validIntRange(fragment.MaxSplit, 1, 1024)) {
		return nil, errors.New("invalid Finalmask fragment")
	}
	return result, nil
}

// ParseSubscriptionBody accepts either raw VLESS lines or a bounded base64
// encoded list. The response is never retained after reconciliation.
func ParseSubscriptionBody(body []byte) ([]ParsedProfile, error) {
	if len(body) > MaxProfileInput {
		return nil, errors.New("subscription response exceeds bounded size")
	}
	text := strings.TrimSpace(string(body))
	if strings.Contains(strings.ToLower(text), "vless://") {
		return ParseProfiles(text)
	}
	compact := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, text)
	if compact == "" || len(compact) > MaxProfileInput*2 {
		return nil, errors.New("invalid subscription response")
	}
	decoders := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	for _, decoder := range decoders {
		decoded, err := decoder.DecodeString(compact)
		if err != nil || len(decoded) > MaxProfileInput {
			continue
		}
		if result, parseErr := ParseProfiles(string(decoded)); parseErr == nil {
			return result, nil
		}
	}
	return nil, errors.New("invalid subscription response")
}

func firstValue(values url.Values, key, fallback string) string {
	if value := values.Get(key); value != "" {
		return value
	}
	return fallback
}

func firstAlias(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			return value
		}
	}
	return ""
}

type legacyDocument struct {
	Outbounds []json.RawMessage `json:"outbounds"`
}

type legacyOutbound struct {
	Tag            string               `json:"tag"`
	Protocol       string               `json:"protocol"`
	Settings       legacySettings       `json:"settings"`
	StreamSettings legacyStreamSettings `json:"streamSettings"`
}

type legacySettings struct {
	Address    string `json:"address"`
	Port       int    `json:"port"`
	ID         string `json:"id"`
	Encryption string `json:"encryption"`
	Flow       string `json:"flow"`
	VNext      []struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
		Users   []struct {
			ID         string `json:"id"`
			Encryption string `json:"encryption"`
			Flow       string `json:"flow"`
		} `json:"users"`
	} `json:"vnext"`
}

func (s legacySettings) connection() (string, int, string, string, string, bool) {
	if len(s.VNext) == 1 && len(s.VNext[0].Users) == 1 && s.Address == "" && s.ID == "" {
		server := s.VNext[0]
		user := server.Users[0]
		return server.Address, server.Port, user.ID, user.Encryption, user.Flow, true
	}
	if len(s.VNext) == 0 && s.Address != "" && s.ID != "" {
		return s.Address, s.Port, s.ID, s.Encryption, s.Flow, true
	}
	return "", 0, "", "", "", false
}

type legacyStreamSettings struct {
	Network         string `json:"network"`
	Security        string `json:"security"`
	RealitySettings struct {
		ServerName  string `json:"serverName"`
		Fingerprint string `json:"fingerprint"`
		PublicKey   string `json:"publicKey"`
		ShortID     string `json:"shortId"`
		SpiderX     string `json:"spiderX"`
	} `json:"realitySettings"`
	WSSettings struct {
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
	} `json:"wsSettings"`
	GRPCSettings struct {
		ServiceName string `json:"serviceName"`
		Mode        string `json:"mode"`
	} `json:"grpcSettings"`
}

// MigrateLegacy projects supported VLESS/REALITY entries and ignores fixed
// non-node outbounds such as direct/block. It performs no writes.
func MigrateLegacy(contents []byte) (Registry, error) {
	if len(contents) == 0 || len(contents) > MaxLegacyDocument {
		return Registry{}, errors.New("legacy outbound document exceeds bounded size")
	}
	var document legacyDocument
	if err := json.Unmarshal(contents, &document); err != nil || len(document.Outbounds) == 0 || len(document.Outbounds) > MaxNodes*4 {
		return Registry{}, errors.New("invalid legacy outbound document")
	}
	result := NewRegistry()
	for index, raw := range document.Outbounds {
		var outbound legacyOutbound
		if json.Unmarshal(raw, &outbound) != nil {
			return Registry{}, errors.New("invalid legacy outbound entry")
		}
		if !strings.EqualFold(outbound.Protocol, "vless") {
			continue
		}
		address, port, id, encryption, flow, ok := outbound.Settings.connection()
		if !ok {
			return Registry{}, errors.New("legacy VLESS outbound is ambiguous")
		}
		profile := VLESS{
			UUID:        id,
			Host:        address,
			Port:        port,
			Encryption:  encryption,
			Flow:        flow,
			Security:    outbound.StreamSettings.Security,
			ServerName:  outbound.StreamSettings.RealitySettings.ServerName,
			Fingerprint: outbound.StreamSettings.RealitySettings.Fingerprint,
			PublicKey:   outbound.StreamSettings.RealitySettings.PublicKey,
			ShortID:     outbound.StreamSettings.RealitySettings.ShortID,
			SpiderX:     outbound.StreamSettings.RealitySettings.SpiderX,
			Network:     outbound.StreamSettings.Network,
		}
		if profile.Network == "" {
			profile.Network = "tcp"
		}
		if profile.Security == "" && outbound.StreamSettings.RealitySettings.PublicKey != "" {
			profile.Security = "reality"
		}
		if profile.Network == "ws" || profile.Network == "http" || profile.Network == "xhttp" || profile.Network == "splithttp" {
			profile.Path = outbound.StreamSettings.WSSettings.Path
			profile.HostHeader = outbound.StreamSettings.WSSettings.Headers["Host"]
		}
		if profile.Network == "grpc" {
			profile.ServiceName = outbound.StreamSettings.GRPCSettings.ServiceName
			profile.Mode = outbound.StreamSettings.GRPCSettings.Mode
		}
		if err := profile.Validate(); err != nil {
			return Registry{}, errors.New("legacy VLESS profile is unsupported")
		}
		name := legacyName(outbound.Tag, index+1)
		node, err := NewNode(profile, name, Source{Type: "manual"})
		if err != nil {
			return Registry{}, errors.New("legacy node migration failed")
		}
		result.Nodes = append(result.Nodes, node)
	}
	if len(result.Nodes) == 0 {
		return Registry{}, errors.New("legacy document contains no supported VLESS nodes")
	}
	if err := result.Validate(); err != nil {
		return Registry{}, errors.New("migrated registry is invalid")
	}
	return result, nil
}

func legacyName(tag string, index int) string {
	if strings.HasPrefix(tag, "proxy-main-") || strings.HasPrefix(tag, "proxy-us-") {
		return "Node " + strconv.Itoa(index)
	}
	if validDisplay(tag, MaxNameLength) && tag != "" {
		return tag
	}
	return "Imported node " + strconv.Itoa(index)
}
