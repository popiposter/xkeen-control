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
	"testing"
)

func TestManifestSignatureAndDeterministicValidation(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest(t, "1.2.3", "stable")
	first, err := manifest.MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	second, err := manifest.MarshalDeterministic()
	if err != nil || string(first) != string(second) {
		t.Fatalf("manifest encoding is not deterministic: %v", err)
	}
	signature, err := Sign(first, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(first, signature, privateKey.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), first...)
	tampered[len(tampered)-2] ^= 1
	if err := Verify(tampered, signature, privateKey.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("tampered manifest verified")
	}
	if _, err := ParseManifest(append(first, []byte(`{}\n`)...)); err == nil {
		t.Fatal("manifest with trailing JSON verified")
	}
	manifest.Artifacts = append(manifest.Artifacts, Artifact{Name: "install.sh", Size: 1, SHA256: strings.Repeat("0", 64)})
	if err := manifest.Validate(); err == nil {
		t.Fatal("duplicate artifact accepted")
	}
}

func TestFetchCandidateUsesFixedReleaseShapeAndVerifiesHashes(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest(t, "1.2.3", "stable")
	manifestBytes, err := manifest.MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign(manifestBytes, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	assets := map[string][]byte{}
	for _, artifact := range manifest.Artifacts {
		assets[artifact.Name] = []byte(artifact.Name + " synthetic payload")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		switch name {
		case "release-manifest.json":
			_, _ = w.Write(manifestBytes)
		case "release-manifest.sig":
			_, _ = w.Write(signature)
		default:
			_, _ = w.Write(assets[name])
		}
	}))
	defer server.Close()
	client := NewClientForTest(server.URL, privateKey.Public().(ed25519.PublicKey))
	candidate, err := client.FetchCandidate(context.Background(), "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCandidate(candidate); err != nil {
		t.Fatal(err)
	}
	assets["install.sh"][0] ^= 1
	candidate.Assets["install.sh"] = assets["install.sh"]
	if err := VerifyCandidate(candidate); err == nil {
		t.Fatal("tampered release asset verified")
	}
}

func TestManifestRejectsWrongSourceAndUnknownArtifact(t *testing.T) {
	manifest := testManifest(t, "1.2.3", "stable")
	manifest.SourceCommit = strings.Repeat("A", 40)
	if err := manifest.Validate(); err == nil {
		t.Fatal("uppercase source commit accepted")
	}
	manifest = testManifest(t, "1.2.3", "stable")
	manifest.Artifacts[0].Name = "unexpected"
	if err := manifest.Validate(); err == nil {
		t.Fatal("unknown artifact accepted")
	}
}

func testManifest(t *testing.T, version, channel string) Manifest {
	t.Helper()
	dir := t.TempDir()
	paths := make(map[string]string, len(RequiredArtifacts))
	for _, name := range RequiredArtifacts {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name+" synthetic payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	manifest, err := BuildManifest(version, strings.Repeat("a", 40), channel, 1_750_000_000, paths)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
