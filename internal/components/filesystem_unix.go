//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package components

import (
	"errors"
	"os"
	"syscall"
)

func sameFilesystem(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	leftStat, ok := leftInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("filesystem identity is unavailable")
	}
	rightStat, ok := rightInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("filesystem identity is unavailable")
	}
	return leftStat.Dev == rightStat.Dev, nil
}
