package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
)

const validXrayVersionOutput = "Xray 1.8.24 (Xray, Penetrates Everything.)\ngo1.24.4 linux/arm64\n"

type fakeXrayProbe struct {
	result XrayVersionResult
	calls  atomic.Int32
	block  bool
}

func (probe *fakeXrayProbe) ProbeXrayVersion(ctx context.Context) XrayVersionResult {
	probe.calls.Add(1)
	if probe.block {
		<-ctx.Done()
		return XrayVersionResult{ExitCode: -1, Err: ctx.Err()}
	}
	return probe.result
}

type inventoryFixture struct {
	root   string
	config Config
	probe  *fakeXrayProbe
}

func newInventoryFixture(t *testing.T) *inventoryFixture {
	t.Helper()
	root := t.TempDir()
	probe := &fakeXrayProbe{result: XrayVersionResult{Stdout: []byte(validXrayVersionOutput)}}
	return &inventoryFixture{
		root:  root,
		probe: probe,
		config: Config{
			Panel: buildinfo.Info{
				Product:      "xkeen-control",
				Version:      "1.2.3",
				SourceCommit: strings.Repeat("a", 40),
				Channel:      "stable",
			},
			XrayBinary:             filepath.Join(root, "opt", "sbin", "xray"),
			XrayVersionProbe:       probe,
			XrayProbeTimeout:       100 * time.Millisecond,
			XkeenBinary:            filepath.Join(root, "opt", "sbin", "xkeen"),
			XkeenModuleDir:         filepath.Join(root, "opt", "sbin", ".xkeen"),
			XkeenModuleImport:      filepath.Join(root, "opt", "sbin", ".xkeen", "import.sh"),
			XkeenRuntimeInit:       filepath.Join(root, "opt", "etc", "init.d", "S05xkeen"),
			XkeenLegacyRuntimeInit: filepath.Join(root, "opt", "etc", "init.d", "S24xray"),
			XkeenConfig:            filepath.Join(root, "opt", "etc", "xkeen", "xkeen.json"),
			XkeenPackageMetadata:   filepath.Join(root, "opt", "lib", "opkg", "info", "xkeen.control"),
			GeodataDir:             filepath.Join(root, "opt", "etc", "xray", "dat"),
			AppliancePath:          filepath.Join(root, "opt", "etc", "xkeen-control", "config", "appliance.json"),
			RoutingPath:            filepath.Join(root, "opt", "etc", "xray", "configs", "05_routing.json"),
			DNSPath:                filepath.Join(root, "opt", "etc", "xray", "configs", "02_dns.json"),
			KeeneticOSVersionPath:  filepath.Join(root, "etc", "version"),
			EntwareReleasePath:     filepath.Join(root, "opt", "etc", "entware_release"),
			EntwareBinary:          filepath.Join(root, "opt", "bin", "opkg"),
			InventoryTimeout:       500 * time.Millisecond,
		},
	}
}

func (fixture *inventoryFixture) service() *Service {
	return NewService(fixture.config)
}

func writeFixtureFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureExecutable(t *testing.T, path string, contents string) {
	t.Helper()
	writeFixtureFile(t, path, []byte(contents), 0o755)
}

func (fixture *inventoryFixture) createXray(t *testing.T) {
	t.Helper()
	writeFixtureExecutable(t, fixture.config.XrayBinary, "synthetic xray fixture\n")
}

func (fixture *inventoryFixture) createXkeenLayout(t *testing.T) {
	t.Helper()
	writeFixtureExecutable(t, fixture.config.XkeenBinary, "#!/bin/sh\nprintf marker > "+filepath.Join(fixture.root, "xkeen-executed")+"\n")
	if err := os.MkdirAll(fixture.config.XkeenModuleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, fixture.config.XkeenModuleImport, []byte("#!/bin/sh\nprintf marker > "+filepath.Join(fixture.root, "import-executed")+"\n"), 0o644)
	writeFixtureExecutable(t, fixture.config.XkeenRuntimeInit, "#!/bin/sh\nprintf marker > "+filepath.Join(fixture.root, "init-executed")+"\n")
	writeFixtureFile(t, fixture.config.XkeenConfig, []byte(`{"enabled":true}`), 0o644)
}

func (fixture *inventoryFixture) createGeodata(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(fixture.config.GeodataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, entry := range productGeodataCatalog {
		writeFixtureFile(t, filepath.Join(fixture.config.GeodataDir, entry.Name), []byte(entry.ID+"\n"), 0o644)
	}
}

func (fixture *inventoryFixture) createPlatformSignals(t *testing.T) {
	t.Helper()
	writeFixtureFile(t, fixture.config.KeeneticOSVersionPath, []byte("4.3.6.1\n"), 0o644)
	writeFixtureFile(t, fixture.config.EntwareReleasePath, []byte("1.0.0\n"), 0o644)
	writeFixtureFile(t, fixture.config.EntwareBinary, []byte("synthetic opkg fixture\n"), 0o755)
}

func TestSnapshotReportsAllFixedClassesAndSafeFacts(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createXray(t)
	fixture.createXkeenLayout(t)
	writeFixtureFile(t, fixture.config.XkeenPackageMetadata, []byte("Package: xkeen\nVersion: 0.8.2\nArchitecture: all\n"), 0o644)
	fixture.createGeodata(t)
	fixture.createPlatformSignals(t)

	beforeEntries := fixtureEntries(t, fixture.root)
	snapshot := fixture.service().Snapshot(context.Background())
	afterEntries := fixtureEntries(t, fixture.root)
	if !reflect.DeepEqual(beforeEntries, afterEntries) {
		t.Fatalf("snapshot changed fixture files: before=%v after=%v", beforeEntries, afterEntries)
	}

	if snapshot.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", snapshot.SchemaVersion)
	}
	if snapshot.Panel.State != StatePresent || snapshot.Panel.Version != "1.2.3" || snapshot.Panel.SourceCommit != strings.Repeat("a", 40) || snapshot.Panel.Channel != "stable" {
		t.Fatalf("panel = %+v", snapshot.Panel)
	}
	if snapshot.Xray.State != StatePresent || snapshot.Xray.Version != "1.8.24" || snapshot.Xray.Architecture != "arm64" || snapshot.Xray.Capability != CapabilitySupported {
		t.Fatalf("xray = %+v", snapshot.Xray)
	}
	if snapshot.XKeen.State != StatePresent || snapshot.XKeen.Version != "0.8.2" || snapshot.XKeen.Channel != "dev" || snapshot.XKeen.Capability != CapabilitySupported {
		t.Fatalf("xkeen = %+v", snapshot.XKeen)
	}
	if snapshot.Geodata.State != StatePresent || snapshot.Geodata.Capability != CapabilitySupported || len(snapshot.Geodata.Items) != len(productGeodataCatalog) {
		t.Fatalf("geodata = %+v", snapshot.Geodata)
	}
	catalogByID := make(map[string]catalogEntry, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		catalogByID[entry.ID] = entry
	}
	for _, item := range snapshot.Geodata.Items {
		entry, ok := catalogByID[item.ID]
		if !ok {
			t.Fatalf("unexpected geodata item = %+v", item)
		}
		if item.ID != entry.ID || item.Name != entry.Name || item.Source != "product-catalog" || item.State != StatePresent || !item.Present || item.SHA256 == "" || item.MTime == "" {
			t.Fatalf("geodata item %s = %+v", item.ID, item)
		}
		expected := sha256.Sum256([]byte(entry.ID + "\n"))
		if item.SHA256 != hex.EncodeToString(expected[:]) {
			t.Fatalf("geodata hash %s = %s", item.ID, item.SHA256)
		}
	}
	if snapshot.KeeneticOS.State != StatePresent || snapshot.KeeneticOS.Version != "4.3.6.1" || snapshot.KeeneticOS.Capability != CapabilityInformational {
		t.Fatalf("keeneticos = %+v", snapshot.KeeneticOS)
	}
	if snapshot.Entware.State != StatePresent || snapshot.Entware.Version != "1.0.0" || snapshot.Entware.Capability != CapabilityInformational {
		t.Fatalf("entware = %+v", snapshot.Entware)
	}
	if fixture.probe.calls.Load() != 1 {
		t.Fatalf("xray probe calls = %d", fixture.probe.calls.Load())
	}

	contents, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	body := string(contents)
	for _, forbidden := range []string{fixture.root, "synthetic xray fixture", "xkeen-executed", "import-executed", "init-executed"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("safe projection contains %q", forbidden)
		}
	}
}

func TestSnapshotDistinguishesMissingAndUnknownSignals(t *testing.T) {
	fixture := newInventoryFixture(t)
	snapshot := fixture.service().Snapshot(context.Background())

	for _, component := range []Component{snapshot.XKeen, snapshot.Xray, snapshot.KeeneticOS, snapshot.Entware} {
		if component.State != StateMissing && component.Kind != KindKeeneticOS {
			t.Fatalf("missing component %s = %+v", component.Kind, component)
		}
		if !component.VersionUnknown {
			t.Fatalf("missing component %s reported a version", component.Kind)
		}
	}
	if snapshot.KeeneticOS.State != StateUnknown || snapshot.KeeneticOS.ReasonCode != "version-unavailable" {
		t.Fatalf("missing OS signal = %+v", snapshot.KeeneticOS)
	}
	if snapshot.Geodata.State != StateMissing || snapshot.Geodata.ReasonCode != "required-files-missing" || len(snapshot.Geodata.Items) != len(productGeodataCatalog) {
		t.Fatalf("missing geodata = %+v", snapshot.Geodata)
	}
	for _, item := range snapshot.Geodata.Items {
		if item.State != StateMissing || item.Present {
			t.Fatalf("missing geodata item = %+v", item)
		}
	}
	if fixture.probe.calls.Load() != 0 {
		t.Fatalf("xray probe ran for missing binary: %d", fixture.probe.calls.Load())
	}
}

func TestXrayVersionParsingIsStrictAndCapabilityIsArchitectureBound(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantReason string
		wantArch   string
		wantCap    Capability
	}{
		{name: "valid arm64", output: validXrayVersionOutput, wantArch: "arm64", wantCap: CapabilitySupported},
		{name: "malformed", output: "Xray 1.8.24 (Xray, Penetrates Everything.)\n", wantReason: "version-unparseable", wantCap: CapabilityUnsupported},
		{name: "unsupported architecture", output: "Xray 1.8.24 (Xray, Penetrates Everything.)\ngo1.24.4 linux/amd64\n", wantArch: "amd64", wantReason: "architecture-unsupported", wantCap: CapabilityUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInventoryFixture(t)
			fixture.createXray(t)
			fixture.probe.result = XrayVersionResult{Stdout: []byte(test.output)}
			component := fixture.service().Snapshot(context.Background()).Xray
			if component.ReasonCode != test.wantReason || component.Architecture != test.wantArch || component.Capability != test.wantCap {
				t.Fatalf("xray = %+v", component)
			}
			if test.wantReason == "" && component.VersionUnknown {
				t.Fatalf("valid xray version remained unknown: %+v", component)
			}
		})
	}

	if _, err := ParseXrayVersionOutput([]byte("prefix Xray 1.8.24 (Xray, Penetrates Everything.)\ngo1.24.4 linux/arm64\n"), nil); err == nil {
		t.Fatal("parser accepted an unanchored version line")
	}
}

func TestXrayProbeFailureTimeoutAndOutputLimitAreSafe(t *testing.T) {
	tests := []struct {
		name       string
		result     XrayVersionResult
		wantReason string
	}{
		{name: "nonzero", result: XrayVersionResult{ExitCode: 2, Err: errors.New("synthetic failure")}, wantReason: "version-probe-failed"},
		{name: "oversize", result: XrayVersionResult{Stdout: bytes.Repeat([]byte("x"), MaxXrayProbeOutput+1)}, wantReason: "version-output-too-large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInventoryFixture(t)
			fixture.createXray(t)
			fixture.probe.result = test.result
			component := fixture.service().Snapshot(context.Background()).Xray
			if component.ReasonCode != test.wantReason || component.Version != "" || !component.VersionUnknown {
				t.Fatalf("xray = %+v", component)
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		fixture := newInventoryFixture(t)
		fixture.createXray(t)
		fixture.probe.block = true
		fixture.config.XrayProbeTimeout = 20 * time.Millisecond
		component := fixture.service().Snapshot(context.Background()).Xray
		if component.ReasonCode != "version-probe-timeout" || fixture.probe.calls.Load() != 1 {
			t.Fatalf("xray timeout = %+v calls=%d", component, fixture.probe.calls.Load())
		}
	})
}

func TestCommandXrayProbeUsesOnlyVersionAndBoundsAggregateOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("synthetic POSIX executable fixture")
	}
	fixture := newInventoryFixture(t)
	probePath := filepath.Join(fixture.root, "probe-xray")
	writeFixtureExecutable(t, probePath, "#!/bin/sh\nset -eu\n[ \"${1:-}\" = version ]\n/usr/bin/dd if=/dev/zero bs=65537 count=1 2>/dev/null\n")
	result := (commandXrayVersionProbe{binary: probePath}).ProbeXrayVersion(context.Background())
	if !errors.Is(result.Err, errXrayOutputTooLarge) {
		t.Fatalf("oversize command result error = %v", result.Err)
	}
	if len(result.Stdout)+len(result.Stderr) > MaxXrayProbeOutput {
		t.Fatalf("oversize command captured %d bytes", len(result.Stdout)+len(result.Stderr))
	}
}

func TestXKeenNeverExecutesFixedScriptsOrGuessesVersion(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createXkeenLayout(t)
	component := fixture.service().Snapshot(context.Background()).XKeen
	if component.State != StatePresent || !component.Present || !component.VersionUnknown || component.Capability != CapabilityUnsupported || component.ReasonCode != "version-unavailable" {
		t.Fatalf("xkeen = %+v", component)
	}
	for _, marker := range []string{"xkeen-executed", "import-executed", "init-executed"} {
		if _, err := os.Stat(filepath.Join(fixture.root, marker)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("xkeen marker %s exists or could not be checked: %v", marker, err)
		}
	}
}

func TestXKeenRecognizesLegacyS24xrayWithoutTreatingItAsSupportedDevLayout(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createXkeenLayout(t)
	if err := os.Remove(fixture.config.XkeenRuntimeInit); err != nil {
		t.Fatal(err)
	}
	writeFixtureExecutable(t, fixture.config.XkeenLegacyRuntimeInit, "#!/bin/sh\nprintf legacy-init-executed > "+filepath.Join(fixture.root, "legacy-init-executed")+"\n")
	writeFixtureFile(t, fixture.config.XkeenPackageMetadata, []byte("Package: xkeen\nVersion: 2.0.1\nArchitecture: all\n"), 0o644)

	component := fixture.service().Snapshot(context.Background()).XKeen
	if component.State != StatePresent || !component.Present || component.ReasonCode != "legacy-layout" || component.Capability != CapabilityUnsupported || !component.VersionUnknown || component.Version != "" || component.Channel != "" {
		t.Fatalf("legacy xkeen = %+v", component)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "legacy-init-executed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy init was executed or could not be checked: %v", err)
	}
}

func TestXKeenRejectsMixedS05xkeenAndLegacyS24xrayLayout(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createXkeenLayout(t)
	writeFixtureExecutable(t, fixture.config.XkeenLegacyRuntimeInit, "#!/bin/sh\nprintf legacy-init-executed > "+filepath.Join(fixture.root, "legacy-init-executed")+"\n")
	writeFixtureFile(t, fixture.config.XkeenPackageMetadata, []byte("Package: xkeen\nVersion: 2.0.1\nArchitecture: all\n"), 0o644)

	component := fixture.service().Snapshot(context.Background()).XKeen
	if component.State != StatePresent || !component.Present || component.ReasonCode != "mixed-layout" || component.Capability != CapabilityUnsupported || !component.VersionUnknown || component.Version != "" || component.Channel != "" {
		t.Fatalf("mixed xkeen = %+v", component)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "legacy-init-executed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy init was executed or could not be checked: %v", err)
	}
}

func TestXrayRejectsSymlinksAndNonRegularFilesBeforeProbing(t *testing.T) {
	t.Run("non-regular", func(t *testing.T) {
		fixture := newInventoryFixture(t)
		if err := os.MkdirAll(fixture.config.XrayBinary, 0o755); err != nil {
			t.Fatal(err)
		}
		component := fixture.service().Snapshot(context.Background()).Xray
		if component.State != StatePresent || component.ReasonCode != "not-regular" || fixture.probe.calls.Load() != 0 {
			t.Fatalf("xray directory = %+v calls=%d", component, fixture.probe.calls.Load())
		}
	})

	t.Run("symlink", func(t *testing.T) {
		fixture := newInventoryFixture(t)
		target := filepath.Join(fixture.root, "real-xray")
		writeFixtureExecutable(t, target, "synthetic target\n")
		if err := os.Symlink(target, fixture.config.XrayBinary); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		component := fixture.service().Snapshot(context.Background()).Xray
		if component.State != StatePresent || component.ReasonCode != "not-regular" || fixture.probe.calls.Load() != 0 {
			t.Fatalf("xray symlink = %+v calls=%d", component, fixture.probe.calls.Load())
		}
	})
}

func TestGeodataUsesExactCatalogAndMarksUnknownExpressionsManual(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createGeodata(t)
	if err := os.MkdirAll(filepath.Join(fixture.config.GeodataDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(fixture.config.GeodataDir, "custom.dat"), []byte("must not be read"), 0o644)
	writeFixtureFile(t, filepath.Join(fixture.config.GeodataDir, "nested", "geosite_refilter.dat"), []byte("must not be walked"), 0o644)
	writeFixtureFile(t, fixture.config.RoutingPath, []byte(`{"routing":{"rules":[{"domain":["ext:custom.dat:foo","ext:../outside:foo"],"ip":[]}]}}`), 0o644)

	geodata := fixture.service().Snapshot(context.Background()).Geodata
	if len(geodata.Items) != len(productGeodataCatalog)+2 {
		t.Fatalf("geodata item count = %d (%+v)", len(geodata.Items), geodata.Items)
	}
	if geodata.State != StateUnknown || geodata.Capability != CapabilityUnsupported || geodata.Present != true {
		t.Fatalf("geodata summary = %+v", geodata.Component)
	}
	items := make(map[string]GeodataItem, len(geodata.Items))
	for _, item := range geodata.Items {
		items[item.ID] = item
		if strings.Contains(item.Name, "nested") {
			t.Fatalf("recursive file leaked into projection: %+v", item)
		}
	}
	for _, entry := range productGeodataCatalog {
		item, ok := items[entry.ID]
		if !ok || item.Source != "product-catalog" || item.State != StatePresent || item.SHA256 == "" {
			t.Fatalf("catalog item %s = %+v, present=%v", entry.ID, item, ok)
		}
	}
	for _, id := range []string{"manual-geosite-custom.dat", "manual-geosite-unsupported"} {
		item, ok := items[id]
		if !ok || item.Source != "manual/unsupported" || item.State != StateUnknown || item.Present || item.SHA256 != "" || item.SizeBytes != 0 {
			t.Fatalf("manual item %s = %+v, present=%v", id, item, ok)
		}
	}
}

func TestGeodataPrefersValidatedTypedApplianceAuthority(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createGeodata(t)
	value := appliance.Appliance{
		SchemaVersion: appliance.SchemaVersion,
		DNS: appliance.DNSPolicy{
			Servers:       []appliance.DNSServer{{Address: "8.8.8.8", Domains: []string{"ext:geosite_refilter.dat:foo"}}},
			QueryStrategy: "UseIPv4",
		},
		Routing: appliance.RoutingPolicy{
			DomainStrategy: "AsIs",
			Rules: []appliance.RoutingRule{{
				Type:   "field",
				IP:     []string{"ext:geoip_refilter.dat:foo"},
				Domain: []string{"ext:geosite_v2fly.dat:bar"},
				Action: appliance.RuleAction{BalancerTag: "bal-proxy"},
			}},
			Balancers: []appliance.Balancer{{
				Tag:         "bal-proxy",
				Selector:    []string{"proxy-test"},
				FallbackTag: "block",
				Strategy:    appliance.BalancerStrategy{Type: "leastPing"},
			}},
		},
		Observatory: appliance.ObservatoryPolicy{SubjectSelector: []string{"proxy-test"}, ProbeInterval: "5m"},
	}
	contents, err := appliance.MarshalCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, fixture.config.AppliancePath, contents, 0o644)
	writeFixtureFile(t, fixture.config.RoutingPath, []byte(`{"routing":{"rules":[{"domain":["ext:manual-from-fallback.dat:foo"]}]}}`), 0o644)

	geodata := fixture.service().Snapshot(context.Background()).Geodata
	if len(geodata.Items) != len(productGeodataCatalog) {
		t.Fatalf("typed appliance produced fallback requirements: %+v", geodata.Items)
	}
	for _, item := range geodata.Items {
		if item.Source != "product-catalog" {
			t.Fatalf("typed appliance item = %+v", item)
		}
	}
}

func TestGeodataOversizeAndSymlinkRemainUnknownWithoutHash(t *testing.T) {
	t.Run("oversize", func(t *testing.T) {
		fixture := newInventoryFixture(t)
		if err := os.MkdirAll(fixture.config.GeodataDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(fixture.config.GeodataDir, productGeodataCatalog[0].Name)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(MaxGeodataFileBytes + 1); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		geodata := fixture.service().Snapshot(context.Background()).Geodata
		item := geodataItemByID(t, geodata, productGeodataCatalog[0].ID)
		if item.ReasonCode != "file-too-large" || item.State != StatePresent || item.SHA256 != "" || geodata.Capability != CapabilityUnsupported {
			t.Fatalf("oversize geodata = %+v summary=%+v", item, geodata.Component)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if before.Size() != after.Size() {
			t.Fatalf("oversize file changed from %d to %d", before.Size(), after.Size())
		}
	})

	t.Run("symlink", func(t *testing.T) {
		fixture := newInventoryFixture(t)
		if err := os.MkdirAll(fixture.config.GeodataDir, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(fixture.root, "secret-like-geodata")
		writeFixtureFile(t, target, []byte("not a catalog read"), 0o644)
		link := filepath.Join(fixture.config.GeodataDir, productGeodataCatalog[0].Name)
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		geodata := fixture.service().Snapshot(context.Background()).Geodata
		item := geodataItemByID(t, geodata, productGeodataCatalog[0].ID)
		if item.ReasonCode != "not-regular" || item.State != StatePresent || item.SHA256 != "" {
			t.Fatalf("symlink geodata = %+v", item)
		}
	})
}

func TestPlatformSignalsAreInformationalAndNeverExecuteOpkg(t *testing.T) {
	fixture := newInventoryFixture(t)
	writeFixtureFile(t, fixture.config.KeeneticOSVersionPath, []byte("release unknown\n"), 0o644)
	writeFixtureExecutable(t, fixture.config.EntwareBinary, "#!/bin/sh\nprintf marker > "+filepath.Join(fixture.root, "opkg-executed")+"\n")
	writeFixtureFile(t, fixture.config.EntwareReleasePath, []byte("not-a-version\n"), 0o644)
	snapshot := fixture.service().Snapshot(context.Background())
	if snapshot.KeeneticOS.State != StatePresent || snapshot.KeeneticOS.Version != "" || snapshot.KeeneticOS.ReasonCode != "version-unparseable" || snapshot.KeeneticOS.Capability != CapabilityInformational {
		t.Fatalf("OS signal = %+v", snapshot.KeeneticOS)
	}
	if snapshot.Entware.State != StatePresent || snapshot.Entware.Version != "" || snapshot.Entware.ReasonCode != "version-unparseable" || snapshot.Entware.Capability != CapabilityInformational {
		t.Fatalf("Entware signal = %+v", snapshot.Entware)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "opkg-executed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opkg marker exists or could not be checked: %v", err)
	}
}

func fixtureEntries(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(contents)
		entries[path] = hex.EncodeToString(hash[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func geodataItemByID(t *testing.T, value GeodataComponent, id string) GeodataItem {
	t.Helper()
	for _, item := range value.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("geodata item %s not found", id)
	return GeodataItem{}
}
