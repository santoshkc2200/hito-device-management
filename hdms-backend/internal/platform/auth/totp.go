package auth

import (
	"fmt"

	"github.com/pquerna/otp/totp"
)

// GenerateTOTPSecret mints a new TOTP secret for accountEmail and returns
// both the raw base32 secret and the otpauth:// enrollment URL. Used only
// by `hdms-cli admin bootstrap` — there is no self-service enrollment
// endpoint (registration/account-creation is administrator-only throughout
// this system, docs/09-security-privacy-ops.md).
func GenerateTOTPSecret(accountEmail string) (secret, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "HDMS",
		AccountName: accountEmail,
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: generate totp secret: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTPCode reports whether code is a valid current TOTP code for
// secret, allowing the standard +/-1 step skew.
func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}
