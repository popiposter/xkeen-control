package nativequality

import (
	"context"
	"regexp"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

const (
	// recoveryInterval is the REQ-009 attempt cadence: at most one bounded
	// probe attempt every ten minutes.
	recoveryInterval = 10 * time.Minute
	// recoveryApplyGap keeps a provisional pool that also fails from turning
	// into a restart loop: at most one recovery Apply per hour.
	recoveryApplyGap = time.Hour
	// recoveryCandidates bounds one attempt (12 x 10 s probes, two minutes).
	recoveryCandidates = 12
	// recoveryPoolSize is the largest provisional pool.
	recoveryPoolSize = 6
)

// RecoveryStatus is the last REQ-009 no-healthy-member recovery attempt. It is
// availability recovery, never a speed ranking.
type RecoveryStatus struct {
	State     string    `json:"state,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	Probed    int       `json:"probed"`
	Verified  int       `json:"verified"`
	Pool      []string  `json:"pool,omitempty"`
}

// noHealthyMember reports whether no configured pool member is healthy. Every
// resolved member must have a native Observatory record that says it is down;
// a member without a record is unknown, never an outage. A pool whose members
// all vanished from the registry (orphans only) has no healthy member.
func noHealthyMember(snapshot xrayapi.Snapshot, resolved []string) bool {
	if len(resolved) == 0 {
		return true
	}
	if !snapshot.ObservatoryReachable {
		return false
	}
	seen := make(map[string]bool, len(snapshot.OutboundHealth))
	for _, h := range snapshot.OutboundHealth {
		if h.Alive {
			seen[h.Tag] = true
		} else if _, ok := seen[h.Tag]; !ok {
			seen[h.Tag] = false
		}
	}
	for _, tag := range resolved {
		if alive, ok := seen[tag]; !ok || alive {
			return false
		}
	}
	return true
}

// recoverPool runs one REQ-009 attempt when the scheduler ticks. It shares the
// lifecycle exclusion of reviews: it never starts while a review, an Apply or
// an inspection fence is active, and holds the quota lock for its duration so
// a cross-process review cannot interleave.
func (s *Service) recoverPool(parent context.Context) {
	if s.Editor == nil || s.Lease == nil || s.Reader == nil || s.Nodes == nil || s.Measurement == nil || s.Jobs == nil || s.Resources == nil || !s.profile().Automatic || s.AutomaticDisabled {
		return
	}
	s.mu.Lock()
	if s.closed || s.cancel != nil || s.autoApplying || s.recovering || s.status.InspectionRequired {
		s.mu.Unlock()
		return
	}
	s.recovering = true
	s.recovery.State = "checking"
	s.mu.Unlock()
	outcome := s.recoveryAttempt(parent)
	outcome.CheckedAt = time.Now().UTC()
	s.mu.Lock()
	s.recovering = false
	s.recovery = outcome
	s.mu.Unlock()
}

func (s *Service) recoveryAttempt(parent context.Context) RecoveryStatus {
	deferred := func(reason string) RecoveryStatus { return RecoveryStatus{State: "deferred", Reason: reason} }
	path := s.QuotaPath
	if path == "" {
		path = defaultQuotaPath
	}
	unlock, err := acquireQuotaLock(path)
	if err != nil {
		return deferred("quota-busy-or-unavailable")
	}
	defer unlock()
	now := time.Now().UTC()
	q, err := readQuotaLocked(path, now)
	if err != nil {
		return deferred("quota-unavailable")
	}
	if q.InspectionRequired {
		return deferred("inspection-required")
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete {
		return deferred("configuration-pending-or-unavailable")
	}
	routing := w.Documents["05_routing.json"].Text
	observatory := w.Documents["07_observatory.json"].Text
	nodes := s.Nodes(ctx)
	resolved, orphans, index, err := resolveActivePool(routing, nodes, w.Targets, true)
	if err != nil {
		return deferred("selector-unavailable")
	}
	snapshot := s.Reader.Snapshot(ctx)
	if !snapshot.APIReachable || !snapshot.RoutingReachable {
		return deferred("native-unavailable")
	}
	if !noHealthyMember(snapshot, resolved) {
		return RecoveryStatus{State: "healthy"}
	}
	// The operator's native override owns selection; recovery never clears or
	// overwrites it.
	if snapshot.Balancer.Override != "" {
		return deferred("manual-override-active")
	}
	if !q.ProvisionalAt.IsZero() && now.Sub(q.ProvisionalAt) < recoveryApplyGap {
		return deferred("recovery-apply-gap")
	}
	eligible, _, err := measurementPool(routing, nodes, w.Targets)
	if err != nil {
		return deferred("eligible-unavailable")
	}
	criteria, err := readCriteria(routing, observatory, index)
	if err != nil {
		return deferred("criteria-unavailable")
	}
	if probeRouteShadowed(routing) {
		return deferred("probe-route-shadowed")
	}
	cursor, hash := s.recoveryCursor, s.recoveryHash
	if hash == "" {
		cursor, hash = q.FairCursor, q.EligibleSetHash
	}
	active := append(append([]string(nil), resolved...), orphans...)
	plan, err := planSweep(eligible, active, snapshot.Balancer.NativeSelected, cursor, hash, recoveryCandidates)
	if err != nil {
		return deferred("eligible-unavailable")
	}
	// Recovery rotates in memory only; the durable fair cursor belongs to
	// speed reviews.
	s.recoveryCursor, s.recoveryHash = plan.NextCursor, plan.EligibleSetHash
	cancel()
	s.mu.Lock()
	s.recovery.State = "probing"
	s.mu.Unlock()
	probeCtx, stopProbe := context.WithTimeout(parent, time.Duration(len(plan.Candidates))*c1.RTTProbeTimeout+time.Minute)
	admitted, alive, reason := s.rttPrePhase(probeCtx, plan.Candidates, criteria.maxRTT, w.Digest)
	stopProbe()
	out := RecoveryStatus{Probed: len(plan.Candidates), Verified: len(admitted)}
	if reason != "" {
		out.State, out.Reason = "deferred", reason
		return out
	}
	for _, tag := range resolved {
		if alive[tag] {
			// The probe proves a member answers: Xray can still select it.
			out.State, out.Reason = "healthy", "member-answers-probe"
			return out
		}
	}
	if len(admitted) == 0 {
		// Leave the configuration intact: proxied destinations keep the
		// existing block fallback, never a silent DIRECT path.
		out.State, out.Reason = "vpn-unavailable", "no-candidate-verified"
		return out
	}
	sort.SliceStable(admitted, func(i, j int) bool { return admitted[i].RTTMS < admitted[j].RTTMS })
	selected := make([]string, 0, recoveryPoolSize)
	costs := make([]c1.NativeQualityCost, 0, recoveryPoolSize)
	for _, candidate := range admitted[:min(recoveryPoolSize, len(admitted))] {
		selected = append(selected, candidate.Tag)
		// Equal anchored costs: the pool is not speed-ranked, and the anchor
		// keeps each member quality-owned for orphan detection.
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(candidate.Tag) + "$", Value: 1})
	}
	out.Pool = selected
	fail := func(state, reason string) RecoveryStatus {
		out.State, out.Reason = state, reason
		return out
	}
	if !exactSelectors(selected, w.Targets) {
		return fail("failed", "selector-unavailable")
	}
	applyCtx, stopApply := context.WithTimeout(parent, 15*time.Second)
	defer stopApply()
	if reason := s.applyAdmission(applyCtx); reason != "" {
		return fail("deferred", reason)
	}
	current, err := s.Editor.Workspace(applyCtx)
	if err != nil || current.Pending != nil || !current.TargetsComplete || current.Digest != w.Digest {
		return fail("deferred", "configuration-changed-or-pending")
	}
	if check := s.Reader.Snapshot(applyCtx); !check.APIReachable || !check.RoutingReachable || check.Balancer.Override != "" {
		return fail("deferred", "native-override-or-unavailable")
	}
	// The durable intent precedes the save, so a crash before the proof is
	// fenced for inspection exactly like a review Apply.
	q.InspectionRequired = true
	if writeQuotaLocked(path, q) != nil {
		return fail("failed", "quota-unavailable")
	}
	s.mu.Lock()
	s.recovery.State = "applying"
	s.mu.Unlock()
	result := s.applyPool(parent, w.Digest, index, eligible, selected, costs, routing, observatory, nil)
	if !result.inspection {
		q.InspectionRequired = false
	}
	if result.state == "applied" {
		q.ProvisionalAt = time.Now().UTC()
	}
	if writeQuotaLocked(path, q) != nil {
		result.inspection, result.reason = true, "inspection-receipt-unavailable"
	}
	if result.inspection {
		s.mu.Lock()
		s.status.InspectionRequired = true
		s.mu.Unlock()
		return fail("inspection-required", result.reason)
	}
	if result.state != "applied" {
		return fail("failed", result.reason)
	}
	return fail("applied", "no-healthy-member")
}
