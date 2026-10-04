package xkeen

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

var ErrAttachmentConflict = errors.New("native configuration needs explicit integration review")

// BuildAttachment builds a minimal candidate from native files. It never writes
// files, starts services, or replaces native interception, DNS or scheduling.
// The caller must snapshot, validate the complete candidate with Xray, and commit
// under the panel operation lease before recording successful attachment.
func BuildAttachment(files map[string][]byte) (map[string][]byte, error) {
	if len(files) == 0 || len(files) > maxNativeConfigFiles {
		return nil, ErrAttachmentConflict
	}
	objects := make(map[string]map[string]json.RawMessage, len(files))
	var routingFile, outboundsFile string
	seenInbounds, seenOutbounds := map[string]bool{}, map[string]bool{}
	for name, data := range files {
		if len(data) > maxNativeConfig {
			return nil, ErrAttachmentConflict
		}
		object, err := configjson.DecodeObject(data)
		if err != nil {
			return nil, ErrAttachmentConflict
		}
		objects[name] = object
		for _, section := range []string{"api", "observatory", "burstObservatory"} {
			if _, ok := object[section]; ok {
				return nil, ErrAttachmentConflict
			}
		}
		if _, ok := object["routing"]; ok {
			if routingFile != "" {
				return nil, ErrAttachmentConflict
			}
			routingFile = name
		}
		for _, section := range []string{"inbounds", "outbounds"} {
			raw, ok := object[section]
			if !ok {
				continue
			}
			if section == "outbounds" {
				if outboundsFile != "" {
					return nil, ErrAttachmentConflict
				}
				outboundsFile = name
			}
			var entries []map[string]json.RawMessage
			if string(raw) == "null" || json.Unmarshal(raw, &entries) != nil {
				return nil, ErrAttachmentConflict
			}
			for _, entry := range entries {
				if entry == nil {
					return nil, ErrAttachmentConflict
				}
				var tag string
				if raw, exists := entry["tag"]; exists && json.Unmarshal(raw, &tag) != nil {
					return nil, ErrAttachmentConflict
				}
				seen := seenInbounds
				if section == "outbounds" {
					seen = seenOutbounds
				}
				if tag != "" && seen[tag] {
					return nil, ErrAttachmentConflict
				}
				seen[tag] = true
				if tag == "api" || tag == "probe" {
					return nil, ErrAttachmentConflict
				}
				if section == "outbounds" && strings.HasPrefix(tag, "proxy-node-") {
					return nil, ErrAttachmentConflict
				}
				if section == "inbounds" {
					var port int
					_ = json.Unmarshal(entry["port"], &port)
					if port == 10085 || port == 10808 {
						return nil, ErrAttachmentConflict
					}
				}
			}
		}
	}
	if routingFile == "" {
		return nil, ErrAttachmentConflict
	}
	if outboundsFile != "" && outboundsFile != "04_outbounds.json" {
		return nil, ErrAttachmentConflict
	}
	if outboundsFile == "" {
		outboundsFile = "04_outbounds.json"
		if _, ok := objects[outboundsFile]; !ok {
			objects[outboundsFile] = map[string]json.RawMessage{}
		}
	}
	for _, name := range []string{"07_observatory.json", "08_api.json"} {
		if _, exists := files[name]; exists {
			return nil, ErrAttachmentConflict
		}
	}
	var routing map[string]json.RawMessage
	if json.Unmarshal(objects[routingFile]["routing"], &routing) != nil || routing == nil {
		return nil, ErrAttachmentConflict
	}
	var balancers []map[string]json.RawMessage
	if raw, ok := routing["balancers"]; ok && json.Unmarshal(raw, &balancers) != nil {
		return nil, ErrAttachmentConflict
	}
	for _, balancer := range balancers {
		var tag string
		if json.Unmarshal(balancer["tag"], &tag) != nil || tag == "bal-proxy" {
			return nil, ErrAttachmentConflict
		}
	}
	var rules []map[string]json.RawMessage
	if json.Unmarshal(routing["rules"], &rules) != nil {
		return nil, ErrAttachmentConflict
	}
	for _, rule := range rules {
		if rule == nil {
			return nil, ErrAttachmentConflict
		}
		// Temporary per-node probe rules are appended through Xray's API.
		// Existing catch-all rules could shadow them and measure DIRECT traffic.
		var inboundTags []string
		if json.Unmarshal(rule["inboundTag"], &inboundTags) != nil || len(inboundTags) == 0 {
			return nil, ErrAttachmentConflict
		}
		for _, tag := range inboundTags {
			if tag == "api" || tag == "probe" || tag == "" {
				return nil, ErrAttachmentConflict
			}
		}
		var outbound string
		_ = json.Unmarshal(rule["outboundTag"], &outbound)
		// Only replace the exact unresolved placeholder shipped by native XKeen.
		// A real, operator-configured native outbound remains untouched.
		if outbound == "vless-reality" && !seenOutbounds[outbound] {
			var tags []string
			if len(rule) != 2 || json.Unmarshal(rule["inboundTag"], &tags) != nil || len(tags) != 2 ||
				tags[0] != "force-proxy-redirect" || tags[1] != "force-proxy-tproxy" {
				return nil, ErrAttachmentConflict
			}
			delete(rule, "outboundTag")
			rule["balancerTag"] = json.RawMessage(`"bal-proxy"`)
		}
	}
	rules = append([]map[string]json.RawMessage{
		{"type": json.RawMessage(`"field"`), "inboundTag": json.RawMessage(`["api"]`), "outboundTag": json.RawMessage(`"api"`)},
	}, rules...)
	balancers = append(balancers, map[string]json.RawMessage{"tag": json.RawMessage(`"bal-proxy"`), "selector": json.RawMessage(`["proxy-node-"]`), "fallbackTag": json.RawMessage(`"block"`), "strategy": json.RawMessage(`{"type":"leastPing"}`)})
	routing["rules"], _ = json.Marshal(rules)
	routing["balancers"], _ = json.Marshal(balancers)
	objects[routingFile]["routing"], _ = json.Marshal(routing)
	var outbounds []map[string]json.RawMessage
	if raw, ok := objects[outboundsFile]["outbounds"]; ok {
		_ = json.Unmarshal(raw, &outbounds)
	}
	for _, fixed := range []struct{ tag, protocol string }{{"direct", "freedom"}, {"block", "blackhole"}} {
		if seenOutbounds[fixed.tag] {
			for _, outbound := range outbounds {
				var tag, protocol string
				_ = json.Unmarshal(outbound["tag"], &tag)
				_ = json.Unmarshal(outbound["protocol"], &protocol)
				if tag == fixed.tag && protocol != fixed.protocol {
					return nil, ErrAttachmentConflict
				}
			}
			continue
		}
		tag, _ := json.Marshal(fixed.tag)
		protocol, _ := json.Marshal(fixed.protocol)
		outbounds = append(outbounds, map[string]json.RawMessage{"tag": tag, "protocol": protocol})
	}
	objects[outboundsFile]["outbounds"], _ = json.Marshal(outbounds)
	changed := make(map[string][]byte, 4)
	for _, name := range []string{routingFile, outboundsFile} {
		data, err := json.MarshalIndent(objects[name], "", "  ")
		if err != nil {
			return nil, ErrAttachmentConflict
		}
		changed[name] = append(data, '\n')
	}
	changed["07_observatory.json"] = []byte(`{"observatory":{"subjectSelector":["proxy-node-"],"probeUrl":"https://www.google.com/generate_204","probeInterval":"5m","enableConcurrency":true}}` + "\n")
	changed["08_api.json"] = []byte(`{"api":{"tag":"api","services":["RoutingService","ObservatoryService","StatsService"]},"inbounds":[{"tag":"api","protocol":"tunnel","listen":"127.0.0.1","port":10085,"settings":{"address":"127.0.0.1"}},{"tag":"probe","protocol":"http","listen":"127.0.0.1","port":10808}]}` + "\n")
	return changed, nil
}
