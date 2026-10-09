package nativequality

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

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
	Version      int                `json:"version"`
	Reservations []quotaReservation `json:"reservations"`
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, errQuota
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !quotaOwnerOK(info) {
		return 0, errQuota
	}
	q := quotaReceipt{Version: 1}
	if info, err = os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 || !quotaOwnerOK(info) {
			return 0, errQuota
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return 0, errQuota
		}
		decoder := json.NewDecoder(io.LimitReader(f, 4097))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&q)
		var trailing any
		endErr := decoder.Decode(&trailing)
		closeErr := f.Close()
		if decodeErr != nil || endErr != io.EOF || closeErr != nil {
			return 0, errQuota
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, errQuota
	}
	if q.Version != 1 || len(q.Reservations) > 2 {
		return 0, errQuota
	}
	kept := make([]quotaReservation, 0, 2)
	var used int64
	for _, r := range q.Reservations {
		if r.At.IsZero() || r.At.After(now) || r.Bytes != maxSweepBytes {
			return 0, errQuota
		}
		if now.Sub(r.At) < 24*time.Hour {
			kept = append(kept, r)
			used += r.Bytes
		}
	}
	if used > maxDailySweepBytes-maxSweepBytes || len(kept) >= 2 {
		return used, errQuota
	}
	q.Reservations = append(kept, quotaReservation{At: now, Bytes: maxSweepBytes})
	b, err := json.Marshal(q)
	if err != nil {
		return used, errQuota
	}
	f, err := os.CreateTemp(dir, ".quality-quota-*")
	if err != nil {
		return used, errQuota
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), path) != nil {
		return used, errQuota
	}
	d, err := os.Open(dir)
	if err != nil {
		return used, errQuota
	}
	syncErr = d.Sync()
	closeErr = d.Close()
	if syncErr != nil || closeErr != nil {
		return used, errQuota
	}
	return used + maxSweepBytes, nil
}
