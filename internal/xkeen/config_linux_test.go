//go:build linux

package xkeen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeDraftPrivateAndDoesNotWriteNativeConfig(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	e.DraftDir = filepath.Join(t.TempDir(), "drafts")
	before, _ := e.Snapshot(context.Background())
	if err := e.SaveDraft(context.Background(), "02_dns.json", "{ invalid draft", false); err != nil {
		t.Fatal(err)
	}
	w, err := e.Workspace(context.Background())
	if err != nil || w.Digest != before.Digest || w.Documents["02_dns.json"].Draft == nil || *w.Documents["02_dns.json"].Draft != "{ invalid draft" {
		t.Fatal("draft changed native state or missing", err)
	}
	if _, ok := w.Documents["04_outbounds.json"]; ok {
		t.Fatal("generated secret outbounds exposed by editor")
	}
	info, _ := os.Stat(filepath.Join(e.DraftDir, "02_dns.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("draft permissions")
	}
	if e.SaveDraft(context.Background(), "../../xkeen", "test", false) == nil {
		t.Fatal("arbitrary path accepted")
	}
	if err := e.SaveDraft(context.Background(), "02_dns.json", "", true); err != nil {
		t.Fatal(err)
	}
	w, err = e.Workspace(context.Background())
	if err != nil || w.Documents["02_dns.json"].Draft != nil {
		t.Fatal("draft retained", err)
	}
}

func TestNativePendingRetainsFirstOriginalAcrossSavesAndDetectsExternalDrift(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	before, _ := e.Snapshot(context.Background())
	digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
	if err != nil {
		t.Fatal(err)
	}
	digest, err = e.SaveField(context.Background(), digest, "dns", "disableCache", true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.SaveField(context.Background(), digest, "routing", "domainStrategy", "IPOnDemand")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := e.readPending()
	if err != nil || len(pending.Original) != 2 || string(pending.Original["02_dns.json"]) != string(before.files["02_dns.json"]) {
		t.Fatal("lost pre-apply generation", err)
	}
	w, err := e.Workspace(context.Background())
	if err != nil || w.Pending == nil || w.Pending.Drift || len(w.Pending.Files) != 2 {
		t.Fatal("pending set not visible", err)
	}
	if err := os.WriteFile(filepath.Join(e.Dir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	w, err = e.Workspace(context.Background())
	if err != nil || w.Pending == nil || !w.Pending.Drift {
		t.Fatal("external drift invisible", err)
	}
	if _, err := e.SaveField(context.Background(), w.Digest, "dns", "disableFallback", true); err == nil {
		t.Fatal("overwrote drift while pending")
	}
}

func TestNativePendingRoundTripsEscapingExpansionWithoutUnreadableRecord(t *testing.T) {
	e := editorFixture(t, "exit 0\n")
	// Three 1MiB originals containing comment control bytes exceed 16MiB when
	// serialized as escaped strings. Lossless base64 keeps the record bounded.
	original := "{/*" + strings.Repeat("\x01", 1<<20) + "*/}"
	for _, name := range []string{"01_log.json", "02_dns.json", "05_routing.json"} {
		if err := os.WriteFile(filepath.Join(e.Dir, name), []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	digest := before.Digest
	for _, name := range []string{"01_log.json", "02_dns.json", "05_routing.json"} {
		digest, err = e.SaveText(context.Background(), digest, name, "{}")
		if err != nil {
			t.Fatal("unreadable expanding snapshot", err)
		}
	}
	pending, err := e.readPending()
	if err != nil || len(pending.Original) != 3 || string(pending.Original["01_log.json"]) != original {
		t.Fatal("lost original bytes", err)
	}
}

func TestNativeTextSaveValidatesCombinedCandidateAndReturnsPrivateDiagnostics(t *testing.T) {
	e := editorFixture(t, "echo 'synthetic validator detail' >&2\nexit 1\n")
	before, _ := e.Snapshot(context.Background())
	_, err := e.SaveText(context.Background(), before.Digest, "02_dns.json", `{"dns":{"queryStrategy":"UseIPv4"}}`)
	var detail *ValidationError
	if !errors.As(err, &detail) || detail.File != "02_dns.json" || !strings.Contains(detail.Output, "synthetic validator detail") {
		t.Fatal("private diagnostic absent", err)
	}
	after, _ := e.Snapshot(context.Background())
	if after.Digest != before.Digest {
		t.Fatal("invalid config written")
	}
	if _, err := e.SaveText(context.Background(), before.Digest, "04_outbounds.json", "{}"); err == nil {
		t.Fatal("generated outbounds writable")
	}
}

func editorFixture(t *testing.T, validator string) *ConfigEditor {
	t.Helper()
	dir := t.TempDir()
	xray := filepath.Join(t.TempDir(), "xray")
	if os.WriteFile(xray, []byte("#!/bin/sh\n"+validator), 0700) != nil {
		t.Fatal("validator fixture")
	}
	files := map[string]string{"02_dns.json": "{/* DNS comment */\"dns\":{\"queryStrategy\":\"UseIP\",\"future\":42}}", "05_routing.json": `{"routing":{"domainStrategy":"AsIs","rules":[]}}`, "04_outbounds.json": `{"outbounds":[{"tag":"unmanaged","protocol":"freedom","future":9007199254740993}]}`}
	for name, data := range files {
		if os.WriteFile(filepath.Join(dir, name), []byte(data), 0600) != nil {
			t.Fatal("config fixture")
		}
	}
	return &ConfigEditor{Dir: dir, XrayBinary: xray, Lease: nil, PreviousDir: filepath.Join(t.TempDir(), "private")}
}

func TestNativeConfigSavesOneFieldAndPreservesNativeBytes(t *testing.T) {
	e := editorFixture(t, "[ \"$1 $2 $3\" = 'run -test -confdir' ] || exit 9\n[ -f \"$4/04_outbounds.json\" ] || exit 8\nexit 0\n")
	before, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := e.SaveField(context.Background(), before.Digest, "dns", "queryStrategy", "UseIPv4")
	if err != nil || digest == before.Digest {
		t.Fatal(digest, err)
	}
	after, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(after.files["04_outbounds.json"]) != string(before.files["04_outbounds.json"]) || string(after.files["05_routing.json"]) != string(before.files["05_routing.json"]) {
		t.Fatal("changed unrelated native files")
	}
	if string(after.files["02_dns.json"]) != strings.Replace(string(before.files["02_dns.json"]), "UseIP", "UseIPv4", 1) {
		t.Fatal("lost native comments/unknown fields")
	}
	pending, err := e.readPending()
	if err != nil || string(pending.Original["02_dns.json"]) != string(before.files["02_dns.json"]) {
		t.Fatal("missing own rollback copy", err)
	}
	if _, err := e.SaveField(context.Background(), before.Digest, "dns", "disableCache", true); err == nil {
		t.Fatal("accepted stale baseline")
	}
}

func TestNativeConfigValidationFailureDoesNotPromote(t *testing.T) {
	e := editorFixture(t, "exit 1\n")
	before, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveField(context.Background(), before.Digest, "dns", "disableCache", true); err == nil {
		t.Fatal("invalid candidate promoted")
	}
	after, err := e.Snapshot(context.Background())
	if err != nil || after.Digest != before.Digest {
		t.Fatal("validation failure modified native state")
	}
}

func TestNativeConfigIncludesJSONCAndRefusesUnparsedNativeFormats(t *testing.T) {
	e := editorFixture(t, "[ -f \"$4/09_extra.jsonc\" ] || exit 9\nexit 0\n")
	path := filepath.Join(e.Dir, "09_extra.jsonc")
	if err := os.WriteFile(path, []byte(`{/* native sibling */"future":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveField(context.Background(), before.Digest, "dns", "disableCache", true); err != nil {
		t.Fatal("JSONC missing from candidate", err)
	}
	before, err = e.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"future":43}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveField(context.Background(), before.Digest, "dns", "disableCache", false); err == nil {
		t.Fatal("JSONC drift invisible")
	}
	if err := os.WriteFile(filepath.Join(e.Dir, "10_native.yaml"), []byte("dns: {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Snapshot(context.Background()); err == nil {
		t.Fatal("excluded live YAML config")
	}
}
