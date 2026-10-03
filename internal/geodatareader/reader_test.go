package geodatareader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

func bytesField(n protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, n, protowire.BytesType), value)
}
func intField(n protowire.Number, value uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, n, protowire.VarintType), value)
}
func site(code string, kind uint64, value string) []byte {
	domain := append(intField(1, kind), bytesField(2, []byte(value))...)
	return bytesField(1, append(bytesField(1, []byte(code)), bytesField(2, domain)...))
}
func fixture(t *testing.T, name string, data []byte) *Reader {
	t.Helper()
	r := &Reader{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(r.Dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestInstalledDomainTypesPagingAndUpdateInvalidation(t *testing.T) {
	data := append(site("ROOT", 2, "example.test"), site("EXACT", 3, "www.example.test")...)
	data = append(data, site("WORDS", 0, "example")...)
	data = append(data, site("PATTERN", 1, `^www\.example\.test$`)...)
	r := fixture(t, "geosite_vendor.dat", data)
	q := Request{File: "geosite_vendor.dat", View: "match", Search: "www.example.test", Limit: 2}
	page, err := r.Query(context.Background(), q)
	if err != nil || page.Total != 4 || len(page.Items) != 2 || !page.More {
		t.Fatal(page, err)
	}
	q.Offset = 2
	q.Snapshot = page.Snapshot
	last, err := r.Query(context.Background(), q)
	if err != nil || last.More || last.Items[1].Type != "regexp" {
		t.Fatal(last, err)
	}
	q.Offset = 0
	q.Search = "notexample.test"
	other, err := r.Query(context.Background(), q)
	if err != nil || other.Total != 1 || other.Items[0].Type != "substring" {
		t.Fatal("suffix boundary mistaken", other, err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, q.File), site("NEW", 3, "new.test"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Query(context.Background(), q); err == nil {
		t.Fatal("stale page accepted after native update")
	}
	q.Snapshot = ""
	q.View = "categories"
	q.Search = ""
	updated, err := r.Query(context.Background(), q)
	if err != nil || updated.Items[0].Category != "new" {
		t.Fatal(updated, err)
	}
}

func TestInstalledIPMembershipAndInverseCategory(t *testing.T) {
	cidr := append(bytesField(1, []byte{192, 0, 2, 0}), intField(2, 24)...)
	entry := append(bytesField(1, []byte("TEST")), bytesField(2, cidr)...)
	inverse := append(bytesField(1, []byte("INVERSE")), bytesField(2, cidr)...)
	inverse = append(inverse, intField(3, 1)...)
	r := fixture(t, "geoip.dat", append(bytesField(1, entry), bytesField(1, inverse)...))
	for _, test := range []struct{ ip, category, kind string }{{"192.0.2.5", "test", "cidr"}, {"198.51.100.3", "inverse", "inverse"}} {
		result, err := r.Query(context.Background(), Request{File: "geoip.dat", View: "match", Search: test.ip})
		if err != nil || result.Total != 1 || result.Items[0].Category != test.category || result.Items[0].Type != test.kind {
			t.Fatal(result, err)
		}
	}
}

func TestReaderRejectsUnsafePathsMalformedAndBoundedInputs(t *testing.T) {
	r := fixture(t, "geosite.dat", site("GOOD", 3, "example.test"))
	for _, q := range []Request{{File: "../geosite.dat", View: "categories"}, {File: "geosite.dat", View: "entries", Limit: 101}, {File: "geosite.dat", View: "match", Search: strings.Repeat("a", 254)}} {
		if _, err := r.Query(context.Background(), q); err == nil {
			t.Fatal("unsafe request accepted", q)
		}
	}
	link := filepath.Join(r.Dir, "geosite_link.dat")
	if err := os.Symlink(filepath.Join(r.Dir, "geosite.dat"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Query(context.Background(), Request{File: "geosite_link.dat", View: "categories"}); err == nil {
		t.Fatal("symlink followed")
	}
	malformed := append([]byte{10}, protowire.AppendVarint(nil, maxEntry+1)...)
	if err := os.WriteFile(filepath.Join(r.Dir, "geosite.dat"), malformed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Query(context.Background(), Request{File: "geosite.dat", View: "categories"}); err == nil {
		t.Fatal("oversized entry accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Query(canceled, Request{File: "geosite.dat", View: "categories"}); err == nil {
		t.Fatal("canceled read accepted")
	}
}

// Explicit operator-local snapshot qualification. Ordinary fixtures require no
// router, public downloads or production material in the repository.
func TestReaderInstalledSnapshot(t *testing.T) {
	dir := os.Getenv("XKEEN_TEST_INSTALLED_DAT")
	if dir == "" {
		t.Skip("installed snapshot not supplied")
	}
	r := &Reader{Dir: dir}
	inventory, err := r.Inventory()
	if err != nil || len(inventory) == 0 {
		t.Fatal(inventory, err)
	}
	for _, file := range inventory {
		t.Run(file.Name, func(t *testing.T) {
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			catalog, err := r.Query(ctx, Request{File: file.Name, View: "categories", Limit: 1})
			if err != nil || catalog.Total == 0 {
				t.Fatal("catalog", err)
			}
			contents, err := r.Query(ctx, Request{File: file.Name, View: "entries", Category: catalog.Items[0].Category, Snapshot: catalog.Snapshot, Limit: 1})
			if err != nil {
				t.Fatal("contents", err)
			}
			search := "example.com"
			if file.Kind == "geoip" {
				search = "1.1.1.1"
			}
			matched, err := r.Query(ctx, Request{File: file.Name, View: "match", Search: search, Snapshot: catalog.Snapshot, Limit: 1})
			if err != nil {
				t.Fatal("membership", err)
			}
			t.Logf("size=%d categories=%d firstCategoryEntries=%d matchingEntries=%d elapsed=%s snapshot=%s", file.Size, catalog.Total, contents.Total, matched.Total, time.Since(start), catalog.Snapshot)
		})
	}
}
