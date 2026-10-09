package nativequality

import (
	"context"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

// InspectAndResolve is an explicit operator action. It only settles the
// quality intent after independent, bounded readback of the current native
// configuration, lifecycle and temporary probe state. It does not retry Apply,
// assert that the previous recommendation won, or clear an unknown native job.
func (s *Service) InspectAndResolve(parent context.Context) error {
	if s.Editor == nil || s.Jobs == nil || s.Lease == nil || s.Reader == nil || s.Control == nil || s.Nodes == nil || s.Resources == nil || !s.profile().Constrained || !s.profile().Automatic {
		return ErrUnavailable
	}
	path := s.QuotaPath
	if path == "" {
		path = defaultQuotaPath
	}
	unlock, err := acquireQuotaLock(path)
	if err != nil {
		return ErrUnavailable
	}
	defer unlock()
	s.mu.Lock()
	if s.closed || s.cancel != nil || s.autoApplying {
		s.mu.Unlock()
		return ErrUnavailable
	}
	s.autoApplying = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.autoApplying = false; s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	q, err := readQuotaLocked(path, time.Now().UTC())
	if err != nil || !q.InspectionRequired || s.Resources.CheckConflict() != nil {
		return ErrUnavailable
	}
	release, err := s.Lease.TryAcquire()
	if err != nil {
		return ErrUnavailable
	}
	defer release()
	before, err := s.Editor.Workspace(ctx)
	if err != nil || before.Pending != nil || !before.TargetsComplete || before.Digest == "" {
		return ErrUnavailable
	}
	if err := s.Jobs.InspectQualitySettlement(ctx); err != nil {
		return ErrUnavailable
	}
	if err := s.Editor.VerifySetupRuntime(ctx, before.Digest); err != nil {
		return ErrUnavailable
	}
	rules, err := s.Control.ListRules(ctx)
	if err != nil {
		return ErrUnavailable
	}
	for _, rule := range rules {
		if rule.RuleTag == c1.AdaptiveRuleTag {
			// This explicit inspected action may remove only the quality probe's
			// temporary rule. It never changes native configuration or restarts Xray.
			if err := s.Control.RemoveRule(ctx, c1.AdaptiveRuleTag); err != nil {
				return ErrUnavailable
			}
			continue
		}
		if strings.HasPrefix(rule.RuleTag, "xkeen-control-probe-") {
			return ErrUnavailable
		}
	}
	rules, err = s.Control.ListRules(ctx)
	if err != nil {
		return ErrUnavailable
	}
	for _, rule := range rules {
		if strings.HasPrefix(rule.RuleTag, "xkeen-control-probe-") {
			return ErrUnavailable
		}
	}
	pool, _, err := routingPool(before.Documents["05_routing.json"].Text, s.Nodes(ctx), before.Targets)
	if err != nil || len(pool) < 2 {
		return ErrUnavailable
	}
	snapshot := s.Reader.Snapshot(ctx)
	selected := false
	for _, tag := range pool {
		if tag == snapshot.Balancer.NativeSelected {
			selected = true
			break
		}
	}
	if !snapshot.APIReachable || !snapshot.RoutingReachable || !snapshot.ObservatoryReachable || snapshot.Balancer.Override != "" || !selected || !s.Reader.ProbeReachable(ctx) {
		return ErrUnavailable
	}
	after, err := s.Editor.Workspace(ctx)
	if err != nil || after.Pending != nil || !after.TargetsComplete || after.Digest != before.Digest {
		return ErrUnavailable
	}
	if err := s.Jobs.InspectQualitySettlement(ctx); err != nil {
		return ErrUnavailable
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	q, err = readQuotaLocked(path, time.Now().UTC())
	if err != nil || !q.InspectionRequired {
		return ErrUnavailable
	}
	q.InspectionRequired = false
	if err := writeQuotaLocked(path, q); err != nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	s.status.InspectionRequired = false
	s.status.AppliedState = "inspected"
	s.status.ReviewReason = "inspection-settled"
	s.mu.Unlock()
	return nil
}
