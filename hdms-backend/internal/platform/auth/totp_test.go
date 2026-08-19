package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateAndValidateTOTPRoundTrip(t *testing.T) {
	secret, url, err := GenerateTOTPSecret("admin@example.org")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if secret == "" || url == "" {
		t.Fatal("expected a non-empty secret and otpauth URL")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if !ValidateTOTPCode(secret, code) {
		t.Fatal("expected the freshly generated code to validate")
	}
}

func TestValidateTOTPRejectsWrongCode(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("admin@example.org")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if ValidateTOTPCode(secret, "000000") {
		t.Fatal("expected an arbitrary code to be rejected (astronomically unlikely to collide)")
	}
}

func TestEncryptDecryptTOTPSecretRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	const secret = "JBSWY3DPEHPK3PXP"

	enc, err := encryptSecret(secret, key)
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	got, err := decryptSecret(enc, key)
	if err != nil {
		t.Fatalf("decryptSecret: %v", err)
	}
	if got != secret {
		t.Fatalf("got %q, want %q", got, secret)
	}
}
