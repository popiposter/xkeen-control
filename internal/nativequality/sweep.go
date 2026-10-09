package nativequality

import (
	"context"
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

func rotateCandidates(inputs []c1.AdaptiveCandidateInput, offset int) []c1.AdaptiveCandidateInput {
	if len(inputs) == 0 {
		return nil
	}
	offset %= len(inputs)
	if offset < 0 {
		offset += len(inputs)
	}
	out := make([]c1.AdaptiveCandidateInput, 0, len(inputs))
	out = append(out, inputs[offset:]...)
	return append(out, inputs[:offset]...)
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
	nodes := s.Nodes(ctx)
	pool, index, err := measurementPool(w.Documents["05_routing.json"].Text, nodes, w.Targets)
	if err != nil {
		return err
	}
	activePool, _, err := routingPool(w.Documents["05_routing.json"].Text, nodes, w.Targets)
	if err != nil {
		return err
	}
	criteria, err := readCriteria(w.Documents["05_routing.json"].Text, w.Documents["07_observatory.json"].Text, index, w.Targets)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	snapshot := s.Reader.Snapshot(ctx)
	generation, err := prepareWithCriteria(snapshot, pool, s.status.Generation+1, 2, now, criteria)
	if err != nil {
		s.status.ReviewReason = "eligible-unavailable-or-over-limit"
		return err
	}
	retainHealthEvidence(&generation, s.Measurement.NativeQualityEvidence(snapshot))
	if _, err := batchSizes(len(generation.Candidates)); err != nil {
		return err
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
	if _, err := reserveSweepLocked(quotaPath, now); err != nil {
		quotaUnlock()
		s.status.ReviewReason = "quota-unavailable-or-exhausted"
		return err
	}
	quotaReceipt, err := readQuotaLocked(quotaPath, now)
	if err != nil {
		quotaUnlock()
		s.status.InspectionRequired = true
		return errQuota
	}
	quota := viewQuota(quotaReceipt, now)
	// The review decision is capped at 30 minutes; the separate native Apply
	// may need a bounded validation/restart/readback margin afterwards.
	job, cancelJob := context.WithTimeout(parent, sweepWall+6*time.Minute)
	done := make(chan struct{})
	s.cancel, s.done = cancelJob, done
	s.lastStartedAt = now
	s.pool = append([]string(nil), pool...)
	s.result = c1.AdaptiveResult{}
	s.status = Status{State: "running", Digest: w.Digest, Generation: generation.Generation, PoolCount: len(pool), ActivePoolCount: len(activePool), EligibleCount: len(generation.Candidates), LatencyLimitMS: criteria.maxRTT, LatencySource: criteria.latencySource, ReviewTrigger: trigger, Progress: c1.AdaptivePerformanceStatus{State: "running"}, AppliedState: "not-attempted", QuotaState: "available", QuotaUsedBytes: quota.UsedBytes, QuotaRemainingBytes: quota.RemainingBytes, QuotaReviewsUsed: quota.ReviewsUsed, QuotaNextResetAt: quota.NextResetAt}
	ordered := rotateCandidates(generation.Candidates, s.cursor)
	s.cursor = (s.cursor + 1) % len(ordered)
	go s.runSweep(job, cancelJob, done, generation, ordered, w.Digest, quotaUnlock)
	return nil
}

func (s *Service) runSweep(ctx context.Context, cancel context.CancelFunc, done chan struct{}, frozen c1.AdaptiveGeneration, ordered []c1.AdaptiveCandidateInput, digest string, quotaUnlock func()) {
	defer close(done)
	defer cancel()
	if quotaUnlock != nil {
		defer quotaUnlock()
	}
	sizes, _ := batchSizes(len(ordered))
	result := c1.AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: len(ordered), Generation: frozen.Generation, StartedAt: time.Now().UTC(), State: "running"}
	reason := ""
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
		if measureErr != nil || measured.State != "completed" || len(measured.Candidates) != size || measured.AggregateBytes < 0 {
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
	if quotaUnlock != nil {
		path := s.QuotaPath
		if path == "" {
			path = defaultQuotaPath
		}
		if err := settleSweepLocked(path, time.Now().UTC(), !inspection); err != nil {
			s.mu.Lock()
			s.status.InspectionRequired = true
			s.status.ReviewReason = "inspection-receipt-unavailable"
			s.mu.Unlock()
		}
	}
}
