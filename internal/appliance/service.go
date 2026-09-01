package appliance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/popiposter/xkeen-control/internal/nodes"
)

const defaultCandidateValidationTimeout = 45 * time.Second

// CandidateValidator is deliberately a single narrow capability. The CLI
// supplies the existing nodes.CommandActivator, which runs the normal bounded
// Xray candidate validation command and does not expose a generic command API.
type CandidateValidator interface {
	ValidateCandidate(context.Context, string) error
}

type Config struct {
	AppliancePath       string
	ConfigDir           string
	XkeenConfigPath     string
	NodesPath           string
	ActiveOutboundsPath string
	Validator           CandidateValidator
	CandidateValidation time.Duration
}

type Service struct {
	config Config
}

func NewService(config Config) *Service {
	if config.CandidateValidation <= 0 {
		config.CandidateValidation = defaultCandidateValidationTimeout
	}
	return &Service{config: config}
}

// ValidateStored strictly validates only appliance.json. It has no runtime or
// persistent side effects.
func (s *Service) ValidateStored() error {
	if s == nil {
		return errors.New("appliance service unavailable")
	}
	_, err := s.loadAppliance()
	return err
}

// Adopt proves that the active policy is representable by appliance v1 and
// that the node registry/generated outbounds are coherent. Only after the
// complete temporary candidate validates is appliance.json atomically written.
// It never writes an active runtime file and never restarts Xray/XKeen.
func (s *Service) Adopt(ctx context.Context) error {
	if s == nil {
		return errors.New("appliance service unavailable")
	}
	if err := requireAbsent(s.config.AppliancePath); err != nil {
		return err
	}
	active, err := parseActivePolicy(s.config.ConfigDir)
	if err != nil {
		return err
	}
	if err := s.verifyFixedCompatibility(); err != nil {
		return err
	}
	registry, err := s.loadCoherentRegistry()
	if err != nil {
		return err
	}
	if err := s.validateRenderedCandidate(ctx, active, registry); err != nil {
		return err
	}
	if err := s.verifyRenderedPolicy(active); err != nil {
		return err
	}
	contents, err := MarshalCanonical(active)
	if err != nil {
		return errors.New("unable to encode appliance authority")
	}
	if err := writeAuthority(s.config.AppliancePath, contents); err != nil {
		return errors.New("unable to commit appliance authority")
	}
	return nil
}

// Verify re-renders the stored authority, checks every active managed/fixed
// file and the generated node artifact, then validates a complete temporary
// Xray candidate. It performs no persistent writes and no runtime lifecycle
// operation.
func (s *Service) Verify(ctx context.Context) error {
	if s == nil {
		return errors.New("appliance service unavailable")
	}
	stored, err := s.loadAppliance()
	if err != nil {
		return err
	}
	active, err := parseActivePolicy(s.config.ConfigDir)
	if err != nil {
		return err
	}
	if !canonicalEqual(stored, active) {
		return errors.New("active appliance policy drift detected")
	}
	if err := s.verifyFixedCompatibility(); err != nil {
		return err
	}
	registry, err := s.loadCoherentRegistry()
	if err != nil {
		return err
	}
	if err := s.validateRenderedCandidate(ctx, stored, registry); err != nil {
		return err
	}
	if err := s.verifyRenderedPolicy(stored); err != nil {
		return err
	}
	return nil
}

// Render writes a deterministic complete candidate tree from the stored
// appliance authority, current nodes registry and embedded fixed templates.
// The destination is an explicit candidate directory; active configured paths
// are rejected to preserve the zero-runtime-mutation boundary.
func (s *Service) Render(outputDir string) error {
	if s == nil {
		return errors.New("appliance service unavailable")
	}
	if outputDir == "" {
		return errors.New("render output directory is required")
	}
	if samePath(outputDir, s.config.ConfigDir) || samePath(outputDir, filepath.Dir(s.config.XkeenConfigPath)) || samePath(outputDir, filepath.Dir(s.config.AppliancePath)) {
		return errors.New("render output overlaps runtime state")
	}
	value, err := s.loadAppliance()
	if err != nil {
		return err
	}
	registry, err := s.loadCoherentRegistry()
	if err != nil {
		return err
	}
	files, err := renderFiles(value, registry)
	if err != nil {
		return err
	}
	return writeRenderTree(outputDir, files)
}

func (s *Service) loadAppliance() (Appliance, error) {
	contents, err := readRegularFile(s.config.AppliancePath, MaxDocumentSize)
	if err != nil {
		return Appliance{}, errors.New("appliance authority is unavailable")
	}
	value, err := Parse(contents)
	if err != nil {
		return Appliance{}, errors.New("appliance authority is invalid")
	}
	if mode, err := fileMode(s.config.AppliancePath); err != nil || (runtime.GOOS != "windows" && mode.Perm() != 0o600) {
		return Appliance{}, errors.New("appliance authority permissions are invalid")
	}
	if err := checkPrivateDir(filepath.Dir(s.config.AppliancePath)); err != nil {
		return Appliance{}, errors.New("appliance authority directory is not private")
	}
	return value, nil
}

func (s *Service) loadCoherentRegistry() (nodes.Registry, error) {
	contents, err := readRegularFile(s.config.NodesPath, nodes.MaxLegacyDocument)
	if err != nil {
		return nodes.Registry{}, errors.New("node registry is unavailable")
	}
	var registry nodes.Registry
	if err := json.Unmarshal(contents, &registry); err != nil {
		return nodes.Registry{}, errors.New("node registry is invalid")
	}
	if err := registry.Validate(); err != nil {
		return nodes.Registry{}, errors.New("node registry is invalid")
	}
	rendered, err := nodes.Render(registry)
	if err != nil {
		return nodes.Registry{}, errors.New("node outbound render failed")
	}
	active, err := readRegularFile(s.config.ActiveOutboundsPath, nodes.MaxLegacyDocument)
	if err != nil {
		return nodes.Registry{}, errors.New("active generated outbounds are unavailable")
	}
	if !bytes.Equal(rendered, active) {
		return nodes.Registry{}, errors.New("active generated outbounds do not match node registry")
	}
	return registry, nil
}

func (s *Service) verifyFixedCompatibility() error {
	for _, path := range fixedTemplatePaths {
		var activePath string
		if filepath.Base(filepath.Dir(path)) == "xray" {
			activePath = filepath.Join(s.config.ConfigDir, filepath.Base(path))
		} else {
			activePath = s.config.XkeenConfigPath
		}
		active, err := readRegularFile(activePath, MaxDocumentSize)
		if err != nil {
			return errors.New("fixed compatibility file is unavailable")
		}
		template, err := compatibilityTemplate(path)
		if err != nil || !semanticJSONEqual(active, template) {
			return errors.New("fixed compatibility file drift detected")
		}
	}
	return nil
}

func (s *Service) validateRenderedCandidate(ctx context.Context, value Appliance, registry nodes.Registry) error {
	if s.config.Validator == nil {
		return errors.New("Xray candidate validation is unavailable")
	}
	files, err := renderFiles(value, registry)
	if err != nil {
		return err
	}
	candidate, err := os.MkdirTemp("", "xkeen-appliance-candidate-")
	if err != nil {
		return errors.New("unable to create appliance candidate")
	}
	defer os.RemoveAll(candidate)
	if err := writeRenderTree(candidate, files); err != nil {
		return errors.New("unable to prepare appliance candidate")
	}
	validationContext, cancel := context.WithTimeout(ctx, s.config.CandidateValidation)
	defer cancel()
	if err := s.config.Validator.ValidateCandidate(validationContext, filepath.Join(candidate, "xray")); err != nil {
		return errors.New("appliance Xray candidate validation failed")
	}
	return nil
}

func (s *Service) verifyRenderedPolicy(value Appliance) error {
	files, err := renderPolicyFiles(value)
	if err != nil {
		return errors.New("appliance policy render failed")
	}
	rendered, err := parseActivePolicyBytes(files["xray/02_dns.json"], files["xray/05_routing.json"], files["xray/07_observatory.json"])
	if err != nil || !canonicalEqual(value, rendered) {
		return errors.New("rendered appliance policy is not semantically equivalent")
	}
	return nil
}

func requireAbsent(path string) error {
	if path == "" {
		return errors.New("appliance authority path is not configured")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("appliance authority path is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("appliance authority path is unsafe")
	}
	return errors.New("appliance authority already exists")
}

func readRegularFile(path string, limit int) ([]byte, error) {
	if path == "" || limit <= 0 {
		return nil, errors.New("file path is not configured")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > int64(limit) {
		return nil, errors.New("file is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(contents) > limit {
		return nil, errors.New("file exceeds bounded size")
	}
	return contents, nil
}

func fileMode(path string) (os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return 0, errors.New("file is not a regular file")
	}
	return info.Mode(), nil
}

func writeAuthority(path string, contents []byte) error {
	if path == "" {
		return errors.New("empty authority path")
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("authority path is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".xkeen-appliance-")
	if err != nil {
		return errors.New("unable to create authority temporary file")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func writeRenderTree(root string, files map[string][]byte) error {
	if err := ensurePrivateDir(root); err != nil {
		return err
	}
	for relative, contents := range files {
		relativePath := filepath.FromSlash(relative)
		joinedPath := filepath.Join(root, relativePath)
		if relative == "" || filepath.IsAbs(relativePath) || filepath.Clean(relativePath) != relativePath || relativePath == "." || filepath.Clean(joinedPath) != joinedPath {
			return errors.New("invalid render path")
		}
		path := joinedPath
		if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
			return err
		}
		if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
			return errors.New("render target is unsafe")
		}
		if err := writePrivateFile(path, contents); err != nil {
			return err
		}
	}
	return nil
}

func writePrivateFile(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".xkeen-appliance-render-")
	if err != nil {
		return errors.New("unable to create render temporary file")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ensurePrivateDir(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("unable to create private directory")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("private directory is unsafe")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return errors.New("unable to protect private directory")
	}
	return nil
}

func checkPrivateDir(path string) error {
	if path == "" || path == "." {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("private directory is unsafe")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errors.New("private directory is not root-only")
	}
	return nil
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
