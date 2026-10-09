package main

import (
	"strings"
	"testing"
)

func TestRecoveryCLIHasOnlyExactActions(t *testing.T) {
	for _, args := range [][]string{{}, {"inspect", "extra"}, {"activate-current"}, {"activate-current", "--digest", strings.Repeat("g", 64)}, {"activate-current", "--digest", strings.Repeat("A", 64)}, {"shell"}, {"activate-current", "--digest", strings.Repeat("a", 64), "extra"}} {
		if validRecoveryArgs(args) {
			t.Fatal("unexpected maintenance admission", args)
		}
	}
	if !validRecoveryArgs([]string{"inspect"}) || !validRecoveryArgs([]string{"activate-current", "--digest", strings.Repeat("a", 64)}) {
		t.Fatal("valid typed command rejected")
	}
	if !validRecoveryArgs([]string{"verify-existing", "--digest", strings.Repeat("a", 64)}) || validRecoveryArgs([]string{"verify-existing", "--digest", strings.Repeat("A", 64)}) || validRecoveryArgs([]string{"verify-existing"}) || updateReadOnlyCommand([]string{"nodes", "recovery", "verify-existing", "--digest", strings.Repeat("a", 64)}) {
		t.Fatal("verification must remain exact and maintenance-only")
	}
}
