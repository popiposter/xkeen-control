package xkeen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSpeedConflictDoesNotRunOrRewriteCron(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron")
	r := Reader{CronPath: path}
	if conflict, err := r.NativeSpeedConflict(); err != nil || conflict {
		t.Fatal(conflict, err)
	}
	for _, tt := range []struct {
		text     string
		conflict bool
	}{
		{"# */30 * * * * xkeen -sbt\n", false},
		{"*/30 * * * * /opt/sbin/xkeen -sbt >/dev/null 2>&1\n", true},
		{"0 3 * * * /opt/sbin/xkeen -ug\n", false},
	} {
		if err := os.WriteFile(path, []byte(tt.text), 0600); err != nil {
			t.Fatal(err)
		}
		if conflict, err := r.NativeSpeedConflict(); err != nil || conflict != tt.conflict {
			t.Fatal(conflict, err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != tt.text {
			t.Fatal("cron changed")
		}
	}
	if err := os.WriteFile(path, make([]byte, maxCronSize+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.NativeSpeedConflict(); err == nil {
		t.Fatal("unknown cron accepted")
	}
}
