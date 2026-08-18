// Package config loads HDMS configuration from the environment. It fails
// fast and loudly: a missing required variable is a startup error, never a
// nil-pointer surprise three requests later.
package config

import (
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

	TokenPepper string
	SessionTTL  time.Duration

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
