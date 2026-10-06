package xkeen

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

var ErrConfig = errors.New("native configuration unavailable or changed")

// ConfigEditor operates on fixed native data files. It has no raw file API and
// does not generate appliance.json or change unrelated native policy.
type ConfigEditor struct {
	Dir          string
	XrayBinary   string
	Lease        *authority.Lease
	PreviousDir  string
	AssetDir     string
	DraftDir     string
	ProcRoot     string
	RegistryPath string
	// Optional derived-data validation; never changes a running DNS service.
	ValidateDerived func(context.Context, map[string][]byte) error
}

type ConfigSnapshot struct {
	Digest string
	files  map[string][]byte
}

// NativeDocuments is internal/private integration input, never a status response.
func (s ConfigSnapshot) NativeDocuments() map[string][]byte {
	files := map[string][]byte{}
	for name, data := range s.files {
		if name != registryConfigID {
			files[name] = append([]byte(nil), data...)
		}
	}
	return files
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
	}
	if len(names) == 0 {
		return result, ErrConfig
	}
	if e.RegistryPath != "" {
		data, err := e.registryBytes()
		if err != nil || total+len(data) > 8<<20 {
			return result, ErrConfig
		}
		result.files[registryConfigID] = data
	}
	result.Digest = configDigest(result.files)
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
	return e.saveCandidate(ctx, before, map[string][]byte{file: candidate})
}

// SaveTexts validates the complete set once, then saves the selected fixed data
// files together. It never restarts XKeen or silently includes unfinished drafts.
func (e *ConfigEditor) SaveTexts(ctx context.Context, baseline string, texts map[string]string) (string, error) {
	release, err := e.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return "", err
	}
	defer release()
	before, err := e.Snapshot(ctx)
	if err != nil || before.Digest != baseline || len(texts) == 0 || len(texts) > 7 {
		return "", ErrConfig
	}
	changes := map[string][]byte{}
	for name, text := range texts {
		if !editableConfig(name) || before.files[name] == nil {
			return "", ErrConfig
		}
		changes[name] = []byte(text)
	}
	return e.saveCandidate(ctx, before, changes)
}
