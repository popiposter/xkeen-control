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
	"sort"
	"strings"
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

type proofSnapshot struct {
	appliance Appliance
	registry  nodes.Registry
	inputs    map[string][]byte
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
// complete temporary candidate validates and all proof inputs remain unchanged
// is appliance.json atomically created. It never writes an active runtime file
// and never restarts Xray/XKeen.
func (s *Service) Adopt(ctx context.Context) error {
	if s == nil {
		return errors.New("appliance service unavailable")
	}
	if err := requireAbsent(s.config.AppliancePath); err != nil {
		return err
	}
	before, err := s.captureProofSnapshot()
	if err != nil {
		return err
	}
	if err := s.validateRenderedCandidate(ctx, before.appliance, before.registry); err != nil {
		return err
	}
	if err := s.verifyRenderedPolicy(before.appliance); err != nil {
		return err
	}
	after, err := s.captureProofSnapshot()
	if err != nil {
		return err
	}
	if !sameProofInputs(before.inputs, after.inputs) {
		return errors.New("active appliance proof changed during adoption")
	}
	contents, err := MarshalCanonical(before.appliance)
	if err != nil {
		return errors.New("unable to encode appliance authority")
	}
	if err := writeAuthorityExclusive(s.config.AppliancePath, contents); err != nil {
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
// The destination is a fresh product-owned child under the process temporary
// directory; arbitrary filesystem destinations are intentionally unsupported.
func (s *Service) Render(outputDir string) (err error) {
	if s == nil {
		return errors.New("appliance service unavailable")
	}
	output, err := validateRenderDestination(outputDir, s.config)
	if err != nil {
		return err
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
	root := filepath.Dir(output)
	if err := ensureCandidateRoot(root); err != nil {
		return err
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("render output already exists")
		}
		return errors.New("unable to create render output")
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(output)
		}
	}()
	if err := writeRenderTree(output, files); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Service) captureProofSnapshot() (proofSnapshot, error) {
	inputs := make(map[string][]byte, 11)
	read := func(key, path string, limit int) ([]byte, error) {
		contents, err := readRegularFile(path, limit)
		if err != nil {
			return nil, err
		}
		inputs[key] = bytes.Clone(contents)
		return contents, nil
	}

	dns, err := read("xray/02_dns.json", filepath.Join(s.config.ConfigDir, "02_dns.json"), MaxDocumentSize)
	if err != nil {
		return proofSnapshot{}, errors.New("active DNS policy is unavailable")
	}
	routing, err := read("xray/05_routing.json", filepath.Join(s.config.ConfigDir, "05_routing.json"), MaxDocumentSize)
	if err != nil {
		return proofSnapshot{}, errors.New("active routing policy is unavailable")
	}
	observatory, err := read("xray/07_observatory.json", filepath.Join(s.config.ConfigDir, "07_observatory.json"), MaxDocumentSize)
	if err != nil {
		return proofSnapshot{}, errors.New("active Observatory policy is unavailable")
	}
	value, err := parseActivePolicyBytes(dns, routing, observatory)
	if err != nil {
		return proofSnapshot{}, err
	}

	for _, path := range fixedTemplatePaths {
		var activePath string
		if filepath.Base(filepath.Dir(path)) == "xray" {
			activePath = filepath.Join(s.config.ConfigDir, filepath.Base(path))
		} else {
			activePath = s.config.XkeenConfigPath
		}
		active, err := read(path, activePath, MaxDocumentSize)
		if err != nil {
			return proofSnapshot{}, errors.New("fixed compatibility file is unavailable")
		}
		template, err := compatibilityTemplate(path)
		if err != nil || !semanticJSONEqual(active, template) {
			return proofSnapshot{}, errors.New("fixed compatibility file drift detected")
		}
	}

	nodeContents, err := read("nodes.json", s.config.NodesPath, nodes.MaxLegacyDocument)
	if err != nil {
		return proofSnapshot{}, errors.New("node registry is unavailable")
	}
	var registry nodes.Registry
	if err := json.Unmarshal(nodeContents, &registry); err != nil || registry.Validate() != nil {
		return proofSnapshot{}, errors.New("node registry is invalid")
	}
	rendered, err := nodes.Render(registry)
	if err != nil {
		return proofSnapshot{}, errors.New("node outbound render failed")
	}
	activeOutbounds, err := read("xray/04_outbounds.json", s.config.ActiveOutboundsPath, nodes.MaxLegacyDocument)
	if err != nil {
		return proofSnapshot{}, errors.New("active generated outbounds are unavailable")
	}
	if !bytes.Equal(rendered, activeOutbounds) {
		return proofSnapshot{}, errors.New("active generated outbounds do not match node registry")
	}
	return proofSnapshot{appliance: value, registry: registry, inputs: inputs}, nil
}

func sameProofInputs(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		other, ok := right[key]
		if !ok || !bytes.Equal(value, other) {
			return false
		}
	}
	return true
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
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > int64(limit) {
		return nil, errors.New("file is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() < 0 || opened.Size() > int64(limit) || !os.SameFile(before, opened) {
		return nil, errors.New("file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(contents) > limit {
		return nil, errors.New("file exceeds bounded size")
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || after.Size() != int64(len(contents)) {
		return nil, errors.New("file changed while reading")
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

func writeAuthorityExclusive(path string, contents []byte) error {
	if path == "" {
		return errors.New("empty authority path")
	}
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	if err := requireAbsent(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".xkeen-appliance-")
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
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		_ = os.Remove(path)
		return err
	}
	return directory.Close()
}

func validateRenderDestination(outputDir string, config Config) (string, error) {
	if outputDir == "" {
		return "", errors.New("render output directory is required")
	}
	output, err := filepath.Abs(outputDir)
	if err != nil {
		return "", errors.New("render output path is invalid")
	}
	output = filepath.Clean(output)
	root, err := filepath.Abs(filepath.Join(os.TempDir(), "xkeen-control"))
	if err != nil {
		return "", errors.New("render root is unavailable")
	}
	root = filepath.Clean(root)
	if filepath.Dir(output) != root || !strings.HasPrefix(filepath.Base(output), "candidate-") {
		return "", errors.New("render output is outside the bounded candidate root")
	}
	for _, live := range []string{
		config.ConfigDir,
		filepath.Dir(config.XkeenConfigPath),
		filepath.Dir(config.AppliancePath),
		filepath.Dir(config.NodesPath),
		filepath.Dir(config.ActiveOutboundsPath),
	} {
		if pathsOverlap(output, live) {
			return "", errors.New("render output overlaps runtime state")
		}
	}
	if _, err := os.Lstat(output); err == nil {
		return "", errors.New("render output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("render output is unavailable")
	}
	return output, nil
}

func ensureCandidateRoot(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("unable to create candidate root")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("candidate root is unsafe")
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return true
	}
	leftAbs = filepath.Clean(leftAbs)
	rightAbs = filepath.Clean(rightAbs)
	return pathWithin(leftAbs, rightAbs) || pathWithin(rightAbs, leftAbs)
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

func writeRenderTree(root string, files map[string][]byte) error {
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("render root is unsafe")
	}
	keys := make([]string, 0, len(files))
	for relative := range files {
		keys = append(keys, relative)
	}
	sort.Strings(keys)
	for _, relative := range keys {
		contents := files[relative]
		relativePath := filepath.FromSlash(relative)
		joinedPath := filepath.Join(root, relativePath)
		if relative == "" || filepath.IsAbs(relativePath) || filepath.Clean(relativePath) != relativePath || relativePath == "." || filepath.Clean(joinedPath) != joinedPath || !pathWithin(joinedPath, root) {
			return errors.New("invalid render path")
		}
		if err := makePrivateChildDir(root, filepath.Dir(joinedPath)); err != nil {
			return err
		}
		if err := writePrivateFileExclusive(joinedPath, contents); err != nil {
			return err
		}
	}
	return nil
}

func makePrivateChildDir(root, path string) error {
	if path == root {
		return nil
	}
	if !pathWithin(path, root) {
		return errors.New("render directory escapes candidate root")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || strings.Contains(relative, string(os.PathSeparator)) {
		return errors.New("invalid render directory")
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return errors.New("unable to create render directory")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("render directory is unsafe")
	}
	return nil
}

func writePrivateFileExclusive(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("render target already exists or is unavailable")
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
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
