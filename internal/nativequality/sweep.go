package nativequality

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
)

// batchSizes splits the admitted candidates into near-equal sequential
// batches of at most maxBatch, never leaving a one-node batch (the shared
// measurement runner needs at least two candidates).
func batchSizes(count, maxBatch int) ([]int, error) {
	if count < 2 || maxBatch < 2 || count > c1.NativeQualityBroadAttempts {
		return nil, ErrUnavailable
	}
	batches := (count + maxBatch - 1) / maxBatch
	sizes := make([]int, batches)
	for i := range sizes {
		sizes[i] = count / batches
		if i < count%batches {
			sizes[i]++
		}
		if sizes[i] < 2 {
			return nil, ErrUnavailable
		}
	}
	return sizes, nil
}

// startSweep starts an automatic review.
func (s *Service) startSweep(parent context.Context, trigger string) error {
	_, err := s.startReviewDone(parent, trigger, false)
	return err
}

// startNightly starts the nightly review and returns the channel closed when
// it, including its native Apply, has finished. Without apply it measures
// like a manual test and leaves the recommendation for an explicit Apply.
func (s *Service) startNightly(parent context.Context, apply bool) (<-chan struct{}, error) {
	return s.startReviewDone(parent, "nightly", !apply)
}

func (s *Service) startReview(parent context.Context, trigger string, manual bool) error {
	_, err := s.startReviewDone(parent, trigger, manual)
	return err
}

// startReviewDone freezes candidates and starts one review on either profile. An
// automatic (nightly) review may apply its decision; a manual review (the
// operator's speed test) runs the same pipeline and leaves the recommendation
// for an explicit Stage. A manual test is refused while the nightly window
// runs, because the window restarts Xray around it.
func (s *Service) startReviewDone(parent context.Context, trigger string, manual bool) (chan struct{}, error) {
	if s.Editor == nil || s.Lease == nil || s.Reader == nil || s.Nodes == nil || s.Measurement == nil || s.Resources == nil {
		return nil, ErrUnavailable
	}
	// The operator's "test all and apply" is explicit, so it runs even when
	// automatic maintenance is switched off; a profile without automatic
	// reviews still refuses it.
	if !manual && (!s.profile().Automatic || s.AutomaticDisabled && trigger != "operator") {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.cancel != nil || s.autoApplying || s.recovering || (trigger == "manual" || trigger == "operator") && s.maintenance.Phase != "" {
		return nil, c1.ErrManualBusy
	}
	if s.status.InspectionRequired {
		// Not transient: the operator must inspect first.
		return nil, ErrUnavailable
	}

	s.status.StartReason = ""
	if err := s.Resources.CheckConflict(); err != nil {
		s.status.ReviewReason = "native-speed-conflict-or-unavailable"
		if manual {
			s.status.StartReason = "resource-telemetry-unavailable"
			if errors.Is(err, resourcepolicy.ErrExternalBenchmark) {
				s.status.StartReason = "native-speed-conflict"
			}
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	w, err := s.Editor.Workspace(ctx)
	if err != nil || w.Pending != nil || !w.TargetsComplete {
		s.status.ReviewReason = "configuration-pending-or-unavailable"
		return nil, ErrUnavailable
	}
	routing := w.Documents["05_routing.json"].Text
	nodes := s.Nodes(ctx)
	pool, index, err := measurementPool(routing, nodes, w.Targets)
	if err != nil {
		return nil, err
	}
	resolved, orphans, _, err := resolveActivePool(routing, nodes, w.Targets, true)
	if err != nil {
		return nil, err
	}
	activePool := append(append([]string(nil), resolved...), orphans...)
	criteria, err := readCriteria(routing, w.Documents["07_observatory.json"].Text, index)
	if err != nil {
		return nil, err
	}
	if probeRouteShadowed(routing) {
		s.status.ReviewReason = "probe-route-shadowed"
		return nil, ErrUnavailable
	}
	now := time.Now().UTC()
	snapshot := s.Reader.Snapshot(ctx)
	if !snapshot.APIReachable || !snapshot.RoutingReachable || snapshot.Balancer.Override != "" {
		s.status.ReviewReason = "native-override-or-unavailable"
		return nil, ErrUnavailable
	}
	quotaPath := s.QuotaPath
	if quotaPath == "" {
		quotaPath = defaultQuotaPath
	}
	quotaUnlock, err := acquireQuotaLock(quotaPath)
	if err != nil {
		s.status.ReviewReason = "quota-busy-or-unavailable"
		return nil, err
	}
	limits := s.reviewLimits()
	previous, err := readQuotaLocked(quotaPath, now)
	if err != nil || previous.InspectionRequired {
		quotaUnlock()
		s.status.ReviewReason = "quota-unavailable-or-exhausted"
		return nil, errQuota
	}
	// Every enabled node is probed; the speed phase then takes the fastest
	// answering ones (revision 2026-10-11). There is no rotating subset.
	plan := planAll(pool, activePool, snapshot.Balancer.NativeSelected, s.poolSize())
	plan.Provisional = !previous.ProvisionalAt.IsZero()
	// The speed phase is capped at the profile wall; the probe-all pre-phase
	// and the separate native Apply have their own bounded margins. The Apply
	// chain is the save, the native job (up to four minutes) and the wait for
	// Xray's selection (up to three), so it gets eleven minutes.
	job, cancelJob := context.WithTimeout(context.WithoutCancel(parent), limits.Wall+time.Duration(len(plan.Candidates))*c1.RTTProbeTimeout+11*time.Minute)
	done := make(chan struct{})
	s.cancel, s.done = cancelJob, done
	s.lastStartedAt = now
	s.pool = append([]string(nil), pool...)
	s.sweepPlan = plan
	s.orphans = append([]string(nil), orphans...)
	s.rttAlive = nil
	s.result = c1.AdaptiveResult{}
	s.status = Status{State: "running", Digest: w.Digest, Generation: s.status.Generation + 1, PoolCount: len(pool), ActivePoolCount: len(activePool), ActivePool: append([]string(nil), activePool...), OrphanedPool: append([]string(nil), orphans...), ActivePoolState: "frozen-at-review", EligibleCount: plan.TotalEligible, TotalEligible: plan.TotalEligible, LatencyLimitMS: criteria.maxRTT, LatencySource: criteria.latencySource, ReviewTrigger: trigger, ReviewPhase: "rtt", ManualSample: manual, Progress: c1.AdaptivePerformanceStatus{State: "running"}, AppliedState: "not-attempted"}
	observeNativeSelection(&s.status, snapshot, now)
	frozen := c1.AdaptiveGeneration{NativeQuality: true, Generation: s.status.Generation, StartedAt: now, CurrentTarget: snapshot.Balancer.NativeSelected}
	go s.runSweep(job, cancelJob, done, frozen, plan, w.Digest, quotaPath, quotaUnlock, manual)
	return done, nil
}

// rttPrePhaseChunk bounds how long one pre-phase step holds the panel lease:
// six probes at RTTProbeTimeout plus cleanup stay well under two minutes.
const rttPrePhaseChunk = 6

// rttPrePhase probes every frozen candidate once through the same fixed
// endpoint and timeout. It returns the candidates admitted to the speed phase,
// in plan order, and every probed tag's health. A tag that does not answer
// within the probe timeout is unhealthy. The probe (a fixed HTTPS request
// through the loopback proxy and the outbound) is a different measurement from
// native Observatory and is not comparable with it, so the native maxRTT is not
// applied here (REQ-007): Xray still applies maxRTT to its own observations,
// and the speed phase ranks the answering candidates.
// Probes run in chunks; each chunk holds the panel lease only for its own
// duration and first rechecks the frozen configuration.
// rttPrePhase probes tags in chunks under the panel lease. A review
// publishes each chunk to its status; a recovery check does not, so its probes
// never mix with (or grow) the last review's results.
func (s *Service) rttPrePhase(ctx context.Context, tags []string, digest string, publish bool) ([]c1.AdaptiveCandidateInput, map[string]bool, string) {
	alive := make(map[string]bool, len(tags))
	var admitted []c1.AdaptiveCandidateInput
	for start := 0; start < len(tags); start += rttPrePhaseChunk {
		chunk := tags[start:min(start+rttPrePhaseChunk, len(tags))]
		if ctx.Err() != nil {
			return nil, nil, "review-timeout-or-cancelled"
		}
		release, err := s.Lease.TryAcquire()
		if err != nil {
			return nil, nil, "panel-busy"
		}
		wctx, stop := context.WithTimeout(ctx, 10*time.Second)
		w, err := s.Editor.Workspace(wctx)
		stop()
		if err != nil || w.Pending != nil || !w.TargetsComplete || w.Digest != digest {
			release()
			return nil, nil, "configuration-changed-or-pending"
		}
		samples, err := s.Measurement.MeasureRTT(ctx, chunk)
		release()
		if err != nil {
			if errors.Is(err, c1.ErrProbeCleanup) || errors.Is(err, c1.ErrProbeBlocked) {
				return nil, nil, "probe-cleanup-pending"
			}
			return nil, nil, "rtt-probe-unavailable"
		}
		if len(samples) != len(chunk) {
			return nil, nil, "rtt-probe-incomplete"
		}
		probes := make([]ProbeResult, 0, len(samples))
		for i, sample := range samples {
			if sample.Tag != chunk[i] {
				return nil, nil, "rtt-probe-incomplete"
			}
			healthy := sample.Valid && sample.RTTMS > 0
			alive[sample.Tag] = healthy
			probe := ProbeResult{Tag: sample.Tag, Answered: healthy, SampledAt: sample.SampledAt}
			if healthy {
				probe.RTTMS = sample.RTTMS
				admitted = append(admitted, c1.AdaptiveCandidateInput{Tag: sample.Tag, RTTMS: sample.RTTMS, LatestAt: sample.SampledAt})
			}
			probes = append(probes, probe)
		}
		// Publish each chunk as it lands, so the operator sees the probe move.
		if publish {
			s.mu.Lock()
			s.status.Probes = append(s.status.Probes, probes...)
			s.status.RTTProbed += len(chunk)
			s.mu.Unlock()
		}
	}
	return admitted, alive, ""
}

func (s *Service) runSweep(ctx context.Context, cancel context.CancelFunc, done chan struct{}, frozen c1.AdaptiveGeneration, plan sweepPlan, digest, quotaPath string, quotaUnlock func(), manual bool) {
	limits := s.reviewLimits()
	defer close(done)
	defer cancel()
	if quotaUnlock != nil {
		defer quotaUnlock()
	}
	ordered, alive, reason := s.rttPrePhase(ctx, plan.Candidates, digest, true)
	s.mu.Lock()
	// An orphaned selector has no outbound to probe: it is an unhealthy member.
	for _, orphan := range s.orphans {
		if alive != nil {
			alive[orphan] = false
		}
	}
	s.rttAlive = alive
	s.status.RTTValidCount = len(ordered)
	s.mu.Unlock()
	ordered = fastestAnswering(ordered, limits.Candidates)
	s.mu.Lock()
	s.status.SelectedForSpeed = len(ordered)
	s.mu.Unlock()
	if reason == "" && len(ordered) < 2 {
		reason = "rtt-candidates-insufficient"
	}
	reserved := false
	if reason == "" && quotaUnlock != nil && manual {
		// A manual review records its start; it applies nothing by itself.
		if err := recordComparisonStartLocked(quotaPath, time.Now().UTC()); err != nil {
			reason = "quota-unavailable-or-exhausted"
		}
	} else if reason == "" && quotaUnlock != nil {
		// The automatic review records its start and the durable intent that
		// fences an unknown Apply outcome; there is no traffic quota.
		if err := recordAutomaticStartLocked(quotaPath, time.Now().UTC()); err != nil {
			reason = "quota-unavailable-or-exhausted"
		} else {
			reserved = true
		}
	}
	if reason != "" {
		s.mu.Lock()
		s.result = c1.AdaptiveResult{}
		s.status.State = "deferred"
		s.status.ReviewReason = reason
		s.status.Progress = c1.AdaptivePerformanceStatus{State: "deferred"}
		// A closed probe gate is retried before the next probe is admitted, so
		// it needs no inspection flag of its own.
		s.cancel = nil
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.status.ReviewPhase = "speed"
	s.mu.Unlock()
	sizes, err := batchSizes(len(ordered), limits.BatchSize)
	if err != nil {
		reason = "eligible-unavailable"
	}
	s.mu.Lock()
	s.status.BatchTotal = len(sizes)
	s.mu.Unlock()
	batchWall := s.profile().Comparison(false).Wall() + c1.AdaptiveCleanupReserve
	result := c1.AdaptiveResult{NativeQuality: true, Sweep: true, SweepEligibleCount: len(ordered), Generation: frozen.Generation, StartedAt: time.Now().UTC(), CurrentTarget: frozen.CurrentTarget, State: "running"}
	position := 0
	pause := limits.Pause
	if s.batchPause > 0 {
		pause = s.batchPause
	}
	// measureBatch runs one batch of candidates after the pause between
	// batches and the same configuration, conflict and native-state checks;
	// it returns the measurement or why the batch could not run or finish.
	measureBatch := func(paused bool, candidates []c1.AdaptiveCandidateInput) (c1.AdaptiveResult, string) {
		if paused {
			timer := time.NewTimer(pause)
			select {
			case <-ctx.Done():
				timer.Stop()
				return c1.AdaptiveResult{}, "review-cancelled"
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return c1.AdaptiveResult{}, "review-timeout-or-cancelled"
		}
		wctx, stop := context.WithTimeout(ctx, 10*time.Second)
		w, err := s.Editor.Workspace(wctx)
		if err != nil || w.Pending != nil || !w.TargetsComplete || w.Digest != digest {
			stop()
			return c1.AdaptiveResult{}, "configuration-changed-or-pending"
		}
		if err := s.Resources.CheckConflict(); err != nil {
			stop()
			return c1.AdaptiveResult{}, "native-speed-conflict-or-unavailable"
		}
		snapshot := s.Reader.Snapshot(wctx)
		s.mu.Lock()
		observeNativeSelection(&s.status, snapshot, time.Now().UTC())
		s.mu.Unlock()
		if !snapshot.APIReachable || !snapshot.RoutingReachable || !snapshot.ObservatoryReachable || snapshot.Balancer.Override != "" {
			stop()
			return c1.AdaptiveResult{}, "native-override-or-unavailable"
		}
		stop()
		release, err := s.Lease.TryAcquire()
		if err != nil {
			return c1.AdaptiveResult{}, "panel-busy"
		}
		batch := c1.AdaptiveGeneration{NativeQuality: true, Generation: frozen.Generation, StartedAt: time.Now().UTC(), CurrentTarget: frozen.CurrentTarget, Candidates: candidates}
		batchCtx, batchCancel := context.WithTimeout(ctx, batchWall)
		measured, measureErr := s.Measurement.MeasureNativeQuality(batchCtx, batch, func(progress c1.AdaptivePerformanceStatus) {
			s.mu.Lock()
			s.status.Progress = progress
			s.mu.Unlock()
		})
		batchCancel()
		release()
		matched := len(measured.Candidates) == len(candidates)
		if matched {
			for i, candidate := range measured.Candidates {
				if candidate.Tag != candidates[i].Tag {
					matched = false
					break
				}
			}
		}
		if measureErr != nil || measured.State != "completed" || !matched || measured.AggregateBytes < 0 {
			// Only an automatic review with an uncertain probe cleanup is fenced:
			// its quota receipt keeps the flag, so explicit inspection can clear
			// it. A manual review has no receipt to clear and the probe gate is
			// retried before the next probe, so it just fails.
			if !manual && (measured.State == "cleanup-pending" || errors.Is(measureErr, c1.ErrProbeCleanup) || errors.Is(measureErr, c1.ErrManualCleanupPending)) {
				s.mu.Lock()
				s.status.InspectionRequired = true
				s.mu.Unlock()
			}
			return measured, "batch-incomplete-or-unknown"
		}
		return measured, ""
	}
	for batchIndex, size := range sizes {
		if reason != "" {
			break
		}
		measured, batchReason := measureBatch(batchIndex > 0, ordered[position:position+size])
		result.AggregateBytes += max(measured.AggregateBytes, 0)
		if result.AggregateBytes > limits.Bytes {
			reason = "review-budget-exceeded"
			break
		}
		if batchReason == "" || batchReason == "batch-incomplete-or-unknown" {
			result.Candidates = append(result.Candidates, measured.Candidates...)
			position += len(measured.Candidates)
			s.mu.Lock()
			s.status.AttemptedCount = position
			s.status.ValidCount += measured.ValidCount
			s.status.BatchCount++
			s.status.AggregateBytes = result.AggregateBytes
			s.mu.Unlock()
		}
		if batchReason != "" {
			reason = batchReason
			break
		}
	}
	// Second pass (standard profile): measure the best-ranked nodes again and
	// combine both results, so one lucky or unlucky transfer does not decide
	// the pool (PC benchmark 2026-10-12: a node read 8 then 186 Mbps).
	if reason == "" && limits.Repeat > 0 && len(result.Candidates) == len(ordered) {
		again := s.repeatCandidates(result, ordered, limits.Repeat)
		repeatSizes, sizeErr := batchSizes(len(again), limits.BatchSize)
		if sizeErr == nil && len(again) > 0 {
			s.mu.Lock()
			s.status.BatchTotal += len(repeatSizes)
			s.mu.Unlock()
			at := 0
			for _, size := range repeatSizes {
				measured, batchReason := measureBatch(true, again[at:at+size])
				result.AggregateBytes += max(measured.AggregateBytes, 0)
				if result.AggregateBytes > limits.Bytes {
					reason = "review-budget-exceeded"
					break
				}
				if batchReason != "" {
					// The first pass stands; only an inspection fence or the
					// review's own cancellation ends the review.
					s.mu.Lock()
					fenced := s.status.InspectionRequired
					s.mu.Unlock()
					if fenced || ctx.Err() != nil {
						reason = batchReason
					}
					break
				}
				combineRepeat(&result, measured.Candidates)
				at += size
				s.mu.Lock()
				s.status.Remeasured += len(measured.Candidates)
				s.status.BatchCount++
				s.status.AggregateBytes = result.AggregateBytes
				s.mu.Unlock()
			}
			if reason == "" {
				// A second pass stopped early still completes the speed
				// stage: no planned batch is left pending.
				s.mu.Lock()
				s.status.BatchTotal = s.status.BatchCount
				s.mu.Unlock()
			}
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
	if reason == "" && len(result.Candidates) == len(ordered) && result.CompletedAt.Sub(result.StartedAt) <= limits.Wall {
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
	apply := result.State == "completed" && ctx.Err() == nil && !manual
	if apply {
		s.autoApplying = true
		s.status.State = "applying"
	}
	s.mu.Unlock()
	if apply {
		s.applySweep(ctx, result, digest, append([]string(nil), s.pool...))
	}
	s.mu.Lock()
	s.autoApplying = false
	s.cancel = nil
	if apply {
		if s.status.AppliedState == "applied" || s.status.AppliedState == "no-op" {
			s.status.State = "completed"
		} else {
			s.status.State = "failed"
		}
	}
	inspection := s.status.InspectionRequired
	// A completed automatic review that applied or kept its measured pool ends
	// a provisional recovery pool's label.
	ranked := apply && (s.status.AppliedState == "applied" || s.status.AppliedState == "no-op")
	s.mu.Unlock()
	if reserved {
		if err := settleSweepLocked(quotaPath, time.Now().UTC(), !inspection, ranked); err != nil {
			s.mu.Lock()
			s.status.InspectionRequired = true
			s.status.ReviewReason = "inspection-receipt-unavailable"
			s.mu.Unlock()
		}
	}
}

// planAll freezes every eligible node as a probe candidate, in tag order. A
// selector wider than the pool size is a broad selector to adopt.
func planAll(eligible, active []string, nativeSelected string, poolSize int) sweepPlan {
	candidates := append([]string(nil), eligible...)
	sort.Strings(candidates)
	return sweepPlan{Candidates: candidates, Active: append([]string(nil), active...), NativeSelected: nativeSelected, TotalEligible: len(eligible), FirstInitialization: len(active) > poolSize, CursorState: "all-eligible"}
}

// fastestAnswering keeps the limit answering candidates with the lowest probe
// RTT (ties by tag), in that order, for the speed phase.
func fastestAnswering(admitted []c1.AdaptiveCandidateInput, limit int) []c1.AdaptiveCandidateInput {
	out := append([]c1.AdaptiveCandidateInput(nil), admitted...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RTTMS == out[j].RTTMS {
			return out[i].Tag < out[j].Tag
		}
		return out[i].RTTMS < out[j].RTTMS
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// repeatCandidates returns the best-ranked valid first-pass nodes, at most
// limit, in rank order, with the probe RTT they were admitted with.
func (s *Service) repeatCandidates(result c1.AdaptiveResult, ordered []c1.AdaptiveCandidateInput, limit int) []c1.AdaptiveCandidateInput {
	provisional := result
	provisional.CompletedAt = time.Now().UTC()
	provisional.ShortlistCount = len(provisional.Candidates)
	provisional.State = "completed"
	costs, err := c1.NativeQualityCosts(provisional, provisional.CompletedAt, s.pool)
	if err != nil {
		return nil
	}
	value := make(map[string]float64, len(costs))
	for _, cost := range costs {
		value[cost.Match] = cost.Value
	}
	type scored struct {
		input c1.AdaptiveCandidateInput
		score float64
	}
	inputs := make(map[string]c1.AdaptiveCandidateInput, len(ordered))
	for _, input := range ordered {
		inputs[input.Tag] = input
	}
	var ranked []scored
	for _, candidate := range result.Candidates {
		cost, ok := value["^"+regexp.QuoteMeta(candidate.Tag)+"$"]
		input, known := inputs[candidate.Tag]
		if !candidate.Valid || !ok || !known {
			continue
		}
		ranked = append(ranked, scored{input: input, score: float64(candidate.RTTMS) * math.Sqrt(cost)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].input.Tag < ranked[j].input.Tag
		}
		return ranked[i].score < ranked[j].score
	})
	out := make([]c1.AdaptiveCandidateInput, 0, min(limit, len(ranked)))
	for _, item := range ranked[:min(limit, len(ranked))] {
		out = append(out, item.input)
	}
	return out
}

// combineRepeat folds second-pass results into the first: two valid results
// give their geometric mean; a valid second result replaces an invalid first
// one; a failed second transfer leaves the first result as it was.
func combineRepeat(result *c1.AdaptiveResult, again []c1.AdaptiveCandidateResult) {
	index := make(map[string]int, len(result.Candidates))
	for i, candidate := range result.Candidates {
		index[candidate.Tag] = i
	}
	for _, second := range again {
		i, ok := index[second.Tag]
		if !ok || !second.Valid {
			continue
		}
		first := &result.Candidates[i]
		if first.Valid {
			first.DownloadBPS = math.Sqrt(first.DownloadBPS * second.DownloadBPS)
			first.UploadBPS = math.Sqrt(first.UploadBPS * second.UploadBPS)
		} else {
			first.DownloadBPS, first.UploadBPS, first.Valid, first.ErrorCode = second.DownloadBPS, second.UploadBPS, true, ""
		}
		first.SampledAt = second.SampledAt
	}
}
