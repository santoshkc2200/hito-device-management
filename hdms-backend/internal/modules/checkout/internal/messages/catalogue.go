// Package messages renders machine.MessageKey values into the
// server-authored, display-ready text the kiosk shows
// (docs/phases/phase-2/2.3b-transition-table.md). Wording lives here, not
// in table.go or a handler, so it can be corrected — or translated —
// without shipping a new kiosk build.
package messages

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
)

// Template is one catalogue entry. Detail is a text/template string
// rendered against a Render call's args.
type Template struct {
	Title  string
	Detail string
	Tone   checkoutapi.Tone
}

// catalogue has one entry per machine.MessageKey the table can produce.
// TestEveryMessageKeyHasATemplate asserts that is exactly true — an
// unrendered key must be impossible, because the kiosk has no fallback
// wording.
//
// Every one of the three refusal templates (unbound, unknown, revoked)
// names the paper fallback (docs/04): a kiosk that only says "no" sends
// someone away holding a device they need.
var catalogue = map[machine.MessageKey]Template{
	machine.MsgDevicePendingAvailable: {
		Title: "Device scanned", Detail: "{{.deviceName}} — now scan your ID card", Tone: checkoutapi.ToneInfo,
	},
	machine.MsgDevicePendingOnLoan: {
		Title: "On loan", Detail: "On loan — scan your card", Tone: checkoutapi.ToneInfo,
	},
	machine.MsgDeviceUnavailable: {
		Title: "Not available", Detail: "{{.deviceName}} is {{.deviceStatus}} and cannot be borrowed.", Tone: checkoutapi.ToneWarning,
	},
	machine.MsgUserIdentified: {
		Title: "Hello {{.fullName}}", Detail: "You have {{.openLoanCount}} item(s) out.", Tone: checkoutapi.ToneInfo,
	},
	machine.MsgUserSuspended: {
		Title: "Borrowing suspended", Detail: "Borrowing suspended — see the equipment desk.", Tone: checkoutapi.ToneWarning,
	},
	machine.MsgUserArchived: {
		Title: "Account archived", Detail: "This account is archived and cannot borrow. Please see the equipment desk.", Tone: checkoutapi.ToneWarning,
	},
	machine.MsgUnbound: {
		Title: "Card not registered",
		Detail: "This card has not been registered yet. The attendant can write your item in the " +
			"register — please see them to get your card.",
		Tone: checkoutapi.ToneWarning,
	},
	machine.MsgUnknown: {
		Title: "Card not recognised",
		Detail: "Card not recognised. You can still take the item — the attendant will record it. " +
			"Please see them to register.",
		Tone: checkoutapi.ToneWarning,
	},
	machine.MsgRevoked: {
		Title: "Card replaced", Detail: "This card was replaced on {{.revokedAt}}. Please use your current card.", Tone: checkoutapi.ToneWarning,
	},
	machine.MsgBorrowed: {
		Title: "Borrowed", Detail: "{{if .dueAt}}Due {{.dueAt}}{{else}}✓ Borrowed{{end}}", Tone: checkoutapi.ToneSuccess,
	},
	machine.MsgReturned: {
		Title: "Returned", Detail: "✓ Returned. Thank you.", Tone: checkoutapi.ToneSuccess,
	},
	machine.MsgDeviceHeldByOther: {
		Title: "Held by someone else",
		Detail: "{{.deviceName}} is with {{.holderName}} ({{.holderDepartment}}), out since {{.borrowedAt}}. " +
			"Please see the equipment desk.",
		Tone: checkoutapi.ToneWarning,
	},
	machine.MsgDuplicate: {
		Title: "Already scanned", Detail: "Already scanned — no change.", Tone: checkoutapi.ToneInfo,
	},
	machine.MsgExpired: {
		Title: "Session timed out", Detail: "Session timed out.", Tone: checkoutapi.ToneInfo,
	},
}

var compiled = mustCompile(catalogue)

func mustCompile(cat map[machine.MessageKey]Template) map[machine.MessageKey]*template.Template {
	out := make(map[machine.MessageKey]*template.Template, len(cat))
	for key, tmpl := range cat {
		t, err := template.New(string(key)).Parse(tmpl.Detail)
		if err != nil {
			panic(fmt.Sprintf("messages: catalogue entry %q does not parse: %v", key, err))
		}
		out[key] = t
	}
	return out
}

// Render turns a MessageKey and its args into the display-ready message a
// scan response carries. It panics on an unknown key — every key the
// table can produce has a template (TestEveryMessageKeyHasATemplate
// guards this at build time), so reaching this branch means the catalogue
// itself is incomplete, not a bad runtime input.
func Render(key machine.MessageKey, args map[string]any) checkoutapi.Message {
	tmpl, ok := catalogue[key]
	if !ok {
		panic(fmt.Sprintf("messages: no template registered for key %q", key))
	}
	var buf bytes.Buffer
	if err := compiled[key].Execute(&buf, args); err != nil {
		panic(fmt.Sprintf("messages: render key %q: %v", key, err))
	}
	return checkoutapi.Message{Title: tmpl.Title, Detail: buf.String(), Tone: tmpl.Tone}
}
