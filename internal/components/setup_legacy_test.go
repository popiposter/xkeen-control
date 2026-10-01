package components

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func historicalLifecycleFixture() string {
	return "#!/bin/sh\n" +
		`ipv4_exclude="` + historicalS05Assignments["ipv4_exclude"] + "\"\n" +
		`ipv6_exclude="` + historicalS05Assignments["ipv6_exclude"] + "\"\n" +
		"proxy_dns=\"off\"\nipv6_support=\"on\"\nprintf 'synthetic lifecycle'\n"
}

func TestHistoricalLifecycleOnlyAdmitsTypedConfiguredSlots(t *testing.T) {
	baseline := historicalLifecycleFixture()
	digest := digestSetupBytes([]byte(baseline))
	configured := strings.NewReplacer(historicalS05Assignments["ipv4_exclude"], "192.0.2.1 198.51.100.0/24", historicalS05Assignments["ipv6_exclude"], "2001:db8::1 2001:db8:1::/64", `proxy_dns="off"`, `proxy_dns="on"`, `ipv6_support="on"`, `ipv6_support="off"`).Replace(baseline)
	if !reviewedConfiguredHistoricalS05([]byte(configured), digest) || !reviewedConfiguredHistoricalS05([]byte(strings.ReplaceAll(configured, "\n", "\r\n")), digest) {
		t.Fatal("typed configured slots or line endings rejected")
	}
	for _, invalid := range []string{
		configured + "printf 'unknown code'\n",
		configured + "proxy_dns=\"off\"\n",
		strings.Replace(configured, "192.0.2.1 198.51.100.0/24", "$(synthetic-command)", 1),
		strings.Replace(configured, "192.0.2.1 198.51.100.0/24", "2001:db8::1", 1),
		strings.Replace(configured, "2001:db8::1 2001:db8:1::/64", "192.0.2.1", 1),
		strings.Replace(configured, "192.0.2.1 198.51.100.0/24", "example.com", 1),
		strings.Replace(configured, "192.0.2.1 198.51.100.0/24", strings.Repeat("192.0.2.1 ", 129), 1),
		strings.Replace(configured, `proxy_dns="on"`, `proxy_dns="unknown"`, 1),
		strings.Replace(configured, `ipv6_support="off"`, `ipv6_support="${synthetic}"`, 1),
	} {
		if reviewedConfiguredHistoricalS05([]byte(invalid), digest) {
			t.Fatal("unreviewed lifecycle accepted")
		}
	}
	if reviewedUpstreamS05([]byte(configured)) {
		t.Fatal("synthetic template admitted as immutable public source")
	}
}

func setupTestAliases(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string, 2)
	for _, name := range []string{"zkeen.dat", "zkeenip.dat"} {
		path := filepath.Join(root, name)
		target := filepath.Join(root, setupAliasMember(name))
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("required symlink fixture unavailable: %v", err)
		}
		result[path] = target
	}
	return result
}

func assertSetupAliases(t *testing.T, aliases map[string]string) {
	t.Helper()
	for path, expected := range aliases {
		actual, err := os.Readlink(path)
		if err != nil || actual != expected {
			t.Fatalf("exact alias not restored: %v", err)
		}
	}
}

func TestSetupAliasesBindSnapshotAndRestorePartialRetirement(t *testing.T) {
	paths := setupTestPaths(t.TempDir())
	paths.PreviousDir = filepath.Join(filepath.Dir(paths.Journal), "previous")
	if err := os.MkdirAll(paths.XrayAssetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, member := range productGeodataCatalog {
		if err := os.WriteFile(filepath.Join(paths.XrayAssetDir, member.Name), []byte("old synthetic asset"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	aliases := setupTestAliases(t, paths.XrayAssetDir)
	service := setupTestService(t, paths)
	before, err := service.setupSourceGenerationDigest(nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.captureSetupSnapshot(context.Background(), "managed-takeover", nil)
	if err != nil {
		t.Fatal(err)
	}
	links, err := service.snapshotAliases(snapshot)
	if err != nil || len(links) != 2 {
		t.Fatalf("alias snapshot: %v", err)
	}
	if snapshot.Manifest.SchemaVersion != setupAliasSnapshotSchemaVersion {
		t.Fatal("alias snapshot could be admitted by an older reader")
	}
	accounted := int64(0)
	for _, entry := range snapshot.Manifest.Entries {
		accounted += entry.Size
	}
	if accounted != snapshot.Manifest.Bytes {
		t.Fatal("snapshot does not account for literal link payload bytes")
	}
	oldVersion := snapshot.Manifest.SchemaVersion
	snapshot.Manifest.SchemaVersion = SetupTransactionSchemaVersion
	if _, err := service.snapshotAliases(snapshot); err == nil {
		t.Fatal("new link kind accepted in an old manifest version")
	}
	snapshot.Manifest.SchemaVersion = oldVersion
	path := filepath.Join(paths.XrayAssetDir, "zkeen.dat")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("geosite_zkeen.dat", path); err != nil {
		t.Fatal(err)
	}
	if changed, err := service.setupSourceGenerationDigest(nil); err != nil || changed == before {
		t.Fatalf("link text not bound to Preview: %v", err)
	}
	if err := service.retireSetupAliases(snapshot); err == nil {
		t.Fatal("changed source alias retired")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// Crash after retiring one link: restoration must admit only the other
	// exact snapshot-owned link, without following it or permitting new links.
	if err := os.WriteFile(filepath.Join(paths.XrayAssetDir, "geosite_zkeen.dat"), []byte("new synthetic asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.restoreSetupSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	assertSetupAliases(t, aliases)
	if actual, err := service.setupSourceGenerationDigest(nil); err != nil || actual != before {
		t.Fatalf("complete asset generation not restored: %v", err)
	}
	if err := service.retireSetupAliases(snapshot); err != nil || !setupAliasesAbsent(paths.XrayAssetDir) {
		t.Fatalf("retirement: %v", err)
	}
	if err := service.restoreSetupSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	assertSetupAliases(t, aliases)
	// Tamper a protected link payload: reject before deleting the live root.
	for _, entry := range snapshot.Manifest.Entries {
		if entry.Kind == "symlink" {
			if err := os.WriteFile(filepath.Join(snapshot.Dir, "payload", entry.Payload), []byte("../../synthetic-escape"), 0o600); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := service.restoreSetupSnapshot(snapshot); err == nil {
		t.Fatal("tampered alias payload restored")
	}
	assertSetupAliases(t, aliases)
}

func TestSetupAliasManifestRejectsSubstitutionBeforeLiveRemoval(t *testing.T) {
	paths := setupTestPaths(t.TempDir())
	paths.PreviousDir = filepath.Join(filepath.Dir(paths.Journal), "previous")
	if err := os.MkdirAll(paths.XrayAssetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, member := range productGeodataCatalog {
		if err := os.WriteFile(filepath.Join(paths.XrayAssetDir, member.Name), []byte("synthetic catalog"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	aliases := setupTestAliases(t, paths.XrayAssetDir)
	service := setupTestService(t, paths)
	snapshot, err := service.captureSetupSnapshot(context.Background(), "managed-takeover", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, contour := range []string{"duplicate", "wrong-root", "wrong-key", "unknown-name", "missing-target", "bad-mode", "oversize"} {
		t.Run(contour, func(t *testing.T) {
			poisoned := snapshot
			poisoned.Manifest.Entries = append([]setupSnapshotEntry(nil), snapshot.Manifest.Entries...)
			for i := range poisoned.Manifest.Entries {
				entry := &poisoned.Manifest.Entries[i]
				if entry.Kind != "symlink" {
					continue
				}
				switch contour {
				case "duplicate":
					poisoned.Manifest.Entries = append(poisoned.Manifest.Entries, *entry)
				case "wrong-root":
					entry.Target = filepath.Dir(paths.XrayAssetDir)
				case "wrong-key":
					entry.Key = "xray-config"
				case "unknown-name":
					entry.Relative = "unknown.dat"
				case "missing-target":
					for j := range poisoned.Manifest.Entries {
						if poisoned.Manifest.Entries[j].Relative == setupAliasMember(entry.Relative) {
							poisoned.Manifest.Entries[j].Kind = "absent"
						}
					}
				case "bad-mode":
					entry.Mode = 0o600
				case "oversize":
					entry.Size = setupMaxAliasBytes + 1
				}
				break
			}
			if err := service.restoreSetupSnapshot(poisoned); err == nil {
				t.Fatal("substituted alias manifest restored")
			}
			assertSetupAliases(t, aliases)
		})
	}
}

func TestSetupRejectsUnknownEscapedDanglingAndChainedAliases(t *testing.T) {
	for _, contour := range []string{"unknown-name", "outside", "dangling", "chained", "regular-alias", "catalog-symlink"} {
		t.Run(contour, func(t *testing.T) {
			root := t.TempDir()
			allowed := map[string]struct{}{"geosite_zkeen.dat": {}, "geoip_zkeenip.dat": {}}
			member := filepath.Join(root, "geosite_zkeen.dat")
			if err := os.WriteFile(member, []byte("synthetic"), 0o600); err != nil {
				t.Fatal(err)
			}
			name, target := "zkeen.dat", member
			switch contour {
			case "unknown-name":
				name = "unexpected.dat"
			case "outside":
				target = filepath.Join(t.TempDir(), "geosite_zkeen.dat")
			case "dangling":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
			case "chained":
				target = filepath.Join(root, "another-link")
			case "regular-alias":
				if err := os.WriteFile(filepath.Join(root, name), []byte("synthetic"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "catalog-symlink":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
				name, target = "geosite_zkeen.dat", filepath.Join(t.TempDir(), "synthetic")
			}
			if contour != "regular-alias" {
				if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			if unexpected, err := setupAssetDirectoryHasUnexpected(root, allowed); err != nil || !unexpected {
				t.Fatalf("unsafe asset contour accepted: %v", err)
			}
		})
	}
}
