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

// TestNoTemplateRendersNoValue is the guard behind the catalogue's
// defensive `or`/`with` style: every arg comes from a lookup 2.3c is
// allowed to fail, so rendering with *no* args at all must still produce
// text a person can read. A bare {{.x}} would put the literal
// "<no value>" on a kiosk screen instead.
func TestNoTemplateRendersNoValue(t *testing.T) {
	for _, key := range machine.AllMessageKeys {
		msg := Render(key, nil)
		if strings.Contains(msg.Title, "<no value>") || strings.Contains(msg.Detail, "<no value>") {
			t.Errorf("key %q renders a gap with no args: title=%q detail=%q", key, msg.Title, msg.Detail)
		}
		if msg.Title == "" || msg.Detail == "" {
			t.Errorf("key %q rendered an empty title or detail with no args: %+v", key, msg)
		}
	}
}

// TestTitlesAreRenderedNotEchoed pins the other half: a title is a
// template like any detail, so an unsubstituted "{{" reaching the kiosk
// means Render stopped rendering one of them.
func TestTitlesAreRenderedNotEchoed(t *testing.T) {
	for _, key := range machine.AllMessageKeys {
		msg := Render(key, map[string]any{"fullName": "Dr. Sharma"})
		if strings.Contains(msg.Title, "{{") || strings.Contains(msg.Detail, "{{") {
			t.Errorf("key %q left template syntax in the message: %+v", key, msg)
		}
	}
	if got := Render(machine.MsgUserIdentified, map[string]any{"fullName": "Dr. Sharma"}).Title; got != "Hello Dr. Sharma" {
		t.Errorf("MsgUserIdentified title = %q, want %q", got, "Hello Dr. Sharma")
	}
	if got := Render(machine.MsgUserIdentified, nil).Title; got != "Hello" {
		t.Errorf("MsgUserIdentified title with no name = %q, want %q", got, "Hello")
	}
}

// TestDeviceHeldByOtherNamesHolderAndTime covers scenario 5 of docs/04 as
// the kiosk actually sees it — the holder's name, their department by
// name, and when they took it — rather than only asserting the detail is
// non-empty.
func TestDeviceHeldByOtherNamesHolderAndTime(t *testing.T) {
	msg := Render(machine.MsgDeviceHeldByOther, map[string]any{
		"deviceName": "LAPTOP-07", "holderName": "Dr. Karki",
		"holderDepartment": "Radiology", "borrowedAt": "2026-08-14 09:20 NPT",
	})
	for _, want := range []string{"LAPTOP-07", "Dr. Karki", "(Radiology)", "out since 2026-08-14 09:20 NPT"} {
		if !strings.Contains(msg.Detail, want) {
			t.Errorf("Detail = %q, want it to contain %q", msg.Detail, want)
		}
	}

	// Degraded: nothing resolved but the device name. The sentence still
	// has to stand on its own, with no dangling "with  ()," fragments.
	degraded := Render(machine.MsgDeviceHeldByOther, map[string]any{"deviceName": "LAPTOP-07"})
	if want := "LAPTOP-07 is with a colleague. Please see the equipment desk."; degraded.Detail != want {
		t.Errorf("degraded Detail = %q, want %q", degraded.Detail, want)
	}
}

// TestRevokedNamesTheDateOnlyWhenItHasOne mirrors the same rule for the
// revoked-card message, whose date arrives from the credential.
func TestRevokedNamesTheDateOnlyWhenItHasOne(t *testing.T) {
	with := Render(machine.MsgRevoked, map[string]any{"revokedAt": "2026-08-12 10:00 NPT"})
	if !strings.Contains(with.Detail, "replaced on 2026-08-12 10:00 NPT") {
		t.Errorf("Detail = %q, want it to name the revocation date", with.Detail)
	}
	if got, want := Render(machine.MsgRevoked, nil).Detail, "This card was replaced. Please use your current card."; got != want {
		t.Errorf("Detail with no date = %q, want %q", got, want)
	}
}
