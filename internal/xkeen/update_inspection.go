package xkeen

import (
	"context"
	"regexp"
	"strings"
)

// NativeRelease contains only literal public release identity, never config or
// console text. A changed identity is not proof of a working service or tunnel.
type NativeRelease struct {
	Version        string `json:"version"`
	Channel        string `json:"channel"`
	BuildTimestamp string `json:"buildTimestamp,omitempty"`
}

type NativeUpdateResult struct {
	Before      *NativeRelease `json:"before,omitempty"`
	After       *NativeRelease `json:"after,omitempty"`
	Change      string         `json:"change"`
	XrayProcess string         `json:"xrayProcess"`
}

type updateSnapshot struct {
	release     *NativeRelease
	xrayProcess string
}

// ConfigureUpdateInspection reads fixed installed paths while holding the panel
// lease. It neither executes native commands nor locks external CLI/cron writers.
// Observations live in RAM; the durable job receipt still records interruption.
func (m *Jobs) ConfigureUpdateInspection(d Discovery) {
	m.inspectUpdate = d.updateSnapshot
}

func (d Discovery) updateSnapshot(ctx context.Context) updateSnapshot {
	snapshot := updateSnapshot{xrayProcess: "unknown"}
	if ctx.Err() != nil {
		return snapshot
	}
	_, installed := d.read("opt/sbin/xkeen", maxNativeSource)
	variables, state := d.read("opt/sbin/.xkeen/01_info/01_info_variable.sh", 64<<10)
	if installed == CapabilityAvailable && state == CapabilityAvailable {
		version, versionOK := updateIdentityAssignment(variables, "xkeen_current_version", `[0-9]+(?:\.[0-9]+){1,3}`)
		channel, channelOK := updateIdentityAssignment(variables, "xkeen_build", `Stable|Beta|Dev`)
		build, buildOK := updateIdentityAssignment(variables, "build_timestamp", `(?:[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} [A-Z0-9+:-]{1,16})?`)
		if versionOK && channelOK && buildOK && version != "" && channel != "" {
			snapshot.release = &NativeRelease{Version: version, Channel: channel,
				BuildTimestamp: build}
			snapshot.release.Channel = strings.ToLower(channel)
		}
	}
	init, state := d.read("opt/etc/init.d/S05xkeen", maxNativeSource)
	if state != CapabilityAvailable || literalAssignment(init, "name_client", `xray|mihomo`) != "xray" {
		return snapshot
	}
	entries, err := d.entries("proc", 4096)
	if err != nil {
		return snapshot
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return updateSnapshot{xrayProcess: "unknown"}
		}
		if !entry.IsDir() || !isDigits(entry.Name()) {
			continue
		}
		comm, state := d.read("proc/"+entry.Name()+"/comm", 256)
		if state == CapabilityMissing {
			continue // Process exited during the read.
		}
		if state != CapabilityAvailable {
			return snapshot
		}
		if strings.TrimSpace(string(comm)) == "xray" {
			snapshot.xrayProcess = "running"
			return snapshot
		}
	}
	snapshot.xrayProcess = "stopped"
	return snapshot
}

// Identity observations accept the stock literal assignment format only. Count
// unsupported assignments too: a later malformed value must not leave an earlier
// valid value looking authoritative. Missing optional build stamps are allowed.
func updateIdentityAssignment(data []byte, name, allowed string) (string, bool) {
	assignment := regexp.MustCompile(`(?:^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `[ \t]*=`)
	literal := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `="(` + allowed + `)"[ \t]*(?:#[^\r\n]*)?\r?$`)
	count := 0
	value := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		matches := assignment.FindAllStringIndex(line, 2)
		if len(matches) == 0 {
			continue
		}
		count += len(matches)
		parsed := literal.FindStringSubmatch(line)
		if count != 1 || parsed == nil {
			return "", false
		}
		value = parsed[1]
	}
	return value, true
}

func updateResult(before, after updateSnapshot) *NativeUpdateResult {
	r := &NativeUpdateResult{Before: before.release, After: after.release, Change: "unknown", XrayProcess: after.xrayProcess}
	if before.release != nil && after.release != nil {
		r.Change = "unchanged"
		if *before.release != *after.release {
			r.Change = "changed"
		}
	}
	return r
}
