// Package config loads HDMS configuration from the environment. It fails
// fast and loudly: a missing required variable is a startup error, never a
// nil-pointer surprise three requests later.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"
)

// Config holds every environment-driven setting the API needs to start.
type Config struct {
	Env         string // "development" | "test" | "production"
	HTTPAddr    string
	DatabaseURL string
	TLSCertFile string
	TLSKeyFile  string

	TokenPepper      string
	CredentialEncKey []byte        // 32 raw bytes, AES-256-GCM key for reversible device token storage
	SessionTTL       time.Duration // idle kiosk *scan* session TTL — unrelated to admin login

	TOTPSecretEncKey []byte        // 32 raw bytes, AES-256-GCM key for admin TOTP secrets at rest
	AdminSessionTTL  time.Duration // admin login session TTL, 12h sliding renewal (docs/09)

	OTLPEndpoint string // empty disables the exporter
	LogLevel     string
}

// Load reads Config from the process environment.
func Load() (Config, error) {
	var errs []error

	cfg := Config{
		Env:          getenvDefault("HDMS_ENV", "development"),
		HTTPAddr:     getenvDefault("HDMS_HTTP_ADDR", ":8443"),
		DatabaseURL:  requireEnv("HDMS_DATABASE_URL", &errs),
		TLSCertFile:  getenvDefault("HDMS_TLS_CERT_FILE", "certs/localhost.pem"),
		TLSKeyFile:   getenvDefault("HDMS_TLS_KEY_FILE", "certs/localhost-key.pem"),
		TokenPepper:  requireEnv("HDMS_TOKEN_PEPPER", &errs),
		OTLPEndpoint: os.Getenv("HDMS_OTLP_ENDPOINT"),
		LogLevel:     getenvDefault("HDMS_LOG_LEVEL", "info"),
	}

	cfg.SessionTTL = getenvDurationDefault("HDMS_SESSION_TTL", 25*time.Second, &errs)
	cfg.CredentialEncKey = getenvBase64Key32("HDMS_CREDENTIAL_ENC_KEY", &errs)
	cfg.TOTPSecretEncKey = getenvBase64Key32("HDMS_TOTP_ENC_KEY", &errs)
	cfg.AdminSessionTTL = getenvDurationDefault("HDMS_ADMIN_SESSION_TTL", 12*time.Hour, &errs)

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func getenvDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func requireEnv(key string, errs *[]error) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		*errs = append(*errs, fmt.Errorf("missing required environment variable %s", key))
		return ""
	}
	return v
}

// getenvBase64Key32 reads a standard-base64-encoded 32-byte AES-256 key.
// Generate one with `openssl rand -base64 32`.
func getenvBase64Key32(key string, errs *[]error) []byte {
	v := requireEnv(key, errs)
	if v == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: invalid base64: %w", key, err))
		return nil
	}
	if len(raw) != 32 {
		*errs = append(*errs, fmt.Errorf("%s: must decode to 32 bytes for AES-256, got %d", key, len(raw)))
		return nil
	}
	return raw
}

func getenvDurationDefault(key string, def time.Duration, errs *[]error) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: invalid duration %q: %w", key, v, err))
		return def
	}
	return d
}
