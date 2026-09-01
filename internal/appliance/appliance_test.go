package appliance

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/nodes"
)

func TestCurrentRepositoryPolicyRoundTripsThroughTypedAppliance(t *testing.T) {
	repoRoot := repositoryRoot(t)
	value, err := parseActivePolicy(filepath.Join(repoRoot, "config", "xray"))
	if err != nil {
		t.Fatalf("parse repository policy: %v", err)
	}
	contents, err := MarshalCanonical(value)
	if err != nil {
		t.Fatalf("marshal appliance: %v", err)
	}
	decoded, err := Parse(contents)
	if err != nil {
		t.Fatalf("parse canonical appliance: %v", err)
	}
	if !canonicalEqual(value, decoded) {
		t.Fatal("typed appliance changed across canonical round trip")
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("repository policy validation: %v", err)
	}
	files, err := renderPolicyFiles(value)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := parseActivePolicyBytes(files["xray/02_dns.json"], files["xray/05_routing.json"], files["xray/07_observatory.json"])
	if err != nil || !canonicalEqual(value, rendered) {
		t.Fatalf("policy render did not round trip: %v", err)
	}
	if len(value.Routing.Rules) < 2 || value.Routing.Rules[0].RuleTag != "xkeen-api" {
		t.Fatalf("routing rule order was not preserved: %+v", value.Routing.Rules[:1])
	}
}

func TestApplianceStrictValidationRejectsUnknownFieldsAndBadPorts(t *testing.T) {
	base := `{"schemaVersion":1,"dns":{"servers":["localhost"],"queryStrategy":"UseIPv4","disableCache":false,"serveStale":true,"serveExpiredTTL":3600,"disableFallback":false,"disableFallbackIfMatch":true,"enableParallelQuery":true,"useSystemHosts":true},"routing":{"domainStrategy":"IPIfNonMatch","domainMatcher":"hybrid","rules":[{"type":"field","inboundTag":["api"],"action":{"outboundTag":"api"}}],"balancers":[{"tag":"bal-proxy","selector":["proxy-"],"fallbackTag":"block","strategy":{"type":"leastPing"}}]},"observatory":{"subjectSelector":["proxy-"],"probeInterval":"5m"}}`
	if _, err := Parse([]byte(strings.Replace(base, `"action":{"outboundTag":"api"}`, `"action":{"outboundTag":"api"},"unexpected":true`, 1))); err == nil {
		t.Fatal("unknown appliance field was accepted")
	}
	badPort := strings.Replace(base, `"action":{"outboundTag":"api"}`, `"ports":[{"from":0,"to":1}],"action":{"outboundTag":"api"}`, 1)
	if _, err := Parse([]byte(badPort)); err == nil {
		t.Fatal("invalid typed port range was accepted")
	}
	badNetwork := strings.Replace(base, `"action":{"outboundTag":"api"}`, `"network":["sctp"],"action":{"outboundTag":"api"}`, 1)
	if _, err := Parse([]byte(badNetwork)); err == nil {
		t.Fatal("invalid typed network was accepted")
	}
}

func TestAdoptionWritesOnlyApplianceAuthorityAndVerifyDetectsDrift(t *testing.T) {
	fixture := newApplianceFixture(t)
	before := fixture.snapshotActive()
	validator := &recordingValidator{}
	fixture.service = NewService(Config{
		AppliancePath:       fixture.appliancePath,
		ConfigDir:           fixture.configDir,
		XkeenConfigPath:     fixture.xkeenPath,
		NodesPath:           fixture.nodesPath,
		ActiveOutboundsPath: fixture.outboundsPath,
		Validator:           validator,
	})
	if err := fixture.service.Adopt(context.Background()); err != nil {
		t.Fatalf("adopt current policy: %v", err)
	}
	if validator.calls != 1 || validator.candidate == "" {
		t.Fatalf("candidate validation calls = %d path=%q", validator.calls, validator.candidate)
	}
	for path, expected := range before {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("adoption changed active file %s", path)
		}
	}
	info, err := os.Stat(fixture.appliancePath)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("appliance authority mode = %v, %v", info.Mode().Perm(), err)
	}
	if err := fixture.service.Verify(context.Background()); err != nil {
		t.Fatalf("verify adopted policy: %v", err)
	}
	if validator.calls != 2 {
		t.Fatalf("verify candidate validation calls = %d", validator.calls)
	}
	if err := os.WriteFile(filepath.Join(fixture.configDir, "05_routing.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Verify(context.Background()); err == nil {
		t.Fatal("verify accepted active policy drift")
	}
	if err := fixture.service.Adopt(context.Background()); err == nil {
		t.Fatal("second adoption unexpectedly replaced appliance authority")
	}
}

func TestAdoptionBlocksFixedTemplateAndOutboundDriftBeforeCandidateValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*applianceFixture) error
	}{
		{name: "fixed template", mutate: func(fixture *applianceFixture) error {
			return os.WriteFile(filepath.Join(fixture.configDir, "03_inbounds.json"), []byte(`{"inbounds":[]}`), 0o600)
		}},
		{name: "generated outbounds", mutate: func(fixture *applianceFixture) error {
			return os.WriteFile(fixture.outboundsPath, []byte(`{"outbounds":[]}`), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newApplianceFixture(t)
			if err := test.mutate(fixture); err != nil {
				t.Fatal(err)
			}
			validator := &recordingValidator{}
			fixture.service = NewService(Config{
				AppliancePath: fixture.appliancePath, ConfigDir: fixture.configDir,
				XkeenConfigPath: fixture.xkeenPath, NodesPath: fixture.nodesPath,
				ActiveOutboundsPath: fixture.outboundsPath, Validator: validator,
			})
			if err := fixture.service.Adopt(context.Background()); err == nil {
				t.Fatal("incompatible active state was adopted")
			}
			if validator.calls != 0 {
				t.Fatalf("candidate validator ran before compatibility proof: %d", validator.calls)
			}
			if _, err := os.Stat(fixture.appliancePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("appliance authority was written after failed adoption: %v", err)
			}
		})
	}
}

func TestCandidateValidationFailureDoesNotCommitAppliance(t *testing.T) {
	fixture := newApplianceFixture(t)
	before := fixture.snapshotActive()
	validator := &recordingValidator{err: errors.New("synthetic candidate failure")}
	fixture.service = NewService(Config{
		AppliancePath: fixture.appliancePath, ConfigDir: fixture.configDir,
		XkeenConfigPath: fixture.xkeenPath, NodesPath: fixture.nodesPath,
		ActiveOutboundsPath: fixture.outboundsPath, Validator: validator,
	})
	if err := fixture.service.Adopt(context.Background()); err == nil {
		t.Fatal("failed candidate was adopted")
	}
	if validator.calls != 1 {
		t.Fatalf("candidate validation calls = %d", validator.calls)
	}
	if _, err := os.Stat(fixture.appliancePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed adoption wrote appliance authority: %v", err)
	}
	for path, expected := range before {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("failed adoption changed active file %s", path)
		}
	}
}

type recordingValidator struct {
	calls     int
	candidate string
	err       error
}

func (v *recordingValidator) ValidateCandidate(_ context.Context, path string) error {
	v.calls++
	v.candidate = path
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json", "06_policy.json", "07_observatory.json", "08_api.json"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			return errors.New("candidate is incomplete")
		}
	}
	return v.err
}

type applianceFixture struct {
	configDir     string
	xkeenPath     string
	nodesPath     string
	outboundsPath string
	appliancePath string
	service       *Service
}

func newApplianceFixture(t *testing.T) *applianceFixture {
	t.Helper()
	repoRoot := repositoryRoot(t)
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	nodesPath := filepath.Join(root, "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	for _, name := range []string{"02_dns.json", "05_routing.json", "07_observatory.json"} {
		copyFixtureFile(t, filepath.Join(repoRoot, "config", "xray", name), filepath.Join(configDir, name))
	}
	for _, name := range []string{"01_log.json", "03_inbounds.json", "06_policy.json", "08_api.json"} {
		copyFixtureFile(t, filepath.Join(repoRoot, "config", "xray", name), filepath.Join(configDir, name))
	}
	copyFixtureFile(t, filepath.Join(repoRoot, "config", "xkeen", "xkeen.json"), xkeenPath)
	registry := nodes.NewRegistry()
	node, err := nodes.NewNodeWithID(nodes.VLESS{
		UUID: "11111111-1111-1111-1111-111111111111", Host: "node.example.com", Port: 443,
		Encryption: "none", Security: "reality", ServerName: "node.example.com", Fingerprint: "chrome",
		PublicKey: "AAAAAAAAAAAAAAAA", ShortID: "0123456789abcdef", Network: "tcp",
	}, "Fixture node", nodes.Source{Type: "manual"}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	registry.Nodes = []nodes.Node{node}
	if err := (nodes.Store{Path: nodesPath}).Save(registry); err != nil {
		t.Fatal(err)
	}
	contents, err := nodes.Render(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, outboundsPath, contents)
	return &applianceFixture{
		configDir: configDir, xkeenPath: xkeenPath, nodesPath: nodesPath,
		outboundsPath: outboundsPath, appliancePath: appliancePath,
	}
}

func (f *applianceFixture) snapshotActive() map[string][]byte {
	result := make(map[string][]byte)
	for _, path := range []string{
		filepath.Join(f.configDir, "01_log.json"), filepath.Join(f.configDir, "02_dns.json"),
		filepath.Join(f.configDir, "03_inbounds.json"), filepath.Join(f.configDir, "04_outbounds.json"),
		filepath.Join(f.configDir, "05_routing.json"), filepath.Join(f.configDir, "06_policy.json"),
		filepath.Join(f.configDir, "07_observatory.json"), filepath.Join(f.configDir, "08_api.json"), f.xkeenPath,
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		result[path] = contents
	}
	return result
}

func copyFixtureFile(t *testing.T, source, destination string) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, destination, contents)
}

func writeFixtureFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
