package components

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type policyCheckStub struct {
	mu        sync.Mutex
	requests  []CheckRequest
	result    CheckResult
	resultFor func(CheckRequest) CheckResult
	err       error
}

type scheduledCheckFunc func(context.Context, CheckRequest) (CheckResult, error)

func (check scheduledCheckFunc) Check(ctx context.Context, request CheckRequest) (CheckResult, error) {
	return check(ctx, request)
}

func (stub *policyCheckStub) Check(_ context.Context, request CheckRequest) (CheckResult, error) {
	stub.mu.Lock()
	stub.requests = append(stub.requests, request)
	result, err := stub.result, stub.err
	if stub.resultFor != nil {
		result = stub.resultFor(request)
	}
	stub.mu.Unlock()
	if err != nil {
		return CheckResult{}, err
	}
	result.Component = request.Component
	result.Channel = request.Channel
	return result, nil
}

func (stub *policyCheckStub) Requests() []CheckRequest {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]CheckRequest(nil), stub.requests...)
}

func writePrivatePolicy(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func validPolicyScheduledResult(request CheckRequest, checkedAt time.Time, installedState string) CheckResult {
	result := CheckResult{
		SchemaVersion:  CheckSchemaVersion,
		Component:      request.Component,
		Channel:        request.Channel,
		CheckedAt:      checkedAt,
		InstalledState: installedState,
		Eligible:       true,
	}
	switch request.Component {
	case KindXray:
		result.Candidate = &CheckCandidate{
			Version:   "2.0.0",
			AssetName: xrayCandidateAsset,
			SizeBytes: 1,
			SHA256:    strings.Repeat("a", 64),
		}
	case KindXKeen:
		result.Candidate = &CheckCandidate{
			Version:         "2.0.1",
			Generation:      strings.Repeat("b", 64),
			AssetName:       xkeenDevArtifactPath,
			SizeBytes:       1,
			SHA256:          strings.Repeat("a", 64),
			BuildCommitSHA:  strings.Repeat("c", 40),
			SourceCommitSHA: strings.Repeat("d", 40),
			BlobSHA:         strings.Repeat("e", 40),
		}
	case KindGeodata:
		result.Items = make([]CheckItem, len(productGeodataCatalog))
		for index, entry := range productGeodataCatalog {
			result.Items[index] = CheckItem{
				ID:             entry.ID,
				SourceID:       "github/" + entry.Repository,
				Generation:     "2026-09-03",
				AssetName:      entry.Asset,
				SizeBytes:      int64(index + 1),
				SHA256:         strings.Repeat(string(rune('a'+index)), 64),
				InstalledState: "current",
				Eligible:       true,
			}
		}
	}
	return result
}

func TestComponentPolicyDefaultsAndFailClosedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "component-policy.json")
	manager := newPolicyManager(path)

	status := manager.Status()
	if status.SchemaVersion != ComponentPolicySchemaVersion || status.Mode != ComponentPolicyModeManual || status.CheckCadenceMinutes != DefaultComponentPolicyCadenceMinutes || status.ReasonCode != "" {
		t.Fatalf("absent policy status = %+v", status)
	}
	if epoch, err := manager.AdmitUpdate(); err != nil || !manager.AllowUpdate(epoch) {
		t.Fatalf("absent policy update admission epoch=%d err=%v", epoch, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	invalidCases := []struct {
		name   string
		body   string
		reason string
	}{
		{name: "unknown field", body: `{"schemaVersion":1,"mode":"manual","checkCadenceMinutes":1440,"url":"https://secret.example"}`, reason: "policy-invalid"},
		{name: "duplicate field", body: `{"schemaVersion":1,"mode":"manual","mode":"notify","checkCadenceMinutes":1440}`, reason: "policy-invalid"},
		{name: "trailing value", body: `{"schemaVersion":1,"mode":"manual","checkCadenceMinutes":1440}{}`, reason: "policy-invalid"},
		{name: "missing field", body: `{"schemaVersion":1,"mode":"manual"}`, reason: "policy-invalid"},
		{name: "invalid mode", body: `{"schemaVersion":1,"mode":"automatic","checkCadenceMinutes":1440}`, reason: "policy-invalid"},
		{name: "short cadence", body: `{"schemaVersion":1,"mode":"manual","checkCadenceMinutes":59}`, reason: "policy-invalid"},
	}
	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			writePrivatePolicy(t, path, testCase.body, 0o600)
			status := manager.Status()
			if status.Mode != ComponentPolicyModeOff || status.ReasonCode != testCase.reason || status.Scheduler.State != "disabled" {
				t.Fatalf("invalid policy status = %+v", status)
			}
			if _, err := manager.AdmitUpdate(); !errors.Is(err, ErrComponentPolicyDisabled) {
				t.Fatalf("invalid policy admission error = %v", err)
			}
		})
	}

	writePrivatePolicy(t, path, strings.Repeat("x", MaxComponentPolicyBytes+1), 0o600)
	if status := manager.Status(); status.Mode != ComponentPolicyModeOff || status.ReasonCode != "policy-too-large" {
		t.Fatalf("oversized policy status = %+v", status)
	}

	if runtime.GOOS != "windows" {
		writePrivatePolicy(t, path, `{"schemaVersion":1,"mode":"manual","checkCadenceMinutes":1440}`, 0o644)
		if status := manager.Status(); status.Mode != ComponentPolicyModeOff || status.ReasonCode != "policy-permissions" {
			t.Fatalf("permission-unsafe policy status = %+v", status)
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.Mode != ComponentPolicyModeOff || status.ReasonCode != "policy-non-regular" {
		t.Fatalf("directory policy status = %+v", status)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "target.json")
	link := filepath.Join(t.TempDir(), "component-policy.json")
	writePrivatePolicy(t, target, `{"schemaVersion":1,"mode":"manual","checkCadenceMinutes":1440}`, 0o600)
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if status := newPolicyManager(link).Status(); status.Mode != ComponentPolicyModeOff || status.ReasonCode != "policy-symlink" {
		t.Fatalf("symlink policy status = %+v", status)
	}
}

func TestComponentPolicySetIsAtomicPrivateAndStrict(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "component-policy.json")
	manager := newPolicyManager(path)
	notify := ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}
	status, err := manager.SetPolicy(notify)
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != ComponentPolicyModeNotify || status.CheckCadenceMinutes != 60 {
		t.Fatalf("saved policy status = %+v", status)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ComponentPolicy
	if err := json.Unmarshal(contents, &decoded); err != nil || decoded != notify {
		t.Fatalf("saved policy = %+v err=%v contents=%s", decoded, err, contents)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("policy mode = %o", info.Mode().Perm())
		}
		directory, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if directory.Mode().Perm() != 0o700 {
			t.Fatalf("policy directory mode = %o", directory.Mode().Perm())
		}
	}
	if strings.Contains(string(contents), "https://") || strings.Contains(string(contents), "secret") {
		t.Fatalf("policy contains unexpected detail: %s", contents)
	}

	before := string(contents)
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: "automatic", CheckCadenceMinutes: 60}); !errors.Is(err, ErrInvalidComponentPolicy) {
		t.Fatalf("invalid set error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("invalid set changed policy: before=%s after=%s", before, after)
	}
}

func TestComponentPolicyGatesChecksAndInvalidatesUpdatePreviews(t *testing.T) {
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	checks := &policyCheckStub{result: CheckResult{SchemaVersion: CheckSchemaVersion, CheckedAt: time.Now().UTC(), InstalledState: "current"}}
	policyChecks := NewPolicyCheckService(checks, manager)
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeOff, CheckCadenceMinutes: 1440}); err != nil {
		t.Fatal(err)
	}
	if _, err := policyChecks.Check(context.Background(), componentCheckTuples[0]); !errors.Is(err, ErrComponentPolicyDisabled) || len(checks.Requests()) != 0 {
		t.Fatalf("off check err=%v requests=%v", err, checks.Requests())
	}
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeManual, CheckCadenceMinutes: 1440}); err != nil {
		t.Fatal(err)
	}
	if _, err := policyChecks.Check(context.Background(), componentCheckTuples[0]); err != nil || len(checks.Requests()) != 1 {
		t.Fatalf("manual check err=%v requests=%v", err, checks.Requests())
	}

	backend := &f1XrayBackend{previous: XrayPreviousGeneration{Generation: strings.Repeat("b", 64), Version: "1.0.0", SizeBytes: 1, SHA256: strings.Repeat("b", 64), Mode: 0o755}}
	service := NewMutationService(MutationConfig{
		XrayResolver: &f1XrayResolver{identity: f1XrayIdentity()},
		Xray:         backend,
		Policy:       manager,
	})
	preview, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindXray, Operation: MutationOperationUpdate, Channel: MutationChannelStable})
	if err != nil {
		t.Fatalf("manual update preview = %v", err)
	}
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeOff, CheckCadenceMinutes: 1440}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.PreviewToken); !errors.Is(err, ErrMutationPolicyDisabled) || len(backend.applied) != 0 {
		t.Fatalf("stale update apply err=%v applied=%v", err, backend.applied)
	}
	rollback, err := service.Preview(context.Background(), "session-a", MutationRequest{Component: KindXray, Operation: MutationOperationRollback})
	if err != nil {
		t.Fatalf("off rollback preview = %v", err)
	}
	if _, err := service.Rollback(context.Background(), "session-a", rollback.PreviewToken); err != nil || len(backend.rolledBack) != 1 {
		t.Fatalf("off rollback err=%v rollbacks=%v", err, backend.rolledBack)
	}
}

func TestCheckSchedulerUsesFixedSequentialTuplesAndRAMNotificationDedupe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "component-policy.json")
	manager := newPolicyManager(path)
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	checks := &policyCheckStub{resultFor: func(request CheckRequest) CheckResult {
		return validPolicyScheduledResult(request, now, "update-available")
	}}
	var events []NotificationEvent
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy:    manager,
		Checks:    checks,
		Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
		Notification: NotificationHookFunc(func(_ context.Context, event NotificationEvent) error {
			events = append(events, event)
			return nil
		}),
		Now: func() time.Time { return now },
	})
	manager.SetScheduler(scheduler)
	evaluation := manager.snapshot()
	scheduler.runCycle(context.Background(), evaluation.epoch)
	if !reflect.DeepEqual(checks.Requests(), fixedComponentCheckTuples()) {
		t.Fatalf("scheduled requests = %v, want %v", checks.Requests(), fixedComponentCheckTuples())
	}
	if len(events) != len(componentCheckTuples) {
		t.Fatalf("notification count = %d", len(events))
	}
	status := scheduler.Status()
	if status.State != "completed" || !status.Enabled || len(status.Results) != len(componentCheckTuples) || status.NextDueAt == nil || !status.NextDueAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("completed scheduler status = %+v", status)
	}

	scheduler.runCycle(context.Background(), evaluation.epoch)
	if len(events) != len(componentCheckTuples) {
		t.Fatalf("duplicate notifications = %d", len(events))
	}

	maintenance := true
	scheduler.lifecycle = func() (LifecycleProjection, bool) { return LifecycleProjection{Maintenance: maintenance}, true }
	beforeChecks := len(checks.Requests())
	scheduler.runCycle(context.Background(), evaluation.epoch)
	if len(checks.Requests()) != beforeChecks || scheduler.Status().LastSkipReason != "maintenance" {
		t.Fatalf("maintenance skip requests=%d status=%+v", len(checks.Requests()), scheduler.Status())
	}

	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeOff, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	scheduler.runCycle(context.Background(), manager.snapshot().epoch)
	if status := scheduler.Status(); status.State != "disabled" || status.Enabled || len(checks.Requests()) != beforeChecks {
		t.Fatalf("off scheduler status = %+v requests=%d", status, len(checks.Requests()))
	}
}

func TestCheckSchedulerFailedDeliveryRetriesNextNormalCycle(t *testing.T) {
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	manager.SetPolicy(ComponentPolicy{SchemaVersion: 1, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60})
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	calls := 0
	fail := true
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy: manager, Now: func() time.Time { return now },
		Checks: &policyCheckStub{resultFor: func(request CheckRequest) CheckResult {
			return validPolicyScheduledResult(request, now, "update-available")
		}},
		Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
		Notification: NotificationHookFunc(func(context.Context, NotificationEvent) error {
			calls++
			if fail {
				return errors.New("synthetic failure")
			}
			return nil
		}),
	})
	epoch := manager.snapshot().epoch
	scheduler.runCycle(context.Background(), epoch)
	if calls != 3 || len(scheduler.notified) != 0 || scheduler.status.NotificationState != "failed" || !scheduler.nextDue.Equal(now.Add(time.Hour)) {
		t.Fatal("failure retried/deduped outside cadence")
	}
	now = now.Add(time.Hour)
	fail = false
	scheduler.runCycle(context.Background(), epoch)
	if calls != 6 || len(scheduler.notified) != 3 {
		t.Fatal("next cycle did not retry")
	}
	now = now.Add(time.Hour)
	scheduler.runCycle(context.Background(), epoch)
	if calls != 6 {
		t.Fatal("success did not dedupe")
	}
}

func TestCheckSchedulerCycleKeepsFailureVisibleAndRetriesOnlyFailedCandidate(t *testing.T) {
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	manager.SetPolicy(ComponentPolicy{SchemaVersion: 1, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60})
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	fail := true
	calls := 0
	scheduler := NewCheckScheduler(CheckSchedulerConfig{Policy: manager, Now: func() time.Time { return now }, Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true }, Checks: &policyCheckStub{resultFor: func(request CheckRequest) CheckResult {
		return validPolicyScheduledResult(request, now, "update-available")
	}}, Notification: NotificationHookFunc(func(_ context.Context, event NotificationEvent) error {
		calls++
		if fail && event.Component == KindXray {
			return errors.New("synthetic failure")
		}
		return nil
	})})
	scheduler.runCycle(context.Background(), manager.snapshot().epoch)
	if scheduler.status.NotificationState != "failed" || len(scheduler.notified) != 2 || calls != 3 {
		t.Fatal("mixed cycle hid failure")
	}
	fail = false
	now = now.Add(time.Hour)
	scheduler.runCycle(context.Background(), manager.snapshot().epoch)
	if scheduler.status.NotificationState != "notified" || len(scheduler.notified) != 3 || calls != 4 {
		t.Fatal("successful candidates retried or failed candidate suppressed")
	}
}

func TestCheckSchedulerBoundsNotificationHook(t *testing.T) {
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy: manager,
		Checks: &policyCheckStub{resultFor: func(request CheckRequest) CheckResult {
			return validPolicyScheduledResult(request, now, "update-available")
		}},
		Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
		Notification: NotificationHookFunc(func(context.Context, NotificationEvent) error {
			<-release
			return nil
		}),
		Now:                 func() time.Time { return now },
		NotificationTimeout: 10 * time.Millisecond,
	})
	manager.SetScheduler(scheduler)

	started := time.Now()
	scheduler.runCycle(context.Background(), manager.snapshot().epoch)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("notification hook was not bounded: %s", elapsed)
	}
	if status := scheduler.Status(); status.NotificationState != "failed" {
		t.Fatalf("timed out notification status = %+v", status)
	}
	close(release)
}

func TestCheckSchedulerNotificationLatencyPreservesLaterCheckBudget(t *testing.T) {
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: 1, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	// Scale the production limits together. Wait on deadlines, not sleeps or
	// scheduler polling: every check consumes its full metadata allowance and
	// every delivery consumes its full separate allowance.
	const scale = 300
	checkTimeout := MaxCheckDuration / scale
	notificationTimeout := DefaultComponentNotificationTimeout / scale
	var requests []CheckRequest
	notifications := make(chan NotificationEvent, len(componentCheckTuples))
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy:              manager,
		Lifecycle:           func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
		CycleTimeout:        DefaultComponentSchedulerCycleTimeout / scale,
		NotificationTimeout: notificationTimeout,
		Checks: scheduledCheckFunc(func(ctx context.Context, request CheckRequest) (CheckResult, error) {
			requests = append(requests, request)
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) < checkTimeout {
				t.Errorf("%s lost its full check allowance after notification latency", request.Component)
				return CheckResult{}, ErrCheckTimeout
			}
			checkContext, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			<-checkContext.Done()
			if ctx.Err() != nil {
				return CheckResult{}, ErrCheckTimeout
			}
			return validPolicyScheduledResult(request, time.Now(), "update-available"), nil
		}),
		Notification: NotificationHookFunc(func(ctx context.Context, event NotificationEvent) error {
			notifications <- event
			<-ctx.Done()
			return ctx.Err()
		}),
	})
	scheduler.runCycle(context.Background(), manager.snapshot().epoch)
	status := scheduler.Status()
	if !reflect.DeepEqual(requests, fixedComponentCheckTuples()) || len(notifications) != len(componentCheckTuples) || status.State != "completed" || status.NotificationState != "failed" || len(scheduler.notified) != 0 {
		t.Fatalf("bounded cycle requests=%v status=%+v dedupe=%v", requests, status, scheduler.notified)
	}
	if DefaultComponentSchedulerCycleTimeout != time.Duration(len(componentCheckTuples))*(MaxCheckDuration+DefaultComponentNotificationTimeout) {
		t.Fatal("production cycle must include all check and notification allowances")
	}
}

func TestCheckSchedulerPolicyChangeDuringCheckSuppressesNotification(t *testing.T) {
	for _, mode := range []string{ComponentPolicyModeManual, ComponentPolicyModeOff} {
		t.Run(mode, func(t *testing.T) {
			manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
			policy := ComponentPolicy{SchemaVersion: 1, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}
			if _, err := manager.SetPolicy(policy); err != nil {
				t.Fatal(err)
			}
			checkStarted := make(chan struct{})
			releaseCheck := make(chan struct{})
			done := make(chan struct{})
			notifications := make(chan NotificationEvent, len(componentCheckTuples))
			var requests []CheckRequest
			scheduler := NewCheckScheduler(CheckSchedulerConfig{
				Policy:    manager,
				Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
				Checks: scheduledCheckFunc(func(ctx context.Context, request CheckRequest) (CheckResult, error) {
					requests = append(requests, request)
					close(checkStarted)
					select {
					case <-releaseCheck:
						return validPolicyScheduledResult(request, time.Now(), "update-available"), nil
					case <-ctx.Done():
						return CheckResult{}, ctx.Err()
					}
				}),
				Notification: NotificationHookFunc(func(_ context.Context, event NotificationEvent) error {
					notifications <- event
					return nil
				}),
			})
			manager.SetScheduler(scheduler)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			epoch := manager.snapshot().epoch
			go func() {
				defer close(done)
				scheduler.runCycle(ctx, epoch)
			}()
			select {
			case <-checkStarted:
			case <-ctx.Done():
				t.Fatal("scheduled check did not start")
			}
			policy.Mode = mode
			_, err := manager.SetPolicy(policy)
			// The old Check completes only AFTER the persisted mutation returns.
			close(releaseCheck)
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("scheduled cycle did not finish")
			}
			if err != nil {
				t.Fatal(err)
			}
			status := scheduler.Status()
			if len(notifications) != 0 || len(scheduler.notified) != 0 || len(requests) != 1 || status.Enabled || status.State != "disabled" || status.NotificationState != "idle" || status.Results[KindXray].State != "checked" {
				t.Fatalf("delivery after %s mutation: calls=%d requests=%v status=%+v", mode, len(notifications), requests, status)
			}
		})
	}
}

func TestCheckSchedulerAtomicNotificationAdmission(t *testing.T) {
	for _, change := range []string{"manual", "off", "notify-new-epoch"} {
		for _, admitted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/admitted=%t", change, admitted), func(t *testing.T) {
				manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
				policy := ComponentPolicy{SchemaVersion: 1, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}
				if _, err := manager.SetPolicy(policy); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				calls := atomic.Int32{}
				scheduler := NewCheckScheduler(CheckSchedulerConfig{
					Policy: manager,
					Checks: &policyCheckStub{resultFor: func(request CheckRequest) CheckResult {
						return validPolicyScheduledResult(request, time.Now(), "update-available")
					}},
					Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
					Notification: NotificationHookFunc(func(ctx context.Context, _ NotificationEvent) error {
						calls.Add(1)
						if admitted {
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return nil
					}),
				})
				manager.SetScheduler(scheduler)
				if !admitted {
					scheduler.beforeNotificationAdmission = func() { close(entered); <-release }
				}
				epoch := manager.snapshot().epoch
				go func() { defer close(done); scheduler.runCycle(ctx, epoch) }()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("notification boundary not reached")
				}
				mutation := make(chan error, 1)
				go func() {
					updated := policy
					updated.Mode = change
					if change == "notify-new-epoch" {
						updated.Mode = ComponentPolicyModeManual
					}
					_, err := manager.SetPolicy(updated)
					if err == nil && change == "notify-new-epoch" {
						_, err = manager.SetPolicy(policy) // Same values, different epoch.
					}
					mutation <- err
				}()
				select {
				case err := <-mutation:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("policy mutation waited on notification network work")
				}
				// Successful mutation must precede release of the old send path.
				unblock()
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("notification cycle did not finish")
				}
				want := int32(0)
				if admitted {
					want = 1
				}
				if calls.Load() != want || len(scheduler.notified) != int(want) {
					t.Fatalf("send calls=%d dedupe=%v, want %d", calls.Load(), scheduler.notified, want)
				}
				if admitted && scheduler.status.NotificationState != "notified" {
					t.Fatal("already admitted delivery did not complete successfully")
				}
			})
		}
	}
}

func TestCheckSchedulerNotificationIdentityTracksExactCandidates(t *testing.T) {
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	var events []NotificationEvent
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy: manager,
		Notification: NotificationHookFunc(func(_ context.Context, event NotificationEvent) error {
			events = append(events, event)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	xray := validPolicyScheduledResult(componentCheckTuples[0], now, "update-available")
	xrayChanged := validPolicyScheduledResult(componentCheckTuples[0], now, "update-available")
	xrayChanged.Candidate.SHA256 = strings.Repeat("b", 64)
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, componentCheckTuples[0], xray)
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, componentCheckTuples[0], xrayChanged)

	xkeen := validPolicyScheduledResult(componentCheckTuples[2], now, "changed")
	xkeenChanged := validPolicyScheduledResult(componentCheckTuples[2], now, "changed")
	xkeenChanged.Candidate.SHA256 = strings.Repeat("b", 64)
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, componentCheckTuples[2], xkeen)
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, componentCheckTuples[2], xkeenChanged)

	geodata := validPolicyScheduledResult(componentCheckTuples[1], now, "changed")
	geodataChanged := validPolicyScheduledResult(componentCheckTuples[1], now, "changed")
	geodataChanged.Items = append([]CheckItem(nil), geodata.Items...)
	geodataChanged.Items[4].SHA256 = strings.Repeat("f", 64)
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, componentCheckTuples[1], geodata)
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, componentCheckTuples[1], geodataChanged)

	if len(events) != 6 {
		t.Fatalf("exact candidate notification count = %d, want 6", len(events))
	}
	for index, event := range events {
		if !isHexSHA256(event.CandidateIdentity) {
			t.Fatalf("event %d identity = %q", index, event.CandidateIdentity)
		}
	}
	for _, pair := range [][2]int{{0, 1}, {2, 3}, {4, 5}} {
		if events[pair[0]].CandidateIdentity == events[pair[1]].CandidateIdentity {
			t.Fatalf("candidate identity did not change for events %d/%d: %q", pair[0], pair[1], events[pair[0]].CandidateIdentity)
		}
	}
	if status := scheduledCheckStatus(geodataChanged); status.CandidateIdentity != events[5].CandidateIdentity {
		t.Fatalf("status identity = %q, notification identity = %q", status.CandidateIdentity, events[5].CandidateIdentity)
	}
}

func TestCheckSchedulerSuppressesUnknownInstalledState(t *testing.T) {
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	var events []NotificationEvent
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy: manager,
		Notification: NotificationHookFunc(func(_ context.Context, event NotificationEvent) error {
			events = append(events, event)
			return nil
		}),
		Now: func() time.Time { return now },
	})
	request := componentCheckTuples[0]
	unknown := validPolicyScheduledResult(request, now, "unknown")
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, request, unknown)
	if len(events) != 0 {
		t.Fatalf("unknown installed state emitted %d notifications", len(events))
	}

	knownAbsent := validPolicyScheduledResult(request, now, "not-installed")
	scheduler.notifyCandidate(context.Background(), manager.snapshot().epoch, request, knownAbsent)
	if len(events) != 1 || events[0].InstalledState != "not-installed" {
		t.Fatalf("known absent installed state events = %+v", events)
	}
}

func TestCheckSchedulerDoesNotCheckBeforeFullCadence(t *testing.T) {
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	checks := &policyCheckStub{}
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy:    manager,
		Checks:    checks,
		Lifecycle: func() (LifecycleProjection, bool) { return LifecycleProjection{}, true },
		Now:       func() time.Time { return now },
	})
	scheduler.Start(context.Background())
	status := scheduler.Status()
	if status.State != "waiting" || !status.Enabled || status.NextDueAt == nil || !status.NextDueAt.Equal(now.Add(time.Hour)) || len(checks.Requests()) != 0 {
		t.Fatalf("initial scheduler status = %+v requests=%v", status, checks.Requests())
	}
	scheduler.Stop()
}
