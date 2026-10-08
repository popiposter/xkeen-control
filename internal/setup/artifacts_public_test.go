package setup

import (
	"os"
	"path/filepath"
	"testing"
)

// Optional focused check mounts only operator-cached public release archives.
// Aggregate FULL is offline for upstream payloads and uses adverse fixtures.
func TestPinnedPublicArchives(t *testing.T) {
	dir := os.Getenv("XKEEN_TEST_PUBLIC_ARCHIVES")
	if dir == "" {
		t.Skip("public artifact cache not mounted")
	}
	native, e := os.ReadFile(filepath.Join(dir, "xkeen.tar.gz"))
	if e != nil {
		t.Fatal(e)
	}
	files, e := decodeNative(native)
	if e != nil || len(files) != 72 {
		t.Fatal("native pinned payload failed", e)
	}
	resolver, e := os.ReadFile(filepath.Join(dir, "mosdns-linux-arm64.zip"))
	if e != nil {
		t.Fatal(e)
	}
	binary, e := decodeDNS(resolver)
	if e != nil || len(binary) != 17891512 {
		t.Fatal("resolver pinned payload failed", e)
	}
}
