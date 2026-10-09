//go:build linux

package resourcepolicy

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type deviceID struct{ major, minor uint32 }

func blockDeviceID(path string) (deviceID, error) {
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice != 0 {
		return deviceID{}, ErrTelemetry
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return deviceID{}, ErrTelemetry
	}
	return deviceID{unix.Major(uint64(stat.Rdev)), unix.Minor(uint64(stat.Rdev))}, nil
}

func readSwapDiskWrites(root string) (uint64, string, error) {
	return readSwapDiskWritesWith(root, blockDeviceID)
}

// The diskstats write counter is a conservative proxy for swap-out when this
// kernel omits pswpout. Only a single active block swap device is admissible.
func readSwapDiskWritesWith(root string, identify func(string) (deviceID, error)) (uint64, string, error) {
	b, err := readBounded(filepath.Join(root, "swaps"))
	if err != nil {
		return 0, "", ErrTelemetry
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || strings.Join(strings.Fields(lines[0]), " ") != "Filename Type Size Used Priority" {
		return 0, "", ErrTelemetry
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 5 || fields[1] != "partition" || !strings.HasPrefix(fields[0], "/dev/") || filepath.Clean(fields[0]) != fields[0] {
		return 0, "", ErrTelemetry
	}
	size, sizeErr := strconv.ParseUint(fields[2], 10, 64)
	used, usedErr := strconv.ParseUint(fields[3], 10, 64)
	if sizeErr != nil || usedErr != nil || size == 0 || used > size {
		return 0, "", ErrTelemetry
	}
	id, err := identify(fields[0])
	if err != nil {
		return 0, "", ErrTelemetry
	}
	b, err = readBounded(filepath.Join(root, "diskstats"))
	if err != nil {
		return 0, "", ErrTelemetry
	}
	var sectors uint64
	matches := 0
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		major, e1 := strconv.ParseUint(f[0], 10, 32)
		minor, e2 := strconv.ParseUint(f[1], 10, 32)
		if e1 != nil || e2 != nil || uint32(major) != id.major || uint32(minor) != id.minor {
			continue
		}
		matches++
		if len(f) < 14 {
			return 0, "", ErrTelemetry
		}
		sectors, err = strconv.ParseUint(f[9], 10, 64)
		if err != nil {
			return 0, "", ErrTelemetry
		}
	}
	if matches != 1 {
		return 0, "", ErrTelemetry
	}
	return sectors, "diskstats:" + strconv.FormatUint(uint64(id.major), 10) + ":" + strconv.FormatUint(uint64(id.minor), 10), nil
}
