package configview

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/popiposter/xkeen-control/internal/benchmarkpolicy"
)

const maxConfigFileSize = 4 << 20
const maxRuleDisplayLabelRunes = 128

type Summary struct {
	Available     bool
	Routing       RoutingSummary
	DNS           DNSSummary
	Observatory   ObservatorySummary
	SpeedBalancer SpeedBalancerSummary
}

type RoutingSummary struct {
	RuleCount int
	RuleTags  []string
	Balancers []string
}

type DNSSummary struct {
	Upstreams []DNSUpstream
}

type DNSUpstream struct {
	Host string
	Tag  string
}

type ObservatorySummary struct {
	SubjectSelectors []string
	ProbeInterval    string
}

type SpeedBalancerSummary struct {
	Enabled       bool
	IntervalMin   int
	Hysteresis    int
	Balancer      string
	EligibleNodes int
	PayloadBytes  int64
	NodeSeconds   int
	MaxBytes      int64
	MaxSeconds    int
}

type Reader struct {
	ConfigDir       string
	XkeenConfigPath string
}

func NewReader(configDir, xkeenConfigPath string) Reader {
	if configDir == "" {
		configDir = "/opt/etc/xray/configs"
	}
	if xkeenConfigPath == "" {
		xkeenConfigPath = "/opt/etc/xkeen/xkeen.json"
	}
	return Reader{ConfigDir: configDir, XkeenConfigPath: xkeenConfigPath}
}

func (r Reader) Read(ctx context.Context) Summary {
	_ = ctx
	result := Summary{}
	var loaded int

	if raw, ok := r.readFile("05_routing.json"); ok {
		var document struct {
			Routing struct {
				Rules []struct {
					RuleTag string `json:"ruleTag"`
				} `json:"rules"`
				Balancers []struct {
					Tag string `json:"tag"`
				} `json:"balancers"`
			} `json:"routing"`
		}
		if json.Unmarshal(raw, &document) == nil {
			result.Routing.RuleCount = len(document.Routing.Rules)
			for _, rule := range document.Routing.Rules {
				if label := safeRuleDisplayLabel(rule.RuleTag); label != "" {
					result.Routing.RuleTags = append(result.Routing.RuleTags, label)
				}
			}
			for _, balancer := range document.Routing.Balancers {
				if safeConfigLabel(balancer.Tag) != "" {
					result.Routing.Balancers = append(result.Routing.Balancers, balancer.Tag)
				}
			}
			loaded++
		}
	}

	if raw, ok := r.readFile("02_dns.json"); ok {
		var document struct {
			DNS struct {
				Servers []json.RawMessage `json:"servers"`
			} `json:"dns"`
		}
		if json.Unmarshal(raw, &document) == nil {
			for _, server := range document.DNS.Servers {
				if upstream, ok := parseDNSServer(server); ok {
					result.DNS.Upstreams = append(result.DNS.Upstreams, upstream)
				}
			}
			loaded++
		}
	}

	if raw, ok := r.readFile("07_observatory.json"); ok {
		var document struct {
			Observatory struct {
				SubjectSelector []string `json:"subjectSelector"`
				ProbeInterval   string   `json:"probeInterval"`
			} `json:"observatory"`
		}
		if json.Unmarshal(raw, &document) == nil {
			for _, selector := range document.Observatory.SubjectSelector {
				if safeConfigLabel(selector) != "" {
					result.Observatory.SubjectSelectors = append(result.Observatory.SubjectSelectors, selector)
				}
			}
			result.Observatory.ProbeInterval = boundedText(document.Observatory.ProbeInterval, 32)
			loaded++
		}
	}

	if raw, ok := r.readXkeenConfig(); ok {
		var document struct {
			Xkeen struct {
				Xray struct {
					SpeedBalancer struct {
						Enabled    bool   `json:"enabled"`
						Interval   int    `json:"interval"`
						Hysteresis int    `json:"hysteresis"`
						Balancer   string `json:"balancer"`
					} `json:"speed_balancer"`
				} `json:"xray"`
			} `json:"xkeen"`
		}
		if json.Unmarshal(raw, &document) == nil {
			policy := benchmarkpolicy.Parse(raw)
			result.SpeedBalancer = SpeedBalancerSummary{
				Enabled:       document.Xkeen.Xray.SpeedBalancer.Enabled,
				IntervalMin:   nonNegative(document.Xkeen.Xray.SpeedBalancer.Interval),
				Hysteresis:    nonNegative(document.Xkeen.Xray.SpeedBalancer.Hysteresis),
				Balancer:      safeConfigLabel(document.Xkeen.Xray.SpeedBalancer.Balancer),
				EligibleNodes: policy.EligibleNodes,
				PayloadBytes:  policy.PayloadBytes,
				NodeSeconds:   policy.NodeSeconds,
				MaxBytes:      policy.MaxBytes,
				MaxSeconds:    policy.MaxSeconds,
			}
			loaded++
		}
	}

	result.Available = loaded > 0
	sort.Strings(result.Routing.RuleTags)
	sort.Strings(result.Routing.Balancers)
	sort.Slice(result.DNS.Upstreams, func(i, j int) bool {
		if result.DNS.Upstreams[i].Host == result.DNS.Upstreams[j].Host {
			return result.DNS.Upstreams[i].Tag < result.DNS.Upstreams[j].Tag
		}
		return result.DNS.Upstreams[i].Host < result.DNS.Upstreams[j].Host
	})
	return result
}

func (r Reader) readFile(name string) ([]byte, bool) {
	return readBounded(r.ConfigDir + string(os.PathSeparator) + name)
}

func (r Reader) readXkeenConfig() ([]byte, bool) {
	return readBounded(r.XkeenConfigPath)
}

func readBounded(path string) ([]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxConfigFileSize+1))
	if err != nil || len(contents) > maxConfigFileSize {
		return nil, false
	}
	return contents, true
}

func safeUpstreamHost(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	parsed, err := url.Parse(address)
	if err == nil && parsed.Hostname() != "" {
		return boundedText(parsed.Hostname(), 128)
	}
	if safeConfigLabel(address) != "" {
		return boundedText(address, 128)
	}
	return "configured"
}

func parseDNSServer(raw json.RawMessage) (DNSUpstream, bool) {
	var address string
	if json.Unmarshal(raw, &address) == nil {
		return DNSUpstream{Host: safeUpstreamHost(address)}, address != ""
	}
	var server struct {
		Address string `json:"address"`
		Tag     string `json:"tag"`
	}
	if json.Unmarshal(raw, &server) != nil || server.Address == "" {
		return DNSUpstream{}, false
	}
	return DNSUpstream{
		Host: safeUpstreamHost(server.Address),
		Tag:  safeConfigLabel(server.Tag),
	}, true
}

func safeConfigLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-/", r) {
			continue
		}
		return ""
	}
	return value
}

func safeRuleDisplayLabel(value string) string {
	if !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) || (unicode.IsSpace(r) && r != ' ') {
			return ""
		}
	}
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxRuleDisplayLabelRunes {
		return ""
	}
	return value
}

func boundedText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
