package xkeen

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/geodatareader"
)

// Preview evaluates explicit sample facts only. It never resolves DNS, changes
// configuration or selects a member of a native balancer.
type RoutingSample struct {
	Domain  string `json:"domain"`
	IP      string `json:"ip"`
	Inbound string `json:"inbound"`
	Network string `json:"network"`
	Port    int    `json:"port"`
}
type RoutingExample struct {
	State      string `json:"state"`
	Rule       int    `json:"rule,omitempty"`
	Target     string `json:"target,omitempty"`
	TargetKind string `json:"targetKind,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
type verdict int

const (
	unknown verdict = iota
	no
	yes
)

func allConditions(v ...verdict) verdict {
	result := yes
	for _, x := range v {
		if x == no {
			return no
		}
		if x == unknown {
			result = unknown
		}
	}
	return result
}
func anyCondition(v ...verdict) verdict {
	result := no
	for _, x := range v {
		if x == yes {
			return yes
		}
		if x == unknown {
			result = unknown
		}
	}
	return result
}
func negate(v verdict) verdict {
	if v == yes {
		return no
	}
	if v == no {
		return yes
	}
	return unknown
}
func truth(b bool) verdict {
	if b {
		return yes
	}
	return no
}

func PreviewRouting(ctx context.Context, text string, sample RoutingSample, reader *geodatareader.Reader) (RoutingExample, error) {
	bad := errors.New("invalid routing example")
	if len(text) > 512<<10 || len(sample.Domain) > 253 || len(sample.Inbound) > 128 || sample.Port < 0 || sample.Port > 65535 || sample.Network != "" && sample.Network != "tcp" && sample.Network != "udp" {
		return RoutingExample{}, bad
	}
	domain := strings.ToLower(strings.TrimSuffix(sample.Domain, "."))
	if strings.ContainsAny(domain, " /:@") {
		return RoutingExample{}, bad
	}
	var ip netip.Addr
	if sample.IP != "" {
		var err error
		ip, err = netip.ParseAddr(sample.IP)
		if err != nil {
			return RoutingExample{}, bad
		}
		ip = ip.Unmap()
	}
	obj, err := configjson.DecodeObject([]byte(text))
	if err != nil {
		return RoutingExample{}, bad
	}
	var routing struct {
		DomainStrategy string
		Rules          []map[string]json.RawMessage
	}
	if json.Unmarshal(obj["routing"], &routing) != nil || len(routing.Rules) > 2048 {
		return RoutingExample{}, bad
	}
	budget := 32
	cache := map[string]verdict{}
	snapshots := map[string]string{}
	geo := func(pattern, kind, search string) verdict {
		key := kind + "|" + pattern + "|" + search
		if v, ok := cache[key]; ok {
			return v
		}
		if reader == nil || budget == 0 || ctx.Err() != nil {
			return unknown
		}
		file, category := "", ""
		if strings.HasPrefix(pattern, kind+":") {
			file, category = kind+".dat", strings.TrimPrefix(pattern, kind+":")
		} else if strings.HasPrefix(pattern, "ext:") {
			parts := strings.Split(pattern, ":")
			if len(parts) == 3 {
				file, category = parts[1], parts[2]
			}
		}
		if file == "" || category == "" || strings.Contains(category, "@") || !strings.HasPrefix(file, kind) {
			return unknown
		}
		budget--
		result, err := reader.Query(ctx, geodatareader.Request{File: file, Category: category, View: "match", Search: search, Limit: 1, Snapshot: snapshots[file]})
		if err != nil {
			return unknown
		}
		snapshots[file] = result.Snapshot
		// Empty results cannot distinguish an absent category from a non-match.
		// Confirm the exact category exists before declaring a negative.
		v := yes
		if result.Total == 0 {
			if budget == 0 {
				return unknown
			}
			budget--
			categories, err := reader.Query(ctx, geodatareader.Request{File: file, Category: category, View: "categories", Limit: 1, Snapshot: result.Snapshot})
			if err != nil || categories.Total != 1 {
				return unknown
			}
			v = no
		}
		cache[key] = v
		return v
	}
	domainMatch := func(pattern string) verdict {
		if domain == "" {
			return unknown
		}
		if strings.HasPrefix(pattern, "geosite:") || strings.HasPrefix(pattern, "ext:") {
			return geo(pattern, "geosite", domain)
		}
		if strings.HasPrefix(pattern, "regexp:") {
			pattern = strings.TrimPrefix(pattern, "regexp:")
			if len(pattern) > 1024 {
				return unknown
			}
			r, err := regexp.Compile(pattern)
			if err != nil {
				return unknown
			}
			return truth(r.MatchString(domain))
		}
		if strings.HasPrefix(pattern, "domain:") {
			p := strings.ToLower(strings.TrimPrefix(pattern, "domain:"))
			return truth(domain == p || strings.HasSuffix(domain, "."+p))
		}
		if strings.HasPrefix(pattern, "full:") {
			return truth(domain == strings.ToLower(strings.TrimPrefix(pattern, "full:")))
		}
		if strings.HasPrefix(pattern, "dotless:") {
			// Native versions compile the suffix as a regular expression. Keep
			// this uncommon condition uncertain rather than inventing a winner.
			return unknown
		}
		if strings.Contains(pattern, ":") && !strings.HasPrefix(pattern, "keyword:") {
			return unknown
		}
		return truth(strings.Contains(domain, strings.TrimPrefix(pattern, "keyword:")))
	}
	ipMatch := func(pattern string) verdict {
		if !ip.IsValid() {
			return unknown
		}
		if strings.HasPrefix(pattern, "geoip:") || strings.HasPrefix(pattern, "ext:") {
			return geo(pattern, "geoip", ip.String())
		}
		if prefix, err := netip.ParsePrefix(pattern); err == nil {
			return truth(prefix.Contains(ip))
		}
		if addr, err := netip.ParseAddr(pattern); err == nil {
			return truth(addr.Unmap() == ip)
		}
		return unknown
	}
	for index, rule := range routing.Rules {
		if ctx.Err() != nil {
			return RoutingExample{State: "unknown", Rule: index + 1, Reason: "Query budget or time limit reached."}, nil
		}
		conditions := []verdict{}
		var target, balancer, kind string
		for key, raw := range rule {
			switch key {
			case "type":
				if json.Unmarshal(raw, &kind) != nil || kind != "field" {
					conditions = append(conditions, unknown)
				}
			case "outboundTag":
				if json.Unmarshal(raw, &target) != nil {
					conditions = append(conditions, unknown)
				}
			case "balancerTag":
				if json.Unmarshal(raw, &balancer) != nil {
					conditions = append(conditions, unknown)
				}
			case "ruleTag":
			case "domain", "ip", "inboundTag":
				var values []string
				if json.Unmarshal(raw, &values) != nil || len(values) == 0 || len(values) > 2048 {
					conditions = append(conditions, unknown)
					continue
				}
				positive, inverse := []verdict{}, []verdict{}
				for _, pattern := range values {
					if key == "domain" {
						positive = append(positive, domainMatch(pattern))
					} else if key == "inboundTag" {
						if sample.Inbound == "" {
							positive = append(positive, unknown)
						} else {
							positive = append(positive, truth(pattern == sample.Inbound))
						}
					} else if strings.HasPrefix(pattern, "!") {
						inverse = append(inverse, negate(ipMatch(strings.TrimPrefix(pattern, "!"))))
					} else {
						positive = append(positive, ipMatch(pattern))
					}
				}
				group := anyCondition(positive...)
				if len(inverse) > 0 {
					group = anyCondition(group, allConditions(inverse...))
				}
				conditions = append(conditions, group)
			case "network":
				var network string
				if json.Unmarshal(raw, &network) != nil || sample.Network == "" {
					conditions = append(conditions, unknown)
				} else {
					v := no
					for _, n := range strings.Split(network, ",") {
						v = anyCondition(v, truth(n == sample.Network))
					}
					conditions = append(conditions, v)
				}
			case "port":
				if sample.Port == 0 {
					conditions = append(conditions, unknown)
					continue
				}
				ports := string(raw)
				var p string
				if json.Unmarshal(raw, &p) == nil {
					ports = p
				}
				v := no
				for _, part := range strings.Split(ports, ",") {
					bounds := strings.Split(part, "-")
					first, e := strconv.Atoi(bounds[0])
					last := first
					if len(bounds) == 2 {
						last, e = strconv.Atoi(bounds[1])
					}
					if e != nil || len(bounds) > 2 || first < 1 || last > 65535 || last < first {
						v = anyCondition(v, unknown)
					} else {
						v = anyCondition(v, truth(sample.Port >= first && sample.Port <= last))
					}
				}
				conditions = append(conditions, v)
			default:
				conditions = append(conditions, unknown)
			}
		}
		if kind != "field" || len(conditions) == 0 || target == "" && balancer == "" || target != "" && balancer != "" {
			conditions = append(conditions, unknown)
		}
		switch allConditions(conditions...) {
		case no:
			continue
		case unknown:
			return RoutingExample{State: "unknown", Rule: index + 1, Reason: "This earlier rule needs additional facts, DNS resolution, or a condition not supported by this example."}, nil
		case yes:
			result := RoutingExample{State: "matched", Rule: index + 1, Target: target, TargetKind: "outbound"}
			if balancer != "" {
				result.Target = balancer
				result.TargetKind = "balancer"
			}
			return result, nil
		}
	}
	if domain != "" && sample.IP == "" && routing.DomainStrategy == "IPIfNonMatch" {
		return RoutingExample{State: "unknown", Reason: "Xray may resolve the name and make another pass; no DNS query was made."}, nil
	}
	return RoutingExample{State: "default", Reason: "No rule matched these facts. Xray uses its first outbound; no node or DNS query was selected."}, nil
}
