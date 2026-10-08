//go:build linux

package setup

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestPrivateSourceNeverEchoesAndRejectsOversize(t *testing.T) {
	for _, input := range []string{fixtureURI + "\n", strings.Repeat("x", 8193) + "\n"} {
		read, write, e := os.Pipe()
		if e != nil {
			t.Fatal(e)
		}
		go func() { defer write.Close(); _, _ = write.WriteString(input) }()
		var out bytes.Buffer
		source, e := privateSource(context.Background(), read, &out)
		read.Close()
		if len(input) < 8193 {
			if e != nil || source != fixtureURI {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("oversize private input")
		}
		if strings.Contains(out.String(), "vless://") || strings.Contains(out.String(), "edge.example.com") {
			t.Fatal("private input echoed")
		}
	}
}
