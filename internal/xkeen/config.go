package xkeen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

var ErrConfig = errors.New("native configuration unavailable or changed")

// ConfigEditor operates on fixed native data files. It has no raw file API and
// does not generate appliance.json or change unrelated native policy.
type ConfigEditor struct {
	Dir         string
	XrayBinary  string
	Lease       *authority.Lease
	PreviousDir string
	AssetDir    string
	DraftDir    string
}

type ConfigSnapshot struct {
	Digest string
	files  map[string][]byte
}

// Snapshot retains bounded native bytes locally; HTTP projections are typed.
func (e *ConfigEditor) Snapshot(ctx context.Context) (ConfigSnapshot, error) {
	result := ConfigSnapshot{files: map[string][]byte{}}
	dir, err := os.Open(e.Dir)
	if err != nil {
		return result, ErrConfig
	}
	defer dir.Close()
	entries, err := dir.ReadDir(maxNativeConfigFiles + 1)
	if err != nil && err != io.EOF || len(entries) > maxNativeConfigFiles {
		return result, ErrConfig
	}
	var names []string
	for _, entry := range entries {
		switch filepath.Ext(entry.Name()) {
		case ".toml", ".yaml", ".yml":
			return result, ErrConfig
		case ".json", ".jsonc":
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	digest := sha256.New()
	total := 0
	for _, name := range names {
		if ctx.Err() != nil {
			return result, ErrConfig
		}
		d := Discovery{Root: e.Dir}
		data, state := d.read(name, maxNativeConfig)
		if state != CapabilityAvailable {
			return result, ErrConfig
		}
		if _, err := configjson.DecodeObject(data); err != nil {
			return result, ErrConfig
		}
		total += len(data)
		if total > 8<<20 {
			return result, ErrConfig
		}
		result.files[name] = data
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		digest.Write(data)
		digest.Write([]byte{0})
	}
	if len(names) == 0 {
		return result, ErrConfig
	}
	result.Digest = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

// SaveField validates the complete Xray candidate and edits exactly one fixed
// field/file. Unknown siblings and all other native files remain byte-for-byte.
func (e *ConfigEditor) SaveField(ctx context.Context, baseline, area, field string, value any) (string, error) {
	file := ""
	allowed := false
	switch area {
	case "dns":
		file = "02_dns.json"
		allowed = field == "queryStrategy" || field == "disableCache" || field == "disableFallback"
	case "observatory":
		file = "07_observatory.json"
		allowed = field == "probeInterval" || field == "enableConcurrency"
	case "routing":
		file = "05_routing.json"
		allowed = field == "domainStrategy"
	}
	if !allowed {
		return "", ErrConfig
	}
	return e.saveDocument(ctx, baseline, file, func(original []byte) ([]byte, error) {
		return configjson.ReplacePath(original, []string{area, field}, value)
	})
}

// SaveText is a private fixed-ID editor, never an arbitrary file writer.
func (e *ConfigEditor) SaveText(ctx context.Context, baseline, file, text string) (string, error) {
	if !editableConfig(file) || len(text) > maxNativeConfig {
		return "", ErrConfig
	}
	return e.saveDocument(ctx, baseline, file, func([]byte) ([]byte, error) {
		data := []byte(text)
		_, err := configjson.DecodeObject(data)
		return data, err
	})
}

func (e *ConfigEditor) saveDocument(ctx context.Context, baseline, file string, edit func([]byte) ([]byte, error)) (string, error) {
	release, err := e.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return "", err
	}
	defer release()
	before, err := e.Snapshot(ctx)
	if err != nil || before.Digest != baseline {
		return "", ErrConfig
	}
	original, ok := before.files[file]
	if !ok {
		return "", ErrConfig
	}
	candidate, err := edit(original)
	if err != nil {
		return "", ErrConfig
	}
	if string(candidate) == string(original) {
		return before.Digest, nil
	}
	tmp, err := os.MkdirTemp("", "xkeen-native-config-*")
	if err != nil {
		return "", ErrConfig
	}
	defer os.RemoveAll(tmp)
	for name, data := range before.files {
		if name == file {
			data = candidate
		}
		if os.WriteFile(filepath.Join(tmp, name), data, 0600) != nil {
			return "", ErrConfig
		}
	}
	validateCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(validateCtx, e.XrayBinary, "run", "-test", "-confdir", tmp)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "XRAY_LOCATION_ASSET=") {
			command.Env = append(command.Env, entry)
		}
	}
	if e.AssetDir != "" {
		command.Env = append(command.Env, "XRAY_LOCATION_ASSET="+e.AssetDir)
	}
	// Returned only by the authenticated private editor, never public logging.
	output := &validationOutput{}
	command.Stdout, command.Stderr = output, output
	if command.Run() != nil {
		return "", &ValidationError{File: file, Output: string(output.data), Truncated: output.truncated}
	}
	current, err := e.Snapshot(ctx)
	if err != nil || current.Digest != baseline {
		return "", ErrConfig
	}
	if e.stagePending(before, file, candidate) != nil {
		return "", ErrConfig
	}
	path := filepath.Join(e.Dir, file)
	f, err := os.CreateTemp(e.Dir, ".panel-config-*")
	if err != nil {
		return "", ErrConfig
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(candidate); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return "", ErrConfig
	}
	// Final baseline check immediately precedes promotion. External CLI/cron
	// concurrency is not serialized by the panel lease.
	current, err = e.Snapshot(ctx)
	if err != nil || current.Digest != baseline {
		return "", ErrConfig
	}
	if os.Rename(f.Name(), path) != nil {
		return "", ErrConfig
	}
	d, err := os.Open(e.Dir)
	if err != nil {
		return "", ErrConfig
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		return "", ErrConfig
	}
	readbackCtx, readbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readbackCancel()
	after, err := e.Snapshot(readbackCtx)
	if err != nil {
		return "", ErrConfig
	}
	if len(after.files) != len(before.files) {
		return "", ErrConfig
	}
	for name, data := range before.files {
		if name == file {
			data = candidate
		}
		if !bytes.Equal(data, after.files[name]) {
			return "", ErrConfig
		}
	}
	return after.Digest, nil
}
