package configview

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestReaderReturnsPurposeBuiltSummary(t *testing.T) {
	dir := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("05_routing.json", `{"routing":{"rules":[{"ruleTag":"one"},{"ruleTag":"two"}],"balancers":[{"tag":"bal-proxy"}]}}`)
	write("02_dns.json", `{"dns":{"servers":[{"address":"https://8.8.8.8/dns-query?token=SECRET","tag":"dns-proxy"},{"address":"localhost"}]}}`)
	write("07_observatory.json", `{"observatory":{"subjectSelector":["proxy-main-","proxy-us-"],"probeInterval":"5m","probeUrl":"https://secret.invalid"}}`)
	xkeenPath := filepath.Join(dir, "xkeen.json")
	if err := os.WriteFile(xkeenPath, []byte(`{"xkeen":{"xray":{"speed_balancer":{"enabled":true,"interval":1440,"hysteresis":20,"balancer":"bal-proxy","max_nodes":128,"max_time":10,"test_url":"https://secret.invalid/down?bytes=20971520"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := NewReader(dir, xkeenPath).Read(context.Background())
	if !got.Available || got.Routing.RuleCount != 2 || len(got.Routing.Balancers) != 1 {
		t.Fatalf("unexpected routing summary: %+v", got.Routing)
	}
	if len(got.DNS.Upstreams) != 2 || got.DNS.Upstreams[0].Host != "8.8.8.8" || got.DNS.Upstreams[0].Tag != "dns-proxy" || got.DNS.Upstreams[1].Host != "localhost" || got.DNS.Upstreams[1].Tag != "" {
		t.Fatalf("unexpected DNS summary: %+v", got.DNS.Upstreams)
	}
	if got.Observatory.ProbeInterval != "5m" || got.SpeedBalancer.IntervalMin != 1440 {
		t.Fatalf("unexpected cadence summary: %+v %+v", got.Observatory, got.SpeedBalancer)
	}
	if got.SpeedBalancer.EligibleNodes != 128 || got.SpeedBalancer.PayloadBytes != 20971520 || got.SpeedBalancer.MaxBytes != (20<<20)*128 || got.SpeedBalancer.MaxSeconds != 1280 {
		t.Fatalf("benchmark budget summary = %+v", got.SpeedBalancer)
	}
}

func TestReaderPreservesProductionRuleDisplayTags(t *testing.T) {
	dir := t.TempDir()
	routing := `{"routing":{"domainStrategy":"AsIs","rules":[
{"type":"field","domain":["geosite:category-ads-all"],"ruleTag":"proxy DNS via unified pool","outboundTag":"bal-proxy"},
{"type":"field","domain":["geosite:category-ru"],"ruleTag":"Russian domains direct","outboundTag":"direct"},
{"type":"field","domain":["geosite:geolocation-!cn"],"ruleTag":"blocked and geo-sensitive domains","outboundTag":"block"},
{"type":"field","network":"tcp,udp","ruleTag":"xkeen-api","outboundTag":"api"},
{"type":"field","ruleTag":"rejected\tcontrol label","outboundTag":"block"}
],"balancers":[{"tag":"bal-proxy"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(routing), 0o600); err != nil {
		t.Fatal(err)
	}

	got := NewReader(dir, filepath.Join(dir, "missing-xkeen.json")).Read(context.Background())
	want := []string{
		"blocked and geo-sensitive domains",
		"proxy DNS via unified pool",
		"Russian domains direct",
		"xkeen-api",
	}
	sort.Strings(want)
	if got.Routing.RuleCount != 5 || !reflect.DeepEqual(got.Routing.RuleTags, want) {
		t.Fatalf("production routing summary = %+v, want count=%d tags=%v", got.Routing, 5, want)
	}
	if safeConfigLabel("proxy DNS via unified pool") != "" {
		t.Fatal("selector/tag sanitizer must continue rejecting spaces")
	}
	if safeRuleDisplayLabel("label\twith tab") != "" || safeRuleDisplayLabel("label\nwith newline") != "" {
		t.Fatal("rule display labels must reject control whitespace")
	}
	if safeRuleDisplayLabel("  proxy DNS via unified pool  ") != "proxy DNS via unified pool" {
		t.Fatal("rule display labels should trim only outer whitespace")
	}
	if safeRuleDisplayLabel("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx") != "" {
		t.Fatal("rule display labels must remain bounded")
	}
}
