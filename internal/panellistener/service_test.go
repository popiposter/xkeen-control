package panellistener

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type listenerLifecycleStub struct {
	mu      sync.Mutex
	entered int
	exited  int
}

func (stub *listenerLifecycleStub) BeginApply(context.Context) (func(), error) {
	stub.mu.Lock()
	stub.entered++
	stub.mu.Unlock()
	return func() {
		stub.mu.Lock()
		stub.exited++
		stub.mu.Unlock()
	}, nil
}

func (stub *listenerLifecycleStub) counts() (int, int) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.entered, stub.exited
}

func listenerAddresses() ([]net.Addr, error) {
	return []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.2")},
		&net.IPNet{IP: net.ParseIP("192.168.10.2")},
		&net.IPNet{IP: net.ParseIP("10.0.0.4")},
		&net.IPNet{IP: net.ParseIP("fd00::2")},
		&net.IPNet{IP: net.ParseIP("169.254.4.2")},
		&net.IPNet{IP: net.ParseIP("8.8.8.8")},
	}, nil
}

func TestResolveStartupUsesEnvironmentFileThenDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "listen-address")
	resolution, err := ResolveStartup("", path)
	if err != nil || resolution.Source != SourceDefault || resolution.Address != DefaultAddress {
		t.Fatalf("default resolution = %+v, %v", resolution, err)
	}
	if err := os.WriteFile(path, []byte("192.168.10.2:8787\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolution, err = ResolveStartup("", path)
	if err != nil || resolution.Source != SourceFile || resolution.Address != "192.168.10.2:8787" || resolution.FileFingerprint == absentFingerprint {
		t.Fatalf("file resolution = %+v, %v", resolution, err)
	}
	resolution, err = ResolveStartup("127.0.0.1:9999", path)
	if err != nil || resolution.Source != SourceEnvironment || resolution.Address != "127.0.0.1:9999" {
		t.Fatalf("environment resolution = %+v, %v", resolution, err)
	}
	if _, err := ResolveStartup("", filepath.Join(dir, "missing", "listen-address")); err != nil {
		t.Fatalf("missing parent should select default: %v", err)
	}
}

func TestResolveStartupAcceptsLegacy0644ListenerFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "listen-address")
	if err := os.WriteFile(path, []byte("192.168.10.2:8787\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveStartup("", path)
	if err != nil || resolution.Source != SourceFile || resolution.Address != "192.168.10.2:8787" {
		t.Fatalf("legacy 0644 startup resolution = %+v, %v", resolution, err)
	}
	service := NewService(Config{FilePath: path, Initial: resolution, InterfaceAddrs: listenerAddresses})
	if err := service.StartupError(); err != nil {
		t.Fatalf("legacy 0644 listener prevented startup: %v", err)
	}
	projection, err := service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityEditable || projection.Host != "192.168.10.2" {
		t.Fatalf("legacy 0644 projection = %+v, %v", projection, err)
	}
}

func TestParseAddressRejectsHostnameWildcardAndPublicBind(t *testing.T) {
	for _, value := range []string{"localhost:8787", "0.0.0.0:8787", "8.8.8.8:8787", "169.254.1.2:8787", "192.168.1.2:0", "not-an-address"} {
		if _, err := ParseAddress(value); err == nil {
			t.Fatalf("invalid listener %q was accepted", value)
		}
	}
	for _, value := range []string{"127.0.0.1:8787", "192.168.1.2:8787", "[fd00::2]:8787", "[::1]:8787"} {
		if _, err := ParseAddress(value); err != nil {
			t.Fatalf("valid listener %q was rejected: %v", value, err)
		}
	}
}

func TestProjectionCatalogAndFileDriftAreFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "listen-address")
	if err := os.WriteFile(path, []byte("192.168.10.2:8787\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveStartup("", path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(Config{FilePath: path, Initial: resolution, InterfaceAddrs: listenerAddresses})
	projection, err := service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityEditable || projection.Host != "192.168.10.2" || projection.Port != 8787 {
		t.Fatalf("editable projection = %+v, %v", projection, err)
	}
	if strings.Join(projection.AllowedHosts, ",") != "127.0.0.1,::1,10.0.0.4,192.168.10.2,fd00::2" {
		t.Fatalf("allowed hosts = %v", projection.AllowedHosts)
	}
	if err := os.WriteFile(path, []byte("192.168.10.3:8787\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err = service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityDriftDetected || projection.Host != "192.168.10.2" {
		t.Fatalf("drift projection = %+v, %v", projection, err)
	}
	if _, err := service.Preview(context.Background(), "session", "10.0.0.4"); !errors.Is(err, ErrDriftDetected) {
		t.Fatalf("Preview after drift = %v", err)
	}
}

func TestEnvironmentOwnedListenerCannotPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "listen-address")
	if err := os.WriteFile(path, []byte("192.168.10.2:8787\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveStartup("127.0.0.1:8787", path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(Config{FilePath: path, Initial: resolution, InterfaceAddrs: listenerAddresses})
	projection, err := service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityEnvironmentOwned || projection.Source != SourceEnvironment {
		t.Fatalf("environment projection = %+v, %v", projection, err)
	}
	if _, err := service.Preview(context.Background(), "session", "10.0.0.4"); !errors.Is(err, ErrEnvironmentOwned) {
		t.Fatalf("environment Preview = %v", err)
	}
}

func TestNoopPreviewDoesNotAcquireLifecycleOrLaunchHelper(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "listen-address")
	resolution, err := ResolveStartup("", path)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &listenerLifecycleStub{}
	launched := make(chan struct{}, 1)
	service := NewService(Config{FilePath: path, Initial: resolution, InterfaceAddrs: listenerAddresses, Lifecycle: lifecycle, RunHelper: func(context.Context, string) error { launched <- struct{}{}; return nil }})
	preview, err := service.Preview(context.Background(), "session", "127.0.0.1")
	if err != nil || !preview.Noop || preview.RestartRequired || preview.SessionInvalidated || preview.LoginRequired {
		t.Fatalf("no-op Preview = %+v, %v", preview, err)
	}
	result, err := service.Apply(context.Background(), "session", preview.Token)
	if err != nil || !result.Noop || result.Accepted || result.State != "no-op" {
		t.Fatalf("no-op Apply = %+v, %v", result, err)
	}
	select {
	case <-launched:
		t.Fatal("no-op launched helper")
	case <-time.After(50 * time.Millisecond):
	}
	entered, exited := lifecycle.counts()
	if entered != 0 || exited != 0 {
		t.Fatalf("no-op lifecycle counts = %d/%d", entered, exited)
	}
}

func TestApplyStagesFixedFilesAndLaunchesOnlyRebind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "listen-address")
	stage := filepath.Join(dir, "panel-listener")
	if err := os.WriteFile(path, []byte("127.0.0.1:8787\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveStartup("", path)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &listenerLifecycleStub{}
	launched := make(chan string, 1)
	service := NewService(Config{
		FilePath: path, StagingDir: stage, Initial: resolution, InterfaceAddrs: listenerAddresses, Lifecycle: lifecycle,
		RunHelper: func(_ context.Context, action string) error {
			launched <- action
			candidate, err := os.ReadFile(filepath.Join(stage, "candidate"))
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(stage, "previous")); err != nil {
				return err
			}
			if err := os.WriteFile(path, candidate, 0o600); err != nil {
				return err
			}
			return os.RemoveAll(stage)
		},
	})
	preview, err := service.Preview(context.Background(), "session", "10.0.0.4")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), "session", preview.Token)
	if err != nil || !result.Accepted || result.State != "rebind-started" || result.After.Host != "10.0.0.4" || result.After.Port != 8787 {
		t.Fatalf("Apply = %+v, %v", result, err)
	}
	select {
	case action := <-launched:
		if action != "rebind" {
			t.Fatalf("helper action = %q", action)
		}
	case <-time.After(time.Second):
		t.Fatal("helper was not launched")
	}
	deadline := time.Now().Add(time.Second)
	for {
		if entered, exited := lifecycle.counts(); entered == 1 && exited == 1 {
			break
		}
		if time.Now().After(deadline) {
			entered, exited := lifecycle.counts()
			t.Fatalf("lifecycle counts = %d/%d", entered, exited)
		}
		time.Sleep(time.Millisecond)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "10.0.0.4:8787\n" {
		t.Fatalf("listener file = %q, %v", contents, err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("listener staging remains: %v", err)
	}
}
