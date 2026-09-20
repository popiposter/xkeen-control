package routingpolicy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/restore"
)

type policyTestActivator struct {
	restarts int
}

func (*policyTestActivator) ValidateCandidate(context.Context, string) error { return nil }
func (activator *policyTestActivator) Restart(context.Context) error {
	activator.restarts++
	return nil
}
func (*policyTestActivator) WaitReady(context.Context) error { return nil }
func (*policyTestActivator) VerifyOutboundTags(context.Context, []string) error {
	return nil
}

func TestPreviewApplyUsesSharedSettingsOwnerAndPreservesNodeOutputs(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	writePolicyTestFiles(t, root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath)

	lease := authority.NewLease()
	activator := &policyTestActivator{}
	restoreService := restore.NewService(restore.Config{
		AppliancePath: appliancePath, NodesPath: nodesPath, ConfigDir: configDir,
		XkeenConfigPath: xkeenPath, ActiveOutboundsPath: outboundsPath,
		PreviousDir: filepath.Join(root, "control", "previous", "appliance-import"),
		StateDir:    filepath.Join(root, "control", "state"), Activator: activator, AuthorityLease: lease,
	})
	applianceService := appliance.NewService(appliance.Config{ConfigDir: configDir})
	service := NewService(Config{Appliance: applianceService, Settings: restoreService})

	projection, err := service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityEditable || len(projection.Rules) != 0 {
		t.Fatalf("initial policy projection = %+v, %v", projection, err)
	}
	rules := []appliance.CustomRule{{
		Name: "Work proxy", Domains: []string{"domain:work.example"}, Action: appliance.CustomRuleProxy,
	}}
	preview, err := service.Preview(context.Background(), "session-a", rules)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Token == "" || preview.Noop || !preview.Diff.RestartRequired || preview.Diff.DNSDerivedDomainCountDelta != 1 {
		t.Fatalf("policy preview = %+v", preview)
	}
	originalNodes, err := os.ReadFile(nodesPath)
	if err != nil {
		t.Fatal(err)
	}
	originalOutbounds, err := os.ReadFile(outboundsPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), "session-a", preview.Token)
	if err != nil || result.Classification != "applied" || result.Noop {
		t.Fatalf("policy apply = %+v, %v", result, err)
	}
	gotNodes, _ := os.ReadFile(nodesPath)
	gotOutbounds, _ := os.ReadFile(outboundsPath)
	if !bytes.Equal(gotNodes, originalNodes) || !bytes.Equal(gotOutbounds, originalOutbounds) {
		t.Fatal("policy apply changed node authority or generated outbounds")
	}
	projection, err = service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityEditable || len(projection.Rules) != 1 || projection.DNS.DerivedDomainCount != 1 {
		t.Fatalf("post-apply policy projection = %+v, %v", projection, err)
	}
	if _, err := os.Stat(filepath.Join(root, "control", "state", "appliance-import-transaction.json")); !os.IsNotExist(err) {
		t.Fatalf("shared transaction journal remains: %v", err)
	}
}

func TestNoopPolicyApplyDoesNotWriteOrRestart(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	writePolicyTestFiles(t, root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath)
	activator := &policyTestActivator{}
	restoreService := newPolicyRestoreService(root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath, activator)
	service := NewService(Config{Appliance: appliance.NewService(appliance.Config{ConfigDir: configDir}), Settings: restoreService})
	beforeAppliance, _ := os.ReadFile(appliancePath)
	beforeRouting, _ := os.ReadFile(filepath.Join(configDir, "05_routing.json"))
	preview, err := service.Preview(context.Background(), "session-a", []appliance.CustomRule{})
	if err != nil || !preview.Noop || preview.Diff.RestartRequired {
		t.Fatalf("no-op policy preview = %+v, %v", preview, err)
	}
	result, err := service.Apply(context.Background(), "session-a", preview.Token)
	if err != nil || !result.Noop || result.Classification != "no-op" || activator.restarts != 0 {
		t.Fatalf("no-op policy apply = %+v, %v, restarts=%d", result, err, activator.restarts)
	}
	afterAppliance, _ := os.ReadFile(appliancePath)
	afterRouting, _ := os.ReadFile(filepath.Join(configDir, "05_routing.json"))
	if !bytes.Equal(beforeAppliance, afterAppliance) || !bytes.Equal(beforeRouting, afterRouting) {
		t.Fatal("no-op policy apply changed persistent files")
	}
}

func TestMatchMemberPermutationsAreNoopAndDoNotRestart(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	writePolicyTestFiles(t, root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath)
	activator := &policyTestActivator{}
	restoreService := newPolicyRestoreService(root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath, activator)
	service := NewService(Config{Appliance: appliance.NewService(appliance.Config{ConfigDir: configDir}), Settings: restoreService})

	canonical := []appliance.CustomRule{{
		Name:      "set rule",
		Domains:   []string{"domain:b.example", "domain:a.example"},
		IPs:       []string{"192.0.2.0/24", "10.0.0.0/8"},
		Protocols: []string{"tls", "http"},
		Networks:  []string{"udp", "tcp"},
		Ports:     []appliance.PortRange{{From: 443, To: 443}, {From: 80, To: 80}},
		Action:    appliance.CustomRuleDirect,
	}}
	preview, err := service.Preview(context.Background(), "session-a", canonical)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Noop {
		t.Fatal("first custom-rule preview was unexpectedly a no-op")
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.Token); err != nil {
		t.Fatal(err)
	}
	if activator.restarts != 1 {
		t.Fatalf("first apply restarts = %d, want 1", activator.restarts)
	}
	beforeAppliance, err := os.ReadFile(appliancePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeRouting, err := os.ReadFile(filepath.Join(configDir, "05_routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeDNS, err := os.ReadFile(filepath.Join(configDir, "02_dns.json"))
	if err != nil {
		t.Fatal(err)
	}

	permuted := []appliance.CustomRule{{
		Name:      "set rule",
		Domains:   []string{"domain:a.example", "domain:b.example"},
		IPs:       []string{"10.0.0.0/8", "192.0.2.0/24"},
		Protocols: []string{"http", "tls"},
		Networks:  []string{"tcp", "udp"},
		Ports:     []appliance.PortRange{{From: 80, To: 80}, {From: 443, To: 443}},
		Action:    appliance.CustomRuleDirect,
	}}
	permutedPreview, err := service.Preview(context.Background(), "session-a", permuted)
	if err != nil {
		t.Fatal(err)
	}
	if !permutedPreview.Noop || permutedPreview.Diff.RestartRequired || len(permutedPreview.Diff.Changed) != 0 || len(permutedPreview.Diff.Reordered) != 0 {
		t.Fatalf("permuted preview = %+v", permutedPreview)
	}
	result, err := service.Apply(context.Background(), "session-a", permutedPreview.Token)
	if err != nil || !result.Noop || result.Classification != "no-op" || activator.restarts != 1 {
		t.Fatalf("permuted apply = %+v, %v, restarts=%d", result, err, activator.restarts)
	}
	afterAppliance, _ := os.ReadFile(appliancePath)
	afterRouting, _ := os.ReadFile(filepath.Join(configDir, "05_routing.json"))
	afterDNS, _ := os.ReadFile(filepath.Join(configDir, "02_dns.json"))
	if !bytes.Equal(beforeAppliance, afterAppliance) || !bytes.Equal(beforeRouting, afterRouting) || !bytes.Equal(beforeDNS, afterDNS) {
		t.Fatal("permuted no-op apply changed persistent policy files")
	}
}

func TestPolicyPreviewBecomesStaleAfterAuthorityChange(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	writePolicyTestFiles(t, root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath)
	activator := &policyTestActivator{}
	restoreService := newPolicyRestoreService(root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath, activator)
	service := NewService(Config{Appliance: appliance.NewService(appliance.Config{ConfigDir: configDir}), Settings: restoreService})
	rules := []appliance.CustomRule{{Name: "stale", Action: appliance.CustomRuleDirect}}
	preview, err := service.Preview(context.Background(), "session-a", rules)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := appliance.CompileCustomRules(appliance.ProductDefault(), rules)
	if err != nil {
		t.Fatal(err)
	}
	candidateBytes, err := appliance.MarshalCanonical(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appliancePath, candidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.Token); err != ErrPreviewStale {
		t.Fatalf("stale policy apply = %v", err)
	}
	if _, err := service.Apply(context.Background(), "session-a", preview.Token); err != ErrPreviewExpired {
		t.Fatalf("stale token replay = %v", err)
	}
}

func TestReadAndPreviewFailClosedOnProtectedDriftAndRequestBounds(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	xkeenPath := filepath.Join(root, "xkeen", "xkeen.json")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	writePolicyTestFiles(t, root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath)
	activator := &policyTestActivator{}
	restoreService := newPolicyRestoreService(root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath, activator)
	service := NewService(Config{Appliance: appliance.NewService(appliance.Config{ConfigDir: configDir}), Settings: restoreService})
	if err := os.WriteFile(filepath.Join(configDir, "05_routing.json"), []byte(`{"routing":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err := service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityDriftDetected {
		t.Fatalf("drift projection = %+v, %v", projection, err)
	}
	if _, err := service.Preview(context.Background(), "session-a", []appliance.CustomRule{{Name: "bad", Action: appliance.CustomRuleAction("raw")}}); err != ErrInvalidRequest {
		t.Fatalf("invalid policy request = %v", err)
	}
}

func writePolicyTestFiles(t *testing.T, root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath string) {
	t.Helper()
	base := appliance.ProductDefault()
	applianceBytes, err := appliance.MarshalCanonical(base)
	if err != nil {
		t.Fatal(err)
	}
	writePolicyFile(t, appliancePath, applianceBytes)
	policyFiles, err := appliance.RenderPolicyFiles(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range policyFiles {
		writePolicyFile(t, filepath.Join(configDir, filepath.Base(name)), contents)
	}
	fixed, err := appliance.CompatibilityFiles()
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range fixed {
		if filepath.Base(filepath.Dir(name)) == "xray" {
			writePolicyFile(t, filepath.Join(configDir, filepath.Base(name)), contents)
		} else {
			writePolicyFile(t, xkeenPath, contents)
		}
	}
	registryBytes, err := nodes.MarshalCanonical(nodes.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	writePolicyFile(t, nodesPath, registryBytes)
	outbounds, err := nodes.Render(nodes.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	writePolicyFile(t, outboundsPath, outbounds)
	_ = root
}

func newPolicyRestoreService(root, configDir, xkeenPath, appliancePath, nodesPath, outboundsPath string, activator *policyTestActivator) *restore.Service {
	return restore.NewService(restore.Config{
		AppliancePath: appliancePath, NodesPath: nodesPath, ConfigDir: configDir,
		XkeenConfigPath: xkeenPath, ActiveOutboundsPath: outboundsPath,
		PreviousDir: filepath.Join(root, "control", "previous", "appliance-import"),
		StateDir:    filepath.Join(root, "control", "state"), Activator: activator,
		AuthorityLease: authority.NewLease(),
	})
}

func writePolicyFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
