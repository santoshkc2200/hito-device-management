package audit_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit"
)

// TestTokenNeverAppearsInAuditPayload verifies that audit payloads sanitize
// credential tokens and sensitive fields according to the platform denylist
// before recording to the database.
func TestTokenNeverAppearsInAuditPayload(t *testing.T) {
	const plaintextToken = "HD-U-7K3M9QXA2F-4"

	rawPayload := map[string]any{
		"token":         plaintextToken,
		"password":      "secretPassword123",
		"totpCode":      "123456",
		"kioskToken":    "kiosk-secret-token",
		"description":   "Card scanned with token " + plaintextToken,
		"allowedEntity": "user-uuid-123",
		"nested": map[string]any{
			"authorization": "Bearer " + plaintextToken,
			"subToken":      plaintextToken,
			"safeField":     "all good",
		},
	}

	sanitized := audit.SanitizePayload(rawPayload)

	marshaled, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(marshaled)

	if strings.Contains(jsonStr, plaintextToken) {
		t.Fatalf("audit payload JSON contains plaintext token: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "secretPassword123") {
		t.Fatalf("audit payload JSON contains plaintext password: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "kiosk-secret-token") {
		t.Fatalf("audit payload JSON contains kiosk token: %s", jsonStr)
	}

	// Verify safe fields are preserved
	if sanitized["allowedEntity"] != "user-uuid-123" {
		t.Errorf("allowedEntity = %v, want user-uuid-123", sanitized["allowedEntity"])
	}

	nested, ok := sanitized["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested is not map[string]any: %T", sanitized["nested"])
	}
	if nested["safeField"] != "all good" {
		t.Errorf("nested.safeField = %v, want all good", nested["safeField"])
	}
	if nested["authorization"] != "[REDACTED]" {
		t.Errorf("nested.authorization = %v, want [REDACTED]", nested["authorization"])
	}
	if sanitized["token"] != "[REDACTED]" {
		t.Errorf("sanitized.token = %v, want [REDACTED]", sanitized["token"])
	}
}
