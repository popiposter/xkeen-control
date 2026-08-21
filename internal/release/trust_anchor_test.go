package release

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func TestStablePublicKeyHexIsCanonicalEd25519Key(t *testing.T) {
	if len(StablePublicKeyHex) != ed25519.PublicKeySize*2 {
		t.Fatalf("StablePublicKeyHex length = %d, want %d", len(StablePublicKeyHex), ed25519.PublicKeySize*2)
	}
	if StablePublicKeyHex != strings.ToLower(StablePublicKeyHex) {
		t.Fatal("StablePublicKeyHex must use canonical lowercase hex")
	}
	key, err := DecodePublicKey([]byte(StablePublicKeyHex))
	if err != nil {
		t.Fatalf("DecodePublicKey(StablePublicKeyHex): %v", err)
	}
	if len(key) != ed25519.PublicKeySize {
		t.Fatalf("decoded public key length = %d, want %d", len(key), ed25519.PublicKeySize)
	}
}
