//go:build linux

package update

import (
	"os"
	"path/filepath"
	"syscall"
)

func protectedPath(path string) bool {
	return protectedPathKind(path, false)
}

func protectedDirectory(path string) bool { return protectedPathKind(path, true) }

func protectedPathKind(path string, directory bool) bool {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	leaf := true
	for {
		i, err := os.Lstat(path)
		if err != nil || i.Mode()&os.ModeSymlink != 0 {
			return false
		}
		s, ok := i.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 {
			return false
		}
		if leaf {
			if directory && (!i.IsDir() || i.Mode().Perm()&0022 != 0) || !directory && (!i.Mode().IsRegular() || s.Nlink != 1 || i.Mode().Perm()&0022 != 0) {
				return false
			}
		} else if !i.IsDir() || i.Mode().Perm()&0022 != 0 && i.Mode()&os.ModeSticky == 0 {
			return false
		}
		if path == "/" {
			break
		}
		path = filepath.Dir(path)
		leaf = false
	}
	return true
}
