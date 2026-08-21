package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
