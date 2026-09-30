package checkout

import (
	"testing"
	"time"
)

func TestDisplayArgsKeepsNamesAndRFC3339TimesButNoIDs(t *testing.T) {
	borrowed := time.Date(2026, 9, 30, 5, 4, 0, 0, time.UTC)
	got := displayArgs(map[string]any{
		"deviceId":                "dev-uuid",
		"holderUserId":            "user-uuid",
		"deviceName":              "iPad 12",
		"holderName":              "Dr. Sato",
		"holderDepartment":        "",
		"pendingDeviceBorrowedAt": borrowed,
		"openLoanCount":           2,
	})
	want := map[string]string{
		"deviceName":    "iPad 12",
		"holderName":    "Dr. Sato",
		"borrowedAt":    "2026-09-30T05:04:00Z",
		"openLoanCount": "2",
	}
	if len(got) != len(want) {
		t.Fatalf("displayArgs = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("displayArgs[%q] = %q, want %q", k, got[k], v)
		}
	}
}
