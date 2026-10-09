package setup

import (
	"encoding/json"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"strings"
	"testing"
)

func TestFreshSchemaDoesNotReinterpretLegacyDNSProof(t *testing.T) {
	r := Receipt{Schema: 1, Release: buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("a", 40), Channel: "stable"}, Phase: "completed", PolicyID: "Policy1", PolicyMark: 1, ProfileID: "1", Generation: strings.Repeat("b", 64), PrerequisitesSaved: true, FirmwareSaved: true, RuntimeVerified: true, StartupVerified: true}
	if !r.valid() {
		t.Fatal("legacy completed rejected")
	}
	r.ProfileID = ""
	if r.valid() {
		t.Fatal("legacy DNS proof skipped")
	}
	r.Schema = 2
	if !r.valid() {
		t.Fatal("DNS-independent completed rejected")
	}
	r.ProfileID = "1"
	if r.valid() {
		t.Fatal("mixed generation admitted")
	}
	r.ProfileID = ""
	r.Phase = "dns"
	if r.valid() {
		t.Fatal("schema2 phantom DNS phase")
	}
}

func TestReceiptDecodeKeepsSchemaProofBoundaries(t *testing.T) {
	base := Receipt{Schema: 1, Release: buildinfo.Info{Product: "xkeen-control", Version: "1.0.0", SourceCommit: strings.Repeat("a", 40), Channel: "stable"}, Phase: "completed", PolicyID: "Policy1", PolicyMark: 1, ProfileID: "1", Generation: strings.Repeat("b", 64), PrerequisitesSaved: true, FirmwareSaved: true, RuntimeVerified: true, StartupVerified: true}
	for _, schema := range []int{1, 2} {
		r := base
		r.Schema = schema
		if schema == 2 {
			r.ProfileID = ""
		}
		b, _ := json.Marshal(r)
		if got, e := decodeReceipt(b); e != nil || got.Schema != schema {
			t.Fatal("completed decode", schema, e)
		}
		r.Phase = "activation"
		b, _ = json.Marshal(r)
		if got, e := decodeReceipt(b); e != nil || got.Phase == "completed" {
			t.Fatal("pending reinterpreted", schema, e)
		}
		r.Phase = "completed"
		r.RuntimeVerified = false
		b, _ = json.Marshal(r)
		if _, e := decodeReceipt(b); e == nil {
			t.Fatal("false completion", schema)
		}
	}
	base.Schema = 2
	b, _ := json.Marshal(base)
	if _, e := decodeReceipt(b); e == nil {
		t.Fatal("legacy profile reinterpreted")
	}
	base.ProfileID = ""
	base.Phase = "dns"
	b, _ = json.Marshal(base)
	if _, e := decodeReceipt(b); e == nil {
		t.Fatal("DNS phase admitted")
	}
}
