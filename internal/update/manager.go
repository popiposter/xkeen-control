package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	maxPolicyBytes      = 4 << 10
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
	Installed                    buildinfo.Info `json:"installed"`
	Channel                      string         `json:"channel"`
	LatestCompatible             string         `json:"latestCompatibleVersion"`
	LatestChannel                string         `json:"latestChannel,omitempty"`
	LatestSource                 string         `json:"latestSource,omitempty"`
	LatestSourceCommit           string         `json:"latestSourceCommit,omitempty"`
	ReleaseNotesURL              string         `json:"releaseNotesUrl,omitempty"`
	LastCheckAt                  string         `json:"lastCheckAt,omitempty"`
	LastCheckResult              string         `json:"lastCheckResult,omitempty"`
	RollbackAvailable            bool           `json:"rollbackAvailable"`
	RollbackVerificationRequired bool           `json:"rollbackVerificationRequired"`
	Policy                       Policy         `json:"policy"`
	SigningKeyConfigured         bool           `json:"signingKeyConfigured"`
	Scheduler                    NotifyStatus   `json:"scheduler"`
}

type Service interface {
	Status(context.Context) Status
	Check(context.Context, string, string) (Status, error)
	SetPolicy(Policy) (Status, error)
	Apply(context.Context, string, string) error
	ApplyChecked(context.Context, string, string) error
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

	mu                           sync.Mutex
	lastCheck                    time.Time
	lastResult                   string
	latest                       *release.Manifest
	rollbackAdmissionClaimed     bool
	rollbackVerificationRequired bool
	notify                       *NotifyScheduler
	policyRevision               uint64
	policyChanges                chan struct{}
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
	return &Manager{
		current: config.Current, client: config.Client, lifecycle: config.Lifecycle,
		paths: config.Paths, now: config.Now, runHelper: config.RunHelper, policyChanges: make(chan struct{}, 1),
	}
}

func (m *Manager) Status(_ context.Context) Status {
	if m == nil {
		return Status{LastCheckResult: "unavailable"}
	}
	policy := m.readPolicy()
	installed := m.current
	if marker, err := os.ReadFile(m.paths.MarkerPath); err == nil {
		var value buildinfo.Info
		if json.Unmarshal(marker, &value) == nil && value.Validate() == nil {
			installed = value
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{Installed: installed, Channel: policy.Channel, Policy: policy, SigningKeyConfigured: m.client != nil && m.clientSigningKeyConfigured(), LastCheckResult: m.lastResult}
	status.Scheduler = notifyPolicyStatus(policy)
	if m.notify != nil {
		status.Scheduler = m.notify.statusFor(policy)
	}
	if !m.lastCheck.IsZero() {
		status.LastCheckAt = m.lastCheck.UTC().Format(time.RFC3339)
	}
	if m.latest != nil {
		status.LatestCompatible = m.latest.Version
		status.LatestChannel = m.latest.Channel
		status.LatestSource = "github-release"
		status.LatestSourceCommit = m.latest.SourceCommit
		status.ReleaseNotesURL = release.ReleaseNotesURL(m.latest.Version)
	}
	status.RollbackVerificationRequired = m.rollbackVerificationRequired
	if !m.rollbackAdmissionClaimed && !m.rollbackVerificationRequired {
		_, rollbackErr := os.Stat(filepath.Join(m.paths.PreviousDir, "xkeen-control-linux-arm64"))
		status.RollbackAvailable = rollbackErr == nil
	}
	return status
}

func (m *Manager) Check(ctx context.Context, channel, version string) (Status, error) {
	channel, err := release.ParseChannel(channel)
	if err != nil {
		return m.Status(ctx), err
	}
	version = strings.TrimSpace(version)
	if err := validateVersionRequest(channel, version); err != nil {
		return m.Status(ctx), err
	}
	// A new valid Check attempt supersedes any remembered candidate immediately;
	// a failed lookup must never leave an older release applyable.
	m.mu.Lock()
	m.latest = nil
	m.mu.Unlock()
	manifest, err := m.client.Check(ctx, channel, version)
	m.mu.Lock()
	m.lastCheck = m.now()
	if err != nil {
		m.latest = nil
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
	m.mu.Lock()
	m.policyRevision++
	if m.latest != nil && m.latest.Channel != channel {
		m.latest = nil
	}
	m.mu.Unlock()
	select {
	case m.policyChanges <- struct{}{}:
	default:
	}
	return m.Status(context.Background()), nil
}

// Apply verifies and stages the complete candidate before entering the shared
// lifecycle barrier. Once admitted, it starts the fixed external helper and
// returns. The helper owns all post-stop commit/rollback work; the serving Go
// process must not be required to execute code after it has been stopped.
func (m *Manager) Apply(ctx context.Context, channel, version string) error {
	channel, err := release.ParseChannel(channel)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if err := validateVersionRequest(channel, version); err != nil {
		return err
	}
	candidate, err := m.client.FetchCandidate(ctx, channel, version)
	if err != nil {
		return err
	}
	if err := release.VerifyCandidate(candidate); err != nil {
		return err
	}
	return m.applyCandidate(ctx, candidate)
}

// ValidateChecked proves that a candidate was explicitly checked for the
// current effective channel. It is intentionally separate from Service so
// existing non-UI callers retain their narrow Apply contract.
func (m *Manager) ValidateChecked(ctx context.Context, channel, version string) error {
	channel, err := release.ParseChannel(channel)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if err := release.ValidateVersion(version); err != nil {
		return err
	}
	status := m.Status(ctx)
	if status.Policy.Channel != channel || status.LatestChannel != channel || status.LatestCompatible != version || status.LatestSource == "" {
		return errors.New("release candidate was not explicitly checked")
	}
	return nil
}

// ApplyChecked consumes the explicitly checked candidate before doing any
// network, staging or lifecycle work. A lost response therefore cannot be
// replayed with the same check. The candidate remains consumed if a later
// fetch/stage/helper step fails; the operator must run a fresh Check.
func (m *Manager) ApplyChecked(ctx context.Context, channel, version string) error {
	checked, err := m.claimChecked(ctx, channel, version)
	if err != nil {
		return err
	}
	candidate, err := m.client.FetchCandidate(ctx, channel, version)
	if err != nil {
		return err
	}
	if candidate.Manifest.Channel != channel || candidate.Manifest.Version != version || (checked.SourceCommit != "" && candidate.Manifest.SourceCommit != checked.SourceCommit) {
		return errors.New("release candidate changed after check")
	}
	if err := release.VerifyCandidate(candidate); err != nil {
		return err
	}
	if m.readPolicy().Channel != channel {
		return errors.New("release policy changed after check")
	}
	return m.applyCandidate(ctx, candidate)
}

func (m *Manager) claimChecked(_ context.Context, channel, version string) (release.Manifest, error) {
	channel, err := release.ParseChannel(channel)
	if err != nil {
		return release.Manifest{}, err
	}
	version = strings.TrimSpace(version)
	if err := release.ValidateVersion(version); err != nil {
		return release.Manifest{}, err
	}
	policy := m.readPolicy()
	m.mu.Lock()
	defer m.mu.Unlock()
	if policy.Channel != channel || m.latest == nil || m.latest.Channel != channel || m.latest.Version != version {
		return release.Manifest{}, errors.New("release candidate was not explicitly checked")
	}
	checked := *m.latest
	m.latest = nil
	return checked, nil
}

func (m *Manager) applyCandidate(ctx context.Context, candidate release.Candidate) error {

	releaseToken, err := m.beginLifecycle(ctx)
	if err != nil {
		return err
	}
	if err := m.stage(candidate); err != nil {
		releaseToken()
		return err
	}
	if err := m.launchHelper("install", releaseToken); err != nil {
		_ = os.RemoveAll(m.paths.CandidateDir)
		return errors.New("panel update helper could not start")
	}
	return nil
}

func validateVersionRequest(channel, version string) error {
	if channel == "beta" && version == "" {
		return errors.New("beta checks require an explicit version")
	}
	if version != "" && release.ValidateVersion(version) != nil {
		return errors.New("release version is invalid")
	}
	return nil
}

// Rollback admits the fixed helper under the same lifecycle barrier and then
// returns so the HTTP layer can deliver 202 before the helper's bounded handoff
// grace expires and process replacement begins.
func (m *Manager) Rollback(ctx context.Context) error {
	m.mu.Lock()
	if m.rollbackVerificationRequired {
		m.mu.Unlock()
		return errors.New("panel rollback outcome requires verification")
	}
	if m.rollbackAdmissionClaimed {
		m.mu.Unlock()
		return errors.New("panel rollback is busy")
	}
	m.mu.Unlock()
	if _, err := os.Stat(filepath.Join(m.paths.PreviousDir, "xkeen-control-linux-arm64")); err != nil {
		return errors.New("panel rollback is unavailable")
	}
	// Claim before lifecycle admission/helper start so concurrent requests cannot
	// enter the fixed helper twice. This is only transient ownership: a proven
	// pre-handoff failure releases it below.
	m.mu.Lock()
	if m.rollbackVerificationRequired {
		m.mu.Unlock()
		return errors.New("panel rollback outcome requires verification")
	}
	if m.rollbackAdmissionClaimed {
		m.mu.Unlock()
		return errors.New("panel rollback is busy")
	}
	m.rollbackAdmissionClaimed = true
	m.mu.Unlock()
	releaseToken, err := m.beginLifecycle(ctx)
	if err != nil {
		m.mu.Lock()
		m.rollbackAdmissionClaimed = false
		m.mu.Unlock()
		return err
	}
	if err := m.launchHelper("rollback", releaseToken); err != nil {
		m.mu.Lock()
		m.rollbackAdmissionClaimed = false
		m.mu.Unlock()
		return errors.New("panel rollback helper could not start")
	}
	// launchHelper returning nil proves that the fixed helper was started. From
	// this point the response may be lost while replacement proceeds, so retain
	// a no-replay verification latch and release transient admission ownership.
	m.mu.Lock()
	m.rollbackAdmissionClaimed = false
	m.rollbackVerificationRequired = true
	m.mu.Unlock()
	return nil
}

func (m *Manager) beginLifecycle(ctx context.Context) (func(), error) {
	if m.lifecycle == nil {
		return func() {}, nil
	}
	return m.lifecycle.BeginApply(ctx)
}

// launchHelper starts a process that is intentionally independent from the
// request context: an admitted update must survive the HTTP connection and the
// old panel process going away. The helper itself waits one bounded second
// before stop/swap, giving the handler time to flush its accepted response.
// The lifecycle token remains held while this process is alive; if the helper
// fails before stopping the panel, Wait releases the token in the old process.
func (m *Manager) launchHelper(action string, releaseToken func()) error {
	if m.runHelper != nil {
		go func() {
			defer releaseToken()
			_ = m.runHelper(context.Background(), action)
		}()
		return nil
	}
	cmd := exec.Command(m.paths.HelperPath, action)
	cmd.Env = append(os.Environ(), "XKEEN_CONTROL_HANDOFF_DELAY=1")
	if err := cmd.Start(); err != nil {
		releaseToken()
		return err
	}
	go func() {
		defer releaseToken()
		_ = cmd.Wait()
	}()
	return nil
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
	manifest, err := candidate.Manifest.MarshalDeterministic()
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(m.paths.CandidateDir, "release-manifest.json"), manifest, 0o600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(m.paths.CandidateDir, "release-manifest.sig"), candidate.Signature, 0o600); err != nil {
		return err
	}
	marker := buildinfo.Info{
		Product:      release.Product,
		Version:      candidate.Manifest.Version,
		SourceCommit: candidate.Manifest.SourceCommit,
		Channel:      candidate.Manifest.Channel,
	}
	return writeJSONAtomic(filepath.Join(m.paths.CandidateDir, "installed-release.json"), marker, 0o600)
}

func (m *Manager) readPolicy() Policy {
	defaultPolicy := Policy{Channel: "stable", Mode: "manual", CheckCadenceMinutes: 360}
	info, err := os.Lstat(m.paths.PolicyPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPolicyBytes || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return defaultPolicy
	}
	file, err := os.Open(m.paths.PolicyPath)
	if err != nil {
		return defaultPolicy
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() > maxPolicyBytes {
		return defaultPolicy
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxPolicyBytes+1))
	if err != nil || len(contents) > maxPolicyBytes {
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
