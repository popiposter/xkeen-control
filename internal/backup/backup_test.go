package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type incrementingReader struct{ next byte }

func (r *incrementingReader) Read(value []byte) (int, error) {
	for index := range value {
		value[index] = r.next
		r.next++
	}
	return len(value), nil
}

func fastDeriver(password, salt []byte, _, _ uint32, _ uint8, keyBytes uint32) []byte {
	digest := sha256.Sum256(append(append([]byte(nil), password...), salt...))
	return append([]byte(nil), digest[:keyBytes]...)
}

const syntheticPassphrase = "correct synthetic passphrase"

func openWith(t *testing.T, contents []byte, passphrase string) ([]byte, error) {
	t.Helper()
	var opened []byte
	err := openAndUse(contents, passphrase, fastDeriver, func(plaintext []byte) error {
		opened = append([]byte(nil), plaintext...)
		return nil
	})
	return opened, err
}

func TestEnvelopeRoundTripUsesFixedParametersAndFreshRandomness(t *testing.T) {
	random := &incrementingReader{}
	plaintext := []byte(`{"synthetic":"private-marker-0123456789abcdef"}`)
	first, err := sealPayload(plaintext, syntheticPassphrase, random, fastDeriver)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sealPayload(plaintext, syntheticPassphrase, random, fastDeriver)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) || bytes.Contains(first, []byte("private-marker")) || len(first) > MaxEncryptedEnvelope {
		t.Fatalf("envelope reused randomness, leaked plaintext or exceeded bound (%d bytes)", len(first))
	}
	var envelope encryptedEnvelope
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Format != EncryptedFormat || envelope.EnvelopeVersion != EnvelopeVersion || envelope.KDF.Name != KDFName || envelope.KDF.Version != Argon2Version || envelope.KDF.MemoryKiB != Argon2MemoryKiB || envelope.KDF.Iterations != Argon2Iterations || envelope.KDF.Parallelism != Argon2Parallelism || envelope.KDF.KeyBytes != Argon2KeyBytes || envelope.Cipher.Name != "XChaCha20-Poly1305" {
		t.Fatalf("envelope parameters = %+v", envelope)
	}
	if salt, ok := decodeRawURL(envelope.KDF.Salt, Argon2SaltBytes); !ok {
		t.Fatal("salt is not the fixed length")
	} else {
		clearBytes(salt)
	}
	if nonce, ok := decodeRawURL(envelope.Cipher.Nonce, XChaCha20NonceBytes); !ok {
		t.Fatal("nonce is not the fixed length")
	} else {
		clearBytes(nonce)
	}
	opened, err := openWith(t, first, syntheticPassphrase)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("roundtrip = %q, %v", opened, err)
	}
}

func TestEnvelopeRejectsWrongPassphraseAndTamperingBeforePlaintext(t *testing.T) {
	original, err := sealPayload([]byte(`{"synthetic":true}`), syntheticPassphrase, &incrementingReader{}, fastDeriver)
	if err != nil {
		t.Fatal(err)
	}
	var envelope encryptedEnvelope
	if err := json.Unmarshal(original, &envelope); err != nil {
		t.Fatal(err)
	}
	tamper := func(mutate func(*encryptedEnvelope)) []byte {
		mutated := envelope
		mutate(&mutated)
		contents, err := json.Marshal(mutated)
		if err != nil {
			t.Fatal(err)
		}
		return contents
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(original, &fields); err != nil {
		t.Fatal(err)
	}
	fields["unexpected"] = json.RawMessage(`true`)
	unknownField, _ := json.Marshal(fields)
	cases := map[string]struct {
		data []byte
		pass string
	}{
		"wrong passphrase": {original, "wrong synthetic passphrase"},
		"ciphertext tamper": {tamper(func(value *encryptedEnvelope) {
			ciphertext, _ := decodeRawURL(value.Ciphertext, 0)
			ciphertext[0] ^= 1
			value.Ciphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
		}), syntheticPassphrase},
		"AAD nonce tamper": {tamper(func(value *encryptedEnvelope) {
			value.Cipher.Nonce = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x7f}, XChaCha20NonceBytes))
		}), syntheticPassphrase},
		"unknown outer field":     {unknownField, syntheticPassphrase},
		"malformed base64":        {tamper(func(value *encryptedEnvelope) { value.KDF.Salt = "%%%" }), syntheticPassphrase},
		"alternate KDF parameter": {tamper(func(value *encryptedEnvelope) { value.KDF.MemoryKiB++ }), syntheticPassphrase},
		"alternate cipher":        {tamper(func(value *encryptedEnvelope) { value.Cipher.Name = "AES-GCM" }), syntheticPassphrase},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if opened, err := openWith(t, test.data, test.pass); err == nil || opened != nil {
				t.Fatalf("tampered open = %q, %v", opened, err)
			}
		})
	}
}

func TestSecretOperationIsSingleFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	derive := func(password, salt []byte, memoryKiB, iterations uint32, parallelism uint8, keyBytes uint32) []byte {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return fastDeriver(password, salt, memoryKiB, iterations, parallelism, keyBytes)
	}
	archive, err := sealPayload([]byte(`{}`), syntheticPassphrase, &incrementingReader{}, fastDeriver)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() {
		first <- openAndUse(archive, syntheticPassphrase, derive, func([]byte) error { return nil })
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first KDF did not start")
	}
	if _, err := SealPayload([]byte(`{}`), "second synthetic passphrase"); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent seal = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("KDF calls = %d", calls.Load())
	}
	close(release)
	select {
	case err := <-first:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first operation did not finish")
	}
}

func TestPassphraseBoundsAreBytesWithoutNormalization(t *testing.T) {
	for _, value := range []string{"short", string(bytes.Repeat([]byte("a"), MaxPassphraseBytes+1)), "invalid \xff utf8 value"} {
		if !errors.Is(ValidatePassphrase(value), ErrInvalidPassphrase) {
			t.Fatalf("passphrase %q accepted", value)
		}
	}
	if err := ValidatePassphrase(" " + syntheticPassphrase + " "); err != nil {
		t.Fatal(err)
	}
}
