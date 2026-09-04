//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris

package components

import "errors"

func sameFilesystem(string, string) (bool, error) {
	return false, errors.New("filesystem identity is unavailable")
}
