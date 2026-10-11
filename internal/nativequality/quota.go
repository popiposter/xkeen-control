package nativequality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
)

const (
	defaultQuotaPath = "/tmp/xkeen-control/native-quality-quota.json"
	// maxReservationBytes bounds any stored reservation; profiles reserve
	// their own Review().Bytes, never more than this.
	maxReservationBytes = 432 * resourcepolicy.MiB
)

var errQuota = errors.New("native quality quota unavailable")

// errQuotaBusy is a held receipt lock; it is still errQuota.
var errQuotaBusy = fmt.Errorf("%w: receipt lock held", errQuota)

// quotaReservation is a legacy daily-quota entry (REQ-006, superseded by the
// nightly window). Receipts written by older panels are still read and
// validated; new starts drop them.
type quotaReservation struct {
	At    time.Time `json:"at"`
	Bytes int64     `json:"bytes"`
}

type quotaReceipt struct {
	Version                 int                `json:"version"`
	Reservations            []quotaReservation `json:"reservations"`
	LastComparisonStartedAt time.Time          `json:"lastComparisonStartedAt,omitempty"`
	InspectionRequired      bool               `json:"inspectionRequired,omitempty"`
	FairCursor              int                `json:"fairCursor,omitempty"`
	EligibleSetHash         string             `json:"eligibleSetHash,omitempty"`
	// ProvisionalAt is when REQ-009 recovery applied an unranked pool. The
	// next complete automatic review that applies or keeps a measured pool
	// clears it. omitzero keeps receipts without a provisional pool readable
	// by v0.4.12, which rejects unknown fields (rollback target).
	ProvisionalAt time.Time `json:"provisionalAt,omitzero"`
}

type quotaView struct {
	LastStartedAt      time.Time
	InspectionRequired bool
	FairCursor         int
	ProvisionalAt      time.Time
}

func quotaState(path string, now time.Time, reviewBytes int64) (quotaView, error) {
	if path == "" {
		path = defaultQuotaPath
	}
	release, err := acquireQuotaLock(path)
	if err != nil {
		return quotaView{}, err
	}
	defer release()
	q, err := readQuotaLocked(path, now)
	if err != nil {
		return quotaView{}, err
	}
	return viewQuota(q, now, reviewBytes), nil
}

func viewQuota(q quotaReceipt, now time.Time, reviewBytes int64) quotaView {
	return quotaView{LastStartedAt: q.LastComparisonStartedAt, InspectionRequired: q.InspectionRequired, FairCursor: q.FairCursor, ProvisionalAt: q.ProvisionalAt}
}

// recordComparisonStart persists the floor before a manual transfer starts.
func recordComparisonStart(path string, now time.Time) error {
	if path == "" {
		path = defaultQuotaPath
	}
	release, err := acquireQuotaLock(path)
	if err != nil {
		return errQuota
	}
	defer release()
	return recordComparisonStartLocked(path, now, nil)
}

// recordComparisonStartLocked requires the quota lock held by the caller. A
// manual review also advances the shared fair cursor, so every review rotates.
func recordComparisonStartLocked(path string, now time.Time, plan *sweepPlan) error {
	q, err := readQuotaLocked(path, now)
	if err != nil || q.InspectionRequired {
		return errQuota
	}
	q.LastComparisonStartedAt = now
	if plan != nil {
		q.FairCursor = plan.NextCursor
		q.EligibleSetHash = plan.EligibleSetHash
	}
	return writeQuotaLocked(path, q)
}

// settleSweepLocked requires the fixed quota lock retained by the review owner.
// An unknown or interrupted review leaves the durable inspection intent set.
// ranked clears a provisional recovery label.
func settleSweepLocked(path string, now time.Time, inspected, ranked bool) error {
	q, err := readQuotaLocked(path, now)
	if err != nil {
		return errQuota
	}
	if inspected {
		q.InspectionRequired = false
		if ranked {
			q.ProvisionalAt = time.Time{}
		}
	}
	return writeQuotaLocked(path, q)
}

func readQuotaLocked(path string, now time.Time) (quotaReceipt, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return quotaReceipt{}, errQuota
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !quotaOwnerOK(info) {
		return quotaReceipt{}, errQuota
	}
	q := quotaReceipt{Version: 1}
	if info, err = os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 || !quotaOwnerOK(info) {
			return quotaReceipt{}, errQuota
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return quotaReceipt{}, errQuota
		}
		decoder := json.NewDecoder(io.LimitReader(f, 4097))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&q)
		var trailing any
		endErr := decoder.Decode(&trailing)
		closeErr := f.Close()
		if decodeErr != nil || endErr != io.EOF || closeErr != nil {
			return quotaReceipt{}, errQuota
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return quotaReceipt{}, errQuota
	}
	if q.Version != 1 || len(q.Reservations) > 2 {
		return quotaReceipt{}, errQuota
	}
	if q.FairCursor < 0 || q.FairCursor > c1.MaxRegistryNodes || q.EligibleSetHash == "" && q.FairCursor != 0 {
		return quotaReceipt{}, errQuota
	}
	if q.EligibleSetHash != "" {
		decoded, decodeErr := hex.DecodeString(q.EligibleSetHash)
		if decodeErr != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != q.EligibleSetHash {
			return quotaReceipt{}, errQuota
		}
	}
	if !q.LastComparisonStartedAt.IsZero() && q.LastComparisonStartedAt.After(now) || q.ProvisionalAt.After(now) {
		return quotaReceipt{}, errQuota
	}
	for _, r := range q.Reservations {
		if r.At.IsZero() || r.At.After(now) || r.Bytes <= 0 || r.Bytes > maxReservationBytes {
			return quotaReceipt{}, errQuota
		}
	}
	return q, nil
}

func writeQuotaLocked(path string, q quotaReceipt) error {
	dir := filepath.Dir(path)
	b, err := json.Marshal(q)
	if err != nil {
		return errQuota
	}
	f, err := os.CreateTemp(dir, ".quality-quota-*")
	if err != nil {
		return errQuota
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return errQuota
	}
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), path) != nil {
		return errQuota
	}
	d, err := os.Open(dir)
	if err != nil {
		return errQuota
	}
	syncErr = d.Sync()
	closeErr = d.Close()
	if syncErr != nil || closeErr != nil {
		return errQuota
	}
	return nil
}

// recordAutomaticStartLocked records an automatic review start and sets the
// durable intent that an unknown Apply outcome leaves for inspection. It
// reserves no traffic: the nightly window runs once a day by construction.
func recordAutomaticStartLocked(path string, now time.Time) error {
	q, err := readQuotaLocked(path, now)
	if err != nil || q.InspectionRequired {
		return errQuota
	}
	q.LastComparisonStartedAt = now
	q.InspectionRequired = true
	q.Reservations = nil
	return writeQuotaLocked(path, q)
}
