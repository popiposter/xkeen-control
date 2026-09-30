//go:build !linux

package notifications

import "os"

func rootOwned(os.FileInfo) bool                  { return true }
func openAuthority(path string) (*os.File, error) { return os.Open(path) }
