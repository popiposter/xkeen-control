package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This fixture is generated from the exact reviewed upstream S05 generator at
// reviewedUpstreamS05SourceCommit. It contains the emitted substantive proxy
// hook after generator-only injection calls have been materialized as
// assignments, the outer EOL delimiter has been omitted, and comment/blank-
// line cleanup has been applied, with only sanitized runtime assignments and
// an empty policy block.
//
//go:embed testdata/reviewed-upstream-proxy.sh
var reviewedUpstreamProxyHookFixture []byte

// Exact source-owned template extracted from released beta.4 source
// 2c4d4f97ade2f968a1d2f0e1e27f480f12c9f735, without EOL normalization.
//
//go:embed testdata/beta4-source-owned-proxy.sh
var beta4SourceOwnedHybridHookFixture []byte

func TestSetupInterceptionPreviousSourceHookAdmissionAndExactRollback(t *testing.T) {
	digest := sha256.Sum256(beta4SourceOwnedHybridHookFixture)
	if fmt.Sprintf("%x", digest) != setupPreviousSourceOwnedHybridHookSHA256 || bytes.Equal(beta4SourceOwnedHybridHookFixture, setupSourceOwnedHybridHookBytes()) {
		t.Fatal("previous released hook fixture identity drifted")
	}
	paths := setupTestPaths(t.TempDir())
	owner := NewFileHybridInterceptionOwner(paths, func(string) error { return nil })
	ctx := context.Background()
	if err := owner.Apply(ctx, setupHybridInterceptionGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.InterceptionHook, beta4SourceOwnedHybridHookFixture, 0o700); err != nil {
		t.Fatal(err)
	}
	prior, err := owner.Inspect(ctx)
	if err != nil || !prior.PreviousHook || !prior.Complete {
		t.Fatalf("prior ownership identity = %+v, %v", prior, err)
	}
	if err := owner.Verify(ctx, setupHybridInterceptionGeneration()); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("prior hook proved current candidate: %v", err)
	}
	kernel := prior
	kernel.PreviousHook = false
	merged, err := mergeNativeInterceptionEvidence(prior, finalizeSetupInterceptionEvidence(kernel))
	if err != nil || !merged.PreviousHook || !validSetupInterceptionEvidence(merged) {
		t.Fatalf("native merge lost prior-template identity: %+v, %v", merged, err)
	}
	previous, err := owner.Snapshot(ctx)
	if err != nil {
		t.Fatalf("previous released generation cannot be inspected: %v", err)
	}
	service := NewSetupService(SetupConfig{Paths: paths})
	files, err := service.captureSetupSnapshot(ctx, "managed-takeover", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Apply(ctx, setupHybridInterceptionGeneration()); err != nil {
		t.Fatal(err)
	}
	if current, err := os.ReadFile(paths.InterceptionHook); err != nil || !bytes.Equal(current, setupSourceOwnedHybridHookBytes()) {
		t.Fatal("Apply did not emit current hook")
	}
	if err := service.restoreSetupSnapshot(files); err != nil {
		t.Fatal(err)
	}
	if err := owner.Restore(ctx, previous); err != nil {
		t.Fatal(err)
	}
	if err := owner.VerifyRestored(ctx, previous); err != nil {
		t.Fatalf("previous hook rollback failed verification: %v", err)
	}
	if restored, err := os.ReadFile(paths.InterceptionHook); err != nil || !bytes.Equal(restored, beta4SourceOwnedHybridHookFixture) {
		t.Fatal("previous hook bytes not restored exactly")
	}
	if err := os.WriteFile(paths.InterceptionHook, append(append([]byte(nil), beta4SourceOwnedHybridHookFixture...), []byte("# drift\n")...), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Inspect(ctx); !errors.Is(err, ErrSetupInterceptionConflict) {
		t.Fatalf("modified previous hook admitted: %v", err)
	}
}

func TestReviewedLegacyHookFixtureBindsReviewedUpstreamS05(t *testing.T) {
	if reviewedUpstreamS05SourceCommit != "da20a5e4d739101f951417754038acaee614631f" || reviewedUpstreamS05SourcePath != "scripts/_xkeen/02_install/07_install_register/04_register_init.sh" || reviewedUpstreamS05SHA256 != "6e2998bd8c471637ed4d0128eebc2d10bf72dc15b1600208601d70f0a1d0ee13" {
		t.Fatalf("reviewed S05 identity drifted: commit=%s path=%s sha=%s", reviewedUpstreamS05SourceCommit, reviewedUpstreamS05SourcePath, reviewedUpstreamS05SHA256)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(reviewedUpstreamProxyHookFixture), "\n"), "\n") {
		if line == "EOL" || line == "SCHEDULE_EOL" || strings.HasPrefix(line, "cat > ") || strings.HasPrefix(line, "inject_var ") {
			t.Fatalf("reviewed upstream proxy fixture contains generator-only syntax %q", line)
		}
	}
	digest, ok := reviewedLegacyProxyHookFingerprint(reviewedUpstreamProxyHookFixture)
	if !ok || digest != reviewedUpstreamProxyHookCanonicalSHA256 {
		t.Fatalf("reviewed upstream proxy fixture fingerprint = %s ok=%v", digest, ok)
	}
}

func reviewedLegacyInterceptionFixtures(t *testing.T, paths SetupPaths) ([]byte, []byte) {
	t.Helper()
	hook := append([]byte(nil), reviewedUpstreamProxyHookFixture...)
	for _, marker := range [][]byte{[]byte("iptables-restore"), []byte("ipset"), []byte("configure_route()"), []byte("TPROXY")} {
		if !bytes.Contains(hook, marker) {
			t.Fatalf("reviewed upstream proxy fixture lacks substantive marker %q", marker)
		}
	}
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

func TestSetupHybridHookDoesNotDuplicateAnExistingDefaultRoute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("source shell fixture runs in Docker/Linux")
	}
	for _, route := range []string{"local default dev lo scope host", "local 0.0.0.0/0 dev lo scope host"} {
		root := t.TempDir()
		files := map[string]string{
			"iptables": "#!/bin/sh\nexit 0\n",
			"ip":       "#!/bin/sh\ncase \"$*\" in\n'-4 link show dev br0') exit 0;;\n'-4 rule show') printf '%s\\n' '111: from all fwmark 0x111/0xfff lookup 111';;\n'-4 route show table 111') printf '%s\\n' \"$TEST_EXISTING_ROUTE\";;\n*) printf '%s\\n' 'unexpected route mutation' >&2; exit 99;;\nesac\n",
			"hook":     string(setupSourceOwnedHybridHookBytes()),
		}
		for name, contents := range files {
			if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		for attempt := 0; attempt < 2; attempt++ {
			command := exec.Command("sh", filepath.Join(root, "hook"))
			command.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "TEST_EXISTING_ROUTE="+route)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("existing route reconciliation mutated routing: %v %s", err, output)
			}
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
		[]byte(strings.Replace(string(hook), "name_chain='xkeen'", "name_chain_extra='xkeen'", 1)),
		[]byte(strings.Replace(string(hook), "name_chain='xkeen'", "name_chain='xkeen'\nname_chain='xkeen'", 1)),
		[]byte(strings.Replace(string(hook), "iptables-restore", "operator-restore", 1)),
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

func TestNativeInterceptionParserAcceptsDefaultRouteSpelling(t *testing.T) {
	ipv4, rules, _, link := nativeSourceSaveFixture()
	for _, route := range []string{"local default dev lo scope host\n", "local 0.0.0.0/0 dev lo scope host\n"} {
		evidence, _, err := parseNativeInterceptionState(ipv4, nil, nil, rules, []byte(route), nil, nil, link)
		if err != nil || !evidence.Complete {
			t.Fatalf("equivalent route rejected: %v", err)
		}
	}
	for _, route := range []string{"local default dev eth0 scope host\n", "default via 192.0.2.1 dev eth0\n", "local default dev lo\nlocal 0.0.0.0/0 dev lo\n"} {
		if _, _, err := parseNativeInterceptionState(ipv4, nil, nil, rules, []byte(route), nil, nil, link); !errors.Is(err, ErrSetupInterceptionConflict) {
			t.Fatalf("unknown or duplicate route admitted: %v", err)
		}
	}
	if _, found, err := nativePolicyExact([]byte("local default dev lo metric 1024\n"), "route", "ipv6"); err != nil || !found {
		t.Fatalf("IPv6 legacy default rejected: %v", err)
	}
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
