package benchmarkpolicy

import (
	"encoding/json"
	"net/url"
	"strconv"
)

const (
	MaxRegistryNodes = 256
	MaxEligibleNodes = 128
	MaxPayloadBytes  = 20 << 20
	MaxNodeSeconds   = 10
)

type Policy struct {
	EligibleNodes int
	PayloadBytes  int64
	NodeSeconds   int
	MaxBytes      int64
	MaxSeconds    int
}

func Parse(raw []byte) Policy {
	var document struct {
		Xkeen struct {
			Xray struct {
				SpeedBalancer struct {
					MaxNodes int    `json:"max_nodes"`
					MaxTime  int    `json:"max_time"`
					TestURL  string `json:"test_url"`
				} `json:"speed_balancer"`
			} `json:"xray"`
		} `json:"xkeen"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return Policy{}
	}
	config := document.Xkeen.Xray.SpeedBalancer
	if config.MaxNodes < 1 || config.MaxNodes > MaxEligibleNodes || config.MaxTime < 1 || config.MaxTime > MaxNodeSeconds {
		return Policy{}
	}
	payload := payloadBytes(config.TestURL)
	if payload < 1 || payload > MaxPayloadBytes {
		return Policy{}
	}
	return Policy{
		EligibleNodes: config.MaxNodes,
		PayloadBytes:  payload,
		NodeSeconds:   config.MaxTime,
		MaxBytes:      int64(config.MaxNodes) * payload,
		MaxSeconds:    config.MaxNodes * config.MaxTime,
	}
}

func payloadBytes(rawURL string) int64 {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return 0
	}
	value := parsed.Query().Get("bytes")
	if value == "" {
		return 0
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil || result < 1 {
		return 0
	}
	return result
}
