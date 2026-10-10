// Package backup owns the fixed portable encryption envelope (Argon2id +
// XChaCha20-Poly1305) and its single-flight secret-operation budget. Callers own
// their payload format.
package backup

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
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
	SecretFilename           = "xkeen-control-backup-encrypted.json"
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

// KeyDeriver is injectable for bounded tests. Production always uses the
// fixed Argon2id tuple passed to the function.
type KeyDeriver func(password, salt []byte, memoryKiB, iterations uint32, parallelism uint8, keyBytes uint32) []byte

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

// OpenProduced holds the same private-memory/KDF budget through authenticated
// decryption and typed decoding. Returned application data belongs to the caller;
// temporary plaintext is cleared after the callback.
func OpenProduced(contents []byte, passphrase string, consume func([]byte) error) error {
	return openAndUse(contents, passphrase, argon2.IDKey, consume)
}

func openAndUse(contents []byte, passphrase string, deriveKey KeyDeriver, consume func([]byte) error) error {
	if err := validatePassphrase(passphrase); err != nil {
		return err
	}
	if consume == nil {
		return ErrUnavailable
	}
	release, ok := trySecretOperation()
	if !ok {
		return ErrBusy
	}
	defer release()
	plaintext, err := openPayloadReserved(contents, passphrase, deriveKey)
	defer clearBytes(plaintext)
	if err != nil {
		return err
	}
	return consume(plaintext)
}

// SealPayload encrypts a bounded typed payload with the fixed portable envelope.
// Callers must validate their own payload format; this does not authorize restore.
func SealPayload(plaintext []byte, passphrase string) ([]byte, error) {
	if len(plaintext) == 0 || len(plaintext) > MaxSecretPlaintext {
		return nil, ErrInvalidBundle
	}
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	release, ok := trySecretOperation()
	if !ok {
		return nil, ErrBusy
	}
	defer release()
	return sealPayload(plaintext, passphrase, rand.Reader, argon2.IDKey)
}

// SealProduced reserves the shared secret-operation budget before collecting
// and encoding a private snapshot, not only before the expensive KDF. The
// producer runs once and returned plaintext is cleared on every exit.
func SealProduced(passphrase string, produce func() ([]byte, error)) ([]byte, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	if produce == nil {
		return nil, ErrUnavailable
	}
	release, ok := trySecretOperation()
	if !ok {
		return nil, ErrBusy
	}
	defer release()
	plaintext, err := produce()
	defer clearBytes(plaintext)
	if err != nil {
		return nil, err
	}
	if len(plaintext) == 0 || len(plaintext) > MaxSecretPlaintext {
		return nil, ErrInvalidBundle
	}
	return sealPayload(plaintext, passphrase, rand.Reader, argon2.IDKey)
}

func sealPayload(plaintext []byte, passphrase string, random io.Reader, deriveKey KeyDeriver) ([]byte, error) {
	salt, err := randomBytes(random, Argon2SaltBytes)
	if err != nil {
		return nil, ErrRandomUnavailable
	}
	nonce, err := randomBytes(random, XChaCha20NonceBytes)
	if err != nil {
		clearBytes(salt)
		return nil, ErrRandomUnavailable
	}
	defer clearBytes(salt)
	defer clearBytes(nonce)

	password := []byte(passphrase)
	key := deriveKey(password, salt, Argon2MemoryKiB, Argon2Iterations, Argon2Parallelism, Argon2KeyBytes)
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

func openPayloadReserved(contents []byte, passphrase string, deriveKey KeyDeriver) ([]byte, error) {
	salt, nonce, ciphertext, aad, err := parseEnvelope(contents)
	if err != nil {
		return nil, err
	}
	defer clearBytes(salt)
	defer clearBytes(nonce)
	defer clearBytes(ciphertext)
	defer clearBytes(aad)
	password := []byte(passphrase)
	if deriveKey == nil {
		clearBytes(password)
		return nil, ErrDecryptionFailed
	}
	key := deriveKey(password, salt, Argon2MemoryKiB, Argon2Iterations, Argon2Parallelism, Argon2KeyBytes)
	clearBytes(password)
	defer clearBytes(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil || len(plaintext) > MaxSecretPlaintext {
		clearBytes(plaintext)
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
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

func randomBytes(random io.Reader, size int) ([]byte, error) {
	if random == nil {
		return nil, ErrRandomUnavailable
	}
	value := make([]byte, size)
	if _, err := io.ReadFull(random, value); err != nil {
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
