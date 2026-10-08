//go:build linux

package setup

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"

	"golang.org/x/sys/unix"
)

func fresh() error {
	if os.Geteuid() != 0 || runtime.GOARCH != "arm64" {
		return ErrUnsupported
	}
	if noRunningComponents("/proc", os.Getpid()) != nil {
		return ErrState
	}
	for _, p := range []string{
		"/opt/sbin/xkeen", "/opt/sbin/_xkeen", "/opt/sbin/.xkeen", "/opt/sbin/xray", "/opt/sbin/mihomo", "/opt/sbin/mosdns", "/opt/sbin/xkeen-control",
		"/opt/etc/xkeen", "/opt/etc/xray", "/opt/etc/mihomo", "/opt/etc/mosdns", "/opt/etc/xkeen-control",
		"/opt/etc/init.d/S05xkeen", "/opt/etc/init.d/S06mosdns", "/opt/etc/init.d/S99xkeen-control", "/opt/etc/ndm/netfilter.d/proxy.sh", "/opt/libexec/xkeen-control-updater",
	} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return ErrState
		}
	}
	if _, e := exec.LookPath("opkg"); e != nil {
		return ErrUnsupported
	}
	var fs unix.Statfs_t
	if unix.Statfs("/opt", &fs) != nil || fs.Bavail*uint64(fs.Bsize) < 128<<20 {
		return ErrUnsupported
	}
	mem, e := os.ReadFile("/proc/meminfo")
	if e != nil || len(mem) > 64<<10 {
		return ErrUnsupported
	}
	available := uint64(0)
	for _, line := range strings.Split(string(mem), "\n") {
		w := strings.Fields(line)
		if len(w) == 3 && w[0] == "MemAvailable:" && w[2] == "kB" {
			available, _ = strconv.ParseUint(w[1], 10, 64)
		}
	}
	if available < 128<<10 {
		return ErrUnsupported
	}
	return nil
}

func noRunningComponents(root string, own int) error {
	d, e := os.Open(root)
	if e != nil {
		return ErrState
	}
	defer d.Close()
	entries, e := d.ReadDir(4097)
	if e != nil && e != io.EOF || len(entries) > 4096 {
		return ErrState
	}
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || pid == own {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		f, e := os.Open(filepath.Join(dir, "comm"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return ErrState
		}
		b, e := io.ReadAll(io.LimitReader(f, 1025))
		f.Close()
		if e != nil || len(b) > 1024 {
			return ErrState
		}
		switch strings.TrimSpace(string(b)) {
		case "xray", "mihomo", "mosdns", "xkeen-control", "xkeen", "S05xkeen":
			return ErrState
		}
		exe, e := os.Readlink(filepath.Join(dir, "exe"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil { // Kernel threads have no executable; other read failures stay unknown.
			return ErrState
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		switch exe {
		case "/opt/sbin/xray", "/opt/sbin/mihomo", "/opt/sbin/mosdns", "/opt/sbin/xkeen-control":
			return ErrState
		}
	}
	return nil
}

func exclusiveFile(path string, b []byte, mode uint32) error {
	d, e := ownedDirectory(filepath.Dir(path), false, false)
	if e != nil {
		return ErrState
	}
	defer d.Close()
	fd, e := unix.Openat(int(d.Fd()), filepath.Base(path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if e != nil {
		return ErrState
	}
	f := os.NewFile(uintptr(fd), path)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil || d.Sync() != nil {
		return ErrState
	}
	return nil
}

func bootstrapNative(payload map[string][]byte) error {
	for _, p := range []string{"/opt/sbin/xkeen", "/opt/sbin/_xkeen", "/opt/sbin/.xkeen"} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return ErrState
		}
	}
	d, e := ownedDirectory("/opt/sbin", false, false)
	if e != nil {
		return ErrState
	}
	d.Close()
	names := make([]string, 0, len(payload))
	for n := range payload {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if n == "xkeen" {
			continue
		}
		if !strings.HasPrefix(n, "_xkeen/") || strings.Contains(n, "..") || filepath.Clean(n) != n {
			return ErrState
		}
		d, e := ownedDirectory(filepath.Dir("/opt/sbin/"+n), true, false)
		if e != nil {
			return ErrState
		}
		d.Close()
		if exclusiveFile("/opt/sbin/"+n, payload[n], 0600) != nil {
			return ErrState
		}
	}
	if len(payload["xkeen"]) == 0 {
		return ErrState
	}
	return exclusiveFile("/opt/sbin/xkeen", payload["xkeen"], 0700)
}

func fixedCommand(ctx context.Context, timeout time.Duration, binary string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p := exec.CommandContext(ctx, binary, args...)
	p.Stdout, p.Stderr = io.Discard, io.Discard
	p.Stdin = nil
	p.WaitDelay = time.Second
	p.Env = append([]string{"PATH=/opt/bin:/opt/sbin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/opt/root", "TERM=dumb", "XKEEN_FOREGROUND=1"}, "LANG=C")
	return p.Run()
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, ErrState
	}
	return b.Buffer.Write(p)
}
func fixedOutput(ctx context.Context, timeout time.Duration, limit int, binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p := exec.CommandContext(ctx, binary, args...)
	p.WaitDelay = time.Second
	p.Stderr = io.Discard
	b := &boundedOutput{limit: limit}
	p.Stdout = b
	if p.Run() != nil {
		return nil, ErrState
	}
	return b.Bytes(), nil
}

func installTools(ctx context.Context) error {
	b, e := fixedOutput(ctx, 5*time.Second, 4096, "/opt/bin/tar", "--version")
	if e == nil && strings.Contains(string(b), "GNU tar") {
		return nil
	}
	if fixedCommand(ctx, time.Minute, "/opt/bin/opkg", "update") != nil || fixedCommand(ctx, 2*time.Minute, "/opt/bin/opkg", "install", "tar") != nil {
		return ErrState
	}
	b, e = fixedOutput(ctx, 5*time.Second, 4096, "/opt/bin/tar", "--version")
	if e != nil || !strings.Contains(string(b), "GNU tar") {
		return ErrState
	}
	return nil
}

const dnsInit = `#!/bin/sh
set -eu
PATH=/opt/bin:/opt/sbin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
BIN=/opt/sbin/mosdns
CONF=/opt/etc/mosdns/config.json
PID=/opt/var/run/mosdns.pid
valid_pid() {
 [ -f "$PID" ] && [ ! -L "$PID" ] || return 1
 pid=$(cat "$PID")
 case "$pid" in ''|*[!0-9]*) return 1;; esac
 [ "$(readlink "/proc/$pid/exe")" = "$BIN" ] || return 1
 tr '\000' '\n' < "/proc/$pid/cmdline" | grep -Fxq "$CONF"
}
start() {
 if valid_pid; then return 0; fi
 [ ! -L "$PID" ] || return 1
 mkdir -p /opt/var/run
 umask 077
 nohup "$BIN" start -c "$CONF" >/dev/null 2>&1 &
 printf '%s\n' "$!" > "$PID"
}
stop() {
 if valid_pid; then
  kill -TERM "$pid"
  n=0
  while valid_pid; do
   n=$((n+1)); [ "$n" -lt 10 ] || return 1
   sleep 1
  done
 fi
 rm -f "$PID"
}
case "${1:-start}" in
 start) start;; stop) stop;; restart) stop; start;; status) valid_pid;; *) exit 2;;
esac
`

func provisionDNS(binary, config []byte) error {
	for _, p := range []string{"/opt/sbin/mosdns", "/opt/etc/mosdns", "/opt/etc/init.d/S06mosdns"} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return ErrState
		}
	}
	d, e := privateDirectory("/opt/etc/mosdns", true)
	if e != nil {
		return ErrState
	}
	d.Close()
	if exclusiveFile("/opt/sbin/mosdns", binary, 0700) != nil || exclusiveFile("/opt/etc/mosdns/config.json", config, 0600) != nil {
		return ErrState
	}
	return exclusiveFile("/opt/etc/init.d/S06mosdns", []byte(dnsInit), 0700)
}

func installPanel(ctx context.Context, lock *os.File) error {
	// Invoke the existing release-owned installer, with the live owner FD. Its
	// internal branch performs ordinary verified placement and defers startup.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	p := exec.CommandContext(ctx, "/bin/sh", "/tmp/xkeen-control/guided-bootstrap/assets/install.sh", "--setup-panel")
	p.ExtraFiles = []*os.File{lock}
	p.Stdout, p.Stderr = io.Discard, io.Discard
	p.WaitDelay = time.Second
	p.Env = []string{"PATH=/opt/bin:/opt/sbin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/opt/root"}
	info := buildinfo.Current()
	if info.Channel == "beta" {
		p.Env = append(p.Env, "XKEEN_CONTROL_CHANNEL=beta", "XKEEN_CONTROL_VERSION="+info.Version)
	}
	if p.Run() != nil {
		return ErrState
	}
	return nil
}
