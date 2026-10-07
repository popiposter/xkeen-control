package xkeen

import (
	"context"
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
		version := literalAssignment(variables, "xkeen_current_version", `[0-9]+(?:\.[0-9]+){1,3}`)
		channel := strings.ToLower(literalAssignment(variables, "xkeen_build", `Stable|Beta|Dev`))
		if version != "" && channel != "" {
			snapshot.release = &NativeRelease{Version: version, Channel: channel,
				BuildTimestamp: literalAssignment(variables, "build_timestamp", `[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} [A-Z0-9+:-]{1,16}`)}
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
