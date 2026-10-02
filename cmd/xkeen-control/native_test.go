package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestNativeInspectionCLIIsReadOnlyAndExact(t *testing.T) {
	var output bytes.Buffer
	d := xkeen.Discovery{Root: t.TempDir()}
	if err := runNativeCommand([]string{"inspect", "--json"}, &output, d); err != nil {
		t.Fatal(err)
	}
	var facts xkeen.Capabilities
	if err := json.Unmarshal(output.Bytes(), &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Installation != xkeen.CapabilityMissing {
		t.Fatalf("facts=%+v", facts)
	}
	for _, args := range [][]string{{}, {"restart"}, {"inspect"}, {"inspect", "--json", "--root=/tmp"}} {
		output.Reset()
		if err := runNativeCommand(args, &output, d); err == nil || output.Len() != 0 {
			t.Fatalf("accepted argv %v", args)
		}
	}
}
