//go:build linux

package nativequality

import (
	"context"
	"testing"
	"time"
)

func TestRecoveryTickStartsOneMeasuredProvisionalRecheck(t *testing.T) {
	s, _, _, _ := sweepFixture(t, true)
	now := time.Now().UTC()
	s.nextMaintenance = now.Add(20 * time.Hour)
	q := quotaReceipt{Version: 1, LastComparisonStartedAt: now.Add(-3 * time.Hour), ProvisionalAt: now.Add(-time.Hour - time.Minute), RecoveryAppliedAt: now.Add(-time.Hour - time.Minute)}
	unlock, err := acquireQuotaLock(s.QuotaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeQuotaLocked(s.QuotaPath, q); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	s.recoverPool(context.Background())
	s.mu.Lock()
	done := s.done
	trigger := s.status.ReviewTrigger
	s.mu.Unlock()
	if done == nil || trigger != "provisional-recheck" {
		t.Fatalf("recovery did not start measured recheck: trigger=%q done=%t", trigger, done != nil)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("synthetic recheck did not settle")
	}
	q, err = readQuotaLocked(s.QuotaPath, time.Now().UTC())
	if err != nil || !q.LastComparisonStartedAt.After(q.ProvisionalAt) {
		t.Fatalf("started recheck lacks durable once-per-episode receipt: %+v %v", q, err)
	}
	s.recoverPool(context.Background())
	if got := s.Read().ReviewTrigger; got != "provisional-recheck" {
		t.Fatalf("second tick replaced review trigger: %q", got)
	}
}
