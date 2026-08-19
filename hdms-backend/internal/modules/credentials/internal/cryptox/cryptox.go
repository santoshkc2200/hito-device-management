// Package cryptox implements the two credential-storage primitives from
// docs/05-credentials-and-labeling.md: an irreversible HMAC for the
// indexed lookup every scan performs, and reversible AES-GCM for device
// tokens only, where a genuine reprint (not a reissue) is a real
// requirement because the sticker is not a secret.
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

// HashToken computes HMAC-SHA256(token, pepper). The pepper is a
// deployment secret held outside the database (config.TokenPepper), so a
// leaked database backup alone does not yield working cards.
func HashToken(token string, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(token))
	return mac.Sum(nil)
}

// Encrypt seals token with AES-256-GCM under key, prepending a random
// nonce to the ciphertext so Decrypt is self-contained.
func Encrypt(token string, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cryptox: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cryptox: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("cryptox: read nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(token), nil), nil
}

// Decrypt reverses Encrypt.
func Decrypt(ciphertext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cryptox: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cryptox: new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("cryptox: ciphertext shorter than nonce")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("cryptox: decrypt: %w", err)
	}
	return string(plain), nil
}
