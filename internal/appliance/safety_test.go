package appliance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type blockingValidator struct {
	started chan struct{}
	release chan struct{}
}

func (v *blockingValidator) ValidateCandidate(ctx context.Context, _ string) error {
	close(v.started)
	select {
	case <-v.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestAdoptionRejectsProofDriftDuringValidation(t *testing.T) {
	fixture := newApplianceFixture(t)
	validator := &blockingValidator{started: make(chan struct{}), release: make(chan struct{})}
	fixture.service = NewService(Config{
		AppliancePath: fixture.appliancePath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, NodesPath: fixture.nodesPath,
		ActiveOutboundsPath: fixture.outboundsPath, Validator: validator,
		CandidateValidation: 5 * time.Second,
	})

	result := make(chan error, 1)
	go func() { result <- fixture.service.Adopt(context.Background()) }()
	<-validator.started

	routingPath := filepath.Join(fixture.configDir, "05_routing.json")
	contents, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routingPath, append(contents, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	close(validator.release)

	if err := <-result; err == nil {
		t.Fatal("adoption accepted proof input drift during candidate validation")
	}
	if _, err := os.Lstat(fixture.appliancePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale adoption committed appliance authority: %v", err)
	}
}

func TestAdoptionDoesNotReplaceAuthorityCreatedDuringValidation(t *testing.T) {
	fixture := newApplianceFixture(t)
	validator := &blockingValidator{started: make(chan struct{}), release: make(chan struct{})}
	fixture.service = NewService(Config{
		AppliancePath: fixture.appliancePath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, NodesPath: fixture.nodesPath,
		ActiveOutboundsPath: fixture.outboundsPath, Validator: validator,
		CandidateValidation: 5 * time.Second,
	})

	result := make(chan error, 1)
	go func() { result <- fixture.service.Adopt(context.Background()) }()
	<-validator.started

	if err := os.MkdirAll(filepath.Dir(fixture.appliancePath), 0o700); err != nil {
		t.Fatal(err)
	}
	const sentinel = "created concurrently\n"
	if err := os.WriteFile(fixture.appliancePath, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	close(validator.release)

	if err := <-result; err == nil {
		t.Fatal("adoption replaced authority created during validation")
	}
	contents, err := os.ReadFile(fixture.appliancePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != sentinel {
		t.Fatalf("concurrent authority changed: %q", string(contents))
	}
}

func TestRenderRejectsUnboundedAndExistingOutputWithoutMutation(t *testing.T) {
	fixture := newApplianceFixture(t)
	validator := &recordingValidator{}
	fixture.service = NewService(Config{
		AppliancePath: fixture.appliancePath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, NodesPath: fixture.nodesPath,
		ActiveOutboundsPath: fixture.outboundsPath, Validator: validator,
	})
	if err := fixture.service.Adopt(context.Background()); err != nil {
		t.Fatalf("adopt fixture: %v", err)
	}

	tmpInfo, err := os.Stat(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Render(os.TempDir()); err == nil {
		t.Fatal("render accepted the process temporary directory itself")
	}
	afterInfo, err := os.Stat(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && tmpInfo.Mode().Perm() != afterInfo.Mode().Perm() {
		t.Fatalf("render changed temp directory mode from %v to %v", tmpInfo.Mode().Perm(), afterInfo.Mode().Perm())
	}

	candidateRoot := filepath.Join(os.TempDir(), "xkeen-control")
	seed, err := os.MkdirTemp(candidateRoot, "seed-")
	if err != nil {
		if err := os.MkdirAll(candidateRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		seed, err = os.MkdirTemp(candidateRoot, "seed-")
		if err != nil {
			t.Fatal(err)
		}
	}
	candidate := filepath.Join(candidateRoot, "candidate-"+filepath.Base(seed))
	if err := os.Remove(seed); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(candidate)
	if err := fixture.service.Render(candidate); err != nil {
		t.Fatalf("render bounded candidate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(candidate, "xray", "05_routing.json")); err != nil {
		t.Fatalf("bounded candidate missing routing policy: %v", err)
	}

	stale := candidate + "-stale"
	if !strings.HasPrefix(filepath.Base(stale), "candidate-") {
		t.Fatal("invalid test candidate name")
	}
	if err := os.Mkdir(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stale)
	marker := filepath.Join(stale, "keep")
	if err := os.WriteFile(marker, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modeBefore, _ := os.Stat(stale)
	if err := fixture.service.Render(stale); err == nil {
		t.Fatal("render accepted a pre-existing output directory")
	}
	modeAfter, _ := os.Stat(stale)
	if runtime.GOOS != "windows" && modeBefore.Mode().Perm() != modeAfter.Mode().Perm() {
		t.Fatal("render chmodded a pre-existing output directory")
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "stale\n" {
		t.Fatalf("render changed stale output content: %q, %v", string(contents), err)
	}
}
