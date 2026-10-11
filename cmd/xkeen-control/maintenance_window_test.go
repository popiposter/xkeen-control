package main

import (
	"testing"
	"time"
)

func TestMaintenanceWindowAndOffsetParsing(t *testing.T) {
	if d, ok := maintenanceWindow("04:00"); !ok || d != 4*time.Hour {
		t.Fatal("04:00", d, ok)
	}
	if d, ok := maintenanceWindow("23:45"); !ok || d != 23*time.Hour+45*time.Minute {
		t.Fatal("23:45", d, ok)
	}
	for _, bad := range []string{"", "4:00", "24:00", "04:60", "04-00", "aa:bb"} {
		if _, ok := maintenanceWindow(bad); ok {
			t.Fatal("accepted", bad)
		}
	}
	if loc, ok := parseUTCOffset("+0300"); !ok || time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Unix() != time.Date(2025, 12, 31, 21, 0, 0, 0, time.UTC).Unix() {
		t.Fatal("+0300")
	}
	if loc, ok := parseUTCOffset("-0530"); !ok || time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Unix() != time.Date(2026, 1, 1, 5, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("-0530")
	}
	for _, bad := range []string{"", "0300", "+03:00", "+1500", "+03x0"} {
		if _, ok := parseUTCOffset(bad); ok {
			t.Fatal("accepted", bad)
		}
	}
}
