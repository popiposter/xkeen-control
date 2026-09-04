//go:build windows

package components

import (
	"os"
	"path/filepath"
	"strings"
)

func sameFilesystem(left, right string) (bool, error) {
	if _, err := os.Stat(left); err != nil {
		return false, err
	}
	if _, err := os.Stat(right); err != nil {
		return false, err
	}
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false, err
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(filepath.VolumeName(leftAbs), filepath.VolumeName(rightAbs)), nil
}
