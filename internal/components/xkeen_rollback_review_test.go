package components

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func TestXKeenRollbackExpectedRejectsRotatedPreviousBeforeActivation(t *testing.T) {
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
	xkeenConfig := filepath.Join(root, "opt", "etc", "xkeen", "xkeen.json")
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
	if err := os.Chmod(filepath.Dir(activeBinary), 0o755); err != nil {
		t.Fatal(err)
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
	if err := os.WriteFile(xrayBinary, bytes.Repeat([]byte("x"), MaxXKeenGenerationFileBytes+1), 0o700); err != nil {
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
	if err := os.WriteFile(filepath.Join(configDir, "05_routing.json"), []byte("{\"routing\":{\"rules\":[{\"domain\":[\"ext:synthetic\"]}]}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetFixture := filepath.Join(assetDir, "synthetic.dat")
	if err := os.WriteFile(assetFixture, bytes.Repeat([]byte("g"), MaxXKeenGenerationFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	archivePath := writeTestGzipTar(t, []testTarEntry{
		{name: "_xkeen/runtime.sh", kind: tar.TypeReg, mode: 0o644, format: tar.FormatGNU, contents: []byte("candidate shell text")},
		{name: "xkeen", kind: tar.TypeReg, mode: 0o755, format: tar.FormatGNU, contents: []byte("candidate binary text")},
	})
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	entry, identity := installableCatalogFixture(t, archive)
	entry.ArchiveMembers = []XKeenArchiveMember{
		{Name: "_xkeen/runtime.sh", Type: xkeenArchiveRegular, Mode: 0o644, Size: int64(len("candidate shell text"))},
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
	validator := &xkeenAssetAwareValidator{expectedAssetDir: assetDir, requiredAsset: assetFixture, requiredExpression: "ext:synthetic"}
	authoritySnapshot := xrayTestAppliance()
	authoritySnapshot.Routing.Rules[0].Domain = []string{"ext:synthetic"}
	authorityProvider := &fakeXrayAuthority{snapshot: XrayAuthoritySnapshot{
		Appliance: authoritySnapshot, Registry: xrayTestRegistry(t),
		Generation: [32]byte{1},
	}}
	runtimeService := &fakeXrayRuntime{}
	service := NewXKeenService(XKeenConfig{
		Resolver: resolver, Downloader: downloader, Authority: authorityProvider, Runtime: runtimeService,
		CandidateProbe: &fakeTransactionalProbe{}, CandidateValidator: validator,
		AuthorityLease: authority.NewLease(), Coordinator: &fakeXrayCoordinator{},
		ActiveBinaryPath: activeBinary, ModuleDir: moduleDir, LifecycleInitPath: initPath,
		LegacyInitPath:    filepath.Join(root, "opt", "etc", "init.d", "S24xray"),
		SiblingModulePath: filepath.Join(root, "opt", "sbin", "_xkeen"), InstallHelperPath: filepath.Join(root, "opt", "root", "install.sh"),
		MarkerPath: markerPath, XrayBinaryPath: xrayBinary, XrayConfigDir: configDir, XrayAssetDir: assetDir,
		PreviousDir: previousDir, JournalPath: journalPath, StagingDir: stagingDir, ActivationPath: activationPath,
		RestoreJournalPath: filepath.Join(root, "opt", "etc", "xkeen-control", "state", "restore.json"),
		PreservedPaths:     []string{xkeenConfig, initPath, xrayBinary, configDir, assetDir},
		AvailableSpace:     func(string) (uint64, error) { return ^uint64(0), nil },
		SyncDirectory:      func(string) error { return nil },
	})

	if err := service.Apply(context.Background(), identity); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	expected, err := service.PreviousGeneration()
	if err != nil {
		t.Fatalf("first previous generation: %v", err)
	}
	if err := service.Apply(context.Background(), identity); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	rotated, err := service.PreviousGeneration()
	if err != nil {
		t.Fatalf("rotated previous generation: %v", err)
	}
	if rotated.Generation == expected.Generation {
		t.Fatal("second apply did not rotate the retained previous generation")
	}
	activeBefore, markerBefore, _, err := service.readActiveGeneration()
	if err != nil {
		t.Fatalf("active generation before stale rollback: %v", err)
	}
	restartsBefore := runtimeService.restartCalls
	if err := service.RollbackExpected(context.Background(), expected); !errors.Is(err, ErrXKeenCandidateStale) {
		t.Fatalf("rotated rollback error = %v", err)
	}
	activeAfter, markerAfter, _, err := service.readActiveGeneration()
	if err != nil {
		t.Fatalf("active generation after stale rollback: %v", err)
	}
	if !sameXKeenGeneration(activeAfter, activeBefore) || !bytes.Equal(markerAfter, markerBefore) {
		t.Fatalf("stale rollback changed active generation")
	}
	if runtimeService.restartCalls != restartsBefore {
		t.Fatalf("stale rollback restarted runtime: before=%d after=%d", restartsBefore, runtimeService.restartCalls)
	}
	if present, _ := componentTransactionPresent(journalPath); present {
		t.Fatal("stale rollback left a transaction journal")
	}
	actual, err := service.PreviousGeneration()
	if err != nil {
		t.Fatalf("retained generation after stale rollback: %v", err)
	}
	if actual.Generation != rotated.Generation {
		t.Fatalf("stale rollback changed retained generation: got=%s want=%s", actual.Generation, rotated.Generation)
	}
}
