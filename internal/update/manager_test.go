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
	"testing"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/release"
)

type fakeLifecycle struct{ entered int }

func (f *fakeLifecycle) BeginApply(context.Context) (func(), error) {
	f.entered++
	return func() {}, nil
}

func TestManagerAppliesVerifiedCandidateUnderLifecycleAndCleansCandidate(t *testing.T) {
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
	lifecycle := &fakeLifecycle{}
	actions := []string{}
	manager := NewManager(Config{
		Current:   buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("b", 40), Channel: "stable"},
		Client:    release.NewClientForTest(server.URL, privateKey.Public().(ed25519.PublicKey)),
		Lifecycle: lifecycle,
		Paths:     Paths{CandidateDir: filepath.Join(dir, "candidate"), PreviousDir: filepath.Join(dir, "previous"), MarkerPath: filepath.Join(dir, "state", "installed-release.json"), PolicyPath: filepath.Join(dir, "state", "update-policy.json"), HelperPath: filepath.Join(dir, "helper")},
		RunHelper: func(_ context.Context, action string) error { actions = append(actions, action); return nil },
	})
	if err := manager.Apply(context.Background(), "stable", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if lifecycle.entered != 1 || len(actions) != 1 || actions[0] != "install" {
		t.Fatalf("lifecycle/helper evidence = %d/%v", lifecycle.entered, actions)
	}
	if _, err := os.Stat(filepath.Join(dir, "candidate")); !os.IsNotExist(err) {
		t.Fatalf("candidate directory remains: %v", err)
	}
	marker, err := os.ReadFile(filepath.Join(dir, "state", "installed-release.json"))
	if err != nil || !strings.Contains(string(marker), `"version":"1.2.3"`) {
		t.Fatalf("installed marker = %q, %v", marker, err)
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
