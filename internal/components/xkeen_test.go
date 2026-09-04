package components

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func TestXKeenCatalogStartsNonInstallable(t *testing.T) {
	entry, ok := reviewedXKeenEntry(xkeenCatalogBuildCommit, xkeenCatalogAsset)
	if !ok {
		t.Fatal("fixed XKeen build catalog entry is missing")
	}
	if err := validateXKeenCompatibilityEntry(entry); err != nil {
		t.Fatalf("catalog validation: %v", err)
	}
	if entry.Repository != "jameszeroX/XKeen" || entry.Channel != "dev" || entry.Version != "2.0.1" ||
		entry.CommitSHA != "e461c4e9964fb8ac78e5fe01aa2e27ab980af712" ||
		entry.SourceParentSHA != "bb4060d6a87364eff8314fa723a168454df372bd" ||
		entry.AssetName != "test/xkeen.tar.gz" || entry.BlobSHA != "e6218668692c41565d288bf3a0bc6a420650edbd" ||
		entry.SizeBytes != 111409 {
		t.Fatalf("catalog identity changed: %+v", entry)
	}
	if entry.Installable || entry.SHA256 != "" || entry.GenerationSHA256 != "" || len(entry.ArchiveMembers) != 0 {
		t.Fatalf("unqualified entry became installable: %+v", entry)
	}
	identity := XKeenReleaseIdentity{
		Repository: entry.Repository, Channel: entry.Channel, Tag: entry.Tag, Version: entry.Version,
		CommitSHA: entry.CommitSHA, SourceParentSHA: entry.SourceParentSHA, AssetName: entry.AssetName,
		BlobSHA: entry.BlobSHA, SizeBytes: entry.SizeBytes,
	}
	if validXKeenIdentity(identity) {
		t.Fatal("unqualified catalog identity authorized mutation")
	}
	if _, _, err := markerForGeneration(strings.Repeat("a", 64)); !errors.Is(err, errXKeenMarkerInvalid) {
		t.Fatalf("marker authorization error = %v", err)
	}
}

func TestXKeenResolverDoesNotFetchNonInstallableCatalog(t *testing.T) {
	var calls atomic.Int32
	resolver := NewXKeenResolver(nil, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected network request")
	})})
	_, err := resolver.ResolveXKeen(context.Background())
	if !errors.Is(err, ErrXKeenResolutionUnavailable) || calls.Load() != 0 {
		t.Fatalf("resolver result = %v, network calls = %d", err, calls.Load())
	}
}

func TestXKeenResolverUsesOnlyPinnedBuildAndTreePaths(t *testing.T) {
	contents := []byte("resolver fixture")
	entry, identity := installableCatalogFixture(t, contents)
	commitBody := fmt.Sprintf(`{"sha":%q,"commit":{"message":%q,"verification":{"verified":true}},"parents":[{"sha":%q}],"files":[{"filename":%q,"status":"modified","sha":%q}]}`, entry.CommitSHA, xkeenDevBuildCommitMessage, entry.SourceParentSHA, entry.AssetName, entry.BlobSHA)
	treeBody := fmt.Sprintf(`{"truncated":false,"tree":[{"path":%q,"mode":"100644","type":"blob","sha":%q,"size":%d}]}`, entry.AssetName, entry.BlobSHA, entry.SizeBytes)
	var paths []string
	resolver := NewXKeenResolver(nil, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.EscapedPath()+func() string {
			if request.URL.RawQuery == "" {
				return ""
			}
			return "?" + request.URL.RawQuery
		}())
		var body string
		switch paths[len(paths)-1] {
		case xkeenBuildCommitPath:
			body = commitBody
		case xkeenBuildTreePath:
			body = treeBody
		default:
			return nil, fmt.Errorf("unexpected metadata path %s", paths[len(paths)-1])
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
	})})
	resolved, err := resolver.ResolveXKeen(context.Background())
	if err != nil || !sameXKeenIdentity(resolved, identity) {
		t.Fatalf("resolved identity = %+v, err=%v", resolved, err)
	}
	if len(paths) != 2 || paths[0] != xkeenBuildCommitPath || paths[1] != xkeenBuildTreePath {
		t.Fatalf("metadata paths = %v", paths)
	}
}

func TestXKeenArtifactDownloaderReadsExactBlobEndpoint(t *testing.T) {
	contents := []byte("transport fixture")
	entry, identity := installableCatalogFixture(t, contents)
	encoded := base64.StdEncoding.EncodeToString(contents)
	body := fmt.Sprintf(`{"sha":%q,"size":%d,"encoding":"base64","content":%q}`, entry.BlobSHA, len(contents), encoded)
	var requestPath string
	client := NewXKeenArtifactDownloader(nil, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestPath = request.URL.EscapedPath()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
	})})
	var destination bytes.Buffer
	if err := client.DownloadXKeen(context.Background(), identity, &destination); err != nil {
		t.Fatalf("blob download: %v", err)
	}
	if requestPath != xkeenBlobPathPrefix+entry.BlobSHA || string(destination.Bytes()) != string(contents) {
		t.Fatalf("blob request/output = %q / %q", requestPath, destination.Bytes())
	}
}

func TestXKeenApplySwapsOnlyTheManagedPairAndUsesFixedRuntime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("transaction fixture is qualified in the Linux container")
	}
	root := t.TempDir()
	activeBinary := filepath.Join(root, "opt", "sbin", "xkeen")
	moduleDir := filepath.Join(root, "opt", "sbin", ".xkeen")
	initPath := filepath.Join(root, "opt", "etc", "init.d", "S05xkeen")
	xrayBinary := filepath.Join(root, "opt", "sbin", "xray")
	configDir := filepath.Join(root, "opt", "etc", "xray", "configs")
	assetDir := filepath.Join(root, "opt", "etc", "xray", "dat")
	xkeenConfig := filepath.Join(filepath.Dir(configDir), "xkeen", "xkeen.json")
	markerPath := filepath.Join(root, "opt", "etc", "xkeen-control", "state", "xkeen-generation.json")
	previousDir := filepath.Join(root, "opt", "etc", "xkeen-control", "previous", "components", "xkeen")
	journalPath := filepath.Join(root, "opt", "etc", "xkeen-control", "state", "component-transaction.json")
	stagingDir := filepath.Join(root, "tmp", "xkeen-control", "components", "xkeen")
	activationPath := filepath.Join(root, "opt", "sbin", ".xkeen-control-activation")
	for _, directory := range []string{filepath.Dir(activeBinary), moduleDir, filepath.Dir(initPath), filepath.Dir(xrayBinary), configDir, assetDir, filepath.Dir(xkeenConfig), filepath.Dir(markerPath), filepath.Dir(previousDir), filepath.Dir(journalPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(activeBinary, []byte("old-xkeen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "old.sh"), []byte("old-module"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xrayBinary, []byte("old-xray"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xkeenConfig, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name := range xrayCandidateFiles {
		if strings.HasPrefix(name, "xray/") {
			if err := os.WriteFile(filepath.Join(configDir, filepath.Base(name)), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	oldGeneration, err := readXKeenGeneration(activeBinary, moduleDir)
	if err != nil {
		t.Fatalf("old generation: %v", err)
	}
	archivePath := writeTestGzipTar(t, []testTarEntry{
		{name: "_xkeen/", kind: tar.TypeDir, mode: 0o755},
		{name: "_xkeen/runtime.sh", kind: tar.TypeReg, mode: 0o755, contents: []byte("candidate shell text")},
		{name: "xkeen", kind: tar.TypeReg, mode: 0o755, contents: []byte("candidate binary text")},
	})
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	entry, identity := installableCatalogFixture(t, archive)
	entry.ArchiveMembers = []XKeenArchiveMember{
		{Name: "_xkeen/", Type: xkeenArchiveDirectory, Mode: 0o755},
		{Name: "_xkeen/runtime.sh", Type: xkeenArchiveRegular, Mode: 0o755, Size: int64(len("candidate shell text"))},
		{Name: "xkeen", Type: xkeenArchiveRegular, Mode: 0o755, Size: int64(len("candidate binary text"))},
	}
	probePath := filepath.Join(t.TempDir(), "candidate")
	candidate, err := extractXKeenArchiveMembers(context.Background(), archivePath, probePath, entry.ArchiveMembers)
	if err != nil {
		t.Fatalf("candidate qualification: %v", err)
	}
	entry.GenerationSHA256 = candidate.Generation
	identity.GenerationSHA256 = candidate.Generation
	identity.Generation = candidate.Generation
	reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)] = entry
	resolver := &fakeXKeenResolver{identity: identity}
	downloader := &fakeXKeenDownloader{archive: archive}
	authorityProvider := &fakeXrayAuthority{snapshot: XrayAuthoritySnapshot{
		Appliance: xrayTestAppliance(), Registry: xrayTestRegistry(t), Generation: sha256.Sum256([]byte("authority")),
	}}
	runtimeService := &fakeXrayRuntime{}
	service := NewXKeenService(XKeenConfig{
		Resolver: resolver, Downloader: downloader, Authority: authorityProvider, Runtime: runtimeService,
		CandidateProbe: &fakeTransactionalProbe{}, CandidateValidator: &fakeXrayCandidateValidator{},
		AuthorityLease: authority.NewLease(), Coordinator: &fakeXrayCoordinator{},
		ActiveBinaryPath: activeBinary, ModuleDir: moduleDir, LifecycleInitPath: initPath,
		LegacyInitPath:    filepath.Join(root, "opt", "etc", "init.d", "S24xray"),
		SiblingModulePath: filepath.Join(root, "opt", "sbin", "_xkeen"), InstallHelperPath: filepath.Join(root, "opt", "root", "install.sh"),
		MarkerPath: markerPath, XrayBinaryPath: xrayBinary, XrayConfigDir: configDir, XrayAssetDir: assetDir,
		PreviousDir: previousDir, JournalPath: journalPath, StagingDir: stagingDir, ActivationPath: activationPath,
		RestoreJournalPath: filepath.Join(root, "opt", "etc", "xkeen-control", "state", "restore.json"),
		PreservedPaths:     []string{xkeenConfig, initPath, xrayBinary, configDir, assetDir},
		AvailableSpace:     func(string) (uint64, error) { return ^uint64(0), nil }, SyncDirectory: func(string) error { return nil },
	})
	if err := service.Apply(context.Background(), identity); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if actual, err := os.ReadFile(activeBinary); err != nil || string(actual) != "candidate binary text" {
		t.Fatalf("active xkeen = %q, %v", actual, err)
	}
	if actual, err := os.ReadFile(filepath.Join(moduleDir, "runtime.sh")); err != nil || string(actual) != "candidate shell text" {
		t.Fatalf("active module = %q, %v", actual, err)
	}
	if actual, err := os.ReadFile(initPath); err != nil || string(actual) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("fixed init changed = %q, %v", actual, err)
	}
	if _, err := os.Stat(filepath.Join(root, "candidate-executed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate execution marker = %v", err)
	}
	previous, err := service.loadPreviousGeneration()
	if err != nil || !sameXKeenGeneration(previous.meta, oldGeneration) || len(previous.marker) != 0 {
		t.Fatalf("previous generation = %+v, marker=%q, err=%v", previous.meta, previous.marker, err)
	}
	if _, err := os.Lstat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal remains: %v", err)
	}
	if resolver.calls != 1 || downloader.calls != 1 || runtimeService.restartCalls != 1 {
		t.Fatalf("transaction calls = resolve:%d download:%d restart:%d", resolver.calls, downloader.calls, runtimeService.restartCalls)
	}
	if _, _, _, err := readXKeenMarker(markerPath); err != nil {
		t.Fatalf("managed marker: %v", err)
	}
}

func installableCatalogFixture(t *testing.T, contents []byte) (xkeenCompatibilityEntry, XKeenReleaseIdentity) {
	t.Helper()
	original := reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)]
	gitDigest := sha1.New()
	_, _ = fmt.Fprintf(gitDigest, "blob %d\x00", len(contents))
	_, _ = gitDigest.Write(contents)
	archiveDigest := sha256.Sum256(contents)
	entry := original
	entry.BlobSHA = hex.EncodeToString(gitDigest.Sum(nil))
	entry.SizeBytes = int64(len(contents))
	entry.SHA256 = hex.EncodeToString(archiveDigest[:])
	entry.GenerationSHA256 = strings.Repeat("a", 64)
	entry.Installable = true
	entry.ArchiveMembers = []XKeenArchiveMember{{Name: "_xkeen/", Type: xkeenArchiveDirectory, Mode: 0o755}, {Name: "_xkeen/empty", Type: xkeenArchiveRegular, Mode: 0o755, Size: 0}, {Name: "xkeen", Type: xkeenArchiveRegular, Mode: 0o755, Size: int64(len(contents))}}
	reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)] = entry
	t.Cleanup(func() {
		reviewedXKeenCompatibility[xkeenCompatibilityKey(xkeenCatalogBuildCommit, xkeenCatalogAsset)] = original
	})
	identity := XKeenReleaseIdentity{Repository: entry.Repository, Channel: entry.Channel, Tag: entry.Tag, Version: entry.Version, CommitSHA: entry.CommitSHA, SourceParentSHA: entry.SourceParentSHA, AssetName: entry.AssetName, BlobSHA: entry.BlobSHA, SizeBytes: entry.SizeBytes, SHA256: entry.SHA256, GenerationSHA256: entry.GenerationSHA256, Generation: entry.GenerationSHA256}
	return entry, identity
}

type fakeXKeenResolver struct {
	identity XKeenReleaseIdentity
	err      error
	calls    int
}

func (r *fakeXKeenResolver) ResolveXKeen(context.Context) (XKeenReleaseIdentity, error) {
	r.calls++
	if r.err != nil {
		return XKeenReleaseIdentity{}, r.err
	}
	return r.identity, nil
}

type fakeXKeenDownloader struct {
	archive []byte
	err     error
	calls   int
}

func (d *fakeXKeenDownloader) DownloadXKeen(_ context.Context, _ XKeenReleaseIdentity, destination io.Writer) error {
	d.calls++
	if d.err != nil {
		return d.err
	}
	_, err := destination.Write(d.archive)
	return err
}

func TestXKeenBlobDecoderRequiresGitIdentityAndArchiveDigest(t *testing.T) {
	contents := []byte("fixed blob bytes")
	gitDigest := sha1.New()
	_, _ = fmt.Fprintf(gitDigest, "blob %d\x00", len(contents))
	_, _ = gitDigest.Write(contents)
	archiveDigest := sha256.Sum256(contents)
	entry := xkeenCompatibilityEntry{
		BlobSHA:   hex.EncodeToString(gitDigest.Sum(nil)),
		SizeBytes: int64(len(contents)),
		SHA256:    hex.EncodeToString(archiveDigest[:]),
	}
	encoded := base64.StdEncoding.EncodeToString(contents)
	body := []byte(fmt.Sprintf(`{"sha":%q,"size":%d,"encoding":"base64","content":%q}`, entry.BlobSHA, len(contents), encoded[:8]+"\n"+encoded[8:]))
	decoded, err := decodeXKeenBlob(body, entry)
	if err != nil || string(decoded) != string(contents) {
		t.Fatalf("blob decode = %q, %v", decoded, err)
	}
	for _, malformed := range []string{
		fmt.Sprintf(`{"sha":%q,"size":%d,"encoding":"base64","content":%q}`, entry.BlobSHA, len(contents), encoded+" "),
		fmt.Sprintf(`{"sha":%q,"size":%d,"encoding":"base64","content":%q}`, strings.Repeat("0", 40), len(contents), encoded),
	} {
		if _, err := decodeXKeenBlob([]byte(malformed), entry); err == nil {
			t.Fatalf("malformed Git blob was accepted: %s", malformed)
		}
	}
}

func TestXKeenCanonicalGenerationIncludesEmptyRegularFiles(t *testing.T) {
	root := t.TempDir()
	binaryPath := filepath.Join(root, "xkeen")
	modulePath := filepath.Join(root, ".xkeen")
	if err := os.MkdirAll(filepath.Join(modulePath, "02_install"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modulePath, "empty"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modulePath, "02_install", "script.sh"), []byte("echo inert\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	metadata, err := readXKeenGeneration(binaryPath, modulePath)
	if err != nil {
		t.Fatalf("read generation: %v", err)
	}
	if len(metadata.Entries) != 5 || metadata.Bytes != int64(len("binary")+len("echo inert\n")) || !isHexSHA256(metadata.Generation) {
		t.Fatalf("generation metadata = %+v", metadata)
	}
	for _, entry := range metadata.Entries {
		if entry.Path == ".xkeen/empty" && (entry.Size != 0 || entry.SHA256 != emptySHA256()) {
			t.Fatalf("empty regular file metadata = %+v", entry)
		}
	}
	if metadata.Generation != canonicalXKeenGenerationDigest(metadata.Entries) {
		t.Fatal("generation digest is not deterministic")
	}
}

func TestXKeenStrictGzipTarReaderPinsMembersAndRejectsUnsafeTypes(t *testing.T) {
	members := []XKeenArchiveMember{
		{Name: "_xkeen/", Type: xkeenArchiveDirectory, Mode: 0o755},
		{Name: "_xkeen/empty", Type: xkeenArchiveRegular, Mode: 0o755, Size: 0},
		{Name: "xkeen", Type: xkeenArchiveRegular, Mode: 0o755, Size: 6},
	}
	validEntries := []testTarEntry{
		{name: "_xkeen/", kind: tar.TypeDir, mode: 0o755},
		{name: "_xkeen/empty", kind: tar.TypeReg, mode: 0o755},
		{name: "xkeen", kind: tar.TypeReg, mode: 0o755, contents: []byte("binary")},
	}
	t.Run("valid zero length regular", func(t *testing.T) {
		archive := writeTestGzipTar(t, validEntries)
		if _, err := extractXKeenArchiveMembers(context.Background(), archive, filepath.Join(t.TempDir(), "candidate"), members); err != nil {
			t.Fatalf("valid archive rejected: %v", err)
		}
	})
	t.Run("unknown member", func(t *testing.T) {
		archive := writeTestGzipTar(t, []testTarEntry{
			validEntries[0], validEntries[1],
			{name: "_xkeen/extra", kind: tar.TypeReg, mode: 0o755, contents: []byte("x")},
			validEntries[2],
		})
		if _, err := extractXKeenArchiveMembers(context.Background(), archive, filepath.Join(t.TempDir(), "candidate"), members); !errors.Is(err, errXKeenArchiveLayoutInvalid) {
			t.Fatalf("unknown member error = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		archive := writeTestGzipTar(t, []testTarEntry{
			validEntries[0],
			{name: "_xkeen/empty", kind: tar.TypeSymlink, mode: 0o755, linkname: "xkeen"},
			validEntries[2],
		})
		if _, err := extractXKeenArchiveMembers(context.Background(), archive, filepath.Join(t.TempDir(), "candidate"), members); err == nil {
			t.Fatal("symlink archive was accepted")
		}
	})
	t.Run("trailing compressed data", func(t *testing.T) {
		archive := writeTestGzipTar(t, validEntries)
		file, err := os.OpenFile(archive, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte{0x01}); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := extractXKeenArchiveMembers(context.Background(), archive, filepath.Join(t.TempDir(), "candidate"), members); err == nil {
			t.Fatal("trailing compressed data was accepted")
		}
	})
}

func TestXKeenLifecycleRejectsLegacyArtifactsWithoutMutation(t *testing.T) {
	root := t.TempDir()
	initPath := filepath.Join(root, "init", "S05xkeen")
	sibling := filepath.Join(root, "sbin", "_xkeen")
	if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initPath, []byte("preserved\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	service := NewXKeenService(XKeenConfig{LifecycleInitPath: initPath, LegacyInitPath: filepath.Join(root, "init", "S24xray"), SiblingModulePath: sibling, InstallHelperPath: filepath.Join(root, "root", "install.sh")})
	if err := service.validateXKeenLifecycle(); err == nil {
		t.Fatal("legacy artifact was accepted")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("legacy artifact was mutated: %v", err)
	}
	contents, err := os.ReadFile(initPath)
	if err != nil || string(contents) != "preserved\n" {
		t.Fatalf("preserved init changed: %q, %v", contents, err)
	}
}

func TestXKeenRecoveryInspectorArbitratesStaging(t *testing.T) {
	root := t.TempDir()
	config := ComponentRecoveryConfig{
		JournalPath: filepath.Join(root, "state", "component-transaction.json"), RestoreJournalPath: filepath.Join(root, "state", "restore.json"),
		XrayPreviousStagingPath: filepath.Join(root, "xray.previous.staging"), XrayStagingDir: filepath.Join(root, "xray.staging"),
		GeodataPreviousStagingPath: filepath.Join(root, "geo.previous.staging"), GeodataStagingDir: filepath.Join(root, "geo.staging"),
		XKeenPreviousStagingPath: filepath.Join(root, "xkeen.previous.staging"), XKeenStagingDir: filepath.Join(root, "xkeen.staging"),
		XKeenActivationPath: filepath.Join(root, "activation"), XKeenMarkerStagingPath: filepath.Join(root, "state", "xkeen-generation.json.staging"),
	}
	if err := ensureXKeenOwnedDirectory(config.XKeenStagingDir, xkeenOwnerValue); err != nil {
		t.Fatal(err)
	}
	if state, err := InspectComponentRecovery(config); err != nil || state.Kind != KindXKeen || !state.XKeenStagingPresent || !state.Pending() {
		t.Fatalf("XKeen recovery state = %+v, err=%v", state, err)
	}
}

func emptySHA256() string {
	return "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}

func hexString(value []byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2], result[index*2+1] = alphabet[item>>4], alphabet[item&0x0f]
	}
	return string(result)
}

func formatMode(value uint32) string { return fmt.Sprintf("%o", value) }
func formatSize(value int64) string  { return fmt.Sprintf("%d", value) }

type testTarEntry struct {
	name     string
	kind     byte
	mode     int64
	contents []byte
	linkname string
}

func writeTestGzipTar(t *testing.T, entries []testTarEntry) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "xkeen.tar.gz")
	file, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: entry.mode, Size: int64(len(entry.contents)), Linkname: entry.linkname}
		if entry.kind == tar.TypeDir {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if len(entry.contents) > 0 {
			if _, err := tarWriter.Write(entry.contents); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
