package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
)

type geodataFixture struct {
	root             string
	activeBinary     string
	configDir        string
	assetDir         string
	prefixedSentinel string
	previousDir      string
	journalPath      string
	stagingDir       string
	restorePath      string

	oldFiles map[string][]byte
	newFiles map[string][]byte
	set      GeodataCandidateSet

	authority   *fakeXrayAuthority
	resolver    *fakeGeodataResolver
	downloader  *fakeGeodataDownloader
	probe       *fakeTransactionalProbe
	validator   *fakeXrayCandidateValidator
	runtime     *fakeXrayRuntime
	coordinator *fakeXrayCoordinator
	lease       *authority.Lease
	service     *GeodataService
}

func newGeodataFixture(t *testing.T) *geodataFixture {
	t.Helper()
	root := t.TempDir()
	f := &geodataFixture{
		root:         root,
		activeBinary: filepath.Join(root, "xray", "xray"),
		configDir:    filepath.Join(root, "xray", "configs"),
		assetDir:     filepath.Join(root, "xray", "dat"),
		previousDir:  filepath.Join(root, "control", "previous", "components", "geodata"),
		journalPath:  filepath.Join(root, "control", "state", "component-transaction.json"),
		stagingDir:   filepath.Join(root, "tmp", "xkeen-control", "components", "geodata"),
		restorePath:  filepath.Join(root, "control", "state", "appliance-import-transaction.json"),
		oldFiles:     make(map[string][]byte, len(productGeodataCatalog)),
		newFiles:     make(map[string][]byte, len(productGeodataCatalog)),
		lease:        authority.NewLease(),
		coordinator:  &fakeXrayCoordinator{},
	}
	f.prefixedSentinel = filepath.Join(f.assetDir, ".xkeen-geodata-operator-note")
	for _, directory := range []string{filepath.Dir(f.activeBinary), f.configDir, f.assetDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.activeBinary, []byte("old-xray-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range productGeodataCatalog {
		old := []byte("old-" + entry.ID)
		fresh := []byte("new-" + entry.ID + "-geodata")
		f.oldFiles[entry.Name] = old
		f.newFiles[entry.Name] = fresh
		if err := os.WriteFile(filepath.Join(f.assetDir, entry.Name), old, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.assetDir, "manual-preserved.dat"), []byte("manual-bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.prefixedSentinel, []byte("prefix-sentinel"), 0o640); err != nil {
		t.Fatal(err)
	}
	items := make([]GeodataReleaseIdentity, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		payload := f.newFiles[entry.Name]
		digest := sha256.Sum256(payload)
		items[index] = GeodataReleaseIdentity{
			ID: entry.ID, Repository: entry.Repository, Tag: "2026.09.03",
			AssetName: entry.Asset, ActiveName: entry.Name, SizeBytes: int64(len(payload)), SHA256: fmt.Sprintf("%x", digest[:]),
		}
	}
	f.set = GeodataCandidateSet{Items: items, Generation: CandidateSetGeneration(items)}
	f.authority = &fakeXrayAuthority{events: new([]string)}
	f.authority.snapshot = XrayAuthoritySnapshot{
		Appliance:  xrayTestAppliance(),
		Registry:   xrayTestRegistry(t),
		Generation: sha256.Sum256([]byte("geodata-authority-1")),
	}
	f.resolver = &fakeGeodataResolver{set: f.set}
	f.downloader = &fakeGeodataDownloader{payloads: f.newFiles}
	f.probe = &fakeTransactionalProbe{newVersion: "1.2.3"}
	f.validator = &fakeXrayCandidateValidator{}
	f.runtime = &fakeXrayRuntime{}
	f.coordinator.events = new([]string)
	f.service = NewGeodataService(f.config())
	return f
}

func (f *geodataFixture) config() GeodataConfig {
	return GeodataConfig{
		Resolver:             f.resolver,
		Downloader:           f.downloader,
		Authority:            f.authority,
		Runtime:              f.runtime,
		CandidateValidator:   f.validator,
		CandidateProbe:       f.probe,
		AuthorityLease:       f.lease,
		Coordinator:          f.coordinator,
		ActiveBinaryPath:     f.activeBinary,
		ConfigDir:            f.configDir,
		AssetDir:             f.assetDir,
		PreviousDir:          f.previousDir,
		JournalPath:          f.journalPath,
		StagingDir:           f.stagingDir,
		RestoreJournalPath:   f.restorePath,
		AvailableSpace:       func(string) (uint64, error) { return ^uint64(0), nil },
		SyncDirectory:        func(string) error { return nil },
		AuthorityWaitTimeout: time.Second,
		PrepareTimeout:       10 * time.Second,
		ActivationTimeout:    10 * time.Second,
		RollbackTimeout:      10 * time.Second,
		TransactionTimeout:   30 * time.Second,
	}
}

func TestGeodataApplyCommitsOneCompleteGenerationAndPreservesManualFiles(t *testing.T) {
	f := newGeodataFixture(t)
	manualBefore := readFixtureFile(t, filepath.Join(f.assetDir, "manual-preserved.dat"))
	oldActive, err := f.service.readActiveSet()
	if err != nil {
		t.Fatalf("read old active set: %v", err)
	}
	if err := f.service.Apply(context.Background(), f.set); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for name, expected := range f.newFiles {
		if actual := readFixtureFile(t, filepath.Join(f.assetDir, name)); !bytes.Equal(actual, expected) {
			t.Fatalf("active %s changed incorrectly", name)
		}
	}
	if actual := readFixtureFile(t, filepath.Join(f.assetDir, "manual-preserved.dat")); !bytes.Equal(actual, manualBefore) {
		t.Fatal("unrelated manual geodata file changed")
	}
	assertGeodataPrefixedSentinel(t, f)
	previous, err := f.service.loadPreviousGeneration()
	if err != nil {
		t.Fatalf("load previous: %v", err)
	}
	if previous.meta.Generation != oldActive.Generation {
		t.Fatalf("previous generation metadata does not describe the full old set: got=%s want=%s meta=%+v", previous.meta.Generation, oldActive.Generation, previous.meta)
	}
	if present, err := componentTransactionPresent(f.journalPath); err != nil || present {
		t.Fatalf("journal remains after success: present=%v err=%v", present, err)
	}
	if f.resolver.calls != 1 || f.downloader.totalCalls() != len(productGeodataCatalog) {
		t.Fatalf("unexpected resolver/download counts: resolve=%d download=%d", f.resolver.calls, f.downloader.totalCalls())
	}
	if err := f.service.Ready(); err != nil {
		t.Fatalf("service not ready after success: %v", err)
	}
}

func TestGeodataApplyReResolvesExactSetBeforeDownload(t *testing.T) {
	f := newGeodataFixture(t)
	intended := cloneGeodataCandidateSet(f.set)
	intended.Items[0].Tag = "2026.09.02"
	intended.Generation = CandidateSetGeneration(intended.Items)
	err := f.service.Apply(context.Background(), intended)
	if !errors.Is(err, ErrGeodataCandidateStale) {
		t.Fatalf("expected stale candidate, got %v", err)
	}
	if f.resolver.calls != 1 || f.downloader.totalCalls() != 0 {
		t.Fatalf("stale candidate reached download: resolve=%d download=%d", f.resolver.calls, f.downloader.totalCalls())
	}
}

func TestGeodataApplyRejectsUnknownPolicyBeforeResolution(t *testing.T) {
	f := newGeodataFixture(t)
	value := f.authority.snapshot
	value.Appliance.DNS.Servers[0].Domains = []string{"ext:custom.dat:unknown"}
	f.authority.snapshot = value
	err := f.service.Apply(context.Background(), f.set)
	if !errors.Is(err, ErrGeodataPolicyUnsupported) {
		t.Fatalf("expected unsupported policy, got %v", err)
	}
	if f.resolver.calls != 0 || f.downloader.totalCalls() != 0 {
		t.Fatalf("unsupported policy reached upstream: resolve=%d download=%d", f.resolver.calls, f.downloader.totalCalls())
	}
}

func TestGeodataApplyRejectsIncompleteActiveSetBeforeResolution(t *testing.T) {
	f := newGeodataFixture(t)
	if err := os.Remove(filepath.Join(f.assetDir, productGeodataCatalog[0].Name)); err != nil {
		t.Fatal(err)
	}
	err := f.service.Apply(context.Background(), f.set)
	if !errors.Is(err, ErrGeodataAuthorityUnavailable) {
		t.Fatalf("incomplete active set error = %v", err)
	}
	if f.resolver.calls != 0 || f.downloader.totalCalls() != 0 {
		t.Fatalf("incomplete active set reached upstream: resolve=%d download=%d", f.resolver.calls, f.downloader.totalCalls())
	}
}

func TestGeodataApplyRejectsActiveGenerationDriftBeforeCommit(t *testing.T) {
	f := newGeodataFixture(t)
	f.resolver.after = func() {
		if err := os.WriteFile(filepath.Join(f.assetDir, productGeodataCatalog[0].Name), []byte("external-geodata-drift"), 0o600); err != nil {
			t.Fatalf("write synthetic drift: %v", err)
		}
	}
	err := f.service.Apply(context.Background(), f.set)
	if !errors.Is(err, ErrGeodataCandidateStale) {
		t.Fatalf("active generation drift error = %v", err)
	}
	if present, _ := componentTransactionPresent(f.journalPath); present {
		t.Fatal("active generation drift created a journal")
	}
}

func TestGeodataApplyRejectsActiveXrayDriftBeforeCandidateValidation(t *testing.T) {
	f := newGeodataFixture(t)
	f.resolver.after = func() {
		if err := os.WriteFile(f.activeBinary, []byte("external-xray-drift"), 0o700); err != nil {
			t.Fatalf("write synthetic Xray drift: %v", err)
		}
	}
	err := f.service.Apply(context.Background(), f.set)
	if !errors.Is(err, ErrGeodataCandidateStale) {
		t.Fatalf("active Xray drift error = %v", err)
	}
	if f.validator.calls != 0 {
		t.Fatalf("candidate validation ran after active Xray drift: %d", f.validator.calls)
	}
}

func TestGeodataFailuresAfterEachFileRenameRestoreTheWholeSet(t *testing.T) {
	for failAfter := 1; failAfter <= len(productGeodataCatalog); failAfter++ {
		t.Run(fmt.Sprintf("rename-%d", failAfter), func(t *testing.T) {
			f := newGeodataFixture(t)
			count := 0
			config := f.config()
			config.InjectFailure = func(stage GeodataStage) error {
				if stage != GeodataStageFileCommitted {
					return nil
				}
				count++
				if count == failAfter {
					return errors.New("synthetic rename fault")
				}
				return nil
			}
			f.service = NewGeodataService(config)
			err := f.service.Apply(context.Background(), f.set)
			if !errors.Is(err, ErrGeodataApplyFailed) {
				t.Fatalf("expected recovered apply failure, got %v", err)
			}
			for name, expected := range f.oldFiles {
				if actual := readFixtureFile(t, filepath.Join(f.assetDir, name)); !bytes.Equal(actual, expected) {
					t.Fatalf("active %s is mixed after recovery", name)
				}
			}
			if present, _ := componentTransactionPresent(f.journalPath); present {
				t.Fatal("journal remains after bounded recovery")
			}
			assertGeodataPrefixedSentinel(t, f)
			if err := f.service.Ready(); err != nil {
				t.Fatalf("service not ready after recovery: %v", err)
			}
		})
	}
}

func TestGeodataRollbackUsesPreviousWithoutResolverAndPreservesOneStepTarget(t *testing.T) {
	f := newGeodataFixture(t)
	if err := f.service.Apply(context.Background(), f.set); err != nil {
		t.Fatalf("apply: %v", err)
	}
	newActive, err := f.service.readActiveSet()
	if err != nil {
		t.Fatalf("read new active set: %v", err)
	}
	resolverCalls := f.resolver.calls
	downloadCalls := f.downloader.totalCalls()
	f.resolver.err = errors.New("resolver must not be used by rollback")
	if err := f.service.Rollback(context.Background()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	for name, expected := range f.oldFiles {
		if actual := readFixtureFile(t, filepath.Join(f.assetDir, name)); !bytes.Equal(actual, expected) {
			t.Fatalf("rollback did not restore %s", name)
		}
	}
	previous, err := f.service.loadPreviousGeneration()
	if err != nil {
		t.Fatalf("load rollback previous: %v", err)
	}
	if previous.meta.Generation != newActive.Generation {
		t.Fatalf("rollback did not preserve displaced current set as previous: got=%s want=%s meta=%+v", previous.meta.Generation, newActive.Generation, previous.meta)
	}
	if f.resolver.calls != resolverCalls || f.downloader.totalCalls() != downloadCalls {
		t.Fatalf("rollback contacted upstream: resolve=%d download=%d", f.resolver.calls, f.downloader.totalCalls())
	}
	assertGeodataPrefixedSentinel(t, f)
}

func TestGeodataRollbackExpectedRejectsRotatedPreviousBeforeActivation(t *testing.T) {
	f := newGeodataFixture(t)
	if err := f.service.Apply(context.Background(), f.set); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	expected, err := f.service.PreviousGeneration()
	if err != nil {
		t.Fatalf("first previous generation: %v", err)
	}
	if err := f.service.Apply(context.Background(), f.set); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	rotated, err := f.service.PreviousGeneration()
	if err != nil {
		t.Fatalf("rotated previous generation: %v", err)
	}
	if rotated.Generation == expected.Generation {
		t.Fatal("second apply did not rotate the retained previous generation")
	}
	activeBefore, err := f.service.readActiveSet()
	if err != nil {
		t.Fatalf("active set before stale rollback: %v", err)
	}
	restartsBefore := f.runtime.restartCalls
	if err := f.service.RollbackExpected(context.Background(), expected); !errors.Is(err, ErrGeodataCandidateStale) {
		t.Fatalf("rotated rollback error = %v", err)
	}
	activeAfter, err := f.service.readActiveSet()
	if err != nil {
		t.Fatalf("active set after stale rollback: %v", err)
	}
	if !sameGeodataSetMetadata(activeAfter, activeBefore) {
		t.Fatalf("stale rollback changed active set: before=%+v after=%+v", activeBefore, activeAfter)
	}
	if f.runtime.restartCalls != restartsBefore {
		t.Fatalf("stale rollback restarted runtime: before=%d after=%d", restartsBefore, f.runtime.restartCalls)
	}
	if present, _ := componentTransactionPresent(f.journalPath); present {
		t.Fatal("stale rollback left a transaction journal")
	}
	actual, err := f.service.PreviousGeneration()
	if err != nil {
		t.Fatalf("retained generation after stale rollback: %v", err)
	}
	if actual.Generation != rotated.Generation {
		t.Fatalf("stale rollback changed retained generation: got=%s want=%s", actual.Generation, rotated.Generation)
	}
	assertGeodataPrefixedSentinel(t, f)
}

func TestGeodataRuntimeMutationAfterRestartFailsClosedAndRestores(t *testing.T) {
	f := newGeodataFixture(t)
	f.runtime.restartMutation = func() {
		if err := os.WriteFile(filepath.Join(f.assetDir, productGeodataCatalog[0].Name), []byte("runtime-geodata-drift"), 0o600); err != nil {
			t.Fatalf("write synthetic runtime drift: %v", err)
		}
	}
	if err := f.service.Apply(context.Background(), f.set); !errors.Is(err, ErrGeodataApplyFailed) || !errors.Is(err, ErrGeodataApplyRestored) {
		t.Fatalf("runtime mutation result = %v", err)
	}
	for name, expected := range f.oldFiles {
		if actual := readFixtureFile(t, filepath.Join(f.assetDir, name)); !bytes.Equal(actual, expected) {
			t.Fatalf("active %s was not restored after runtime mutation", name)
		}
	}
	if present, _ := componentTransactionPresent(f.journalPath); present {
		t.Fatal("journal remains after runtime mutation was recovered")
	}
	assertGeodataPrefixedSentinel(t, f)
	if err := f.service.Ready(); err != nil {
		t.Fatalf("service not ready after runtime mutation recovery: %v", err)
	}
}

func TestGeodataPreCommitFailureDoesNotClaimVerifiedRestore(t *testing.T) {
	f := newGeodataFixture(t)
	f.service.config.InjectFailure = func(stage GeodataStage) error {
		if stage == GeodataStagePreviousSaved {
			return errors.New("synthetic pre-commit preparation failure")
		}
		return nil
	}
	err := f.service.Apply(context.Background(), f.set)
	if !errors.Is(err, ErrGeodataApplyFailed) || errors.Is(err, ErrGeodataApplyRestored) {
		t.Fatalf("pre-commit result = %v", err)
	}
	for name, expected := range f.oldFiles {
		if actual := readFixtureFile(t, filepath.Join(f.assetDir, name)); !bytes.Equal(actual, expected) {
			t.Fatalf("pre-commit failure changed active %s: %q", name, actual)
		}
	}
}

func TestGeodataStartupRecoveryPreservesRollbackTargetAfterOldSettled(t *testing.T) {
	f := newGeodataFixture(t)
	oldActive, err := f.service.readActiveSet()
	if err != nil {
		t.Fatalf("read old active set: %v", err)
	}
	if err := f.service.Apply(context.Background(), f.set); err != nil {
		t.Fatalf("apply: %v", err)
	}
	newActive, err := f.service.readActiveSet()
	if err != nil {
		t.Fatalf("read new active set: %v", err)
	}
	if _, err := f.service.savePreviousGeneration(newActive); err != nil {
		t.Fatalf("stage current generation: %v", err)
	}
	if err := f.service.promotePreviousGeneration(); err != nil {
		t.Fatalf("promote current generation: %v", err)
	}
	if err := f.service.settlePreviousGeneration(); err != nil {
		t.Fatalf("settle current generation: %v", err)
	}
	for name, contents := range f.oldFiles {
		if err := os.WriteFile(filepath.Join(f.assetDir, name), contents, 0o600); err != nil {
			t.Fatalf("restore synthetic rollback candidate %s: %v", name, err)
		}
	}
	if err := f.service.writeJournal(geodataTransactionJournal{
		SchemaVersion: GeodataTransactionSchemaVersion,
		Component:     string(KindGeodata),
		Operation:     geodataOperationRollback,
		Phase:         geodataPhaseFilesCommitted,
		Previous:      newActive,
		Candidate:     oldActive,
	}); err != nil {
		t.Fatalf("write synthetic crash journal: %v", err)
	}
	resolverCalls := f.resolver.calls
	downloadCalls := f.downloader.totalCalls()
	f.service = NewGeodataService(f.config())
	if err := f.service.RecoverStartup(context.Background()); err != nil {
		t.Fatalf("startup recovery: %v", err)
	}
	active, err := f.service.readActiveSet()
	if err != nil || !sameGeodataSetMetadata(active, newActive) {
		t.Fatalf("active generation after recovery = %+v err=%v, want %+v", active, err, newActive)
	}
	previous, err := f.service.loadPreviousGeneration()
	if err != nil || !sameGeodataSetMetadata(previous.meta, oldActive) {
		t.Fatalf("previous generation after recovery = %+v err=%v, want %+v", previous.meta, err, oldActive)
	}
	assertGeodataPrefixedSentinel(t, f)
	if f.resolver.calls != resolverCalls || f.downloader.totalCalls() != downloadCalls {
		t.Fatal("startup rollback recovery contacted upstream")
	}
}

func TestGeodataJournalClearFailureFailsClosedAndStartupRecoveryIsLocal(t *testing.T) {
	f := newGeodataFixture(t)
	allowJournalClear := false
	stateDir := filepath.Dir(f.journalPath)
	config := f.config()
	config.SyncDirectory = func(directory string) error {
		if filepath.Clean(directory) == filepath.Clean(stateDir) {
			if _, err := os.Lstat(f.journalPath); errors.Is(err, os.ErrNotExist) && !allowJournalClear {
				return errors.New("synthetic journal clear durability fault")
			}
		}
		return nil
	}
	f.service = NewGeodataService(config)
	err := f.service.Apply(context.Background(), f.set)
	if !errors.Is(err, ErrGeodataRecoveryFailed) {
		t.Fatalf("expected fail-closed result, got %v", err)
	}
	if err := f.service.Ready(); !errors.Is(err, ErrGeodataRecoveryFailed) {
		t.Fatalf("service did not retain maintenance: %v", err)
	}
	if present, _ := componentTransactionPresent(f.journalPath); !present {
		t.Fatal("journal was lost after clear durability failure")
	}
	allowJournalClear = true
	resolverCalls := f.resolver.calls
	downloadCalls := f.downloader.totalCalls()
	if err := f.service.RecoverStartup(context.Background()); err != nil {
		t.Fatalf("local startup recovery: %v", err)
	}
	if f.resolver.calls != resolverCalls || f.downloader.totalCalls() != downloadCalls {
		t.Fatal("startup recovery contacted upstream")
	}
	if err := f.service.Ready(); err != nil {
		t.Fatalf("service not ready after startup recovery: %v", err)
	}
	assertGeodataPrefixedSentinel(t, f)
}

func TestGeodataLegacyDatActivationNameRemainsUnrelated(t *testing.T) {
	for _, test := range []struct {
		name      string
		directory bool
	}{
		{name: "regular-file"},
		{name: "directory", directory: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newGeodataFixture(t)
			path := filepath.Join(f.assetDir, geodataActivationTempDirName)
			collision := createGeodataActivationCollision(t, path, test.directory)
			if err := f.service.Apply(context.Background(), f.set); err != nil {
				t.Fatalf("apply with legacy dat collision: %v", err)
			}
			assertGeodataActivationCollision(t, path, test.directory, collision)
			if err := f.service.Rollback(context.Background()); err != nil {
				t.Fatalf("rollback with legacy dat collision: %v", err)
			}
			assertGeodataActivationCollision(t, path, test.directory, collision)
			assertGeodataActivationTempAbsent(t, f)
		})
	}
}

func TestGeodataPrivateActivationCollisionsFailClosedWithoutDeletingForeignPath(t *testing.T) {
	for _, test := range []struct {
		name      string
		directory bool
	}{
		{name: "regular-file"},
		{name: "directory", directory: true},
	} {
		for _, operation := range []string{"apply", "rollback"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				f := newGeodataFixture(t)
				var activeBefore geodataSetMetadata
				if operation == "rollback" {
					if err := f.service.Apply(context.Background(), f.set); err != nil {
						t.Fatalf("seed apply: %v", err)
					}
					var err error
					activeBefore, err = f.service.readActiveSet()
					if err != nil {
						t.Fatalf("read active before collision: %v", err)
					}
				}
				path := f.service.activationTempDir()
				collision := createGeodataActivationCollision(t, path, test.directory)
				var err error
				if operation == "apply" {
					err = f.service.Apply(context.Background(), f.set)
				} else {
					err = f.service.Rollback(context.Background())
				}
				if !errors.Is(err, ErrGeodataRecoveryFailed) {
					t.Fatalf("%s with private activation collision: %v", operation, err)
				}
				assertGeodataActivationCollision(t, path, test.directory, collision)
				if operation == "rollback" {
					activeAfter, readErr := f.service.readActiveSet()
					if readErr != nil || !sameGeodataSetMetadata(activeAfter, activeBefore) {
						t.Fatalf("active generation changed during collision failure: %+v err=%v", activeAfter, readErr)
					}
				}
				if recoveryErr := f.service.RecoverStartup(context.Background()); !errors.Is(recoveryErr, ErrGeodataRecoveryFailed) {
					t.Fatalf("startup recovery with private activation collision: %v", recoveryErr)
				}
				assertGeodataActivationCollision(t, path, test.directory, collision)
				if readyErr := f.service.Ready(); !errors.Is(readyErr, ErrGeodataRecoveryFailed) {
					t.Fatalf("service readiness after collision: %v", readyErr)
				}
			})
		}
	}
}

func TestComponentRecoveryArbitratesOwnerAndConflicts(t *testing.T) {
	f := newGeodataFixture(t)
	xrayPrevious := filepath.Join(f.root, "control", "previous", "components", "xray.staging")
	xrayStaging := filepath.Join(f.root, "tmp", "xkeen-control", "components", "xray")
	config := ComponentRecoveryConfig{
		JournalPath:                f.journalPath,
		RestoreJournalPath:         f.restorePath,
		XrayPreviousStagingPath:    xrayPrevious,
		XrayStagingDir:             xrayStaging,
		GeodataPreviousStagingPath: f.previousDir + ".staging",
		GeodataStagingDir:          f.stagingDir,
	}
	state, err := InspectComponentRecovery(config)
	if err != nil || state.Pending() {
		t.Fatalf("unexpected clean recovery state: %+v err=%v", state, err)
	}
	if err := os.MkdirAll(filepath.Join(f.stagingDir, "candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	state, err = InspectComponentRecovery(config)
	if err != nil || state.Kind != KindGeodata || !state.GeodataStagingPresent {
		t.Fatalf("geodata staging was not selected: %+v err=%v", state, err)
	}
	if err := os.MkdirAll(filepath.Join(xrayStaging, "candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectComponentRecovery(config); !errors.Is(err, ErrComponentRecoveryConflict) {
		t.Fatalf("expected cross-component staging conflict, got %v", err)
	}
}

func assertGeodataPrefixedSentinel(t *testing.T, f *geodataFixture) {
	t.Helper()
	if actual := readFixtureFile(t, f.prefixedSentinel); !bytes.Equal(actual, []byte("prefix-sentinel")) {
		t.Fatalf("unrelated activation-prefix sentinel changed: %q", actual)
	}
}

func createGeodataActivationCollision(t *testing.T, path string, directory bool) []byte {
	t.Helper()
	if directory {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("create activation collision directory: %v", err)
		}
		contents := []byte("foreign-directory-bytes")
		if err := os.WriteFile(filepath.Join(path, "manual.dat"), contents, 0o640); err != nil {
			t.Fatalf("write activation collision directory sentinel: %v", err)
		}
		return contents
	}
	contents := []byte("foreign-regular-file-bytes")
	if err := os.WriteFile(path, contents, 0o640); err != nil {
		t.Fatalf("create activation collision regular file: %v", err)
	}
	return contents
}

func assertGeodataActivationCollision(t *testing.T, path string, directory bool, expected []byte) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("activation collision path missing: %v", err)
	}
	if directory {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("activation collision directory changed type: %v", info.Mode())
		}
		if actual := readFixtureFile(t, filepath.Join(path, "manual.dat")); !bytes.Equal(actual, expected) {
			t.Fatalf("activation collision directory contents changed: %q", actual)
		}
		return
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("activation collision regular file changed type: %v", info.Mode())
	}
	if actual := readFixtureFile(t, path); !bytes.Equal(actual, expected) {
		t.Fatalf("activation collision regular file contents changed: %q", actual)
	}
}

func assertGeodataActivationTempAbsent(t *testing.T, f *geodataFixture) {
	t.Helper()
	if _, err := os.Lstat(f.service.activationTempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("product-owned activation temp state remains: %v", err)
	}
}

func TestComponentMaintenanceIsReferenceCountedByOwner(t *testing.T) {
	f := newGeodataFixture(t)
	maintenance := NewComponentMaintenance(f.coordinator, f.lease)
	maintenance.Enter(KindXray)
	maintenance.Enter(KindGeodata)
	maintenance.Exit(KindXray)
	if !maintenance.HasOwner(KindGeodata) || !f.coordinator.maintenance {
		t.Fatal("one component released another component's maintenance")
	}
	maintenance.Exit(KindGeodata)
	if f.coordinator.maintenance {
		t.Fatal("maintenance remained after final owner exited")
	}
}

func TestGeodataResolverFetchesEachDistinctFixedSourceOnce(t *testing.T) {
	contents := make(map[string][]byte)
	for _, entry := range productGeodataCatalog {
		payload := []byte("candidate-" + entry.ID)
		digest := sha256.Sum256(payload)
		var assets []githubAssetMetadata
		if entry.Repository == "1andrevich/Re-filter-lists" {
			assets = []githubAssetMetadata{
				{Name: "geosite.dat", Size: int64(len(payload)), State: "uploaded", Digest: fmt.Sprintf("sha256:%x", digest[:])},
				{Name: "geoip.dat", Size: int64(len(payload)), State: "uploaded", Digest: fmt.Sprintf("sha256:%x", digest[:])},
			}
		} else {
			assets = []githubAssetMetadata{{Name: entry.Asset, Size: int64(len(payload)), State: "uploaded", Digest: fmt.Sprintf("sha256:%x", digest[:])}}
		}
		body, err := json.Marshal(githubReleaseMetadata{Draft: boolPtr(false), Prerelease: boolPtr(false), TagName: "2026.09.03", Assets: assets})
		if err != nil {
			t.Fatal(err)
		}
		contents[geodataMetadataPath(entry)] = body
	}
	transport := &metadataFixtureTransport{responses: contents}
	client := &http.Client{Transport: transport}
	resolver := NewGeodataResolver(nil, client)
	set, err := resolver.ResolveGeodata(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(set.Items) != len(productGeodataCatalog) {
		t.Fatalf("unexpected item count: %d", len(set.Items))
	}
	calls := transportCalls(transport)
	if len(calls) != 5 {
		t.Fatalf("expected five distinct source requests, got %d (%v)", len(calls), calls)
	}
	counts := make(map[string]int, len(calls))
	for _, source := range calls {
		counts[source]++
	}
	for source, count := range counts {
		if count != 1 {
			t.Fatalf("source %s fetched %d times", source, count)
		}
	}
}

func TestGeodataResolverRejectsInvalidReleaseMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]githubReleaseMetadata)
	}{
		{name: "draft", mutate: func(releases map[string]githubReleaseMetadata) {
			release := releases[geodataMetadataPath(productGeodataCatalog[0])]
			release.Draft = boolPtr(true)
			releases[geodataMetadataPath(productGeodataCatalog[0])] = release
		}},
		{name: "prerelease", mutate: func(releases map[string]githubReleaseMetadata) {
			release := releases[geodataMetadataPath(productGeodataCatalog[0])]
			release.Prerelease = boolPtr(true)
			releases[geodataMetadataPath(productGeodataCatalog[0])] = release
		}},
		{name: "missing asset", mutate: func(releases map[string]githubReleaseMetadata) {
			release := releases[geodataMetadataPath(productGeodataCatalog[0])]
			release.Assets = nil
			releases[geodataMetadataPath(productGeodataCatalog[0])] = release
		}},
		{name: "duplicate asset", mutate: func(releases map[string]githubReleaseMetadata) {
			path := geodataMetadataPath(productGeodataCatalog[1])
			release := releases[path]
			release.Assets = append(release.Assets, release.Assets[0])
			releases[path] = release
		}},
		{name: "missing digest", mutate: func(releases map[string]githubReleaseMetadata) {
			path := geodataMetadataPath(productGeodataCatalog[1])
			release := releases[path]
			release.Assets[0].Digest = ""
			releases[path] = release
		}},
		{name: "invalid generation", mutate: func(releases map[string]githubReleaseMetadata) {
			path := geodataMetadataPath(productGeodataCatalog[1])
			release := releases[path]
			release.TagName = "latest release"
			releases[path] = release
		}},
		{name: "zero size", mutate: func(releases map[string]githubReleaseMetadata) {
			path := geodataMetadataPath(productGeodataCatalog[1])
			release := releases[path]
			release.Assets[0].Size = 0
			releases[path] = release
		}},
		{name: "oversize", mutate: func(releases map[string]githubReleaseMetadata) {
			path := geodataMetadataPath(productGeodataCatalog[1])
			release := releases[path]
			release.Assets[0].Size = MaxGeodataFileBytes + 1
			releases[path] = release
		}},
		{name: "aggregate overflow", mutate: func(releases map[string]githubReleaseMetadata) {
			size := MaxGeodataCandidateBytes/int64(len(productGeodataCatalog)) + 1
			for path, release := range releases {
				for index := range release.Assets {
					release.Assets[index].Size = size
				}
				releases[path] = release
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			releases := geodataReleaseMetadataFixtures(t)
			test.mutate(releases)
			resolver := NewGeodataResolver(nil, &http.Client{Transport: &metadataFixtureTransport{responses: encodeGeodataReleaseMetadata(t, releases)}})
			if _, err := resolver.ResolveGeodata(context.Background()); !errors.Is(err, ErrGeodataCandidateRejected) {
				t.Fatalf("resolver error = %v", err)
			}
		})
	}
}

func TestGeodataPrepareStagesExactlyTheOwnedSet(t *testing.T) {
	f := newGeodataFixture(t)
	prepared, err := f.service.prepare(context.Background(), f.set)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer f.service.removeOwned(prepared.stageDir)
	entries, err := os.ReadDir(prepared.candidatePath)
	if err != nil {
		t.Fatalf("read candidate directory: %v", err)
	}
	if len(entries) != len(productGeodataCatalog) {
		t.Fatalf("candidate entry count = %d", len(entries))
	}
	if len(f.validator.assetDirs) == 0 || filepath.Clean(f.validator.assetDirs[0]) != filepath.Clean(prepared.candidatePath) {
		t.Fatalf("candidate validator asset directory = %v, want %s", f.validator.assetDirs, prepared.candidatePath)
	}
	wantNames := make(map[string]struct{}, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		wantNames[entry.Name] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := wantNames[entry.Name()]; !ok || entry.IsDir() {
			t.Fatalf("unexpected candidate entry = %+v", entry)
		}
	}
}

func TestGeodataApplyFailsClosedOnPreJournalStagingFault(t *testing.T) {
	f := newGeodataFixture(t)
	maintenance := NewComponentMaintenance(f.coordinator, f.lease)
	config := f.config()
	config.Maintenance = maintenance
	config.InjectFailure = func(stage GeodataStage) error {
		if stage == GeodataStagePreviousStaging {
			return errors.New("synthetic pre-journal process loss")
		}
		return nil
	}
	f.service = NewGeodataService(config)
	if err := f.service.Apply(context.Background(), f.set); !errors.Is(err, ErrGeodataApplyFailed) {
		t.Fatalf("pre-journal fault result = %v", err)
	}
	if err := f.service.Ready(); !errors.Is(err, ErrGeodataRecoveryRequired) {
		t.Fatalf("pre-journal readiness = %v", err)
	}
	if !maintenance.HasOwner(KindGeodata) || !f.coordinator.maintenance {
		t.Fatal("pre-journal fault did not retain shared maintenance")
	}
	config.InjectFailure = nil
	f.service.config.InjectFailure = nil
	if err := f.service.RecoverStartup(context.Background()); err != nil {
		t.Fatalf("pre-journal recovery: %v", err)
	}
	if err := f.service.Ready(); err != nil || maintenance.HasOwner(KindGeodata) || f.coordinator.maintenance {
		t.Fatalf("pre-journal recovery readiness=%v maintenance=%v", err, f.coordinator.maintenance)
	}
}

func TestGeodataJournalWriteFailureClearsUncommittedIntentSafely(t *testing.T) {
	f := newGeodataFixture(t)
	stateDir := filepath.Dir(f.journalPath)
	failed := false
	config := f.config()
	config.SyncDirectory = func(directory string) error {
		if filepath.Clean(directory) == filepath.Clean(stateDir) && !failed {
			failed = true
			return errors.New("synthetic journal intent sync fault")
		}
		return nil
	}
	f.service = NewGeodataService(config)
	if err := f.service.Apply(context.Background(), f.set); !errors.Is(err, ErrGeodataApplyFailed) {
		t.Fatalf("journal write fault result = %v", err)
	}
	if present, _ := componentTransactionPresent(f.journalPath); present {
		t.Fatal("uncommitted journal remained after durable clear")
	}
	if _, err := os.Lstat(f.service.stagingPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncommitted previous staging remained: %v", err)
	}
	if err := f.service.Ready(); err != nil {
		t.Fatalf("journal write failure left service unavailable: %v", err)
	}
}

func TestGeodataArtifactDownloadIsFixedAndDoesNotRetry(t *testing.T) {
	f := newGeodataFixture(t)
	identity := f.set.Items[0]
	payload := f.newFiles[identity.ActiveName]
	for _, test := range []struct {
		name       string
		body       []byte
		contentLen int64
		status     int
		wantErr    bool
	}{
		{name: "exact", body: payload, contentLen: int64(len(payload))},
		{name: "short", body: payload[:len(payload)-1], contentLen: -1, wantErr: true},
		{name: "extra", body: append(append([]byte(nil), payload...), 'x'), contentLen: -1, wantErr: true},
		{name: "status", body: payload, contentLen: int64(len(payload)), status: http.StatusBadGateway, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := NewGeodataArtifactDownloader(nil, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				status := test.status
				if status == 0 {
					status = http.StatusOK
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(test.body)), ContentLength: test.contentLen, Header: make(http.Header), Request: request}, nil
			})})
			var destination bytes.Buffer
			err := client.DownloadGeodata(context.Background(), identity, &destination)
			if test.wantErr != (err != nil) {
				t.Fatalf("download error = %v", err)
			}
			if calls != 1 {
				t.Fatalf("download transport calls = %d", calls)
			}
			if !test.wantErr && !bytes.Equal(destination.Bytes(), payload) {
				t.Fatalf("download payload = %q", destination.Bytes())
			}
		})
	}
}

func TestGeodataArtifactRedirectPolicyIsBoundedAndHostRestricted(t *testing.T) {
	initial := &http.Request{URL: mustURL(t, "https://github.com/jameszeroX/zkeen-ip/releases/download/2026.09.03/zkeenip.dat")}
	allowed := &http.Request{URL: mustURL(t, "https://release-assets.githubusercontent.com/assets/1")}
	if err := geodataArtifactRedirectPolicy(allowed, []*http.Request{initial}); err != nil {
		t.Fatalf("allowed asset redirect rejected: %v", err)
	}
	for _, raw := range []string{"https://example.invalid/asset", "http://release-assets.githubusercontent.com/asset", "https://github.com/other/path"} {
		if err := geodataArtifactRedirectPolicy(&http.Request{URL: mustURL(t, raw)}, []*http.Request{initial}); err == nil {
			t.Fatalf("unsafe redirect accepted: %s", raw)
		}
	}
	via := make([]*http.Request, 4)
	for index := range via {
		via[index] = initial
	}
	if err := geodataArtifactRedirectPolicy(allowed, via); err == nil {
		t.Fatal("redirect chain beyond bound accepted")
	}
}

type fakeGeodataResolver struct {
	mu    sync.Mutex
	set   GeodataCandidateSet
	err   error
	calls int
	after func()
}

func (r *fakeGeodataResolver) ResolveGeodata(context.Context) (GeodataCandidateSet, error) {
	r.mu.Lock()
	r.calls++
	set := cloneGeodataCandidateSet(r.set)
	err := r.err
	after := r.after
	r.mu.Unlock()
	if after != nil {
		after()
	}
	if err != nil {
		return GeodataCandidateSet{}, err
	}
	return set, nil
}

type fakeGeodataDownloader struct {
	mu       sync.Mutex
	payloads map[string][]byte
	calls    int
}

func (d *fakeGeodataDownloader) DownloadGeodata(_ context.Context, identity GeodataReleaseIdentity, destination io.Writer) error {
	d.mu.Lock()
	d.calls++
	payload := append([]byte(nil), d.payloads[identity.ActiveName]...)
	d.mu.Unlock()
	if len(payload) == 0 {
		return errors.New("missing synthetic payload")
	}
	_, err := destination.Write(payload)
	return err
}

func (d *fakeGeodataDownloader) totalCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func geodataReleaseMetadataFixtures(t *testing.T) map[string]githubReleaseMetadata {
	t.Helper()
	result := make(map[string]githubReleaseMetadata, 5)
	for _, entry := range productGeodataCatalog {
		path := geodataMetadataPath(entry)
		release, ok := result[path]
		if !ok {
			release = githubReleaseMetadata{Draft: boolPtr(false), Prerelease: boolPtr(false), TagName: "2026.09.03"}
		}
		payload := []byte("resolved-" + entry.ID)
		digest := sha256.Sum256(payload)
		release.Assets = append(release.Assets, githubAssetMetadata{
			Name: entry.Asset, Size: int64(len(payload)), State: "uploaded", Digest: fmt.Sprintf("sha256:%x", digest[:]),
		})
		result[path] = release
	}
	return result
}

func encodeGeodataReleaseMetadata(t *testing.T, releases map[string]githubReleaseMetadata) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(releases))
	for path, release := range releases {
		body, err := json.Marshal(release)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = body
	}
	return result
}

func boolPtr(value bool) *bool { return &value }

func cloneGeodataCandidateSet(value GeodataCandidateSet) GeodataCandidateSet {
	value.Items = append([]GeodataReleaseIdentity(nil), value.Items...)
	return value
}
