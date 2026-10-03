//go:build linux

package nativebackup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

func transferFixture(t *testing.T) (*Service, []byte) {
	t.Helper()
	service, path := nativeService(t)
	service.Editor.RegistryPath = path
	service.Editor.XrayBinary = filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(service.Editor.XrayBinary, []byte("#!/bin/sh\n[ \"$1 $2 $3\" = 'run -test -confdir' ] || exit 9\n[ ! -e \"$4/nodes.registry\" ] || exit 8\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	service.Interfaces = func() ([]string, error) { return []string{"destination0"}, nil }
	bundle := fixture(t)
	bundle.Files["04_outbounds.json"] = []byte(`{"outbounds":[{"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"interface":"source0","extension":9007199254740993}}}]}`)
	plaintext, _ := json.Marshal(bundle)
	archive, err := backup.SealPayload(plaintext, "synthetic passphrase")
	if err != nil {
		t.Fatal(err)
	}
	return service, archive
}

func TestNativeTransferPreviewMapsInterfacesAndStagesWithoutRestart(t *testing.T) {
	service, archive := transferFixture(t)
	ctx := context.Background()
	before, err := service.Editor.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := service.Preview(ctx, "owner", archive, "synthetic passphrase", nil); err != nil || !value.MappingRequired || value.Token != "" || len(value.References) != 1 {
		t.Fatal("missing interface did not produce a read-only mapping step", err)
	}
	preview, err := service.Preview(ctx, "owner", archive, "synthetic passphrase", map[string]string{"source0": "destination0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Files) != 2 || preview.Nodes != 0 || preview.Subscriptions != 0 {
		t.Fatal(preview)
	}
	if current, _ := service.Editor.Snapshot(ctx); current.Digest != before.Digest {
		t.Fatal("Preview saved data")
	}
	if _, err := os.Lstat(service.Editor.RegistryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Preview created registry", err)
	}
	service.Cancel("other-owner", preview.Token)
	if _, err := service.Stage(ctx, "other-owner", preview.Token, true); !errors.Is(err, ErrPreview) {
		t.Fatal("foreign session consumed transfer", err)
	}
	if _, err := service.Stage(ctx, "owner", preview.Token, false); !errors.Is(err, ErrPreview) {
		t.Fatal("unchecked native settings accepted", err)
	}
	digest, err := service.Stage(ctx, "owner", preview.Token, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Stage(ctx, "owner", preview.Token, true); !errors.Is(err, ErrPreview) {
		t.Fatal("Stage replayed", err)
	}
	raw, err := os.ReadFile(filepath.Join(service.Editor.Dir, "04_outbounds.json"))
	if err != nil || !strings.Contains(string(raw), "destination0") || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatal("mapping lost opaque socket data", err)
	}
	if _, err := (nodes.Store{Path: service.Editor.RegistryPath}).Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Editor.RestoreSaved(ctx, digest); err != nil {
		t.Fatal(err)
	}
	current, err := service.Editor.Snapshot(ctx)
	if err != nil || current.Digest != before.Digest {
		t.Fatal("combined transfer discard lost baseline", err)
	}
}

func TestNativeTransferExpiredOrChangedPreviewCannotStage(t *testing.T) {
	for _, expired := range []bool{true, false} {
		service, archive := transferFixture(t)
		ctx := context.Background()
		preview, err := service.Preview(ctx, "owner", archive, "synthetic passphrase", map[string]string{"source0": "destination0"})
		if err != nil {
			t.Fatal(err)
		}
		if expired {
			service.preview.expires = time.Now().Add(-time.Second)
		} else {
			if err := os.WriteFile(filepath.Join(service.Editor.Dir, "02_dns.json"), []byte(`{"dns":{"servers":[]}}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.Stage(ctx, "owner", preview.Token, true); err == nil {
			t.Fatal("stale preview staged")
		}
		if _, err := os.Lstat(service.Editor.RegistryPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected Stage wrote registry", err)
		}
		if _, err := service.Stage(ctx, "owner", preview.Token, true); !errors.Is(err, ErrPreview) {
			t.Fatal("failed Stage remained replayable", err)
		}
	}
}
