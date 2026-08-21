package c1

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultStateDir      = "/opt/etc/xkeen-control/state"
	DefaultSelectionPath = DefaultStateDir + "/selection.json"
	DefaultBenchmarkPath = DefaultStateDir + "/benchmark.json"
	DefaultTransientDir  = "/tmp/xkeen-control/benchmark"
	maxStateBytes        = 32 << 10
)

type SelectionRecord struct {
	Target                  string    `json:"target"`
	ManualOverride          string    `json:"manualOverride,omitempty"`
	StableSince             time.Time `json:"stableSince"`
	LastSwitchReason        string    `json:"lastSwitchReason"`
	LastSwitchAt            time.Time `json:"lastSwitchAt"`
	LastBenchmarkGeneration uint64    `json:"lastBenchmarkGeneration,omitempty"`
}

type SelectionStore struct {
	Path string
}

func (s SelectionStore) Load() (SelectionRecord, error) {
	if s.Path == "" {
		s.Path = DefaultSelectionPath
	}
	contents, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SelectionRecord{}, nil
		}
		return SelectionRecord{}, errors.New("selection state unavailable")
	}
	if len(contents) > maxStateBytes {
		return SelectionRecord{}, errors.New("selection state is too large")
	}
	var record SelectionRecord
	if err := json.Unmarshal(contents, &record); err != nil {
		return SelectionRecord{}, errors.New("selection state is invalid")
	}
	if (record.Target != "" && !validTag(record.Target)) || (record.ManualOverride != "" && !validTag(record.ManualOverride)) {
		return SelectionRecord{}, errors.New("selection state is invalid")
	}
	return record, nil
}

func (s SelectionStore) SaveIfChanged(previous, next SelectionRecord) (bool, error) {
	if previous.Target == next.Target && previous.ManualOverride == next.ManualOverride && previous.StableSince.Equal(next.StableSince) && previous.LastSwitchReason == next.LastSwitchReason && previous.LastSwitchAt.Equal(next.LastSwitchAt) && previous.LastBenchmarkGeneration == next.LastBenchmarkGeneration {
		return false, nil
	}
	if next.Target != "" && !validTag(next.Target) {
		return false, errors.New("selection target is invalid")
	}
	if next.ManualOverride != "" && !validTag(next.ManualOverride) {
		return false, errors.New("manual override target is invalid")
	}
	if s.Path == "" {
		s.Path = DefaultSelectionPath
	}
	if err := atomicPrivateWrite(s.Path, next); err != nil {
		return false, err
	}
	return true, nil
}

type BenchmarkSnapshot struct {
	Generation       uint64                      `json:"generation"`
	CompletedAt      time.Time                   `json:"completedAt"`
	EligibleNodes    int                         `json:"eligibleNodes"`
	ValidSamples     int                         `json:"validSamples"`
	AggregateBytes   int64                       `json:"aggregateBytes"`
	DurationMS       int64                       `json:"durationMs"`
	PayloadBytes     int64                       `json:"payloadBytes"`
	PerNodeTimeoutMS int64                       `json:"perNodeTimeoutMs"`
	ResultClass      string                      `json:"resultClass"`
	SelectionTarget  string                      `json:"selectionTarget,omitempty"`
	Samples          map[string]ThroughputStatus `json:"samples,omitempty"`
}

type BenchmarkStore struct {
	Path string
}

func (s BenchmarkStore) Load() (BenchmarkSnapshot, error) {
	if s.Path == "" {
		s.Path = DefaultBenchmarkPath
	}
	contents, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return BenchmarkSnapshot{}, nil
		}
		return BenchmarkSnapshot{}, errors.New("benchmark state unavailable")
	}
	if len(contents) > maxStateBytes {
		return BenchmarkSnapshot{}, errors.New("benchmark state is too large")
	}
	var snapshot BenchmarkSnapshot
	if err := json.Unmarshal(contents, &snapshot); err != nil || snapshot.EligibleNodes < 0 || snapshot.ValidSamples < 0 || snapshot.AggregateBytes < 0 || snapshot.DurationMS < 0 || (snapshot.SelectionTarget != "" && !validTag(snapshot.SelectionTarget)) || !validThroughputStatuses(snapshot.Samples) {
		return BenchmarkSnapshot{}, errors.New("benchmark state is invalid")
	}
	return snapshot, nil
}

func (s BenchmarkStore) Save(snapshot BenchmarkSnapshot) error {
	if snapshot.EligibleNodes < 0 || snapshot.ValidSamples < 0 || snapshot.AggregateBytes < 0 || snapshot.DurationMS < 0 || (snapshot.SelectionTarget != "" && !validTag(snapshot.SelectionTarget)) || !validThroughputStatuses(snapshot.Samples) {
		return errors.New("benchmark snapshot is invalid")
	}
	if s.Path == "" {
		s.Path = DefaultBenchmarkPath
	}
	return atomicPrivateWrite(s.Path, snapshot)
}

func validThroughputStatuses(samples map[string]ThroughputStatus) bool {
	for tag, sample := range samples {
		if !validTag(tag) || sample.BytesPerSecond < 0 || math.IsNaN(sample.BytesPerSecond) || math.IsInf(sample.BytesPerSecond, 0) {
			return false
		}
	}
	return true
}

func atomicPrivateWrite(path string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("state encoding failed")
	}
	contents = append(contents, '\n')
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".xkeen-state-*")
	if err != nil {
		return errors.New("state temporary file unavailable")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return errors.New("state permissions unavailable")
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return errors.New("state write failed")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("state sync failed")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("state close failed")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("state replace failed")
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func ensurePrivateDir(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("state directory is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("state directory is unavailable")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("state directory creation failed")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("state directory is unsafe")
	}
	_ = os.Chmod(path, 0o700)
	return nil
}

func stateEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
