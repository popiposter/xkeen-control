package setup

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
)

func archiveFixture(t *testing.T, headers ...tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	w := tar.NewWriter(g)
	for _, h := range headers {
		if w.WriteHeader(&h) != nil {
			t.Fatal("header")
		}
		if h.Typeflag == tar.TypeReg {
			if _, e := w.Write(bytes.Repeat([]byte("x"), int(h.Size))); e != nil {
				t.Fatal(e)
			}
		}
	}
	if w.Close() != nil || g.Close() != nil {
		t.Fatal("close")
	}
	return b.Bytes()
}
func TestNativeArchiveRejectsUnexpectedUnsafeAndDuplicateMembers(t *testing.T) {
	good := tar.Header{Name: "xkeen", Typeflag: tar.TypeReg, Mode: 0755, Size: 10}
	if _, e := decodeNativeMembers(archiveFixture(t, good), "xkeen"); e != nil {
		t.Fatal(e)
	}
	for _, h := range []tar.Header{
		{Name: "../xkeen", Typeflag: tar.TypeReg, Mode: 0755, Size: 1},
		{Name: "xkeen", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0755},
		{Name: "xkeen", Typeflag: tar.TypeReg, Mode: 0777, Size: 1},
		{Name: "unreviewed", Typeflag: tar.TypeReg, Mode: 0755, Size: 1},
	} {
		if _, e := decodeNativeMembers(archiveFixture(t, h), "xkeen"); e == nil {
			t.Fatal("unsafe payload admitted")
		}
	}
	if _, e := decodeNativeMembers(archiveFixture(t, good, good), "xkeen"); e == nil {
		t.Fatal("duplicate admitted")
	}
	if _, e := decodeNative(archiveFixture(t, good)); e == nil {
		t.Fatal("unpinned archive admitted")
	}
}
func zipFixture(t *testing.T, name string, mode uint32) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	h.CreatorVersion = 3 << 8
	h.ExternalAttrs = mode << 16
	f, e := w.CreateHeader(h)
	if e != nil {
		t.Fatal(e)
	}
	data := make([]byte, 1<<20)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1})
	data[18] = 183
	if _, e = f.Write(data); e != nil {
		t.Fatal(e)
	}
	if w.Close() != nil {
		t.Fatal("close")
	}
	return b.Bytes()
}
func TestResolverArchiveRequiresARM64AndFixedRegularMembers(t *testing.T) {
	if _, e := decodeDNSMembers(zipFixture(t, "mosdns", 0100755)); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"../mosdns", "arbitrary"} {
		if _, e := decodeDNSMembers(zipFixture(t, name, 0100755)); e == nil {
			t.Fatal("unsafe resolver archive")
		}
	}
	if _, e := decodeDNSMembers(zipFixture(t, "mosdns", 0120777)); e == nil {
		t.Fatal("symlink resolver")
	}
	if _, e := decodeDNS(zipFixture(t, "mosdns", 0100755)); e == nil {
		t.Fatal("unpinned resolver")
	}
}
