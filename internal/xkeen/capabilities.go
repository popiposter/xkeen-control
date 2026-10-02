package xkeen

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type CapabilityState string

const (
	CapabilityAvailable   CapabilityState = "available"
	CapabilityMissing     CapabilityState = "missing"
	CapabilityUnknown     CapabilityState = "unknown"
	CapabilityUnsupported CapabilityState = "unsupported"
	maxNativeSource                       = 512 << 10
	maxNativeConfig                       = 2 << 20
	maxNativeConfigFiles                  = 32
)

// Capabilities describes observed files, not a working tunnel or permission to
// execute a command. Discovery never invokes native self-heal/install logic.
type Capabilities struct {
	Installation         CapabilityState `json:"installation"`
	Version              string          `json:"version,omitempty"`
	Channel              string          `json:"channel,omitempty"`
	Core                 string          `json:"core,omitempty"`
	Lifecycle            CapabilityState `json:"lifecycle"`
	Configuration        CapabilityState `json:"configuration"`
	ConfigFiles          int             `json:"configFiles"`
	GeodataFiles         int             `json:"geodataFiles"`
	GeodataCron          CapabilityState `json:"geodataCron"`
	NativeHook           CapabilityState `json:"nativeHook"`
	KernelModules        CapabilityState `json:"kernelModules"`
	XrayRunning          bool            `json:"xrayRunning"`
	APIConfigured        bool            `json:"apiConfigured"`
	PoolConfigured       bool            `json:"poolConfigured"`
	NeedsOnboarding      bool            `json:"needsOnboarding"`
	SpeedBalancer        CapabilityState `json:"speedBalancer"`
	SpeedBalancerEnabled bool            `json:"speedBalancerEnabled"`
}

// Root is only an operator/test filesystem root; it is never request input.
// The zero value discovers the installed appliance using fixed paths.
type Discovery struct{ Root string }

func (d Discovery) path(name string) string {
	if d.Root == "" {
		return filepath.FromSlash("/" + name)
	}
	return filepath.Join(d.Root, filepath.FromSlash(name))
}

func (d Discovery) Inspect(ctx context.Context) Capabilities {
	r := Capabilities{Installation: CapabilityUnknown, Lifecycle: CapabilityUnknown,
		Configuration: CapabilityUnknown, GeodataCron: CapabilityUnknown,
		NativeHook: CapabilityUnknown, KernelModules: CapabilityUnknown, SpeedBalancer: CapabilityUnknown}
	if ctx.Err() != nil {
		return r
	}
	dispatcher, state := d.read("opt/sbin/xkeen", maxNativeSource)
	r.Installation = state
	variables, variablesState := d.read("opt/sbin/.xkeen/01_info/01_info_variable.sh", 64<<10)
	if variablesState == CapabilityAvailable {
		r.Version = literalAssignment(variables, "xkeen_current_version", `[0-9]+(?:\.[0-9]+){1,3}`)
		r.Channel = strings.ToLower(literalAssignment(variables, "xkeen_build", `Stable|Beta|Dev`))
	}
	if state == CapabilityAvailable && (r.Version == "" || r.Channel == "") {
		r.Installation = CapabilityUnknown
	}
	init, initState := d.read("opt/etc/init.d/S05xkeen", maxNativeSource)
	r.Lifecycle = initState
	if initState == CapabilityAvailable {
		r.Core = literalAssignment(init, "name_client", `xray|mihomo`)
		if r.Core == "" || state != CapabilityAvailable || !strings.Contains(string(dispatcher), "XKEEN_FOREGROUND") {
			r.Lifecycle = CapabilityUnsupported
		}
	}
	_, r.NativeHook = d.read("opt/etc/ndm/netfilter.d/proxy.sh", maxNativeSource)
	if data, state := d.read("proc/modules", 256<<10); state == CapabilityAvailable {
		// Presence is deliberately not a claim that the mode-specific set is loaded.
		if len(data) > 0 {
			r.KernelModules = CapabilityAvailable
		} else {
			r.KernelModules = CapabilityMissing
		}
	} else {
		r.KernelModules = state
	}
	r.XrayRunning = processExistsInProc(d.path("proc"), "xray")
	r.Configuration, r.ConfigFiles, r.APIConfigured, r.PoolConfigured = d.configCapabilities(ctx)
	if data, state := d.read("opt/etc/xkeen/xkeen.json", maxNativeConfig); state == CapabilityAvailable {
		object, err := decodeNativeObject(data)
		if err == nil {
			var settings struct {
				Xray struct {
					SpeedBalancer struct {
						Enabled bool `json:"enabled"`
					} `json:"speed_balancer"`
				} `json:"xray"`
			}
			raw, ok := object["xkeen"]
			// A fresh native installation writes {} and uses upstream defaults.
			if !ok || json.Unmarshal(raw, &settings) == nil {
				r.SpeedBalancer = CapabilityUnsupported
				if regexp.MustCompile(`(?m)^\s*-sb\)`).Match(dispatcher) {
					r.SpeedBalancer = CapabilityAvailable
				}
				r.SpeedBalancerEnabled = settings.Xray.SpeedBalancer.Enabled
			}
		}
	}
	if data, state := d.read("opt/var/spool/cron/crontabs/root", maxCronSize); state == CapabilityAvailable {
		r.GeodataCron = CapabilityMissing
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 7 && !strings.HasPrefix(fields[0], "#") &&
				(fields[5] == "/opt/sbin/xkeen" || fields[5] == "xkeen") && fields[6] == "-ug" {
				r.GeodataCron = CapabilityAvailable
			}
		}
	} else {
		r.GeodataCron = state
	}
	// These are the native geodata locations. Count regular files only and do
	// not parse potentially large dat payloads during dashboard discovery.
	if entries, err := d.entries("opt/etc/xray/dat", 128); err == nil {
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".dat") {
				r.GeodataFiles++
			}
		}
	}
	r.NeedsOnboarding = r.Installation == CapabilityAvailable && r.Core == "xray" &&
		r.Configuration == CapabilityAvailable && (!r.APIConfigured || !r.PoolConfigured)
	return r
}

func (d Discovery) configCapabilities(ctx context.Context) (CapabilityState, int, bool, bool) {
	entries, err := d.entries("opt/etc/xray/configs", maxNativeConfigFiles)
	if errors.Is(err, os.ErrNotExist) {
		return CapabilityMissing, 0, false, false
	}
	if err != nil {
		return CapabilityUnknown, 0, false, false
	}
	count, apiService, apiInbound, apiRoute, pool := 0, false, false, false, false
	total := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return CapabilityUnknown, count, false, false
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, state := d.read("opt/etc/xray/configs/"+entry.Name(), maxNativeConfig)
		total += len(data)
		if state != CapabilityAvailable || total > 8<<20 {
			return CapabilityUnknown, count, false, false
		}
		object, err := decodeNativeObject(data)
		if err != nil {
			return CapabilityUnsupported, count, false, false
		}
		count++
		var api struct {
			Tag      string   `json:"tag"`
			Services []string `json:"services"`
		}
		if json.Unmarshal(object["api"], &api) == nil && api.Tag == "api" {
			for _, service := range api.Services {
				apiService = apiService || service == "RoutingService"
			}
		}
		var inbounds []struct {
			Tag    string `json:"tag"`
			Listen string `json:"listen"`
			Port   int    `json:"port"`
		}
		if json.Unmarshal(object["inbounds"], &inbounds) == nil {
			for _, inbound := range inbounds {
				apiInbound = apiInbound || inbound.Tag == "api" && inbound.Listen == "127.0.0.1" && inbound.Port == 10085
			}
		}
		var routing struct {
			Rules []struct {
				InboundTag  []string `json:"inboundTag"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
			Balancers []struct {
				Tag string `json:"tag"`
			} `json:"balancers"`
		}
		if json.Unmarshal(object["routing"], &routing) == nil {
			for _, rule := range routing.Rules {
				for _, tag := range rule.InboundTag {
					apiRoute = apiRoute || tag == "api" && rule.OutboundTag == "api"
				}
			}
			for _, balancer := range routing.Balancers {
				pool = pool || balancer.Tag == "bal-proxy"
			}
		}
	}
	if count == 0 {
		return CapabilityMissing, 0, false, false
	}
	return CapabilityAvailable, count, apiService && apiInbound && apiRoute, pool
}

func literalAssignment(data []byte, name, allowed string) string {
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `="(` + allowed + `)"[ \t]*(?:#[^\r\n]*)?\r?$`)
	matches := pattern.FindAllSubmatch(data, 2)
	if len(matches) != 1 {
		return ""
	}
	return string(matches[0][1])
}

func (d Discovery) read(path string, limit int64) ([]byte, CapabilityState) {
	info, err := os.Lstat(d.path(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, CapabilityMissing
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, CapabilityUnknown
	}
	file, err := os.Open(d.path(path))
	if err != nil {
		return nil, CapabilityUnknown
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, CapabilityUnknown
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, CapabilityUnknown
	}
	return data, CapabilityAvailable
}

func (d Discovery) entries(path string, limit int) ([]os.DirEntry, error) {
	file, err := os.Open(d.path(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(limit + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > limit {
		return nil, errors.New("native directory exceeds supported bound")
	}
	return entries, nil
}
