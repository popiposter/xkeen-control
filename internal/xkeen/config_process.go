package xkeen

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
			if arg == "-config" || arg == "-c" || strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "-c=") {
				return "", ErrConfig
			}
			value := ""
			if arg == "-confdir" && index+1 < len(args) {
				value = args[index+1]
			}
			if strings.HasPrefix(arg, "-confdir=") {
				value = strings.TrimPrefix(arg, "-confdir=")
			}
			if value == "" {
				continue
			}
			actual, err := filepath.EvalSymlinks(value)
			if err != nil || filepath.Clean(actual) != filepath.Clean(configDir) {
				return "", ErrConfig
			}
			confdirs++
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
