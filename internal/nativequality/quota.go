package nativequality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
)

const (
	defaultQuotaPath   = "/tmp/xkeen-control/native-quality-quota.json"
	maxSweepBytes      = 144 * resourcepolicy.MiB
	maxDailySweepBytes = 288 * resourcepolicy.MiB
)

var errQuota = errors.New("native quality quota unavailable")

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
}

type quotaView struct {
	UsedBytes          int64
	RemainingBytes     int64
	ReviewsUsed        int
	NextResetAt        time.Time
	LastStartedAt      time.Time
	InspectionRequired bool
	FairCursor         int
}

func quotaState(path string, now time.Time) (quotaView, error) {
	if path == "" {
		path = defaultQuotaPath
	}
	release, err := acquireQuotaLock(path)
	if err != nil {
		return quotaView{}, errQuota
	}
	defer release()
	q, err := readQuotaLocked(path, now)
	if err != nil {
		return quotaView{}, err
	}
	return viewQuota(q, now), nil
}

func viewQuota(q quotaReceipt, now time.Time) quotaView {
	v := quotaView{RemainingBytes: maxDailySweepBytes, LastStartedAt: q.LastComparisonStartedAt, InspectionRequired: q.InspectionRequired, FairCursor: q.FairCursor}
	for _, r := range q.Reservations {
		if now.Sub(r.At) < 24*time.Hour {
			v.UsedBytes += r.Bytes
			v.ReviewsUsed++
			if v.NextResetAt.IsZero() || r.At.Add(24*time.Hour).Before(v.NextResetAt) {
				v.NextResetAt = r.At.Add(24 * time.Hour)
			}
		}
	}
	v.RemainingBytes -= v.UsedBytes
	return v
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
	q, err := readQuotaLocked(path, now)
	if err != nil || q.InspectionRequired {
		return errQuota
	}
	q.LastComparisonStartedAt = now
	return writeQuotaLocked(path, q)
}

// settleSweepLocked requires the fixed quota lock retained by the review owner.
// An unknown or interrupted review leaves the durable inspection intent set.
func settleSweepLocked(path string, now time.Time, inspected bool) error {
	q, err := readQuotaLocked(path, now)
	if err != nil {
		return errQuota
	}
	if inspected {
		q.InspectionRequired = false
	}
	return writeQuotaLocked(path, q)
}

// reserveSweep charges the full worst-case transfer before the first byte is
// sent. A crash or unknown outcome therefore cannot reset the rolling cap.
// Reservations are deliberately not refunded after a partial review.
func reserveSweep(path string, now time.Time) (int64, error) {
	if path == "" {
		path = defaultQuotaPath
	}
	release, err := acquireQuotaLock(path)
	if err != nil {
		return 0, errQuota
	}
	defer release()
	return reserveSweepLocked(path, now)
}

// Caller holds the separate fixed-inode quota lock. The sweep retains that
// lock until all measurement and native application readback has settled.
func reserveSweepLocked(path string, now time.Time) (int64, error) {
	return reserveSweepPlannedLocked(path, now, nil)
}

// A planned reservation charges traffic, records the recovery intent and
// advances fairness in one private durable write before any transfer starts.
func reserveSweepPlannedLocked(path string, now time.Time, plan *sweepPlan) (int64, error) {
	q, err := readQuotaLocked(path, now)
	if err != nil {
		return 0, errQuota
	}
	if q.InspectionRequired || (!q.LastComparisonStartedAt.IsZero() && now.Sub(q.LastComparisonStartedAt) < 6*time.Hour) {
		return 0, errQuota
	}
	v := viewQuota(q, now)
	if v.UsedBytes > maxDailySweepBytes-maxSweepBytes || v.ReviewsUsed >= 2 {
		return v.UsedBytes, errQuota
	}
	kept := make([]quotaReservation, 0, 2)
	for _, r := range q.Reservations {
		if now.Sub(r.At) < 24*time.Hour {
			kept = append(kept, r)
		}
	}
	q.Reservations = append(kept, quotaReservation{At: now, Bytes: maxSweepBytes})
	q.LastComparisonStartedAt = now
	q.InspectionRequired = true
	if plan != nil {
		q.FairCursor = plan.NextCursor
		q.EligibleSetHash = plan.EligibleSetHash
	}
	if err := writeQuotaLocked(path, q); err != nil {
		return v.UsedBytes, errQuota
	}
	return v.UsedBytes + maxSweepBytes, nil
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
	if !q.LastComparisonStartedAt.IsZero() && q.LastComparisonStartedAt.After(now) {
		return quotaReceipt{}, errQuota
	}
	for _, r := range q.Reservations {
		if r.At.IsZero() || r.At.After(now) || r.Bytes != maxSweepBytes {
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
