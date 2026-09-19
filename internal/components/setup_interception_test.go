package components

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestSetupSourceOwnedHybridHookIsLANScopedIPv4OnlyAndFailClosed(t *testing.T) {
	hook := string(setupSourceOwnedHybridHookBytes())
	for _, required := range []string{"-i br0", "-m addrtype ! --dst-type LOCAL", "ip -4 rule add fwmark 0x111/0xfff table 111 pref 111", "ip -4 route add local 0.0.0.0/0 dev lo table 111"} {
		if !strings.Contains(hook, required) {
			t.Fatalf("source hook lacks required scoped shape %q", required)
		}
	}
	for _, forbidden := range []string{"ip6tables", "-A PREROUTING -p", "|| true", "ip -6"} {
		if strings.Contains(hook, forbidden) {
			t.Fatalf("source hook contains forbidden blanket/IPv6/suppressed operation %q", forbidden)
		}
	}
}

func TestSetupInterceptionFreshRollbackRemovesPartialSourceGeneration(t *testing.T) {
	paths := setupTestPaths(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.InterceptionHook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.InterceptionHook, setupSourceOwnedHybridHookBytes(), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := NewFileHybridInterceptionOwner(paths, func(string) error { return nil })
	if err := owner.Restore(context.Background(), nil); err != nil {
		t.Fatalf("partial fresh interception rollback failed: %v", err)
	}
	for _, path := range []string{paths.InterceptionHook, paths.InterceptionScheduleHook, paths.InterceptionState} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("partial source-owned interception artifact survived rollback at %s: %v", path, err)
		}
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

func TestSetupReviewedLegacyHookRequiresClosedIdentity(t *testing.T) {
	root := t.TempDir()
	paths := setupTestPaths(root)
	hook, _ := reviewedLegacyInterceptionFixtures(t, paths)
	owner := NewFileHybridInterceptionOwner(paths, func(string) error { return nil })
	if _, err := owner.Inspect(context.Background()); err != nil {
		t.Fatalf("closed reviewed fixture was rejected: %v", err)
	}
	mutations := [][]byte{
		append(append([]byte(nil), hook...), []byte("rm -rf /\n")...),
		[]byte(strings.Replace(string(hook), "name_chain=xkeen", "name_chain=operator", 1)),
		[]byte(strings.Replace(string(hook), "name_chain=xkeen\n", "name_chain=xkeen\nname_chain=xkeen\n", 1)),
	}
	for _, mutation := range mutations {
		if err := os.WriteFile(paths.InterceptionHook, mutation, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Inspect(context.Background()); !errors.Is(err, ErrSetupInterceptionConflict) {
			t.Fatalf("legacy identity mutation was admitted: %v", err)
		}
		if err := os.WriteFile(paths.InterceptionHook, hook, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func nativeSourceSaveFixture() (ipv4, rules, routes, link []byte) {
	ipv4 = []byte(`*nat
:PREROUTING ACCEPT [0:0]
:XKEEN_CONTROL_HYBRID - [0:0]
-A PREROUTING -i br0 -m addrtype ! --dst-type LOCAL -p tcp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID
-A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p tcp -m comment --comment xkeen-control-hybrid -j REDIRECT --to-ports 61219
COMMIT
*mangle
:PREROUTING ACCEPT [0:0]
:XKEEN_CONTROL_HYBRID - [0:0]
-A PREROUTING -i br0 -m addrtype ! --dst-type LOCAL -p udp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID
-A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p udp -m socket --transparent -m comment --comment xkeen-control-hybrid -j MARK --set-mark 0x111/0xfff
-A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p udp -m comment --comment xkeen-control-hybrid -j TPROXY --on-ip 0.0.0.0 --on-port 61219 --tproxy-mark 0x111/0xfff
COMMIT
*filter
:INPUT ACCEPT [0:0]
-A INPUT -j ACCEPT
COMMIT
`)
	rules = []byte("111: from all fwmark 0x111/0xfff lookup 111\n")
	routes = []byte("local 0.0.0.0/0 dev lo scope host\n")
	link = []byte("2: br0: <BROADCAST,UP,LOWER_UP> mtu 1500\n")
	return ipv4, rules, routes, link
}

func TestNativeInterceptionParserRequiresScopedShapeAndRoutingProof(t *testing.T) {
	ipv4, rules, routes, link := nativeSourceSaveFixture()
	evidence, owned, err := parseNativeInterceptionState(ipv4, nil, nil, rules, routes, nil, nil, link)
	if err != nil || evidence.Owner != setupInterceptionOwner || !evidence.LANScoped || !evidence.PolicyRouting || !evidence.IPv6Disabled || len(owned.Chains) != 2 || len(owned.Rules) != 5 || len(owned.Policy) != 2 {
		t.Fatalf("valid native source state = %+v owned=%+v err=%v", evidence, owned, err)
	}
	if _, _, err := parseNativeInterceptionState(ipv4, nil, nil, rules, nil, nil, nil, link); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("missing table-111 route was admitted: %v", err)
	}
	wan := bytes.Replace(ipv4, []byte("-i br0 -m addrtype ! --dst-type LOCAL"), []byte("-m addrtype ! --dst-type LOCAL"), 1)
	if _, _, err := parseNativeInterceptionState(wan, nil, nil, rules, routes, nil, nil, link); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("WAN/unscoped source jump was admitted: %v", err)
	}
	if _, _, err := parseNativeInterceptionState(ipv4, ipv4, nil, rules, routes, nil, nil, link); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("IPv6 interception was admitted: %v", err)
	}
}

func TestNativeInterceptionSnapshotIsTypedCompactAndOmitsUnrelatedFirewallState(t *testing.T) {
	ipv4, rules, routes, link := nativeSourceSaveFixture()
	evidence, owned, err := parseNativeInterceptionState(ipv4, nil, nil, rules, routes, nil, nil, link)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(nativeHybridSnapshot{SchemaVersion: setupInterceptionSchemaVersion, Evidence: evidence, Owned: owned})
	if err != nil || len(snapshot) >= setupMaxInterceptionSnapshotBytes || bytes.Contains(snapshot, []byte(`"INPUT"`)) || bytes.Contains(snapshot, []byte(`iptables-restore`)) {
		t.Fatalf("typed native snapshot=%d bytes err=%v payload=%s", len(snapshot), err, snapshot)
	}
	if _, err := parseNativeHybridSnapshot(snapshot); err != nil {
		t.Fatalf("typed snapshot did not round-trip: %v", err)
	}
	concurrent := bytes.Replace(ipv4, []byte("-A INPUT -j ACCEPT"), []byte("-A INPUT -j ACCEPT\n-A FORWARD -j ACCEPT"), 1)
	_, concurrentOwned, err := parseNativeInterceptionState(concurrent, nil, nil, rules, routes, nil, nil, link)
	if err != nil || nativeOwnedDigest(concurrentOwned) != nativeOwnedDigest(owned) {
		t.Fatalf("unrelated concurrent firewall state changed the owned rollback subset: err=%v before=%s after=%s", err, nativeOwnedDigest(owned), nativeOwnedDigest(concurrentOwned))
	}
	previous := setupPreviousRecord{AllAbsent: false, Class: "managed-takeover", SnapshotDir: "/opt/etc/xkeen-control/previous-setup/.setup-snapshot", SnapshotSHA: strings.Repeat("a", 64), InterceptionClass: evidence.Owner, InterceptionSHA: evidence.Digest}
	journalBytes, err := json.Marshal(previous)
	if err != nil || len(journalBytes) >= MaxComponentJournalBytes || bytes.Contains(journalBytes, []byte("interceptionSnapshot")) || bytes.Contains(journalBytes, []byte("XKEEN_CONTROL_HYBRID")) {
		t.Fatalf("journal previous record is not compact: %d bytes err=%v payload=%s", len(journalBytes), err, journalBytes)
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
