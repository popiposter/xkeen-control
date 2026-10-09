//go:build linux

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestActivationBranchTimingsPersistThroughVerification(t *testing.T) {
	a := &fakeActivator{readyErr: &readinessError{reason: "unavailable"}}
	tx, next, _, _ := rollbackFixture(t, a)
	tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
	if err := tx.Apply(context.Background(), next); !errors.Is(err, ErrNodeRecoveryRequired) {
		t.Fatal(err)
	}
	before, err := readRecoveryReceipt(tx.PreviousDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, timing := range []*StageTimings{before.ActivationTiming, before.RollbackTiming} {
		if timing == nil || timing.RestartMS == nil || timing.ReadinessMS == nil || timing.ReadinessBudgetMS == nil || timing.InventoryMS != nil {
			t.Fatal("visited stages lost", timing)
		}
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
	after, err := readRecoveryReceipt(tx.PreviousDir)
	if err != nil || !reflect.DeepEqual(before.ActivationTiming, after.ActivationTiming) || !reflect.DeepEqual(before.RollbackTiming, after.RollbackTiming) || a.restarts != 2 {
		t.Fatal("verification changed history", err)
	}
}
func TestActivationTimingSuccessAndStrictDecode(t *testing.T) {
	m, _, r := recoveryFixture(t)
	view, err := m.InspectRecovery(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RecoverCurrent(context.Background(), view.Digest, r); err != nil {
		t.Fatal(err)
	}
	receipt, err := readRecoveryReceipt(m.recoveryDir())
	if err != nil {
		t.Fatal(err)
	}
	timing := receipt.ActivationTiming
	if timing == nil || timing.RestartMS == nil || timing.ReadinessMS == nil || timing.InventoryMS == nil || receipt.RollbackTiming != nil {
		t.Fatal(timing)
	}
	original, _ := json.Marshal(receipt)
	file := filepath.Join(m.recoveryDir(), recoveryReceiptName)
	for _, mutate := range []func(*recoveryReceipt){
		func(r *recoveryReceipt) { v := int64(-1); r.ActivationTiming.RestartMS = &v },
		func(r *recoveryReceipt) { v := int64(375001); r.ActivationTiming.InventoryMS = &v },
		func(r *recoveryReceipt) { r.ActivationTiming.ReadinessBudgetMS = nil },
		func(r *recoveryReceipt) { r.RollbackTiming = r.ActivationTiming },
	} {
		var changed recoveryReceipt
		json.Unmarshal(original, &changed)
		mutate(&changed)
		b, _ := json.Marshal(changed)
		if err = os.WriteFile(file, b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = readRecoveryReceipt(m.recoveryDir()); err == nil {
			t.Fatal("malformed timing accepted")
		}
	}
	var legacy recoveryReceipt
	json.Unmarshal(original, &legacy)
	legacy.ActivationTiming = nil
	b, _ := json.Marshal(legacy)
	os.WriteFile(file, b, 0600)
	if _, err = readRecoveryReceipt(m.recoveryDir()); err != nil {
		t.Fatal("legacy receipt refused", err)
	}
}
func TestNoopAndMetadataHaveNoActivationTiming(t *testing.T) {
	a := &fakeActivator{}
	tx, _, old, _ := rollbackFixture(t, a)
	tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
	if err := tx.Apply(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tx.PreviousDir, recoveryReceiptName)); !os.IsNotExist(err) {
		t.Fatal("noop wrote receipt", err)
	}
	old.Nodes[0].Name = "metadata name"
	if err := tx.Apply(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	rc, err := readRecoveryReceipt(tx.PreviousDir)
	if err != nil || rc.ActivationTiming != nil || rc.RollbackTiming != nil || a.restarts != 0 {
		t.Fatal(rc, err)
	}
}
