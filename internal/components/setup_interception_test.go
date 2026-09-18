package components

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func reviewedLegacyInterceptionFixtures(t *testing.T, paths SetupPaths) ([]byte, []byte) {
	t.Helper()
	hook := []byte("#!/bin/sh\n# XKeen: Auto-generated file. DO NOT EDIT!\nfile_netfilter_hook=/opt/etc/ndm/netfilter.d/proxy.sh\nfile_schedule_hook=/opt/etc/ndm/schedule.d/00-xkeen-hotspot-sync.sh\nname_chain=xkeen\niptables ip6tables iptables-restore ipset xkeen_rule XKEEN\nconfigure_firewall() { :; }\nclean_firewall() { :; }\nproxy_start() { :; }\nproxy_stop() { :; }\n")
	schedule := []byte("#!/bin/sh\n# XKeen: re-sync deny MAC ipset on schedule start/stop. Auto-generated. DO NOT EDIT!\n[ \"$1\" = \"start\" ] || [ \"$1\" = \"stop\" ] || exit 0\n[ -x /opt/etc/ndm/netfilter.d/proxy.sh ] && /opt/etc/ndm/netfilter.d/proxy.sh\n")
	if err := os.MkdirAll(filepath.Dir(paths.InterceptionHook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.InterceptionScheduleHook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.InterceptionHook, hook, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.InterceptionScheduleHook, schedule, 0o755); err != nil {
		t.Fatal(err)
	}
	return hook, schedule
}

func TestSetupInterceptionFreshCreatesAndVerifiesHybridGeneration(t *testing.T) {
	paths := setupTestPaths(t.TempDir())
	owner := NewFileHybridInterceptionOwner(paths, func(string) error { return nil })
	before, err := owner.Inspect(context.Background())
	if err != nil || before.Owner != "" {
		t.Fatalf("fresh interception state = %+v, %v", before, err)
	}
	target := setupHybridInterceptionGeneration()
	if err := owner.Apply(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := owner.Verify(context.Background(), target); err != nil {
		t.Fatalf("fresh Hybrid target was not verified: %v", err)
	}
	after, err := owner.Inspect(context.Background())
	if err != nil || after.Owner != setupInterceptionOwner || !after.TCPRedirect || !after.UDPTProxy || !after.Complete {
		t.Fatalf("fresh Hybrid evidence = %+v, %v", after, err)
	}
	hook, err := os.ReadFile(paths.InterceptionHook)
	if err != nil || !bytes.Contains(hook, []byte("TCP redirect + UDP TProxy")) {
		t.Fatalf("source-owned Hybrid hook = %q, %v", hook, err)
	}
}

func TestSetupInterceptionTakeoverRetiresReviewedOwnerPreservesUnrelatedNDM(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	legacyHook, legacySchedule := reviewedLegacyInterceptionFixtures(t, paths)
	unrelated := filepath.Join(root, "ndm", "netfilter.d", "operator-owned.sh")
	if err := os.WriteFile(unrelated, []byte("#!/bin/sh\noperator state\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := NewFileHybridInterceptionOwner(paths, func(string) error { return nil })
	legacy, err := owner.Inspect(context.Background())
	if err != nil || legacy.Owner != "xkeen-legacy" || !legacy.LegacyHook || !legacy.LegacySchedule {
		t.Fatalf("reviewed legacy interception evidence = %+v, %v", legacy, err)
	}
	if err := owner.RetireLegacy(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	if err := owner.Apply(context.Background(), setupHybridInterceptionGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Verify(context.Background(), setupHybridInterceptionGeneration()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(paths.InterceptionHook); err != nil || bytes.Equal(got, legacyHook) {
		t.Fatalf("legacy hook was not replaced: %q, %v", got, err)
	}
	if got, err := os.ReadFile(paths.InterceptionScheduleHook); err != nil || bytes.Equal(got, legacySchedule) {
		t.Fatalf("legacy schedule was not replaced: %q, %v", got, err)
	}
	if got, err := os.ReadFile(unrelated); err != nil || string(got) != "#!/bin/sh\noperator state\n" {
		t.Fatalf("unrelated NDM state changed: %q, %v", got, err)
	}
}

func TestSetupInterceptionRecoveryRestoresExactReviewedGeneration(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	legacyHook, legacySchedule := reviewedLegacyInterceptionFixtures(t, paths)
	service := NewSetupService(SetupConfig{Paths: paths})
	owner := service.config.Interception
	previous, err := owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.captureSetupSnapshot(context.Background(), "managed-takeover", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.RetireLegacy(context.Background(), mustSetupInterceptionEvidence(t, owner)); err != nil {
		t.Fatal(err)
	}
	if err := owner.Apply(context.Background(), setupHybridInterceptionGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := service.restoreSetupSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := owner.Restore(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	if err := owner.VerifyRestored(context.Background(), previous); err != nil {
		t.Fatalf("restored interception generation was not proven: %v", err)
	}
	if got, err := os.ReadFile(paths.InterceptionHook); err != nil || !bytes.Equal(got, legacyHook) {
		t.Fatalf("legacy hook was not restored exactly: %q, %v", got, err)
	}
	if got, err := os.ReadFile(paths.InterceptionScheduleHook); err != nil || !bytes.Equal(got, legacySchedule) {
		t.Fatalf("legacy schedule was not restored exactly: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "interception.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target interception state survived rollback: %v", err)
	}
}

func TestSetupInterceptionUnknownHookFailsClosed(t *testing.T) {
	paths := setupTestPaths(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.InterceptionHook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.InterceptionHook, []byte("#!/bin/sh\noperator-owned unknown firewall mutation\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := NewFileHybridInterceptionOwner(paths, func(string) error { return nil })
	if _, err := owner.Inspect(context.Background()); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("unknown interception hook error = %v", err)
	}
	service := NewSetupService(SetupConfig{Paths: paths})
	if projection := service.Status(); projection.State != "blocked" || projection.ReasonCode != SetupReasonInterceptionConflict {
		t.Fatalf("unknown interception projection = %+v", projection)
	}
}

func TestSetupNativeInterceptionEvidenceRequiresReviewedPersistentOwner(t *testing.T) {
	sourceRules := finalizeSetupInterceptionEvidence(SetupInterceptionEvidence{
		Owner: "xkeen-control", Generation: "hybrid-v1", TCPRedirect: true, UDPTProxy: true, Complete: true,
	})
	if _, err := mergeNativeInterceptionEvidence(SetupInterceptionEvidence{}, sourceRules); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("source rules without the source-owned files were admitted: %v", err)
	}
	legacyRules := finalizeSetupInterceptionEvidence(SetupInterceptionEvidence{Owner: "xkeen-legacy", LegacyRules: true})
	if _, err := mergeNativeInterceptionEvidence(SetupInterceptionEvidence{}, legacyRules); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("legacy rules without the reviewed NDM owner were admitted: %v", err)
	}
	legacyFiles := finalizeSetupInterceptionEvidence(SetupInterceptionEvidence{Owner: "xkeen-legacy", LegacyHook: true, LegacySchedule: true})
	merged, err := mergeNativeInterceptionEvidence(legacyFiles, legacyRules)
	if err != nil || merged.Owner != "xkeen-legacy" || !merged.LegacyHook || !merged.LegacySchedule || !merged.LegacyRules {
		t.Fatalf("reviewed legacy owner merge = %+v, %v", merged, err)
	}
}

func mustSetupInterceptionEvidence(t *testing.T, owner SetupInterceptionOwner) SetupInterceptionEvidence {
	t.Helper()
	evidence, err := owner.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
