package setup

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	_ "embed"
	"encoding/hex"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/netguard"
)

const nativeURL = "https://github.com/jameszeroX/XKeen/releases/download/2.1/xkeen.tar.gz"
const nativeSize = 129691
const nativeHash = "4b9350b11fab7fd3e4973db609db0fb780994f4b5ab0096f0549bc52acac8c73"
const dnsURL = "https://github.com/IrineSistiana/mosdns/releases/download/v5.3.4/mosdns-linux-arm64.zip"
const dnsSize = 6591859
const dnsHash = "82d80a1a21606fca0bc6b65ac6f90d30cff6bb4a19a6ab6a246cf247dbb78bc0"
const xrayVersion = "v26.3.27"

//go:embed native-members.txt
var nativeMembers string

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func download(ctx context.Context, url string, size int, digest string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	t := &http.Transport{Proxy: nil, DisableCompression: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: netguard.Dialer{Resolver: netguard.DefaultResolver{}}.DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	defer t.CloseIdleConnections()
	c := &http.Client{Transport: t, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Scheme != "https" || r.URL.User != nil {
			return ErrState
		}
		switch r.URL.Host {
		case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
			return nil
		default:
			return ErrState
		}
	}}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, ErrState
	}
	r.Header.Set("User-Agent", "xkeen-control-setup/1")
	resp, e := c.Do(r)
	if e != nil {
		return nil, ErrState
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > int64(size) {
		return nil, ErrState
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, int64(size)+1))
	if e != nil || len(b) != size || sum(b) != digest {
		return nil, ErrState
	}
	return b, nil
}

func decodeNative(b []byte) (map[string][]byte, error) {
	if len(b) != nativeSize || sum(b) != nativeHash {
		return nil, ErrState
	}
	return decodeNativeMembers(b, nativeMembers)
}
func decodeNativeMembers(b []byte, members string) (map[string][]byte, error) {
	want := map[string]bool{}
	for _, n := range strings.Fields(members) {
		want[n] = true
	}
	g, e := gzip.NewReader(bytes.NewReader(b))
	if e != nil {
		return nil, ErrState
	}
	defer g.Close()
	tr := tar.NewReader(g)
	out := map[string][]byte{}
	total := int64(0)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil || h == nil {
			return nil, ErrState
		}
		if !want[h.Name] || out[h.Name] != nil || path.Clean(h.Name) != h.Name || strings.ContainsAny(h.Name, "\\\x00") || h.Typeflag != tar.TypeReg || h.Linkname != "" || h.Size < 1 || h.Size > 512<<10 || h.Mode&007022 != 0 {
			return nil, ErrState
		}
		total += h.Size
		if total > 4<<20 {
			return nil, ErrState
		}
		data, e := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if e != nil || int64(len(data)) != h.Size {
			return nil, ErrState
		}
		out[h.Name] = data
	}
	if len(out) != len(want) {
		return nil, ErrState
	}
	return out, nil
}

func decodeDNS(b []byte) ([]byte, error) {
	if len(b) != dnsSize || sum(b) != dnsHash {
		return nil, ErrState
	}
	return decodeDNSMembers(b)
}
func decodeDNSMembers(b []byte) ([]byte, error) {
	r, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil || len(r.File) > 4 {
		return nil, ErrState
	}
	seen := map[string]bool{}
	var binary []byte
	for _, f := range r.File {
		if seen[f.Name] || path.Clean(f.Name) != f.Name || strings.ContainsAny(f.Name, "\\\x00") || !f.Mode().IsRegular() || f.UncompressedSize64 > 32<<20 {
			return nil, ErrState
		}
		seen[f.Name] = true
		switch f.Name {
		case "mosdns", "LICENSE", "README.md", "config.yaml":
		default:
			return nil, ErrState
		}
		h, e := f.Open()
		if e != nil {
			return nil, ErrState
		}
		data, e := io.ReadAll(io.LimitReader(h, 32<<20+1))
		closeErr := h.Close()
		if e != nil || closeErr != nil || uint64(len(data)) != f.UncompressedSize64 {
			return nil, ErrState
		}
		if f.Name == "mosdns" {
			binary = data
		}
	}
	if len(binary) < 1<<20 || !bytes.HasPrefix(binary, []byte{0x7f, 'E', 'L', 'F'}) || len(binary) < 20 || binary[4] != 2 || binary[5] != 1 || binary[18] != 183 || binary[19] != 0 {
		return nil, ErrState
	}
	return binary, nil
}
