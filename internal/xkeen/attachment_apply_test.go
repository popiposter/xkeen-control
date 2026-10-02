package xkeen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func attachmentFixture(t *testing.T) (Discovery, string, map[string][]byte) {
	t.Helper()
	d := nativeFixture(t)
	for name, data := range nativeTemplates(t) {
		writeNativeFixture(t, d, "opt/etc/xray/configs/"+name, string(data))
	}
	writeNativeFixture(t, d, "proc/1/comm", "init\n")
	files, digest, err := d.attachmentSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return d, digest, files
}

func acceptAttachment(context.Context, string) error { return nil }

func assertAttachmentOriginal(t *testing.T, d Discovery, expected string) {
	t.Helper()
	_, digest, err := d.attachmentSource(context.Background())
	if err != nil || digest != expected {
		t.Fatalf("active configuration changed: err=%v", err)
	}
}

func TestAttachStoppedCommitsWithDurableSnapshotAndRefusesReplay(t *testing.T) {
	d, digest, files := attachmentFixture(t)
	result, err := d.attachStopped(context.Background(), digest, acceptAttachment)
	if err != nil || !result.Validated {
		t.Fatalf("attach: %v", err)
	}
	changes, _ := BuildAttachment(files)
	for name, data := range changes {
		got, err := os.ReadFile(d.path("opt/etc/xray/configs/" + name))
		if err != nil || !bytes.Equal(data, got) {
			t.Fatalf("candidate mismatch: %s", name)
		}
	}
	for name, data := range files {
		got, err := os.ReadFile(d.path("opt/etc/xkeen-control/state/native-attachment/" + name))
		if err != nil || !bytes.Equal(data, got) {
			t.Fatalf("snapshot mismatch: %s", name)
		}
	}
	raw, _ := os.ReadFile(d.path("opt/etc/xkeen-control/state/native-attachment/receipt.json"))
	var receipt struct {
		State string
		Check AttachmentCheck
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.State != "committed-stopped" || receipt.Check.SourceSHA256 != digest {
		t.Fatal("missing committed receipt")
	}
	_, after, _ := d.attachmentSource(context.Background())
	if _, err := d.attachStopped(context.Background(), digest, acceptAttachment); err == nil {
		t.Fatal("replayed attachment")
	}
	assertAttachmentOriginal(t, d, after)
}

func TestAttachStoppedRejectsUnprovenPreconditionsWithoutActiveWrites(t *testing.T) {
	for _, scenario := range []string{"stale", "running", "missing-proc", "unreadable-pid", "existing-receipt", "process-starts-during-check", "unsafe-state-directory"} {
		t.Run(scenario, func(t *testing.T) {
			d, digest, _ := attachmentFixture(t)
			expected := digest
			validate := acceptAttachment
			switch scenario {
			case "stale":
				expected = strings.Repeat("0", 64)
			case "running":
				writeNativeFixture(t, d, "proc/2/comm", "xray\n")
			case "missing-proc":
				if err := os.RemoveAll(d.path("proc")); err != nil {
					t.Fatal(err)
				}
			case "unreadable-pid":
				if err := os.MkdirAll(d.path("proc/2/comm"), 0700); err != nil {
					t.Fatal(err)
				}
			case "existing-receipt":
				if err := os.MkdirAll(d.path("opt/etc/xkeen-control/state/native-attachment"), 0700); err != nil {
					t.Fatal(err)
				}
			case "unsafe-state-directory":
				if err := os.Symlink(t.TempDir(), d.path("opt/etc/xkeen-control")); err != nil {
					t.Fatal(err)
				}
			case "process-starts-during-check":
				validate = func(context.Context, string) error { writeNativeFixture(t, d, "proc/2/comm", "xray\n"); return nil }
			}
			if _, err := d.attachStopped(context.Background(), expected, validate); err == nil {
				t.Fatal("accepted uncertain precondition")
			}
			assertAttachmentOriginal(t, d, digest)
		})
	}
}

func TestAttachStoppedWriteFailureRestoresOnlyOwnedFilesAndRetainsReceipt(t *testing.T) {
	for _, scenario := range []string{"snapshot", "first-active", "third-active", "receipt", "rollback-drift", "process-starts-after-write"} {
		t.Run(scenario, func(t *testing.T) {
			d, digest, _ := attachmentFixture(t)
			activeWrites, receipts := 0, 0
			writer := func(path string, data []byte) error {
				active := filepath.Dir(path) == d.path("opt/etc/xray/configs")
				if filepath.Base(path) == "receipt.json" {
					receipts++
				}
				if scenario == "snapshot" && !active && filepath.Base(path) != "receipt.json" {
					return errors.New("injected snapshot failure")
				}
				if scenario == "receipt" && receipts == 2 {
					return errors.New("injected receipt failure")
				}
				if err := writeAttachmentFile(path, data); err != nil {
					return err
				}
				if active {
					activeWrites++
					if scenario == "rollback-drift" {
						writeNativeFixture(t, d, "opt/etc/xray/configs/04_outbounds.json", "{}\n")
						return errors.New("injected drift")
					}
					if scenario == "process-starts-after-write" {
						writeNativeFixture(t, d, "proc/2/comm", "xray\n")
						return errors.New("process appeared")
					}
					if scenario == "first-active" && activeWrites == 1 || scenario == "third-active" && activeWrites == 3 {
						return errors.New("injected interrupted write")
					}
				}
				return nil
			}
			if _, err := d.attachStoppedWithWriter(context.Background(), digest, acceptAttachment, writer); !errors.Is(err, ErrAttachmentRecovery) {
				t.Fatalf("result: %v", err)
			}
			if scenario == "rollback-drift" {
				got, _ := os.ReadFile(d.path("opt/etc/xray/configs/04_outbounds.json"))
				if string(got) != "{}\n" {
					t.Fatal("overwrote external drift")
				}
			} else if scenario == "process-starts-after-write" {
				_, current, _ := d.attachmentSource(context.Background())
				if current == digest {
					t.Fatal("rolled back while process running")
				}
			} else {
				assertAttachmentOriginal(t, d, digest)
			}
			if _, err := os.Stat(d.path("opt/etc/xkeen-control/state/native-attachment/receipt.json")); err != nil {
				t.Fatal("lost recovery receipt")
			}
			if _, err := d.attachStopped(context.Background(), digest, acceptAttachment); err == nil {
				t.Fatal("replayed failed attachment")
			}
		})
	}
}
