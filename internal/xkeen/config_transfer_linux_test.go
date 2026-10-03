//go:build linux

package xkeen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
)

func transferEditor(t *testing.T) *ConfigEditor {
	t.Helper()
	e := editorFixture(t, "[ ! -e \"$4/nodes.registry\" ] || exit 8\nexit 0\n")
	e.RegistryPath = filepath.Join(t.TempDir(), "secrets", "nodes.json")
	e.Lease = authority.NewLease()
	return e
}

func TestNativeTransferStagesOneGenerationAndDiscardRestoresAbsence(t *testing.T) {
	e := transferEditor(t)
	ctx := context.Background()
	before, err := e.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"04_outbounds.json":   []byte(`{"outbounds":[{"tag":"direct","protocol":"freedom"}]}`),
		"07_observatory.json": []byte(`{"observatory":{"subjectSelector":["proxy-"]}}`),
	}
	registry := []byte(`{"schemaVersion":1,"nodes":[]}`)
	lease, err := e.Lease.Acquire(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := e.StageTransferUnderLease(ctx, before.Digest, files, registry)
	lease()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(e.RegistryPath)
	if err != nil || string(got) != string(registry) {
		t.Fatal("registry was not saved with configs", err)
	}
	pending, err := e.readPending()
	if err != nil || pending == nil || len(pending.Original) != 3 || pending.Expected != digest {
		t.Fatal("transfer has separate or incomplete history", err)
	}
	if _, err := e.RestoreSaved(ctx, digest); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{e.RegistryPath, filepath.Join(e.Dir, "07_observatory.json")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("discard failed to restore original absence", err)
		}
	}
	after, err := e.Snapshot(ctx)
	if err != nil || after.Digest != before.Digest {
		t.Fatal("discard lost original native bytes", err)
	}
	if pending, err := e.readPending(); err != nil || pending != nil {
		t.Fatal("discard left a pending transfer", err)
	}
}

func TestNativeTransferDriftAndInvalidCandidateNeverOverwriteRegistry(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		e := transferEditor(t)
		ctx := context.Background()
		before, _ := e.Snapshot(ctx)
		files := map[string][]byte{"02_dns.json": []byte(`{"dns":{"servers":[]}}`)}
		if invalid {
			if err := os.WriteFile(e.XrayBinary, []byte("#!/bin/sh\nexit 5\n"), 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(filepath.Join(e.Dir, "02_dns.json"), []byte(`{"dns":{"servers":["localhost"]}}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := e.StageTransferUnderLease(ctx, before.Digest, files, []byte(`{"schemaVersion":1,"nodes":[]}`)); err == nil {
			t.Fatal("unsafe transfer staged")
		}
		if _, err := os.Lstat(e.RegistryPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected transfer changed registry", err)
		}
		if pending, err := e.readPending(); err != nil || pending != nil {
			t.Fatal("rejected transfer left history", err)
		}
	}
}

func TestTransferSidecarDoesNotOpenPublicRawOutboundsOrRegistryEditing(t *testing.T) {
	e := transferEditor(t)
	ctx := context.Background()
	before, _ := e.Snapshot(ctx)
	for _, id := range []string{"04_outbounds.json", registryConfigID, "../xkeen"} {
		if _, err := e.SaveTexts(ctx, before.Digest, map[string]string{id: `{}`}); err == nil {
			t.Fatal("public editor accepted transfer-only ID", id)
		}
	}
	workspace, err := e.Workspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := workspace.Documents[registryConfigID]; ok {
		t.Fatal("registry exposed as raw editor document")
	}
}
