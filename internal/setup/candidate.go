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
	if json.Unmarshal(presets.RUSelective, &ref) != nil {
		return nil, nil, ErrState
	}
	var rules []json.RawMessage
	if json.Unmarshal(ref.Routing["rules"], &rules) != nil {
		return nil, nil, ErrState
	}
	rules = append([]json.RawMessage{
		json.RawMessage(`{"type":"field","inboundTag":["api"],"outboundTag":"api"}`),
		json.RawMessage(`{"type":"field","inboundTag":["panel-dns-socks"],"balancerTag":"bal-proxy"}`),
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
	inbounds, err := configjson.DecodeObject(files["03_inbounds.json"])
	if err != nil {
		return nil, nil, ErrState
	}
	var ins []map[string]json.RawMessage
	if json.Unmarshal(inbounds["inbounds"], &ins) != nil {
		return nil, nil, ErrState
	}
	for _, in := range ins {
		var tag string
		var port int
		_ = json.Unmarshal(in["tag"], &tag)
		_ = json.Unmarshal(in["port"], &port)
		if tag == "panel-dns-socks" || port == 5310 {
			return nil, nil, ErrState
		}
	}
	var socks map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"tag":"panel-dns-socks","listen":"127.0.0.1","port":5310,"protocol":"socks","settings":{"auth":"noauth","udp":true}}`), &socks)
	ins = append(ins, socks)
	inbounds["inbounds"], _ = json.Marshal(ins)
	files["03_inbounds.json"], _ = json.Marshal(inbounds)
	dns, err := configjson.DecodeObject(files["02_dns.json"])
	if err != nil {
		return nil, nil, ErrState
	}
	dnsSettings := map[string]json.RawMessage{}
	if raw, ok := dns["dns"]; ok && (json.Unmarshal(raw, &dnsSettings) != nil || dnsSettings == nil) {
		return nil, nil, ErrState
	}
	dnsSettings["servers"] = json.RawMessage(`[{"address":"https://1.1.1.1/dns-query","tag":"panel-dns-vpn"},"localhost"]`)
	dns["dns"], _ = json.Marshal(dnsSettings)
	files["02_dns.json"], _ = json.Marshal(dns)
	data, err := nodes.MarshalCanonical(registry)
	if err != nil {
		return nil, nil, ErrState
	}
	return files, data, nil
}
