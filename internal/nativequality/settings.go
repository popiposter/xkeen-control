package nativequality

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultSettingsPath is the operator's nightly maintenance settings. Older
// panels ignore it; a missing or invalid file means the defaults.
const DefaultSettingsPath = "/opt/etc/xkeen-control/state/maintenance.json"

const maxSettingsBytes = 1024

// Settings are the operator-tunable nightly maintenance choices. Everything
// else (batches, traffic and time budgets, probe timeouts) derives from the
// hardware profile and the speed-test breadth.
type Settings struct {
	// Enabled runs the nightly window at all.
	Enabled bool `json:"enabled"`
	// Window is the local start time, "HH:MM".
	Window string `json:"window"`
	// RefreshSubscriptions, Review and UpdateGeodata are the window steps.
	// With a step off nothing refreshes or updates it automatically.
	RefreshSubscriptions bool `json:"refreshSubscriptions"`
	Review               bool `json:"review"`
	UpdateGeodata        bool `json:"updateGeodata"`
	// AutoApply applies the best pool; off leaves a recommendation.
	AutoApply bool `json:"autoApply"`
	// SpeedCandidates is how many of the fastest answering nodes are
	// speed-tested; PoolSize is how many form the pool.
	SpeedCandidates int `json:"speedCandidates"`
	PoolSize        int `json:"poolSize"`
	// Recovery runs the ten-minute no-healthy-member check (REQ-009).
	Recovery bool `json:"recovery"`
	// Report sends the Telegram summary after the window.
	Report bool `json:"report"`
}

// Settings bounds. The pool may not exceed the six exact anchored balancer
// members the native quality editor supports; eighteen candidates fit the
// review budgets on both profiles.
const (
	MinSpeedCandidates = 2
	MaxSpeedCandidates = 18
	MinPoolSize        = 2
	MaxPoolSize        = 6
)

func DefaultSettings() Settings {
	return Settings{Enabled: true, Window: "04:00", RefreshSubscriptions: true, Review: true, UpdateGeodata: true, AutoApply: true, SpeedCandidates: MaxSpeedCandidates, PoolSize: MaxPoolSize, Recovery: true, Report: true}
}

var errSettings = errors.New("maintenance settings rejected")

// Validate checks the bounds and returns the window as a time-of-day offset.
func (v Settings) Validate() (time.Duration, error) {
	window, ok := ParseWindow(v.Window)
	if !ok || v.SpeedCandidates < MinSpeedCandidates || v.SpeedCandidates > MaxSpeedCandidates || v.PoolSize < MinPoolSize || v.PoolSize > MaxPoolSize || v.SpeedCandidates < v.PoolSize {
		return 0, errSettings
	}
	return window, nil
}

// ParseWindow parses an "HH:MM" local start time.
func ParseWindow(value string) (time.Duration, bool) {
	hours, minutes, found := strings.Cut(value, ":")
	if !found || len(hours) != 2 || len(minutes) != 2 {
		return 0, false
	}
	h, errH := strconv.Atoi(hours)
	m, errM := strconv.Atoi(minutes)
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, true
}

// SettingsStore persists Settings in one small private file and keeps the
// current value in RAM. Reads never fail: a missing or invalid file yields
// the defaults.
type SettingsStore struct {
	Path string
	mu   sync.Mutex
	// loaded caches the value; zero until the first read.
	loaded  bool
	current Settings
	// changes wakes the schedule after a save.
	changes chan struct{}
}

func NewSettingsStore(path string) *SettingsStore {
	if path == "" {
		path = DefaultSettingsPath
	}
	return &SettingsStore{Path: path, changes: make(chan struct{}, 1)}
}

// Get returns the current settings, defaults when unset or unreadable.
func (s *SettingsStore) Get() Settings {
	if s == nil {
		return DefaultSettings()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		// An unreadable file (for example /opt not yet mounted) is retried on
		// the next read instead of fixing the defaults for the process.
		value, ok := readSettings(s.Path)
		if !ok {
			return value
		}
		s.current, s.loaded = value, true
	}
	return s.current
}

// Set validates and durably saves the settings, then wakes the schedule.
func (s *SettingsStore) Set(value Settings) (Settings, error) {
	if s == nil {
		return Settings{}, errSettings
	}
	if _, err := value.Validate(); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	err := writeSettings(s.Path, value)
	if err == nil {
		s.current, s.loaded = value, true
	}
	s.mu.Unlock()
	if err != nil {
		return Settings{}, err
	}
	select {
	case s.changes <- struct{}{}:
	default:
	}
	return value, nil
}

// Changes is signalled after every successful save.
func (s *SettingsStore) Changes() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.changes
}

// readSettings returns the stored settings and whether the file was settled:
// absent or present but invalid (both mean the defaults for good), as opposed
// to a transient read failure worth retrying.
func readSettings(path string) (Settings, bool) {
	defaults := DefaultSettings()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, true
	}
	if err != nil {
		return defaults, false
	}
	if !info.Mode().IsRegular() || info.Size() > maxSettingsBytes || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 || !settingsOwnerOK(info) {
		return defaults, true
	}
	file, err := os.Open(path)
	if err != nil {
		return defaults, false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return defaults, false
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxSettingsBytes+1))
	decoder.DisallowUnknownFields()
	value := defaults
	if decoder.Decode(&value) != nil {
		return defaults, true
	}
	var trailing json.RawMessage
	if decoder.Decode(&trailing) != io.EOF {
		return defaults, true
	}
	if _, err := value.Validate(); err != nil {
		return defaults, true
	}
	return value, true
}

// ErrSettingsStorage is a settings save that failed on disk, not on input.
var ErrSettingsStorage = errors.New("maintenance settings could not be saved")

func writeSettings(path string, value Settings) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ErrSettingsStorage
	}
	// The directory must be private and the target, when present, a plain
	// owned file: never follow a planted link.
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 || !settingsOwnerOK(info) {
		return ErrSettingsStorage
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || !settingsOwnerOK(info)) {
		return ErrSettingsStorage
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ErrSettingsStorage
	}
	f, err := os.CreateTemp(dir, ".maintenance-*")
	if err != nil {
		return ErrSettingsStorage
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return ErrSettingsStorage
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), path) != nil {
		return ErrSettingsStorage
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
