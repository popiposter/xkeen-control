package xkeen

import (
	"context"
	"os"
	"strings"
)

// stoppedForAttachment requires positive process visibility, unlike the
// display-only XrayRunning boolean. A vanished PID is allowed; an unreadable
// live PID or missing proc filesystem is not evidence of a stopped service.
func (d Discovery) stoppedForAttachment(ctx context.Context) bool {
	if _, state := d.read("proc/1/comm", 256); state != CapabilityAvailable {
		return false
	}
	entries, err := d.entries("proc", 8192)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return false
		}
		if !isDigits(entry.Name()) {
			continue
		}
		name := "proc/" + entry.Name()
		data, state := d.read(name+"/comm", 256)
		if state != CapabilityAvailable {
			if _, err := os.Lstat(d.path(name)); os.IsNotExist(err) {
				continue
			}
			return false
		}
		if strings.TrimSpace(string(data)) == "xray" {
			return false
		}
	}
	return ctx.Err() == nil
}
