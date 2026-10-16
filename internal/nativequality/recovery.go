package nativequality

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/xkeen"
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
	// selectedProbes is how many targeted probes in a row Xray's selected
	// member must fail before it counts as carrying no traffic; the gap
	// between them keeps one dropped request from becoming an Apply.
	selectedProbes   = 3
	selectedProbeGap = 5 * time.Second
)

// RecoveryStatus is the last REQ-009 no-healthy-member recovery attempt. It is
// availability recovery, never a speed ranking.
type RecoveryStatus struct {
	State     string    `json:"state,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	Probed    int       `json:"probed"`
	Verified  int       `json:"verified"`
	Pool      []string  `json:"pool,omitempty"`
	// Unusable is Xray's selected member that native Observatory reported
	// healthy but that failed every targeted probe.
	Unusable string `json:"unusable,omitempty"`
	// Provisional marks an applied pool that is not speed-ranked: a new
	// provisional pool, or an older one that only lost its unusable member.
	Provisional bool `json:"provisional,omitempty"`
}

// recoveryGap reports whether a recovery Apply of either kind ran within the
// last hour; at most one runs per hour.
func recoveryGap(q quotaReceipt, now time.Time) bool {
	return !q.RecoveryAppliedAt.IsZero() && now.Sub(q.RecoveryAppliedAt) < recoveryApplyGap || !q.ProvisionalAt.IsZero() && now.Sub(q.ProvisionalAt) < recoveryApplyGap
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
	if s.Editor == nil || s.Lease == nil || s.Reader == nil || s.Nodes == nil || s.Measurement == nil || s.Jobs == nil || s.Resources == nil || !s.profile().Automatic || s.AutomaticDisabled || !s.settings().Recovery {
		return
	}
	s.mu.Lock()
	// The nightly window restarts Xray on purpose; a pool that is briefly
	// unreachable then is not an outage.
	if s.closed || s.cancel != nil || s.autoApplying || s.recovering || s.status.InspectionRequired || s.maintenance.Phase != "" {
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
	if outcome.State == "applied" && s.Recovered != nil {
		s.Recovered(parent, outcome)
	}
}

func (s *Service) recoveryAttempt(parent context.Context) RecoveryStatus {
	// Until the pool health is known, a refusal says nothing about an outage:
	// it is "unchecked", not a deferred recovery.
	deferred := func(reason string) RecoveryStatus { return RecoveryStatus{State: "unchecked", Reason: reason} }
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
	unusable := ""
	if !noHealthyMember(snapshot, resolved) {
		// Observatory probes one fixed URL; a member whose server passes only
		// that destination stays alive and selected while it carries nothing
		// else. Check the selected member with the panel's own probe.
		cancel()
		outcome, next := s.checkSelected(parent, path, q, now, w, routing, observatory, index, resolved, snapshot)
		if !next {
			return outcome
		}
		unusable = outcome.Unusable
	}
	deferred = func(reason string) RecoveryStatus {
		return RecoveryStatus{State: "deferred", Reason: reason, Unusable: unusable}
	}
	// The operator's native override owns selection; recovery never clears or
	// overwrites it.
	if snapshot.Balancer.Override != "" {
		return deferred("manual-override-active")
	}
	if recoveryGap(q, now) {
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
	s.recovery.State, s.recovery.Unusable = "probing", unusable
	s.mu.Unlock()
	probeCtx, stopProbe := context.WithTimeout(parent, time.Duration(len(plan.Candidates))*c1.RTTProbeTimeout+time.Minute)
	admitted, alive, reason := s.rttPrePhase(probeCtx, plan.Candidates, w.Digest, false)
	stopProbe()
	out := RecoveryStatus{Probed: len(plan.Candidates), Verified: len(admitted), Unusable: unusable}
	if reason != "" {
		out.State, out.Reason = "deferred", reason
		return out
	}
	answered := make(map[string]int64, len(admitted))
	for _, candidate := range admitted {
		answered[candidate.Tag] = candidate.RTTMS
	}
	for _, tag := range resolved {
		// Stop only for a member that answered within the native maxRTT: a
		// slower answer is no proof that Xray, which already reports every
		// member down, will select it again. Missing an outage costs more than
		// one unneeded recovery Apply.
		if rtt, ok := answered[tag]; ok && alive[tag] && rtt <= criteria.maxRTT {
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
	for _, candidate := range admitted[:min(recoveryPoolSize, s.poolSize(), len(admitted))] {
		selected = append(selected, candidate.Tag)
		// Equal anchored costs: the pool is not speed-ranked, and the anchor
		// keeps each member quality-owned for orphan detection.
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: "^" + regexp.QuoteMeta(candidate.Tag) + "$", Value: 1})
	}
	out.Pool, out.Provisional = selected, true
	return s.recoveryApply(parent, path, q, w, index, eligible, selected, costs, routing, observatory, true, out)
}

// recoveryApply applies one recovery pool through the review Apply path: the
// durable inspection intent precedes the save. A provisional pool records
// ProvisionalAt for the hourly gap; a pool that only drops an unusable member
// keeps the review costs and is not provisional.
func (s *Service) recoveryApply(parent context.Context, path string, q quotaReceipt, w xkeen.EditorWorkspace, index int, eligible, selected []string, costs []c1.NativeQualityCost, routing, observatory string, provisional bool, out RecoveryStatus) RecoveryStatus {
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
	check := s.Reader.Snapshot(applyCtx)
	if !check.APIReachable || !check.RoutingReachable || !check.ObservatoryReachable || check.Balancer.Override != "" || !s.Reader.ProbeReachable(applyCtx) {
		return fail("deferred", "native-override-or-unavailable")
	}
	// A removal stands down when Xray left the unusable member by itself.
	if !provisional && check.Balancer.NativeSelected != out.Unusable {
		out.State, out.Reason, out.Pool, out.Provisional = "healthy", "selection-changed", nil, false
		return out
	}
	// The durable intent precedes the save, so a crash before the proof is
	// fenced for inspection exactly like a review Apply.
	q.InspectionRequired = true
	if writeQuotaLocked(path, q) != nil {
		return fail("failed", "quota-unavailable")
	}
	s.mu.Lock()
	s.recovery.State, s.recovery.Unusable = "applying", out.Unusable
	s.mu.Unlock()
	result := s.applyPool(parent, w.Digest, index, eligible, selected, costs, false, routing, observatory, nil)
	if !result.inspection {
		q.InspectionRequired = false
	}
	if result.state == "applied" {
		q.RecoveryAppliedAt = time.Now().UTC()
		if provisional {
			q.ProvisionalAt = q.RecoveryAppliedAt
		}
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
	if provisional {
		return fail("applied", "no-healthy-member")
	}
	return fail("applied", "selected-member-unusable")
}

func (s *Service) selectedProbeGap() time.Duration {
	if s.readbackWait > 0 {
		return s.readbackWait / 20
	}
	return selectedProbeGap
}

// checkSelected probes Xray's selected member while a reachable Observatory
// reports a healthy pool. A member that answers, a changed selection or a
// probe that never reached the outbound ends the attempt as healthy or
// unchecked. After selectedProbes routed failures the member is removed: the
// other members that answer keep their costs and are applied as the pool.
// next asks the caller to continue with the provisional recovery when no
// other member answers.
func (s *Service) checkSelected(parent context.Context, path string, q quotaReceipt, now time.Time, w xkeen.EditorWorkspace, routing, observatory string, index int, resolved []string, snapshot xrayapi.Snapshot) (RecoveryStatus, bool) {
	unchecked := func(reason string) (RecoveryStatus, bool) {
		return RecoveryStatus{State: "unchecked", Reason: reason}, false
	}
	selected := snapshot.Balancer.NativeSelected
	// The operator's native override owns selection.
	if snapshot.Balancer.Override != "" || !slices.Contains(resolved, selected) {
		return RecoveryStatus{State: "healthy"}, false
	}
	// Removal needs Observatory's own verdict that the pool is healthy.
	if !snapshot.ObservatoryReachable {
		return unchecked("observatory-unavailable")
	}
	if probeRouteShadowed(routing) {
		return unchecked("probe-route-shadowed")
	}
	// sameSelection re-reads Xray: a moved selection or an override ends the
	// attempt without an Apply.
	sameSelection := func() (RecoveryStatus, bool, bool) {
		readCtx, stopRead := context.WithTimeout(parent, 10*time.Second)
		check := s.Reader.Snapshot(readCtx)
		stopRead()
		if !check.APIReachable || !check.RoutingReachable {
			out, next := unchecked("native-unavailable")
			return out, next, false
		}
		if check.Balancer.Override != "" || check.Balancer.NativeSelected != selected {
			return RecoveryStatus{State: "healthy", Reason: "selection-changed"}, false, false
		}
		return RecoveryStatus{}, false, true
	}
	for attempt := 1; ; attempt++ {
		probeCtx, stop := context.WithTimeout(parent, c1.RTTProbeTimeout+time.Minute)
		_, alive, routed, reason := s.rttProbe(probeCtx, []string{selected}, w.Digest, false)
		stop()
		if reason != "" {
			return unchecked(reason)
		}
		if alive[selected] {
			return RecoveryStatus{State: "healthy"}, false
		}
		if !routed[selected] {
			return unchecked("probe-route-unavailable")
		}
		if attempt == selectedProbes {
			break
		}
		if !sleepCtx(parent, s.selectedProbeGap()) {
			return unchecked("review-timeout-or-cancelled")
		}
		if out, next, same := sameSelection(); !same {
			return out, next
		}
	}
	if out, next, same := sameSelection(); !same {
		return out, next
	}
	out := RecoveryStatus{Unusable: selected, Probed: 1, Provisional: !q.ProvisionalAt.IsZero()}
	deferred := func(reason string) (RecoveryStatus, bool) {
		out.State, out.Reason = "deferred", reason
		return out, false
	}
	if recoveryGap(q, now) {
		return deferred("recovery-apply-gap")
	}
	others := make([]string, 0, len(resolved))
	for _, tag := range resolved {
		if tag != selected {
			others = append(others, tag)
		}
	}
	if len(others) == 0 {
		return out, true
	}
	s.mu.Lock()
	s.recovery.State, s.recovery.Unusable = "probing", selected
	s.mu.Unlock()
	probeCtx, stopProbe := context.WithTimeout(parent, time.Duration(len(others))*c1.RTTProbeTimeout+time.Minute)
	_, alive, routed, reason := s.rttProbe(probeCtx, others, w.Digest, false)
	stopProbe()
	if reason != "" {
		return deferred(reason)
	}
	out.Probed += len(others)
	var keep []string
	for _, tag := range others {
		if !routed[tag] {
			// Which members answer is unknown; keep the configuration.
			return deferred("probe-route-unavailable")
		}
		if alive[tag] {
			keep = append(keep, tag)
		}
	}
	out.Verified = len(keep)
	if len(keep) == 0 {
		return out, true
	}
	if res, next, same := sameSelection(); !same {
		return res, next
	}
	eligible, _, err := measurementPool(routing, s.Nodes(parent), w.Targets)
	if err != nil {
		return deferred("eligible-unavailable")
	}
	out.Pool = keep
	return s.recoveryApply(parent, path, q, w, index, eligible, keep, memberCosts(routing, index, keep), routing, observatory, false, out), false
}

// memberCosts returns each member's anchored native cost from the balancer,
// so a pool without the unusable member keeps the review ranking. A member
// without a cost (or a leastPing balancer) gets the neutral cost 1.
func memberCosts(text string, index int, members []string) []c1.NativeQualityCost {
	var doc struct {
		Routing struct {
			Balancers []struct {
				Strategy struct {
					Settings struct {
						Costs []c1.NativeQualityCost `json:"costs"`
					} `json:"settings"`
				} `json:"strategy"`
			} `json:"balancers"`
		} `json:"routing"`
	}
	byMatch := map[string]float64{}
	if configjson.Decode([]byte(text), &doc) == nil && index >= 0 && index < len(doc.Routing.Balancers) {
		for _, cost := range doc.Routing.Balancers[index].Strategy.Settings.Costs {
			if cost.Regexp {
				byMatch[cost.Match] = cost.Value
			}
		}
	}
	costs := make([]c1.NativeQualityCost, 0, len(members))
	for _, tag := range members {
		match := "^" + regexp.QuoteMeta(tag) + "$"
		value, ok := byMatch[match]
		if !ok || value < 1 {
			value = 1
		}
		costs = append(costs, c1.NativeQualityCost{Regexp: true, Match: match, Value: value})
	}
	return costs
}
