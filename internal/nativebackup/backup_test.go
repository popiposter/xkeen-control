package nativebackup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

type source struct {
	registry nodes.Registry
	onRead   func()
	snapshot func() (nodes.Registry, string, error)
}

func (s source) NativeSnapshotUnderLease(context.Context) (nodes.Registry, string, error) {
	if s.snapshot != nil {
		return s.snapshot()
	}
	if s.onRead != nil {
		s.onRead()
	}
	return s.registry, "synthetic-stable", nil
}

func nativeService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range fixture(t).Files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	lease := authority.NewLease()
	editor := &xkeen.ConfigEditor{Dir: dir, PreviousDir: filepath.Join(t.TempDir(), "history"), Lease: lease}
	registryPath := filepath.Join(t.TempDir(), "secrets", "nodes.json")
	manager := nodes.NewManager(nodes.Config{Store: nodes.Store{Path: registryPath}, Authority: lease})
	return &Service{Editor: editor, Nodes: manager, Lease: lease}, registryPath
}

func TestNativeOnlyExportDoesNotCreateRegistryAndDetectsAppearance(t *testing.T) {
	service, path := nativeService(t)
	if _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only export created registry", err)
	}
	manager := service.Nodes
	reads := 0
	service.Nodes = source{snapshot: func() (nodes.Registry, string, error) {
		registry, digest, err := manager.NativeSnapshotUnderLease(context.Background())
		reads++
		if reads == 1 {
			if err := (nodes.Store{Path: path}).Save(nodes.NewRegistry()); err != nil {
				t.Fatal(err)
			}
		}
		return registry, digest, err
	}}
	if _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); !errors.Is(err, backup.ErrUnavailable) {
		t.Fatal("registry appearance was missed", err)
	}
	service.Nodes = manager
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); !errors.Is(err, backup.ErrUnavailable) {
		t.Fatal("corrupt registry treated as absent", err)
	}
}

func TestNativeExportBusyBeforePrivateSnapshot(t *testing.T) {
	service, _ := nativeService(t)
	entered, unblock := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	first := true
	service.Nodes = source{registry: nodes.NewRegistry(), onRead: func() {
		if first {
			first = false
			close(entered)
			<-unblock
		}
	}}
	go func() { _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); finished <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first export did not reach snapshot")
	}
	_, err := service.ExportSecret(context.Background(), "synthetic passphrase")
	close(unblock)
	if !errors.Is(err, backup.ErrBusy) {
		t.Fatal("busy export took a second private snapshot", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) Bundle {
	t.Helper()
	registry, err := nodes.MarshalCanonical(nodes.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	return Bundle{Format: Format, Version: Version, ExportedAt: time.Now().UTC().Format(time.RFC3339Nano), Registry: registry,
		Files: map[string][]byte{"04_outbounds.json": []byte("// preserved comment\n{\"extension\":9007199254740993,\"outbounds\":[{\"tag\":\"direct\",\"protocol\":\"freedom\"}]}"), "02_dns.json": []byte(`{"dns":{"servers":["localhost"]}}`)}}
}

func TestNativeArchiveRoundtripPreservesBytesAndRejectsWrongPassword(t *testing.T) {
	want := fixture(t)
	plaintext, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := backup.SealPayload(plaintext, "synthetic passphrase")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(archive, "synthetic passphrase")
	if err != nil || !reflect.DeepEqual(got.Files, want.Files) || got.Format != want.Format || got.Version != want.Version || got.ExportedAt != want.ExportedAt {
		t.Fatal("lossless roundtrip failed", err)
	}
	if _, err := Open(archive, "different passphrase"); !errors.Is(err, backup.ErrDecryptionFailed) {
		t.Fatal(err)
	}
}

func TestNativeArchiveRejectsExecutablePathsAndRegistryMismatch(t *testing.T) {
	for _, name := range []string{"../xkeen", "S05xkeen", "auth/password.bcrypt"} {
		bundle := fixture(t)
		bundle.Files[name] = []byte(`{}`)
		data, _ := json.Marshal(bundle)
		if _, err := Parse(data); err == nil {
			t.Fatal("accepted non-config path", name)
		}
	}
	bundle := fixture(t)
	node, err := nodes.NewNodeWithID(nodes.VLESS{
		UUID: "11111111-1111-4111-8111-111111111111", Host: "edge.example.com", Port: 443,
		Encryption: "none", Flow: "xtls-rprx-vision", Security: "reality", ServerName: "front.example.com",
		Fingerprint: "chrome", PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ShortID: "abcd", Network: "tcp",
	}, "Synthetic", nodes.Source{Type: "manual"}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	registry := nodes.NewRegistry()
	registry.Nodes = []nodes.Node{node}
	bundle.Registry, _ = nodes.MarshalCanonical(registry)
	data, _ := json.Marshal(bundle)
	if _, err := Parse(data); err == nil {
		t.Fatal("accepted missing managed runtime profile")
	}
}

func TestNativeExportDetectsExternalConfigDriftAndPendingChanges(t *testing.T) {
	bundle := fixture(t)
	dir := t.TempDir()
	for name, data := range bundle.Files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	history := filepath.Join(t.TempDir(), "history")
	editor := &xkeen.ConfigEditor{Dir: dir, PreviousDir: history}
	service := &Service{Editor: editor, Nodes: source{registry: nodes.NewRegistry()}, Lease: authority.NewLease()}
	if _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); err != nil {
		t.Fatal(err)
	}
	service.Nodes = source{registry: nodes.NewRegistry(), onRead: func() { _ = os.WriteFile(filepath.Join(dir, "02_dns.json"), []byte(`{"dns":{"servers":[]}}`), 0600) }}
	if _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); !errors.Is(err, backup.ErrUnavailable) {
		t.Fatal("exported incoherent set", err)
	}
	if err := os.MkdirAll(history, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(history, "pending.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ExportSecret(context.Background(), "synthetic passphrase"); !errors.Is(err, backup.ErrUnavailable) {
		t.Fatal("ignored invalid pending history", err)
	}
}
