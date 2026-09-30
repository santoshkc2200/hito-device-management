// Package config loads HDMS configuration from the environment. It fails
// fast and loudly: a missing required variable is a startup error, never a
// nil-pointer surprise three requests later.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DevTokenPepper is the known example value from .env.example.
const DevTokenPepper = "dev-only-pepper-change-me"

// Config holds every environment-driven setting the API needs to start.
type Config struct {
	Env         string // "development" | "test" | "staging" | "production"
	HTTPAddr    string
	DatabaseURL string

	// OwnerDatabaseURL is the database owner's DSN (HDMS_OWNER_DATABASE_URL).
	// Only the worker uses it — for migrations and backups. Empty means the
	// worker uses DatabaseURL, which is right for dev and staging, where the
	// app already connects as the owner. Never given to the API in production.
	OwnerDatabaseURL string

	// AppDBPassword, when set, is applied to the hdms_app role by the worker
	// at start (HDMS_APP_DB_PASSWORD), replacing the hand-run ALTER ROLE.
	AppDBPassword string

	// MigrateOnStart lets the API skip migrations when the worker owns them
	// (HDMS_MIGRATE_ON_START, default true). Production sets it false: the
	// API connects as hdms_app, which cannot run DDL.
	MigrateOnStart bool
	TLSCertFile    string
	TLSKeyFile     string

	TokenPepper      string
	CredentialEncKey []byte        // 32 raw bytes, AES-256-GCM key for reversible device token storage
	SessionTTL       time.Duration // idle kiosk *scan* session TTL — unrelated to admin login

	TOTPSecretEncKey []byte        // 32 raw bytes, AES-256-GCM key for admin TOTP secrets at rest
	AdminSessionTTL  time.Duration // admin login session TTL, 12h sliding renewal (docs/09)

	EntraTenantID       string
	EntraClientID       string
	EntraClientSecret   string
	EntraRedirectURL    string
	EntraAllowedDomains []string
	StaffSessionTTL     time.Duration

	RateLimitEnabled   bool
	CORSAllowedOrigins []string

	// MetricsAllowCIDRs restricts /metrics to the monitoring host (5.5a).
	// HDMS_METRICS_ALLOW_CIDRS is a comma-separated list of CIDRs (plain IPs
	// accepted as /32 or /128); it defaults to localhost only. Caddy never
	// routes /metrics at all — this is the second, in-app gate for direct
	// scrapes of the API port.
	MetricsAllowCIDRs []string

	BackupDir    string // 5.4a: nightly backup target (HDMS_BACKUP_DIR, default /var/backups/hdms)
	BackupEncKey []byte // 5.4a: 32 raw bytes, AES-256-GCM key for backup encryption (HDMS_BACKUP_ENC_KEY); nil when unconfigured — the backup command fails closed, the API does not require it to boot

	// BackupAllowedRoots limits where a path destination may point
	// (HDMS_BACKUP_ALLOWED_ROOTS, colon-separated). Destination paths are
	// administrator input; an empty list rejects every path destination.
	BackupAllowedRoots []string

	// ResticBinary names the restic executable (HDMS_RESTIC_BIN, default
	// "restic"). Overridable so a pinned build can be used without PATH games.
	ResticBinary string

	// RcloneConfig optionally names the rclone configuration file (HDMS_RCLONE_CONFIG).
	// Empty uses rclone's own default location.
	RcloneConfig string

	// RetentionMode gates the 5.5e retention job: "report" (default) only
	// reports candidates and changes no domain rows; "enforce" anonymises
	// and deletes per docs/09-security-privacy-ops.md. Report-only until
	// Q7 is answered in writing — the mode is explicit configuration, not
	// a commented-out line. See jobs.ParseRetentionMode.
	RetentionMode string

	// JobMetricsDir optionally receives node_exporter textfile metrics
	// (hdms_job_last_success) from scheduled jobs (5.4b/5.5e). Empty
	// disables the textfile write — job_runs remains the source of truth.
	JobMetricsDir string

	// SMTP relay configuration (6.2c). Password comes from environment only
	// and must never be committed or logged.
	SMTPHost         string
	SMTPPort         int
	SMTPUsername     string
	SMTPPassword     string
	SMTPFromAddress  string
	SMTPReplyAddress string

	// LDAP directory configuration (6.3b).
	LDAPURL          string
	LDAPBindDN       string
	LDAPBindPassword string
	LDAPBaseDN       string
	LDAPUserFilter   string

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

	cfg.OwnerDatabaseURL = strings.TrimSpace(os.Getenv("HDMS_OWNER_DATABASE_URL"))
	cfg.AppDBPassword = os.Getenv("HDMS_APP_DB_PASSWORD")
	cfg.MigrateOnStart = true
	if raw := strings.TrimSpace(os.Getenv("HDMS_MIGRATE_ON_START")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("HDMS_MIGRATE_ON_START: %q is not true or false", raw))
		} else {
			cfg.MigrateOnStart = v
		}
	}

	cfg.SessionTTL = getenvDurationDefault("HDMS_SESSION_TTL", 25*time.Second, &errs)
	cfg.CredentialEncKey = getenvBase64Key32("HDMS_CREDENTIAL_ENC_KEY", &errs)
	cfg.TOTPSecretEncKey = getenvBase64Key32("HDMS_TOTP_ENC_KEY", &errs)
	cfg.AdminSessionTTL = getenvDurationDefault("HDMS_ADMIN_SESSION_TTL", 12*time.Hour, &errs)

	// 5.4a: backup target and encryption key. The dir always has a default;
	// the key is optional at boot (the API serves without it) and required
	// at backup time — runBackup fails closed naming HDMS_BACKUP_ENC_KEY.
	// It must live separately from the backups (password manager + root-only
	// file), never in the backup target itself.
	cfg.BackupDir = getenvDefault("HDMS_BACKUP_DIR", "/var/backups/hdms")
	cfg.BackupEncKey = getenvOptionalBase64Key32("HDMS_BACKUP_ENC_KEY", &errs)
	cfg.BackupAllowedRoots = splitAndTrim(os.Getenv("HDMS_BACKUP_ALLOWED_ROOTS"), ":")
	cfg.ResticBinary = getenvDefault("HDMS_RESTIC_BIN", "restic")
	cfg.RcloneConfig = os.Getenv("HDMS_RCLONE_CONFIG")

	// 5.5e: retention mode is explicit configuration defaulting to
	// report-only. A misspelled value fails closed at startup.
	cfg.RetentionMode = parseRetentionModeEnv(&errs)

	// 5.4b/5.5e: node_exporter textfile directory for job last-success
	// metrics. Unset by default — the textfile is a convenience, job_runs
	// is the source of truth.
	cfg.JobMetricsDir = strings.TrimSpace(os.Getenv("HDMS_JOB_METRICS_DIR"))

	cfg.EntraTenantID = os.Getenv("HDMS_ENTRA_TENANT_ID")
	cfg.EntraClientID = os.Getenv("HDMS_ENTRA_CLIENT_ID")
	cfg.EntraClientSecret = os.Getenv("HDMS_ENTRA_CLIENT_SECRET")
	cfg.EntraRedirectURL = getenvDefault("HDMS_ENTRA_REDIRECT_URL", "https://localhost:8443/v1/staff/auth/microsoft/callback")
	cfg.EntraAllowedDomains = splitAndTrim(os.Getenv("HDMS_ENTRA_ALLOWED_EMAIL_DOMAINS"), ",")
	cfg.StaffSessionTTL = getenvDurationDefault("HDMS_STAFF_SESSION_TTL", 12*time.Hour, &errs)

	// 6.2c: SMTP relay configuration for overdue reminders and digests.
	// Local development defaults to the MailHog / Mailpit catcher at localhost:1025.
	cfg.SMTPHost = getenvDefault("HDMS_SMTP_HOST", "localhost")
	cfg.SMTPPort = getenvIntDefault("HDMS_SMTP_PORT", 1025, &errs)
	cfg.SMTPUsername = os.Getenv("HDMS_SMTP_USERNAME")
	cfg.SMTPPassword = os.Getenv("HDMS_SMTP_PASSWORD")
	cfg.SMTPFromAddress = getenvDefault("HDMS_SMTP_FROM_ADDRESS", "hdms@hospital.local")
	cfg.SMTPReplyAddress = os.Getenv("HDMS_SMTP_REPLY_ADDRESS")

	// 6.3b: LDAP directory configuration.
	cfg.LDAPURL = os.Getenv("HDMS_LDAP_URL")
	cfg.LDAPBindDN = os.Getenv("HDMS_LDAP_BIND_DN")
	cfg.LDAPBindPassword = os.Getenv("HDMS_LDAP_BIND_PASSWORD")
	cfg.LDAPBaseDN = os.Getenv("HDMS_LDAP_BASE_DN")
	cfg.LDAPUserFilter = getenvDefault("HDMS_LDAP_USER_FILTER", "(objectClass=person)")

	// Rate limiting: HDMS_RATE_LIMIT=off disables, on enables.
	// Defaults to enabled in staging/production/test, and disabled in development.
	rateLimitEnv := strings.ToLower(strings.TrimSpace(os.Getenv("HDMS_RATE_LIMIT")))
	switch rateLimitEnv {
	case "off":
		cfg.RateLimitEnabled = false
	case "on":
		cfg.RateLimitEnabled = true
	default:
		cfg.RateLimitEnabled = (cfg.Env != "development")
	}

	// CORS allowed origins: HDMS_CORS_ALLOWED_ORIGINS comma-separated.
	// In development and test, defaults to the Vite dev server ports.
	// In production/staging, defaults to nil (same-origin behind reverse proxy).
	corsEnv := os.Getenv("HDMS_CORS_ALLOWED_ORIGINS")
	if corsEnv != "" {
		cfg.CORSAllowedOrigins = splitAndTrim(corsEnv, ",")
	} else if cfg.Env == "development" || cfg.Env == "test" {
		cfg.CORSAllowedOrigins = []string{
			"https://localhost:5173",
			"https://localhost:5174",
		}
	} else {
		cfg.CORSAllowedOrigins = nil
	}

	// /metrics allowlist (5.5a): localhost-only unless the operator names the
	// monitoring host's network explicitly.
	if metricsEnv := os.Getenv("HDMS_METRICS_ALLOW_CIDRS"); metricsEnv != "" {
		cfg.MetricsAllowCIDRs = splitAndTrim(metricsEnv, ",")
	} else {
		cfg.MetricsAllowCIDRs = []string{"127.0.0.1/32", "::1/128"}
	}

	validate(cfg, &errs)

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// WorkerDatabaseURL is the DSN the worker migrates and backs up with.
func (c Config) WorkerDatabaseURL() string {
	if c.OwnerDatabaseURL != "" {
		return c.OwnerDatabaseURL
	}
	return c.DatabaseURL
}

// validate enforces fail-closed production safety constraints.
func validate(cfg Config, errs *[]error) {
	if cfg.Env != "production" {
		return
	}

	// 1. Token pepper cannot use dev example value.
	if cfg.TokenPepper == DevTokenPepper {
		*errs = append(*errs, errors.New("HDMS_TOKEN_PEPPER: cannot use the example development pepper in production; generate a secure random 32-byte hex pepper with 'openssl rand -hex 32'"))
	}

	// 2. Rate limiting cannot be disabled in production.
	if strings.ToLower(strings.TrimSpace(os.Getenv("HDMS_RATE_LIMIT"))) == "off" {
		*errs = append(*errs, errors.New("HDMS_RATE_LIMIT: rate limiting cannot be set to 'off' in production; remove HDMS_RATE_LIMIT or set to 'on'"))
	}

	// 3. TLS cannot be disabled, and cert/key cannot point to dev/localhost defaults.
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("HDMS_TLS_ENABLED"))); v == "false" || v == "off" {
		*errs = append(*errs, errors.New("HDMS_TLS_ENABLED: TLS cannot be disabled in production"))
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("HDMS_TLS_DISABLED"))); v == "true" || v == "1" {
		*errs = append(*errs, errors.New("HDMS_TLS_DISABLED: TLS cannot be disabled in production"))
	}
	if cfg.TLSCertFile == "" || cfg.TLSCertFile == "certs/localhost.pem" || cfg.TLSCertFile == "/certs/localhost.pem" || strings.Contains(strings.ToLower(cfg.TLSCertFile), "localhost") {
		*errs = append(*errs, fmt.Errorf("HDMS_TLS_CERT_FILE: TLS certificate cannot use development/localhost defaults in production (got %q); provide path to valid certificate", cfg.TLSCertFile))
	}
	if cfg.TLSKeyFile == "" || cfg.TLSKeyFile == "certs/localhost-key.pem" || cfg.TLSKeyFile == "/certs/localhost-key.pem" || strings.Contains(strings.ToLower(cfg.TLSKeyFile), "localhost") {
		*errs = append(*errs, fmt.Errorf("HDMS_TLS_KEY_FILE: TLS private key cannot use development/localhost defaults in production (got %q); provide path to valid private key", cfg.TLSKeyFile))
	}

	// 4. CORS cannot allow wildcard or dev/localhost origins in production.
	for _, origin := range cfg.CORSAllowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "*" {
			*errs = append(*errs, errors.New("HDMS_CORS_ALLOWED_ORIGINS: production CORS cannot allow wildcard '*'; configure specific allowed domains or leave unset for same-origin"))
		} else if strings.Contains(strings.ToLower(trimmed), "localhost") || strings.Contains(trimmed, "127.0.0.1") || strings.HasPrefix(strings.ToLower(trimmed), "http://") {
			*errs = append(*errs, fmt.Errorf("HDMS_CORS_ALLOWED_ORIGINS: production CORS cannot allow localhost or unencrypted development origins (got %q); configure specific allowed production domains or leave unset for same-origin", trimmed))
		}
	}

	// 5. HDMS_CREDENTIAL_ENC_KEY and HDMS_TOTP_ENC_KEY must be set.
	// Note: requireEnv already catches missing variables, but validate checks explicitly in case empty.
	if len(cfg.CredentialEncKey) == 0 {
		*errs = append(*errs, errors.New("HDMS_CREDENTIAL_ENC_KEY: missing required environment variable; provide a base64-encoded 32-byte key generated with 'openssl rand -base64 32'"))
	}
	if len(cfg.TOTPSecretEncKey) == 0 {
		*errs = append(*errs, errors.New("HDMS_TOTP_ENC_KEY: missing required environment variable; provide a base64-encoded 32-byte key generated with 'openssl rand -base64 32'"))
	}

	// 6. SMTP relay configuration in production (6.2c). Local development defaults
	// (localhost:1025, hdms@hospital.local) must never reach production.
	if cfg.SMTPHost == "" || cfg.SMTPHost == "localhost" || cfg.SMTPHost == "127.0.0.1" {
		*errs = append(*errs, fmt.Errorf("HDMS_SMTP_HOST: production cannot use development/localhost default (got %q); configure hospital SMTP relay host", cfg.SMTPHost))
	}
	if cfg.SMTPPort <= 0 || cfg.SMTPPort == 1025 {
		*errs = append(*errs, fmt.Errorf("HDMS_SMTP_PORT: production cannot use development port 1025 or invalid port (got %d); configure hospital SMTP relay port (typically 25, 465, or 587)", cfg.SMTPPort))
	}
	if cfg.SMTPFromAddress == "" || strings.Contains(cfg.SMTPFromAddress, "localhost") || strings.HasSuffix(cfg.SMTPFromAddress, ".local") {
		*errs = append(*errs, fmt.Errorf("HDMS_SMTP_FROM_ADDRESS: production cannot use development default (got %q); configure valid hospital sender address", cfg.SMTPFromAddress))
	}
}

// LogEffective logs the active configuration to the provided logger with all
// secrets redacted (token pepper, AES encryption keys, Entra client secret,
// and any password in the database URL).
func (c Config) LogEffective(logger *slog.Logger) {
	logger.Info("effective configuration",
		slog.String("env", c.Env),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("database_url", redactURL(c.DatabaseURL)),
		slog.String("tls_cert_file", c.TLSCertFile),
		slog.String("tls_key_file", c.TLSKeyFile),
		slog.String("token_pepper", "[REDACTED]"),
		slog.String("credential_enc_key", "[REDACTED]"),
		slog.Duration("session_ttl", c.SessionTTL),
		slog.String("totp_secret_enc_key", "[REDACTED]"),
		slog.Duration("admin_session_ttl", c.AdminSessionTTL),
		slog.String("entra_tenant_id", c.EntraTenantID),
		slog.String("entra_client_id", c.EntraClientID),
		slog.String("entra_client_secret", redactSecret(c.EntraClientSecret)),
		slog.String("entra_redirect_url", c.EntraRedirectURL),
		slog.Any("entra_allowed_domains", c.EntraAllowedDomains),
		slog.Duration("staff_session_ttl", c.StaffSessionTTL),
		slog.Bool("rate_limit_enabled", c.RateLimitEnabled),
		slog.Any("cors_allowed_origins", c.CORSAllowedOrigins),
		slog.Any("metrics_allow_cidrs", c.MetricsAllowCIDRs),
		slog.String("backup_dir", c.BackupDir),
		slog.String("backup_enc_key", "[REDACTED]"),
		slog.String("backup_allowed_roots", strings.Join(c.BackupAllowedRoots, ":")),
		slog.String("restic_binary", c.ResticBinary),
		slog.String("rclone_config", c.RcloneConfig),
		slog.String("retention_mode", c.RetentionMode),
		slog.String("job_metrics_dir", c.JobMetricsDir),
		slog.String("smtp_host", c.SMTPHost),
		slog.Int("smtp_port", c.SMTPPort),
		slog.String("smtp_username", c.SMTPUsername),
		slog.String("smtp_password", redactSecret(c.SMTPPassword)),
		slog.String("smtp_from_address", c.SMTPFromAddress),
		slog.String("smtp_reply_address", c.SMTPReplyAddress),
		slog.String("otlp_endpoint", c.OTLPEndpoint),
		slog.String("log_level", c.LogLevel),
	)
}

func redactSecret(v string) string {
	if v == "" {
		return ""
	}
	return "[REDACTED]"
}

func redactURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[malformed database url]"
	}
	return u.Redacted()
}

// parseRetentionModeEnv reads HDMS_RETENTION_MODE (5.5e). Unset or blank
// means "report" — the job ships report-only until Q7 is answered in
// writing. Any other value besides "report" or "enforce" is a startup
// error naming the variable, so a typo can never silently enable deletion.
func parseRetentionModeEnv(errs *[]error) string {
	v, ok := os.LookupEnv("HDMS_RETENTION_MODE")
	if !ok || strings.TrimSpace(v) == "" {
		return "report"
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "report", "enforce":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		*errs = append(*errs, fmt.Errorf("HDMS_RETENTION_MODE: must be \"report\" or \"enforce\", got %q", strings.TrimSpace(v)))
		return "report"
	}
}

// splitAndTrim splits s on sep and drops empty entries, so a trailing or
// doubled separator in an env var is not read as an empty path.
func splitAndTrim(s, sep string) []string {
	var out []string
	for _, part := range strings.Split(s, sep) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
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
		*errs = append(*errs, fmt.Errorf("%s: missing required environment variable", key))
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

// getenvOptionalBase64Key32 reads HDMS_BACKUP_ENC_KEY when set, returning nil
// when unset so the API can boot without backup configured. A present but
// malformed value is still a startup error — silently running backups under
// a bad key would be worse than refusing to start.
func getenvOptionalBase64Key32(key string, errs *[]error) []byte {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
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

func getenvIntDefault(key string, def int, errs *[]error) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: invalid integer %q: %w", key, v, err))
		return def
	}
	return n
}
