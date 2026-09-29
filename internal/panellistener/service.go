// Package panellistener owns the process-local panel bind and its narrow,
// session-bound rebind handoff. It is deliberately not a generic settings or
// process-control surface.
package panellistener

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultAddress       = "127.0.0.1:8787"
	DefaultFilePath      = "/opt/etc/xkeen-control/listen-address"
	DefaultStagingDir    = "/tmp/xkeen-control/panel-listener"
	DefaultHelperPath    = "/opt/libexec/xkeen-control-updater"
	MaxListenerFileBytes = 256
	DefaultPreviewTTL    = 5 * time.Minute
	DefaultMaxPreviews   = 4
	maxHostBytes         = 64
)

var (
	ErrUnavailable      = errors.New("panel listener is unavailable")
	ErrInvalidRequest   = errors.New("panel listener request is invalid")
	ErrEnvironmentOwned = errors.New("panel listener is environment-owned")
	ErrDriftDetected    = errors.New("panel listener authority drift detected")
	ErrPreviewExpired   = errors.New("panel listener preview is expired or invalid")
	ErrPreviewStale     = errors.New("panel listener preview is stale")
	ErrBusy             = errors.New("panel listener is busy")
	ErrHelperStart      = errors.New("panel listener helper could not start")
)

type Source string

const (
	SourceDefault     Source = "default"
	SourceFile        Source = "file"
	SourceEnvironment Source = "environment"
)

type Editability string

const (
	EditabilityEditable         Editability = "editable"
	EditabilityDriftDetected    Editability = "drift-detected"
	EditabilityEnvironmentOwned Editability = "environment-owned"
	EditabilityUnavailable      Editability = "unavailable"
)

type Address struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Projection struct {
	Host         string      `json:"host"`
	Port         int         `json:"port"`
	Source       Source      `json:"source"`
	Editability  Editability `json:"editability"`
	AllowedHosts []string    `json:"allowedHosts"`
}

type Preview struct {
	Token                   string    `json:"previewToken"`
	ExpiresAt               time.Time `json:"expiresAt"`
	Before                  Address   `json:"before"`
	After                   Address   `json:"after"`
	Noop                    bool      `json:"noop"`
	ReconnectClassification string    `json:"reconnectClassification"`
	RestartRequired         bool      `json:"restartRequired"`
	SessionInvalidated      bool      `json:"sessionInvalidated"`
	LoginRequired           bool      `json:"loginRequired"`
}

type ApplyResult struct {
	Accepted                bool    `json:"accepted"`
	State                   string  `json:"state"`
	Before                  Address `json:"before"`
	After                   Address `json:"after"`
	Noop                    bool    `json:"noop"`
	ReconnectClassification string  `json:"reconnectClassification"`
	RestartRequired         bool    `json:"restartRequired"`
	SessionInvalidated      bool    `json:"sessionInvalidated"`
	LoginRequired           bool    `json:"loginRequired"`
}

// Resolution is the immutable startup decision used by the Go process. A
// file fingerprint is retained only for drift detection; it is never exposed
// to the browser.
type Resolution struct {
	Address         string
	Source          Source
	FileFingerprint string
}

type InterfaceAddressProvider func() ([]net.Addr, error)

type Config struct {
	FilePath       string
	HelperPath     string
	StagingDir     string
	Initial        Resolution
	InterfaceAddrs InterfaceAddressProvider
	Lifecycle      interface {
		BeginApply(context.Context) (func(), error)
	}
	PreviewTTL  time.Duration
	MaxPreviews int
	Now         func() time.Time
	Random      io.Reader
	RunHelper   func(context.Context, string) error
}

type Service struct {
	filePath       string
	helperPath     string
	stagingDir     string
	initial        Resolution
	active         Address
	interfaceAddrs InterfaceAddressProvider
	lifecycle      interface {
		BeginApply(context.Context) (func(), error)
	}
	previewTTL  time.Duration
	maxPreviews int
	now         func() time.Time
	random      io.Reader
	runHelper   func(context.Context, string) error
	startupErr  error

	mu       sync.Mutex
	previews map[string]previewEntry
	busy     bool
}

type previewEntry struct {
	Binding     string
	Before      Address
	After       Address
	Fingerprint string
	Source      Source
	Noop        bool
	ExpiresAt   time.Time
}

type fileSnapshot struct {
	Present     bool
	Address     Address
	Raw         []byte
	Mode        os.FileMode
	Fingerprint string
}

// NewService constructs the purpose-specific listener owner. Production
// wiring supplies Initial from ResolveStartup so an invalid startup source
// stops the process before the HTTP server can bind.
func NewService(config Config) *Service {
	if config.FilePath == "" {
		config.FilePath = DefaultFilePath
	}
	if config.HelperPath == "" {
		config.HelperPath = DefaultHelperPath
	}
	if config.StagingDir == "" {
		config.StagingDir = DefaultStagingDir
	}
	if config.PreviewTTL <= 0 || config.PreviewTTL > DefaultPreviewTTL {
		config.PreviewTTL = DefaultPreviewTTL
	}
	if config.MaxPreviews <= 0 || config.MaxPreviews > DefaultMaxPreviews {
		config.MaxPreviews = DefaultMaxPreviews
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.InterfaceAddrs == nil {
		config.InterfaceAddrs = net.InterfaceAddrs
	}
	service := &Service{
		filePath: config.FilePath, helperPath: config.HelperPath, stagingDir: config.StagingDir,
		initial: config.Initial, interfaceAddrs: config.InterfaceAddrs, lifecycle: config.Lifecycle,
		previewTTL: config.PreviewTTL, maxPreviews: config.MaxPreviews, now: config.Now,
		random: config.Random, runHelper: config.RunHelper, previews: make(map[string]previewEntry),
	}
	if service.initial.Address == "" {
		service.initial, service.startupErr = ResolveStartup("", service.filePath)
	}
	if service.startupErr == nil {
		service.active, service.startupErr = ParseAddress(service.initial.Address)
	}
	return service
}

func (s *Service) StartupError() error {
	if s == nil {
		return ErrUnavailable
	}
	return s.startupErr
}

// ResolveStartup applies the only permitted startup precedence: a real
// environment value, the fixed listener file, then the source default.
func ResolveStartup(environmentValue, filePath string) (Resolution, error) {
	if filePath == "" {
		filePath = DefaultFilePath
	}
	if value := strings.TrimSpace(environmentValue); value != "" {
		address, err := ParseAddress(value)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Address: formatAddress(address), Source: SourceEnvironment}, nil
	}
	if !filepath.IsAbs(filePath) {
		return Resolution{}, fmt.Errorf("listener file path must be absolute")
	}
	snapshot, err := readFileSnapshot(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Resolution{Address: DefaultAddress, Source: SourceDefault, FileFingerprint: absentFingerprint}, nil
		}
		return Resolution{}, err
	}
	if !snapshot.Present {
		return Resolution{Address: DefaultAddress, Source: SourceDefault, FileFingerprint: absentFingerprint}, nil
	}
	return Resolution{Address: formatAddress(snapshot.Address), Source: SourceFile, FileFingerprint: snapshot.Fingerprint}, nil
}

// ParseAddress validates the existing loopback/exact-private bind contract.
// Hostnames, wildcard binds, public IPs, link-local values and multicast are
// intentionally rejected.
func ParseAddress(value string) (Address, error) {
	value = strings.TrimSpace(value)
	host, portText, err := net.SplitHostPort(value)
	if err != nil || host == "" || portText == "" {
		return Address{}, ErrInvalidRequest
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(value, "[") {
		return Address{}, ErrInvalidRequest
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Address{}, ErrInvalidRequest
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !allowedBindIP(ip) {
		return Address{}, ErrInvalidRequest
	}
	return Address{Host: canonicalIP(ip), Port: port}, nil
}

func ParseHost(value string) (string, error) {
	if len(value) == 0 || len(value) > maxHostBytes || strings.TrimSpace(value) != value || strings.ContainsAny(value, "[]:/\\") {
		return "", ErrInvalidRequest
	}
	ip := net.ParseIP(value)
	if ip == nil || !allowedBindIP(ip) {
		return "", ErrInvalidRequest
	}
	return canonicalIP(ip), nil
}

func (s *Service) Read(_ context.Context) (Projection, error) {
	if s == nil || s.startupErr != nil {
		return Projection{Editability: EditabilityUnavailable}, ErrUnavailable
	}
	projection, _, err := s.readProjection()
	return projection, err
}

func (s *Service) Preview(_ context.Context, binding, host string) (Preview, error) {
	if s == nil || binding == "" {
		return Preview{}, ErrInvalidRequest
	}
	projection, snapshot, err := s.readProjection()
	if err != nil {
		return Preview{}, err
	}
	if projection.Source == SourceEnvironment {
		return Preview{}, ErrEnvironmentOwned
	}
	if projection.Editability == EditabilityDriftDetected {
		return Preview{}, ErrDriftDetected
	}
	if projection.Editability != EditabilityEditable {
		return Preview{}, ErrUnavailable
	}
	afterHost, err := ParseHost(host)
	if err != nil {
		return Preview{}, ErrInvalidRequest
	}
	if afterHost != projection.Host && !contains(projection.AllowedHosts, afterHost) {
		return Preview{}, ErrInvalidRequest
	}
	before := Address{Host: projection.Host, Port: projection.Port}
	after := Address{Host: afterHost, Port: projection.Port}
	created := s.now().UTC()
	entry := previewEntry{
		Binding: binding, Before: before, After: after, Fingerprint: snapshot.Fingerprint,
		Source: projection.Source, Noop: before.Host == after.Host, ExpiresAt: created.Add(s.previewTTL),
	}
	token, err := s.randomToken()
	if err != nil {
		return Preview{}, ErrUnavailable
	}
	s.mu.Lock()
	s.purgeExpiredLocked(created)
	for oldToken, old := range s.previews {
		if old.Binding == binding {
			delete(s.previews, oldToken)
		}
	}
	for len(s.previews) >= s.maxPreviews {
		s.evictOldestLocked()
	}
	s.previews[token] = entry
	s.mu.Unlock()
	return Preview{
		Token: token, ExpiresAt: entry.ExpiresAt, Before: before, After: after, Noop: entry.Noop,
		ReconnectClassification: reconnectClassification(before.Host, after.Host), RestartRequired: !entry.Noop,
		SessionInvalidated: !entry.Noop, LoginRequired: !entry.Noop,
	}, nil
}

func (s *Service) Apply(ctx context.Context, binding, token string) (ApplyResult, error) {
	if s == nil || binding == "" || token == "" {
		return ApplyResult{}, ErrPreviewExpired
	}
	entry, ok := s.takePreview(binding, token)
	if !ok {
		return ApplyResult{}, ErrPreviewExpired
	}
	projection, snapshot, err := s.readProjection()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := validateEntry(projection, snapshot, entry); err != nil {
		return ApplyResult{}, err
	}
	if entry.Noop {
		return resultForEntry(entry, false, "no-op"), nil
	}
	if !s.tryBusy() {
		return ApplyResult{}, ErrBusy
	}
	if err := s.stage(snapshot, entry.After); err != nil {
		s.clearBusy()
		return ApplyResult{}, ErrUnavailable
	}
	release, err := s.beginLifecycle(ctx)
	if err != nil {
		_ = os.RemoveAll(s.stagingDir)
		s.clearBusy()
		return ApplyResult{}, err
	}
	projection, snapshot, err = s.readProjection()
	if err != nil {
		release()
		_ = os.RemoveAll(s.stagingDir)
		s.clearBusy()
		return ApplyResult{}, err
	}
	if err := validateEntry(projection, snapshot, entry); err != nil {
		release()
		_ = os.RemoveAll(s.stagingDir)
		s.clearBusy()
		return ApplyResult{}, err
	}
	if err := s.launchHelper(release); err != nil {
		_ = os.RemoveAll(s.stagingDir)
		s.clearBusy()
		return ApplyResult{}, ErrHelperStart
	}
	return resultForEntry(entry, true, "rebind-started"), nil
}

func (s *Service) Cancel(binding, token string) {
	if s == nil || binding == "" || token == "" {
		return
	}
	s.mu.Lock()
	if entry, ok := s.previews[token]; ok && entry.Binding == binding {
		delete(s.previews, token)
	}
	s.mu.Unlock()
}

func (s *Service) Invalidate(binding string) {
	if s == nil || binding == "" {
		return
	}
	s.mu.Lock()
	for token, entry := range s.previews {
		if entry.Binding == binding {
			delete(s.previews, token)
		}
	}
	s.mu.Unlock()
}

func (s *Service) InvalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.previews = make(map[string]previewEntry)
	s.mu.Unlock()
}

func (s *Service) readProjection() (Projection, fileSnapshot, error) {
	if s.startupErr != nil {
		return Projection{Editability: EditabilityUnavailable}, fileSnapshot{}, ErrUnavailable
	}
	active := s.active
	projection := Projection{Host: active.Host, Port: active.Port, Source: s.initial.Source}
	allowed, err := s.allowedHosts()
	if err != nil {
		projection.Editability = EditabilityUnavailable
		return projection, fileSnapshot{}, nil
	}
	projection.AllowedHosts = allowed
	if s.initial.Source == SourceEnvironment {
		projection.Editability = EditabilityEnvironmentOwned
		return projection, fileSnapshot{}, nil
	}
	snapshot, err := readFileSnapshot(s.filePath)
	if err != nil {
		projection.Editability = EditabilityDriftDetected
		return projection, snapshot, nil
	}
	if snapshot.Fingerprint != s.initial.FileFingerprint || (s.initial.Source == SourceFile && formatAddress(snapshot.Address) != s.initial.Address) {
		projection.Editability = EditabilityDriftDetected
		return projection, snapshot, nil
	}
	projection.Editability = EditabilityEditable
	return projection, snapshot, nil
}

func (s *Service) allowedHosts() ([]string, error) {
	addresses, err := s.interfaceAddrs()
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{"127.0.0.1": {}}
	for _, address := range addresses {
		ip := ipFromNetAddr(address)
		if ip == nil || !allowedBindIP(ip) {
			continue
		}
		canonical := canonicalIP(ip)
		if ip.IsLoopback() && canonical != "127.0.0.1" && canonical != "::1" {
			continue
		}
		set[canonical] = struct{}{}
	}
	// IPv6 loopback is part of the closed catalog when this build can parse
	// IPv6 listener values; it is never a wildcard or a link-local address.
	set["::1"] = struct{}{}
	result := make([]string, 0, len(set))
	for host := range set {
		result = append(result, host)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i] == "127.0.0.1" {
			return true
		}
		if result[j] == "127.0.0.1" {
			return false
		}
		if result[i] == "::1" {
			return true
		}
		if result[j] == "::1" {
			return false
		}
		return result[i] < result[j]
	})
	return result, nil
}

func (s *Service) stage(snapshot fileSnapshot, candidate Address) error {
	if err := os.RemoveAll(s.stagingDir); err != nil {
		return err
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(s.stagingDir, "candidate"), []byte(formatAddress(candidate)+"\n")); err != nil {
		return err
	}
	if snapshot.Present {
		if err := writePrivate(filepath.Join(s.stagingDir, "previous"), snapshot.Raw); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(s.stagingDir, "previous-absent"))
	} else if err := writePrivate(filepath.Join(s.stagingDir, "previous-absent"), []byte("absent\n")); err != nil {
		return err
	} else {
		_ = os.Remove(filepath.Join(s.stagingDir, "previous"))
	}
	return nil
}

func (s *Service) beginLifecycle(ctx context.Context) (func(), error) {
	if s.lifecycle == nil {
		return func() {}, nil
	}
	return s.lifecycle.BeginApply(ctx)
}

func (s *Service) launchHelper(release func()) error {
	if s.runHelper != nil {
		go func() {
			defer release()
			defer s.clearBusy()
			_ = s.runHelper(context.Background(), "rebind")
		}()
		return nil
	}
	command := execCommand(s.helperPath, "rebind")
	if err := command.Start(); err != nil {
		release()
		s.clearBusy()
		return err
	}
	go func() {
		defer release()
		defer s.clearBusy()
		_ = command.Wait()
	}()
	return nil
}

// command is wrapped behind a variable so package tests never need to create
// a shell or an arbitrary command runner.
type helperCommand interface {
	Start() error
	Wait() error
}

var execCommand = func(path string, args ...string) helperCommand {
	return exec.Command(path, args...)
}

func (s *Service) randomToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := io.ReadFull(s.random, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func (s *Service) takePreview(binding, token string) (previewEntry, bool) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	entry, ok := s.previews[token]
	if !ok || entry.Binding != binding || !now.Before(entry.ExpiresAt) {
		return previewEntry{}, false
	}
	delete(s.previews, token)
	return entry, true
}

func (s *Service) purgeExpiredLocked(now time.Time) {
	for token, entry := range s.previews {
		if !now.Before(entry.ExpiresAt) {
			delete(s.previews, token)
		}
	}
}

func (s *Service) evictOldestLocked() {
	var oldestToken string
	var oldest time.Time
	for token, entry := range s.previews {
		if oldestToken == "" || entry.ExpiresAt.Before(oldest) {
			oldestToken, oldest = token, entry.ExpiresAt
		}
	}
	if oldestToken != "" {
		delete(s.previews, oldestToken)
	}
}

func (s *Service) tryBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *Service) clearBusy() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func validateEntry(projection Projection, snapshot fileSnapshot, entry previewEntry) error {
	if projection.Source == SourceEnvironment {
		return ErrEnvironmentOwned
	}
	if projection.Editability == EditabilityDriftDetected {
		return ErrPreviewStale
	}
	if projection.Editability == EditabilityUnavailable {
		return ErrUnavailable
	}
	if projection.Editability != EditabilityEditable || projection.Source != entry.Source || snapshot.Fingerprint != entry.Fingerprint {
		return ErrPreviewStale
	}
	if projection.Host != entry.Before.Host || projection.Port != entry.Before.Port || (entry.After.Host != entry.Before.Host && !contains(projection.AllowedHosts, entry.After.Host)) {
		return ErrPreviewStale
	}
	return nil
}

func resultForEntry(entry previewEntry, accepted bool, state string) ApplyResult {
	return ApplyResult{
		Accepted: accepted, State: state, Before: entry.Before, After: entry.After, Noop: entry.Noop,
		ReconnectClassification: reconnectClassification(entry.Before.Host, entry.After.Host),
		RestartRequired:         !entry.Noop, SessionInvalidated: !entry.Noop, LoginRequired: !entry.Noop,
	}
}

func reconnectClassification(before, after string) string {
	if before == after {
		return "no-op"
	}
	beforeLoopback := net.ParseIP(before).IsLoopback()
	afterLoopback := net.ParseIP(after).IsLoopback()
	switch {
	case beforeLoopback && !afterLoopback:
		return "loopback-to-lan"
	case !beforeLoopback && afterLoopback:
		return "lan-to-loopback"
	default:
		return "lan-address-change"
	}
}

func readFileSnapshot(path string) (fileSnapshot, error) {
	if !filepath.IsAbs(path) {
		return fileSnapshot{}, ErrUnavailable
	}
	if err := validateParent(path); err != nil {
		return fileSnapshot{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileSnapshot{Fingerprint: absentFingerprint}, nil
		}
		return fileSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return fileSnapshot{}, ErrUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, MaxListenerFileBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return fileSnapshot{}, ErrUnavailable
	}
	if len(contents) > MaxListenerFileBytes {
		return fileSnapshot{}, ErrUnavailable
	}
	address, err := ParseAddress(string(contents))
	if err != nil {
		return fileSnapshot{}, ErrUnavailable
	}
	return fileSnapshot{Present: true, Address: address, Raw: append([]byte(nil), contents...), Mode: info.Mode().Perm(), Fingerprint: fingerprint(info, contents)}, nil
}

const absentFingerprint = "absent"

func fingerprint(info os.FileInfo, contents []byte) string {
	hash := sha256.Sum256(contents)
	return fmt.Sprintf("%d:%d:%o:%s", info.ModTime().UnixNano(), info.Size(), info.Mode().Perm(), hex.EncodeToString(hash[:]))
}

func validateParent(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnavailable
	}
	return nil
}

func writePrivate(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".listener-")
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
	return os.Rename(temporaryPath, path)
}

func allowedBindIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 10 || (v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31) || (v4[0] == 192 && v4[1] == 168)
	}
	v6 := ip.To16()
	return v6 != nil && v6[0]&0xfe == 0xfc
}

func canonicalIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.To16().String()
}

func formatAddress(address Address) string {
	return net.JoinHostPort(address.Host, strconv.Itoa(address.Port))
}

func ipFromNetAddr(address net.Addr) net.IP {
	switch value := address.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
