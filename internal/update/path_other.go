//go:build !linux

package update

func protectedPath(string) bool      { return false }
func protectedDirectory(string) bool { return false }
