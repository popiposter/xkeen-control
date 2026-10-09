//go:build linux

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestTransactionProofUnknownBranchesVerifyWithoutReplay(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "candidate", true: "previous"}[rollback], func(t *testing.T) {
			a := &fakeActivator{restartErrs: []error{xkeen.ErrLifecycleUnknown}}
			if rollback {
				a.restartErrs = []error{errors.New("known fixture failure"), xkeen.ErrLifecycleUnknown}
			}
			tx, next, old, _ := rollbackFixture(t, a)
			tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
			err := tx.Apply(context.Background(), next)
			if !errors.Is(err, ErrNodeRecoveryRequired) {
				t.Fatal(err)
			}
			receipt, e := readRecoveryReceipt(tx.PreviousDir)
			if e != nil {
				t.Fatal(e)
			}
			branch := "candidate"
			expected := next
			calls := 1
			if rollback {
				branch = "previous"
				expected = old
				calls = 2
			}
			if receipt.Branch != branch || receipt.Stage != "restart" || receipt.Reason != "unknown" || receipt.GenerationProof == "" || a.restarts != calls {
				t.Fatal("missing exact branch proof", receipt, a.restarts)
			}
			m := NewManager(Config{Store: tx.Store, Transaction: tx})
			view, e := m.InspectRecovery(context.Background(), activatorRuntime{a})
			if e != nil || !view.CanVerify || view.CanActivate {
				t.Fatal(view, e)
			}
			if e = m.VerifyExistingRecovery(context.Background(), view.Digest, activatorRuntime{a}); e != nil {
				t.Fatal(e)
			}
			if a.restarts != calls {
				t.Fatal("verification replayed native activation")
			}
			assertGeneration(t, tx.Store, tx.ActiveOutboundsPath, expected)
			receipt, e = readRecoveryReceipt(tx.PreviousDir)
			if e != nil || receipt.Phase != "completed" || receipt.Branch != branch {
				t.Fatal(receipt, e)
			}
			if _, e = os.Lstat(filepath.Join(tx.PreviousDir, ".pending")); !os.IsNotExist(e) {
				t.Fatal("pending retained", e)
			}
		})
	}
}

func TestTransactionProofRejectsFullConfigValidationDrift(t *testing.T) {
	for _, change := range []string{"edit", "equal-byte-replace", "add", "remove"} {
		t.Run(change, func(t *testing.T) {
			a := &fakeActivator{}
			tx, next, old, _ := rollbackFixture(t, a)
			tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
			policy := filepath.Join(tx.ConfigDir, "05_routing.json")
			initial := []byte(`{"routing":{"rules":[]}}`)
			if e := atomicWrite(policy, initial, 0600); e != nil {
				t.Fatal(e)
			}
			a.validate = func(context.Context) error {
				switch change {
				case "edit":
					return atomicWrite(policy, []byte(`{"routing":"invalid"}`), 0600)
				case "equal-byte-replace":
					return atomicWrite(policy, initial, 0600)
				case "add":
					return atomicWrite(filepath.Join(tx.ConfigDir, "07_new.json"), []byte(`{}`), 0600)
				default:
					return os.Remove(policy)
				}
			}
			if err := tx.Apply(context.Background(), next); err == nil || a.restarts != 0 {
				t.Fatal("unvalidated generation admitted", err, a.restarts)
			}
			assertGeneration(t, tx.Store, tx.ActiveOutboundsPath, old)
			if _, e := os.Lstat(filepath.Join(tx.PreviousDir, ".pending")); !os.IsNotExist(e) {
				t.Fatal("precommit drift wrote intent", e)
			}
		})
	}
}

func TestTransactionRestoreFailureStopsBeforeRollbackLifecycle(t *testing.T) {
	a := &fakeActivator{}
	tx, next, _, _ := rollbackFixture(t, a)
	a.onRestart = func(n int) {
		if n == 1 {
			if e := os.Remove(tx.Store.Path); e != nil {
				t.Fatal(e)
			}
			if e := os.Mkdir(tx.Store.Path, 0700); e != nil {
				t.Fatal(e)
			}
		}
	}
	a.restartErr = errors.New("known activation error")
	err := tx.Apply(context.Background(), next)
	if !errors.Is(err, ErrNodeRecoveryRequired) || !errors.Is(err, ErrRollbackFailed) || a.restarts != 1 || a.readyCalls != 0 {
		t.Fatal(err, a.restarts, a.readyCalls)
	}
	receipt, e := readRecoveryReceipt(tx.PreviousDir)
	if e != nil || receipt.Stage != "restore" || receipt.Branch != "previous" || receipt.GenerationProof != "" {
		t.Fatal(receipt, e)
	}
}

func TestNewRecoveryAfterCompletedPredecessorHasDirectProof(t *testing.T) {
	m, a, r := recoveryFixture(t)
	s, e := m.recoverySnapshot(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.RecoverCurrent(context.Background(), s.view.Digest, r); e != nil {
		t.Fatal(e)
	}
	old, oi, e := recoveryRead(filepath.Join(m.recoveryDir(), recoveryReceiptName), 8192, true)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := acquireNodeIntent(context.Background(), m.recoveryDir())
	if e != nil {
		t.Fatal(e)
	}
	intent.Close()
	a.onRestart = func(int) { r.identity = strings.Repeat("c", 64) }
	a.restartErr = xkeen.ErrLifecycleUnknown
	s, e = m.recoverySnapshot(context.Background(), r)
	if e != nil || !s.view.CanActivate {
		t.Fatal(s.view, e)
	}
	if e = m.RecoverCurrent(context.Background(), s.view.Digest, r); !errors.Is(e, ErrNodeRecoveryRequired) {
		t.Fatal(e)
	}
	receipt, e := readRecoveryReceipt(m.recoveryDir())
	if e != nil || receipt.GenerationProof == "" || receipt.PredecessorOriginal != recoveryIdentity(oi, old) {
		t.Fatal(receipt, e)
	}
	saved, _, e := recoveryRead(filepath.Join(m.recoveryDir(), predecessorName(receipt.PredecessorSlot)), 8192, true)
	if e != nil || string(saved) != string(old) {
		t.Fatal("predecessor not retained", e)
	}
	view, e := m.InspectRecovery(context.Background(), r)
	if e != nil || !view.CanVerify || view.CanActivate {
		t.Fatal(view, e)
	}
	calls := a.restarts
	if e = m.VerifyExistingRecovery(context.Background(), view.Digest, r); e != nil {
		t.Fatal(e)
	}
	if a.restarts != calls {
		t.Fatal("replayed new recovery")
	}
}

type stoppedProofActivator struct{ fakeActivator }

func (*stoppedProofActivator) RuntimeIdentity(context.Context) (string, error) { return "stopped", nil }

func TestMetadataStoppedVerifiedCrashContinuesWithoutAPI(t *testing.T) {
	a := &stoppedProofActivator{fakeActivator: fakeActivator{readyErr: errors.New("must not call API"), inventoryErr: errors.New("must not query inventory")}}
	tx, _, old, _ := rollbackFixture(t, a)
	tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
	next := old
	next.Nodes = append([]Node(nil), old.Nodes...)
	next.Nodes[0].Name = "renamed"
	if e := tx.Apply(context.Background(), next); e != nil {
		t.Fatal(e)
	}
	receipt, e := readRecoveryReceipt(tx.PreviousDir)
	if e != nil {
		t.Fatal(e)
	}
	// Recreate the durable state after verified + marker unlink, before final
	// completed publication. Completion fence must still exclude normal writes.
	receipt.Phase = "verified"
	if e = saveRecoveryReceipt(tx.PreviousDir, receipt); e != nil {
		t.Fatal(e)
	}
	if _, e = beginRecoveryCompletion(tx.PreviousDir, receipt.Digest); e != nil {
		t.Fatal(e)
	}
	m := NewManager(Config{Store: tx.Store, Transaction: tx})
	view, e := m.InspectRecovery(context.Background(), activatorRuntime{a})
	if e != nil || !view.CanVerify {
		t.Fatal(view, e)
	}
	if e = m.VerifyExistingRecovery(context.Background(), view.Digest, activatorRuntime{a}); e != nil {
		t.Fatal(e)
	}
	if a.restarts != 0 || a.readyCalls != 0 || a.inventoryCalls != 0 {
		t.Fatal("metadata recovery invoked Xray")
	}
}

func TestTransactionReceiptAndPredecessorReplacementRefuseVerification(t *testing.T) {
	m, a, r := recoveryFixture(t)
	s, e := m.recoverySnapshot(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.RecoverCurrent(context.Background(), s.view.Digest, r); e != nil {
		t.Fatal(e)
	}
	intent, e := acquireNodeIntent(context.Background(), m.recoveryDir())
	if e != nil {
		t.Fatal(e)
	}
	intent.Close()
	a.onRestart = func(int) { r.identity = strings.Repeat("c", 64) }
	a.restartErr = xkeen.ErrLifecycleUnknown
	s, e = m.recoverySnapshot(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	_ = m.RecoverCurrent(context.Background(), s.view.Digest, r)
	receipt, e := readRecoveryReceipt(m.recoveryDir())
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(m.recoveryDir(), predecessorName(receipt.PredecessorSlot))
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = atomicWrite(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = m.InspectRecovery(context.Background(), r); e == nil {
		t.Fatal("equal-byte predecessor replacement accepted")
	}
}

func TestPredecessorStagingFailurePreservesCurrentReference(t *testing.T) {
	m, a, r := recoveryFixture(t)
	for _, identity := range []string{strings.Repeat("b", 64), strings.Repeat("c", 64)} {
		a.onRestart = func(int) { r.identity = identity }
		s, e := m.recoverySnapshot(context.Background(), r)
		if e != nil {
			t.Fatal(e)
		}
		if e = m.RecoverCurrent(context.Background(), s.view.Digest, r); e != nil {
			t.Fatal(e)
		}
		intent, e := acquireNodeIntent(context.Background(), m.recoveryDir())
		if e != nil {
			t.Fatal(e)
		}
		intent.Close()
	}
	before, _, e := readRecoveryReceiptIdentity(m.recoveryDir())
	if e != nil {
		t.Fatal(e)
	}
	pred, original, slot, e := preserveCompletedPredecessor(m.recoveryDir())
	if e != nil || slot == before.PredecessorSlot {
		t.Fatal("active predecessor overwritten", e)
	}
	candidate := *before
	candidate.Phase = "prepared"
	candidate.Predecessor = pred
	candidate.PredecessorOriginal = original
	candidate.PredecessorSlot = slot
	_, e = writeRecoveryReceiptWithSync(m.recoveryDir(), &candidate, func(*os.File) error { return errors.New("sync fixture") }, syncNodeDirectory)
	if e == nil {
		t.Fatal("fault ignored")
	}
	after, e := m.recoverySnapshot(context.Background(), r)
	if e != nil || !after.view.CanActivate || *after.receipt != *before {
		t.Fatal("prior proof destroyed", after.view, e)
	}
	if a.restarts != 2 {
		t.Fatal("fault invoked lifecycle")
	}
}

func TestJournalFirstWriteRejectsUnexpectedReceipt(t *testing.T) {
	m, _, r := recoveryFixture(t)
	s, e := m.recoverySnapshot(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	receipt := recoveryReceipt{Schema: 1, Phase: "activation-intent", Digest: s.view.Digest, Marker: s.markerDigest, RuntimeBefore: s.runtime}
	if e = saveRecoveryReceipt(m.recoveryDir(), &receipt); e != nil {
		t.Fatal(e)
	}
	before, id, e := readRecoveryReceiptIdentity(m.recoveryDir())
	if e != nil {
		t.Fatal(e)
	}
	j := &transactionJournal{t: m.tx, r: receipt}
	if e = j.write("prepared", "prepared"); e == nil {
		t.Fatal("first write ignored expected absence")
	}
	after, newID, e := readRecoveryReceiptIdentity(m.recoveryDir())
	if e != nil || newID != id || *after != *before {
		t.Fatal("foreign receipt overwritten", e)
	}
}

func TestTerminalTransactionReceiptRequiresGenerationProof(t *testing.T) {
	a := &fakeActivator{}
	tx, next, _, _ := rollbackFixture(t, a)
	if e := tx.Apply(context.Background(), next); e != nil {
		t.Fatal(e)
	}
	receipt, e := readRecoveryReceipt(tx.PreviousDir)
	if e != nil {
		t.Fatal(e)
	}
	receipt.GenerationProof = ""
	// Emulate corrupt private state without the validating receipt writer.
	b, e := json.Marshal(receipt)
	if e != nil {
		t.Fatal(e)
	}
	if e = atomicWrite(filepath.Join(tx.PreviousDir, recoveryReceiptName), b, 0600); e != nil {
		t.Fatal(e)
	}
	if !RecoveryNeedsInspection(tx.PreviousDir) {
		t.Fatal("malformed terminal proof admitted")
	}
}

type changingProofActivator struct {
	fakeActivator
	changed bool
}

func (a *changingProofActivator) RuntimeIdentity(context.Context) (string, error) {
	n := a.restarts
	if a.changed {
		n += 10
	}
	return testRuntimeIdentity(n), nil
}
func (a *changingProofActivator) VerifyOutboundTags(context.Context, []string) error {
	a.changed = true
	return nil
}

func TestReadinessAndInventoryCannotVerifyReplacementRuntime(t *testing.T) {
	a := &changingProofActivator{}
	tx, next, _, _ := rollbackFixture(t, a)
	tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
	if e := tx.Apply(context.Background(), next); !errors.Is(e, ErrNodeRecoveryRequired) {
		t.Fatal(e)
	}
	if a.restarts != 1 {
		t.Fatal("runtime drift triggered lifecycle")
	}
	m := NewManager(Config{Store: tx.Store, Transaction: tx})
	view, e := m.InspectRecovery(context.Background(), activatorRuntime{a})
	if e != nil || view.CanVerify || view.CanActivate {
		t.Fatal(view, e)
	}
}

type identityDriftActivator struct {
	fakeActivator
	mutate func()
	once   bool
}

func (a *identityDriftActivator) RuntimeIdentity(context.Context) (string, error) {
	if !a.once {
		a.once = true
		a.mutate()
	}
	return testRuntimeIdentity(a.restarts), nil
}

func TestReconcileJournalCannotRebaseValidationOntoNewConfig(t *testing.T) {
	a := &identityDriftActivator{}
	tx, _, old, _ := rollbackFixture(t, a)
	tx.ConfigDir = filepath.Dir(tx.ActiveOutboundsPath)
	policy := filepath.Join(tx.ConfigDir, "05_routing.json")
	if e := atomicWrite(policy, []byte(`{"routing":{"rules":[]}}`), 0600); e != nil {
		t.Fatal(e)
	}
	a.mutate = func() {
		if e := atomicWrite(policy, []byte(`{"routing":"invalid"}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	m := NewManager(Config{Store: tx.Store, Transaction: tx})
	if e := m.ReconcileRuntime(context.Background()); !errors.Is(e, ErrNodeRecoveryRequired) {
		t.Fatal(e)
	}
	if a.restarts != 0 {
		t.Fatal("rebased unvalidated generation")
	}
	assertGeneration(t, tx.Store, tx.ActiveOutboundsPath, old)
}
