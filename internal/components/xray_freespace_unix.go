//go:build !windows

package components

import (
	"errors"
	"syscall"
)

func availableFreeSpace(path string) (uint64, error) {
	if path == "" {
		return 0, errors.New("free-space path is not configured")
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
