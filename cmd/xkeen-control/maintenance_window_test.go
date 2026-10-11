package main

import (
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/nativequality"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

func TestMaintenanceWindowAndOffsetParsing(t *testing.T) {
	if d, ok := maintenanceWindow("04:00"); !ok || d != 4*time.Hour {
		t.Fatal("04:00", d, ok)
	}
	if d, ok := maintenanceWindow("23:45"); !ok || d != 23*time.Hour+45*time.Minute {
		t.Fatal("23:45", d, ok)
	}
	for _, bad := range []string{"", "4:00", "24:00", "04:60", "04-00", "aa:bb"} {
		if _, ok := maintenanceWindow(bad); ok {
			t.Fatal("accepted", bad)
		}
	}
	if loc, ok := parseUTCOffset("+0300"); !ok || time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Unix() != time.Date(2025, 12, 31, 21, 0, 0, 0, time.UTC).Unix() {
		t.Fatal("+0300")
	}
	if loc, ok := parseUTCOffset("-0530"); !ok || time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Unix() != time.Date(2026, 1, 1, 5, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("-0530")
	}
	for _, bad := range []string{"", "0300", "+03:00", "+1500", "+03x0"} {
		if _, ok := parseUTCOffset(bad); ok {
			t.Fatal("accepted", bad)
		}
	}
}

func TestMaintenanceReportUsesDisplayNamesAndRankOrder(t *testing.T) {
	start := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	m := nativequality.MaintenanceStatus{StartedAt: start, FinishedAt: start.Add(40 * time.Minute), ReviewStarted: true, GeodataState: "completed"}
	st := nativequality.Status{TotalEligible: 46, RTTValidCount: 41, AttemptedCount: 18, ValidCount: 15, PoolDecision: "best-six", AppliedState: "applied",
		ActivePool: []string{"tag-b", "tag-a", "tag-c"}, NativeSelected: "tag-a",
		Ranking:  []nativequality.RankedNode{{Tag: "tag-a", Rank: 1}, {Tag: "tag-b", Rank: 2}},
		Progress: c1.AdaptivePerformanceStatus{Candidates: []c1.AdaptiveCandidateStatus{{Tag: "tag-a", RTTMS: 40, DownloadBPS: 1e7, UploadBPS: 2e6, Valid: true}, {Tag: "tag-b", RTTMS: 60, Valid: false}}}}
	registry := []nodes.PublicNode{{OutboundTag: "tag-a", DisplayName: "Frankfurt", CountryCode: "DE", Address: "secret.example"}, {OutboundTag: "tag-b", Name: "Helsinki"}}
	subs := map[string]nodes.AutoRefreshStatus{
		"s1": {LastAttemptAt: start.Add(time.Minute).Format(time.RFC3339), LastResult: "updated"},
		"s2": {LastAttemptAt: start.Add(2 * time.Minute).Format(time.RFC3339), State: "failed", ErrorCode: "fetch-failed"},
		"s3": {LastAttemptAt: start.Add(-time.Hour).Format(time.RFC3339), LastResult: "updated"},
	}
	r := maintenanceReport(m, st, registry, subs, time.FixedZone("MSK", 3*3600))
	if r.Subscriptions != 2 || r.SubscriptionsUpdated != 1 || r.SubscriptionsFailed != 1 || r.Answered != 41 || r.Decision != "best-six" || r.StartedAt.Hour() != 4 {
		t.Fatalf("report %+v", r)
	}
	if len(r.Pool) != 3 || r.Pool[0].Name != "Frankfurt" || !r.Pool[0].Active || !r.Pool[0].Measured || r.Pool[1].Name != "Helsinki" || r.Pool[1].Measured || r.Pool[2].Name != "unknown node" {
		t.Fatalf("pool %+v", r.Pool)
	}
	for _, node := range r.Pool {
		if strings.Contains(node.Name, "secret.example") || strings.Contains(node.Name, "tag-") {
			t.Fatal("endpoint or tag leaked", node)
		}
	}
}
