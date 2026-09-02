package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestBundleAndEncryptedOpenRejectUnknownNestedSubscriptionField(t *testing.T) {
	service := testService(t, &incrementingReader{}, strictNodesTestDerive)
	registry := testRegistry(t)
	plaintext, err := service.encodeBundle(testAppliance(t), &registry, true)
	if err != nil {
		t.Fatal(err)
	}
	mutated := addUnknownSubscriptionField(t, plaintext)
	if _, err := ParseBundle(mutated); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("strict bundle parser accepted unknown nested subscription field: %v", err)
	}

	const passphrase = "correct synthetic passphrase"
	envelope := encryptStrictNodesFixture(t, mutated, passphrase)
	if opened, err := openEncrypted(envelope, passphrase, strictNodesTestDerive); !errors.Is(err, ErrDecryptionFailed) || opened.Nodes != nil {
		t.Fatalf("encrypted strict opener accepted unknown nested subscription field: %+v, %v", opened, err)
	}
}

func addUnknownSubscriptionField(t *testing.T, contents []byte) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(contents, &top); err != nil {
		t.Fatal(err)
	}
	var registry map[string]json.RawMessage
	if err := json.Unmarshal(top["nodes"], &registry); err != nil {
		t.Fatal(err)
	}
	var subscriptions []map[string]json.RawMessage
	if err := json.Unmarshal(registry["subscriptions"], &subscriptions); err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) == 0 {
		t.Fatal("fixture has no subscription")
	}
	subscriptions[0]["futureField"] = json.RawMessage(`true`)
	encodedSubscriptions, err := json.Marshal(subscriptions)
	if err != nil {
		t.Fatal(err)
	}
	registry["subscriptions"] = encodedSubscriptions
	encodedRegistry, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	top["nodes"] = encodedRegistry
	mutated, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return append(mutated, '\n')
}

func strictNodesTestDerive(password, salt []byte, _, _ uint32, _ uint8, keyBytes uint32) []byte {
	input := append(append([]byte(nil), password...), salt...)
	digest := sha256.Sum256(input)
	return append([]byte(nil), digest[:keyBytes]...)
}

func encryptStrictNodesFixture(t *testing.T, plaintext []byte, passphrase string) []byte {
	t.Helper()
	salt := bytes.Repeat([]byte{0x11}, Argon2SaltBytes)
	nonce := bytes.Repeat([]byte{0x22}, XChaCha20NonceBytes)
	key := strictNodesTestDerive([]byte(passphrase), salt, Argon2MemoryKiB, Argon2Iterations, Argon2Parallelism, Argon2KeyBytes)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		t.Fatal(err)
	}
	kdf := kdfParameters{
		Name: KDFName, Version: Argon2Version, MemoryKiB: Argon2MemoryKiB,
		Iterations: Argon2Iterations, Parallelism: Argon2Parallelism,
		KeyBytes: Argon2KeyBytes, Salt: base64.RawURLEncoding.EncodeToString(salt),
	}
	cipher := cipherParameters{Name: "XChaCha20-Poly1305", Nonce: base64.RawURLEncoding.EncodeToString(nonce)}
	aad, err := marshalAAD(aadHeader{Format: EncryptedFormat, EnvelopeVersion: EnvelopeVersion, KDF: kdf, Cipher: cipher})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	envelope, err := marshalEnvelope(encryptedEnvelope{
		Format: EncryptedFormat, EnvelopeVersion: EnvelopeVersion,
		KDF: kdf, Cipher: cipher, Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}
