package components

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Immutable public historical source, not the digest of a configured appliance.
// jameszeroX/XKeen@e2d7a5f052f20ae4315eb2f248fedf8e564ddf1f:
// scripts/_xkeen/02_install/07_install_register/04_register_init.sh.
const reviewedHistoricalS05SHA256 = "f1aab626fddaf2026a92741e2506fb9550cbb1d3242892dcb1b36a03998ea484"
const setupMaxAliasBytes = 1024

// Older panel readers reject this private manifest version before touching the
// live directories. Public Setup DTOs and the shared journal remain version 1.
const setupAliasSnapshotSchemaVersion = 2

var historicalS05Assignments = map[string]string{
	"ipv4_exclude": "0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.168.0.0/16 224.0.0.0/4 255.255.255.255 78.47.125.180",
	"ipv6_exclude": "::/128 ::1/128 64:ff9b::/96 2001::/32 2002::/16 fd00::/8 ff00::/8 fe80::/10",
	"proxy_dns":    "off",
	"ipv6_support": "on",
}

func reviewedConfiguredHistoricalS05(contents []byte, expectedDigest string) bool {
	if len(contents) == 0 || int64(len(contents)) > setupMaxLifecycleBytes {
		return false
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\r", "\n")
	for key, original := range historicalS05Assignments {
		pattern := regexp.MustCompile(`(?m)^` + key + `="([^"\n]*)"$`)
		matches := pattern.FindAllStringSubmatchIndex(normalized, -1)
		if len(matches) != 1 {
			return false
		}
		m := matches[0]
		value := normalized[m[2]:m[3]]
		if key == "proxy_dns" || key == "ipv6_support" {
			if value != "on" && value != "off" {
				return false
			}
		} else if !setupLegacyAddressList(value, key == "ipv4_exclude") {
			return false
		}
		normalized = normalized[:m[2]] + original + normalized[m[3]:]
	}
	return digestSetupBytes([]byte(normalized)) == expectedDigest
}

func setupLegacyAddressList(value string, ipv4 bool) bool {
	if len(value) > 4096 {
		return false
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF:./ ", c) {
			return false
		}
	}
	items := strings.Fields(value)
	if len(items) > 128 {
		return false
	}
	for _, item := range items {
		address, err := netip.ParseAddr(item)
		if err != nil {
			prefix, prefixErr := netip.ParsePrefix(item)
			if prefixErr != nil {
				return false
			}
			address = prefix.Addr()
		}
		if address.Is4() != ipv4 || address.Zone() != "" {
			return false
		}
	}
	return true
}

func setupAliasMember(name string) string {
	switch name {
	case "zkeen.dat":
		return "geosite_zkeen.dat"
	case "zkeenip.dat":
		return "geoip_zkeenip.dat"
	default:
		return ""
	}
}

func validSetupAliasText(root, path, text string) bool {
	member := setupAliasMember(filepath.Base(path))
	return member != "" && filepath.Dir(path) == filepath.Clean(root) && (text == member || text == filepath.Join(root, member)) && len(text) <= setupMaxAliasBytes
}

// Read link text, never its contents. The separately named catalog target must
// be a regular file within the same fixed asset root.
func readSetupAlias(root, path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", errSetupLayoutInvalid
	}
	text, err := os.Readlink(path)
	if err != nil || !validSetupAliasText(root, path, text) {
		return "", errSetupLayoutInvalid
	}
	target, err := os.Lstat(filepath.Join(root, setupAliasMember(filepath.Base(path))))
	if err != nil || !target.Mode().IsRegular() || target.Mode()&os.ModeSymlink != 0 || target.Size() <= 0 || target.Size() > MaxGeodataFileBytes {
		return "", errSetupLayoutInvalid
	}
	return text, nil
}

func setupAssetDirectoryHasUnexpected(root string, allowed map[string]struct{}) (bool, error) {
	state, err := setupPathState(root)
	if err != nil {
		return false, err
	}
	if state == setupPathAbsent {
		return false, nil
	}
	if state != setupPathDirectory {
		return true, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if _, ok := allowed[entry.Name()]; ok {
			kind, err := setupPathState(path)
			if err != nil || kind != setupPathRegular {
				return true, nil
			}
			continue
		}
		if _, err := readSetupAlias(root, path); err != nil {
			return true, nil
		}
	}
	return false, nil
}

func setupAliasesAbsent(root string) bool {
	for _, name := range []string{"zkeen.dat", "zkeenip.dat"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
}

// Validate every link payload before any restoration or retirement. Old
// snapshots have no such entries and remain valid. Only the asset-root pair
// may use this private manifest kind; arbitrary links remain unsupported.
func (s *SetupService) snapshotAliases(snapshot setupSnapshot) (map[string]string, error) {
	result := make(map[string]string, 2)
	if snapshot.Dir == "" && len(snapshot.Manifest.Entries) == 0 {
		return result, nil
	}
	if snapshot.Manifest.SchemaVersion != SetupTransactionSchemaVersion && snapshot.Manifest.SchemaVersion != setupAliasSnapshotSchemaVersion {
		return nil, errSetupLayoutInvalid
	}
	for _, entry := range snapshot.Manifest.Entries {
		if entry.Kind != "symlink" {
			continue
		}
		if snapshot.Manifest.SchemaVersion != setupAliasSnapshotSchemaVersion {
			return nil, errSetupLayoutInvalid
		}
		if entry.Key != "xray-assets" || entry.Target != s.config.Paths.XrayAssetDir || entry.Relative != filepath.Base(entry.Relative) || entry.Mode != 0 || entry.Size < 1 || entry.Size > setupMaxAliasBytes || entry.Payload == "" || entry.Payload != filepath.Base(entry.Payload) || strings.ContainsAny(entry.Payload, `/\`) {
			return nil, errSetupLayoutInvalid
		}
		path := filepath.Join(entry.Target, entry.Relative)
		if _, duplicate := result[path]; duplicate {
			return nil, errSetupLayoutInvalid
		}
		contents, err := readBoundedSetupFile(filepath.Join(snapshot.Dir, "payload", entry.Payload), setupMaxAliasBytes)
		if err != nil || int64(len(contents)) != entry.Size || digestSetupBytes(contents) != entry.SHA256 || !validSetupAliasText(entry.Target, path, string(contents)) {
			return nil, errSetupLayoutInvalid
		}
		backedTarget := false
		for _, other := range snapshot.Manifest.Entries {
			if other.Key == "xray-assets" && other.Target == entry.Target && other.Relative == setupAliasMember(entry.Relative) && other.Kind == "file" {
				backedTarget = true
				break
			}
		}
		if !backedTarget {
			return nil, errSetupLayoutInvalid
		}
		result[path] = string(contents)
	}
	if snapshot.Manifest.SchemaVersion == setupAliasSnapshotSchemaVersion && len(result) == 0 {
		return nil, errSetupLayoutInvalid
	}
	return result, nil
}

func (s *SetupService) retireSetupAliases(snapshot setupSnapshot) error {
	aliases, err := s.snapshotAliases(snapshot)
	if err != nil {
		return err
	}
	for path, expected := range aliases {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return errSetupLayoutInvalid
		}
		text, err := os.Readlink(path)
		if err != nil || text != expected {
			return errSetupLayoutInvalid
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
