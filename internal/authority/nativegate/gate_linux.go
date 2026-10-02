//go:build linux

// Package nativegate implements the opt-in native operation admission protocol.
package nativegate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type Action string

const (
	Start        Action = "start"
	Stop         Action = "stop"
	Restart      Action = "restart"
	ConfigChange Action = "config-change"
	Reconcile    Action = "reconcile"
)

var (
	ErrBusy     = errors.New("native gate busy or unresolved")
	ErrUnsafe   = errors.New("unsafe native gate path")
	ErrNotOwner = errors.New("native gate ownership not proven")
)

// Lease is either an owner or a foreground borrow. Borrowers cannot release.
// The caller must join every foreground borrower before releasing the owner.
// No executor, background propagation, recovery journal or auto-reaper is added.
type Lease struct {
	root, record, token string
	owned               bool
	mu                  sync.Mutex
	released            bool
}

func validAction(a Action) bool {
	return a == Start || a == Stop || a == Restart || a == ConfigChange || a == Reconcile
}

// Acquire attempts immediate admission under an explicit, pre-created root.
// A leftover directory (including incomplete metadata) is always busy.
func Acquire(root string, action Action) (*Lease, error) {
	if !validAction(action) || protectedRoot(root) != nil {
		return nil, ErrUnsafe
	}
	boot, err := bootID()
	if err != nil {
		return nil, ErrUnsafe
	}
	pid := os.Getpid()
	_, start, err := process(pid)
	if err != nil {
		return nil, ErrUnsafe
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, ErrUnsafe
	}
	token := hex.EncodeToString(random[:])
	record := fmt.Sprintf("v1 %s %d %s %s %s\n", boot, pid, start, token, action)
	path := filepath.Join(root, "operation.lock.d")
	if err := os.Mkdir(path, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrBusy
		}
		return nil, ErrUnsafe
	}
	// From here errors deliberately retain the incomplete directory for recovery.
	if protected(path, 0700, true) != nil {
		return nil, ErrUnsafe
	}
	f, err := os.OpenFile(filepath.Join(path, "owner.tmp"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, ErrUnsafe
	}
	_, writeErr := f.WriteString(record)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return nil, ErrUnsafe
	}
	if err := os.Rename(filepath.Join(path, "owner.tmp"), filepath.Join(path, "owner")); err != nil {
		return nil, ErrUnsafe
	}
	return &Lease{root: root, record: record, token: token, owned: true}, nil
}

// Join accepts only a live owner in this process's ancestry with the exact token.
// Environment variables alone never establish ownership or permit release.
func Join(root, token string) (*Lease, error) {
	if protectedRoot(root) != nil {
		return nil, ErrUnsafe
	}
	record, err := readOwner(root)
	if err != nil {
		return nil, ErrNotOwner
	}
	fields := strings.Fields(record)
	boot, err := bootID()
	if err != nil || fields[1] != boot || fields[4] != token {
		return nil, ErrNotOwner
	}
	ownerPID, _ := strconv.Atoi(fields[2])
	_, ownerStart, err := process(ownerPID)
	if err != nil || ownerStart != fields[3] {
		return nil, ErrNotOwner
	}
	pid := os.Getpid()
	for depth := 0; depth < 256 && pid > 0; depth++ {
		parent, start, err := process(pid)
		if err != nil {
			return nil, ErrNotOwner
		}
		if pid == ownerPID {
			if start != fields[3] {
				return nil, ErrNotOwner
			}
			// Recheck the tuple after ancestry traversal; never adopt a replacement.
			again, err := readOwner(root)
			if err != nil || again != record {
				return nil, ErrNotOwner
			}
			return &Lease{root: root, record: record, token: token}, nil
		}
		if parent == pid {
			return nil, ErrNotOwner
		}
		pid = parent
	}
	return nil, ErrNotOwner
}

func (l *Lease) Release() error {
	if l == nil {
		return ErrNotOwner
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.owned || l.released {
		return ErrNotOwner
	}
	if protectedRoot(l.root) != nil {
		return ErrUnsafe
	}
	current, err := readOwner(l.root)
	if err != nil || current != l.record {
		return ErrNotOwner
	}
	fields := strings.Fields(current)
	boot, err := bootID()
	_, start, processErr := process(os.Getpid())
	if err != nil || processErr != nil || fields[1] != boot || fields[2] != strconv.Itoa(os.Getpid()) || fields[3] != start {
		return ErrNotOwner
	}
	path := filepath.Join(l.root, "operation.lock.d")
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 || entries[0].Name() != "owner" {
		return ErrNotOwner
	}
	if err := os.Remove(filepath.Join(path, "owner")); err != nil {
		return ErrNotOwner
	}
	if err := os.Remove(path); err != nil {
		return ErrNotOwner
	}
	l.released = true
	return nil
}

func (l *Lease) Token() string { return l.token }

// ChildEnvironment is an explicit foreground-only projection. It must not be
// passed to long-lived children. StripEnvironment is voluntary until integration.
func (l *Lease) ChildEnvironment(env []string) []string {
	return append(StripEnvironment(env), "XKEEN_GATE_ROOT="+l.root, "XKEEN_GATE_TOKEN="+l.token)
}
func StripEnvironment(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "XKEEN_GATE_ROOT=") && !strings.HasPrefix(e, "XKEEN_GATE_TOKEN=") &&
			!strings.HasPrefix(e, "XKEEN_ADMISSION_ROLE=") && !strings.HasPrefix(e, "XKEEN_ADMISSION_ACTION=") &&
			!strings.HasPrefix(e, "XKEEN_ADMISSION_CALL=") {
			out = append(out, e)
		}
	}
	return out
}

func protectedRoot(root string) error {
	if os.Geteuid() != 0 || !strings.HasPrefix(root, "/tmp/") || filepath.Clean(root) != root {
		return ErrUnsafe
	}
	for _, r := range root {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_-.", r)) {
			return ErrUnsafe
		}
	}
	if protected("/", 0755, true) != nil {
		return ErrUnsafe
	}
	info, err := os.Lstat("/tmp")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0777 || info.Mode()&os.ModeSticky == 0 {
		return ErrUnsafe
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return ErrUnsafe
	}
	path := "/tmp"
	for _, component := range strings.Split(strings.TrimPrefix(root, "/tmp/"), "/") {
		path += "/" + component
		if protected(path, 0700, true) != nil {
			return ErrUnsafe
		}
	}
	return nil
}

func protected(path string, mode os.FileMode, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != dir || !dir && !info.Mode().IsRegular() || info.Mode().Perm() != mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return ErrUnsafe
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !dir && (stat.Nlink != 1 || info.Size() > 200) {
		return ErrUnsafe
	}
	return nil
}

func readOwner(root string) (string, error) {
	path := filepath.Join(root, "operation.lock.d")
	// A native child's unknown outcome poisons every peer, not only Release.
	// Active foreground call records remain allowed while their child is joined.
	if _, err := os.Lstat(filepath.Join(path, "unresolved")); !errors.Is(err, os.ErrNotExist) {
		return "", ErrNotOwner
	}
	if protected(path, 0700, true) != nil || protected(filepath.Join(path, "owner"), 0600, false) != nil {
		return "", ErrNotOwner
	}
	b, err := os.ReadFile(filepath.Join(path, "owner"))
	if err != nil || len(b) > 200 {
		return "", ErrNotOwner
	}
	f := strings.Fields(string(b))
	if len(f) != 6 || f[0] != "v1" || !uuid(f[1]) || !decimal(f[2]) || !decimal(f[3]) || len(f[4]) != 32 || !validAction(Action(f[5])) {
		return "", ErrNotOwner
	}
	if _, err := hex.DecodeString(f[4]); err != nil || strings.ToLower(f[4]) != f[4] {
		return "", ErrNotOwner
	}
	pid, err := strconv.Atoi(f[2])
	if err != nil || pid < 1 {
		return "", ErrNotOwner
	}
	if string(b) != strings.Join(f, " ")+"\n" {
		return "", ErrNotOwner
	}
	return string(b), nil
}

func decimal(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func uuid(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func bootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	value := strings.TrimSpace(string(b))
	if err != nil || !uuid(value) {
		return "", ErrUnsafe
	}
	return value, nil
}
func process(pid int) (int, string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, "", ErrNotOwner
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return 0, "", ErrNotOwner
	}
	f := strings.Fields(string(b)[end+1:])
	if len(f) < 20 || f[0] == "Z" || f[0] == "X" || !decimal(f[19]) {
		return 0, "", ErrNotOwner
	}
	parent, err := strconv.Atoi(f[1])
	if err != nil || parent < 0 {
		return 0, "", ErrNotOwner
	}
	return parent, f[19], nil
}
