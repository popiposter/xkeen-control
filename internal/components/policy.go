package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	ComponentPolicySchemaVersion = 1

	ComponentPolicyModeManual = "manual"
	ComponentPolicyModeNotify = "notify"
	ComponentPolicyModeOff    = "off"

	DefaultComponentPolicyPath           = "/opt/etc/xkeen-control/state/component-policy.json"
	DefaultComponentPolicyCadenceMinutes = 24 * 60
	MinComponentPolicyCadenceMinutes     = 60
	MaxComponentPolicyCadenceMinutes     = 7 * 24 * 60
	MaxComponentPolicyBytes              = 4 << 10

	DefaultComponentSchedulerCycleTimeout = 3 * MaxCheckDuration
	DefaultComponentNotificationTimeout   = 1 * time.Second
)

var (
	ErrInvalidComponentPolicy     = errors.New("component lifecycle policy is invalid")
	ErrComponentPolicyDisabled    = errors.New("component lifecycle policy is disabled")
	ErrComponentPolicyUnavailable = errors.New("component lifecycle policy is unavailable")
	ErrComponentPolicySave        = errors.New("component lifecycle policy could not be saved")
)

// ComponentPolicy is the complete persisted F3 policy. Its shape is also the
// strict HTTP request shape; all fields are required so a partial request can
// never inherit an operator or file default accidentally.
type ComponentPolicy struct {
	SchemaVersion       int    `json:"schemaVersion"`
	Mode                string `json:"mode"`
	CheckCadenceMinutes int    `json:"checkCadenceMinutes"`
}

func (policy *ComponentPolicy) UnmarshalJSON(data []byte) error {
	value, err := decodeComponentPolicy(data)
	if err != nil {
		return err
	}
	*policy = value
	return nil
}

func ValidateComponentPolicy(policy ComponentPolicy) error {
	if policy.SchemaVersion != ComponentPolicySchemaVersion {
		return ErrInvalidComponentPolicy
	}
	switch policy.Mode {
	case ComponentPolicyModeManual, ComponentPolicyModeNotify, ComponentPolicyModeOff:
	default:
		return ErrInvalidComponentPolicy
	}
	if policy.CheckCadenceMinutes < MinComponentPolicyCadenceMinutes || policy.CheckCadenceMinutes > MaxComponentPolicyCadenceMinutes {
		return ErrInvalidComponentPolicy
	}
	return nil
}

func defaultComponentPolicy() ComponentPolicy {
	return ComponentPolicy{
		SchemaVersion:       ComponentPolicySchemaVersion,
		Mode:                ComponentPolicyModeManual,
		CheckCadenceMinutes: DefaultComponentPolicyCadenceMinutes,
	}
}

func offComponentPolicy() ComponentPolicy {
	policy := defaultComponentPolicy()
	policy.Mode = ComponentPolicyModeOff
	return policy
}

func decodeComponentPolicy(data []byte) (ComponentPolicy, error) {
	if err := rejectDuplicatePolicyFields(data); err != nil {
		return ComponentPolicy{}, ErrInvalidComponentPolicy
	}
	type policyWire struct {
		SchemaVersion       *int    `json:"schemaVersion"`
		Mode                *string `json:"mode"`
		CheckCadenceMinutes *int    `json:"checkCadenceMinutes"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire policyWire
	if err := decoder.Decode(&wire); err != nil {
		return ComponentPolicy{}, ErrInvalidComponentPolicy
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ComponentPolicy{}, ErrInvalidComponentPolicy
	}
	if wire.SchemaVersion == nil || wire.Mode == nil || wire.CheckCadenceMinutes == nil {
		return ComponentPolicy{}, ErrInvalidComponentPolicy
	}
	policy := ComponentPolicy{
		SchemaVersion:       *wire.SchemaVersion,
		Mode:                *wire.Mode,
		CheckCadenceMinutes: *wire.CheckCadenceMinutes,
	}
	if err := ValidateComponentPolicy(policy); err != nil {
		return ComponentPolicy{}, err
	}
	return policy, nil
}

func rejectDuplicatePolicyFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return ErrInvalidComponentPolicy
	}
	allowed := map[string]struct{}{
		"schemaVersion":       {},
		"mode":                {},
		"checkCadenceMinutes": {},
	}
	seen := make(map[string]struct{}, len(allowed))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return ErrInvalidComponentPolicy
		}
		if _, ok := allowed[name]; !ok {
			return ErrInvalidComponentPolicy
		}
		if _, ok := seen[name]; ok {
			return ErrInvalidComponentPolicy
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return ErrInvalidComponentPolicy
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrInvalidComponentPolicy
	}
	return nil
}

// ComponentPolicyStatus is the safe GET projection. Invalid persisted input
// is represented by the effective off policy and a bounded reason code; no
// file path or native error crosses this boundary.
type ComponentPolicyStatus struct {
	SchemaVersion       int             `json:"schemaVersion"`
	Mode                string          `json:"mode"`
	CheckCadenceMinutes int             `json:"checkCadenceMinutes"`
	ReasonCode          string          `json:"reasonCode,omitempty"`
	Scheduler           SchedulerStatus `json:"scheduler"`
}

type policyFileState struct {
	policy      ComponentPolicy
	valid       bool
	reasonCode  string
	fingerprint string
}

func readComponentPolicy(path string) policyFileState {
	if path == "" {
		path = DefaultComponentPolicyPath
	}
	if directoryInfo, err := os.Lstat(filepath.Dir(path)); err == nil {
		if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
			return invalidPolicyFileState("policy-unavailable", "directory-unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return invalidPolicyFileState("policy-unavailable", "directory-unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return policyFileState{policy: defaultComponentPolicy(), valid: true, fingerprint: "absent"}
		}
		return invalidPolicyFileState("policy-unavailable", "file-unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return invalidPolicyFileState("policy-symlink", "symlink")
	}
	if !info.Mode().IsRegular() {
		return invalidPolicyFileState("policy-non-regular", "non-regular")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return invalidPolicyFileState("policy-permissions", "permissions")
	}
	if info.Size() > MaxComponentPolicyBytes {
		return invalidPolicyFileState("policy-too-large", "too-large")
	}
	file, err := os.Open(path)
	if err != nil {
		return invalidPolicyFileState("policy-unavailable", "file-unavailable")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Mode()&os.ModeSymlink != 0 || openedInfo.Size() > MaxComponentPolicyBytes {
		return invalidPolicyFileState("policy-invalid", "file-changed")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxComponentPolicyBytes+1))
	if err != nil {
		return invalidPolicyFileState("policy-unavailable", "file-unavailable")
	}
	if len(contents) > MaxComponentPolicyBytes {
		return invalidPolicyFileState("policy-too-large", "too-large")
	}
	digest := sha256.Sum256(contents)
	fingerprint := hex.EncodeToString(digest[:])
	policy, err := decodeComponentPolicy(contents)
	if err != nil {
		return invalidPolicyFileState("policy-invalid", fingerprint)
	}
	return policyFileState{policy: policy, valid: true, fingerprint: fingerprint}
}

func invalidPolicyFileState(reasonCode, fingerprint string) policyFileState {
	return policyFileState{policy: offComponentPolicy(), reasonCode: reasonCode, fingerprint: fingerprint}
}

// PolicyManager owns the fixed persisted policy and the in-memory epoch used
// to invalidate update previews across policy changes. It rereads the file on
// each admission/status call so an invalid manual edit fails closed without
// being normalized or rewritten.
type PolicyManager struct {
	path string

	mu               sync.Mutex
	fingerprint      string
	fingerprintKnown bool
	epoch            uint64
	changes          chan struct{}
	scheduler        *CheckScheduler
}

func NewPolicyManager() *PolicyManager {
	return newPolicyManager(DefaultComponentPolicyPath)
}

// newPolicyManager is an in-package test seam. Production callers must use
// NewPolicyManager so the persisted policy path remains product-fixed.
func newPolicyManager(path string) *PolicyManager {
	if path == "" {
		path = DefaultComponentPolicyPath
	}
	return &PolicyManager{path: path, changes: make(chan struct{}, 1)}
}

func (manager *PolicyManager) snapshotLocked() policyEvaluation {
	if manager == nil {
		return policyEvaluation{policy: offComponentPolicy(), reasonCode: "policy-unavailable"}
	}
	loaded := readComponentPolicy(manager.path)
	if !manager.fingerprintKnown || manager.fingerprint != loaded.fingerprint {
		manager.epoch++
		manager.fingerprint = loaded.fingerprint
		manager.fingerprintKnown = true
	}
	return policyEvaluation{
		policy:     loaded.policy,
		valid:      loaded.valid,
		reasonCode: loaded.reasonCode,
		epoch:      manager.epoch,
	}
}

func (manager *PolicyManager) snapshot() policyEvaluation {
	if manager == nil {
		return policyEvaluation{policy: offComponentPolicy(), reasonCode: "policy-unavailable"}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.snapshotLocked()
}

func (manager *PolicyManager) Status() ComponentPolicyStatus {
	if manager == nil {
		return ComponentPolicyStatus{
			SchemaVersion:       ComponentPolicySchemaVersion,
			Mode:                ComponentPolicyModeOff,
			CheckCadenceMinutes: DefaultComponentPolicyCadenceMinutes,
			ReasonCode:          "policy-unavailable",
			Scheduler:           SchedulerStatus{State: "unavailable", NotificationState: "idle"},
		}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.statusLocked(manager.snapshotLocked())
}

func (manager *PolicyManager) statusLocked(evaluation policyEvaluation) ComponentPolicyStatus {
	status := ComponentPolicyStatus{
		SchemaVersion:       evaluation.policy.SchemaVersion,
		Mode:                evaluation.policy.Mode,
		CheckCadenceMinutes: evaluation.policy.CheckCadenceMinutes,
		ReasonCode:          evaluation.reasonCode,
	}
	if manager.scheduler != nil {
		status.Scheduler = manager.scheduler.statusFor(evaluation)
	} else {
		status.Scheduler = SchedulerStatus{Enabled: evaluation.policy.Mode == ComponentPolicyModeNotify, State: policySchedulerState(evaluation), NotificationState: "idle"}
	}
	return status
}

func policySchedulerState(evaluation policyEvaluation) string {
	if evaluation.policy.Mode == ComponentPolicyModeNotify {
		return "waiting"
	}
	return "disabled"
}

func policySkipReason(evaluation policyEvaluation) string {
	if evaluation.reasonCode != "" {
		return evaluation.reasonCode
	}
	if evaluation.policy.Mode == ComponentPolicyModeOff {
		return "policy-disabled"
	}
	return ""
}

func (manager *PolicyManager) SetPolicy(policy ComponentPolicy) (ComponentPolicyStatus, error) {
	if manager == nil {
		return ComponentPolicyStatus{}, ErrComponentPolicyUnavailable
	}
	if err := ValidateComponentPolicy(policy); err != nil {
		return ComponentPolicyStatus{}, err
	}
	manager.mu.Lock()
	current := manager.snapshotLocked()
	if current.valid && current.policy == policy {
		status := manager.statusLocked(current)
		manager.mu.Unlock()
		return status, nil
	}
	if err := writeComponentPolicyAtomic(manager.path, policy); err != nil {
		manager.mu.Unlock()
		return ComponentPolicyStatus{}, ErrComponentPolicySave
	}
	updated := manager.snapshotLocked()
	if !updated.valid || updated.policy != policy {
		manager.mu.Unlock()
		return ComponentPolicyStatus{}, ErrComponentPolicySave
	}
	status := manager.statusLocked(updated)
	manager.mu.Unlock()
	select {
	case manager.changes <- struct{}{}:
	default:
	}
	return status, nil
}

func (manager *PolicyManager) SetScheduler(scheduler *CheckScheduler) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	manager.scheduler = scheduler
	manager.mu.Unlock()
}

func (manager *PolicyManager) Changes() <-chan struct{} {
	if manager == nil {
		return nil
	}
	return manager.changes
}

// AdmitUpdate is called below the HTTP/UI layer before a fresh update Preview
// is resolved. The returned epoch binds the one-shot update token to the
// policy that admitted it.
func (manager *PolicyManager) AdmitUpdate() (uint64, error) {
	evaluation := manager.snapshot()
	if evaluation.policy.Mode == ComponentPolicyModeOff {
		return 0, ErrComponentPolicyDisabled
	}
	return evaluation.epoch, nil
}

func (manager *PolicyManager) AllowUpdate(epoch uint64) bool {
	evaluation := manager.snapshot()
	return evaluation.policy.Mode != ComponentPolicyModeOff && evaluation.epoch == epoch
}

func (manager *PolicyManager) allowScheduled(epoch uint64) bool {
	evaluation := manager.snapshot()
	return evaluation.policy.Mode == ComponentPolicyModeNotify && evaluation.epoch == epoch
}

type policyEvaluation struct {
	policy     ComponentPolicy
	valid      bool
	reasonCode string
	epoch      uint64
}

type UpdatePolicyGate interface {
	AdmitUpdate() (uint64, error)
	AllowUpdate(uint64) bool
}

// PolicyCheckService applies the discovery gate below the HTTP/UI layer. A
// caller cannot bypass an off policy by invoking the existing CheckService
// directly through a presentation route.
type PolicyCheckService struct {
	delegate CheckService
	policy   *PolicyManager
}

func NewPolicyCheckService(delegate CheckService, policy *PolicyManager) *PolicyCheckService {
	return &PolicyCheckService{delegate: delegate, policy: policy}
}

func (service *PolicyCheckService) Check(ctx context.Context, request CheckRequest) (CheckResult, error) {
	if service == nil || service.delegate == nil {
		return CheckResult{}, ErrCheckUnavailable
	}
	if service.policy != nil && service.policy.snapshot().policy.Mode == ComponentPolicyModeOff {
		return CheckResult{}, ErrComponentPolicyDisabled
	}
	return service.delegate.Check(ctx, request)
}

type LifecycleProjection struct {
	Maintenance bool
	Applying    bool
}

type NotificationEvent struct {
	Component         ComponentKind
	Channel           string
	CandidateIdentity string
	InstalledState    string
	CheckedAt         time.Time
	ReasonCode        string
}

type NotificationHook interface {
	Notify(context.Context, NotificationEvent) error
}

type NotificationHookFunc func(context.Context, NotificationEvent) error

func (hook NotificationHookFunc) Notify(ctx context.Context, event NotificationEvent) error {
	if hook == nil {
		return nil
	}
	return hook(ctx, event)
}

type ScheduledCheckStatus struct {
	State             string     `json:"state"`
	CheckedAt         *time.Time `json:"checkedAt,omitempty"`
	CandidateIdentity string     `json:"candidateIdentity,omitempty"`
	InstalledState    string     `json:"installedState"`
	Eligible          bool       `json:"eligible"`
	ReasonCode        string     `json:"reasonCode,omitempty"`
}

type SchedulerStatus struct {
	Enabled           bool                                   `json:"enabled"`
	State             string                                 `json:"state"`
	LastCycleAt       *time.Time                             `json:"lastCycleAt,omitempty"`
	NextDueAt         *time.Time                             `json:"nextDueAt,omitempty"`
	LastSkipReason    string                                 `json:"lastSkipReason,omitempty"`
	NotificationState string                                 `json:"notificationState"`
	Results           map[ComponentKind]ScheduledCheckStatus `json:"results,omitempty"`
}

type CheckSchedulerConfig struct {
	Policy              *PolicyManager
	Checks              CheckService
	Lifecycle           func() (LifecycleProjection, bool)
	Notification        NotificationHook
	Now                 func() time.Time
	CycleTimeout        time.Duration
	NotificationTimeout time.Duration
}

type CheckScheduler struct {
	policy              *PolicyManager
	checks              CheckService
	lifecycle           func() (LifecycleProjection, bool)
	notification        NotificationHook
	now                 func() time.Time
	cycleTimeout        time.Duration
	notificationTimeout time.Duration

	mu          sync.Mutex
	started     bool
	cancel      context.CancelFunc
	done        chan struct{}
	policyEpoch uint64
	nextDue     time.Time
	status      SchedulerStatus
	notified    map[ComponentKind]string
}

func NewCheckScheduler(config CheckSchedulerConfig) *CheckScheduler {
	cycleTimeout := config.CycleTimeout
	if cycleTimeout <= 0 || cycleTimeout > DefaultComponentSchedulerCycleTimeout {
		cycleTimeout = DefaultComponentSchedulerCycleTimeout
	}
	notificationTimeout := config.NotificationTimeout
	if notificationTimeout <= 0 || notificationTimeout > DefaultComponentNotificationTimeout {
		notificationTimeout = DefaultComponentNotificationTimeout
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &CheckScheduler{
		policy:              config.Policy,
		checks:              config.Checks,
		lifecycle:           config.Lifecycle,
		notification:        config.Notification,
		now:                 now,
		cycleTimeout:        cycleTimeout,
		notificationTimeout: notificationTimeout,
		status:              SchedulerStatus{State: "disabled", NotificationState: "idle"},
		notified:            make(map[ComponentKind]string, len(componentCheckTuples)),
	}
}

func (scheduler *CheckScheduler) Start(parent context.Context) {
	if scheduler == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	evaluation := scheduler.policy.snapshot()
	now := scheduler.clock()
	scheduler.mu.Lock()
	if scheduler.started {
		scheduler.mu.Unlock()
		cancel()
		return
	}
	scheduler.started = true
	scheduler.cancel = cancel
	scheduler.done = make(chan struct{})
	scheduler.applyPolicyLocked(evaluation, now)
	done := scheduler.done
	scheduler.mu.Unlock()
	go func() {
		defer close(done)
		scheduler.loop(ctx)
	}()
}

func (scheduler *CheckScheduler) Stop() {
	if scheduler == nil {
		return
	}
	scheduler.mu.Lock()
	cancel, done := scheduler.cancel, scheduler.done
	scheduler.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (scheduler *CheckScheduler) Status() SchedulerStatus {
	if scheduler == nil {
		return SchedulerStatus{State: "unavailable", NotificationState: "idle"}
	}
	evaluation := scheduler.policy.snapshot()
	return scheduler.statusFor(evaluation)
}

func (scheduler *CheckScheduler) statusFor(evaluation policyEvaluation) SchedulerStatus {
	if scheduler == nil {
		return SchedulerStatus{State: "unavailable", NotificationState: "idle"}
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	status := cloneSchedulerStatus(scheduler.status)
	if evaluation.policy.Mode != ComponentPolicyModeNotify {
		status.Enabled = false
		status.State = "disabled"
		status.NextDueAt = nil
		status.LastSkipReason = policySkipReason(evaluation)
		return status
	}
	status.Enabled = true
	if scheduler.policyEpoch != evaluation.epoch {
		status.State = "waiting"
		due := scheduler.clock().Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
		status.NextDueAt = &due
	} else if status.State == "disabled" || status.State == "unavailable" || status.State == "" {
		status.State = "waiting"
	}
	if status.NextDueAt == nil && status.State != "running" {
		due := scheduler.clock().Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
		status.NextDueAt = &due
	}
	if status.State != "skipped" {
		status.LastSkipReason = ""
	}
	return status
}

func (scheduler *CheckScheduler) loop(ctx context.Context) {
	if scheduler.policy == nil {
		<-ctx.Done()
		return
	}
	changes := scheduler.policy.Changes()
	for {
		evaluation := scheduler.policy.snapshot()
		if evaluation.policy.Mode != ComponentPolicyModeNotify {
			scheduler.setPolicy(evaluation)
			select {
			case <-ctx.Done():
				return
			case <-changes:
				continue
			}
		}
		scheduler.setPolicy(evaluation)

		now := scheduler.clock()
		scheduler.mu.Lock()
		next := scheduler.nextDue
		if next.IsZero() {
			next = now.Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
			scheduler.nextDue = next
			scheduler.status.Enabled = true
			scheduler.status.State = "waiting"
			scheduler.status.NextDueAt = timePointer(next)
		}
		scheduler.mu.Unlock()
		wait := next.Sub(now)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return
		case <-changes:
			stopTimer(timer)
			continue
		case <-timer.C:
		}

		evaluation = scheduler.policy.snapshot()
		if evaluation.policy.Mode != ComponentPolicyModeNotify || evaluation.epoch != scheduler.currentEpoch() {
			continue
		}
		scheduler.runCycle(ctx, evaluation.epoch)
	}
}

func (scheduler *CheckScheduler) currentEpoch() uint64 {
	if scheduler.policy == nil {
		return 0
	}
	return scheduler.policy.snapshot().epoch
}

func (scheduler *CheckScheduler) setPolicy(evaluation policyEvaluation) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.applyPolicyLocked(evaluation, scheduler.clock())
}

func (scheduler *CheckScheduler) applyPolicyLocked(evaluation policyEvaluation, now time.Time) {
	if evaluation.policy.Mode != ComponentPolicyModeNotify {
		scheduler.policyEpoch = evaluation.epoch
		scheduler.nextDue = time.Time{}
		scheduler.status.Enabled = false
		scheduler.status.State = "disabled"
		scheduler.status.NextDueAt = nil
		scheduler.status.LastSkipReason = policySkipReason(evaluation)
		return
	}
	if scheduler.policyEpoch != evaluation.epoch {
		scheduler.policyEpoch = evaluation.epoch
		scheduler.nextDue = now.Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
		scheduler.status.State = "waiting"
	} else if scheduler.status.State != "running" {
		scheduler.status.State = "waiting"
	}
	scheduler.status.Enabled = true
	scheduler.status.LastSkipReason = ""
	if scheduler.nextDue.IsZero() {
		scheduler.nextDue = now.Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
	}
	scheduler.status.NextDueAt = timePointer(scheduler.nextDue)
}

func (scheduler *CheckScheduler) runCycle(parent context.Context, epoch uint64) {
	if scheduler == nil || scheduler.policy == nil {
		return
	}
	evaluation := scheduler.policy.snapshot()
	if evaluation.policy.Mode != ComponentPolicyModeNotify || evaluation.epoch != epoch {
		scheduler.setPolicy(evaluation)
		return
	}
	scheduler.setPolicy(evaluation)
	now := scheduler.clock()
	if scheduler.lifecycle == nil {
		scheduler.skipCycle("lifecycle-unavailable", now)
		return
	}
	lifecycle, available := scheduler.lifecycle()
	if !available {
		scheduler.skipCycle("lifecycle-unavailable", now)
		return
	}
	if lifecycle.Maintenance {
		scheduler.skipCycle("maintenance", now)
		return
	}
	if lifecycle.Applying {
		scheduler.skipCycle("applying", now)
		return
	}
	if scheduler.checks == nil {
		scheduler.skipCycle("check-unavailable", now)
		return
	}

	scheduler.mu.Lock()
	scheduler.status.Enabled = true
	scheduler.status.State = "running"
	scheduler.status.NextDueAt = nil
	scheduler.status.LastSkipReason = ""
	scheduler.mu.Unlock()

	if parent == nil {
		parent = context.Background()
	}
	cycleContext, cancel := context.WithTimeout(parent, scheduler.cycleTimeout)
	defer cancel()
	results := make(map[ComponentKind]ScheduledCheckStatus, len(componentCheckTuples))
	partial := false
	for index, request := range componentCheckTuples {
		if !scheduler.policy.allowScheduled(epoch) {
			partial = true
			for _, remaining := range componentCheckTuples[index:] {
				results[remaining.Component] = ScheduledCheckStatus{State: "skipped", InstalledState: "unknown", ReasonCode: "policy-disabled"}
			}
			break
		}
		result, err := scheduler.checks.Check(cycleContext, request)
		if err != nil {
			partial = true
			results[request.Component] = ScheduledCheckStatus{State: "failed", InstalledState: "unknown", ReasonCode: schedulerErrorCode(err)}
			continue
		}
		results[request.Component] = scheduledCheckStatus(result)
		scheduler.notifyCandidate(cycleContext, request, result)
	}

	completedAt := scheduler.clock()
	evaluation = scheduler.policy.snapshot()
	scheduler.mu.Lock()
	scheduler.status.LastCycleAt = timePointer(completedAt)
	scheduler.status.Results = results
	if evaluation.policy.Mode != ComponentPolicyModeNotify || evaluation.epoch != epoch {
		scheduler.nextDue = time.Time{}
		scheduler.status.Enabled = false
		scheduler.status.State = "disabled"
		scheduler.status.NextDueAt = nil
		scheduler.status.LastSkipReason = policySkipReason(evaluation)
	} else {
		next := completedAt.Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
		scheduler.nextDue = next
		scheduler.status.Enabled = true
		if partial {
			scheduler.status.State = "partial"
		} else {
			scheduler.status.State = "completed"
		}
		scheduler.status.NextDueAt = timePointer(next)
		scheduler.status.LastSkipReason = ""
	}
	scheduler.mu.Unlock()
}

func (scheduler *CheckScheduler) skipCycle(reason string, now time.Time) {
	evaluation := scheduler.policy.snapshot()
	if evaluation.policy.Mode != ComponentPolicyModeNotify {
		scheduler.setPolicy(evaluation)
		return
	}
	next := now.Add(time.Duration(evaluation.policy.CheckCadenceMinutes) * time.Minute)
	scheduler.mu.Lock()
	scheduler.status.Enabled = true
	scheduler.status.State = "skipped"
	scheduler.status.LastCycleAt = timePointer(now)
	scheduler.status.NextDueAt = timePointer(next)
	scheduler.status.LastSkipReason = reason
	scheduler.nextDue = next
	scheduler.mu.Unlock()
}

func scheduledCheckStatus(result CheckResult) ScheduledCheckStatus {
	state := safeInstalledState(result.InstalledState)
	identity := safeCandidateIdentity(result)
	checkedAt := result.CheckedAt.UTC()
	var checkedAtPointer *time.Time
	if !checkedAt.IsZero() {
		checkedAtPointer = &checkedAt
	}
	return ScheduledCheckStatus{
		State:             "checked",
		CheckedAt:         checkedAtPointer,
		CandidateIdentity: identity,
		InstalledState:    state,
		Eligible:          result.Eligible,
		ReasonCode:        safeSchedulerReason(result.ReasonCode),
	}
}

func safeInstalledState(value string) string {
	switch value {
	case "current", "update-available", "candidate-older", "changed", "not-installed", "unknown":
		return value
	default:
		return "unknown"
	}
}

func safeCandidateIdentity(result CheckResult) string {
	if result.Candidate == nil {
		if result.Component != KindGeodata {
			return ""
		}
		return safeGeodataCandidateIdentity(result.Items)
	}

	identity := scheduledCandidateIdentity{
		Component:       result.Component,
		Version:         result.Candidate.Version,
		Generation:      result.Candidate.Generation,
		AssetName:       result.Candidate.AssetName,
		SizeBytes:       result.Candidate.SizeBytes,
		SHA256:          result.Candidate.SHA256,
		BuildCommitSHA:  result.Candidate.BuildCommitSHA,
		SourceCommitSHA: result.Candidate.SourceCommitSHA,
		BlobSHA:         result.Candidate.BlobSHA,
	}
	switch result.Component {
	case KindXray:
		version, ok := parseStrictVersion(identity.Version)
		if !ok || identity.AssetName != xrayCandidateAsset || identity.SizeBytes <= 0 || identity.SizeBytes > MaxCandidateAssetBytes || !isHexSHA256(identity.SHA256) {
			return ""
		}
		identity.Version = version.String()
		identity.Generation = ""
		identity.BuildCommitSHA = ""
		identity.SourceCommitSHA = ""
		identity.BlobSHA = ""
	case KindXKeen:
		version, ok := parseStrictVersion(identity.Version)
		if !ok || identity.AssetName != xkeenDevArtifactPath || identity.SizeBytes <= 0 || identity.SizeBytes > MaxXKeenDevArtifactBytes || !isHexSHA256(identity.Generation) || !isHexSHA256(identity.SHA256) || !isGitSHA1(identity.BuildCommitSHA) || !isGitSHA1(identity.SourceCommitSHA) || !isGitSHA1(identity.BlobSHA) {
			return ""
		}
		identity.Version = version.String()
		identity.Generation = strings.ToLower(identity.Generation)
		identity.SHA256 = strings.ToLower(identity.SHA256)
		identity.BuildCommitSHA = strings.ToLower(identity.BuildCommitSHA)
		identity.SourceCommitSHA = strings.ToLower(identity.SourceCommitSHA)
		identity.BlobSHA = strings.ToLower(identity.BlobSHA)
	default:
		return ""
	}
	return hashScheduledCandidateIdentity(identity)
}

type scheduledCandidateIdentity struct {
	Component       ComponentKind `json:"component"`
	Version         string        `json:"version"`
	Generation      string        `json:"generation,omitempty"`
	AssetName       string        `json:"assetName"`
	SizeBytes       int64         `json:"sizeBytes"`
	SHA256          string        `json:"sha256"`
	BuildCommitSHA  string        `json:"buildCommitSha,omitempty"`
	SourceCommitSHA string        `json:"sourceCommitSha,omitempty"`
	BlobSHA         string        `json:"blobSha,omitempty"`
}

func safeGeodataCandidateIdentity(items []CheckItem) string {
	if len(items) != len(productGeodataCatalog) {
		return ""
	}
	identities := make([]GeodataReleaseIdentity, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		item := items[index]
		if item.ID != entry.ID || item.SourceID != "github/"+entry.Repository || item.AssetName != entry.Asset || !item.Eligible || len(item.Generation) == 0 || len(item.Generation) > MaxMetadataStringBytes || !metadataGenerationPattern.MatchString(item.Generation) || item.SizeBytes <= 0 || item.SizeBytes > MaxCandidateAssetBytes || !isHexSHA256(item.SHA256) {
			return ""
		}
		identities[index] = GeodataReleaseIdentity{
			ID:         entry.ID,
			Repository: entry.Repository,
			Tag:        item.Generation,
			AssetName:  entry.Asset,
			ActiveName: entry.Name,
			SizeBytes:  item.SizeBytes,
			SHA256:     strings.ToLower(item.SHA256),
		}
	}
	return geodataIdentityGeneration(identities)
}

func hashScheduledCandidateIdentity(identity scheduledCandidateIdentity) string {
	encoded, err := json.Marshal(identity)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func safeSchedulerReason(value string) string {
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return ""
		}
	}
	return value
}

func schedulerErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrComponentPolicyDisabled):
		return "policy-disabled"
	case errors.Is(err, ErrCheckTimeout):
		return "check-timeout"
	case errors.Is(err, ErrCheckBusy):
		return "check-busy"
	case errors.Is(err, ErrUpstreamRejected):
		return "upstream-rejected"
	case errors.Is(err, ErrCheckUnavailable):
		return "check-unavailable"
	default:
		return "check-unavailable"
	}
}

func (scheduler *CheckScheduler) notifyCandidate(ctx context.Context, request CheckRequest, result CheckResult) {
	identity := safeCandidateIdentity(result)
	installedState := safeInstalledState(result.InstalledState)
	if !result.Eligible || identity == "" || !actionableInstalledState(installedState) {
		return
	}
	fingerprint := string(request.Component) + "\x00" + request.Channel + "\x00" + identity + "\x00" + installedState
	scheduler.mu.Lock()
	if scheduler.notified[request.Component] == fingerprint {
		scheduler.mu.Unlock()
		return
	}
	// Mark before invoking the hook so a failed hook cannot cause an immediate
	// retry loop. A later process restart intentionally starts a new RAM epoch.
	scheduler.notified[request.Component] = fingerprint
	scheduler.mu.Unlock()

	eventTime := result.CheckedAt.UTC()
	if eventTime.IsZero() {
		eventTime = scheduler.clock()
	}
	event := NotificationEvent{
		Component:         request.Component,
		Channel:           request.Channel,
		CandidateIdentity: identity,
		InstalledState:    installedState,
		CheckedAt:         eventTime,
		ReasonCode:        safeSchedulerReason(result.ReasonCode),
	}
	if scheduler.notification == nil {
		return
	}
	notificationContext, cancel := context.WithTimeout(ctx, scheduler.notificationTimeout)
	notificationResult := make(chan error, 1)
	go func() {
		notificationResult <- scheduler.notification.Notify(notificationContext, event)
	}()
	var err error
	select {
	case err = <-notificationResult:
	case <-notificationContext.Done():
		err = notificationContext.Err()
	}
	cancel()
	scheduler.mu.Lock()
	if err != nil {
		scheduler.status.NotificationState = "failed"
	} else {
		scheduler.status.NotificationState = "notified"
	}
	scheduler.mu.Unlock()
}

func (scheduler *CheckScheduler) clock() time.Time {
	if scheduler == nil || scheduler.now == nil {
		return time.Now().UTC()
	}
	return scheduler.now().UTC()
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func stopTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func actionableInstalledState(value string) bool {
	switch value {
	case "update-available", "changed", "not-installed":
		return true
	default:
		return false
	}
}

func cloneSchedulerStatus(status SchedulerStatus) SchedulerStatus {
	status.Results = cloneScheduledResults(status.Results)
	if status.LastCycleAt != nil {
		value := *status.LastCycleAt
		status.LastCycleAt = &value
	}
	if status.NextDueAt != nil {
		value := *status.NextDueAt
		status.NextDueAt = &value
	}
	return status
}

func cloneScheduledResults(results map[ComponentKind]ScheduledCheckStatus) map[ComponentKind]ScheduledCheckStatus {
	if results == nil {
		return nil
	}
	clone := make(map[ComponentKind]ScheduledCheckStatus, len(results))
	for kind, result := range results {
		copied := result
		if result.CheckedAt != nil {
			value := *result.CheckedAt
			copied.CheckedAt = &value
		}
		clone[kind] = copied
	}
	return clone
}

func writeComponentPolicyAtomic(path string, policy ComponentPolicy) error {
	if err := ValidateComponentPolicy(policy); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return ErrComponentPolicySave
	}
	contents = append(contents, '\n')
	directory := filepath.Dir(path)
	if err := ensureComponentPolicyDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".xkeen-component-policy-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ensureComponentPolicyDirectory(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrComponentPolicySave
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrComponentPolicySave
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return ErrComponentPolicySave
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrComponentPolicySave
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o700); err != nil {
			return ErrComponentPolicySave
		}
	}
	return nil
}
