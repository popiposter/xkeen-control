package c1

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"
)

const (
	AdaptiveMode                        = "adaptive"
	AdaptiveCadence                     = 3 * time.Hour
	AdaptiveMaxDownloadBytes      int64 = 16 * MiB
	AdaptiveMaxUploadBytes        int64 = 8 * MiB
	AdaptiveMaxWallTime                 = 30 * time.Second
	AdaptiveCleanupReserve              = 3 * time.Second
	AdaptiveWorkTimeout                 = AdaptiveMaxWallTime - AdaptiveCleanupReserve
	AdaptiveStageTimeout                = 8 * time.Second
	AdaptiveMinimumRateDuration         = 250 * time.Millisecond
	AdaptiveEarlyStopDuration           = time.Second
	AdaptiveShortlistLimit              = 5
	AdaptiveMaxCandidates               = 6
	AdaptiveMaxGenerationBytes          = 144 * MiB
	AdaptiveMaxGenerationWallTime       = 180 * time.Second
	AdaptiveRTTGuardMS                  = 75
	AdaptiveRTTGuardRatio               = 2
	AdaptiveQualityHysteresis           = 1.10
)

var (
	adaptiveDownloadStages = [...]int64{1 * MiB, 3 * MiB, 4 * MiB, 8 * MiB}
	adaptiveUploadStages   = [...]int64{1 * MiB, 3 * MiB, 4 * MiB}
)

const (
	AdaptiveReasonManualOverride         = "manual-override"
	AdaptiveReasonBusy                   = "busy"
	AdaptiveReasonUnavailable            = "unavailable"
	AdaptiveReasonNoCurrentTarget        = "no-current-target"
	AdaptiveReasonCurrentIneligible      = "current-ineligible"
	AdaptiveReasonInsufficientCandidates = "insufficient-candidates"
	AdaptiveReasonGenerationBudget       = "generation-budget"
	AdaptiveReasonCancelled              = "cancelled"
	AdaptiveReasonCleanupPending         = "cleanup-pending"
	AdaptiveReasonStaleGeneration        = "stale-generation"
	AdaptiveReasonCurrentInvalid         = "current-invalid"
	AdaptiveReasonNoChallenger           = "no-challenger"
	AdaptiveReasonMinimumDwell           = "minimum-dwell"
	AdaptiveReasonHysteresis             = "hysteresis"
	AdaptiveReasonNoSwitch               = "no-switch"
	AdaptiveReasonAdaptiveQuality        = "adaptive-quality"
)

// AdaptiveCandidateInput is the frozen, secret-free RTT evidence passed from
// Supervisor to one quality generation. The input is built only from the
// Observatory samples already retained by PolicyEngine.
type AdaptiveCandidateInput struct {
	Tag      string
	RTTMS    int64
	Samples  int
	LatestAt time.Time
}

// AdaptiveCandidate is a concise compatibility name for the frozen input
// used by tests and internal callers.
type AdaptiveCandidate = AdaptiveCandidateInput

// AdaptiveGeneration freezes all selection inputs before any fixed transfer
// starts. It deliberately contains no endpoint, profile or registry material.
type AdaptiveGeneration struct {
	Generation    uint64
	StartedAt     time.Time
	CurrentTarget string
	StableSince   time.Time
	Candidates    []AdaptiveCandidateInput
}

type AdaptiveCandidateResult struct {
	Tag         string
	RTTMS       int64
	DownloadBPS float64
	UploadBPS   float64
	Score       float64
	Valid       bool
	ErrorCode   string
}

type AdaptiveResult struct {
	Generation     uint64
	StartedAt      time.Time
	CompletedAt    time.Time
	CurrentTarget  string
	ShortlistCount int
	ValidCount     int
	CurrentValid   bool
	SelectedTarget string
	SelectedScore  float64
	CurrentScore   float64
	SwitchApplied  bool
	State          string
	ReasonCode     string
	AggregateBytes int64
	Duration       time.Duration
	Candidates     []AdaptiveCandidateResult
}

// AdaptiveCandidateStatus is the only per-candidate material exposed by the
// authenticated performance projection.
type AdaptiveCandidateStatus struct {
	Tag         string  `json:"tag"`
	RTTMS       int64   `json:"rttMs"`
	DownloadBPS float64 `json:"downloadBps"`
	UploadBPS   float64 `json:"uploadBps"`
	Score       float64 `json:"score"`
	Valid       bool    `json:"valid"`
}

// AdaptivePerformanceStatus is bounded, RAM-only status for the scheduled
// adaptive quality generation. It has no run-now or configuration surface.
type AdaptivePerformanceStatus struct {
	State          string                    `json:"state"`
	NextRunAt      time.Time                 `json:"nextRunAt"`
	StartedAt      time.Time                 `json:"startedAt,omitempty"`
	CompletedAt    time.Time                 `json:"completedAt,omitempty"`
	Generation     uint64                    `json:"generation"`
	CurrentTarget  string                    `json:"currentTarget,omitempty"`
	ShortlistCount int                       `json:"shortlistCount"`
	ValidCount     int                       `json:"validCount"`
	SelectedTarget string                    `json:"selectedTarget,omitempty"`
	SwitchApplied  bool                      `json:"switchApplied"`
	ReasonCode     string                    `json:"reasonCode,omitempty"`
	Candidates     []AdaptiveCandidateStatus `json:"candidates,omitempty"`
}

// AdaptiveStatus is retained as a short name for the same bounded projection.
type AdaptiveStatus = AdaptivePerformanceStatus

func idleAdaptivePerformanceStatus() AdaptivePerformanceStatus {
	return AdaptivePerformanceStatus{State: "waiting"}
}

func DefaultAdaptivePerformanceStatus() AdaptivePerformanceStatus {
	return idleAdaptivePerformanceStatus()
}

// AdaptiveMeasurementTransport is the shared fixed down/up seam. The
// production implementation is the Slice D fixed transport; tests use an
// offline synthetic adapter.
type AdaptiveMeasurementTransport = BandwidthMeasurementTransport

// AdaptiveRunner executes one frozen generation sequentially under the same
// ProbeRouter ownership used by Slice D. A transport failure invalidates one
// candidate, while cancellation or cleanup failure terminates the generation.
type AdaptiveRunner struct {
	Probe     *ProbeRouter
	Transport BandwidthMeasurementTransport
	Now       func() time.Time
}

func NewAdaptiveRunner(probe *ProbeRouter) *AdaptiveRunner {
	return &AdaptiveRunner{Probe: probe, Transport: newFixedMeasurementTransport()}
}

func (r *AdaptiveRunner) Run(parent context.Context, generation AdaptiveGeneration, publish func(AdaptivePerformanceStatus)) AdaptiveResult {
	if parent == nil {
		parent = context.Background()
	}
	started := adaptiveNow(r)
	result := AdaptiveResult{
		Generation:     generation.Generation,
		StartedAt:      started,
		CurrentTarget:  generation.CurrentTarget,
		ShortlistCount: len(generation.Candidates),
		State:          "running",
	}
	status := AdaptivePerformanceStatus{
		State:          "running",
		StartedAt:      started,
		Generation:     generation.Generation,
		CurrentTarget:  safeTag(generation.CurrentTarget),
		ShortlistCount: len(generation.Candidates),
		Candidates:     adaptiveInputStatuses(generation.Candidates),
	}
	emit := func() {
		if publish != nil {
			publish(sanitizeAdaptiveStatus(status))
		}
	}
	emit()
	finishEarly := func(state, reason string) AdaptiveResult {
		result = finishAdaptiveResult(result, state, reason, adaptiveNow(r))
		status.State = result.State
		status.CompletedAt = result.CompletedAt
		status.ValidCount = result.ValidCount
		status.ReasonCode = result.ReasonCode
		status.Candidates = adaptiveResultStatuses(result.Candidates)
		emit()
		return result
	}

	if !validAdaptiveGeneration(generation) || len(generation.Candidates) > AdaptiveMaxCandidates {
		return finishEarly("failed", AdaptiveReasonUnavailable)
	}
	if r == nil || r.Probe == nil {
		return finishEarly("failed", AdaptiveReasonUnavailable)
	}
	transport := r.Transport
	if transport == nil {
		transport = newFixedMeasurementTransport()
	}

	generationContext, cancelGeneration := context.WithTimeout(parent, AdaptiveMaxGenerationWallTime)
	defer cancelGeneration()
	for _, input := range generation.Candidates {
		if err := parent.Err(); err != nil {
			result.State = "cancelled"
			result.ReasonCode = AdaptiveReasonCancelled
			break
		}
		if generationContext.Err() != nil {
			result.State = "failed"
			result.ReasonCode = AdaptiveReasonGenerationBudget
			break
		}
		if !adaptiveCanAdmitCandidate(generationContext, result.AggregateBytes) {
			result.State = "failed"
			result.ReasonCode = AdaptiveReasonGenerationBudget
			break
		}

		candidateContext, cancelCandidate := context.WithTimeout(generationContext, AdaptiveWorkTimeout)
		execution := &adaptiveExecution{transport: transport}
		probeErr := r.Probe.WithTarget(candidateContext, AdaptiveMode, input.Tag, func(probeContext context.Context) error {
			return execution.run(probeContext)
		})
		candidateErr := candidateContext.Err()
		cancelCandidate()

		if r.Probe.Blocked() || errors.Is(probeErr, ErrProbeCleanup) || errors.Is(probeErr, ErrProbeBlocked) {
			result.State = "cleanup-pending"
			result.ReasonCode = AdaptiveReasonCleanupPending
			status.State = result.State
			status.ReasonCode = result.ReasonCode
			status.CompletedAt = adaptiveNow(r)
			emit()
			break
		}
		if err := parent.Err(); err != nil {
			result.State = "cancelled"
			result.ReasonCode = AdaptiveReasonCancelled
			break
		}
		if generationContext.Err() != nil && candidateErr == context.DeadlineExceeded {
			result.State = "failed"
			result.ReasonCode = AdaptiveReasonGenerationBudget
			break
		}

		candidate := AdaptiveCandidateResult{
			Tag:         input.Tag,
			RTTMS:       input.RTTMS,
			DownloadBPS: execution.downloadBPS,
			UploadBPS:   execution.uploadBPS,
			Valid:       probeErr == nil && candidateErr == nil && finitePositive(execution.downloadBPS) && finitePositive(execution.uploadBPS),
			ErrorCode:   classifyAdaptiveCandidateError(probeErr, candidateErr, execution),
		}
		if candidate.Valid {
			result.AggregateBytes += execution.bytes
		} else if result.AggregateBytes+execution.bytes <= AdaptiveMaxGenerationBytes {
			// Invalid transport bytes are still bounded accounting; they never
			// become score evidence or a persistence input.
			result.AggregateBytes += maxInt64(execution.bytes, 0)
		}
		result.Candidates = append(result.Candidates, candidate)
		status.Candidates = adaptiveResultStatuses(result.Candidates)
		status.ValidCount = countAdaptiveValid(result.Candidates)
		status.ShortlistCount = result.ShortlistCount
		emit()
	}

	result.ValidCount = countAdaptiveValid(result.Candidates)
	result.CurrentValid = adaptiveCandidateValid(result.Candidates, result.CurrentTarget)
	if result.State == "running" {
		result.State = "completed"
		result.ReasonCode = AdaptiveReasonNoSwitch
		winner, winnerScore, currentScore, challenger := scoreAdaptiveResults(result.Candidates, result.CurrentTarget)
		result.SelectedTarget = winner
		result.SelectedScore = winnerScore
		result.CurrentScore = currentScore
		if !challenger {
			result.SelectedTarget = ""
		}
		status.Candidates = adaptiveResultStatuses(result.Candidates)
		status.ValidCount = result.ValidCount
		status.SelectedTarget = safeTag(result.SelectedTarget)
		status.ReasonCode = result.ReasonCode
	}
	result.CompletedAt = adaptiveNow(r)
	result.Duration = result.CompletedAt.Sub(result.StartedAt)
	status.State = result.State
	status.CompletedAt = result.CompletedAt
	status.ReasonCode = safeAdaptiveReason(result.ReasonCode)
	status.ValidCount = result.ValidCount
	status.SelectedTarget = safeTag(result.SelectedTarget)
	status.Candidates = adaptiveResultStatuses(result.Candidates)
	emit()
	return result
}

type adaptiveExecution struct {
	transport   BandwidthMeasurementTransport
	bytes       int64
	downloadBPS float64
	uploadBPS   float64
}

func (e *adaptiveExecution) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	download, err := e.runDirection(ctx, adaptiveDownloadStages[:], e.transport.Download)
	if err != nil {
		return err
	}
	e.downloadBPS = download
	upload, err := e.runDirection(ctx, adaptiveUploadStages[:], e.transport.Upload)
	if err != nil {
		return err
	}
	e.uploadBPS = upload
	return nil
}

func (e *adaptiveExecution) runDirection(ctx context.Context, stages []int64, transfer func(context.Context, int64) (ManualTransfer, error)) (float64, error) {
	complete := make([]manualCompleteStage, 0, len(stages))
	for _, payload := range stages {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		stageStarted := time.Now()
		stageContext, cancel, ok := boundedStageContext(ctx, AdaptiveStageTimeout)
		if !ok {
			return 0, ctx.Err()
		}
		result, err := transfer(stageContext, payload)
		stageContextErr := stageContext.Err()
		cancel()
		if result.Duration <= 0 {
			result.Duration = time.Since(stageStarted)
		}
		reportedBytes := result.Bytes
		if reportedBytes < 0 {
			reportedBytes = 0
		}
		if reportedBytes > payload {
			reportedBytes = payload
		}
		// Count bounded bytes even when the stage is invalid. A failed
		// transport may have read a partial response, and those bytes still
		// consume the generation envelope.
		e.bytes += reportedBytes
		if err != nil || stageContextErr != nil {
			if err != nil {
				return 0, err
			}
			return 0, stageContextErr
		}
		if result.Bytes < 0 || result.Bytes > payload || result.Bytes != payload || result.Duration <= 0 {
			return 0, errors.New("adaptive transfer was incomplete")
		}
		complete = append(complete, manualCompleteStage{bytes: result.Bytes, duration: result.Duration})
		if result.Duration >= AdaptiveEarlyStopDuration {
			break
		}
	}
	rate, ok := aggregateMeasurementRate(complete, AdaptiveMinimumRateDuration)
	if !ok {
		return 0, errors.New("adaptive transfer rate is invalid")
	}
	return rate, nil
}

func adaptiveCanAdmitCandidate(ctx context.Context, aggregateBytes int64) bool {
	if aggregateBytes < 0 || aggregateBytes+AdaptiveMaxDownloadBytes+AdaptiveMaxUploadBytes > AdaptiveMaxGenerationBytes {
		return false
	}
	deadline, ok := ctx.Deadline()
	return ok && time.Until(deadline) >= AdaptiveMaxWallTime
}

func validAdaptiveGeneration(generation AdaptiveGeneration) bool {
	if generation.Generation == 0 || !validTag(generation.CurrentTarget) || len(generation.Candidates) < 2 {
		return false
	}
	seen := make(map[string]struct{}, len(generation.Candidates))
	foundCurrent := false
	for _, candidate := range generation.Candidates {
		if !validTag(candidate.Tag) || candidate.RTTMS <= 0 {
			return false
		}
		if _, ok := seen[candidate.Tag]; ok {
			return false
		}
		seen[candidate.Tag] = struct{}{}
		foundCurrent = foundCurrent || candidate.Tag == generation.CurrentTarget
	}
	return foundCurrent
}

func adaptiveNow(r *AdaptiveRunner) time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func finishAdaptiveResult(result AdaptiveResult, state, reason string, completed time.Time) AdaptiveResult {
	result.State = state
	result.ReasonCode = reason
	result.CompletedAt = completed
	result.Duration = completed.Sub(result.StartedAt)
	result.ValidCount = countAdaptiveValid(result.Candidates)
	result.CurrentValid = adaptiveCandidateValid(result.Candidates, result.CurrentTarget)
	return result
}

func classifyAdaptiveCandidateError(probeErr, contextErr error, execution *adaptiveExecution) string {
	if probeErr == nil && contextErr == nil && execution != nil {
		return ""
	}
	if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(contextErr, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(probeErr, context.Canceled) || errors.Is(contextErr, context.Canceled) {
		return "cancelled"
	}
	return "transport-failure"
}

func adaptiveInputStatuses(inputs []AdaptiveCandidateInput) []AdaptiveCandidateStatus {
	result := make([]AdaptiveCandidateStatus, 0, minInt(len(inputs), AdaptiveMaxCandidates))
	for index, input := range inputs {
		if index >= AdaptiveMaxCandidates {
			break
		}
		result = append(result, AdaptiveCandidateStatus{Tag: safeTag(input.Tag), RTTMS: positiveInt64(input.RTTMS)})
	}
	return result
}

func adaptiveResultStatuses(results []AdaptiveCandidateResult) []AdaptiveCandidateStatus {
	result := make([]AdaptiveCandidateStatus, 0, minInt(len(results), AdaptiveMaxCandidates))
	for index, candidate := range results {
		if index >= AdaptiveMaxCandidates {
			break
		}
		item := AdaptiveCandidateStatus{Tag: safeTag(candidate.Tag), RTTMS: positiveInt64(candidate.RTTMS), Valid: candidate.Valid}
		if finitePositive(candidate.DownloadBPS) {
			item.DownloadBPS = candidate.DownloadBPS
		}
		if finitePositive(candidate.UploadBPS) {
			item.UploadBPS = candidate.UploadBPS
		}
		if finiteNonNegative(candidate.Score) {
			item.Score = candidate.Score
		}
		if !item.Valid {
			item.Score = 0
		}
		result = append(result, item)
	}
	return result
}

func countAdaptiveValid(results []AdaptiveCandidateResult) int {
	count := 0
	for _, candidate := range results {
		if candidate.Valid && candidate.RTTMS > 0 && finitePositive(candidate.DownloadBPS) && finitePositive(candidate.UploadBPS) {
			count++
		}
	}
	return count
}

func adaptiveCandidateValid(results []AdaptiveCandidateResult, tag string) bool {
	for _, candidate := range results {
		if candidate.Tag == tag {
			return candidate.Valid && candidate.RTTMS > 0 && finitePositive(candidate.DownloadBPS) && finitePositive(candidate.UploadBPS)
		}
	}
	return false
}

func scoreAdaptiveResults(results []AdaptiveCandidateResult, current string) (winner string, winnerScore, currentScore float64, challenger bool) {
	bestRTT := int64(0)
	bestDownload := 0.0
	bestUpload := 0.0
	for _, candidate := range results {
		if !candidate.Valid || candidate.RTTMS <= 0 || !finitePositive(candidate.DownloadBPS) || !finitePositive(candidate.UploadBPS) {
			continue
		}
		if bestRTT == 0 || candidate.RTTMS < bestRTT {
			bestRTT = candidate.RTTMS
		}
		if candidate.DownloadBPS > bestDownload {
			bestDownload = candidate.DownloadBPS
		}
		if candidate.UploadBPS > bestUpload {
			bestUpload = candidate.UploadBPS
		}
	}
	if bestRTT <= 0 || !finitePositive(bestDownload) || !finitePositive(bestUpload) {
		return "", 0, 0, false
	}
	bestCandidateScore := -1.0
	for index := range results {
		candidate := &results[index]
		if !candidate.Valid || candidate.RTTMS <= 0 || !finitePositive(candidate.DownloadBPS) || !finitePositive(candidate.UploadBPS) {
			continue
		}
		latencyComponent := clampFloat(float64(bestRTT)/float64(candidate.RTTMS), 0, 1)
		downloadComponent := math.Log1p(candidate.DownloadBPS) / math.Log1p(bestDownload)
		uploadComponent := math.Log1p(candidate.UploadBPS) / math.Log1p(bestUpload)
		candidate.Score = 0.35*latencyComponent + 0.45*downloadComponent + 0.20*uploadComponent
		if !finiteNonNegative(candidate.Score) {
			candidate.Valid = false
			candidate.Score = 0
			continue
		}
		if candidate.Tag == current {
			currentScore = candidate.Score
		}
		if candidate.Tag != current {
			if float64(candidate.RTTMS) <= float64(bestRTT)+AdaptiveRTTGuardMS && float64(candidate.RTTMS) <= float64(bestRTT)*AdaptiveRTTGuardRatio {
				challenger = true
			} else {
				continue
			}
		}
		if candidate.Score > bestCandidateScore || (candidate.Score == bestCandidateScore && (winner == "" || candidate.Tag < winner)) {
			winner, bestCandidateScore = candidate.Tag, candidate.Score
		}
	}
	return winner, bestCandidateScore, currentScore, challenger
}

func clampFloat(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func positiveInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func safeAdaptiveReason(reason string) string {
	if reason == "" {
		return ""
	}
	switch reason {
	case AdaptiveReasonManualOverride, AdaptiveReasonBusy, AdaptiveReasonUnavailable,
		AdaptiveReasonNoCurrentTarget, AdaptiveReasonCurrentIneligible, AdaptiveReasonInsufficientCandidates,
		AdaptiveReasonGenerationBudget, AdaptiveReasonCancelled, AdaptiveReasonCleanupPending,
		AdaptiveReasonStaleGeneration, AdaptiveReasonCurrentInvalid, AdaptiveReasonNoChallenger,
		AdaptiveReasonMinimumDwell, AdaptiveReasonHysteresis, AdaptiveReasonNoSwitch,
		AdaptiveReasonAdaptiveQuality:
		return reason
	default:
		return AdaptiveReasonUnavailable
	}
}

func sanitizeAdaptiveStatus(status AdaptivePerformanceStatus) AdaptivePerformanceStatus {
	switch status.State {
	case "waiting", "running", "skipped", "completed", "failed", "cancelled", "cleanup-pending":
	default:
		status.State = "failed"
	}
	status.CurrentTarget = safeTag(status.CurrentTarget)
	status.SelectedTarget = safeTag(status.SelectedTarget)
	if status.ShortlistCount < 0 {
		status.ShortlistCount = 0
	}
	if status.ShortlistCount > AdaptiveMaxCandidates {
		status.ShortlistCount = AdaptiveMaxCandidates
	}
	if status.ValidCount < 0 {
		status.ValidCount = 0
	}
	if status.ValidCount > AdaptiveMaxCandidates {
		status.ValidCount = AdaptiveMaxCandidates
	}
	status.ReasonCode = safeAdaptiveReason(status.ReasonCode)
	status.Candidates = adaptiveResultStatusesFromStatus(status.Candidates)
	return status
}

func adaptiveResultStatusesFromStatus(candidates []AdaptiveCandidateStatus) []AdaptiveCandidateStatus {
	result := make([]AdaptiveCandidateStatus, 0, minInt(len(candidates), AdaptiveMaxCandidates))
	for index, candidate := range candidates {
		if index >= AdaptiveMaxCandidates {
			break
		}
		candidate.Tag = safeTag(candidate.Tag)
		candidate.RTTMS = positiveInt64(candidate.RTTMS)
		if !finitePositive(candidate.DownloadBPS) {
			candidate.DownloadBPS = 0
		}
		if !finitePositive(candidate.UploadBPS) {
			candidate.UploadBPS = 0
		}
		if !finiteNonNegative(candidate.Score) {
			candidate.Score = 0
		}
		if !candidate.Valid {
			candidate.Score = 0
		}
		result = append(result, candidate)
	}
	return result
}

// sortAdaptiveCandidates is shared with Supervisor's frozen shortlist builder
// and kept here so the ordering contract remains visible beside scoring.
func sortAdaptiveCandidates(candidates []AdaptiveCandidateInput) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].RTTMS != candidates[j].RTTMS {
			return candidates[i].RTTMS < candidates[j].RTTMS
		}
		return candidates[i].Tag < candidates[j].Tag
	})
}
