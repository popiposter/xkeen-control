package nativequality

import (
	"context"
	"errors"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

const (
	sweepMaxEligible = 18
	sweepMaxBatches  = 6
	sweepWall        = 30 * time.Minute
	sweepBatchPause  = 3 * time.Minute
)

// batchSizes covers each frozen candidate once, without a one-node final
// generation (the shared measurement runner requires at least two candidates).
func batchSizes(count int) ([]int, error) {
	if count < 2 || count > sweepMaxEligible {
		return nil, ErrUnavailable
	}
	var sizes []int
	for count > 0 {
		size := min(3, count)
		if count == 4 {
			size = 2
		}
		sizes = append(sizes, size)
		count -= size
	}
	if len(sizes) > sweepMaxBatches {
		return nil, ErrUnavailable
	}
	return sizes, nil
}

func (s *Service) startSweep(parent context.Context, trigger string) error {
	if s.Editor == nil || s.Lease == nil || s.Reader == nil || s.Nodes == nil || s.Measurement == nil || s.Resources == nil || !s.profile().Constrained || !s.profile().Automatic || s.AutomaticDisabled {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.cancel != nil || s.autoApplying || s.status.InspectionRequired {
		return ErrUnavailable
	}
	if err := s.Resources.CheckConflict(); err != nil {
		s.status.ReviewReason = "native-speed-conflict-or-unavailable"
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete {
		s.status.ReviewReason = "configuration-pending-or-unavailable"
		return ErrUnavailable
	}
	routing := w.Documents["05_routing.json"].Text
	nodes := s.Nodes(ctx)
	pool, index, err := measurementPool(routing, nodes, w.Targets)
	if err != nil {
		return err
	}
	activePool, _, err := routingPool(routing, nodes, w.Targets)
	if err != nil {
		return err
	}
	criteria, err := readCriteria(routing, w.Documents["07_observatory.json"].Text, index, w.Targets)
	if err != nil {
		return err
	}
	if probeRouteShadowed(routing) {
		s.status.ReviewReason = "probe-route-shadowed"
		return ErrUnavailable
	}
	now := time.Now().UTC()
	snapshot := s.Reader.Snapshot(ctx)
	if !snapshot.APIReachable || !snapshot.RoutingReachable || snapshot.Balancer.Override != "" {
		s.status.ReviewReason = "native-override-or-unavailable"
		return ErrUnavailable
	}
	quotaPath := s.QuotaPath
	if quotaPath == "" {
		quotaPath = defaultQuotaPath
	}
	quotaUnlock, err := acquireQuotaLock(quotaPath)
	if err != nil {
		s.status.ReviewReason = "quota-busy-or-unavailable"
		return errQuota
	}
	previous, err := readQuotaLocked(quotaPath, now)
	if err != nil || !quotaAdmits(previous, now) {
		quotaUnlock()
		s.status.ReviewReason = "quota-unavailable-or-exhausted"
		return errQuota
	}
	plan, err := planSweep(pool, activePool, snapshot.Balancer.NativeSelected, previous.FairCursor, previous.EligibleSetHash)
	if err != nil {
		quotaUnlock()
		s.status.ReviewReason = "eligible-unavailable"
		return err
	}
	// The speed phase is capped at sweepWall; the RTT pre-phase and the
	// separate native Apply have their own bounded margins.
	job, cancelJob := context.WithTimeout(parent, sweepWall+time.Duration(len(plan.Candidates))*c1.RTTProbeTimeout+6*time.Minute)
	done := make(chan struct{})
	s.cancel, s.done = cancelJob, done
	s.lastStartedAt = now
	s.pool = append([]string(nil), pool...)
	s.sweepPlan = plan
	s.rttAlive = nil
	s.result = c1.AdaptiveResult{}
	s.status = Status{State: "running", Digest: w.Digest, Generation: s.status.Generation + 1, PoolCount: len(pool), ActivePoolCount: len(activePool), ActivePool: append([]string(nil), activePool...), ActivePoolState: "frozen-at-review", EligibleCount: plan.TotalEligible, TotalEligible: plan.TotalEligible, SelectedForSpeed: len(plan.Candidates), DeferredForFutureReview: plan.Deferred, SubsetState: "all-eligible", FairCursor: plan.NextCursor, FairCursorState: plan.CursorState, LatencyLimitMS: criteria.maxRTT, LatencySource: criteria.latencySource, ReviewTrigger: trigger, ReviewPhase: "rtt", Progress: c1.AdaptivePerformanceStatus{State: "running"}, AppliedState: "not-attempted"}
	observeNativeSelection(&s.status, snapshot, now)
	if plan.Deferred > 0 {
		s.status.SubsetState = "subset-selected"
	}
	if plan.FirstInitialization {
		s.status.PoolDecision = "first-pool-initialization"
	}
	frozen := c1.AdaptiveGeneration{NativeQuality: true, Generation: s.status.Generation, StartedAt: now, CurrentTarget: snapshot.Balancer.NativeSelected}
	go s.runSweep(job, cancelJob, done, frozen, plan, criteria.maxRTT, w.Digest, quotaPath, quotaUnlock)
	return nil
}

// rttPrePhase probes every frozen candidate once through the same fixed
// endpoint and timeout. It returns the candidates admitted to the speed phase,
// in plan order, and every probed tag's health. A tag slower than the native
// maxRTT is alive but not admitted.
func (s *Service) rttPrePhase(ctx context.Context, tags []string, maxRTT int64) ([]c1.AdaptiveCandidateInput, map[string]bool, string) {
	release, err := s.Lease.TryAcquire()
	if err != nil {
		return nil, nil, "panel-busy"
	}
	defer release()
	samples, err := s.Measurement.MeasureRTT(ctx, tags)
	if err != nil {
		if errors.Is(err, c1.ErrProbeCleanup) || errors.Is(err, c1.ErrProbeBlocked) {
			return nil, nil, "probe-cleanup-pending"
		}
		return nil, nil, "rtt-probe-unavailable"
	}
	if len(samples) != len(tags) {
		return nil, nil, "rtt-probe-incomplete"
	}
	alive := make(map[string]bool, len(samples))
	var admitted []c1.AdaptiveCandidateInput
	for i, sample := range samples {
		if sample.Tag != tags[i] {
			return nil, nil, "rtt-probe-incomplete"
		}
		alive[sample.Tag] = sample.Valid
		if sample.Valid && sample.RTTMS > 0 && sample.RTTMS <= maxRTT {
			admitted = append(admitted, c1.AdaptiveCandidateInput{Tag: sample.Tag, RTTMS: sample.RTTMS, LatestAt: sample.SampledAt})
		}
	}
	return admitted, alive, ""
}

func (s *Service) runSweep(ctx context.Context, cancel context.CancelFunc, done chan struct{}, frozen c1.AdaptiveGeneration, plan sweepPlan, maxRTT int64, digest, quotaPath string, quotaUnlock func()) {
	defer close(done)
	defer cancel()
	if quotaUnlock != nil {
		defer quotaUnlock()
	}
	ordered, alive, reason := s.rttPrePhase(ctx, plan.Candidates, maxRTT)
	s.mu.Lock()
	s.rttAlive = alive
	s.status.ReviewPhase = "speed"
	s.status.RTTValidCount = len(ordered)
	s.mu.Unlock()
	if reason == "" && len(ordered) < 2 {
		reason = "rtt-candidates-insufficient"
	}
	reserved := false
	if reason == "" && quotaUnlock != nil {
		// Reserve only now: a review that cannot reach the speed phase must not
		// spend the day's quota or advance the fair cursor.
		now := time.Now().UTC()
		if _, err := reserveSweepPlannedLocked(quotaPath, now, &plan); err != nil {
			reason = "quota-unavailable-or-exhausted"
		} else {
			reserved = true
			if receipt, err := readQuotaLocked(quotaPath, now); err == nil {
				quota := viewQuota(receipt, now)
				s.mu.Lock()
				s.status.QuotaState = "available"
				s.status.QuotaUsedBytes, s.status.QuotaRemainingBytes = quota.UsedBytes, quota.RemainingBytes
				s.status.QuotaReviewsUsed, s.status.QuotaNextResetAt = quota.ReviewsUsed, quota.NextResetAt
				s.mu.Unlock()
			}
		}
	}
	if reason != "" {
		s.mu.Lock()
		s.result = c1.AdaptiveResult{}
		s.status.State = "deferred"
		s.status.ReviewReason = reason
		s.status.Progress = c1.AdaptivePerformanceStatus{State: "deferred"}
		if reason == "probe-cleanup-pending" {
			s.status.InspectionRequired = true
		}
		s.cancel = nil
		s.mu.Unlock()
		return
	}
	sizes, _ := batchSizes(len(ordered))
	result := c1.AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: len(ordered), Generation: frozen.Generation, StartedAt: time.Now().UTC(), CurrentTarget: frozen.CurrentTarget, State: "running"}
	position := 0
	pause := sweepBatchPause
	if s.batchPause > 0 {
		pause = s.batchPause
	}
	for batchIndex, size := range sizes {
		if batchIndex > 0 {
			timer := time.NewTimer(pause)
			select {
			case <-ctx.Done():
				timer.Stop()
				reason = "review-cancelled"
				break
			case <-timer.C:
			}
			if reason != "" {
				break
			}
		}
		if ctx.Err() != nil {
			reason = "review-timeout-or-cancelled"
			break
		}
		wctx, stop := context.WithTimeout(ctx, 10*time.Second)
		w, err := s.Editor.Workspace(wctx)
		if err != nil || w.Pending != nil || !w.TargetsComplete || w.Digest != digest {
			reason = "configuration-changed-or-pending"
			stop()
			break
		}
		if err := s.Resources.CheckConflict(); err != nil {
			reason = "native-speed-conflict-or-unavailable"
			stop()
			break
		}
		snapshot := s.Reader.Snapshot(wctx)
		s.mu.Lock()
		observeNativeSelection(&s.status, snapshot, time.Now().UTC())
		s.mu.Unlock()
		if !snapshot.APIReachable || !snapshot.RoutingReachable || !snapshot.ObservatoryReachable || snapshot.Balancer.Override != "" {
			reason = "native-override-or-unavailable"
			stop()
			break
		}
		stop()
		release, err := s.Lease.TryAcquire()
		if err != nil {
			reason = "panel-busy"
			break
		}
		batch := c1.AdaptiveGeneration{NativeQuality: true, Generation: frozen.Generation, StartedAt: time.Now().UTC(), CurrentTarget: frozen.CurrentTarget, Candidates: ordered[position : position+size]}
		batchCtx, batchCancel := context.WithTimeout(ctx, 90*time.Second)
		measured, measureErr := s.Measurement.MeasureNativeQuality(batchCtx, batch, func(progress c1.AdaptivePerformanceStatus) {
			s.mu.Lock()
			s.status.Progress = progress
			s.mu.Unlock()
		})
		batchCancel()
		release()
		result.AggregateBytes += measured.AggregateBytes
		if measured.AggregateBytes < 0 || result.AggregateBytes > maxSweepBytes {
			reason = "review-budget-exceeded"
			break
		}
		result.Candidates = append(result.Candidates, measured.Candidates...)
		position += len(measured.Candidates)
		s.mu.Lock()
		s.status.AttemptedCount = position
		s.status.ValidCount += measured.ValidCount
		s.status.BatchCount++
		s.status.AggregateBytes = result.AggregateBytes
		s.mu.Unlock()
		matched := len(measured.Candidates) == size
		if matched {
			for i, candidate := range measured.Candidates {
				if candidate.Tag != batch.Candidates[i].Tag {
					matched = false
					break
				}
			}
		}
		if measureErr != nil || measured.State != "completed" || !matched || measured.AggregateBytes < 0 {
			reason = "batch-incomplete-or-unknown"
			if measured.State == "cleanup-pending" || measureErr != nil {
				s.mu.Lock()
				s.status.InspectionRequired = true
				s.mu.Unlock()
			}
			break
		}
	}
	result.CompletedAt = time.Now().UTC()
	result.ShortlistCount = len(result.Candidates)
	result.ValidCount = 0
	for _, candidate := range result.Candidates {
		if candidate.Valid {
			result.ValidCount++
		}
	}
	if reason == "" && len(result.Candidates) == len(ordered) && result.CompletedAt.Sub(result.StartedAt) <= sweepWall {
		result.State = "completed"
		if _, err := c1.NativeQualityCosts(result, result.CompletedAt, s.pool); err != nil {
			reason = "review-insufficient-or-expired"
		}
	} else if reason == "" {
		reason = "review-incomplete-or-expired"
	}
	if reason != "" {
		result.State = "failed"
	}
	s.mu.Lock()
	s.result = result
	s.status.State = result.State
	s.status.ReviewReason = reason
	s.status.Progress = c1.NativeSweepStatus(result)
	s.status.AttemptedCount = len(result.Candidates)
	s.status.ValidCount = result.ValidCount
	s.status.AggregateBytes = result.AggregateBytes
	if result.State == "completed" && s.status.DeferredForFutureReview > 0 {
		s.status.SubsetState = "subset-complete"
	}
	if result.State == "completed" && ctx.Err() == nil {
		s.autoApplying = true
		s.status.State = "applying"
	}
	s.mu.Unlock()
	if result.State == "completed" && ctx.Err() == nil {
		s.applySweep(ctx, result, digest, append([]string(nil), s.pool...))
	}
	s.mu.Lock()
	s.autoApplying = false
	s.cancel = nil
	if result.State == "completed" {
		if s.status.AppliedState == "applied" || s.status.AppliedState == "no-op" {
			s.status.State = "completed"
		} else {
			s.status.State = "failed"
		}
	}
	inspection := s.status.InspectionRequired
	s.mu.Unlock()
	if reserved {
		if err := settleSweepLocked(quotaPath, time.Now().UTC(), !inspection); err != nil {
			s.mu.Lock()
			s.status.InspectionRequired = true
			s.status.ReviewReason = "inspection-receipt-unavailable"
			s.mu.Unlock()
		}
	}
}
