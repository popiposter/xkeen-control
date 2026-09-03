//go:build windows

package components

import "errors"

// The production qualification target is Linux/Keenetic. Windows fixtures
// inject AvailableSpace; this conservative implementation keeps the package
// buildable without introducing a second platform-specific filesystem API.
func availableFreeSpace(path string) (uint64, error) {
	if path == "" {
		return 0, errors.New("free-space path is not configured")
	}
	return ^uint64(0), nil
}
