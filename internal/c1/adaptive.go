package c1

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
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
	NativeQualityMaxAttempts            = 12
	NativeQualityBroadCandidates        = 12
	NativeQualityBroadAttempts          = 18
	NativeQualityBroadBytes             = 288 * MiB
	NativeQualityBroadWallTime          = 360 * time.Second
	AdaptiveMaxGenerationBytes          = 144 * MiB
	AdaptiveMaxGenerationWallTime       = 180 * time.Second
	AdaptiveMaximumRTTMS                = 750
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
	Tag           string
	RTTMS         int64
	Samples       int
	LatestAt      time.Time
	HealthPenalty float64
}

// AdaptiveCandidate is a concise compatibility name for the frozen input
// used by tests and internal callers.
type AdaptiveCandidate = AdaptiveCandidateInput

// AdaptiveGeneration freezes all selection inputs before any fixed transfer
// starts. It deliberately contains no endpoint, profile or registry material.
type AdaptiveGeneration struct {
	NativeQuality bool
	BroadSample   bool
	Fallbacks     []AdaptiveCandidateInput
	Generation    uint64
	StartedAt     time.Time
	CurrentTarget string
	StableSince   time.Time
	Candidates    []AdaptiveCandidateInput
}

type AdaptiveCandidateResult struct {
	Tag           string
	RTTMS         int64
	DownloadBPS   float64
	UploadBPS     float64
	Score         float64
	Valid         bool
	ErrorCode     string
	HealthPenalty float64
}

type AdaptiveResult struct {
	NativeQuality  bool
	BroadSample    bool
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
	NativeQuality  bool                      `json:"-"`
	BroadSample    bool                      `json:"-"`
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
	Resources *resourcepolicy.Guard
	Probe     *ProbeRouter
	Transport BandwidthMeasurementTransport
	Now       func() time.Time
}

func (r *AdaptiveRunner) Limits(broad bool) resourcepolicy.Limits {
	if r != nil && r.Resources != nil {
		return r.Resources.Profile.Comparison(broad)
	}
	return (resourcepolicy.Profile{}).Comparison(broad)
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
		NativeQuality:  generation.NativeQuality,
		BroadSample:    generation.BroadSample,
		Generation:     generation.Generation,
		StartedAt:      started,
		CurrentTarget:  generation.CurrentTarget,
		ShortlistCount: len(generation.Candidates),
		State:          "running",
	}
	status := AdaptivePerformanceStatus{
		State:          "running",
		StartedAt:      started,
		NativeQuality:  generation.NativeQuality,
		BroadSample:    generation.BroadSample,
		Generation:     generation.Generation,
		CurrentTarget:  safeTag(generation.CurrentTarget),
		ShortlistCount: len(generation.Candidates),
		Candidates:     adaptiveInputStatuses(generation.Candidates),
	}
	candidateLimit := AdaptiveMaxCandidates
	if generation.NativeQuality {
		candidateLimit = NativeQualityMaxAttempts
	}
	limits := r.Limits(generation.NativeQuality && generation.BroadSample)
	maxCandidates, maxWall := limits.Candidates, limits.Wall()
	if generation.NativeQuality && generation.BroadSample {
		candidateLimit = limits.Attempts
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
		status.Candidates = adaptiveResultStatuses(result.Candidates, candidateLimit)
		emit()
		return result
	}

	if !validAdaptiveGeneration(generation) || len(generation.Candidates) > maxCandidates {
		return finishEarly("failed", AdaptiveReasonUnavailable)
	}
	if r == nil || r.Probe == nil {
		return finishEarly("failed", AdaptiveReasonUnavailable)
	}
	transport := r.Transport
	if transport == nil {
		transport = newFixedMeasurementTransport()
	}

	generationContext, cancelGeneration := context.WithTimeout(parent, maxWall-AdaptiveCleanupReserve)
	defer cancelGeneration()
	guarded, stop, admissionErr := r.Resources.Start(generationContext)
	if admissionErr != nil {
		return finishEarly("failed", resourceReason(admissionErr))
	}
	defer stop()
	generationContext = guarded
	queue := append(append([]AdaptiveCandidateInput(nil), generation.Candidates...), generation.Fallbacks...)
	if len(queue) > limits.Attempts {
		queue = queue[:limits.Attempts]
	}
	for _, input := range queue {
		if generation.NativeQuality && countAdaptiveValid(result.Candidates) >= len(generation.Candidates) {
			break
		}
		if err := parent.Err(); err != nil {
			result.State = "cancelled"
			result.ReasonCode = AdaptiveReasonCancelled
			break
		}
		if generationContext.Err() != nil {
			result.State = "failed"
			if generation.NativeQuality && countAdaptiveValid(result.Candidates) >= 2 {
				result.State = "completed"
			}
			result.ReasonCode = AdaptiveReasonGenerationBudget
			break
		}
		if !canAdmitResourceCandidate(generationContext, result.AggregateBytes, limits) {
			result.State = "failed"
			if generation.NativeQuality && countAdaptiveValid(result.Candidates) >= 2 {
				result.State = "completed"
			}
			result.ReasonCode = AdaptiveReasonGenerationBudget
			break
		}

		candidateContext, cancelCandidate := context.WithTimeout(generationContext, AdaptiveWorkTimeout)
		execution := &adaptiveExecution{transport: transport, limits: limits}
		probeErr := r.Probe.WithTarget(candidateContext, AdaptiveMode, input.Tag, func(probeContext context.Context) error {
			return execution.run(probeContext)
		})
		candidateErr := candidateContext.Err()
		cancelCandidate()
		// Account traffic before any cancellation or cleanup terminal branch.
		result.AggregateBytes += execution.bytes

		if r.Probe.Blocked() || errors.Is(probeErr, ErrProbeCleanup) || errors.Is(probeErr, ErrProbeBlocked) {
			result.State = "cleanup-pending"
			result.ReasonCode = AdaptiveReasonCleanupPending
			status.State = result.State
			status.ReasonCode = result.ReasonCode
			status.CompletedAt = adaptiveNow(r)
			emit()
			break
		}
		if cause := context.Cause(generationContext); errors.Is(cause, resourcepolicy.ErrPressure) || errors.Is(cause, resourcepolicy.ErrTelemetry) {
			result.State, result.ReasonCode = "failed", resourceReason(cause)
			break
		}
		if err := parent.Err(); err != nil {
			result.State = "cancelled"
			result.ReasonCode = AdaptiveReasonCancelled
			break
		}
		if generationContext.Err() != nil && candidateErr == context.DeadlineExceeded {
			result.State = "failed"
			if generation.NativeQuality && countAdaptiveValid(result.Candidates) >= 2 {
				result.State = "completed"
			}
			result.ReasonCode = AdaptiveReasonGenerationBudget
			break
		}

		candidate := AdaptiveCandidateResult{
			Tag:           input.Tag,
			RTTMS:         input.RTTMS,
			HealthPenalty: input.HealthPenalty,
			DownloadBPS:   execution.downloadBPS,
			UploadBPS:     execution.uploadBPS,
			Valid:         probeErr == nil && candidateErr == nil && finitePositive(execution.downloadBPS) && finitePositive(execution.uploadBPS),
			ErrorCode:     classifyAdaptiveCandidateError(probeErr, candidateErr, execution),
		}
		result.Candidates = append(result.Candidates, candidate)
		if generation.NativeQuality {
			result.ShortlistCount = len(result.Candidates)
		}
		status.Candidates = adaptiveResultStatuses(result.Candidates, candidateLimit)
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
		status.Candidates = adaptiveResultStatuses(result.Candidates, candidateLimit)
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
	status.Candidates = adaptiveResultStatuses(result.Candidates, candidateLimit)
	emit()
	return result
}

type adaptiveExecution struct {
	limits      resourcepolicy.Limits
	transport   BandwidthMeasurementTransport
	bytes       int64
	downloadBPS float64
	uploadBPS   float64
}

func (e *adaptiveExecution) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	down, up := e.limits.Download, e.limits.Upload
	if down == nil {
		down, up = adaptiveDownloadStages[:], adaptiveUploadStages[:]
	}
	download, err := e.runDirection(ctx, down, e.transport.Download)
	if err != nil {
		return err
	}
	e.downloadBPS = download
	upload, err := e.runDirection(ctx, up, e.transport.Upload)
	if err != nil {
		return err
	}
	e.uploadBPS = upload
	return nil
}

func resourceReason(err error) string {
	if errors.Is(err, resourcepolicy.ErrExternalBenchmark) {
		return "native-speed-conflict"
	}
	if errors.Is(err, resourcepolicy.ErrPressure) {
		return "resource-pressure"
	}
	if errors.Is(err, resourcepolicy.ErrTelemetry) {
		return "resource-telemetry-unavailable"
	}
	return AdaptiveReasonCancelled
}

func canAdmitResourceCandidate(ctx context.Context, used int64, limits resourcepolicy.Limits) bool {
	var bytes int64
	for _, n := range limits.Download {
		bytes += n
	}
	for _, n := range limits.Upload {
		bytes += n
	}
	deadline, ok := ctx.Deadline()
	return used >= 0 && used <= limits.Bytes-bytes && ok && time.Until(deadline) >= AdaptiveWorkTimeout
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

func adaptiveCanAdmitCandidate(ctx context.Context, aggregateBytes int64, budgets ...int64) bool {
	limit := int64(AdaptiveMaxGenerationBytes)
	if len(budgets) > 0 && budgets[0] == NativeQualityBroadBytes {
		limit = NativeQualityBroadBytes
	}
	if aggregateBytes < 0 || aggregateBytes+AdaptiveMaxDownloadBytes+AdaptiveMaxUploadBytes > limit {
		return false
	}
	deadline, ok := ctx.Deadline()
	return ok && time.Until(deadline) >= AdaptiveMaxWallTime
}

func validAdaptiveGeneration(generation AdaptiveGeneration) bool {
	maxAttempts := NativeQualityMaxAttempts
	if generation.BroadSample {
		if !generation.NativeQuality {
			return false
		}
		maxAttempts = NativeQualityBroadAttempts
	}
	if len(generation.Fallbacks) > 0 && !generation.NativeQuality || len(generation.Candidates)+len(generation.Fallbacks) > maxAttempts {
		return false
	}
	if generation.Generation == 0 || (!validTag(generation.CurrentTarget) && !(generation.NativeQuality && generation.CurrentTarget == "")) || len(generation.Candidates) < 2 {
		return false
	}
	seen := make(map[string]struct{}, len(generation.Candidates))
	foundCurrent := false
	for _, candidate := range append(append([]AdaptiveCandidateInput(nil), generation.Candidates...), generation.Fallbacks...) {
		if !validTag(candidate.Tag) || candidate.RTTMS <= 0 {
			return false
		}
		if _, ok := seen[candidate.Tag]; ok {
			return false
		}
		seen[candidate.Tag] = struct{}{}
		foundCurrent = foundCurrent || candidate.Tag == generation.CurrentTarget
	}
	return foundCurrent || generation.NativeQuality
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

func adaptiveResultStatuses(results []AdaptiveCandidateResult, limits ...int) []AdaptiveCandidateStatus {
	limit := AdaptiveMaxCandidates
	if len(limits) > 0 && (limits[0] == NativeQualityMaxAttempts || limits[0] == NativeQualityBroadAttempts) {
		limit = limits[0]
	}
	result := make([]AdaptiveCandidateStatus, 0, minInt(len(results), limit))
	for index, candidate := range results {
		if index >= limit {
			break
		}
		item := AdaptiveCandidateStatus{Tag: safeTag(candidate.Tag), RTTMS: positiveInt64(candidate.RTTMS), Valid: candidate.Valid}
		if finitePositive(candidate.DownloadBPS) {
			item.DownloadBPS = candidate.DownloadBPS
		}
		if finitePositive(candidate.UploadBPS) && finitePositive(adaptiveHealthPenalty(candidate.HealthPenalty)) {
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
		if candidate.Valid && candidate.RTTMS > 0 && finitePositive(candidate.DownloadBPS) && finitePositive(candidate.UploadBPS) && finitePositive(adaptiveHealthPenalty(candidate.HealthPenalty)) {
			count++
		}
	}
	return count
}

func adaptiveCandidateValid(results []AdaptiveCandidateResult, tag string) bool {
	for _, candidate := range results {
		if candidate.Tag == tag {
			return candidate.Valid && candidate.RTTMS > 0 && finitePositive(candidate.DownloadBPS) && finitePositive(candidate.UploadBPS) && finitePositive(adaptiveHealthPenalty(candidate.HealthPenalty))
		}
	}
	return false
}

func scoreAdaptiveResults(results []AdaptiveCandidateResult, current string) (winner string, winnerScore, currentScore float64, challenger bool) {
	bestRTT := int64(0)
	bestDownload := 0.0
	bestUpload := 0.0
	for _, candidate := range results {
		if !candidate.Valid || candidate.RTTMS <= 0 || !finitePositive(candidate.DownloadBPS) || !finitePositive(candidate.UploadBPS) || !finitePositive(adaptiveHealthPenalty(candidate.HealthPenalty)) {
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
		if !candidate.Valid || candidate.RTTMS <= 0 || !finitePositive(candidate.DownloadBPS) || !finitePositive(candidate.UploadBPS) || !finitePositive(adaptiveHealthPenalty(candidate.HealthPenalty)) {
			continue
		}
		// Dimensionless geometric quality preserves pairwise ratios when the
		// measurement units or another candidate's normalization maxima change.
		// The 0.40 exponent keeps opposing +/-10% noise on all three axes
		// below 10% hysteresis while equal-RTT 2x throughput clears it.
		// Subtract logs before exponentiating: dividing finite extreme rates
		// first could underflow a valid positive component to zero.
		latencyComponent := math.Log(float64(bestRTT)) - math.Log(float64(candidate.RTTMS))
		downloadComponent := math.Log(candidate.DownloadBPS) - math.Log(bestDownload)
		uploadComponent := math.Log(candidate.UploadBPS) - math.Log(bestUpload)
		candidate.Score = math.Exp(0.40*(0.35*latencyComponent+0.45*downloadComponent+0.20*uploadComponent)) / adaptiveHealthPenalty(candidate.HealthPenalty)
		if !finiteNonNegative(candidate.Score) {
			candidate.Valid = false
			candidate.Score = 0
			continue
		}
		if candidate.Tag == current {
			currentScore = candidate.Score
		}
		if candidate.Tag != current {
			if candidate.RTTMS <= AdaptiveMaximumRTTMS {
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
		AdaptiveReasonAdaptiveQuality, "resource-pressure", "resource-telemetry-unavailable", "native-speed-conflict":
		return reason
	default:
		return AdaptiveReasonUnavailable
	}
}

func sanitizeAdaptiveStatus(status AdaptivePerformanceStatus) AdaptivePerformanceStatus {
	candidateLimit := AdaptiveMaxCandidates
	if status.NativeQuality {
		candidateLimit = NativeQualityMaxAttempts
		if status.BroadSample {
			candidateLimit = NativeQualityBroadAttempts
		}
	}
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
	if status.ShortlistCount > candidateLimit {
		status.ShortlistCount = candidateLimit
	}
	if status.ValidCount < 0 {
		status.ValidCount = 0
	}
	if status.ValidCount > candidateLimit {
		status.ValidCount = candidateLimit
	}
	status.ReasonCode = safeAdaptiveReason(status.ReasonCode)
	status.Candidates = adaptiveResultStatusesFromStatus(status.Candidates, candidateLimit)
	return status
}

func adaptiveResultStatusesFromStatus(candidates []AdaptiveCandidateStatus, limits ...int) []AdaptiveCandidateStatus {
	limit := AdaptiveMaxCandidates
	if len(limits) > 0 && (limits[0] == NativeQualityMaxAttempts || limits[0] == NativeQualityBroadAttempts) {
		limit = limits[0]
	}
	result := make([]AdaptiveCandidateStatus, 0, minInt(len(candidates), limit))
	for index, candidate := range candidates {
		if index >= limit {
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

// adaptiveRTTEvidence reads the existing deduplicated RAM window. Failed or
// future observations cannot count toward the initial three usable RTTs.
func adaptiveRTTEvidence(values []sample, cutoff, now time.Time) (int64, int, time.Time) {
	delays := make([]int64, 0, len(values))
	var latest time.Time
	for _, value := range values {
		if !value.alive || value.delay <= 0 || value.at.Before(cutoff) || value.at.After(now) {
			continue
		}
		delays = append(delays, value.delay)
		if value.at.After(latest) {
			latest = value.at
		}
	}
	if len(delays) == 0 {
		return 0, 0, time.Time{}
	}
	sort.Slice(delays, func(i, j int) bool { return delays[i] < delays[j] })
	return delays[len(delays)/2], len(delays), latest
}

// Zero means no retained health window (compatibility fixtures), not failure.
// A populated window contributes failure frequency and median RTT deviation.
func adaptiveHealthPenalty(value float64) float64 {
	if value == 0 {
		return 1
	}
	if !finitePositive(value) || value < 1 {
		return math.Inf(1)
	}
	return value
}

func adaptiveWindowPenalty(values []sample, cutoff, now time.Time, median int64) float64 {
	total, failures := 0, 0
	deviations := make([]int64, 0, len(values))
	for _, value := range values {
		if value.at.Before(cutoff) || value.at.After(now) {
			continue
		}
		total++
		if !value.alive {
			failures++
			continue
		}
		delta := value.delay - median
		if delta < 0 {
			delta = -delta
		}
		deviations = append(deviations, delta)
	}
	if total == 0 || len(deviations) == 0 || median <= 0 {
		return 0
	}
	sort.Slice(deviations, func(i, j int) bool { return deviations[i] < deviations[j] })
	success := float64(total-failures) / float64(total)
	jitter := float64(deviations[len(deviations)/2]) / math.Max(float64(median), 20)
	return (1 + math.Min(jitter, 2)) / (success * success)
}
