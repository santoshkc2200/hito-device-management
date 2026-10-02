package config_test

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/config"
)

// validProductionEnv returns a map of environment variables that satisfy all
// production safety gates.
func validProductionEnv() map[string]string {
	key32 := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	return map[string]string{
		"HDMS_ENV":                  "production",
		"HDMS_HTTP_ADDR":            ":8443",
		"HDMS_DATABASE_URL":         "postgres://hdms_user:prod_db_secret@db.prod.hospital:5432/hdms_prod?sslmode=verify-full",
		"HDMS_TLS_CERT_FILE":        "/etc/ssl/certs/hdms.hospital.crt",
		"HDMS_TLS_KEY_FILE":         "/etc/ssl/private/hdms.hospital.key",
		"HDMS_TOKEN_PEPPER":         "f4a3c2b1e0d9c8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3",
		"HDMS_CREDENTIAL_ENC_KEY":   key32,
		"HDMS_TOTP_ENC_KEY":         key32,
		"HDMS_RATE_LIMIT":           "on",
		"HDMS_CORS_ALLOWED_ORIGINS": "https://hdms.hospital.local",
		"HDMS_SMTP_HOST":            "smtp.prod.hospital",
		"HDMS_SMTP_PORT":            "587",
		"HDMS_SMTP_FROM_ADDRESS":    "hdms@hospital.org",
	}
}

// TestConfigLoad_ProductionRefusals tests the fail-closed gates in config.Load.
// In production, the process must refuse to start and clearly name the offending
// variable in operator language.
func TestConfigLoad_ProductionRefusals(t *testing.T) {
	tests := []struct {
		name      string
		modifyEnv func(env map[string]string)
		wantVar   string
	}{
		{
			name: "refuses dev token pepper from .env.example",
			modifyEnv: func(env map[string]string) {
				env["HDMS_TOKEN_PEPPER"] = config.DevTokenPepper
			},
			wantVar: "HDMS_TOKEN_PEPPER",
		},
		{
			name: "refuses rate limit off",
			modifyEnv: func(env map[string]string) {
				env["HDMS_RATE_LIMIT"] = "off"
			},
			wantVar: "HDMS_RATE_LIMIT",
		},
		{
			name: "refuses TLS disabled explicitly",
			modifyEnv: func(env map[string]string) {
				env["HDMS_TLS_ENABLED"] = "false"
			},
			wantVar: "HDMS_TLS_ENABLED",
		},
		{
			name: "refuses default localhost cert",
			modifyEnv: func(env map[string]string) {
				env["HDMS_TLS_CERT_FILE"] = "certs/localhost.pem"
			},
			wantVar: "HDMS_TLS_CERT_FILE",
		},
		{
			name: "refuses default localhost key",
			modifyEnv: func(env map[string]string) {
				env["HDMS_TLS_KEY_FILE"] = "certs/localhost-key.pem"
			},
			wantVar: "HDMS_TLS_KEY_FILE",
		},
		{
			name: "refuses CORS wildcard",
			modifyEnv: func(env map[string]string) {
				env["HDMS_CORS_ALLOWED_ORIGINS"] = "*"
			},
			wantVar: "HDMS_CORS_ALLOWED_ORIGINS",
		},
		{
			name: "refuses CORS localhost dev origin",
			modifyEnv: func(env map[string]string) {
				env["HDMS_CORS_ALLOWED_ORIGINS"] = "https://localhost:5173"
			},
			wantVar: "HDMS_CORS_ALLOWED_ORIGINS",
		},
		{
			name: "refuses unset HDMS_CREDENTIAL_ENC_KEY",
			modifyEnv: func(env map[string]string) {
				delete(env, "HDMS_CREDENTIAL_ENC_KEY")
			},
			wantVar: "HDMS_CREDENTIAL_ENC_KEY",
		},
		{
			name: "refuses unset HDMS_TOTP_ENC_KEY",
			modifyEnv: func(env map[string]string) {
				delete(env, "HDMS_TOTP_ENC_KEY")
			},
			wantVar: "HDMS_TOTP_ENC_KEY",
		},
		{
			name: "refuses default localhost smtp host in production",
			modifyEnv: func(env map[string]string) {
				env["HDMS_SMTP_HOST"] = "localhost"
			},
			wantVar: "HDMS_SMTP_HOST",
		},
		{
			name: "refuses dev catcher port 1025 in production",
			modifyEnv: func(env map[string]string) {
				env["HDMS_SMTP_PORT"] = "1025"
			},
			wantVar: "HDMS_SMTP_PORT",
		},
		{
			name: "refuses default localhost from address in production",
			modifyEnv: func(env map[string]string) {
				env["HDMS_SMTP_FROM_ADDRESS"] = "hdms@hospital.local"
			},
			wantVar: "HDMS_SMTP_FROM_ADDRESS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validProductionEnv()
			tt.modifyEnv(env)

			// Apply test environment
			for k, v := range env {
				t.Setenv(k, v)
			}
			// Explicitly unset keys deleted from test env
			for _, k := range []string{"HDMS_CREDENTIAL_ENC_KEY", "HDMS_TOTP_ENC_KEY", "HDMS_TLS_ENABLED"} {
				if _, ok := env[k]; !ok {
					t.Setenv(k, "")
				}
			}

			_, err := config.Load()
			if err == nil {
				t.Fatalf("expected config.Load to fail for %s, but succeeded", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Fatalf("expected error message to name %q, got: %v", tt.wantVar, err)
			}
		})
	}
}

// TestConfigLoad_ProductionValid asserts that when all production requirements
// are met, config.Load succeeds without errors.
func TestConfigLoad_ProductionValid(t *testing.T) {
	env := validProductionEnv()
	for k, v := range env {
		t.Setenv(k, v)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid production config to load successfully, got: %v", err)
	}
	if !cfg.RateLimitEnabled {
		t.Errorf("expected RateLimitEnabled to be true in production")
	}
	if cfg.Env != "production" {
		t.Errorf("expected Env to be 'production', got %q", cfg.Env)
	}
}

// TestMetricsAllowCIDRsDefaultsToLocalhost pins the 5.5a default: /metrics
// is scrape-only from localhost unless the operator names the monitoring
// host's network explicitly.
func TestMetricsAllowCIDRsDefaultsToLocalhost(t *testing.T) {
	env := validProductionEnv()
	for k, v := range env {
		t.Setenv(k, v)
	}
	t.Setenv("HDMS_METRICS_ALLOW_CIDRS", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	joined := strings.Join(cfg.MetricsAllowCIDRs, ",")
	if !strings.Contains(joined, "127.0.0.1/32") || !strings.Contains(joined, "::1/128") {
		t.Fatalf("MetricsAllowCIDRs default = %q, want localhost-only", cfg.MetricsAllowCIDRs)
	}

	t.Setenv("HDMS_METRICS_ALLOW_CIDRS", "10.0.0.0/24, 192.168.1.10")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load with custom allowlist: %v", err)
	}
	if len(cfg.MetricsAllowCIDRs) != 2 {
		t.Fatalf("MetricsAllowCIDRs = %q, want 2 entries", cfg.MetricsAllowCIDRs)
	}
}

// TestProductionConfigLogsNoSecretValues satisfies the 5.0b spec requirement
// named productionConfigLogsNoSecretValues asserting that no secret value
// appears in the startup log output.
func TestProductionConfigLogsNoSecretValues(t *testing.T) {
	productionConfigLogsNoSecretValues(t)
}

func productionConfigLogsNoSecretValues(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	const (
		secretDBPass     = "super_secret_db_password_xyz987"
		secretPepper     = "super_secret_hmac_pepper_value_123"
		secretCredKeyRaw = "super_secret_credential_enc_key"
		secretTotpKeyRaw = "super_secret_totp_secret_enc_key"
		secretEntraKey   = "super_secret_entra_client_secret_456"
		secretSMTPPass   = "super_secret_smtp_password_999"
	)

	cfg := config.Config{
		Env:                 "production",
		HTTPAddr:            ":8443",
		DatabaseURL:         "postgres://hdms_user:" + secretDBPass + "@db.hospital.local:5432/hdms_prod?sslmode=verify-full",
		TLSCertFile:         "/etc/ssl/certs/hdms.crt",
		TLSKeyFile:          "/etc/ssl/private/hdms.key",
		TokenPepper:         secretPepper,
		CredentialEncKey:    []byte(secretCredKeyRaw),
		SessionTTL:          25 * time.Second,
		TOTPSecretEncKey:    []byte(secretTotpKeyRaw),
		AdminSessionTTL:     12 * time.Hour,
		EntraTenantID:       "tenant-12345",
		EntraClientID:       "client-67890",
		EntraClientSecret:   secretEntraKey,
		EntraRedirectURL:    "https://hdms.hospital.local/v1/staff/auth/microsoft/callback",
		EntraAllowedDomains: []string{"hospital.org"},
		StaffSessionTTL:     12 * time.Hour,
		RateLimitEnabled:    true,
		CORSAllowedOrigins:  []string{"https://hdms.hospital.local"},
		SMTPHost:            "smtp.prod.hospital",
		SMTPPort:            587,
		SMTPPassword:        secretSMTPPass,
		SMTPFromAddress:     "hdms@hospital.org",
		OTLPEndpoint:        "http://collector:4318",
		LogLevel:            "info",
	}

	cfg.LogEffective(logger)
	logOutput := buf.String()

	secrets := []struct {
		name  string
		value string
	}{
		{"database password", secretDBPass},
		{"token pepper", secretPepper},
		{"credential encryption key", secretCredKeyRaw},
		{"TOTP encryption key", secretTotpKeyRaw},
		{"Entra client secret", secretEntraKey},
		{"SMTP password", secretSMTPPass},
	}

	for _, s := range secrets {
		if strings.Contains(logOutput, s.value) {
			t.Errorf("leak detected: startup log contains %s %q:\n%s", s.name, s.value, logOutput)
		}
	}

	if !strings.Contains(logOutput, "[REDACTED]") {
		t.Errorf("expected log output to contain '[REDACTED]', got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "xxxxx") {
		t.Errorf("expected database URL in log output to contain redacted password 'xxxxx', got:\n%s", logOutput)
	}
}

func TestWorkerDatabaseURLFallsBackToAppURL(t *testing.T) {
	for k, v := range validProductionEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("HDMS_OWNER_DATABASE_URL", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.OwnerDatabaseURL != "" {
		t.Fatalf("OwnerDatabaseURL = %q, want empty when unset", cfg.OwnerDatabaseURL)
	}
	if got := cfg.WorkerDatabaseURL(); got != cfg.DatabaseURL {
		t.Fatalf("WorkerDatabaseURL = %q, want the app DSN", got)
	}

	owner := "postgres://hdms_prod:owner@db:5432/hdms_prod?sslmode=disable"
	t.Setenv("HDMS_OWNER_DATABASE_URL", owner)
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := cfg.WorkerDatabaseURL(); got != owner {
		t.Fatalf("WorkerDatabaseURL = %q, want the owner DSN", got)
	}
}

func TestMigrateOnStart(t *testing.T) {
	for k, v := range validProductionEnv() {
		t.Setenv(k, v)
	}
	cases := []struct {
		raw     string
		want    bool
		wantErr bool
	}{
		{"", true, false},
		{"true", true, false},
		{"false", false, false},
		{"FALSE", false, false},
		{"sometimes", false, true},
	}
	for _, c := range cases {
		t.Setenv("HDMS_MIGRATE_ON_START", c.raw)
		cfg, err := config.Load()
		if c.wantErr {
			if err == nil || !strings.Contains(err.Error(), "HDMS_MIGRATE_ON_START") {
				t.Fatalf("raw %q: err = %v, want one naming HDMS_MIGRATE_ON_START", c.raw, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("raw %q: %v", c.raw, err)
		}
		if cfg.MigrateOnStart != c.want {
			t.Fatalf("raw %q: MigrateOnStart = %v, want %v", c.raw, cfg.MigrateOnStart, c.want)
		}
	}
}

func TestBackupDrivesConfig(t *testing.T) {
	for k, v := range validProductionEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("HDMS_BACKUP_ALLOWED_ROOTS", "/etc") // removed variable: must be ignored
	t.Setenv("HDMS_BACKUP_DRIVES_DIR", "/drives")
	t.Setenv("HDMS_BACKUP_DRIVES_HOST_PATH", "/mnt")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.BackupDrivesDir != "/drives" || cfg.BackupDrivesHostPath != "/mnt" {
		t.Fatalf("drives = %q on host %q", cfg.BackupDrivesDir, cfg.BackupDrivesHostPath)
	}
	if len(cfg.BackupAllowedRoots) != 1 || cfg.BackupAllowedRoots[0] != "/drives" {
		t.Fatalf("BackupAllowedRoots = %v, want [/drives] only", cfg.BackupAllowedRoots)
	}

	t.Setenv("HDMS_BACKUP_DRIVES_DIR", "")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.BackupAllowedRoots) != 0 {
		t.Fatalf("BackupAllowedRoots = %v, want empty: no drives folder refuses every path destination", cfg.BackupAllowedRoots)
	}
}
