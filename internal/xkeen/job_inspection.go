package xkeen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ConfigureRecovery is installed configuration, never an HTTP path or command.
func (m *Jobs) ConfigureRecovery(discovery Discovery, nodePendingPath string) {
	m.inspectRecovery = func(ctx context.Context) error {
		if _, err := os.Lstat(nodePendingPath); !errors.Is(err, os.ErrNotExist) {
			return ErrJob
		}
		facts := discovery.Inspect(ctx)
		if ctx.Err() != nil || facts.Installation != CapabilityAvailable || facts.Configuration != CapabilityAvailable {
			return ErrJob
		}
		entries, err := discovery.entries("proc", 4096)
		if err != nil {
			return ErrJob
		}
		for _, entry := range entries {
			if !entry.IsDir() || !isDigits(entry.Name()) {
				continue
			}
			data, state := discovery.read(filepath.ToSlash(filepath.Join("proc", entry.Name(), "cmdline")), 8192)
			if state == CapabilityMissing {
				continue
			} // Process exited during inspection.
			if state != CapabilityAvailable {
				return ErrJob
			}
			for _, arg := range strings.Split(string(data), "\x00") {
				if arg == m.Binary || arg == "/opt/etc/init.d/S05xkeen" || strings.HasPrefix(arg, "/opt/sbin/.xkeen/") {
					return ErrJob
				}
			}
		}
		return nil
	}
}
