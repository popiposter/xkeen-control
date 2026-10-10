package nativequality

import (
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

// probeRouteShadowed reports whether a native routing rule could capture probe
// traffic before the appended probe rule does. Xray matches rules in order and
// the panel appends its temporary probe rule, so every existing rule must be
// restricted to inbound tags that exclude the loopback probe inbound. A rule
// without inboundTag (or one naming the probe inbound) would send the probe to
// a different outbound and the review would measure the wrong node.
func probeRouteShadowed(routing string) bool {
	var doc struct {
		Routing struct {
			Rules []struct {
				InboundTag []string `json:"inboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if configjson.Decode([]byte(routing), &doc) != nil {
		return true
	}
	for _, rule := range doc.Routing.Rules {
		if len(rule.InboundTag) == 0 {
			return true
		}
		for _, tag := range rule.InboundTag {
			if tag == c1.ProbeInboundTag {
				return true
			}
		}
	}
	return false
}
