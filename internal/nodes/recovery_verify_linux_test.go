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
)

func legacyRecoveryAttempt(t *testing.T) (*Manager, *fakeActivator, *recoveryRuntimeStub) {
	t.Helper()
	m, a, r := recoveryFixture(t)
	s, err := m.recoverySnapshot(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	// Exact original receipt shape, before optional generation/stage fields existed.
	raw := map[string]any{"schemaVersion": 1, "phase": "activation-intent", "digest": s.view.Digest, "marker": s.markerDigest, "runtimeBefore": r.identity}
	b, _ := json.Marshal(raw)
	if err = os.WriteFile(filepath.Join(m.recoveryDir(), recoveryReceiptName), b, 0600); err != nil {
		t.Fatal(err)
	}
	r.identity = strings.Repeat("b", 64)
	return m, a, r
}

func TestVerifyExistingLegacyProofNoLifecycleAndIdempotent(t *testing.T) {
	m, a, r := legacyRecoveryAttempt(t)
	v, err := m.InspectRecovery(context.Background(), r)
	if err != nil || !v.CanVerify || v.CanActivate {
		t.Fatal(v, err)
	}
	if err = m.VerifyExistingRecovery(context.Background(), v.Digest, r); err != nil {
		t.Fatal(err)
	}
	if a.restarts != 0 || a.validatedPath != "" || a.readyCalls != 1 || a.inventoryCalls != 1 {
		t.Fatal("unexpected actions", a)
	}
	receipt, err := readRecoveryReceipt(m.recoveryDir())
	if err != nil || receipt.Phase != "completed" || !recoveryHex(receipt.GenerationProof) {
		t.Fatal(receipt, err)
	}
	v, err = m.InspectRecovery(context.Background(), r)
	if err != nil || v.Classification != "completed-receipt" {
		t.Fatal(v, err)
	}
	path := filepath.Join(m.recoveryDir(), recoveryReceiptName)
	before, _ := os.Stat(path)
	if err = m.VerifyExistingRecovery(context.Background(), v.Digest, r); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) || a.restarts != 0 || a.readyCalls != 1 {
		t.Fatal("completed operation wrote or repeated verification")
	}
}

func TestVerifyExistingRejectsDriftAndUnprovenHistory(t *testing.T) {
	for _, scenario := range []string{"bytes", "equal-byte-inode", "previous", "absence", "added-config", "removed-config", "marker", "runtime-before", "runtime-old", "runtime-stopped", "runtime-unknown", "malformed-hash", "prior-receipt", "receipt-inode", "stale-preview", "readiness", "inventory", "writer", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			m, a, r := legacyRecoveryAttempt(t)
			v, err := m.InspectRecovery(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			receiptPath := filepath.Join(m.recoveryDir(), recoveryReceiptName)
			p := filepath.Join(m.tx.ConfigDir, "05_routing.json")
			rewrite := func(path string) {
				b, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if e = atomicWrite(path, b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			ctx := context.Background()
			switch scenario {
			case "bytes":
				os.WriteFile(p, []byte(`{"routing":{"rules":[{}]}}`), 0600)
			case "equal-byte-inode":
				rewrite(p)
			case "previous":
				os.WriteFile(filepath.Join(m.recoveryDir(), "nodes.json"), []byte(`{}`), 0600)
			case "absence":
				os.WriteFile(filepath.Join(m.recoveryDir(), ".registry-absent"), nil, 0600)
			case "added-config":
				os.WriteFile(filepath.Join(m.tx.ConfigDir, "99_extra.json"), []byte(`{}`), 0600)
			case "removed-config":
				os.Remove(p)
			case "marker":
				rewrite(filepath.Join(m.recoveryDir(), ".pending"))
			case "runtime-before", "malformed-hash", "prior-receipt":
				rc, _ := readRecoveryReceipt(m.recoveryDir())
				if scenario == "runtime-before" {
					rc.RuntimeBefore = strings.Repeat("c", 64)
				}
				if scenario == "malformed-hash" {
					rc.Digest = strings.Repeat("g", 64)
				}
				if scenario == "prior-receipt" {
					rc.Digest = strings.Repeat("d", 64)
				}
				b, _ := json.Marshal(rc)
				os.WriteFile(receiptPath, b, 0600)
			case "runtime-old":
				r.identity = strings.Repeat("a", 64)
			case "runtime-stopped":
				r.identity = "stopped"
			case "runtime-unknown":
				r.err = errors.New("ambiguous")
			case "receipt-inode":
				rewrite(receiptPath)
			case "stale-preview":
				v.Digest = strings.Repeat("d", 64)
			case "readiness":
				a.readyErr = errors.New("synthetic private error")
			case "inventory":
				a.inventoryErr = errors.New("synthetic private error")
			case "writer":
				m.beforeCommit = func(context.Context) error { return ErrOperationUnavailable }
			case "cancel":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if err = m.VerifyExistingRecovery(ctx, v.Digest, r); err == nil {
				t.Fatal("unsafe settlement")
			}
			if a.restarts != 0 || a.validatedPath != "" {
				t.Fatal("lifecycle or validation invoked")
			}
			if _, err = os.Lstat(filepath.Join(m.recoveryDir(), ".pending")); err != nil {
				t.Fatal("pending removed", err)
			}
		})
	}
}

type verifyReadyMutation struct {
	*fakeActivator
	mutate func()
}

func (a verifyReadyMutation) WaitReady(ctx context.Context) error {
	a.mutate()
	return a.fakeActivator.WaitReady(ctx)
}

func TestVerifyExistingRechecksRuntimeGenerationAndReceipt(t *testing.T) {
	for _, kind := range []string{"runtime", "config", "receipt"} {
		t.Run(kind, func(t *testing.T) {
			m, a, r := legacyRecoveryAttempt(t)
			v, _ := m.InspectRecovery(context.Background(), r)
			m.tx.Activator = verifyReadyMutation{a, func() {
				switch kind {
				case "runtime":
					r.identity = strings.Repeat("c", 64)
				case "config":
					os.WriteFile(filepath.Join(m.tx.ConfigDir, "05_routing.json"), []byte(`{}`), 0600)
				case "receipt":
					p := filepath.Join(m.recoveryDir(), recoveryReceiptName)
					b, _ := os.ReadFile(p)
					atomicWrite(p, b, 0600)
				}
			}}
			if m.VerifyExistingRecovery(context.Background(), v.Digest, r) == nil || a.restarts != 0 {
				t.Fatal("drift accepted")
			}
		})
	}
}

func TestVerifyExistingCrashSettlementAndExplicitContinuation(t *testing.T) {
	for _, point := range []string{"verified-before-write", "verified-after-write", "before-unlink", "after-unlink", "completed-before-write", "completed-after-write"} {
		t.Run(point, func(t *testing.T) {
			m, a, r := legacyRecoveryAttempt(t)
			v, _ := m.InspectRecovery(context.Background(), r)
			save := func(dir string, rc *recoveryReceipt) (string, error) {
				if (point == "verified-before-write" && rc.Phase == "verified") || (point == "completed-before-write" && rc.Phase == "completed") {
					return "", errors.New("fsync fixture")
				}
				identity, err := writeRecoveryReceipt(dir, rc)
				if (point == "verified-after-write" && rc.Phase == "verified") || (point == "completed-after-write" && rc.Phase == "completed") {
					return "", errors.New("lost write response")
				}
				return identity, err
			}
			settle := func(path string, info os.FileInfo) error {
				if point == "before-unlink" {
					return errors.New("unlink fixture")
				}
				err := settleNodeIntent(path, info)
				if point == "after-unlink" {
					return errors.New("lost unlink response")
				}
				return err
			}
			if m.verifyExistingRecovery(context.Background(), v.Digest, r, save, settle) == nil {
				t.Fatal("failure not surfaced")
			}
			if !RecoveryNeedsInspection(m.recoveryDir()) || a.restarts != 0 {
				t.Fatal("failure not fenced")
			}
			v, err := m.InspectRecovery(context.Background(), r)
			if err != nil || !v.CanVerify {
				t.Fatal(v, err)
			}
			if err = m.VerifyExistingRecovery(context.Background(), v.Digest, r); err != nil {
				t.Fatal(err)
			}
			if a.restarts != 0 || RecoveryNeedsInspection(m.recoveryDir()) {
				t.Fatal("continuation did not settle safely")
			}
		})
	}
}

func TestVerifyExistingMissingMarkerRequiresDurableGenerationProof(t *testing.T) {
	m, a, r := legacyRecoveryAttempt(t)
	rc, _ := readRecoveryReceipt(m.recoveryDir())
	rc.Phase = "verified"
	rc.RuntimeAfter = r.identity
	if err := saveRecoveryReceipt(m.recoveryDir(), rc); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(m.recoveryDir(), ".pending"))
	v, err := m.InspectRecovery(context.Background(), r)
	if err != nil || v.CanVerify || v.Reason != "missing-generation-proof" {
		t.Fatal(v, err)
	}
	if m.VerifyExistingRecovery(context.Background(), strings.Repeat("a", 64), r) == nil || a.restarts != 0 {
		t.Fatal("guessed missing proof")
	}
}

func TestVerifyExistingRejectsSettlementDrift(t *testing.T) {
	for _, point := range []string{"verified-write", "marker-unlink", "completed-write"} {
		for _, kind := range []string{"runtime", "config", "receipt", "fence"} {
			t.Run(point+"/"+kind, func(t *testing.T) {
				m, a, r := legacyRecoveryAttempt(t)
				v, _ := m.InspectRecovery(context.Background(), r)
				mutate := func() {
					switch kind {
					case "runtime":
						r.identity = strings.Repeat("c", 64)
					case "config":
						if err := os.WriteFile(filepath.Join(m.tx.ConfigDir, "05_routing.json"), []byte(`{}`), 0600); err != nil {
							t.Fatal(err)
						}
					case "receipt":
						p := filepath.Join(m.recoveryDir(), recoveryReceiptName)
						b, err := os.ReadFile(p)
						if err != nil {
							t.Fatal(err)
						}
						if err = atomicWrite(p, b, 0600); err != nil {
							t.Fatal(err)
						}
					case "fence":
						if err := os.WriteFile(filepath.Join(m.recoveryDir(), recoveryCompletionFenceName), []byte("mismatched\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				save := func(dir string, rc *recoveryReceipt) (string, error) {
					identity, err := writeRecoveryReceipt(dir, rc)
					if point == rc.Phase+"-write" {
						mutate()
					}
					return identity, err
				}
				settle := func(path string, info os.FileInfo) error {
					err := settleNodeIntent(path, info)
					if point == "marker-unlink" {
						mutate()
					}
					return err
				}
				if m.verifyExistingRecovery(context.Background(), v.Digest, r, save, settle) == nil {
					t.Fatal("settlement drift accepted")
				}
				if !RecoveryNeedsInspection(m.recoveryDir()) || a.restarts != 0 || a.validatedPath != "" {
					t.Fatal("drift lost fence or invoked lifecycle")
				}
			})
		}
	}
}

func TestVerifyExistingRejectsUnsafeCompletionFence(t *testing.T) {
	for _, kind := range []string{"malformed", "mismatch", "directory", "symlink", "public"} {
		t.Run(kind, func(t *testing.T) {
			m, a, r := legacyRecoveryAttempt(t)
			v, _ := m.InspectRecovery(context.Background(), r)
			p := filepath.Join(m.recoveryDir(), recoveryCompletionFenceName)
			switch kind {
			case "directory":
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(m.recoveryDir(), recoveryReceiptName), p); err != nil {
					t.Fatal(err)
				}
			default:
				b := []byte("bad\n")
				mode := os.FileMode(0600)
				if kind == "mismatch" {
					b = []byte(strings.Repeat("f", 64) + "\n")
				}
				if kind == "public" {
					mode = 0644
				}
				if err := os.WriteFile(p, b, mode); err != nil {
					t.Fatal(err)
				}
			}
			if !RecoveryNeedsInspection(m.recoveryDir()) {
				t.Fatal("fresh process ignores fence")
			}
			if m.VerifyExistingRecovery(context.Background(), v.Digest, r) == nil || a.restarts != 0 {
				t.Fatal("unsafe fence accepted")
			}
		})
	}
}

func TestVerifyExistingReceiptDurabilityFailures(t *testing.T) {
	for _, phase := range []string{"verified", "completed"} {
		for _, failure := range []string{"file-sync", "directory-sync", "replace-after-rename"} {
			t.Run(phase+"/"+failure, func(t *testing.T) {
				m, a, r := legacyRecoveryAttempt(t)
				v, _ := m.InspectRecovery(context.Background(), r)
				save := func(dir string, rc *recoveryReceipt) (string, error) {
					if rc.Phase != phase {
						return writeRecoveryReceipt(dir, rc)
					}
					return writeRecoveryReceiptWithSync(dir, rc, func(f *os.File) error {
						if failure == "file-sync" {
							return errors.New("fsync fixture")
						}
						return f.Sync()
					}, func(dir string) error {
						if failure == "directory-sync" {
							return errors.New("directory fsync fixture")
						}
						p := filepath.Join(dir, recoveryReceiptName)
						b, err := os.ReadFile(p)
						if err != nil {
							return err
						}
						if err = atomicWrite(p, b, 0600); err != nil {
							return err
						}
						return syncNodeDirectory(dir)
					})
				}
				if m.verifyExistingRecovery(context.Background(), v.Digest, r, save, settleNodeIntent) == nil {
					t.Fatal("failed durability or replaced receipt accepted")
				}
				// Uses only files, as a fresh process would: no in-memory authority.
				if !RecoveryNeedsInspection(m.recoveryDir()) || a.restarts != 0 {
					t.Fatal("fresh process admitted failed completion")
				}
			})
		}
	}
}

func TestVerifyExistingCompletionFenceFailuresAndReappearance(t *testing.T) {
	for _, failure := range []string{"create", "file-sync", "directory-sync", "replaced", "unlink", "cleanup-sync", "reappearance"} {
		t.Run(failure, func(t *testing.T) {
			m, a, r := legacyRecoveryAttempt(t)
			v, _ := m.InspectRecovery(context.Background(), r)
			if err := m.VerifyExistingRecovery(context.Background(), v.Digest, r); err != nil {
				t.Fatal(err)
			}
			rc, _ := readRecoveryReceipt(m.recoveryDir())
			p := filepath.Join(m.recoveryDir(), recoveryCompletionFenceName)
			if failure == "create" {
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			info, err := beginRecoveryCompletionWithSync(m.recoveryDir(), rc.Digest, func(f *os.File) error {
				if failure == "file-sync" {
					return errors.New("file sync fixture")
				}
				return f.Sync()
			}, func(dir string) error {
				if failure == "directory-sync" {
					return errors.New("directory sync fixture")
				}
				return syncNodeDirectory(dir)
			})
			if failure == "create" || failure == "file-sync" || failure == "directory-sync" {
				if err == nil || !RecoveryNeedsInspection(m.recoveryDir()) {
					t.Fatal("fence preparation failure did not block fresh process")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if failure == "replaced" {
				if err = atomicWrite(p, []byte(rc.Digest+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err = finishRecoveryCompletion(m.recoveryDir(), rc.Digest, info, func(path string) error {
				if failure == "unlink" {
					return errors.New("unlink fixture")
				}
				return os.Remove(path)
			}, func(dir string) error {
				if failure == "cleanup-sync" {
					return errors.New("cleanup sync fixture")
				}
				return syncNodeDirectory(dir)
			})
			if failure == "replaced" || failure == "unlink" {
				if err == nil || !RecoveryNeedsInspection(m.recoveryDir()) {
					t.Fatal("precommit failure admitted fresh process")
				}
				return
			}
			if err != nil || RecoveryNeedsInspection(m.recoveryDir()) {
				t.Fatal("logical commit reported failure", err)
			}
			if failure == "reappearance" {
				if _, err = beginRecoveryCompletion(m.recoveryDir(), rc.Digest); err != nil {
					t.Fatal(err)
				}
				if !RecoveryNeedsInspection(m.recoveryDir()) {
					t.Fatal("resurrected fence ignored")
				}
				v, err = m.InspectRecovery(context.Background(), r)
				if err != nil || !v.CanVerify || v.CanActivate {
					t.Fatal(v, err)
				}
				if err = m.VerifyExistingRecovery(context.Background(), v.Digest, r); err != nil {
					t.Fatal(err)
				}
			}
			if a.restarts != 0 {
				t.Fatal("lifecycle invoked")
			}
		})
	}
}

func TestRecoveryPersistsSanitizedFailureStage(t *testing.T) {
	for _, stage := range []string{"validation", "restart", "readiness", "inventory"} {
		t.Run(stage, func(t *testing.T) {
			m, a, r := recoveryFixture(t)
			v, _ := m.InspectRecovery(context.Background(), r)
			secret := errors.New("private synthetic credential")
			switch stage {
			case "validation":
				a.validate = func(context.Context) error { return secret }
			case "restart":
				a.restartErr = secret
			case "readiness":
				a.readyErr = secret
			case "inventory":
				a.inventoryErr = secret
			}
			if m.RecoverCurrent(context.Background(), v.Digest, r) == nil {
				t.Fatal("unexpected success")
			}
			rc, err := readRecoveryReceipt(m.recoveryDir())
			if err != nil || rc.Stage != stage || rc.Reason == "" {
				t.Fatal(rc, err)
			}
			b, _ := os.ReadFile(filepath.Join(m.recoveryDir(), recoveryReceiptName))
			if strings.Contains(string(b), secret.Error()) {
				t.Fatal("private error persisted")
			}
			if stage == "validation" {
				v, _ = m.InspectRecovery(context.Background(), r)
				if v.CanVerify || v.CanActivate {
					t.Fatal("failed validation authorized settlement")
				}
			}
		})
	}
}
