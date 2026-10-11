package main

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/nativequality"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/notifications"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

// routerLocation returns the router's local zone: the Keenetic POSIX zone in
// /etc/TZ (for example MSK-3), then the offset the system date reports, then
// time.Local. Entware has no zone database, and its date may print UTC.
func routerLocation() *time.Location {
	if loc, ok := xkeen.RouterLocation("/etc/TZ"); ok {
		return loc
	}
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

// maintenanceReport builds the closed Telegram report for one finished window
// from the review status, the node display names and the subscription
// statuses. Only endpoint-free node labels and measured figures leave the
// panel.
func maintenanceReport(m nativequality.MaintenanceStatus, st nativequality.Status, registry []nodes.PublicNode, subscriptions map[string]nodes.AutoRefreshStatus, loc *time.Location) notifications.MaintenanceReport {
	if loc == nil {
		loc = time.Local
	}
	report := notifications.MaintenanceReport{StartedAt: m.StartedAt.In(loc), FinishedAt: m.FinishedAt.In(loc), ReviewStarted: m.ReviewStarted, Geodata: m.GeodataState}
	for _, sub := range subscriptions {
		attempted, err := time.Parse(time.RFC3339, sub.LastAttemptAt)
		if err != nil || attempted.Before(m.StartedAt.Truncate(time.Second)) {
			continue
		}
		report.Subscriptions++
		switch {
		case sub.ErrorCode != "" || sub.State == "failed" || sub.State == "deferred":
			report.SubscriptionsFailed++
		case sub.LastResult == "updated":
			report.SubscriptionsUpdated++
		}
	}
	if m.ReviewStarted {
		report.Eligible, report.Answered = st.TotalEligible, st.RTTValidCount
		report.SpeedTested, report.Valid = st.AttemptedCount, st.ValidCount
		report.Decision, report.Applied = st.PoolDecision, st.AppliedState
	}
	names := make(map[string]nodes.PublicNode, len(registry))
	for _, node := range registry {
		names[node.OutboundTag] = node
	}
	// Figures are reported only from tonight's review, never from an older
	// or manual one.
	samples := make(map[string]c1.AdaptiveCandidateStatus)
	rank := make(map[string]int)
	if m.ReviewStarted {
		for _, sample := range st.Progress.Candidates {
			samples[sample.Tag] = sample
		}
		for _, node := range st.Ranking {
			rank[node.Tag] = node.Rank
		}
	}
	pool := append([]string(nil), st.ActivePool...)
	sort.SliceStable(pool, func(i, j int) bool {
		ri, rj := rank[pool[i]], rank[pool[j]]
		return ri != 0 && (rj == 0 || ri < rj)
	})
	for i, tag := range pool {
		entry := notifications.MaintenanceNode{Name: "unknown node"}
		if node, ok := names[tag]; ok {
			entry.Name = nodes.ReportLabel(node, i+1)
		}
		if sample, ok := samples[tag]; ok && sample.Valid {
			entry.Measured, entry.RTTMS, entry.DownBPS, entry.UpBPS = true, sample.RTTMS, sample.DownloadBPS, sample.UploadBPS
		}
		report.Pool = append(report.Pool, entry)
	}
	return report
}
