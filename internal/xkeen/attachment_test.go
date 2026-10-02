package xkeen

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/configjson"
)

func nativeTemplates(t *testing.T) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	paths, err := filepath.Glob("testdata/native-beta/*.json")
	if err != nil || len(paths) != 6 {
		t.Fatal("native fixture missing")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(path)] = data
	}
	return files
}

func TestNativeAttachmentPreservesInstallerPolicyAndAddsOnlyIntegration(t *testing.T) {
	files := nativeTemplates(t)
	changes, err := BuildAttachment(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 4 {
		t.Fatalf("changed %d files", len(changes))
	}
	for _, name := range []string{"01_log.json", "02_dns.json", "03_inbounds.json", "06_policy.json"} {
		if _, exists := changes[name]; exists {
			t.Fatalf("rewrote native %s", name)
		}
	}
	d := nativeFixture(t)
	for name, data := range files {
		writeNativeFixture(t, d, "opt/etc/xray/configs/"+name, string(data))
	}
	for name, data := range changes {
		writeNativeFixture(t, d, "opt/etc/xray/configs/"+name, string(data))
	}
	if got := d.Inspect(context.Background()); got.PanelIntegration != CapabilityAvailable || !got.APIConfigured || !got.PoolConfigured {
		t.Fatalf("candidate integration not discoverable: %+v", got)
	}
	var document struct {
		Routing struct{ Rules []map[string]json.RawMessage }
	}
	if configjson.Decode(changes["05_routing.json"], &document) != nil || len(document.Routing.Rules) != 3 {
		t.Fatal("native rules lost")
	}
	if string(document.Routing.Rules[1]["balancerTag"]) != `"bal-proxy"` || string(document.Routing.Rules[2]["outboundTag"]) != `"direct"` {
		t.Fatal("wrong native placeholder transformation")
	}
	var outbounds struct{ Outbounds []struct{ Tag string } }
	_ = json.Unmarshal(changes["04_outbounds.json"], &outbounds)
	if len(outbounds.Outbounds) != 2 || outbounds.Outbounds[0].Tag != "direct" || outbounds.Outbounds[1].Tag != "block" {
		t.Fatal("invalid empty native outbounds")
	}
	for name, data := range changes {
		files[name] = data
	}
	if _, err := BuildAttachment(files); err == nil {
		t.Fatal("attachment replay accepted")
	}
}

func TestNativeAttachmentKeepsConfiguredOutboundsAndUnknownFields(t *testing.T) {
	files := nativeTemplates(t)
	files["04_outbounds.json"] = []byte(`{"future":{"number":9007199254740993},"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"domainStrategy":"UseIP"}}]}`)
	original := append([]byte(nil), files["04_outbounds.json"]...)
	changes, err := BuildAttachment(files)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, files["04_outbounds.json"]) || !bytes.Contains(changes["04_outbounds.json"], []byte("9007199254740993")) || !bytes.Contains(changes["04_outbounds.json"], []byte("UseIP")) || !bytes.Contains(changes["05_routing.json"], []byte(`"vless-reality"`)) {
		t.Fatal("overwrote existing native configuration")
	}
}

func TestNativeAttachmentRejectsAmbiguousOrConflictingIntegration(t *testing.T) {
	for _, test := range []struct{ name, file, data string }{
		{"api", "09_external.json", `{"api":{}}`},
		{"observatory", "09_external.json", `{"observatory":{}}`},
		{"overlap", "09_external.json", `{"routing":{"rules":[]}}`},
		{"listener", "09_external.json", `{"inbounds":[{"tag":"other","port":10808}]}`},
		{"reserved-tag", "09_external.json", `{"inbounds":[{"tag":"probe","port":1234}]}`},
		{"fixed-conflict", "04_outbounds.json", `{"outbounds":[{"tag":"block","protocol":"freedom"}]}`},
		{"managed-namespace", "04_outbounds.json", `{"outbounds":[{"tag":"proxy-node-external","protocol":"freedom"}]}`},
		{"other-outbound-file", "09_external.json", `{"outbounds":[{"tag":"external","protocol":"freedom"}]}`},
		{"catchall-shadow", "05_routing.json", `{"routing":{"rules":[{"outboundTag":"direct"}]}}`},
		{"ambiguous-placeholder", "05_routing.json", `{"routing":{"rules":[{"inboundTag":["custom"],"outboundTag":"vless-reality"}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := nativeTemplates(t)
			files[test.file] = []byte(test.data)
			if _, err := BuildAttachment(files); err == nil {
				t.Fatal("conflicting attachment accepted")
			}
		})
	}
}

func TestNativeAttachmentCheckIsTemporaryAndDetectsConcurrentEdit(t *testing.T) {
	for _, drift := range []bool{false, true} {
		d := nativeFixture(t)
		files := nativeTemplates(t)
		for name, data := range files {
			writeNativeFixture(t, d, "opt/etc/xray/configs/"+name, string(data))
		}
		var candidateDir string
		result, err := d.checkAttachment(context.Background(), func(ctx context.Context, dir string) error {
			candidateDir = dir
			if _, err := os.Stat(filepath.Join(dir, "08_api.json")); err != nil {
				t.Fatal("missing integration in candidate")
			}
			if data, err := os.ReadFile(filepath.Join(dir, "03_inbounds.json")); err != nil || !bytes.Equal(data, files["03_inbounds.json"]) {
				t.Fatal("native inbounds not preserved")
			}
			if drift {
				writeNativeFixture(t, d, "opt/etc/xray/configs/02_dns.json", "{}")
			}
			return nil
		})
		if drift {
			if err == nil || result.Validated {
				t.Fatal("accepted stale candidate")
			}
		} else if err != nil || !result.Validated || len(result.Changes) != 4 {
			t.Fatalf("candidate validation: %+v %v", result, err)
		}
		if _, err := os.Stat(candidateDir); !os.IsNotExist(err) {
			t.Fatal("temporary candidate not removed")
		}
		if _, err := os.Stat(d.path("opt/etc/xray/configs/08_api.json")); !os.IsNotExist(err) {
			t.Fatal("check activated integration")
		}
	}
}
