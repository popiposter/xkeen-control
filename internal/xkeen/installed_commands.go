package xkeen

import (
	"io"
	"os"
	"strings"
)

// InstalledCatalog inspects literal dispatcher branches without executing its
// startup self-heal logic. It is feature discovery, not a whole-tree pin.
func (m *Jobs) InstalledCatalog() []CommandSpec {
	all := CommandCatalog()
	if !m.checkInstalled {
		return all
	}
	available := make([]CommandSpec, 0, len(all))
	flags := m.installedFlags()
	for _, spec := range all {
		if flags[spec.flag] {
			available = append(available, spec)
		}
	}
	return available
}

func (m *Jobs) supportsFlag(flag string) bool {
	return m.installedFlags()[flag]
}

func (m *Jobs) installedFlags() map[string]bool {
	flags := map[string]bool{}
	f, err := os.Open(m.Binary)
	if err != nil {
		return flags
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > maxNativeSource {
		return flags
	}
	data, err := io.ReadAll(io.LimitReader(f, maxNativeSource+1))
	if err != nil || len(data) > maxNativeSource {
		return flags
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		end := strings.IndexByte(line, ')')
		if end < 0 {
			continue
		}
		for _, alias := range strings.Split(line[:end], "|") {
			alias = strings.TrimSpace(alias)
			if strings.HasPrefix(alias, "-") {
				flags[alias] = true
			}
		}
	}
	return flags
}

// RequireInstalledCommands is enabled by the installed main, not request input.
func (m *Jobs) RequireInstalledCommands() { m.checkInstalled = true }
