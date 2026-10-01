//go:build !linux

package auth

import "os"

func passwordRootOwned(os.FileInfo) bool                  { return true }
func openPasswordAuthority(path string) (*os.File, error) { return os.Open(path) }
