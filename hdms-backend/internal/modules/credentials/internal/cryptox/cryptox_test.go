package cryptox_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/credentials/internal/cryptox"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestHashTokenDeterministicAndDistinguishing(t *testing.T) {
	pepper := []byte("dev-only-pepper")
	h1 := cryptox.HashToken("HD-U-7K3M9QXA2F-4", pepper)
	h2 := cryptox.HashToken("HD-U-7K3M9QXA2F-4", pepper)
	if !bytes.Equal(h1, h2) {
		t.Fatal("HashToken is not deterministic")
	}

	h3 := cryptox.HashToken("HD-D-7K3M9QXA2F-4", pepper)
	if bytes.Equal(h1, h3) {
		t.Fatal("different tokens hashed identically")
	}

	h4 := cryptox.HashToken("HD-U-7K3M9QXA2F-4", []byte("different-pepper"))
	if bytes.Equal(h1, h4) {
		t.Fatal("same token under different peppers hashed identically")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	const token = "HD-D-7K3M9QXA2F-4"

	ciphertext, err := cryptox.Encrypt(token, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, []byte(token)) {
		t.Fatal("ciphertext contains the plaintext token")
	}

	got, err := cryptox.Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != token {
		t.Fatalf("got %q, want %q", got, token)
	}
}

func TestEncryptIsNotDeterministic(t *testing.T) {
	// Each Encrypt call must use a fresh random nonce.
	key := testKey(t)
	c1, err := cryptox.Encrypt("HD-D-7K3M9QXA2F-4", key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	c2, err := cryptox.Encrypt("HD-D-7K3M9QXA2F-4", key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(c1, c2) {
		t.Fatal("two Encrypt calls of the same token produced identical ciphertext")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	key := testKey(t)
	wrongKey := testKey(t)

	ciphertext, err := cryptox.Encrypt("HD-D-7K3M9QXA2F-4", key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := cryptox.Decrypt(ciphertext, wrongKey); err == nil {
		t.Fatal("expected Decrypt with the wrong key to fail")
	}
}
