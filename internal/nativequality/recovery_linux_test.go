//go:build linux

package nativequality

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

var recoveryIncumbents = []string{"proxy-00", "proxy-01", "proxy-02", "proxy-03", "proxy-04", "proxy-05"}

// recoveryFixture is a settled six-member pool whose members native
// Observatory reports down and whose members fail their targeted probe.
func recoveryFixture(t *testing.T) (*Service, *sweepMeasurement, *sweepReader) {
	t.Helper()
	s, _, _, _ := sweepFixture(t, true)
	s.status = Status{}
	m := s.Measurement.(*sweepMeasurement)
	m.rttDown = map[string]bool{}
	for _, tag := range recoveryIncumbents {
		m.rttDown[tag] = true
	}
	r := s.Reader.(*sweepReader)
	r.mutate = func(snapshot *xrayapi.Snapshot) {
		for i := range snapshot.OutboundHealth {
			if m.rttDown[snapshot.OutboundHealth[i].Tag] {
				snapshot.OutboundHealth[i].Alive = false
			}
		}
	}
	return s, m, r
}

func recoveryPending(t *testing.T, s *Service) bool {
	t.Helper()
	w, err := s.Editor.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return w.Pending != nil
}

func TestNoHealthyMemberNeedsEveryMemberObservedDown(t *testing.T) {
	down := xrayapi.Snapshot{ObservatoryReachable: true, OutboundHealth: []xrayapi.OutboundHealth{{Tag: "a"}, {Tag: "b"}}}
	if !noHealthyMember(down, []string{"a", "b"}) {
		t.Fatal("two observed-down members were not an outage")
	}
	if noHealthyMember(down, []string{"a", "b", "c"}) {
		t.Fatal("a member without an Observatory record was treated as down")
	}
	alive := down
	alive.OutboundHealth = append([]xrayapi.OutboundHealth{{Tag: "b", Alive: true}}, down.OutboundHealth...)
	if noHealthyMember(alive, []string{"a", "b"}) {
		t.Fatal("an alive member was ignored")
	}
	if noHealthyMember(xrayapi.Snapshot{}, []string{"a"}) {
		t.Fatal("an unreachable Observatory was treated as an outage")
	}
	if !noHealthyMember(xrayapi.Snapshot{}, nil) {
		t.Fatal("an all-orphaned pool still had a healthy member")
	}
}

func TestRecoveryAppliesProvisionalPoolOfVerifiedCandidates(t *testing.T) {
	s, m, r := recoveryFixture(t)
	dir := s.Editor.Dir
	marker := setupNativeApply(t, s, dir)
	down := r.mutate
	r.mutate = func(snapshot *xrayapi.Snapshot) {
		if _, err := os.Stat(marker); err == nil {
			// After the restart Xray selects inside the provisional pool.
			snapshot.Balancer.NativeSelected = "proxy-06"
			return
		}
		down(snapshot)
	}
	s.recoverPool(context.Background())
	v := s.Read()
	want := []string{"proxy-06", "proxy-07", "proxy-08", "proxy-09", "proxy-10", "proxy-11"}
	if v.Recovery.State != "applied" || v.Recovery.Reason != "no-healthy-member" || !reflect.DeepEqual(v.Recovery.Pool, want) || v.Recovery.Probed != 12 || v.Recovery.Verified != 6 || v.InspectionRequired {
		t.Fatalf("recovery outcome = %+v inspection=%v", v.Recovery, v.InspectionRequired)
	}
	if calls, _ := os.ReadFile(marker); string(calls) != "x" {
		t.Fatal("recovery did not run exactly one native Apply", string(calls))
	}
	if v.ProvisionalAt.IsZero() || !reflect.DeepEqual(v.ActivePool, want) {
		t.Fatal("provisional pool not reported", v.ProvisionalAt, v.ActivePool)
	}
	if len(m.calls) != 0 {
		t.Fatal("recovery ran a speed transfer", m.calls)
	}
	q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes)
	if err != nil || q.ReviewsUsed != 0 || !q.LastStartedAt.IsZero() || q.InspectionRequired || q.ProvisionalAt.IsZero() {
		t.Fatal("recovery spent review quota or left a fence", q, err)
	}
	w, _ := s.Editor.Workspace(context.Background())
	if !observatoryMatchesPool(w.Documents["07_observatory.json"].Text, want, false) || strings.Contains(w.Documents["05_routing.json"].Text, "proxy-00") {
		t.Fatal("applied 05/07 do not hold exactly the provisional pool")
	}
	// The provisional pool is not reapplied within the hour, even if it fails.
	r.mutate = down
	for _, tag := range want {
		m.rttDown[tag] = true
	}
	probes := len(m.rttCalls)
	s.recoverPool(context.Background())
	if v := s.Read(); v.Recovery.State != "deferred" || v.Recovery.Reason != "recovery-apply-gap" || len(m.rttCalls) != probes {
		t.Fatal("a failed provisional pool was reapplied inside the hour", v.Recovery)
	}
}

func TestRecoveryApplyGapPreventsRestartLoop(t *testing.T) {
	s, m, _ := recoveryFixture(t)
	release, err := acquireQuotaLock(s.QuotaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeQuotaLocked(s.QuotaPath, quotaReceipt{Version: 1, ProvisionalAt: time.Now().UTC().Add(-10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	release()
	s.recoverPool(context.Background())
	if v := s.Read(); v.Recovery.State != "deferred" || v.Recovery.Reason != "recovery-apply-gap" || len(m.rttCalls) != 0 {
		t.Fatal("recovery Apply inside the hour", v.Recovery, m.rttCalls)
	}
}

func TestRecoveryLeavesConfigurationWhenNothingIsProven(t *testing.T) {
	for name, setup := range map[string]func(*Service, *sweepMeasurement, *sweepReader){
		"no candidate answers": func(_ *Service, m *sweepMeasurement, _ *sweepReader) {
			for i := 0; i < 14; i++ {
				m.rttDown[fmt.Sprintf("proxy-%02d", i)] = true
			}
		},
		"an incumbent answers its probe": func(_ *Service, m *sweepMeasurement, r *sweepReader) {
			// Observatory still reports every member down, but proxy-03
			// answers the targeted probe: Xray can still select it.
			r.mutate = func(snapshot *xrayapi.Snapshot) {
				for i := range snapshot.OutboundHealth {
					if snapshot.OutboundHealth[i].Tag <= "proxy-05" {
						snapshot.OutboundHealth[i].Alive = false
					}
				}
			}
			delete(m.rttDown, "proxy-03")
		},
		"manual override": func(_ *Service, _ *sweepMeasurement, r *sweepReader) {
			down := r.mutate
			r.mutate = func(snapshot *xrayapi.Snapshot) {
				down(snapshot)
				snapshot.Balancer.Override = "proxy-00"
			}
		},
		"healthy member": func(_ *Service, _ *sweepMeasurement, r *sweepReader) {
			r.mutate = nil
		},
		"probe gate closed": func(_ *Service, m *sweepMeasurement, _ *sweepReader) {
			m.rttErr = c1.ErrProbeBlocked
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, m, r := recoveryFixture(t)
			setup(s, m, r)
			s.recoverPool(context.Background())
			v := s.Read()
			want := map[string]string{
				"no candidate answers":           "vpn-unavailable/no-candidate-verified",
				"an incumbent answers its probe": "healthy/member-answers-probe",
				"manual override":                "deferred/manual-override-active",
				"healthy member":                 "healthy/",
				"probe gate closed":              "deferred/probe-cleanup-pending",
			}[name]
			if got := v.Recovery.State + "/" + v.Recovery.Reason; got != want {
				t.Fatalf("recovery = %s, want %s", got, want)
			}
			if recoveryPending(t, s) || v.InspectionRequired || !v.ProvisionalAt.IsZero() {
				t.Fatal("recovery changed configuration without proof")
			}
			if (name == "manual override" || name == "healthy member") && len(m.rttCalls) != 0 {
				t.Fatal("recovery probed while it had to stand down", m.rttCalls)
			}
		})
	}
}

func TestRecoveryRotatesCandidatesAcrossAttempts(t *testing.T) {
	s, m, _ := recoveryFixture(t)
	for i := 0; i < 14; i++ {
		m.rttDown[fmt.Sprintf("proxy-%02d", i)] = true
	}
	s.recoverPool(context.Background())
	s.recoverPool(context.Background())
	var first, second []string
	for _, call := range m.rttCalls[:2] {
		first = append(first, call...)
	}
	for _, call := range m.rttCalls[2:] {
		second = append(second, call...)
	}
	if len(first) != 12 || len(second) != 12 || reflect.DeepEqual(first, second) {
		t.Fatal("recovery attempts did not rotate", first, second)
	}
	if !strings.Contains(strings.Join(second, ","), "proxy-12") {
		t.Fatal("the second attempt skipped the deferred candidates", second)
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err != nil || q.FairCursor != 0 {
		t.Fatal("recovery advanced the durable review cursor", q, err)
	}
}

func TestRecoveryStandsDownDuringReviewOrInspection(t *testing.T) {
	s, m, _ := recoveryFixture(t)
	s.status.InspectionRequired = true
	s.recoverPool(context.Background())
	s.status.InspectionRequired = false
	s.autoApplying = true
	s.recoverPool(context.Background())
	if len(m.rttCalls) != 0 || s.Read().Recovery.State != "" {
		t.Fatal("recovery ran during inspection or a review Apply", m.rttCalls)
	}
}

func TestRecoveryUnknownApplyOutcomeIsFenced(t *testing.T) {
	s, _, _ := recoveryFixture(t)
	// The fixture's native job runner is missing: the save succeeds and the
	// Apply admission is unknown.
	s.recoverPool(context.Background())
	v := s.Read()
	if v.Recovery.State != "inspection-required" || !v.InspectionRequired || !v.ProvisionalAt.IsZero() {
		t.Fatal("unknown recovery Apply not fenced", v.Recovery, v.InspectionRequired)
	}
	if q, err := quotaState(s.QuotaPath, time.Now().UTC(), testReviewBytes); err != nil || !q.InspectionRequired {
		t.Fatal("durable recovery fence missing", q, err)
	}
}

func TestRecoveryReplacesAnAllOrphanedPool(t *testing.T) {
	s, m, _ := recoveryFixture(t)
	m.rttDown = map[string]bool{}
	marker := setupNativeApply(t, s, s.Editor.Dir)
	var gone []string
	var costs []c1.NativeQualityCost
	for i := 0; i < 3; i++ {
		tag := fmt.Sprintf("proxy-gone-%d", i)
		gone = append(gone, tag)
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(tag) + "$", Value: 1})
	}
	routing, _ := json.Marshal(map[string]any{"routing": map[string]any{"rules": []any{}, "balancers": []any{map[string]any{"tag": "bal-proxy", "selector": gone, "strategy": map[string]any{"type": "leastLoad", "settings": map[string]any{"maxRTT": "10s", "costs": costs}}}}}})
	if err := os.WriteFile(filepath.Join(s.Editor.Dir, "05_routing.json"), routing, 0600); err != nil {
		t.Fatal(err)
	}
	s.recoverPool(context.Background())
	v := s.Read()
	if v.Recovery.State != "applied" || len(v.Recovery.Pool) != 6 || v.ProvisionalAt.IsZero() {
		t.Fatal("all-orphaned pool not recovered", v.Recovery)
	}
	if calls, _ := os.ReadFile(marker); string(calls) != "x" {
		t.Fatal("expected one native Apply", string(calls))
	}
}

func TestRecoveryRefusalBeforeHealthIsNotReportedAsAnOutage(t *testing.T) {
	s, m, _ := recoveryFixture(t)
	p := filepath.Join(s.Editor.Dir, "05_routing.json")
	b, _ := os.ReadFile(p)
	if _, err := s.Editor.SaveTexts(context.Background(), s.status.Digest, map[string]string{"05_routing.json": string(b) + "\n"}); err != nil {
		w, _ := s.Editor.Workspace(context.Background())
		if _, err := s.Editor.SaveTexts(context.Background(), w.Digest, map[string]string{"05_routing.json": string(b) + "\n"}); err != nil {
			t.Fatal(err)
		}
	}
	s.recoverPool(context.Background())
	if v := s.Read(); v.Recovery.State != "unchecked" || v.Recovery.Reason != "configuration-pending-or-unavailable" || len(m.rttCalls) != 0 {
		t.Fatal("a pending draft was reported as a deferred recovery", v.Recovery)
	}
}
