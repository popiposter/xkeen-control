package components

import (
	"context"
	"os"
	"testing"
)

func TestParseXrayVersionOutputAcceptsCurrentOfficialVersionStatement(t *testing.T) {
	output := []byte("Xray 26.7.28 (Xray, Penetrates Everything.) cd4ce97 (go1.25.0 linux/arm64)\nA unified platform for anti-censorship.\n")
	signal, err := ParseXrayVersionOutput(output, nil)
	if err != nil {
		t.Fatalf("official version statement rejected: %v", err)
	}
	if signal.Version != "26.7.28" || signal.Architecture != "arm64" {
		t.Fatalf("official version statement = %+v", signal)
	}
}

func TestGeodataFailsClosedForInvalidPresentApplianceAuthority(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createGeodata(t)
	writeFixtureFile(t, fixture.config.AppliancePath, []byte(`{"schemaVersion":1,"dns":`), 0o644)
	writeFixtureFile(t, fixture.config.DNSPath, []byte(`{"dns":{"servers":[]}}`), 0o644)
	writeFixtureFile(t, fixture.config.RoutingPath, []byte(`{"routing":{"rules":[]}}`), 0o644)

	geodata := fixture.service().Snapshot(context.Background()).Geodata
	if geodata.State != StateUnknown || geodata.Capability != CapabilityUnsupported || geodata.ReasonCode != "appliance-authority-invalid" {
		t.Fatalf("invalid adopted authority = %+v", geodata.Component)
	}
}

func TestGeodataFailsClosedForNonRegularPresentApplianceAuthority(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createGeodata(t)
	if err := os.MkdirAll(fixture.config.AppliancePath, 0o755); err != nil {
		t.Fatal(err)
	}

	geodata := fixture.service().Snapshot(context.Background()).Geodata
	if geodata.State != StateUnknown || geodata.Capability != CapabilityUnsupported || geodata.ReasonCode != "appliance-authority-invalid" {
		t.Fatalf("non-regular adopted authority = %+v", geodata.Component)
	}
}

func TestGeodataFailsClosedForInvalidLegacyFallbackPolicy(t *testing.T) {
	fixture := newInventoryFixture(t)
	fixture.createGeodata(t)
	writeFixtureFile(t, fixture.config.DNSPath, []byte(`{"dns":{"servers":[`), 0o644)
	writeFixtureFile(t, fixture.config.RoutingPath, []byte(`{"routing":{"rules":[]}}`), 0o644)

	geodata := fixture.service().Snapshot(context.Background()).Geodata
	if geodata.State != StateUnknown || geodata.Capability != CapabilityUnsupported || geodata.ReasonCode != "legacy-policy-invalid" {
		t.Fatalf("invalid legacy fallback = %+v", geodata.Component)
	}
}
