package auth

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const maxPasswordHashBytes = 256

func protectedPasswordInfo(info os.FileInfo, directory bool) bool {
	if info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	if directory {
		if !info.IsDir() {
			return false
		}
	} else if !info.Mode().IsRegular() || info.Size() > maxPasswordHashBytes {
		return false
	}
	if runtime.GOOS != "windows" {
		if directory && info.Mode().Perm() != 0o700 || !directory && info.Mode().Perm()&0o077 != 0 {
			return false
		}
	}
	return passwordRootOwned(info)
}

func samePasswordInfo(before, after os.FileInfo, directory bool) bool {
	return protectedPasswordInfo(after, directory) && os.SameFile(before, after) &&
		before.Mode() == after.Mode() && (directory || before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()))
}

// All three credential consumers use this reader; reads never repair authority.
func readPasswordAuthority(path string) ([]byte, error) {
	return readPasswordAuthorityWithOpen(path, openPasswordAuthority)
}

// The opener seam permits deterministic replacement-race fixtures without a
// process-global hook or a production configuration surface.
func readPasswordAuthorityWithOpen(path string, open func(string) (*os.File, error)) ([]byte, error) {
	dir := filepath.Dir(path)
	parent, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil || !protectedPasswordInfo(parent, true) {
		return nil, ErrNotConfigured
	}
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil || !protectedPasswordInfo(before, false) {
		return nil, ErrNotConfigured
	}
	file, err := open(path)
	if err != nil {
		return nil, ErrNotConfigured
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !samePasswordInfo(before, opened, false) {
		return nil, ErrNotConfigured
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPasswordHashBytes+1))
	after, statErr := os.Lstat(path)
	parentAfter, parentErr := os.Lstat(dir)
	final, finalErr := file.Stat()
	if err != nil || len(data) > maxPasswordHashBytes || statErr != nil || parentErr != nil || finalErr != nil ||
		!samePasswordInfo(opened, after, false) || !samePasswordInfo(opened, final, false) || !samePasswordInfo(parent, parentAfter, true) {
		return nil, ErrNotConfigured
	}
	hash := bytesTrimSpace(data)
	if _, err := bcrypt.Cost(hash); err != nil || len(hash) != 60 {
		return nil, ErrNotConfigured
	}
	for _, c := range hash[7:] {
		if !strings.ContainsRune("./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", rune(c)) {
			return nil, ErrNotConfigured
		}
	}
	return hash, nil
}
