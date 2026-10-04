package xkeen

import (
	"context"
	"encoding/json"
	"time"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

type EditorProjection struct {
	Digest      string             `json:"digest"`
	DNS         *DNSFields         `json:"dns,omitempty"`
	Routing     *RoutingFields     `json:"routing,omitempty"`
	Observatory *ObservatoryFields `json:"observatory,omitempty"`
}
type DNSFields struct {
	QueryStrategy   string `json:"queryStrategy"`
	DisableCache    bool   `json:"disableCache"`
	DisableFallback bool   `json:"disableFallback"`
}
type RoutingFields struct {
	DomainStrategy string `json:"domainStrategy"`
}
type ObservatoryFields struct {
	ProbeInterval     string `json:"probeInterval"`
	EnableConcurrency bool   `json:"enableConcurrency"`
}

func (e *ConfigEditor) Read(ctx context.Context) (EditorProjection, error) {
	snapshot, err := e.Snapshot(ctx)
	if err != nil {
		return EditorProjection{}, err
	}
	result := EditorProjection{Digest: snapshot.Digest}
	for _, area := range []struct {
		file, key string
		target    any
	}{
		{"02_dns.json", "dns", &DNSFields{QueryStrategy: "UseIP"}},
		{"05_routing.json", "routing", &RoutingFields{DomainStrategy: "AsIs"}},
		{"07_observatory.json", "observatory", &ObservatoryFields{}},
	} {
		data, exists := snapshot.files[area.file]
		if !exists {
			continue
		}
		object, err := configjson.DecodeObject(data)
		if err != nil {
			return EditorProjection{}, ErrConfig
		}
		raw, exists := object[area.key]
		if !exists {
			continue
		}
		if json.Unmarshal(raw, area.target) != nil {
			return EditorProjection{}, ErrConfig
		}
		switch value := area.target.(type) {
		case *DNSFields:
			if value.QueryStrategy != "UseIP" && value.QueryStrategy != "UseIPv4" && value.QueryStrategy != "UseIPv6" {
				value.QueryStrategy = ""
			}
			result.DNS = value
		case *RoutingFields:
			if value.DomainStrategy != "AsIs" && value.DomainStrategy != "IPIfNonMatch" && value.DomainStrategy != "IPOnDemand" {
				value.DomainStrategy = ""
			}
			result.Routing = value
		case *ObservatoryFields:
			raw, _ := json.Marshal(value.ProbeInterval)
			if _, err := ValidateEditorValue("observatory", "probeInterval", raw); err != nil {
				value.ProbeInterval = ""
			}
			result.Observatory = value
		}
	}
	return result, nil
}

func ValidateEditorValue(area, field string, raw json.RawMessage) (any, error) {
	if string(raw) == "null" {
		return nil, ErrConfig
	}
	if area == "dns" && (field == "disableCache" || field == "disableFallback") || area == "observatory" && field == "enableConcurrency" {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return nil, ErrConfig
		}
		return value, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) > 32 {
		return nil, ErrConfig
	}
	if area == "dns" && field == "queryStrategy" && (value == "UseIP" || value == "UseIPv4" || value == "UseIPv6") {
		return value, nil
	}
	if area == "routing" && field == "domainStrategy" && (value == "AsIs" || value == "IPIfNonMatch" || value == "IPOnDemand") {
		return value, nil
	}
	if area == "observatory" && field == "probeInterval" {
		d, err := time.ParseDuration(value)
		if err == nil && d >= 5*time.Second && d <= 24*time.Hour {
			return value, nil
		}
	}
	return nil, ErrConfig
}
