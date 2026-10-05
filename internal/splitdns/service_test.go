package splitdns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
	"google.golang.org/protobuf/encoding/protowire"
)

func site(category, value string, kind uint64) []byte {
	field := func(n protowire.Number, b []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, n, protowire.BytesType), b)
	}
	domain := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), kind)
	domain = append(domain, field(2, []byte(value))...)
	return field(1, append(field(1, []byte(category)), field(2, domain)...))
}
func fixture(t *testing.T) (*Service, map[string][]byte, *int) {
	t.Helper()
	dir := t.TempDir()
	asset := t.TempDir()
	files := map[string][]byte{
		"02_dns.json":       []byte(`{"dns":{"servers":[{"address":"https://1.1.1.1/dns-query","tag":"panel-dns-vpn"}]}}`),
		"03_inbounds.json":  []byte(`{"inbounds":[{"tag":"dns","listen":"127.0.0.1","port":5310,"protocol":"socks"}]}`),
		"04_outbounds.json": []byte(`{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"},{"protocol":"vless","settings":{"vnext":[{"address":"198.51.100.10"}]}}]}`),
		"05_routing.json":   []byte(`{"routing":{"rules":[{"type":"field","inboundTag":["dns"],"balancerTag":"vpn"},{"type":"field","protocol":["quic"],"domain":["domain:google.test"],"outboundTag":"block"},{"type":"field","domain":["full:ads.test"],"outboundTag":"block"},{"type":"field","domain":["full:google.test","ext:geosite_test.dat:VPN"],"balancerTag":"vpn"},{"type":"field","domain":["domain:ru"],"outboundTag":"direct"},{"type":"field","outboundTag":"direct"}]}}`),
	}
	data := site("VPN", "example.test", 2)
	if os.WriteFile(filepath.Join(asset, "geosite_test.dat"), data, 0600) != nil {
		t.Fatal("fixture")
	}
	config := []byte(`{"log":{"level":"error","file":"/dev/null"},"plugins":[{"tag":"direct","type":"forward","args":{"upstreams":[{"addr":"https://8.8.8.8/dns-query"}]}},{"tag":"tunnel","type":"forward","args":{"socks5":"127.0.0.1:5310","upstreams":[{"addr":"https://8.8.8.8/dns-query"}]}},{"tag":"direct_only","type":"sequence","args":[{"exec":"$direct"},{"exec":"accept"}]},{"tag":"vpn_only","type":"sequence","args":[{"exec":"$tunnel"},{"exec":"accept"}]},{"tag":"main","type":"sequence","args":[{"exec":"goto direct_only"}]}]}`)
	if os.WriteFile(filepath.Join(dir, "config.json"), config, 0600) != nil {
		t.Fatal("fixture")
	}
	count := 0
	s := &Service{Dir: dir, AssetDir: asset, Lease: authority.NewLease(), ReadNative: func(context.Context) (map[string][]byte, error) { return files, nil }, Ready: func(context.Context) bool { return true }, Identity: func(context.Context) string { return fmt.Sprint(count) }}
	s.Restart = func(context.Context) error { count++; return nil }
	return s, files, &count
}

func TestNativeOrderAndNoRestartForUnchangedRules(t *testing.T) {
	s, files, count := fixture(t)
	ctx := context.Background()
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err, s.Status(ctx))
	}
	if *count != 1 {
		t.Fatal(*count)
	}
	config, err := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root struct{ Plugins []plugin }
	_ = json.Unmarshal(config, &root)
	var main []struct{ Exec string }
	for _, p := range root.Plugins {
		if p.Tag == "main" {
			_ = json.Unmarshal(p.Args, &main)
		}
	}
	if len(main) != 4 || main[0].Exec != "reject 3" || main[1].Exec != "goto vpn_only" || main[2].Exec != "goto direct_only" {
		t.Fatal(main)
	}
	if err = s.Sync(ctx); err != nil || *count != 1 {
		t.Fatal(err, *count)
	}
	files["05_routing.json"] = []byte(strings.Replace(string(files["05_routing.json"]), "full:ads.test", "full:new-ads.test", 1))
	if err = s.Sync(ctx); err != nil || *count != 2 {
		t.Fatal(err, *count)
	}
}

func TestPendingConfigurationDoesNotChangeDNS(t *testing.T) {
	s, _, count := fixture(t)
	s.Pending = func() bool { return true }
	before, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if string(before) != string(after) || *count != 0 || s.Status(context.Background()).State != "pending" {
		t.Fatal("pending DNS changed")
	}
}

func TestUnknownRestartSurvivesPanelRestartWithoutReplay(t *testing.T) {
	s, _, count := fixture(t)
	s.Restart = func(context.Context) error { *count++; return errors.New("timeout") }
	if s.Sync(context.Background()) == nil || *count != 1 {
		t.Fatal("unknown restart accepted")
	}
	// A new panel instance sees the same durable own-operation marker.
	fresh := &Service{Dir: s.Dir, AssetDir: s.AssetDir, Lease: s.Lease, ReadNative: s.ReadNative, Ready: s.Ready, Identity: s.Identity, Restart: func(context.Context) error { *count++; return nil }}
	if fresh.ReconcileOwned(context.Background()) == nil || *count != 1 {
		t.Fatal("ambiguous DNS restart replayed")
	}
	// Actual new process/readiness + exact files confirm without another restart.
	if err := fresh.Sync(context.Background()); err != nil || *count != 1 {
		t.Fatal(err, *count)
	}
}

func TestChangedSourceRejectedBeforeActivation(t *testing.T) {
	s, files, count := fixture(t)
	calls := 0
	s.ReadNative = func(context.Context) (map[string][]byte, error) {
		calls++
		copy := map[string][]byte{}
		for n, b := range files {
			copy[n] = append([]byte(nil), b...)
		}
		if calls == 2 {
			copy["02_dns.json"] = []byte(`{"dns":{}}`)
		}
		return copy, nil
	}
	if s.Sync(context.Background()) == nil || *count != 0 {
		t.Fatal("concurrent source drift activated")
	}
}

func TestUnsupportedTargetAndResolverNeverBecomeDirect(t *testing.T) {
	for _, edit := range []func(map[string][]byte){
		func(f map[string][]byte) {
			f["05_routing.json"] = []byte(strings.Replace(string(f["05_routing.json"]), `"balancerTag":"vpn"`, `"balancerTag":"other"`, 1))
		},
		func(f map[string][]byte) {
			f["02_dns.json"] = []byte(strings.Replace(string(f["02_dns.json"]), "https://1.1.1.1/dns-query", "https+local://1.1.1.1/dns-query", 1))
		},
	} {
		s, f, count := fixture(t)
		edit(f)
		if s.Sync(context.Background()) == nil || *count != 0 {
			t.Fatal("unsafe DNS activated")
		}
	}
}

func TestAbsentResolverLeavesNativeWorkflowsAvailable(t *testing.T) {
	s := &Service{Dir: filepath.Join(t.TempDir(), "absent"), Lease: authority.NewLease()}
	if err := s.Validate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(context.Background()); err != nil || s.Status(context.Background()).State != "unconfigured" {
		t.Fatal(err)
	}
}

func TestEffectiveUnchangedInputDoesNotRestart(t *testing.T) {
	s, files, count := fixture(t)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	files["05_routing.json"] = append([]byte(" "), files["05_routing.json"]...)
	if err := s.Sync(context.Background()); err != nil || *count != 1 {
		t.Fatal(err, *count)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, "panel-manifest.json"))
	var m manifest
	_ = json.Unmarshal(b, &m)
	if m.Sources["05_routing.json"] != hash(files["05_routing.json"]) {
		t.Fatal("input proof stale")
	}
}
func TestNativeCatchAllAndUnsafeListExpressions(t *testing.T) {
	s, files, _ := fixture(t)
	files["05_routing.json"] = []byte(strings.Replace(string(files["05_routing.json"]), `{"type":"field","outboundTag":"direct"}`, `{"type":"field","balancerTag":"vpn"}`, 1))
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	var root struct{ Plugins []plugin }
	_ = json.Unmarshal(b, &root)
	for _, p := range root.Plugins {
		if p.Tag == "main" {
			var seq []struct{ Exec string }
			_ = json.Unmarshal(p.Args, &seq)
			if seq[len(seq)-1].Exec != "goto vpn_only" {
				t.Fatal(seq)
			}
		}
	}
	for _, value := range []string{"regexp:ads#ignored", "regexp:with space", "full:x\ny"} {
		if validExpression(value) {
			t.Fatal(value)
		}
	}
}
func TestPresentUnsafeDNSMustNotBecomeOptional(t *testing.T) {
	s, files, _ := fixture(t)
	if err := os.Chmod(s.Dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(context.Background(), files); err == nil {
		t.Fatal("unsafe directory accepted")
	}
	if err := s.Sync(context.Background()); err == nil {
		t.Fatal("unsafe service accepted")
	}
}

func TestStockTransparentLANScopeAndRuleMetadata(t *testing.T) {
	s, files, count := fixture(t)
	files["03_inbounds.json"] = []byte(`{"inbounds":[{"tag":"redirect","protocol":"tunnel","settings":{"followRedirect":true}},{"tag":"tproxy","protocol":"tunnel","settings":{"followRedirect":true},"streamSettings":{"sockopt":{"tproxy":"tproxy"}}},{"tag":"dns","listen":"127.0.0.1","port":5310,"protocol":"socks"}]}`)
	var root map[string]any
	_ = json.Unmarshal(files["05_routing.json"], &root)
	rules := root["routing"].(map[string]any)["rules"].([]any)
	for i, v := range rules {
		r := v.(map[string]any)
		r["ruleTag"] = "synthetic-rule"
		if i > 0 {
			r["inboundTag"] = []string{"redirect", "tproxy"}
		}
	}
	rules[len(rules)-1].(map[string]any)["network"] = "tcp,udp"
	files["05_routing.json"], _ = json.Marshal(root)
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := s.Status(context.Background())
	if status.Entries < 4 || status.Skipped != 2 || *count != 1 {
		t.Fatal(status, *count)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	var config struct{ Plugins []plugin }
	_ = json.Unmarshal(b, &config)
	for _, v := range config.Plugins {
		if v.Tag == "main" {
			var seq []struct{ Exec string }
			_ = json.Unmarshal(v.Args, &seq)
			if len(seq) != 4 || seq[0].Exec != "reject 3" || seq[1].Exec != "goto vpn_only" {
				t.Fatal(seq)
			}
		}
	}
}
func TestUnrecognizedInboundScopeCannotEraseDomainPolicy(t *testing.T) {
	s, files, count := fixture(t)
	files["05_routing.json"] = []byte(`{"routing":{"rules":[{"inboundTag":["dns"],"balancerTag":"vpn"},{"inboundTag":["unknown-lan"],"domain":["domain:example.test"],"balancerTag":"vpn"},{"outboundTag":"direct"}]}}`)
	before, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if s.Sync(context.Background()) == nil {
		t.Fatal("unsupported scope silently became DIRECT")
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if *count != 0 || string(before) != string(after) {
		t.Fatal("running DNS policy was changed")
	}
}
