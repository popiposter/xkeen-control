package nativequality

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

// This is a diagnostic ceiling, not a replacement for omitted native maxRTT.
const diagnosticRTTMS int64 = 10000

type observationCriteria struct {
	maxRTT        int64
	latencySource string
}

func nativeStrategy(text string, index int) (string, map[string]json.RawMessage, error) {
	var doc struct {
		Routing struct {
			Balancers []struct {
				Strategy struct {
					Type     string
					Settings map[string]json.RawMessage
				}
			}
		}
	}
	if configjson.Decode([]byte(text), &doc) != nil || index < 0 || index >= len(doc.Routing.Balancers) {
		return "", nil, ErrUnavailable
	}
	s := doc.Routing.Balancers[index].Strategy
	if s.Type != "leastLoad" && s.Type != "leastPing" {
		return "", nil, ErrUnavailable
	}
	return s.Type, s.Settings, nil
}

// readCriteria reads the native latency limit the review applies to its RTT
// pre-phase. 07_observatory.json must exist, because Apply rewrites it, but
// reviews no longer depend on which outbounds it currently observes: an
// applied pool whose members all vanished must still be reviewable.
func readCriteria(routing, observation string, index int) (observationCriteria, error) {
	c := observationCriteria{maxRTT: diagnosticRTTMS, latencySource: "diagnostic-ceiling"}
	kind, settings, err := nativeStrategy(routing, index)
	if err != nil {
		return c, err
	}
	if raw, ok := settings["maxRTT"]; ok && kind == "leastLoad" {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return c, ErrUnavailable
		}
		d, e := time.ParseDuration(value)
		if e != nil || d <= 0 || d > time.Minute {
			return c, ErrUnavailable
		}
		c.maxRTT = d.Milliseconds()
		c.latencySource = "native-max-rtt"
		if c.maxRTT == 0 {
			return c, ErrUnavailable
		}
	}
	var doc struct {
		Observatory *json.RawMessage `json:"observatory"`
	}
	if configjson.Decode([]byte(observation), &doc) != nil || doc.Observatory == nil {
		return c, ErrUnavailable
	}
	return c, nil
}

func replaceRecommendation(text string, index int, costs []c1.NativeQualityCost, selected []string) ([]byte, error) {
	kind, settings, err := nativeStrategy(text, index)
	if err != nil {
		return nil, err
	}
	output := []byte(text)
	if kind == "leastLoad" {
		if settings == nil {
			settings = map[string]json.RawMessage{}
		}
		raw, e := json.Marshal(costs)
		if e != nil {
			return nil, e
		}
		settings["costs"] = raw
		output, err = configjson.ReplacePath(output, []string{"routing", "balancers", strconv.Itoa(index), "strategy", "settings"}, settings)
		if err != nil {
			return nil, err
		}
	}
	return configjson.ReplacePath(output, []string{"routing", "balancers", strconv.Itoa(index), "selector"}, selected)
}

// qualityProbeInterval is the common native Observatory interval for an
// applied quality pool (REQ-003). Profiles differ only in concurrency.
const qualityProbeInterval = "10s"

// observatoryForPool returns 07_observatory.json observing exactly the pool,
// preserving every other native field such as probeUrl.
func observatoryForPool(text string, pool []string, concurrent bool) ([]byte, error) {
	var doc struct {
		Observatory *json.RawMessage `json:"observatory"`
	}
	if configjson.Decode([]byte(text), &doc) != nil || doc.Observatory == nil || len(pool) == 0 {
		return nil, ErrUnavailable
	}
	out, err := configjson.ReplacePath([]byte(text), []string{"observatory", "subjectSelector"}, pool)
	if err == nil {
		out, err = configjson.ReplacePath(out, []string{"observatory", "probeInterval"}, qualityProbeInterval)
	}
	if err == nil {
		out, err = configjson.ReplacePath(out, []string{"observatory", "enableConcurrency"}, concurrent)
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

// observatoryMatchesPool reports whether 07 already observes exactly the pool
// with the common interval and the profile's concurrency.
func observatoryMatchesPool(text string, pool []string, concurrent bool) bool {
	var doc struct {
		Observatory *struct {
			SubjectSelector   []string
			ProbeInterval     string
			EnableConcurrency bool
		}
	}
	if configjson.Decode([]byte(text), &doc) != nil || doc.Observatory == nil {
		return false
	}
	o := doc.Observatory
	return o.ProbeInterval == qualityProbeInterval && o.EnableConcurrency == concurrent && samePoolMembers(o.SubjectSelector, pool)
}
