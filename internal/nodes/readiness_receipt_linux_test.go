//go:build linux

package nodes

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestReadinessFailureClassesSurviveRollbackAndVerification(t *testing.T) {
	for _, reason := range []string{"deadline", "canceled", "unavailable", "unsupported", "permission", "protocol"} {
		t.Run(reason, func(t *testing.T) {
			cause := error(nil)
			if reason == "deadline" || reason == "unavailable" {
				cause = context.DeadlineExceeded
			}
			if reason == "canceled" {
				cause = context.Canceled
			}
			a := &fakeActivator{readyErr: &readinessError{reason: reason, cause: cause}}
			tx, next, _, _ := rollbackFixture(t, a)
			tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
			if err := tx.Apply(context.Background(), next); !errors.Is(err, ErrNodeRecoveryRequired) {
				t.Fatal(err)
			}
			rc, err := readRecoveryReceipt(tx.PreviousDir)
			expected := "readiness:" + reason
			if err != nil || rc.Branch != "previous" || rc.Phase != "inspection-required" || rc.Reason != reason || rc.ActivationFailure != expected || rc.RollbackFailure != expected || a.restarts != 2 {
				t.Fatal(rc, err, a.restarts)
			}
			a.readyErr = nil
			m := NewManager(Config{Store: tx.Store, Transaction: tx})
			view, err := m.InspectRecovery(context.Background(), activatorRuntime{a})
			if err != nil || !view.CanVerify {
				t.Fatal(view, err)
			}
			if err = m.VerifyExistingRecovery(context.Background(), view.Digest, activatorRuntime{a}); err != nil {
				t.Fatal(err)
			}
			rc, err = readRecoveryReceipt(tx.PreviousDir)
			if err != nil || rc.Phase != "completed" || rc.ActivationFailure != expected || rc.RollbackFailure != expected || a.restarts != 2 {
				t.Fatal(rc, err)
			}
		})
	}
}
func TestReadinessReceiptCodesAreStageRestricted(t *testing.T) {
	for _, reason := range []string{"unavailable", "unsupported", "permission", "protocol"} {
		if !validRecoveryReason("readiness", reason) || !validFailureCode("readiness:"+reason) || validRecoveryReason("restart", reason) || validFailureCode("restart:"+reason) {
			t.Fatal(reason)
		}
	}
	if validRecoveryReason("readiness", "raw PRIVATE details") || validFailureCode("readiness:raw PRIVATE details") {
		t.Fatal("arbitrary reason accepted")
	}
	if !validRecoveryReason("validation", "failed") {
		t.Fatal("historical validation reason rejected")
	}
}
