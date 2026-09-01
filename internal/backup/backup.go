package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	Format                   = "xkeen-control-backup"
	FormatVersion            = 1
	EncryptedFormat          = "xkeen-control-backup-encrypted"
	EnvelopeVersion          = 1
	KDFName                  = "Argon2id"
	Argon2Version            = 19
	Argon2MemoryKiB          = 32768
	Argon2Iterations         = 2
	Argon2Parallelism        = 1
	Argon2KeyBytes           = 32
	Argon2SaltBytes          = 16
	XChaCha20NonceBytes      = 24
	MaxSecretPlaintext       = 6 << 20
	MaxEncryptedEnvelope     = 9 << 20
	MinPassphraseBytes       = 12
	MaxPassphraseBytes       = 256
	MaxSecretRequestBody     = 16 << 10
	SafeFilename             = "xkeen-control-backup.json"
	SecretFilename           = "xkeen-control-backup-encrypted.json"
	BackupMediaType          = "application/vnd.xkeen-control.backup+json"
	EncryptedBackupMediaType = "application/vnd.xkeen-control.backup-encrypted+json"
)

var (
	ErrUnavailable       = errors.New("backup is unavailable")
	ErrBusy              = errors.New("backup encryption is busy")
	ErrInvalidPassphrase = errors.New("passphrase is outside the allowed bounds")
	ErrInvalidBundle     = errors.New("backup bundle is invalid")
	ErrInvalidEnvelope   = errors.New("encrypted backup envelope is invalid")
	ErrDecryptionFailed  = errors.New("encrypted backup could not be opened")
	ErrRandomUnavailable = errors.New("backup randomness is unavailable")
	ErrEncryptionFailed  = errors.New("backup encryption is unavailable")
	secretOperationGate  = make(chan struct{}, 1)
)

// ApplianceSource is the narrow typed read boundary used by backup export.
// It must not fall back to repository policy or runtime files.
type ApplianceSource interface {
	Snapshot() (appliance.Appliance, error)
}

// RegistrySource is the narrow coherent node read boundary used by secret
// export. The implementation serializes the snapshot with node Apply.
type RegistrySource interface {
	Snapshot(context.Context) (nodes.Registry, error)
}

// KeyDeriver is injectable for bounded tests. Production always uses the
// fixed Argon2id tuple passed to the function.
type KeyDeriver func(password, salt []byte, memoryKiB, iterations uint32, parallelism uint8, keyBytes uint32) []byte

type Config struct {
	Appliance ApplianceSource
	Nodes     RegistrySource
	Build     buildinfo.Info
	Now       func() time.Time
	Random    io.Reader
	DeriveKey KeyDeriver
	GOOS      string
	GOARCH    string
}

type Service struct {
	appliance applianceSource
	nodes     RegistrySource
	build     buildinfo.Info
	now       func() time.Time
	random    io.Reader
	deriveKey KeyDeriver
	goos      string
	goarch    string
}

// applianceSource is kept separate from the exported interface so a nil
// source can be handled as a typed service-unavailable result.
type applianceSource interface {
	Snapshot() (appliance.Appliance, error)
}

func NewService(config Config) *Service {
	if config.Build.Product == "" {
		config.Build = buildinfo.Current()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.DeriveKey == nil {
		config.DeriveKey = argon2.IDKey
	}
	if config.GOOS == "" {
		config.GOOS = runtime.GOOS
	}
	if config.GOARCH == "" {
		config.GOARCH = runtime.GOARCH
	}
	return &Service{
		appliance: config.Appliance,
		nodes:     config.Nodes,
		build:     config.Build,
		now:       config.Now,
		random:    config.Random,
		deriveKey: config.DeriveKey,
		goos:      config.GOOS,
		goarch:    config.GOARCH,
	}
}

// New is a short compatibility constructor for package-local callers.
func New(config Config) *Service { return NewService(config) }

// Bundle is the typed plaintext backup package. Nodes is nil for the safe
// export and present for the encrypted secret export only.
type Bundle struct {
	Format        string              `json:"format"`
	FormatVersion int                 `json:"formatVersion"`
	Manifest      Manifest            `json:"manifest"`
	Appliance     appliance.Appliance `json:"appliance"`
	Nodes         *nodes.Registry     `json:"nodes,omitempty"`
}

type Manifest struct {
	ApplianceSchemaVersion int            `json:"applianceSchemaVersion"`
	Build                  buildinfo.Info `json:"build"`
	ExportedAt             string         `json:"exportedAt"`
	GOOS                   string         `json:"goos"`
	GOARCH                 string         `json:"goarch"`
	Sections               []Section      `json:"sections"`
	ContainsSecrets        bool           `json:"containsSecrets"`
}

type Section struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type encryptedEnvelope struct {
	Format          string           `json:"format"`
	EnvelopeVersion int              `json:"envelopeVersion"`
	KDF             kdfParameters    `json:"kdf"`
	Cipher          cipherParameters `json:"cipher"`
	Ciphertext      string           `json:"ciphertext"`
}

type kdfParameters struct {
	Name        string `json:"name"`
	Version     int    `json:"version"`
	MemoryKiB   uint32 `json:"memoryKiB"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	KeyBytes    uint32 `json:"keyBytes"`
	Salt        string `json:"salt"`
}

type cipherParameters struct {
	Name  string `json:"name"`
	Nonce string `json:"nonce"`
}

type aadHeader struct {
	Format          string           `json:"format"`
	EnvelopeVersion int              `json:"envelopeVersion"`
	KDF             kdfParameters    `json:"kdf"`
	Cipher          cipherParameters `json:"cipher"`
}

// Export returns the structurally secretless appliance bundle.
func (s *Service) Export(context.Context) ([]byte, error) {
	if s == nil || s.appliance == nil {
		return nil, ErrUnavailable
	}
	value, err := s.appliance.Snapshot()
	if err != nil {
		return nil, ErrUnavailable
	}
	return s.encodeBundle(value, nil, false)
}

// ExportSecret returns a one-request re-authenticated encrypted backup. The
// caller performs session, CSRF and current-password checks before invoking
// this method; this service never receives or persists the current password.
func (s *Service) ExportSecret(ctx context.Context, passphrase string) ([]byte, error) {
	if s == nil || s.appliance == nil || s.nodes == nil {
		return nil, ErrUnavailable
	}
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	release, ok := trySecretOperation()
	if !ok {
		return nil, ErrBusy
	}
	defer release()

	applianceValue, err := s.appliance.Snapshot()
	if err != nil {
		return nil, ErrUnavailable
	}
	registry, err := s.nodes.Snapshot(ctx)
	if err != nil {
		return nil, ErrUnavailable
	}
	plaintext, err := s.encodeBundle(applianceValue, &registry, true)
	if err != nil {
		return nil, err
	}
	defer clearBytes(plaintext)

	salt, err := s.randomBytes(Argon2SaltBytes)
	if err != nil {
		return nil, ErrRandomUnavailable
	}
	nonce, err := s.randomBytes(XChaCha20NonceBytes)
	if err != nil {
		clearBytes(salt)
		return nil, ErrRandomUnavailable
	}
	defer clearBytes(salt)
	defer clearBytes(nonce)

	password := []byte(passphrase)
	key := s.deriveKey(password, salt, Argon2MemoryKiB, Argon2Iterations, Argon2Parallelism, Argon2KeyBytes)
	clearBytes(password)
	if len(key) != Argon2KeyBytes {
		return nil, ErrEncryptionFailed
	}
	defer clearBytes(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrRandomUnavailable
	}
	kdf := kdfParameters{
		Name: KDFName, Version: Argon2Version, MemoryKiB: Argon2MemoryKiB,
		Iterations: Argon2Iterations, Parallelism: Argon2Parallelism,
		KeyBytes: Argon2KeyBytes, Salt: base64.RawURLEncoding.EncodeToString(salt),
	}
	cipher := cipherParameters{Name: "XChaCha20-Poly1305", Nonce: base64.RawURLEncoding.EncodeToString(nonce)}
	aad, err := marshalAAD(aadHeader{Format: EncryptedFormat, EnvelopeVersion: EnvelopeVersion, KDF: kdf, Cipher: cipher})
	if err != nil {
		return nil, ErrEncryptionFailed
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	envelope := encryptedEnvelope{
		Format: EncryptedFormat, EnvelopeVersion: EnvelopeVersion,
		KDF: kdf, Cipher: cipher,
		Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
	}
	return marshalEnvelope(envelope)
}

// OpenEncrypted is an internal strict decrypt/open path for Phase B tests and
// later bounded import. It never returns plaintext when envelope validation or
// AEAD authentication fails.
func OpenEncrypted(contents []byte, passphrase string) (Bundle, error) {
	return openEncrypted(contents, passphrase, argon2.IDKey)
}

func openEncrypted(contents []byte, passphrase string, deriveKey KeyDeriver) (Bundle, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return Bundle{}, err
	}
	salt, nonce, ciphertext, aad, err := parseEnvelope(contents)
	if err != nil {
		return Bundle{}, err
	}
	release, ok := trySecretOperation()
	if !ok {
		clearBytes(salt)
		clearBytes(nonce)
		clearBytes(ciphertext)
		clearBytes(aad)
		return Bundle{}, ErrBusy
	}
	defer release()
	defer clearBytes(ciphertext)
	defer clearBytes(aad)
	password := []byte(passphrase)
	if deriveKey == nil {
		clearBytes(password)
		return Bundle{}, ErrDecryptionFailed
	}
	key := deriveKey(password, salt, Argon2MemoryKiB, Argon2Iterations, Argon2Parallelism, Argon2KeyBytes)
	clearBytes(password)
	defer clearBytes(key)
	defer clearBytes(salt)
	defer clearBytes(nonce)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return Bundle{}, ErrDecryptionFailed
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil || len(plaintext) > MaxSecretPlaintext {
		clearBytes(plaintext)
		return Bundle{}, ErrDecryptionFailed
	}
	bundle, err := ParseBundle(plaintext)
	clearBytes(plaintext)
	if err != nil {
		return Bundle{}, ErrDecryptionFailed
	}
	if !bundle.Manifest.ContainsSecrets || bundle.Nodes == nil {
		return Bundle{}, ErrDecryptionFailed
	}
	return bundle, nil
}

// ParseBundle strictly opens already-decoded typed bundle bytes. It is kept
// separate from HTTP and is intentionally not an import capability.
func ParseBundle(contents []byte) (Bundle, error) {
	if len(contents) == 0 || len(contents) > MaxSecretPlaintext {
		return Bundle{}, ErrInvalidBundle
	}
	var bundle Bundle
	if err := decodeStrict(contents, &bundle); err != nil {
		return Bundle{}, ErrInvalidBundle
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		return Bundle{}, ErrInvalidBundle
	}
	_, hasNodes := fields["nodes"]
	if !hasNodes && bundle.Manifest.ContainsSecrets {
		return Bundle{}, ErrInvalidBundle
	}
	if hasNodes && bundle.Nodes == nil {
		return Bundle{}, ErrInvalidBundle
	}
	if !bundle.Manifest.ContainsSecrets && hasNodes {
		return Bundle{}, ErrInvalidBundle
	}
	if err := bundle.validate(); err != nil {
		return Bundle{}, ErrInvalidBundle
	}
	return bundle, nil
}

func (s *Service) encodeBundle(value appliance.Appliance, registry *nodes.Registry, containsSecrets bool) ([]byte, error) {
	applianceBytes, err := appliance.MarshalCanonical(value)
	if err != nil {
		return nil, ErrUnavailable
	}
	sections := []Section{sectionFor("appliance", applianceBytes)}
	if containsSecrets {
		if registry == nil {
			return nil, ErrInvalidBundle
		}
		nodeBytes, err := nodes.MarshalCanonical(*registry)
		if err != nil {
			return nil, ErrUnavailable
		}
		sections = append(sections, sectionFor("nodes", nodeBytes))
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	exportedAt := now.UTC().Format(time.RFC3339Nano)
	bundle := Bundle{
		Format: Format, FormatVersion: FormatVersion,
		Manifest: Manifest{
			ApplianceSchemaVersion: value.SchemaVersion,
			Build:                  s.build, ExportedAt: exportedAt, GOOS: s.goos, GOARCH: s.goarch,
			Sections: sections, ContainsSecrets: containsSecrets,
		},
		Appliance: value, Nodes: registry,
	}
	return encodeBundle(bundle)
}

func encodeBundle(bundle Bundle) ([]byte, error) {
	if err := bundle.validate(); err != nil {
		return nil, err
	}
	contents, err := json.Marshal(bundle)
	if err != nil || len(contents)+1 > MaxSecretPlaintext {
		return nil, ErrInvalidBundle
	}
	return append(contents, '\n'), nil
}

func (b Bundle) validate() error {
	if b.Format != Format || b.FormatVersion != FormatVersion || b.Manifest.ContainsSecrets != (b.Nodes != nil) {
		return ErrInvalidBundle
	}
	if err := b.Appliance.Validate(); err != nil || b.Manifest.ApplianceSchemaVersion != b.Appliance.SchemaVersion {
		return ErrInvalidBundle
	}
	if !validExportedAt(b.Manifest.ExportedAt) || !validHeaderToken(b.Manifest.GOOS) || !validHeaderToken(b.Manifest.GOARCH) {
		return ErrInvalidBundle
	}
	if !validBuild(b.Manifest.Build) {
		return ErrInvalidBundle
	}
	expectedSections := 1
	if b.Nodes != nil {
		expectedSections = 2
	}
	if len(b.Manifest.Sections) != expectedSections {
		return ErrInvalidBundle
	}
	applianceBytes, err := appliance.MarshalCanonical(b.Appliance)
	if err != nil || !matchesSection(b.Manifest.Sections[0], "appliance", applianceBytes) {
		return ErrInvalidBundle
	}
	if b.Nodes != nil {
		nodeBytes, err := nodes.MarshalCanonical(*b.Nodes)
		if err != nil || !matchesSection(b.Manifest.Sections[1], "nodes", nodeBytes) {
			return ErrInvalidBundle
		}
	}
	return nil
}

func sectionFor(name string, contents []byte) Section {
	digest := sha256.Sum256(contents)
	return Section{Name: name, Size: int64(len(contents)), SHA256: hex.EncodeToString(digest[:])}
}

func matchesSection(section Section, name string, contents []byte) bool {
	expected := sectionFor(name, contents)
	return section == expected
}

func validBuild(value buildinfo.Info) bool {
	return value.Product == "xkeen-control" && validHeaderToken(value.Product) && validHeaderToken(value.Version) && validHeaderToken(value.SourceCommit) && validHeaderToken(value.Channel)
}

func validHeaderToken(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r > 0x7e {
			return false
		}
	}
	return true
}

func validExportedAt(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC
}

func validatePassphrase(value string) error {
	length := len([]byte(value))
	if !utf8.ValidString(value) || length < MinPassphraseBytes || length > MaxPassphraseBytes {
		return ErrInvalidPassphrase
	}
	return nil
}

// ValidatePassphrase applies the v1 byte bounds without trimming or
// normalizing the caller's passphrase.
func ValidatePassphrase(value string) error { return validatePassphrase(value) }

func (s *Service) randomBytes(size int) ([]byte, error) {
	if s.random == nil {
		return nil, ErrRandomUnavailable
	}
	value := make([]byte, size)
	if _, err := io.ReadFull(s.random, value); err != nil {
		clearBytes(value)
		return nil, err
	}
	return value, nil
}

func marshalAAD(value aadHeader) ([]byte, error) {
	contents, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return contents, nil
}

func marshalEnvelope(value encryptedEnvelope) ([]byte, error) {
	contents, err := json.Marshal(value)
	if err != nil || len(contents)+1 > MaxEncryptedEnvelope {
		return nil, ErrInvalidEnvelope
	}
	return append(contents, '\n'), nil
}

func parseEnvelope(contents []byte) ([]byte, []byte, []byte, []byte, error) {
	if len(contents) == 0 || len(contents) > MaxEncryptedEnvelope {
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	var envelope encryptedEnvelope
	if err := decodeStrict(contents, &envelope); err != nil {
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	if envelope.Format != EncryptedFormat || envelope.EnvelopeVersion != EnvelopeVersion ||
		envelope.KDF.Name != KDFName || envelope.KDF.Version != Argon2Version ||
		envelope.KDF.MemoryKiB != Argon2MemoryKiB || envelope.KDF.Iterations != Argon2Iterations ||
		envelope.KDF.Parallelism != Argon2Parallelism || envelope.KDF.KeyBytes != Argon2KeyBytes ||
		envelope.Cipher.Name != "XChaCha20-Poly1305" {
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	salt, ok := decodeRawURL(envelope.KDF.Salt, Argon2SaltBytes)
	if !ok {
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	nonce, ok := decodeRawURL(envelope.Cipher.Nonce, XChaCha20NonceBytes)
	if !ok {
		clearBytes(salt)
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	ciphertext, ok := decodeRawURL(envelope.Ciphertext, 0)
	if !ok || len(ciphertext) < chacha20poly1305.Overhead || len(ciphertext) > MaxSecretPlaintext+chacha20poly1305.Overhead {
		clearBytes(salt)
		clearBytes(nonce)
		clearBytes(ciphertext)
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	aad, err := marshalAAD(aadHeader{Format: envelope.Format, EnvelopeVersion: envelope.EnvelopeVersion, KDF: envelope.KDF, Cipher: envelope.Cipher})
	if err != nil {
		clearBytes(salt)
		clearBytes(nonce)
		clearBytes(ciphertext)
		return nil, nil, nil, nil, ErrInvalidEnvelope
	}
	return salt, nonce, ciphertext, aad, nil
}

func decodeRawURL(value string, expectedLength int) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || (expectedLength > 0 && len(decoded) != expectedLength) || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, false
	}
	return decoded, true
}

func decodeStrict(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func trySecretOperation() (func(), bool) {
	select {
	case secretOperationGate <- struct{}{}:
		return func() { <-secretOperationGate }, true
	default:
		return nil, false
	}
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
