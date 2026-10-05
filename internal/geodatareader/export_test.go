package geodatareader

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExportPreservesTypesAndAttributeSelection(t *testing.T) {
	// Two categories and all four matcher kinds; attribute filters are semantic.
	var domains []byte
	for kind, value := range []string{"keyword", "^regex\\.test$", "domain.test", "full.test"} {
		d := append(intField(1, uint64(kind)), bytesField(2, []byte(value))...)
		if kind%2 == 0 {
			d = append(d, bytesField(3, bytesField(1, []byte("cn")))...)
		}
		domains = append(domains, bytesField(2, d)...)
	}
	data := bytesField(1, append(bytesField(1, []byte("TEST")), domains...))
	r := fixture(t, "geosite_test.dat", data)
	got, digest, err := r.ExportDomains(context.Background(), "geosite_test.dat", []string{"TEST", "TEST@cn", "TEST@!cn"})
	if err != nil || len(digest) != 64 || !reflect.DeepEqual(got["TEST@cn"], []string{"domain:domain.test", "keyword:keyword"}) || !reflect.DeepEqual(got["TEST@!cn"], []string{"full:full.test", "regexp:^regex\\.test$"}) || len(got["TEST"]) != 4 {
		t.Fatal(got, digest, err)
	}
	if _, _, err = r.ExportDomains(context.Background(), "geosite_test.dat", []string{"MISSING"}); err == nil {
		t.Fatal("missing category accepted")
	}
	if _, _, err = r.ExportDomains(context.Background(), "../geosite_test.dat", []string{"TEST"}); err == nil {
		t.Fatal("path accepted")
	}
	if err = os.Remove(filepath.Join(r.Dir, "geosite_test.dat")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.ExportDomains(context.Background(), "geosite_test.dat", []string{"TEST"}); err == nil {
		t.Fatal("missing input accepted")
	}
}
