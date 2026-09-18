package messages

import (
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
)

// Phase 6.4. The exit criterion for reservations is that a walk-up
// borrower is never surprised: every refusal names the reason and the
// time. That is a property of the rendered text, so it is asserted on the
// rendered text.

func TestAReservationRefusalNamesTheReserverAndTheTime(t *testing.T) {
	msg := Render(machine.MsgDeviceReserved, map[string]any{
		"deviceName":             "Ultrasound trolley 3",
		"reservedForName":        "Dr. Karki",
		"reservationStartAtText": "14:00",
	})

	if !strings.Contains(msg.Detail, "Dr. Karki") {
		t.Errorf("Detail = %q, want it to name the reserver", msg.Detail)
	}
	if !strings.Contains(msg.Detail, "14:00") {
		t.Errorf("Detail = %q, want it to name the window start", msg.Detail)
	}
	if !strings.Contains(msg.Detail, "Ultrasound trolley 3") {
		t.Errorf("Detail = %q, want it to name the device", msg.Detail)
	}
}

// A refusal must degrade to something still useful when a lookup fails —
// never to a raw "<no value>" on a screen at the counter.
func TestAReservationRefusalSurvivesAFailedLookup(t *testing.T) {
	msg := Render(machine.MsgDeviceReserved, map[string]any{})

	if strings.Contains(msg.Detail, "<no value>") {
		t.Errorf("Detail = %q, want no raw template failure", msg.Detail)
	}
	if msg.Detail == "" || msg.Title == "" {
		t.Errorf("Title = %q, Detail = %q, want both non-empty", msg.Title, msg.Detail)
	}
}

// The collection confirmation exists so the reserver's journey feels like
// it worked. A warning tone would undo that.
func TestTheCollectionConfirmationReadsAsASuccess(t *testing.T) {
	msg := Render(machine.MsgReservationCollected, map[string]any{
		"deviceName":             "Ultrasound trolley 3",
		"reservationStartAtText": "14:00",
	})

	if msg.Tone != "success" {
		t.Errorf("Tone = %q, want success", msg.Tone)
	}
	if !strings.Contains(msg.Detail, "Ultrasound trolley 3") {
		t.Errorf("Detail = %q, want it to name the device", msg.Detail)
	}
}
