package xkeen

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// VerifySetupRuntime is readback for the explicit initial setup owner. It does
// not clear pending state, replay a native action or replace InspectApplied.
func (e *ConfigEditor) VerifySetupRuntime(ctx context.Context, expected string) error {
	s, e1 := e.Snapshot(ctx)
	if e1 != nil || s.Digest != expected {
		return ErrConfig
	}
	if e.validateSet(ctx, s) != nil {
		return ErrConfig
	}
	p, e1 := e.configProcess(ctx)
	if e1 != nil || p == "" {
		return ErrConfig
	}
	again, e1 := e.Snapshot(ctx)
	p2, e2 := e.configProcess(ctx)
	if e1 != nil || e2 != nil || again.Digest != expected || p2 != p {
		return ErrConfig
	}
	return nil
}

// VerifySetupStopped requires positive process absence, not a missing comm or
// an unavailable /proc read, on both sides of exact saved-generation readback.
func (e *ConfigEditor) VerifySetupStopped(ctx context.Context, expected string) error {
	s, err := e.Snapshot(ctx)
	if err != nil || s.Digest != expected {
		return ErrConfig
	}
	p, err := e.configProcess(ctx)
	if err != nil || p != "" {
		return ErrConfig
	}
	again, err := e.Snapshot(ctx)
	p2, e2 := e.configProcess(ctx)
	if err != nil || e2 != nil || p2 != "" || again.Digest != expected {
		return ErrConfig
	}
	return nil
}

// Empty means positive absence. Read/resource/multiple-process ambiguity is an
// error. A comm/name-only observation is never accepted as successful Apply.
func (e *ConfigEditor) configProcess(ctx context.Context) (string, error) {
	root := e.ProcRoot
	if root == "" {
		root = "/proc"
	}
	dir, err := os.Open(root)
	if err != nil {
		return "", ErrConfig
	}
	defer dir.Close()
	entries, err := dir.ReadDir(4097)
	if err != nil && err != io.EOF || len(entries) > 4096 {
		return "", ErrConfig
	}
	executable, err := filepath.EvalSymlinks(e.XrayBinary)
	if err != nil {
		return "", ErrConfig
	}
	configDir, err := filepath.EvalSymlinks(e.Dir)
	if err != nil {
		return "", ErrConfig
	}
	result := ""
	d := Discovery{Root: root}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return "", ErrConfig
		}
		if !isDigits(entry.Name()) {
			continue
		}
		name := entry.Name()
		comm, state := d.read(name+"/comm", 256)
		if state != CapabilityAvailable {
			if _, err := os.Lstat(filepath.Join(root, name)); os.IsNotExist(err) {
				continue
			}
			return "", ErrConfig
		}
		if strings.TrimSpace(string(comm)) != "xray" {
			continue
		}
		if result != "" {
			return "", ErrConfig
		}
		path, err := os.Readlink(filepath.Join(root, name, "exe"))
		if err != nil || filepath.Clean(path) != filepath.Clean(executable) {
			return "", ErrConfig
		}
		command, state := d.read(name+"/cmdline", 8192)
		if state != CapabilityAvailable {
			return "", ErrConfig
		}
		args := strings.Split(strings.TrimRight(string(command), "\x00"), "\x00")
		if len(args) < 2 || args[1] != "run" && !strings.HasPrefix(args[1], "-") {
			return "", ErrConfig
		}
		confdirs := 0
		for index, arg := range args {
			flag := strings.SplitN(strings.TrimLeft(arg, "-"), "=", 2)[0]
			if strings.HasPrefix(arg, "-") && (flag == "test" || flag == "dump" || flag == "version" || flag == "help" || flag == "h") {
				return "", ErrConfig
			}
			if strings.HasPrefix(arg, "-") && (flag == "config" || flag == "c") {
				return "", ErrConfig
			}
			if !strings.HasPrefix(arg, "-") || flag != "confdir" {
				continue
			}
			value := ""
			if !strings.Contains(arg, "=") && index+1 < len(args) {
				value = args[index+1]
			}
			if _, after, found := strings.Cut(arg, "="); found {
				value = after
			}
			if value == "" {
				return "", ErrConfig
			}
			actual, err := filepath.EvalSymlinks(value)
			if err != nil || filepath.Clean(actual) != filepath.Clean(configDir) {
				return "", ErrConfig
			}
			confdirs++
		}
		// Stock XKeen starts `xray run` and exports the config directory instead
		// of passing -confdir. Inspect only this bounded process environment;
		// never project its private contents into status or diagnostics.
		if confdirs == 0 {
			environment, state := d.read(name+"/environ", 64<<10)
			if state != CapabilityAvailable {
				return "", ErrConfig
			}
			for _, assignment := range strings.Split(string(environment), "\x00") {
				value, found := strings.CutPrefix(assignment, "XRAY_LOCATION_CONFDIR=")
				if !found {
					continue
				}
				actual, err := filepath.EvalSymlinks(value)
				if err != nil || filepath.Clean(actual) != filepath.Clean(configDir) {
					return "", ErrConfig
				}
				confdirs++
			}
		}
		if confdirs != 1 {
			return "", ErrConfig
		}
		stat, state := d.read(name+"/stat", 4096)
		if state != CapabilityAvailable {
			return "", ErrConfig
		}
		closing := strings.LastIndex(string(stat), ")")
		if closing < 0 {
			return "", ErrConfig
		}
		fields := strings.Fields(string(stat[closing+1:]))
		if len(fields) <= 19 || !isDigits(fields[19]) || fields[0] == "Z" || fields[0] == "X" {
			return "", ErrConfig
		}
		result = name + ":" + fields[19]
	}
	return result, nil
}
