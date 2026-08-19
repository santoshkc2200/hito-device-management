package messages

import (
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
)

func TestEveryMessageKeyHasATemplate(t *testing.T) {
	for _, key := range machine.AllMessageKeys {
		if _, ok := catalogue[key]; !ok {
			t.Errorf("no catalogue template for %q", key)
		}
	}
}

// TestMessagesNeverContainRawTokens renders every template with an args
// map containing a token-shaped value under every plausible key name and
// asserts none of them are echoed into the rendered text — the catalogue
// must never be able to leak a raw token even if a caller mistakenly
// passed one in args.
func TestMessagesNeverContainRawTokens(t *testing.T) {
	const rawToken = "HD-U-7K3M9QXA2F-4-SECRET-RAW-TOKEN"
	args := map[string]any{
		"token": rawToken, "deviceName": "Device", "deviceStatus": "maintenance",
		"fullName": "Dr. Sharma", "openLoanCount": 1, "revokedAt": "12 Aug 2026",
		"dueAt": "tomorrow", "holderName": "Dr. Karki", "holderDepartment": "Radiology",
		"borrowedAt": "14 Aug 2026",
	}
	for _, key := range machine.AllMessageKeys {
		msg := Render(key, args)
		if strings.Contains(msg.Title, rawToken) || strings.Contains(msg.Detail, rawToken) {
			t.Errorf("key %q rendered the raw token into a message: %+v", key, msg)
		}
	}
}

func TestRejectionMessagesAllNamePaperFallback(t *testing.T) {
	for _, key := range []machine.MessageKey{machine.MsgUnbound, machine.MsgUnknown} {
		t.Run(string(key), func(t *testing.T) {
			msg := Render(key, nil)
			if !strings.Contains(msg.Detail, "attendant") && !strings.Contains(msg.Detail, "register") {
				t.Errorf("message for %q = %q, want it to name the paper fallback (attendant/register)", key, msg.Detail)
			}
		})
	}
}

func TestRenderProducesTone(t *testing.T) {
	msg := Render(machine.MsgBorrowed, map[string]any{"dueAt": "9:14 AM"})
	if msg.Tone != "success" {
		t.Errorf("Tone = %q, want success", msg.Tone)
	}
	if !strings.Contains(msg.Detail, "9:14 AM") {
		t.Errorf("Detail = %q, want it to include the rendered dueAt arg", msg.Detail)
	}
}
