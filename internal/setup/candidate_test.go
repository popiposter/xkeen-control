package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"os"
	"path/filepath"
	"testing"
)

const fixtureURI = "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?encryption=none&security=reality&sni=front.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&type=tcp#Synthetic"

func templates(t *testing.T) map[string][]byte {
	t.Helper()
	paths, e := filepath.Glob("../xkeen/testdata/native-beta/*.json")
	if e != nil || len(paths) != 6 {
		t.Fatal("fixture missing")
	}
	out := map[string][]byte{}
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		name := filepath.Base(p)
		if name == "native-outbounds.json" {
			name = "04_outbounds.json"
		}
		out[name] = b
	}
	return out
}
func TestCandidateIsOneCompletePrivateGeneration(t *testing.T) {
	base := templates(t)
	base["01_log.json"] = []byte(`{"log":{"loglevel":"warning"},"future":9007199254740993}`)
	r, e := nodes.PrepareInitialSource(context.Background(), fixtureURI, nil)
	if e != nil {
		t.Fatal(e)
	}
	files, registry, e := Candidate(base, r)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(base["01_log.json"], files["01_log.json"]) || !bytes.Equal(base["06_policy.json"], files["06_policy.json"]) {
		t.Fatal("unrelated policy changed")
	}
	if _, e := nodes.ParseCanonical(registry); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(files["04_outbounds.json"], []byte(r.Nodes[0].OutboundTag)) {
		t.Fatal("registry/outbound mismatch")
	}
	var route struct {
		Routing struct {
			Rules []struct {
				Inbound  []string `json:"inboundTag"`
				Protocol []string
				Outbound string `json:"outboundTag"`
				Balancer string `json:"balancerTag"`
			}
		}
	}
	if json.Unmarshal(files["05_routing.json"], &route) != nil {
		t.Fatal("routing")
	}
	if route.Routing.Rules[0].Outbound != "api" || route.Routing.Rules[1].Balancer != "bal-proxy" {
		t.Fatal("integration not first")
	}
	torrent := false
	for _, rule := range route.Routing.Rules {
		for _, p := range rule.Protocol {
			if p == "bittorrent" {
				torrent = rule.Outbound == "direct"
			}
		}
	}
	if !torrent {
		t.Fatal("torrent direct missing")
	}
	if _, _, e := Candidate(files, r); e == nil {
		t.Fatal("setup candidate replay accepted")
	}
}
func TestCandidateRejectsEmptyOrAmbiguousDNSListener(t *testing.T) {
	if _, _, e := Candidate(templates(t), nodes.NewRegistry()); e == nil {
		t.Fatal("empty registry")
	}
	r, _ := nodes.PrepareInitialSource(context.Background(), fixtureURI, nil)
	base := templates(t)
	base["03_inbounds.json"] = []byte(`{"inbounds":[{"tag":"collision","port":5310,"protocol":"socks"}]}`)
	if _, _, e := Candidate(base, r); e == nil {
		t.Fatal("DNS listener conflict admitted")
	}
}
