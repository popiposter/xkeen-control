package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	Product               = "xkeen-control"
	ManifestSchema        = 1
	UpdaterGeneration     = 1
	SupportedOS           = "linux"
	SupportedArchitecture = "arm64"
)

var RequiredArtifacts = []string{
	"S99xkeen-control",
	"install.sh",
	"xkeen-control-linux-arm64",
	"xkeen-control-updater",
}

// StablePublicKeyHex is the source-pinned production release trust anchor.
// The matching private key must remain confined to the protected release environment.
const StablePublicKeyHex = "70cf46c44b6c3598e68b6100c2f67b101d1a5d6905dd0dc5a8e2a2d48dc25e8b"

type Artifact struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Compatibility struct {
	StateSchemaMin          int  `json:"stateSchemaMin"`
	StateSchemaMax          int  `json:"stateSchemaMax"`
	UpdaterGeneration       int  `json:"updaterGeneration"`
	ManualMigrationRequired bool `json:"manualMigrationRequired"`
	RollbackCompatible      bool `json:"rollbackCompatible"`
}

type Manifest struct {
	SchemaVersion   int           `json:"schemaVersion"`
	Product         string        `json:"product"`
	Version         string        `json:"version"`
	Channel         string        `json:"channel"`
	SourceCommit    string        `json:"sourceCommit"`
	SourceDateEpoch int64         `json:"sourceDateEpoch"`
	OS              string        `json:"os"`
	Architecture    string        `json:"architecture"`
	Artifacts       []Artifact    `json:"artifacts"`
	Compatibility   Compatibility `json:"compatibility"`
}

type Candidate struct {
	Manifest  Manifest
	Signature []byte
	Assets    map[string][]byte
}

func BuildManifest(version, commit, channel string, sourceDateEpoch int64, artifactPaths map[string]string) (Manifest, error) {
	if sourceDateEpoch <= 0 {
		return Manifest{}, errors.New("source date epoch must be positive")
	}
	items := make([]Artifact, 0, len(artifactPaths))
	for name, path := range artifactPaths {
		if !isRequiredArtifact(name) {
			return Manifest{}, fmt.Errorf("unexpected artifact %q", name)
		}
		info, err := os.Stat(path)
		if err != nil {
			return Manifest{}, errors.New("artifact is unavailable")
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return Manifest{}, fmt.Errorf("artifact %q is not a non-empty regular file", name)
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return Manifest{}, errors.New("artifact hash unavailable")
		}
		items = append(items, Artifact{Name: name, Size: info.Size(), SHA256: hash})
	}
	sort.Slice(items, func(a, b int) bool { return items[a].Name < items[b].Name })
	manifest := Manifest{
		SchemaVersion: ManifestSchema, Product: Product, Version: version, Channel: channel,
		SourceCommit: commit, SourceDateEpoch: sourceDateEpoch, OS: SupportedOS, Architecture: SupportedArchitecture,
		Artifacts:     items,
		Compatibility: Compatibility{UpdaterGeneration: UpdaterGeneration, RollbackCompatible: true},
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchema || m.Product != Product || m.OS != SupportedOS || m.Architecture != SupportedArchitecture {
		return errors.New("manifest identity is unsupported")
	}
	if !validSemver(m.Version) {
		return errors.New("manifest version is invalid")
	}
	if m.Channel != "stable" && m.Channel != "beta" {
		return errors.New("manifest channel is invalid")
	}
	if m.Channel == "stable" && strings.Contains(m.Version, "-") {
		return errors.New("stable manifest cannot be prerelease")
	}
	if m.Channel == "beta" && !strings.Contains(m.Version, "-") {
		return errors.New("beta manifest must be prerelease")
	}
	if len(m.SourceCommit) != 40 || !isLowerHex(m.SourceCommit) || m.SourceDateEpoch <= 0 {
		return errors.New("manifest source provenance is invalid")
	}
	if m.Compatibility.UpdaterGeneration != UpdaterGeneration || m.Compatibility.StateSchemaMin < 0 || m.Compatibility.StateSchemaMax < m.Compatibility.StateSchemaMin {
		return errors.New("manifest compatibility is invalid")
	}
	seen := make(map[string]struct{}, len(m.Artifacts))
	previous := ""
	for _, item := range m.Artifacts {
		if !isRequiredArtifact(item.Name) {
			return fmt.Errorf("manifest contains unexpected artifact %q", item.Name)
		}
		if _, ok := seen[item.Name]; ok {
			return fmt.Errorf("manifest contains duplicate artifact %q", item.Name)
		}
		if previous != "" && previous >= item.Name {
			return errors.New("manifest artifacts are not in deterministic order")
		}
		if item.Size <= 0 || len(item.SHA256) != sha256.Size*2 || !isLowerHex(item.SHA256) {
			return fmt.Errorf("manifest artifact %q has invalid hash or size", item.Name)
		}
		seen[item.Name] = struct{}{}
		previous = item.Name
	}
	for _, required := range RequiredArtifacts {
		if _, ok := seen[required]; !ok {
			return fmt.Errorf("manifest is missing required artifact %q", required)
		}
	}
	if len(seen) != len(RequiredArtifacts) {
		return errors.New("manifest contains an unsupported artifact")
	}
	return nil
}

func (m Manifest) MarshalDeterministic() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	contents, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

func ParseManifest(contents []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, errors.New("manifest JSON is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, errors.New("manifest has trailing data")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Sign(manifest []byte, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifest)) + "\n"), nil
}

func Verify(manifest, encodedSignature []byte, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("release signing public key is not configured")
	}
	value := strings.TrimSpace(string(encodedSignature))
	signature, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, manifest, signature) {
		return errors.New("release manifest signature is invalid")
	}
	return nil
}

func DecodePrivateKey(contents []byte) (ed25519.PrivateKey, error) {
	contents = bytes.TrimSpace(contents)
	if decoded, err := base64.StdEncoding.DecodeString(string(contents)); err == nil && len(decoded) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(decoded), nil
	}
	if len(contents) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(append([]byte(nil), contents...)), nil
	}
	return nil, errors.New("private key must be base64 or 64 raw bytes")
}

func DecodePublicKey(contents []byte) (ed25519.PublicKey, error) {
	contents = bytes.TrimSpace(contents)
	if decoded, err := base64.StdEncoding.DecodeString(string(contents)); err == nil && len(decoded) == ed25519.PublicKeySize {
		return ed25519.PublicKey(decoded), nil
	}
	if decoded, err := hex.DecodeString(string(contents)); err == nil && len(decoded) == ed25519.PublicKeySize {
		return ed25519.PublicKey(decoded), nil
	}
	if len(contents) == ed25519.PublicKeySize {
		return ed25519.PublicKey(append([]byte(nil), contents...)), nil
	}
	return nil, errors.New("public key must be base64, hex or 32 raw bytes")
}

func HashBytes(contents []byte) string {
	hash := sha256.Sum256(contents)
	return hex.EncodeToString(hash[:])
}

func VerifyCandidate(candidate Candidate) error {
	if err := candidate.Manifest.Validate(); err != nil {
		return err
	}
	if len(candidate.Assets) != len(RequiredArtifacts) {
		return errors.New("candidate has an unexpected asset set")
	}
	for _, item := range candidate.Manifest.Artifacts {
		contents, ok := candidate.Assets[item.Name]
		if !ok || int64(len(contents)) != item.Size || HashBytes(contents) != item.SHA256 {
			return fmt.Errorf("candidate artifact %q failed size or hash verification", item.Name)
		}
	}
	return nil
}

func isRequiredArtifact(name string) bool {
	for _, required := range RequiredArtifacts {
		if name == required {
			return true
		}
	}
	return false
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validSemver(value string) bool {
	value = strings.TrimPrefix(value, "v")
	if value == "" || strings.ContainsAny(value, " /\\") {
		return false
	}
	parts := strings.Split(value, "+")
	if len(parts) > 2 || parts[0] == "" {
		return false
	}
	if len(parts) == 2 && !validSemverIdentifiers(parts[1], false) {
		return false
	}
	coreAndPrerelease := parts[0]
	core := coreAndPrerelease
	prerelease := ""
	if separator := strings.IndexByte(coreAndPrerelease, '-'); separator >= 0 {
		core = coreAndPrerelease[:separator]
		prerelease = coreAndPrerelease[separator+1:]
		if !validSemverIdentifiers(prerelease, true) {
			return false
		}
	}
	numbers := strings.Split(core, ".")
	if len(numbers) != 3 {
		return false
	}
	for _, part := range numbers {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func validSemverIdentifiers(value string, prerelease bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" || (prerelease && len(identifier) > 1 && identifier[0] == '0' && allDigits(identifier)) {
			return false
		}
		for _, character := range identifier {
			if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '-') {
				return false
			}
		}
	}
	return true
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isLowerHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}
