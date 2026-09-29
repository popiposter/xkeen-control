package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/release"
)

type fakeLifecycle struct {
	mu      sync.Mutex
	entered int
	exited  int
}

func (f *fakeLifecycle) BeginApply(context.Context) (func(), error) {
	f.mu.Lock()
	f.entered++
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.exited++
		f.mu.Unlock()
	}, nil
}

func (f *fakeLifecycle) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entered, f.exited
}

type rejectingLifecycle struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *rejectingLifecycle) BeginApply(context.Context) (func(), error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return nil, f.err
}

func (f *rejectingLifecycle) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type blockingLifecycle struct {
	entered chan struct{}
	release chan struct{}
}

func (f *blockingLifecycle) BeginApply(ctx context.Context) (func(), error) {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	select {
	case <-f.release:
		return func() {}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestManagerStagesMarkerAndHandsOffUnderLifecycle(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest, assets := testCandidate(t)
	manifestBytes, _ := manifest.MarshalDeterministic()
	signature, _ := release.Sign(manifestBytes, privateKey)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		switch name {
		case "release-manifest.json":
			_, _ = w.Write(manifestBytes)
		case "release-manifest.sig":
			_, _ = w.Write(signature)
		default:
			_, _ = w.Write(assets[name])
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	candidateDir := filepath.Join(dir, "candidate")
	lifecycle := &fakeLifecycle{}
	done := make(chan struct{})
	var helperAction string
	var markerContents []byte
	manager := NewManager(Config{
		Current:   buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("b", 40), Channel: "stable"},
		Client:    release.NewClientForTest(server.URL, privateKey.Public().(ed25519.PublicKey)),
		Lifecycle: lifecycle,
		Paths:     Paths{CandidateDir: candidateDir, PreviousDir: filepath.Join(dir, "previous"), MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json"), HelperPath: filepath.Join(dir, "helper")},
		RunHelper: func(_ context.Context, action string) error {
			helperAction = action
			markerContents, _ = os.ReadFile(filepath.Join(candidateDir, "installed-release.json"))
			_ = os.RemoveAll(candidateDir)
			close(done)
			return nil
		},
	})
	if err := manager.Apply(context.Background(), "stable", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("helper handoff did not run")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		entered, exited := lifecycle.counts()
		if entered == 1 && exited == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lifecycle evidence = entered:%d exited:%d", entered, exited)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if helperAction != "install" || !strings.Contains(string(markerContents), `"version":"1.2.3"`) {
		t.Fatalf("helper=%q marker=%q", helperAction, markerContents)
	}
	if _, err := os.Stat(candidateDir); !os.IsNotExist(err) {
		t.Fatalf("synthetic helper did not clean candidate: %v", err)
	}
}

func TestApplyCheckedConsumesCandidateBeforeHandoff(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest, assets := testCandidate(t)
	manifestBytes, _ := manifest.MarshalDeterministic()
	signature, _ := release.Sign(manifestBytes, privateKey)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		switch name {
		case "release-manifest.json":
			_, _ = w.Write(manifestBytes)
		case "release-manifest.sig":
			_, _ = w.Write(signature)
		default:
			_, _ = w.Write(assets[name])
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	helperStarted := make(chan struct{}, 1)
	manager := NewManager(Config{
		Current: buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("b", 40), Channel: "stable"},
		Client:  release.NewClientForTest(server.URL, privateKey.Public().(ed25519.PublicKey)),
		Paths:   Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: filepath.Join(dir, "previous"), MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json")},
		RunHelper: func(_ context.Context, action string) error {
			if action != "install" {
				t.Errorf("helper action = %q", action)
			}
			helperStarted <- struct{}{}
			return nil
		},
	})
	manager.mu.Lock()
	manager.latest = &manifest
	manager.mu.Unlock()

	if err := manager.ApplyChecked(context.Background(), "stable", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-helperStarted:
	case <-time.After(time.Second):
		t.Fatal("checked Apply did not start the helper")
	}
	if status := manager.Status(context.Background()); status.LatestCompatible != "" || status.LatestChannel != "" || status.LatestSource != "" {
		t.Fatalf("checked candidate remained applyable after claim: %+v", status)
	}
	if err := manager.ApplyChecked(context.Background(), "stable", "1.2.3"); err == nil {
		t.Fatal("checked Apply replay unexpectedly succeeded")
	}
}

func TestRollbackStartsHelperOnlyWhenPreviousGenerationExists(t *testing.T) {
	dir := t.TempDir()
	lifecycle := &fakeLifecycle{}
	manager := NewManager(Config{
		Current:   buildinfo.Info{Product: "xkeen-control", Version: "1.2.3", SourceCommit: strings.Repeat("c", 40), Channel: "stable"},
		Lifecycle: lifecycle,
		Paths:     Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: filepath.Join(dir, "previous"), MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json"), HelperPath: filepath.Join(dir, "helper")},
	})
	if err := manager.Rollback(context.Background()); err == nil {
		t.Fatal("rollback without previous generation was accepted")
	}
}

func TestRollbackConsumesHandoffBeforeReturning(t *testing.T) {
	dir := t.TempDir()
	previous := filepath.Join(dir, "previous")
	if err := os.MkdirAll(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "xkeen-control-linux-arm64"), []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	helperStarted := make(chan struct{}, 1)
	manager := NewManager(Config{
		Current: buildinfo.Info{Product: "xkeen-control", Version: "1.2.3", SourceCommit: strings.Repeat("c", 40), Channel: "stable"},
		Paths:   Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: previous, MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json")},
		RunHelper: func(_ context.Context, action string) error {
			if action != "rollback" {
				t.Errorf("helper action = %q", action)
			}
			helperStarted <- struct{}{}
			return nil
		},
	})
	if err := manager.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-helperStarted:
	case <-time.After(time.Second):
		t.Fatal("rollback did not start the helper")
	}
	if err := manager.Rollback(context.Background()); err == nil {
		t.Fatal("rollback replay unexpectedly succeeded")
	}
	status := manager.Status(context.Background())
	if status.RollbackAvailable || !status.RollbackVerificationRequired {
		t.Fatalf("rollback verification state = %+v", status)
	}
}

func TestRollbackReleasesAdmissionAfterLifecycleFailure(t *testing.T) {
	dir := t.TempDir()
	previous := filepath.Join(dir, "previous")
	if err := os.MkdirAll(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "xkeen-control-linux-arm64"), []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	lifecycle := &rejectingLifecycle{err: errors.New("synthetic lifecycle busy")}
	manager := NewManager(Config{
		Current:   buildinfo.Info{Product: "xkeen-control", Version: "1.2.3", SourceCommit: strings.Repeat("c", 40), Channel: "stable"},
		Lifecycle: lifecycle,
		Paths:     Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: previous, MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json"), HelperPath: filepath.Join(dir, "helper")},
	})
	if err := manager.Rollback(context.Background()); err == nil {
		t.Fatal("busy lifecycle unexpectedly admitted rollback")
	}
	status := manager.Status(context.Background())
	if !status.RollbackAvailable || status.RollbackVerificationRequired {
		t.Fatalf("lifecycle failure retained rollback claim: %+v", status)
	}
	if err := manager.Rollback(context.Background()); err == nil {
		t.Fatal("rollback admission was not released after lifecycle failure")
	}
	if lifecycle.callCount() != 2 {
		t.Fatalf("lifecycle admission calls = %d, want 2", lifecycle.callCount())
	}
}

func TestRollbackReleasesAdmissionAfterHelperStartFailure(t *testing.T) {
	dir := t.TempDir()
	previous := filepath.Join(dir, "previous")
	if err := os.MkdirAll(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "xkeen-control-linux-arm64"), []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Current: buildinfo.Info{Product: "xkeen-control", Version: "1.2.3", SourceCommit: strings.Repeat("c", 40), Channel: "stable"},
		Paths:   Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: previous, MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json"), HelperPath: filepath.Join(dir, "missing-helper")},
	})
	if err := manager.Rollback(context.Background()); err == nil {
		t.Fatal("missing rollback helper unexpectedly started")
	}
	status := manager.Status(context.Background())
	if !status.RollbackAvailable || status.RollbackVerificationRequired {
		t.Fatalf("helper-start failure retained rollback claim: %+v", status)
	}
	if err := manager.Rollback(context.Background()); err == nil {
		t.Fatal("rollback admission was not released after helper-start failure")
	}
}

func TestRollbackExcludesConcurrentAdmissionAndPersistsAfterHelperStart(t *testing.T) {
	dir := t.TempDir()
	previous := filepath.Join(dir, "previous")
	if err := os.MkdirAll(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "xkeen-control-linux-arm64"), []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}, 1), release: make(chan struct{})}
	helperStarted := make(chan struct{}, 1)
	manager := NewManager(Config{
		Current:   buildinfo.Info{Product: "xkeen-control", Version: "1.2.3", SourceCommit: strings.Repeat("c", 40), Channel: "stable"},
		Lifecycle: lifecycle,
		Paths:     Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: previous, MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json")},
		RunHelper: func(_ context.Context, action string) error {
			if action != "rollback" {
				t.Errorf("helper action = %q", action)
			}
			helperStarted <- struct{}{}
			return nil
		},
	})
	first := make(chan error, 1)
	go func() { first <- manager.Rollback(context.Background()) }()
	select {
	case <-lifecycle.entered:
	case <-time.After(time.Second):
		t.Fatal("rollback did not reach lifecycle admission")
	}
	if err := manager.Rollback(context.Background()); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("concurrent rollback error = %v", err)
	}
	close(lifecycle.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	select {
	case <-helperStarted:
	case <-time.After(time.Second):
		t.Fatal("rollback helper did not start")
	}
	status := manager.Status(context.Background())
	if status.RollbackAvailable || !status.RollbackVerificationRequired {
		t.Fatalf("successful rollback handoff state = %+v", status)
	}
}

func TestPolicyRejectsBetaAutoAndBoundsCadence(t *testing.T) {
	dir := t.TempDir()
	manager := NewManager(Config{Current: buildinfo.Current(), Paths: Paths{PolicyPath: filepath.Join(dir, "state", "policy.json"), CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: filepath.Join(dir, "previous")}})
	if _, err := manager.SetPolicy(Policy{Channel: "beta", Mode: "auto-stable", CheckCadenceMinutes: 360}); err == nil {
		t.Fatal("beta auto policy accepted")
	}
	if _, err := manager.SetPolicy(Policy{Channel: "stable", Mode: "notify", CheckCadenceMinutes: 1}); err == nil {
		t.Fatal("unbounded cadence accepted")
	}
	status, err := manager.SetPolicy(Policy{Channel: "stable", Mode: "notify", CheckCadenceMinutes: 360})
	if err != nil || status.Policy.Mode != "notify" {
		t.Fatalf("bounded policy = %+v, %v", status, err)
	}
}

func TestCheckedCandidateIsExactAndPolicyChangeClearsIt(t *testing.T) {
	dir := t.TempDir()
	manager := NewManager(Config{Current: buildinfo.Current(), Client: release.NewClientForTest("", nil), Paths: Paths{
		PolicyPath: filepath.Join(dir, "state", "policy.json"), CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: filepath.Join(dir, "previous"),
	}})
	if err := manager.ValidateChecked(context.Background(), "stable", "1.2.3"); err == nil {
		t.Fatal("unchecked candidate was accepted")
	}
	manager.mu.Lock()
	manager.latest = &release.Manifest{Channel: "stable", Version: "1.2.3"}
	manager.mu.Unlock()
	if err := manager.ValidateChecked(context.Background(), "stable", "1.2.3"); err != nil {
		t.Fatalf("exact checked candidate rejected: %v", err)
	}
	if err := manager.ValidateChecked(context.Background(), "stable", "1.2.4"); err == nil {
		t.Fatal("different checked version was accepted")
	}
	status, err := manager.SetPolicy(Policy{Channel: "beta", Mode: "manual", CheckCadenceMinutes: 360})
	if err != nil {
		t.Fatal(err)
	}
	if status.LatestCompatible != "" || status.LatestChannel != "" || status.LatestSource != "" {
		t.Fatalf("policy change retained stale candidate: %+v", status)
	}
	manager.mu.Lock()
	manager.latest = &release.Manifest{Channel: "stable", Version: "1.2.3"}
	manager.mu.Unlock()
	if _, err := manager.Check(context.Background(), "stable", ""); err == nil {
		t.Fatal("unavailable release Check unexpectedly succeeded")
	}
	if status := manager.Status(context.Background()); status.LatestCompatible != "" {
		t.Fatalf("failed Check retained stale candidate: %+v", status)
	}
}

func TestCheckBoundsVersionAndRequiresBetaPin(t *testing.T) {
	dir := t.TempDir()
	manager := NewManager(Config{Paths: Paths{
		PolicyPath: filepath.Join(dir, "state", "policy.json"), CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: filepath.Join(dir, "previous"),
	}})
	if _, err := manager.Check(context.Background(), "beta", ""); err == nil {
		t.Fatal("beta Check without version was accepted")
	}
	if _, err := manager.Check(context.Background(), "stable", strings.Repeat("1", release.MaxRequestedVersionBytes+1)); err == nil {
		t.Fatal("oversized stable version was accepted")
	}
}

func testCandidate(t *testing.T) (release.Manifest, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	assets := map[string][]byte{}
	for _, name := range release.RequiredArtifacts {
		contents := []byte(name + " candidate")
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
		assets[name] = contents
	}
	manifest, err := release.BuildManifest("1.2.3", strings.Repeat("c", 40), "stable", 1_750_000_000, paths)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, assets
}
