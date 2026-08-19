package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// encryptSecret and decryptSecret are the same AES-256-GCM scheme as
// internal/modules/credentials/internal/cryptox (nonce prepended to the
// ciphertext), duplicated here rather than imported: cryptox lives under
// credentials' internal/ package tree and is unreachable from platform code
// by Go's own visibility rules, and it is small enough that copying it is
// cheaper than inventing a shared platform/cryptox package for two
// unrelated secrets (device tokens vs. TOTP secrets) that happen to want
// the same primitive.
func encryptSecret(plaintext string, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("auth: read nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func decryptSecret(ciphertext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("auth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("auth: new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("auth: ciphertext shorter than nonce")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("auth: decrypt: %w", err)
	}
	return string(plain), nil
}
