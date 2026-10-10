//go:build linux

package nativequality

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSweepQuotaPersistsWorstCaseAndRefusesASecondReviewInTheDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	now := time.Now().UTC()
	if used, err := reserveSweep(path, now, testReviewBytes); err != nil || used != testReviewBytes {
		t.Fatalf("first reservation: %d %v", used, err)
	}
	if q, err := quotaState(path, now.Add(time.Second), testReviewBytes); err != nil || q.UsedBytes != testReviewBytes || q.RemainingBytes != 0 || q.ReviewsUsed != 1 || !q.NextResetAt.Equal(now.Add(24*time.Hour)) || !q.InspectionRequired {
		t.Fatalf("quota status omitted reservation or intent: %+v %v", q, err)
	}
	release, err := acquireQuotaLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := settleSweepLocked(path, now, true, false); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := reserveSweep(path, now.Add(12*time.Hour), testReviewBytes); err == nil {
		t.Fatal("second review escaped the rolling one-review cap")
	}
	if used, err := reserveSweep(path, now.Add(24*time.Hour), testReviewBytes); err != nil || used != testReviewBytes {
		t.Fatalf("expired reservation not retired: %d %v", used, err)
	}
}

func TestManualComparisonStartPersistsAcrossServiceLifetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "quota.json")
	now := time.Now().UTC()
	if err := recordComparisonStart(path, now); err != nil {
		t.Fatal(err)
	}
	q, err := quotaState(path, now.Add(time.Second), testReviewBytes)
	if err != nil || !q.LastStartedAt.Equal(now) || q.UsedBytes != 0 || q.RemainingBytes != (dailyReviews*testReviewBytes) {
		t.Fatalf("manual start not represented in receipt: %+v %v", q, err)
	}
	if _, err := reserveSweep(path, now.Add(time.Hour), testReviewBytes); err == nil {
		t.Fatal("manual start did not postpone automatic review")
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
	if _, err := reserveSweep(path, time.Now(), testReviewBytes); err == nil {
		t.Fatal("reservation bypassed active owner")
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
			_, e := reserveSweep(path, time.Now().UTC(), testReviewBytes)
			accepted <- e == nil
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
	if count < 1 || count > 2 {
		t.Fatalf("concurrent reservations: %d", count)
	}
}

func TestSweepQuotaCorruptionAndSymlinkFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quota.json")
	if os.WriteFile(path, []byte("{broken"), 0600) != nil {
		t.Fatal("fixture")
	}
	if _, err := reserveSweep(path, time.Now(), testReviewBytes); err == nil {
		t.Fatal("corruption admitted")
	}
	if _, err := quotaState(path, time.Now(), testReviewBytes); err == nil {
		t.Fatal("corrupt receipt exposed zero quota")
	}
	if os.Remove(path) != nil || os.Symlink(filepath.Join(dir, "target"), path) != nil {
		t.Fatal("fixture")
	}
	if _, err := reserveSweep(path, time.Now(), testReviewBytes); err == nil {
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
