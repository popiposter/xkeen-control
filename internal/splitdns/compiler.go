// Package splitdns synchronizes derived DNS data, not XKeen code or components.
package splitdns

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/geodatareader"
)

var ErrPolicy = errors.New("LAN DNS policy is unsupported or changed; check DNS integration status")

type plugin struct {
	Tag  string          `json:"tag"`
	Type string          `json:"type"`
	Args json.RawMessage `json:"args"`
}
type routeRule struct {
	Type     string   `json:"type"`
	Domain   []string `json:"domain"`
	Outbound string   `json:"outboundTag"`
	Balancer string   `json:"balancerTag"`
}
type compiledRule struct {
	domains []string
	action  string
}
type Plan struct {
	Config  []byte
	Lists   map[string][]byte
	Digest  string
	Sources map[string]string
	Entries int
	Skipped int
}

func hash(b []byte) string       { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func args(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func validExpression(s string) bool {
	if len(s) == 0 || len(s) > 2056 || strings.ContainsAny(s, "\r\n#") || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return false
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return false
	}
	switch parts[0] {
	case "regexp":
		_, err := regexp.Compile(parts[1])
		return err == nil
	case "full", "domain", "keyword":
		return true
	}
	return false
}
func validDoH(address string) bool {
	u, err := url.Parse(address)
	return err == nil && u.Scheme == "https" && net.ParseIP(u.Hostname()) != nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && u.Path != ""
}

// Compile preserves native first-match order for domain decisions unconditional
// within the discovered stock selective LAN input scope.
// IP/protocol/port/inbound conditions cannot classify a DNS question and are
// explicitly counted as skipped. Unknown broad domain targets are rejected.
func Compile(ctx context.Context, files map[string][]byte, config []byte, dir string, geo *geodatareader.Reader) (Plan, error) {
	p := Plan{Lists: map[string][]byte{}, Sources: map[string]string{}}
	root, err := configjson.DecodeObject(config)
	if err != nil {
		return p, ErrPolicy
	}
	var plugins []plugin
	if json.Unmarshal(root["plugins"], &plugins) != nil {
		return p, ErrPolicy
	}
	index := map[string]int{}
	for i, v := range plugins {
		if v.Tag == "" || index[v.Tag] != 0 {
			return p, ErrPolicy
		}
		index[v.Tag] = i + 1
	}
	get := func(tag, typ string) (*plugin, error) {
		i := index[tag]
		if i == 0 || plugins[i-1].Type != typ {
			return nil, ErrPolicy
		}
		return &plugins[i-1], nil
	}
	direct, err := get("direct", "forward")
	if err != nil {
		return p, err
	}
	tunnel, err := get("tunnel", "forward")
	if err != nil {
		return p, err
	}
	if _, err = get("main", "sequence"); err != nil {
		return p, err
	}
	for _, tag := range []string{"direct_only", "vpn_only"} {
		if _, err = get(tag, "sequence"); err != nil {
			return p, err
		}
	}
	var forward struct {
		Socks     string `json:"socks5"`
		Upstreams []struct {
			Addr string `json:"addr"`
		} `json:"upstreams"`
	}
	if json.Unmarshal(direct.Args, &forward) != nil || forward.Socks != "" || len(forward.Upstreams) == 0 || len(forward.Upstreams) > 4 {
		return p, ErrPolicy
	}
	for _, u := range forward.Upstreams {
		if !validDoH(u.Addr) {
			return p, ErrPolicy
		}
	}
	var tunnelArgs map[string]json.RawMessage
	if json.Unmarshal(tunnel.Args, &tunnelArgs) != nil {
		return p, ErrPolicy
	}
	var socks string
	if json.Unmarshal(tunnelArgs["socks5"], &socks) != nil {
		return p, ErrPolicy
	}
	host, port, err := net.SplitHostPort(socks)
	if err != nil || host != "127.0.0.1" {
		return p, ErrPolicy
	}
	var inbounds struct {
		Inbounds []struct {
			Tag, Listen, Protocol string
			Port                  json.RawMessage
			Settings              struct{ FollowRedirect bool }
			StreamSettings        struct{ Sockopt struct{ Tproxy string } }
		}
	}
	if configjson.Decode(files["03_inbounds.json"], &inbounds) != nil {
		return p, ErrPolicy
	}
	lanRedirect, lanTproxy := false, false
	tag := ""
	for _, v := range inbounds.Inbounds {
		transparent := (v.Protocol == "tunnel" || v.Protocol == "dokodemo-door") && v.Settings.FollowRedirect
		if transparent && v.Tag == "redirect" {
			lanRedirect = true
		}
		if transparent && v.Tag == "tproxy" && v.StreamSettings.Sockopt.Tproxy == "tproxy" {
			lanTproxy = true
		}
		if v.Listen == host && string(v.Port) == port && v.Protocol == "socks" {
			if tag != "" {
				return p, ErrPolicy
			}
			tag = v.Tag
		}
	}
	if tag == "" {
		return p, ErrPolicy
	}
	var nativeDNS struct {
		DNS struct{ Servers []json.RawMessage }
	}
	if configjson.Decode(files["02_dns.json"], &nativeDNS) != nil {
		return p, ErrPolicy
	}
	upstreams := []map[string]string{}
	for _, raw := range nativeDNS.DNS.Servers {
		var server struct{ Address, Tag string }
		if json.Unmarshal(raw, &server) == nil && server.Tag == "panel-dns-vpn" {
			if !validDoH(server.Address) {
				return p, ErrPolicy
			}
			upstreams = append(upstreams, map[string]string{"addr": server.Address})
		}
	}
	if len(upstreams) == 0 || len(upstreams) > 4 {
		return p, ErrPolicy
	}
	tunnelArgs["upstreams"] = args(upstreams)
	tunnel.Args = args(tunnelArgs)
	var routing struct {
		Routing struct{ Rules []json.RawMessage }
	}
	if configjson.Decode(files["05_routing.json"], &routing) != nil || len(routing.Routing.Rules) > 256 {
		return p, ErrPolicy
	}
	vpnTarget := ""
	for _, raw := range routing.Routing.Rules {
		var r struct {
			Inbound  []string `json:"inboundTag"`
			Balancer string   `json:"balancerTag"`
			Outbound string   `json:"outboundTag"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return p, ErrPolicy
		}
		for _, in := range r.Inbound {
			if in == tag {
				var keys map[string]json.RawMessage
				_ = json.Unmarshal(raw, &keys)
				for key := range keys {
					switch key {
					case "type", "ruleTag", "inboundTag", "balancerTag", "outboundTag":
					default:
						return p, ErrPolicy
					}
				}
				if vpnTarget != "" || (r.Balancer == "") == (r.Outbound == "") {
					return p, ErrPolicy
				}
				target := r.Balancer + "/" + r.Outbound
				if vpnTarget != "" && vpnTarget != target {
					return p, ErrPolicy
				}
				vpnTarget = target
			}
		}
	}
	if vpnTarget == "" || vpnTarget == "/" || vpnTarget == "/direct" || vpnTarget == "/block" {
		return p, ErrPolicy
	}
	fallback := ""
	var rules []compiledRule
	domainRules := 0
	requests := map[string]map[string]bool{}
	for _, raw := range routing.Routing.Rules {
		var r routeRule
		if json.Unmarshal(raw, &r) != nil {
			return p, ErrPolicy
		}
		if len(r.Domain) > 0 {
			domainRules++
		}
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(raw, &keys)
		conditional := false
		for k := range keys {
			switch k {
			case "type", "ruleTag", "domain", "outboundTag", "balancerTag":
			case "inboundTag":
				var tags []string
				if json.Unmarshal(keys[k], &tags) != nil {
					return p, ErrPolicy
				}
				redirect, tproxy := false, false
				for _, tag := range tags {
					redirect = redirect || tag == "redirect"
					tproxy = tproxy || tag == "tproxy"
				}
				if !lanRedirect || !lanTproxy || !redirect || !tproxy {
					conditional = true
				}
			case "network":
				var network string
				if json.Unmarshal(keys[k], &network) != nil {
					return p, ErrPolicy
				}
				parts := strings.Split(network, ",")
				both := len(parts) == 2 && ((strings.TrimSpace(parts[0]) == "tcp" && strings.TrimSpace(parts[1]) == "udp") || (strings.TrimSpace(parts[0]) == "udp" && strings.TrimSpace(parts[1]) == "tcp"))
				if !both {
					conditional = true
				}
			default:
				conditional = true
			}
		}
		if conditional {
			p.Skipped++
			continue
		}
		action := ""
		switch {
		case r.Outbound == "direct" && r.Balancer == "":
			action = "goto direct_only"
		case r.Outbound == "block" && r.Balancer == "":
			action = "reject 3"
		case r.Balancer+"/"+r.Outbound == vpnTarget:
			action = "goto vpn_only"
		default:
			return p, ErrPolicy
		}
		if len(r.Domain) == 0 {
			fallback = action
			break // Unconditional native catch-all also terminates DNS rules.
		}
		for _, selector := range r.Domain {
			if strings.HasPrefix(selector, "geosite:") {
				selector = "ext:geosite.dat:" + strings.TrimPrefix(selector, "geosite:")
			}
			if strings.HasPrefix(selector, "ext:") {
				parts := strings.SplitN(selector, ":", 3)
				if len(parts) != 3 {
					return p, ErrPolicy
				}
				if requests[parts[1]] == nil {
					requests[parts[1]] = map[string]bool{}
				}
				requests[parts[1]][parts[2]] = true
			} else if !validExpression(selector) {
				return p, ErrPolicy
			}
		}
		rules = append(rules, compiledRule{r.Domain, action})
	}
	// Unsupported native scopes must never turn an existing domain policy
	// into an empty all-DIRECT resolver configuration.
	if domainRules > 0 && len(rules) == 0 {
		return p, ErrPolicy
	}
	exports := map[string]map[string][]string{}
	exportBytes := 0
	for file, cats := range requests {
		var selectors []string
		for cat := range cats {
			selectors = append(selectors, cat)
		}
		sort.Strings(selectors)
		exported, digest, err := geo.ExportDomains(ctx, file, selectors)
		if err != nil {
			return p, ErrPolicy
		}
		for _, values := range exported {
			for _, value := range values {
				exportBytes += len(value) + 1
			}
		}
		if exportBytes > 16<<20 {
			return p, ErrPolicy
		}
		exports[file] = exported
		p.Sources[file] = digest
	}
	// Explicit node endpoint bootstrap exceptions avoid DNS-over-tunnel recursion.
	var outs struct {
		Outbounds []struct {
			Tag, Protocol string
			Settings      struct {
				Vnext   []struct{ Address string }
				Servers []struct{ Address string }
			}
		}
	}
	if configjson.Decode(files["04_outbounds.json"], &outs) != nil {
		return p, ErrPolicy
	}
	if fallback == "" {
		if len(outs.Outbounds) == 0 {
			return p, ErrPolicy
		}
		first := outs.Outbounds[0]
		switch {
		case first.Protocol == "freedom":
			fallback = "goto direct_only"
		case first.Protocol == "blackhole":
			fallback = "reject 3"
		case vpnTarget == "/"+first.Tag:
			fallback = "goto vpn_only"
		default:
			return p, ErrPolicy
		}
	}
	bootstrap := map[string]bool{}
	for _, out := range outs.Outbounds {
		for _, v := range out.Settings.Vnext {
			if net.ParseIP(v.Address) == nil {
				if !validExpression("full:" + v.Address) {
					return p, ErrPolicy
				}
				bootstrap["full:"+v.Address] = true
			}
		}
		for _, v := range out.Settings.Servers {
			if net.ParseIP(v.Address) == nil {
				if !validExpression("full:" + v.Address) {
					return p, ErrPolicy
				}
				bootstrap["full:"+v.Address] = true
			}
		}
	}
	listBytes := 0
	var sequence []map[string]any
	var additions []plugin
	if len(bootstrap) > 0 {
		var values []string
		for v := range bootstrap {
			values = append(values, v)
		}
		sort.Strings(values)
		rules = append([]compiledRule{{values, "goto direct_only"}}, rules...)
	}
	for i, r := range rules {
		unique := map[string]bool{}
		for _, selector := range r.domains {
			if strings.HasPrefix(selector, "geosite:") {
				selector = "ext:geosite.dat:" + strings.TrimPrefix(selector, "geosite:")
			}
			values := []string{selector}
			if strings.HasPrefix(selector, "ext:") {
				parts := strings.SplitN(selector, ":", 3)
				values = exports[parts[1]][parts[2]]
			}
			for _, v := range values {
				if !validExpression(v) {
					return p, ErrPolicy
				}
				unique[v] = true
				if len(unique) > 250000 {
					return p, ErrPolicy
				}
			}
		}
		if len(unique) == 0 {
			continue
		}
		var values []string
		for v := range unique {
			values = append(values, v)
		}
		sort.Strings(values)
		name := "rule-" + itoa(i) + ".txt"
		for _, value := range values {
			listBytes += len(value) + 1
		}
		if listBytes > 16<<20 {
			return p, ErrPolicy
		}
		p.Lists[name] = []byte(strings.Join(values, "\n") + "\n")
		p.Entries += len(values)
		if p.Entries > 500000 {
			return p, ErrPolicy
		}
		ruleTag := "panel-rule-" + itoa(i)
		additions = append(additions, plugin{ruleTag, "domain_set", args(map[string]any{"files": []string{filepath.Join(dir, generationPlaceholder, name)}})})
		sequence = append(sequence, map[string]any{"matches": []string{"qname $" + ruleTag}, "exec": r.action})
	}
	sequence = append(sequence, map[string]any{"exec": fallback})
	main, _ := get("main", "sequence")
	main.Args = args(sequence)
	d, _ := get("direct_only", "sequence")
	d.Args = args([]map[string]string{{"exec": "$direct"}, {"exec": "accept"}})
	v, _ := get("vpn_only", "sequence")
	v.Args = args([]map[string]string{{"exec": "$tunnel"}, {"exec": "accept"}})
	filtered := []plugin{}
	// Domain providers must be loaded before the sequence that references them.
	for _, v := range plugins {
		if !strings.HasPrefix(v.Tag, "panel-rule-") {
			if v.Type == "domain_set" && (v.Tag == "vpn" || v.Tag == "ads" || v.Tag == "bootstrap") {
				referenced := false
				for _, other := range plugins {
					if other.Tag != v.Tag && strings.Contains(string(other.Args), "$"+v.Tag) {
						referenced = true
					}
				}
				if !referenced {
					continue
				}
			}
			if v.Tag == "main" {
				filtered = append(filtered, additions...)
			}
			filtered = append(filtered, v)
		}
	}
	root["plugins"] = args(filtered)
	p.Config, err = json.MarshalIndent(root, "", "  ")
	if err != nil {
		return p, ErrPolicy
	}
	h := sha256.New()
	_, _ = h.Write(p.Config)
	names := []string{}
	for n := range p.Lists {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_, _ = h.Write([]byte(n))
		_, _ = h.Write(p.Lists[n])
	}
	p.Digest = hex.EncodeToString(h.Sum(nil))
	for _, file := range []string{"02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json"} {
		p.Sources[file] = hash(files[file])
	}
	return p, nil
}

const generationPlaceholder = "panel-generation-placeholder"

func renderedConfig(plan Plan, dir string) []byte {
	// Replace only the quoted generation-directory prefix, not arbitrary text.
	from, _ := json.Marshal(filepath.Join(dir, generationPlaceholder) + string(filepath.Separator))
	to, _ := json.Marshal(filepath.Join(dir, "panel-generation-"+plan.Digest[:16]) + string(filepath.Separator))
	return []byte(strings.ReplaceAll(string(plan.Config), string(from[:len(from)-1]), string(to[:len(to)-1])))
}
func itoa(n int) string { return strconv.Itoa(n) }
