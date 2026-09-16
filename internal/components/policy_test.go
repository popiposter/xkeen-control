package components

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type policyCheckStub struct {
	mu       sync.Mutex
	requests []CheckRequest
	result   CheckResult
	err      error
}

func (stub *policyCheckStub) Check(_ context.Context, request CheckRequest) (CheckResult, error) {
	stub.mu.Lock()
	stub.requests = append(stub.requests, request)
	result, err := stub.result, stub.err
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
	checks := &policyCheckStub{result: CheckResult{SchemaVersion: CheckSchemaVersion, CheckedAt: now, Candidate: &CheckCandidate{Version: "2.0.0"}, InstalledState: "update-available", Eligible: true}}
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

func TestCheckSchedulerBoundsNotificationHook(t *testing.T) {
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	manager := newPolicyManager(filepath.Join(t.TempDir(), "component-policy.json"))
	if _, err := manager.SetPolicy(ComponentPolicy{SchemaVersion: ComponentPolicySchemaVersion, Mode: ComponentPolicyModeNotify, CheckCadenceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	scheduler := NewCheckScheduler(CheckSchedulerConfig{
		Policy:    manager,
		Checks:    &policyCheckStub{result: CheckResult{SchemaVersion: CheckSchemaVersion, CheckedAt: now, Candidate: &CheckCandidate{Version: "2.0.0"}, InstalledState: "update-available", Eligible: true}},
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
