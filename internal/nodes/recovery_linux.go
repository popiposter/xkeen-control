//go:build linux

package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/popiposter/xkeen-control/internal/xkeen"
	"golang.org/x/sys/unix"
)

func recoveryDirectory(path string, private bool) (*os.File, error) {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrNodeRecoveryRequired
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != 0 || (st.Mode&0022 != 0 && (i == len(parts)-1 || st.Mode&unix.S_ISVTX == 0)) || private && i == len(parts)-1 && st.Mode&0777 != 0700 {
			unix.Close(fd)
			return nil, ErrNodeRecoveryRequired
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func recoveryRead(path string, max int, private bool) ([]byte, os.FileInfo, error) {
	dir, err := recoveryDirectory(filepath.Dir(path), private)
	if err != nil {
		return nil, nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || st.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || private && info.Mode().Perm() != 0600 || info.Size() > int64(max) {
		return nil, nil, ErrNodeRecoveryRequired
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil || len(b) > max {
		return nil, nil, ErrNodeRecoveryRequired
	}
	after, err := f.Stat()
	current, e := os.Lstat(path)
	if err != nil || e != nil || !os.SameFile(info, current) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, nil, ErrNodeRecoveryRequired
	}
	return b, info, nil
}

func recoveryIdentity(info os.FileInfo, data []byte) string {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "%d:%d:%d:%d:%d:", st.Dev, st.Ino, info.Mode(), info.Size(), info.ModTime().UnixNano())
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func recoveryConfigNames(path string) ([]string, error) {
	d, err := recoveryDirectory(path, false)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(65)
	if err != nil && err != io.EOF || len(entries) > 64 {
		return nil, ErrNodeRecoveryRequired
	}
	var names []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".jsonc") {
			return nil, ErrNodeRecoveryRequired
		}
		if !e.Type().IsRegular() {
			return nil, ErrNodeRecoveryRequired
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 || len(names) > 32 {
		return nil, ErrNodeRecoveryRequired
	}
	sort.Strings(names)
	return names, nil
}

// ProcessRecoveryRuntime reads /proc only. It deliberately requires cron to be
// quiesced and rejects foreign Xray or native lifecycle children.
type ProcessRecoveryRuntime struct{ ProcRoot, Binary, ConfigDir, ReceiptPath string }

func (r ProcessRecoveryRuntime) Snapshot(ctx context.Context) (string, error) {
	if r.ReceiptPath != "" && xkeen.OfflineLifecycleSettled(r.ReceiptPath) != nil {
		return "", ErrNodeRecoveryRequired
	}
	root := r.ProcRoot
	if root == "" {
		root = "/proc"
	}
	if r.Binary == "" || r.ConfigDir == "" {
		return "", ErrNodeRecoveryRequired
	}
	editor := &xkeen.ConfigEditor{ProcRoot: root, XrayBinary: r.Binary, Dir: r.ConfigDir}
	before, err := editor.ReadConfigProcess(ctx)
	if err != nil {
		return "", ErrNodeRecoveryRequired
	}
	directory, err := os.Open(root)
	if err != nil {
		return "", ErrNodeRecoveryRequired
	}
	defer directory.Close()
	entries, err := directory.ReadDir(4097)
	if err != nil && err != io.EOF || len(entries) > 4096 {
		return "", ErrNodeRecoveryRequired
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "cmdline")
		f, err := os.Open(path)
		if err != nil {
			if _, e := os.Lstat(filepath.Dir(path)); os.IsNotExist(e) {
				continue
			}
			return "", ErrNodeRecoveryRequired
		}
		cmd, err := io.ReadAll(io.LimitReader(f, 16385))
		f.Close()
		if err != nil || len(cmd) > 16384 {
			return "", ErrNodeRecoveryRequired
		}
		for _, arg := range strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00") {
			base := filepath.Base(arg)
			if base == "crond" || base == "xkeen" || base == "S05xkeen" || base == "xkeen-control" || strings.HasPrefix(arg, "/opt/etc/xkeen/") || strings.HasPrefix(arg, "/opt/sbin/.xkeen/") || strings.Contains(arg, "xkeen -") || strings.Contains(arg, "S05xkeen ") {
				return "", ErrNodeRecoveryRequired
			}
		}
	}
	after, err := editor.ReadConfigProcess(ctx)
	if err != nil || before != after {
		return "", ErrNodeRecoveryRequired
	}
	if after == "" {
		return "stopped", nil
	}
	sum := sha256.Sum256([]byte(after))
	return hex.EncodeToString(sum[:]), nil
}
