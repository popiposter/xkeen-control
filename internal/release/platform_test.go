package release

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPlatformCandidateIsolation(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"arm64", "mipsle"} {
		t.Run(arch, func(t *testing.T) {
			paths := map[string]string{}
			assets := map[string][]byte{}
			dir := t.TempDir()
			for _, name := range ArtifactsForArchitecture(arch) {
				assets[name] = []byte(name + " synthetic bytes")
				paths[name] = filepath.Join(dir, name)
				if err := os.WriteFile(paths[name], assets[name], 0600); err != nil {
					t.Fatal(err)
				}
			}
			manifest, err := BuildManifestForArchitecture("1.2.3", strings.Repeat("a", 40), "stable", 1750000000, arch, paths)
			if err != nil {
				t.Fatal(err)
			}
			body, err := manifest.MarshalDeterministic()
			if err != nil {
				t.Fatal(err)
			}
			sig, err := Sign(body, key)
			if err != nil {
				t.Fatal(err)
			}
			manifestName, signatureName := ManifestNames(arch)
			requests := []string{}
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				name := filepath.Base(r.URL.Path)
				mu.Lock()
				requests = append(requests, r.URL.Path)
				mu.Unlock()
				switch name {
				case manifestName:
					_, _ = w.Write(body)
				case signatureName:
					_, _ = w.Write(sig)
				default:
					if b, ok := assets[name]; ok {
						_, _ = w.Write(b)
					} else {
						http.NotFound(w, r)
					}
				}
			}))
			defer server.Close()
			client := NewClientForTestArchitecture(server.URL, key.Public().(ed25519.PublicKey), arch)
			if _, err := client.Check(context.Background(), "stable", ""); err != nil {
				t.Fatal(err)
			}
			candidate, err := client.FetchCandidate(context.Background(), "stable", "")
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Manifest.Architecture != arch || len(candidate.Assets) != 4 {
				t.Fatal("wrong platform candidate")
			}
			mu.Lock()
			pathsSeen := append([]string(nil), requests...)
			mu.Unlock()
			for _, path := range pathsSeen {
				if filepath.Base(path) != manifestName && !strings.Contains(path, "/releases/download/v1.2.3/") {
					t.Fatalf("unversioned payload request %q", path)
				}
			}
			foreign := "arm64"
			if arch == foreign {
				foreign = "mipsle"
			}
			candidate.Manifest.Architecture = foreign
			if err := VerifyCandidate(candidate); err == nil {
				t.Fatal("foreign binary set admitted")
			}
			candidate.Manifest.Architecture = "mips"
			if err := VerifyCandidate(candidate); err == nil {
				t.Fatal("big-endian MIPS admitted")
			}
		})
	}
}

func TestClientRejectsSignedForeignPlatformBeforeAssets(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest(t, "1.2.3", "stable")
	body, err := manifest.MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := Sign(body, key)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, ".json") {
			_, _ = w.Write(body)
		} else {
			_, _ = w.Write(sig)
		}
	}))
	defer server.Close()
	client := NewClientForTestArchitecture(server.URL, key.Public().(ed25519.PublicKey), "mipsle")
	if _, err := client.Check(context.Background(), "stable", ""); err == nil {
		t.Fatal("foreign Check admitted")
	}
	if _, err := client.FetchCandidate(context.Background(), "stable", ""); err == nil {
		t.Fatal("foreign Fetch admitted")
	}
	mu.Lock()
	seen := requests
	mu.Unlock()
	if seen != 2 {
		t.Fatalf("foreign platform progressed beyond metadata: %d requests", seen)
	}
	client = NewClientForTestArchitecture(server.URL, key.Public().(ed25519.PublicKey), "mips")
	if _, err := client.FetchCandidate(context.Background(), "stable", ""); err == nil {
		t.Fatal("unsupported platform admitted")
	}
	mu.Lock()
	seen = requests
	mu.Unlock()
	if seen != 2 {
		t.Fatal("unsupported platform downloaded metadata")
	}
}
