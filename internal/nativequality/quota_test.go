//go:build linux

package nativequality

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// startAutomatic takes the receipt lock and records an automatic start.
func startAutomatic(path string, now time.Time) error {
	release, err := acquireQuotaLock(path)
	if err != nil {
		return err
	}
	defer release()
	return recordAutomaticStartLocked(path, now)
}

// An automatic start persists the inspection intent and drops legacy daily
// reservations; once settled, the next night starts with no quota or gap.
func TestAutomaticStartPersistsIntentWithoutQuota(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	now := time.Now().UTC().Add(-time.Hour)
	release, err := acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := quotaReceipt{Version: 1, Reservations: []quotaReservation{{At: now.Add(-time.Minute), Bytes: testReviewBytes}}}
	if err := writeQuotaLocked(path, legacy); err != nil {
		t.Fatal(err)
	}
	release()
	if err := startAutomatic(path, now); err != nil {
		t.Fatal(err)
	}
	if q, err := quotaState(path, now.Add(time.Second), testReviewBytes); err != nil || !q.InspectionRequired || !q.LastStartedAt.Equal(now) {
		t.Fatalf("automatic start omitted intent: %+v %v", q, err)
	}
	if err := startAutomatic(path, now.Add(time.Minute)); err == nil {
		t.Fatal("a start passed an unsettled inspection intent")
	}
	release, err = acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if q, _ := readQuotaLocked(path, now); len(q.Reservations) != 0 {
		t.Fatal("legacy reservations kept", q.Reservations)
	}
	if err := settleSweepLocked(path, now, true, false); err != nil {
		t.Fatal(err)
	}
	release()
	if err := startAutomatic(path, now.Add(2*time.Minute)); err != nil {
		t.Fatal("a settled receipt refused the next start", err)
	}
}

func TestManualStartDoesNotPostponeTheNightlyReview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	now := time.Now().UTC().Add(-time.Hour)
	if err := recordComparisonStart(path, now); err != nil {
		t.Fatal(err)
	}
	if q, err := quotaState(path, now.Add(time.Second), testReviewBytes); err != nil || !q.LastStartedAt.Equal(now) || q.InspectionRequired {
		t.Fatalf("manual start not represented in receipt: %+v %v", q, err)
	}
	if err := startAutomatic(path, now.Add(time.Minute)); err != nil {
		t.Fatal("manual start postponed the automatic review", err)
	}
}

func TestQuotaLockExcludesConcurrentReviewOwners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	release, err := acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireQuotaLock(path); err == nil {
		t.Fatal("second review acquired fixed lock")
	}
	if err := startAutomatic(path, time.Now().UTC()); err == nil {
		t.Fatal("start bypassed active owner")
	}
	release()
	var wg sync.WaitGroup
	start := make(chan struct{})
	accepted := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			accepted <- startAutomatic(path, time.Now().UTC()) == nil
		}()
	}
	close(start)
	wg.Wait()
	close(accepted)
	count := 0
	for ok := range accepted {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("concurrent automatic starts: %d", count)
	}
}

func TestSweepQuotaCorruptionAndSymlinkFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quota.json")
	if os.WriteFile(path, []byte("{broken"), 0600) != nil {
		t.Fatal("fixture")
	}
	if err := startAutomatic(path, time.Now()); err == nil {
		t.Fatal("corruption admitted")
	}
	if _, err := quotaState(path, time.Now(), testReviewBytes); err == nil {
		t.Fatal("corrupt receipt exposed an empty receipt")
	}
	if os.Remove(path) != nil || os.Symlink(filepath.Join(dir, "target"), path) != nil {
		t.Fatal("fixture")
	}
	if err := startAutomatic(path, time.Now()); err == nil {
		t.Fatal("symlink admitted")
	}
}

func TestRankedSettleClearsProvisionalLabel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota", "receipt.json")
	now := time.Now().UTC()
	release, err := acquireQuotaLock(path)
	if err != nil {
		t.Skip("quota lock unavailable on this platform")
	}
	defer release()
	if err := writeQuotaLocked(path, quotaReceipt{Version: 1, ProvisionalAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := settleSweepLocked(path, now, true, false); err != nil {
		t.Fatal(err)
	}
	if q, _ := readQuotaLocked(path, now); q.ProvisionalAt.IsZero() {
		t.Fatal("an unranked settle cleared the provisional label")
	}
	// A receipt without a provisional pool stays readable by v0.4.12.
	if err := writeQuotaLocked(path, quotaReceipt{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "provisionalAt") {
		t.Fatal("a zero provisionalAt was written", string(raw))
	}
	if err := writeQuotaLocked(path, quotaReceipt{Version: 1, ProvisionalAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := settleSweepLocked(path, now, false, true); err != nil {
		t.Fatal(err)
	}
	if q, _ := readQuotaLocked(path, now); q.ProvisionalAt.IsZero() {
		t.Fatal("an uninspected settle cleared the provisional label")
	}
	if err := settleSweepLocked(path, now, true, true); err != nil {
		t.Fatal(err)
	}
	if q, _ := readQuotaLocked(path, now); !q.ProvisionalAt.IsZero() {
		t.Fatal("a ranked review left the provisional label")
	}
}

// A held receipt lock is reported as busy, distinct from a broken receipt,
// both directly and through quotaState and a review start.
func TestHeldQuotaLockIsBusyNotBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota", "receipt.json")
	release, err := acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := acquireQuotaLock(path); !errors.Is(err, errQuotaBusy) || !errors.Is(err, errQuota) {
		t.Fatal("held lock", err)
	}
	if _, err := quotaState(path, time.Now().UTC(), 1); !errors.Is(err, errQuotaBusy) {
		t.Fatal("quotaState under a held lock", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := quotaState(path, time.Now().UTC(), 1); err == nil || errors.Is(err, errQuotaBusy) {
		t.Fatal("broken receipt directory reported as busy", err)
	}
}
