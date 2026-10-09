//go:build linux

package setup

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Walk directory descriptors rather than following pathname symlinks. Existing
// ancestors are root owned and cannot be group/world writable (except a sticky
// ancestor such as /tmp used by synthetic fixtures); the leaf is always 0700.
func privateDirectory(path string, create bool) (*os.File, error) {
	return ownedDirectory(path, create, true)
}

func ownedDirectory(path string, create, private bool) (*os.File, error) {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrState
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrState
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return nil, ErrState
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e == unix.ENOENT && create {
			if unix.Mkdirat(fd, part, 0700) != nil {
				unix.Close(fd)
				return nil, ErrState
			}
			if unix.Fsync(fd) != nil {
				unix.Close(fd)
				return nil, ErrState
			}
			next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != 0 ||
			(st.Mode&0022 != 0 && st.Mode&unix.S_ISVTX == 0) ||
			(i == len(parts)-1 && private && st.Mode&0777 != 0700) {
			unix.Close(fd)
			return nil, ErrState
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func protectedFile(dir *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Nlink != 1 {
		unix.Close(fd)
		return nil, ErrState
	}
	return os.NewFile(uintptr(fd), name), nil
}

func acquireLock(path string, exclusive bool) (func(), error) {
	f, err := acquireLockFile(path, exclusive)
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

func acquireLockFile(path string, exclusive bool) (*os.File, error) {
	return acquireLockFileMode(path, exclusive, true)
}

func acquireExistingLock(path string, exclusive bool) (func(), error) {
	f, err := acquireLockFileMode(path, exclusive, false)
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// MaintenanceFile is the same existing process lock used by Maintenance. The
// update owner may pass this exact open-file-description to its fixed helper;
// it must downgrade EX to SH before starting a Normal daemon.
func MaintenanceFile() (*os.File, error) {
	f, err := acquireLockFileMode(LockPath, true, false)
	if err != nil {
		return nil, err
	}
	r, err := readReceipt(ReceiptPath)
	if err != nil || r != nil && r.Phase != "completed" {
		f.Close()
		return nil, ErrState
	}
	return f, nil
}

func lockPathMatches(path string, f *os.File) bool {
	d, err := privateDirectory(filepath.Dir(path), false)
	if err != nil {
		return false
	}
	defer d.Close()
	current, err := protectedFile(d, filepath.Base(path), unix.O_RDONLY)
	if err != nil {
		return false
	}
	defer current.Close()
	want, err := current.Stat()
	if err != nil {
		return false
	}
	actual, err := f.Stat()
	return err == nil && os.SameFile(want, actual)
}

func acquireLockFileMode(path string, exclusive, create bool) (*os.File, error) {
	d, err := privateDirectory(filepath.Dir(path), create)
	if err != nil {
		return nil, ErrState
	}
	defer d.Close()
	flags := unix.O_RDWR
	if create {
		flags |= unix.O_CREAT
	}
	f, err := protectedFile(d, filepath.Base(path), flags)
	if err != nil {
		return nil, ErrState
	}
	if !lockPathMatches(path, f) {
		f.Close()
		return nil, ErrState
	}
	op := unix.LOCK_SH
	if exclusive {
		op = unix.LOCK_EX
	}
	if err := unix.Flock(int(f.Fd()), op|unix.LOCK_NB); err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, ErrBusy
		}
		return nil, ErrState
	}
	if !lockPathMatches(path, f) {
		f.Close()
		return nil, ErrState
	}
	return f, nil
}

func setupOwner() (*os.File, error) {
	// A release launcher may hand us its already-held exclusive description.
	// A normal direct CLI invocation owns its freshly opened description instead.
	var st unix.Stat_t
	if unix.Fstat(3, &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFREG {
		borrow := os.NewFile(3, "setup-owner")
		actual, e := borrow.Stat()
		if e == nil {
			d, e := privateDirectory(filepath.Dir(LockPath), false)
			if e != nil {
				return nil, ErrState
			}
			defer d.Close()
			want, e := protectedFile(d, filepath.Base(LockPath), unix.O_RDONLY)
			if e != nil {
				return nil, ErrState
			}
			defer want.Close()
			info, e := want.Stat()
			if e != nil || !os.SameFile(info, actual) {
				return nil, ErrState
			}
			if unix.Flock(3, unix.LOCK_EX|unix.LOCK_NB) != nil {
				return nil, ErrBusy
			}
			unix.CloseOnExec(3)
			return borrow, nil
		}
		return nil, ErrState
	}
	return acquireLockFile(LockPath, true)
}

// Only the live setup owner's inherited descriptor admits panel installation;
// no environment switch can bypass the startup fence.
func PanelInstallGuard() error {
	d, e := privateDirectory(filepath.Dir(LockPath), false)
	if e != nil {
		return ErrState
	}
	defer d.Close()
	f, e := protectedFile(d, filepath.Base(LockPath), unix.O_RDONLY)
	if e != nil {
		return ErrState
	}
	defer f.Close()
	want, e := f.Stat()
	if e != nil {
		return ErrState
	}
	borrow := os.NewFile(3, "setup-lock")
	if borrow == nil {
		return ErrState
	}
	actual, e := borrow.Stat()
	if e != nil || !os.SameFile(want, actual) {
		return ErrState
	}
	if unix.Flock(3, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return ErrBusy
	}
	r, e := readReceipt(ReceiptPath)
	if e != nil || r == nil || r.Phase != "panel" {
		return ErrState
	}
	return nil
}

func readReceipt(path string) (*Receipt, error) {
	d, err := privateDirectory(filepath.Dir(path), false)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrState
	}
	defer d.Close()
	f, err := protectedFile(d, filepath.Base(path), unix.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrState
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReceipt+1))
	if err != nil {
		return nil, ErrState
	}
	r, err := decodeReceipt(b)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func writeReceipt(path string, r Receipt) error {
	if !r.valid() {
		return ErrState
	}
	d, err := privateDirectory(filepath.Dir(path), true)
	if err != nil {
		return ErrState
	}
	defer d.Close()
	if _, err := readReceipt(path); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil || len(b) > maxReceipt {
		return ErrState
	}
	// O_EXCL never follows or truncates an existing temporary path. A leftover
	// after a crash is blocked and inspected, never removed automatically.
	temp := filepath.Base(path) + ".new"
	f, err := protectedFile(d, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return ErrState
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrState
	}
	if unix.Renameat(int(d.Fd()), temp, int(d.Fd()), filepath.Base(path)) != nil || d.Sync() != nil {
		return ErrState
	}
	return nil
}
