package dnsobservatory

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
	"github.com/popiposter/xkeen-control/internal/routingpolicy"
)

type brokerTestActivator struct {
	restarts int
}

func (*brokerTestActivator) ValidateCandidate(context.Context, string) error { return nil }
func (activator *brokerTestActivator) Restart(context.Context) error {
	activator.restarts++
	return nil
}
func (*brokerTestActivator) WaitReady(context.Context) error { return nil }
func (*brokerTestActivator) VerifyOutboundTags(context.Context, []string) error {
	return nil
}

func TestDNSObservatoryPreviewApplyPreservesRoutingAndNodeOutputs(t *testing.T) {
	fixture := newBrokerFixture(t)
	service := NewService(Config{Appliance: appliance.NewService(appliance.Config{ConfigDir: fixture.configDir}), Settings: fixture.restore})
	catalog := appliance.DNSResolverCatalog()
	settings := appliance.DNSSettings{
		ProxyResolverIDs: []string{catalog[0].ID},
		FallbackMode:     appliance.FallbackModeDisabled,
		CacheEnabled:     true,
		ServeStale:       true,
		StaleTTLSeconds:  120,
		ParallelQueries:  false,
	}
	observatory := appliance.ObservatorySettings{ProbeIntervalMinutes: 2}
	preview, err := service.Preview(context.Background(), "session-a", settings, observatory)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Token == "" || preview.Noop || preview.Diff.DNS.ResolverIDsRemoved == nil || preview.Diff.Observatory.ProbeIntervalMinutesBefore != 5 || preview.Diff.Observatory.ProbeIntervalMinutesAfter != 2 {
		t.Fatalf("DNS/Observatory preview = %+v", preview)
	}
	nodesBefore, err := os.ReadFile(fixture.nodesPath)
	if err != nil {
		t.Fatal(err)
	}
	outboundsBefore, err := os.ReadFile(fixture.outboundsPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), "session-a", preview.Token)
	if err != nil || result.Noop || result.Classification != "applied" {
		t.Fatalf("DNS/Observatory apply = %+v, %v", result, err)
	}
	nodesAfter, _ := os.ReadFile(fixture.nodesPath)
	outboundsAfter, _ := os.ReadFile(fixture.outboundsPath)
	if !bytes.Equal(nodesBefore, nodesAfter) || !bytes.Equal(outboundsBefore, outboundsAfter) {
		t.Fatal("DNS/Observatory apply changed node authority or generated outbounds")
	}
	projection, err := service.Read(context.Background())
	if err != nil || projection.Editability != EditabilityEditable || len(projection.DNS.ProxyResolverIDs) != 1 || projection.DNS.FallbackMode != appliance.FallbackModeDisabled || projection.Observatory.ProbeIntervalMinutes != 2 {
		t.Fatalf("post-apply projection = %+v, %v", projection, err)
	}
	if fixture.activator.restarts != 1 {
		t.Fatalf("restart count = %d, want 1", fixture.activator.restarts)
	}
	routing := routingpolicy.NewService(routingpolicy.Config{Appliance: appliance.NewService(appliance.Config{ConfigDir: fixture.configDir}), Settings: fixture.restore})
	routingProjection, err := routing.Read(context.Background())
	if err != nil || routingProjection.Editability != routingpolicy.EditabilityEditable {
		t.Fatalf("routing after DNS apply = %+v, %v", routingProjection, err)
	}
}

func TestIndependentBrokerPreviewsDoNotMergeAndRoutingApplyPreservesDNS(t *testing.T) {
	fixture := newBrokerFixture(t)
	appService := appliance.NewService(appliance.Config{ConfigDir: fixture.configDir})
	dnsService := NewService(Config{Appliance: appService, Settings: fixture.restore})
	routingService := routingpolicy.NewService(routingpolicy.Config{Appliance: appService, Settings: fixture.restore})
	catalog := appliance.DNSResolverCatalog()
	dnsSettings := appliance.DNSSettings{
		ProxyResolverIDs: []string{catalog[0].ID, catalog[1].ID},
		FallbackMode:     appliance.FallbackModeSystem,
		CacheEnabled:     true,
		ServeStale:       true,
		StaleTTLSeconds:  600,
		ParallelQueries:  true,
	}
	dnsPreview, err := dnsService.Preview(context.Background(), "session-a", dnsSettings, appliance.ObservatorySettings{ProbeIntervalMinutes: 4})
	if err != nil {
		t.Fatal(err)
	}
	routingPreview, err := routingService.Preview(context.Background(), "session-b", []appliance.CustomRule{{Name: "route", Domains: []string{"domain:route.example"}, Action: appliance.CustomRuleProxy}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routingService.Apply(context.Background(), "session-b", routingPreview.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := dnsService.Apply(context.Background(), "session-a", dnsPreview.Token); err != ErrPreviewStale {
		t.Fatalf("DNS preview after routing apply = %v, want %v", err, ErrPreviewStale)
	}
	current, err := dnsService.Read(context.Background())
	if err != nil || current.Editability != EditabilityEditable || len(current.DNS.ProxyResolverIDs) != 2 || current.Observatory.ProbeIntervalMinutes != 5 {
		t.Fatalf("routing apply changed DNS projection = %+v, %v", current, err)
	}
	dnsPreview, err = dnsService.Preview(context.Background(), "session-a", dnsSettings, appliance.ObservatorySettings{ProbeIntervalMinutes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dnsService.Apply(context.Background(), "session-a", dnsPreview.Token); err != nil {
		t.Fatal(err)
	}
	current, err = dnsService.Read(context.Background())
	if err != nil || current.Observatory.ProbeIntervalMinutes != 4 || len(current.DNS.ProxyResolverIDs) != 2 {
		t.Fatalf("DNS settings after second apply = %+v, %v", current, err)
	}
	routingProjection, err := routingService.Read(context.Background())
	if err != nil || len(routingProjection.Rules) != 1 || routingProjection.DNS.DerivedDomainCount != 1 {
		t.Fatalf("routing after DNS apply = %+v, %v", routingProjection, err)
	}
}

type brokerFixture struct {
	root          string
	configDir     string
	appliancePath string
	nodesPath     string
	outboundsPath string
	activator     *brokerTestActivator
	restore       *restore.Service
}

func newBrokerFixture(t *testing.T) brokerFixture {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "xray")
	appliancePath := filepath.Join(root, "control", "config", "appliance.json")
	nodesPath := filepath.Join(root, "control", "secrets", "nodes.json")
	outboundsPath := filepath.Join(configDir, "04_outbounds.json")
	writeBrokerFile(t, appliancePath, mustBrokerBytes(t, appliance.ProductDefault()))
	policyFiles, err := appliance.RenderPolicyFiles(appliance.ProductDefault())
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range policyFiles {
		writeBrokerFile(t, filepath.Join(configDir, filepath.Base(name)), contents)
	}
	fixed, err := appliance.CompatibilityFiles()
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range fixed {
		if filepath.Base(filepath.Dir(name)) == "xray" {
			writeBrokerFile(t, filepath.Join(configDir, filepath.Base(name)), contents)
		} else {
			writeBrokerFile(t, filepath.Join(root, "xkeen", "xkeen.json"), contents)
		}
	}
	registryBytes, err := nodes.MarshalCanonical(nodes.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	writeBrokerFile(t, nodesPath, registryBytes)
	outbounds, err := nodes.Render(nodes.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	writeBrokerFile(t, outboundsPath, outbounds)
	activator := &brokerTestActivator{}
	service := restore.NewService(restore.Config{
		AppliancePath: appliancePath, NodesPath: nodesPath, ConfigDir: configDir,
		XkeenConfigPath: filepath.Join(root, "xkeen", "xkeen.json"), ActiveOutboundsPath: outboundsPath,
		PreviousDir: filepath.Join(root, "control", "previous", "appliance-import"),
		StateDir:    filepath.Join(root, "control", "state"), Activator: activator, AuthorityLease: authority.NewLease(),
	})
	return brokerFixture{root: root, configDir: configDir, appliancePath: appliancePath, nodesPath: nodesPath, outboundsPath: outboundsPath, activator: activator, restore: service}
}

func mustBrokerBytes(t *testing.T, value appliance.Appliance) []byte {
	t.Helper()
	contents, err := appliance.MarshalCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func writeBrokerFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
