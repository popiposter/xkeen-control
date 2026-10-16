package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/nativequality"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/notifications"
)

func TestUTCOffsetParsing(t *testing.T) {
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

func TestMaintenanceReportUsesEndpointFreeLabelsAndTonightsFigures(t *testing.T) {
	start := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	m := nativequality.MaintenanceStatus{StartedAt: start, FinishedAt: start.Add(40 * time.Minute), SubscriptionsRefreshed: true, ReviewStarted: true, GeodataState: "completed"}
	st := nativequality.Status{TotalEligible: 46, RTTValidCount: 41, AttemptedCount: 18, ValidCount: 15, PoolDecision: "best-pool", AppliedState: "applied",
		ActivePool: []string{"tag-b", "tag-a", "tag-c", "tag-d"},
		Ranking:    []nativequality.RankedNode{{Tag: "tag-a", Rank: 1}, {Tag: "tag-b", Rank: 2}},
		Progress:   c1.AdaptivePerformanceStatus{Candidates: []c1.AdaptiveCandidateStatus{{Tag: "tag-a", RTTMS: 40, DownloadBPS: 1e7, UploadBPS: 2e6, Valid: true}, {Tag: "tag-b", RTTMS: 60, Valid: false}}}}
	registry := []nodes.PublicNode{
		{OutboundTag: "tag-a", Name: "Frankfurt", DisplayName: "🇩🇪 Frankfurt", CountryCode: "DE", Address: "secret.example"},
		{OutboundTag: "tag-b", Name: "Helsinki"},
		{OutboundTag: "tag-c", Name: "Imported node", DisplayName: "🇩🇪 Germany · 203.0.113.7", Address: "203.0.113.7", CountryCode: "DE"},
	}
	subs := map[string]nodes.AutoRefreshStatus{
		"s1": {LastAttemptAt: start.Add(time.Minute).Format(time.RFC3339), LastResult: "updated"},
		"s2": {LastAttemptAt: start.Add(2 * time.Minute).Format(time.RFC3339), State: "failed", ErrorCode: "fetch-failed"},
		"s3": {LastAttemptAt: start.Add(-time.Hour).Format(time.RFC3339), LastResult: "updated"},
	}
	r := maintenanceReport(m, st, registry, subs, time.FixedZone("MSK", 3*3600))
	if r.Subscriptions != 2 || r.SubscriptionsUpdated != 1 || r.SubscriptionsFailed != 1 || r.Answered != 41 || r.Decision != "best-pool" || r.StartedAt.Hour() != 4 {
		t.Fatalf("report %+v", r)
	}
	if len(r.Pool) != 4 || r.Pool[0].Name != "🇩🇪 Frankfurt" || !r.Pool[0].Measured || r.Pool[1].Name != "Helsinki" || r.Pool[1].Measured || r.Pool[2].Name != "🇩🇪 Germany · Node 3" || r.Pool[3].Name != "unknown node" {
		t.Fatalf("pool %+v", r.Pool)
	}
	text := fmt.Sprint(notifications.MaintenanceAlert(r))
	for _, leak := range []string{"203.0.113", "secret.example", "tag-"} {
		if strings.Contains(text, leak) {
			t.Fatal("endpoint or tag leaked", leak)
		}
	}
	// A night whose review did not run reports no figures from older reviews.
	m.ReviewStarted = false
	r = maintenanceReport(m, st, registry, subs, time.UTC)
	if r.Answered != 0 || r.Decision != "" || r.Pool[1].Measured || r.Pool[1].Name != "🇩🇪 Frankfurt" {
		t.Fatalf("stale figures reported %+v", r)
	}
}

func TestRecommendOnlyNightKnowsWhenNothingWouldChange(t *testing.T) {
	m := nativequality.MaintenanceStatus{StartedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now(), SubscriptionsRefreshed: true, ReviewStarted: true, GeodataState: "completed"}
	st := nativequality.Status{State: "completed", ManualSample: true, ReviewTrigger: "nightly", AppliedState: "not-attempted", ActivePool: []string{"a", "b", "c"}, RecommendedPool: []string{"c", "a", "b"}}
	if r := maintenanceReport(m, st, nil, nil, time.UTC); !r.Recommended || !r.RecommendationCurrent {
		t.Fatal("same members not recognised", r)
	}
	st.RecommendedPool = []string{"a", "b", "d"}
	if r := maintenanceReport(m, st, nil, nil, time.UTC); !r.Recommended || r.RecommendationCurrent {
		t.Fatal("a different pool reported as current", r)
	}
	if sameMembers(nil, nil) || sameMembers([]string{"a", "a"}, []string{"a", "b"}) || sameMembers([]string{"a", "b"}, []string{"a", "a"}) {
		t.Fatal("sameMembers edge cases")
	}
}

func TestRecoveryReportUsesEndpointFreeLabels(t *testing.T) {
	registry := []nodes.PublicNode{
		{OutboundTag: "tag-a", Name: "Imported node", DisplayName: "🇫🇮 Finland · 198.51.100.4", Address: "198.51.100.4", CountryCode: "FI"},
		{OutboundTag: "tag-b", Name: "Helsinki"},
	}
	outcome := nativequality.RecoveryStatus{State: "applied", Reason: "selected-member-unusable", Unusable: "tag-a", Pool: []string{"tag-b", "tag-gone"}, CheckedAt: time.Date(2026, 10, 16, 7, 40, 0, 0, time.UTC)}
	r := recoveryReport(outcome, registry, time.FixedZone("MSK", 3*3600))
	if r.Removed != "🇫🇮 Finland" || r.Provisional || !slices.Equal(r.Pool, []string{"Helsinki", "unknown node"}) || r.At.Hour() != 10 {
		t.Fatalf("report %+v", r)
	}
	text := fmt.Sprint(notifications.RecoveryAlert(r))
	for _, leak := range []string{"198.51.100", "tag-"} {
		if strings.Contains(text, leak) {
			t.Fatal("endpoint or tag leaked", leak)
		}
	}
	outcome.Reason, outcome.Unusable = "no-healthy-member", ""
	if r := recoveryReport(outcome, registry, nil); !r.Provisional || r.Removed != "" {
		t.Fatalf("provisional report %+v", r)
	}
}
