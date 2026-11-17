package nativequality

import (
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

func TestProvisionalRecheckEpisodeAdmission(t *testing.T) {
	now := time.Date(2026, 11, 16, 7, 40, 0, 0, time.UTC)
	q := quotaReceipt{Version: 1, LastComparisonStartedAt: now.Add(-3 * time.Hour), ProvisionalAt: now.Add(-time.Hour - time.Minute), RecoveryAppliedAt: now.Add(-time.Hour - time.Minute)}
	snapshot := xrayapi.Snapshot{APIReachable: true, RoutingReachable: true, ObservatoryReachable: true, OutboundHealth: []xrayapi.OutboundHealth{{Tag: "proxy-one", Alive: true}, {Tag: "proxy-two", Alive: false}}}
	settings := DefaultSettings()
	next := now.Add(20 * time.Hour)
	pool := []string{"proxy-one", "proxy-two"}
	eligible := func() bool { return provisionalRecheckEligible(q, now, next, settings, snapshot, pool) }
	if !eligible() {
		t.Fatal("healthy partial provisional episode was not eligible")
	}
	q.LastComparisonStartedAt = now.Add(-time.Minute)
	if eligible() {
		t.Fatal("started review did not fence same episode")
	}
	q.LastComparisonStartedAt = now.Add(-3 * time.Hour)
	q.ProvisionalAt = time.Time{}
	if eligible() {
		t.Fatal("missing receipt inferred an episode")
	}
	q.ProvisionalAt = now.Add(-time.Hour - time.Minute)
	q.RecoveryAppliedAt = now.Add(-30 * time.Minute)
	if eligible() {
		t.Fatal("later recovery Apply bypassed hourly wait")
	}
	q.RecoveryAppliedAt = q.ProvisionalAt
	next = now.Add(90 * time.Minute)
	if eligible() {
		t.Fatal("near nightly window duplicated a review")
	}
}

func TestProvisionalRecheckRequiresPositiveHealthAndEnabledPolicy(t *testing.T) {
	now := time.Now().UTC()
	q := quotaReceipt{Version: 1, ProvisionalAt: now.Add(-2 * time.Hour), RecoveryAppliedAt: now.Add(-2 * time.Hour)}
	snapshot := xrayapi.Snapshot{APIReachable: true, RoutingReachable: true, ObservatoryReachable: true, OutboundHealth: []xrayapi.OutboundHealth{{Tag: "proxy-one", Alive: false}}}
	settings := DefaultSettings()
	next := now.Add(20 * time.Hour)
	pool := []string{"proxy-one", "proxy-two"}
	if provisionalRecheckEligible(q, now, next, settings, snapshot, pool) {
		t.Fatal("all-down pool admitted early review")
	}
	snapshot.OutboundHealth = nil
	if provisionalRecheckEligible(q, now, next, settings, snapshot, pool) {
		t.Fatal("unknown health admitted early review")
	}
	snapshot.OutboundHealth = []xrayapi.OutboundHealth{{Tag: "proxy-one", Alive: true}}
	settings.AutoApply = false
	if provisionalRecheckEligible(q, now, next, settings, snapshot, pool) {
		t.Fatal("disabled automatic Apply admitted early review")
	}
	settings.AutoApply = true
	snapshot.Balancer.Override = "manual"
	if provisionalRecheckEligible(q, now, next, settings, snapshot, pool) {
		t.Fatal("native override admitted early review")
	}
}
