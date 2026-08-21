package xkeen

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReaderParsesBoundedRuntimeSources(t *testing.T) {
	dir := t.TempDir()
	xkeenPath := filepath.Join(dir, "xkeen.json")
	cronPath := filepath.Join(dir, "root")
	logPath := filepath.Join(dir, "speed.log")
	watchdogPath := filepath.Join(dir, "speed_failover_watchdog.sh")
	if err := os.WriteFile(xkeenPath, []byte(`{"xkeen":{"xray":{"speed_balancer":{"enabled":true,"interval":1440,"hysteresis":20,"balancer":"bal-proxy","test_url":"secret"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cronPath, []byte("17 4 * * * /opt/sbin/xkeen -sbt\n*/5 * * * * /opt/etc/xkeen/speed_failover_watchdog.sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("2026-08-19 19:00:29  замер proxy-main-07: 8769 КБ/с\n2026-08-19 19:00:30  замер proxy-main-02: 0 КБ/с (код 000)\n2026-08-19 19:00:31  замер proxy-main-05: 0 КБ/с (код 200)\n2026-08-19 19:00:32  замер proxy-us-02: 99 КБ/с\n2026-08-19 19:00:33  замер proxy-node-3dc89f1d7ba556b04d9a9059: 12345 КБ/с\n2026-08-19 19:00:34  замер direct: 99999 КБ/с\n2026-08-19 19:00:35  переключение: proxy-main-01 (16386) -> proxy-main-03 (18370 КБ/с)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(watchdogPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	reader := NewReader()
	reader.XkeenConfigPath = xkeenPath
	reader.CronPath = cronPath
	reader.SpeedLogPath = logPath
	reader.WatchdogPath = watchdogPath
	reader.LogLocation = time.FixedZone("MSK", 3*60*60)
	reader.ProcessExists = func(name string) bool { return name == "xray" }
	got := reader.Snapshot(context.Background())
	if !got.XrayRunning || !got.XkeenRunning {
		t.Fatalf("process state = %+v", got)
	}
	if got.Speed.IntervalMin != 1440 || got.Speed.Balancer != "bal-proxy" {
		t.Fatalf("speed balancer = %+v", got.Speed)
	}
	if got.Benchmark.InstalledSchedule != "17 4 * * *" || len(got.Benchmark.ThroughputKBps) != 5 {
		t.Fatalf("benchmark = %+v", got.Benchmark)
	}
	if got.Benchmark.ThroughputKBps["proxy-main-07"] != 8769 || got.Benchmark.ThroughputKBps["proxy-main-02"] != 0 || got.Benchmark.ThroughputKBps["proxy-main-05"] != 0 {
		t.Fatalf("throughput values = %+v", got.Benchmark.ThroughputKBps)
	}
	if got.Benchmark.ThroughputKBps["proxy-node-3dc89f1d7ba556b04d9a9059"] != 12345 {
		t.Fatalf("canonical throughput row was not parsed: %+v", got.Benchmark.ThroughputKBps)
	}
	if got.Benchmark.ThroughputError["proxy-main-02"] != "code-000" || got.Benchmark.ThroughputError["proxy-main-05"] != "" {
		t.Fatalf("throughput errors = %+v", got.Benchmark.ThroughputError)
	}
	expectedLastRun := time.Date(2026, 8, 19, 19, 0, 33, 0, reader.LogLocation)
	if !got.Benchmark.LastRunAt.Equal(expectedLastRun) || got.Benchmark.ThroughputAt["proxy-main-02"].IsZero() {
		t.Fatalf("throughput timestamps = last %v rows %+v", got.Benchmark.LastRunAt, got.Benchmark.ThroughputAt)
	}
	if !got.Watchdog.Enabled {
		t.Fatalf("throughput/watchdog = %+v %+v", got.Benchmark, got.Watchdog)
	}
}

func TestReaderParsesBoundedBenchmarkRunnerSchedule(t *testing.T) {
	raw := []byte("17 4 * * * /opt/etc/xkeen-control/run-bounded-speed-benchmark.sh\n")
	if got := parseBenchmarkSchedule(raw, true); got != "17 4 * * *" {
		t.Fatalf("bounded benchmark schedule = %q", got)
	}
}

func TestReaderUsesRouterTimezoneWhenProcessLocalIsUTC(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	dir := t.TempDir()
	logPath := filepath.Join(dir, "speed.log")
	timezonePath := filepath.Join(dir, "TZ")
	if err := os.WriteFile(logPath, []byte("2026-08-19 19:00:33  замер proxy-main-10: 11108 КБ/с\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(timezonePath, []byte("MSK-3\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewReader()
	reader.SpeedLogPath = logPath
	reader.TimezonePath = timezonePath
	reader.ProcessExists = func(string) bool { return false }
	got := reader.Snapshot(context.Background())
	expected := time.Date(2026, 8, 19, 19, 0, 33, 0, time.FixedZone("MSK", 3*60*60))
	if !got.Benchmark.LastRunAt.Equal(expected) || got.Benchmark.LastRunAt.Location().String() != "MSK" {
		t.Fatalf("benchmark timestamp = %v (%s), want %v (MSK)", got.Benchmark.LastRunAt, got.Benchmark.LastRunAt.Location(), expected)
	}
	if got.Benchmark.LastRunAt.UTC().Format(time.RFC3339) != "2026-08-19T16:00:33Z" {
		t.Fatalf("benchmark UTC projection = %s", got.Benchmark.LastRunAt.UTC().Format(time.RFC3339))
	}
}

func TestParseRouterTimezoneUsesPOSIXSignSemantics(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  int
		valid bool
	}{
		{name: "keenetic msk", raw: "MSK-3\n", want: 3 * 60 * 60, valid: true},
		{name: "west of utc", raw: "UTC+5", want: -5 * 60 * 60, valid: true},
		{name: "malformed", raw: "MSK-3DST", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseRouterTimezone(test.raw)
			if ok != test.valid {
				t.Fatalf("valid = %t, want %t", ok, test.valid)
			}
			if !test.valid {
				return
			}
			_, offset := time.Now().In(got).Zone()
			if offset != test.want {
				t.Fatalf("offset = %d, want %d", offset, test.want)
			}
		})
	}
}
