package xkeen

import (
	"strings"
	"testing"
)

func TestEntwarePathOverridesBusyBoxAndKeepsOperatorDirectories(t *testing.T) {
	for _, test := range []struct {
		name string
		env  []string
		want string
	}{
		{"firmware first", []string{"PATH=/opt/usr/bin:/opt/sbin:/opt/bin:/bin:/tools"}, "/opt/bin:/opt/sbin:/opt/usr/bin:/bin:/tools"},
		{"missing", nil, "/opt/bin:/opt/sbin:/usr/sbin:/usr/bin:/sbin:/bin"},
		{"empty", []string{"PATH="}, "/opt/bin:/opt/sbin:/usr/sbin:/usr/bin:/sbin:/bin"},
		{"duplicates and cwd", []string{"PATH=:/bin:/tools:/bin::/opt/bin:"}, "/opt/bin:/opt/sbin:/bin:/tools"},
		{"last assignment", []string{"PATH=/obsolete", "PATH=/bin"}, "/opt/bin:/opt/sbin:/bin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := append(append([]string{}, test.env...), "TERM=xterm", "NATIVE_SETTING=preserved")
			output := withEntwarePath(input)
			count := 0
			for _, entry := range output {
				if strings.HasPrefix(entry, "PATH=") {
					count++
					if entry != "PATH="+test.want {
						t.Fatalf("unexpected tool search path %q", entry)
					}
				}
			}
			if count != 1 || !containsEntry(output, "TERM=xterm") || !containsEntry(output, "NATIVE_SETTING=preserved") {
				t.Fatal("environment authority was lost or duplicated")
			}
		})
	}
}

func containsEntry(env []string, entry string) bool {
	for _, value := range env {
		if value == entry {
			return true
		}
	}
	return false
}
