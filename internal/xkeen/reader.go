package xkeen

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-keenetic/internal/benchmarkpolicy"
	"github.com/popiposter/xkeen-keenetic/internal/redact"
)

const (
	maxCronSize         = 128 << 10
	maxLogTail          = 128 << 10
	maxTimezoneFileSize = 256
)

type Snapshot struct {
	XrayRunning  bool
	XkeenRunning bool
	Speed        SpeedBalancer
	Benchmark    Benchmark
	Watchdog     Watchdog
}

type SpeedBalancer struct {
	Enabled       bool
	IntervalMin   int
	Hysteresis    int
	Balancer      string
	EligibleNodes int
	PayloadBytes  int64
	NodeSeconds   int
	MaxBytes      int64
	MaxSeconds    int
}

type Benchmark struct {
	InstalledSchedule string
	LastRunAt         time.Time
	ThroughputKBps    map[string]float64
	ThroughputAt      map[string]time.Time
	ThroughputError   map[string]string
}

type Watchdog struct {
	Installed bool
	Enabled   bool
}

type Reader struct {
	ProcRoot        string
	XkeenConfigPath string
	CronPath        string
	SpeedLogPath    string
	WatchdogPath    string
	TimezonePath    string
	LogLocation     *time.Location
	ProcessExists   func(string) bool
	PathExists      func(string) bool
}

func NewReader() Reader {
	return Reader{
		ProcRoot:        "/proc",
		XkeenConfigPath: "/opt/etc/xkeen/xkeen.json",
		CronPath:        "/opt/var/spool/cron/crontabs/root",
		SpeedLogPath:    "/opt/var/log/xray/speed_balancer.log",
		WatchdogPath:    "/opt/etc/xkeen/speed_failover_watchdog.sh",
		TimezonePath:    "/etc/TZ",
	}
}

func (r Reader) Snapshot(ctx context.Context) Snapshot {
	_ = ctx
	if r.ProcRoot == "" {
		r.ProcRoot = "/proc"
	}
	if r.XkeenConfigPath == "" {
		r.XkeenConfigPath = "/opt/etc/xkeen/xkeen.json"
	}
	if r.CronPath == "" {
		r.CronPath = "/opt/var/spool/cron/crontabs/root"
	}
	if r.SpeedLogPath == "" {
		r.SpeedLogPath = "/opt/var/log/xray/speed_balancer.log"
	}
	if r.WatchdogPath == "" {
		r.WatchdogPath = "/opt/etc/xkeen/speed_failover_watchdog.sh"
	}
	if r.TimezonePath == "" {
		r.TimezonePath = "/etc/TZ"
	}
	processExists := r.ProcessExists
	if processExists == nil {
		processExists = func(name string) bool { return processExistsInProc(r.ProcRoot, name) }
	}
	pathExists := r.PathExists
	if pathExists == nil {
		pathExists = func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}
	}

	xrayRunning := processExists("xray")
	cron, cronOK := readBounded(r.CronPath, maxCronSize)
	benchmark := Benchmark{
		ThroughputKBps:  make(map[string]float64),
		ThroughputAt:    make(map[string]time.Time),
		ThroughputError: make(map[string]string),
	}
	benchmark.InstalledSchedule = parseBenchmarkSchedule(cron, cronOK)
	benchmark.LastRunAt, benchmark.ThroughputKBps, benchmark.ThroughputAt, benchmark.ThroughputError = parseSpeedLog(r.SpeedLogPath, r.logLocation())
	watchdogInstalled := pathExists(r.WatchdogPath)
	watchdogEnabled := cronHasWatchdog(cron, cronOK)

	result := Snapshot{
		XrayRunning:  xrayRunning,
		XkeenRunning: xrayRunning && pathExists(r.XkeenConfigPath),
		Benchmark:    benchmark,
		Watchdog: Watchdog{
			Installed: watchdogInstalled,
			Enabled:   watchdogEnabled,
		},
	}
	result.Speed = parseSpeedBalancer(readBounded(r.XkeenConfigPath, maxCronSize))
	return result
}

func (r Reader) logLocation() *time.Location {
	if r.LogLocation != nil {
		return r.LogLocation
	}
	if location, ok := loadRouterTimezone(r.TimezonePath); ok {
		return location
	}
	return time.UTC
}

func processExistsInProc(root, name string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isDigits(entry.Name()) {
			continue
		}
		contents, err := os.ReadFile(root + string(os.PathSeparator) + entry.Name() + string(os.PathSeparator) + "comm")
		if err == nil && strings.TrimSpace(string(contents)) == name {
			return true
		}
	}
	return false
}

func parseSpeedBalancer(raw []byte, ok bool) SpeedBalancer {
	if !ok {
		return SpeedBalancer{}
	}
	var document struct {
		Xkeen struct {
			Xray struct {
				SpeedBalancer struct {
					Enabled    bool   `json:"enabled"`
					Interval   int    `json:"interval"`
					Hysteresis int    `json:"hysteresis"`
					Balancer   string `json:"balancer"`
				} `json:"speed_balancer"`
			} `json:"xray"`
		} `json:"xkeen"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return SpeedBalancer{}
	}
	value := document.Xkeen.Xray.SpeedBalancer
	policy := benchmarkpolicy.Parse(raw)
	return SpeedBalancer{
		Enabled:       value.Enabled,
		IntervalMin:   nonNegative(value.Interval),
		Hysteresis:    nonNegative(value.Hysteresis),
		Balancer:      safeLabel(value.Balancer),
		EligibleNodes: policy.EligibleNodes,
		PayloadBytes:  policy.PayloadBytes,
		NodeSeconds:   policy.NodeSeconds,
		MaxBytes:      policy.MaxBytes,
		MaxSeconds:    policy.MaxSeconds,
	}
}

var benchmarkSchedulePattern = regexp.MustCompile(`(?m)^\s*([0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+)\s+([^\s]+)\s+-sbt(?:\s|$)`)
var boundedBenchmarkSchedulePattern = regexp.MustCompile(`(?m)^\s*([0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+)\s+([^\s]*run-bounded-speed-benchmark\.sh)(?:\s|$)`)
var watchdogPattern = regexp.MustCompile(`(?m)^\s*\*/5\s+\*\s+\*\s+\*\s+\*\s+[^\s]*speed_failover_watchdog\.sh(?:\s|$)`)
var productionThroughputPattern = regexp.MustCompile(`^\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+замер\s+([A-Za-z0-9._-]{1,128})\s*:\s*([0-9]+([.,][0-9]+)?)\s*КБ/с(\s+\(код\s+([0-9]{3})\))?\s*$`)
var legacyThroughputPattern = regexp.MustCompile(`(?i)^\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})?\s*.*?([A-Za-z0-9._-]{1,128})\b[^0-9]{0,100}([0-9]+([.,][0-9]+)?)\s*(KB|KiB)/(s|ps)\b`)
var routerTimezonePattern = regexp.MustCompile(`^([A-Za-z]{1,15})([+-])(\d{1,2})(?::([0-5]\d))?(?::([0-5]\d))?$`)

func parseBenchmarkSchedule(raw []byte, ok bool) string {
	if !ok {
		return ""
	}
	if match := boundedBenchmarkSchedulePattern.FindSubmatch(raw); len(match) >= 3 {
		return string(match[1])
	}
	match := benchmarkSchedulePattern.FindSubmatch(raw)
	if len(match) < 3 {
		return ""
	}
	command := string(match[2])
	if command != "xkeen" && !strings.HasSuffix(command, "/xkeen") {
		return ""
	}
	return string(match[1])
}

func cronHasWatchdog(raw []byte, ok bool) bool {
	return ok && watchdogPattern.Match(raw)
}

func parseSpeedLog(path string, location *time.Location) (time.Time, map[string]float64, map[string]time.Time, map[string]string) {
	result := make(map[string]float64)
	timestamps := make(map[string]time.Time)
	errors := make(map[string]string)
	if location == nil {
		location = time.UTC
	}
	file, err := os.Open(path)
	if err != nil {
		return time.Time{}, result, timestamps, errors
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return time.Time{}, result, timestamps, errors
	}
	start := info.Size() - maxLogTail
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return time.Time{}, result, timestamps, errors
	}
	tail, err := io.ReadAll(io.LimitReader(file, maxLogTail))
	if err != nil {
		return time.Time{}, result, timestamps, errors
	}
	var lastRunAt time.Time
	for _, line := range strings.Split(string(tail), "\n") {
		var timestampText, tag, valueText, unit, code string
		if match := productionThroughputPattern.FindStringSubmatch(line); len(match) > 0 {
			timestampText, tag, valueText, unit, code = match[1], match[2], match[3], "KB", match[6]
		} else if match := legacyThroughputPattern.FindStringSubmatch(line); len(match) > 0 {
			timestampText, tag, valueText, unit = match[1], match[2], match[3], match[5]
		} else {
			continue
		}
		if !redact.IsUnifiedOutboundTag(tag) {
			continue
		}
		value, err := strconv.ParseFloat(strings.ReplaceAll(valueText, ",", "."), 64)
		if err == nil && value >= 0 && value < 1e12 {
			if strings.EqualFold(unit, "KiB") {
				value *= 1.024
			}
			at := parseLogTimestamp(timestampText, location)
			if previous, exists := timestamps[tag]; exists && !at.IsZero() && !previous.IsZero() && at.Before(previous) {
				continue
			}
			result[tag] = value
			timestamps[tag] = at
			errors[tag] = throughputError(code)
			if at.After(lastRunAt) {
				lastRunAt = at
			}
		}
	}
	return lastRunAt, result, timestamps, errors
}

func loadRouterTimezone(path string) (*time.Location, bool) {
	if path == "" {
		return nil, false
	}
	raw, ok := readBounded(path, maxTimezoneFileSize)
	if !ok {
		return nil, false
	}
	return parseRouterTimezone(string(raw))
}

func parseRouterTimezone(raw string) (*time.Location, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, false
	}
	match := routerTimezonePattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return nil, false
	}
	hours, err := strconv.Atoi(match[3])
	if err != nil {
		return nil, false
	}
	minutes, err := parseOptionalTimezonePart(match[4])
	if err != nil {
		return nil, false
	}
	seconds, err := parseOptionalTimezonePart(match[5])
	if err != nil {
		return nil, false
	}
	offset := hours*60*60 + minutes*60 + seconds
	if offset > 24*60*60 {
		return nil, false
	}
	if match[2] == "+" {
		offset = -offset
	}
	return time.FixedZone(match[1], offset), true
}

func parseOptionalTimezonePart(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func parseLogTimestamp(value string, location *time.Location) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(value), location)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func throughputError(code string) string {
	if code == "" || code == "200" {
		return ""
	}
	return "code-" + code
}

func readBounded(path string, limit int64) ([]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(contents)) > limit {
		return nil, false
	}
	return contents, true
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func safeLabel(value string) string {
	if len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-/", r) {
			continue
		}
		return ""
	}
	return value
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
