package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/release"
)

const (
	DefaultCandidateDir = "/tmp/xkeen-control/panel-update"
	DefaultPreviousDir  = "/opt/etc/xkeen-control/previous/panel"
	DefaultMarkerPath   = "/opt/etc/xkeen-control/state/installed-release.json"
	DefaultPolicyPath   = "/opt/etc/xkeen-control/state/update-policy.json"
	DefaultHelperPath   = "/opt/libexec/xkeen-control-updater"
)

type Lifecycle interface {
	BeginApply(context.Context) (func(), error)
}

type Paths struct {
	CandidateDir string
	PreviousDir  string
	MarkerPath   string
	PolicyPath   string
	HelperPath   string
}

type Policy struct {
	Channel              string `json:"channel"`
	Mode                 string `json:"mode"`
	CheckCadenceMinutes  int    `json:"checkCadenceMinutes"`
	MaintenanceWindowUTC string `json:"maintenanceWindowUtc,omitempty"`
}

type Status struct {
	Installed            buildinfo.Info `json:"installed"`
	Channel              string         `json:"channel"`
	LatestCompatible     string         `json:"latestCompatibleVersion"`
	LatestSourceCommit   string         `json:"latestSourceCommit,omitempty"`
	ReleaseNotesURL      string         `json:"releaseNotesUrl,omitempty"`
	LastCheckAt          string         `json:"lastCheckAt,omitempty"`
	LastCheckResult      string         `json:"lastCheckResult,omitempty"`
	RollbackAvailable    bool           `json:"rollbackAvailable"`
	Policy               Policy         `json:"policy"`
	SigningKeyConfigured bool           `json:"signingKeyConfigured"`
}

type Service interface {
	Status(context.Context) Status
	Check(context.Context, string, string) (Status, error)
	SetPolicy(Policy) (Status, error)
	Apply(context.Context, string, string) error
	Rollback(context.Context) error
}

type Config struct {
	Current    buildinfo.Info
	Client     *release.Client
	Lifecycle  Lifecycle
	Paths      Paths
	HelperPath string
	Now        func() time.Time
	RunHelper  func(context.Context, string) error
}

type Manager struct {
	current   buildinfo.Info
	client    *release.Client
	lifecycle Lifecycle
	paths     Paths
	now       func() time.Time
	runHelper func(context.Context, string) error

	mu         sync.Mutex
	lastCheck  time.Time
	lastResult string
	latest     *release.Manifest
}

func NewManager(config Config) *Manager {
	if config.Current.Product == "" {
		config.Current = buildinfo.Current()
	}
	if config.Client == nil {
		config.Client = release.NewClient()
	}
	if config.Paths.CandidateDir == "" {
		config.Paths.CandidateDir = DefaultCandidateDir
	}
	if config.Paths.PreviousDir == "" {
		config.Paths.PreviousDir = DefaultPreviousDir
	}
	if config.Paths.MarkerPath == "" {
		config.Paths.MarkerPath = DefaultMarkerPath
	}
	if config.Paths.PolicyPath == "" {
		config.Paths.PolicyPath = DefaultPolicyPath
	}
	if config.Paths.HelperPath == "" {
		config.Paths.HelperPath = DefaultHelperPath
	}
	if config.HelperPath != "" {
		config.Paths.HelperPath = config.HelperPath
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	manager := &Manager{current: config.Current, client: config.Client, lifecycle: config.Lifecycle, paths: config.Paths, now: config.Now}
	if config.RunHelper != nil {
		manager.runHelper = config.RunHelper
	} else {
		manager.runHelper = func(ctx context.Context, action string) error {
			return exec.CommandContext(ctx, manager.paths.HelperPath, action).Run()
		}
	}
	return manager
}

func (m *Manager) Status(_ context.Context) Status {
	if m == nil {
		return Status{LastCheckResult: "unavailable"}
	}
	policy := m.readPolicy()
	installed := m.current
	if marker, err := os.ReadFile(m.paths.MarkerPath); err == nil {
		var value buildinfo.Info
		if json.Unmarshal(marker, &value) == nil && value.Product == buildinfo.Current().Product {
			installed = value
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{Installed: installed, Channel: policy.Channel, Policy: policy, SigningKeyConfigured: m.client != nil && m.clientSigningKeyConfigured(), LastCheckResult: m.lastResult}
	if !m.lastCheck.IsZero() {
		status.LastCheckAt = m.lastCheck.UTC().Format(time.RFC3339)
	}
	if m.latest != nil {
		status.LatestCompatible = m.latest.Version
		status.LatestSourceCommit = m.latest.SourceCommit
		status.ReleaseNotesURL = release.ReleaseNotesURL(m.latest.Version)
	}
	_, rollbackErr := os.Stat(filepath.Join(m.paths.PreviousDir, "xkeen-control-linux-arm64"))
	status.RollbackAvailable = rollbackErr == nil
	return status
}

func (m *Manager) Check(ctx context.Context, channel, version string) (Status, error) {
	channel, err := release.ParseChannel(channel)
	if err != nil {
		return m.Status(ctx), err
	}
	if channel == "beta" && strings.TrimSpace(version) == "" {
		return m.Status(ctx), errors.New("beta checks require an explicit version")
	}
	manifest, err := m.client.Check(ctx, channel, version)
	m.mu.Lock()
	m.lastCheck = m.now()
	if err != nil {
		m.lastResult = "failed"
		m.mu.Unlock()
		return m.Status(ctx), err
	}
	m.latest = &manifest
	m.lastResult = "ok"
	m.mu.Unlock()
	return m.Status(ctx), nil
}

func (m *Manager) SetPolicy(policy Policy) (Status, error) {
	if m == nil {
		return Status{}, errors.New("update service unavailable")
	}
	channel, err := release.ParseChannel(policy.Channel)
	if err != nil {
		return Status{}, err
	}
	if policy.Mode != "manual" && policy.Mode != "notify" && policy.Mode != "auto-stable" {
		return Status{}, errors.New("unsupported update policy")
	}
	if policy.Mode == "auto-stable" && channel != "stable" {
		return Status{}, errors.New("beta cannot be automatic")
	}
	if policy.CheckCadenceMinutes < 60 || policy.CheckCadenceMinutes > 7*24*60 {
		return Status{}, errors.New("check cadence is outside bounds")
	}
	policy.Channel = channel
	if err := writeJSONAtomic(m.paths.PolicyPath, policy, 0o600); err != nil {
		return Status{}, errors.New("update policy could not be saved")
	}
	return m.Status(context.Background()), nil
}

func (m *Manager) Apply(ctx context.Context, channel, version string) error {
	channel, err := release.ParseChannel(channel)
	if err != nil {
		return err
	}
	if channel == "beta" && strings.TrimSpace(version) == "" {
		return errors.New("beta apply requires an explicit version")
	}
	candidate, err := m.client.FetchCandidate(ctx, channel, version)
	if err != nil {
		return err
	}
	if err := release.VerifyCandidate(candidate); err != nil {
		return err
	}
	releaseToken, err := m.beginLifecycle(ctx)
	if err != nil {
		return err
	}
	defer releaseToken()
	if err := m.stage(candidate); err != nil {
		return err
	}
	if err := m.runHelper(ctx, "install"); err != nil {
		_ = os.RemoveAll(m.paths.CandidateDir)
		return errors.New("panel update failed and helper did not commit")
	}
	if err := writeJSONAtomic(m.paths.MarkerPath, buildinfo.Info{Product: release.Product, Version: candidate.Manifest.Version, SourceCommit: candidate.Manifest.SourceCommit, Channel: candidate.Manifest.Channel}, 0o600); err != nil {
		_ = m.runHelper(ctx, "rollback")
		return errors.New("panel update committed without an installed marker")
	}
	_ = os.RemoveAll(m.paths.CandidateDir)
	return nil
}

func (m *Manager) Rollback(ctx context.Context) error {
	releaseToken, err := m.beginLifecycle(ctx)
	if err != nil {
		return err
	}
	defer releaseToken()
	if err := m.runHelper(ctx, "rollback"); err != nil {
		return errors.New("panel rollback failed")
	}
	return nil
}

func (m *Manager) beginLifecycle(ctx context.Context) (func(), error) {
	if m.lifecycle == nil {
		return func() {}, nil
	}
	return m.lifecycle.BeginApply(ctx)
}

func (m *Manager) stage(candidate release.Candidate) error {
	if err := os.RemoveAll(m.paths.CandidateDir); err != nil {
		return errors.New("candidate cleanup failed")
	}
	if err := os.MkdirAll(m.paths.CandidateDir, 0o700); err != nil {
		return errors.New("candidate directory unavailable")
	}
	for _, artifact := range candidate.Manifest.Artifacts {
		contents := candidate.Assets[artifact.Name]
		if err := writeFile(filepath.Join(m.paths.CandidateDir, artifact.Name), contents, 0o755); err != nil {
			return errors.New("candidate asset could not be staged")
		}
	}
	if manifest, err := candidate.Manifest.MarshalDeterministic(); err != nil {
		return err
	} else if err := writeFile(filepath.Join(m.paths.CandidateDir, "release-manifest.json"), manifest, 0o600); err != nil {
		return err
	}
	return writeFile(filepath.Join(m.paths.CandidateDir, "release-manifest.sig"), candidate.Signature, 0o600)
}

func (m *Manager) readPolicy() Policy {
	defaultPolicy := Policy{Channel: "stable", Mode: "manual", CheckCadenceMinutes: 360}
	contents, err := os.ReadFile(m.paths.PolicyPath)
	if err != nil {
		return defaultPolicy
	}
	var value Policy
	if json.Unmarshal(contents, &value) != nil || release.ParseChannelMust(value.Channel) == "" || value.CheckCadenceMinutes < 60 || value.CheckCadenceMinutes > 7*24*60 {
		return defaultPolicy
	}
	if value.Mode != "manual" && value.Mode != "notify" && value.Mode != "auto-stable" {
		return defaultPolicy
	}
	if value.Mode == "auto-stable" && value.Channel != "stable" {
		return defaultPolicy
	}
	return value
}

func (m *Manager) clientSigningKeyConfigured() bool {
	// The client intentionally exposes this only as a boolean; key bytes never
	// enter an API projection or diagnostic message.
	return release.StableKeyConfigured(m.client)
}

func writeFile(path string, contents []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, contents, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	contents, err := json.Marshal(value)
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".xkeen-update-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
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
	return os.Chmod(path, mode)
}
