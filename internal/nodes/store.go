package nodes

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxRegistryDocument = 4 << 20

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
	contents, err := io.ReadAll(io.LimitReader(file, maxRegistryDocument+1))
	if err != nil || len(contents) > maxRegistryDocument {
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
	if err := registry.Validate(); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return errors.New("unable to encode registry")
	}
	contents = append(contents, '\n')
	if len(contents) > maxRegistryDocument {
		return errors.New("registry exceeds bounded size")
	}
	if err := ensurePrivateDir(filepath.Dir(s.Path)); err != nil {
		return err
	}
	return atomicWrite(s.Path, contents, 0o600)
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
