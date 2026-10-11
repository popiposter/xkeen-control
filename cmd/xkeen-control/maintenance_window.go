package main

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// routerLocation returns the router's local zone. Entware usually has no
// zone database, so the fixed offset that the system date reports is used;
// time.Local is the fallback.
func routerLocation() *time.Location {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "date", "+%z").Output()
	if err != nil {
		return time.Local
	}
	if loc, ok := parseUTCOffset(strings.TrimSpace(string(out))); ok {
		return loc
	}
	return time.Local
}

// parseUTCOffset parses a "+hhmm" or "-hhmm" offset.
func parseUTCOffset(value string) (*time.Location, bool) {
	if len(value) != 5 || value[0] != '+' && value[0] != '-' {
		return nil, false
	}
	hours, errH := strconv.Atoi(value[1:3])
	minutes, errM := strconv.Atoi(value[3:5])
	if errH != nil || errM != nil || hours > 14 || minutes > 59 {
		return nil, false
	}
	seconds := hours*3600 + minutes*60
	if value[0] == '-' {
		seconds = -seconds
	}
	return time.FixedZone("UTC"+value, seconds), true
}

// maintenanceWindow parses an "HH:MM" local start time.
func maintenanceWindow(value string) (time.Duration, bool) {
	hours, minutes, found := strings.Cut(value, ":")
	if !found || len(hours) != 2 || len(minutes) != 2 {
		return 0, false
	}
	h, errH := strconv.Atoi(hours)
	m, errM := strconv.Atoi(minutes)
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, true
}
