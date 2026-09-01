package nodes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// MaxRegistryDocument is the largest canonical authoritative registry that
// the control plane will read or write. Backup export uses the same bound.
const MaxRegistryDocument = 4 << 20

type Store struct {
	Path string
}

func (s Store) Load() (Registry, error) {
	if s.Path == "" {
		return Registry{}, errors.New("registry path is not configured")
	}
	file, err := os.Open(s.Path)
	if err != nil {
		return Registry{}, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, MaxRegistryDocument+1))
	if err != nil || len(contents) > MaxRegistryDocument {
		return Registry{}, errors.New("registry exceeds bounded size")
	}
	var registry Registry
	if err := json.Unmarshal(contents, &registry); err != nil {
		return Registry{}, errors.New("invalid registry JSON")
	}
	if err := registry.Validate(); err != nil {
		return Registry{}, errors.New("invalid registry")
	}
	return registry, nil
}

func (s Store) Save(registry Registry) error {
	contents, err := MarshalCanonical(registry)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(filepath.Dir(s.Path)); err != nil {
		return err
	}
	return atomicWrite(s.Path, contents, 0o600)
}

// MarshalCanonical returns the deterministic, newline-terminated registry
// serialization used by the store and by the secret backup section.
func MarshalCanonical(registry Registry) ([]byte, error) {
	if err := registry.Validate(); err != nil {
		return nil, err
	}
	contents, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return nil, errors.New("unable to encode registry")
	}
	contents = append(contents, '\n')
	if len(contents) > MaxRegistryDocument {
		return nil, errors.New("registry exceeds bounded size")
	}
	return contents, nil
}

// ParseCanonical strictly decodes a bounded authoritative registry.
func ParseCanonical(contents []byte) (Registry, error) {
	if len(contents) == 0 || len(contents) > MaxRegistryDocument {
		return Registry{}, errors.New("registry exceeds bounded size")
	}
	var wire strictRegistry
	if err := decodeStrictJSON(contents, &wire); err != nil {
		return Registry{}, errors.New("invalid registry JSON")
	}
	registry := Registry{SchemaVersion: wire.SchemaVersion, Nodes: wire.Nodes}
	if wire.Subscriptions != nil {
		registry.Subscriptions = make([]Subscription, len(wire.Subscriptions))
		for index, subscription := range wire.Subscriptions {
			registry.Subscriptions[index] = Subscription{
				ID: subscription.ID, Name: subscription.Name, URL: subscription.URL,
				Enabled: subscription.Enabled == nil || *subscription.Enabled,
			}
		}
	}
	if err := registry.Validate(); err != nil {
		return Registry{}, errors.New("invalid registry")
	}
	return registry, nil
}

// strictRegistry is only the backup/import wire shape. Production Store.Load
// intentionally retains its compatibility decoder, including the historical
// missing-subscription-enabled default and ignored additive fields.
type strictRegistry struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Nodes         []Node               `json:"nodes"`
	Subscriptions []strictSubscription `json:"subscriptions,omitempty"`
}

type strictSubscription struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled"`
}

func decodeStrictJSON(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func ReadBoundedFile(path string, max int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, int64(max)+1))
	if err != nil || len(contents) > max {
		return nil, errors.New("file exceeds bounded size")
	}
	return contents, nil
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	if path == "" {
		return errors.New("empty write path")
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".xkeen-node-*")
	if err != nil {
		return errors.New("unable to create atomic temporary file")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return errors.New("unable to set atomic file mode")
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return errors.New("unable to write atomic file")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("unable to sync atomic file")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("unable to close atomic file")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("unable to replace atomic file")
	}
	_ = os.Chmod(path, mode)
	return nil
}

func ensurePrivateDir(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("unable to create private directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return errors.New("unable to protect private directory")
	}
	return nil
}

func cloneRegistry(registry Registry) (Registry, error) {
	contents, err := json.Marshal(registry)
	if err != nil {
		return Registry{}, errors.New("unable to clone registry")
	}
	var result Registry
	if err := json.Unmarshal(contents, &result); err != nil {
		return Registry{}, errors.New("unable to clone registry")
	}
	return result, nil
}
