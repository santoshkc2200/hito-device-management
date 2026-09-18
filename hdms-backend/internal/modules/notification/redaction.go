package notification

import (
	"regexp"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

var (
	tokenPattern  = regexp.MustCompile(`HD-[UD]-[0-9A-Z]{10}-[0-9A-Z]`)
	bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[a-zA-Z0-9_\-\.]+`)
	passPattern   = regexp.MustCompile(`(?i)(password[:=]\s*)[^\s,;&]+`)
	secretPattern = regexp.MustCompile(`(?i)(secret[:=]\s*)[^\s,;&]+`)
)

// SanitizeText removes credential tokens, passwords, and secrets from free-form text
// (such as last_error strings or rendered messages).
func SanitizeText(s string) string {
	s = tokenPattern.ReplaceAllString(s, "[REDACTED]")
	s = bearerPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = passPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = secretPattern.ReplaceAllString(s, "${1}[REDACTED]")
	return s
}

// SanitizePayload strips or masks sensitive fields and credential tokens
// matching the platform sensitive-field denylist from a delivery payload.
func SanitizePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	clean := make(map[string]any, len(payload))
	for k, v := range payload {
		if httpx.IsSensitiveField(k) {
			clean[k] = "[REDACTED]"
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			clean[k] = SanitizePayload(sub)
		} else if s, ok := v.(string); ok {
			clean[k] = SanitizeText(s)
		} else {
			clean[k] = v
		}
	}
	return clean
}
