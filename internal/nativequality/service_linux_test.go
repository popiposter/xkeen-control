//go:build linux

package nativequality

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestStageUsesExistingPendingEditorPreservesOtherNativeBytes(t *testing.T) {
	dir := t.TempDir()
	validator := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	routing := `{/*retain*/"routing":{"future":9007199254740993,"rules":[],"balancers":[{"tag":"other","selector":["direct"]},{"tag":"bal-proxy","selector":["proxy-"],"fallbackTag":"blocked","strategy":{"type":"leastPing"}}]}}`
	for name, text := range map[string]string{"05_routing.json": routing, "04_outbounds.json": `{"outbounds":[{"tag":"proxy-a","protocol":"vless"},{"tag":"proxy-b","protocol":"vless"}]}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := &xkeen.ConfigEditor{Dir: dir, XrayBinary: validator, Lease: authority.NewLease(), PreviousDir: filepath.Join(t.TempDir(), "previous")}
	w, err := e.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := &Service{Editor: e, Nodes: func(context.Context) []c1.NodeState {
		return []c1.NodeState{{Tag: "proxy-a", Enabled: true}, {Tag: "proxy-b", Enabled: true}}
	}, pool: []string{"proxy-a", "proxy-b"}, status: Status{State: "completed", Digest: w.Digest}, result: c1.AdaptiveResult{Generation: 1, StartedAt: now.Add(-time.Second), CompletedAt: now, State: "completed", ShortlistCount: 2, Candidates: []c1.AdaptiveCandidateResult{{Tag: "proxy-a", Valid: true, DownloadBPS: 1e6, UploadBPS: 1e6}, {Tag: "proxy-b", Valid: true, DownloadBPS: 100e6, UploadBPS: 10e6}}}}
	digest, err := s.Stage(context.Background(), w.Digest)
	if err != nil {
		t.Fatal(err)
	}
	after, err := e.Workspace(context.Background())
	if err != nil || after.Pending == nil || after.Digest != digest {
		t.Fatalf("not ordinary pending editor change: %+v %v", after.Pending, err)
	}
	text := after.Documents["05_routing.json"].Text
	for _, keep := range []string{"/*retain*/", `"future":9007199254740993`, `"fallbackTag":"blocked"`, `{"tag":"other","selector":["direct"]}`} {
		if !strings.Contains(text, keep) {
			t.Fatalf("lost native sibling %s", keep)
		}
	}
	if !strings.Contains(text, `"type":"leastLoad"`) {
		t.Fatal("no native strategy")
	}
	if _, err := s.Stage(context.Background(), w.Digest); err == nil {
		t.Fatal("consumed proposal replayed")
	}
}
