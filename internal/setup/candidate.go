package setup

import (
	"encoding/json"

	"github.com/popiposter/xkeen-control/config/presets"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

// Candidate composes one full fresh generation. It never saves registry or
// native files; Preview/StageTransfer remain the only transaction owner.
func Candidate(native map[string][]byte, registry nodes.Registry) (map[string][]byte, []byte, error) {
	if registry.Validate() != nil || len(registry.Nodes) == 0 {
		return nil, nil, ErrState
	}
	enabled := 0
	for _, n := range registry.Nodes {
		if n.Stale || n.Missing {
			return nil, nil, ErrState
		}
		if n.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		return nil, nil, ErrState
	}
	files := make(map[string][]byte, len(native)+2)
	for name, b := range native {
		files[name] = append([]byte(nil), b...)
	}
	attached, err := xkeen.BuildAttachment(files)
	if err != nil {
		return nil, nil, ErrState
	}
	for name, b := range attached {
		files[name] = b
	}
	routing, err := configjson.DecodeObject(files["05_routing.json"])
	if err != nil {
		return nil, nil, ErrState
	}
	var route map[string]json.RawMessage
	if json.Unmarshal(routing["routing"], &route) != nil || route == nil {
		return nil, nil, ErrState
	}
	var ref struct {
		Routing map[string]json.RawMessage `json:"routing"`
	}
	if json.Unmarshal(presets.CompactSelective, &ref) != nil {
		return nil, nil, ErrState
	}
	var rules []json.RawMessage
	if json.Unmarshal(ref.Routing["rules"], &rules) != nil {
		return nil, nil, ErrState
	}
	rules = append([]json.RawMessage{
		json.RawMessage(`{"type":"field","inboundTag":["api"],"outboundTag":"api"}`),
	}, rules...)
	for key, value := range ref.Routing {
		if key != "rules" {
			route[key] = value
		}
	}
	route["rules"], _ = json.Marshal(rules)
	routing["routing"], _ = json.Marshal(route)
	files["05_routing.json"], _ = json.Marshal(routing)
	files["04_outbounds.json"], err = nodes.RenderNative(files["04_outbounds.json"], nodes.NewRegistry(), registry)
	if err != nil {
		return nil, nil, ErrState
	}
	// Official typical DNS basis: system resolver, no custom interception.
	files["02_dns.json"] = []byte("{}\n")
	data, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return nil, nil, ErrState
	}
	return files, data, nil
}
