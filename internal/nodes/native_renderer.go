package nodes

import (
	"encoding/json"
	"errors"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

// RenderNative replaces only outbounds owned by the previous registry. Native
// order (including the default first outbound) and unrelated fields survive.
// A colliding external tag is not silently adopted by the panel.
func RenderNative(existing []byte, previous, next Registry) ([]byte, error) {
	if err := previous.Validate(); err != nil {
		return nil, err
	}
	if err := next.Validate(); err != nil {
		return nil, err
	}
	object, err := configjson.DecodeObject(existing)
	if err != nil {
		return nil, errors.New("native outbound configuration is invalid")
	}
	var current []json.RawMessage
	if raw, ok := object["outbounds"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &current) != nil {
			return nil, errors.New("native outbounds must be an array")
		}
	}
	owned := make(map[string]bool, len(previous.Nodes))
	for _, node := range previous.Nodes {
		owned[node.OutboundTag] = true
	}
	replacements := make(map[string]json.RawMessage, len(next.Nodes))
	for _, node := range next.SortedNodes() {
		if !node.Enabled {
			continue
		}
		raw, err := renderNode(node)
		if err != nil {
			return nil, err
		}
		replacements[node.OutboundTag] = raw
	}
	seen := make(map[string]bool, len(current))
	kept := make([]json.RawMessage, 0, len(current)+len(next.Nodes))
	for index, raw := range current {
		var outbound map[string]json.RawMessage
		if json.Unmarshal(raw, &outbound) != nil || outbound == nil {
			return nil, errors.New("invalid native outbound")
		}
		var tag string
		if value, ok := outbound["tag"]; ok {
			if json.Unmarshal(value, &tag) != nil {
				return nil, errors.New("invalid native outbound tag")
			}
		}
		if tag != "" && seen[tag] {
			return nil, errors.New("duplicate native outbound tag")
		}
		if tag != "" {
			seen[tag] = true
		}
		if !owned[tag] {
			kept = append(kept, raw)
		} else if replacement, ok := replacements[tag]; ok {
			kept = append(kept, replacement)
		} else if index == 0 {
			return nil, errors.New("change the default outbound before removing or disabling its managed node")
		}
	}
	for _, node := range next.SortedNodes() {
		if seen[node.OutboundTag] && !owned[node.OutboundTag] {
			return nil, errors.New("managed node conflicts with native outbound")
		}
		if !node.Enabled || seen[node.OutboundTag] {
			continue
		}
		kept = append(kept, replacements[node.OutboundTag])
	}
	object["outbounds"], err = json.Marshal(kept)
	if err != nil {
		return nil, errors.New("unable to encode native outbounds")
	}
	result, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return nil, errors.New("unable to encode native configuration")
	}
	return append(result, '\n'), nil
}
