package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. OWASP's current baseline recommendation for
// interactive login (as opposed to the lighter defaults meant for
// high-throughput services): 64 MiB memory, 1 pass, 4 threads.
const (
	argonMemoryKiB = 64 * 1024
	argonTime      = 1
	argonThreads   = 4
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// HashPassword returns a self-describing PHC-style string
// ($argon2id$v=19$m=...,t=...,p=...$salt$hash) so the parameters travel
// with the hash and can change later without invalidating stored values.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword checks password against a hash produced by HashPassword,
// using whatever parameters are encoded in it rather than the package's
// current defaults, and a constant-time comparison of the derived key.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, fmt.Errorf("auth: invalid password hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("auth: invalid password hash version: %w", err)
	}

	var memKiB, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memKiB, &timeCost, &threads); err != nil {
		return false, fmt.Errorf("auth: invalid password hash params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("auth: invalid password hash salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("auth: invalid password hash value: %w", err)
	}

	got := argon2.IDKey([]byte(password), salt, timeCost, memKiB, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
