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

// Template is one catalogue entry. Title and Detail are both
// text/template strings rendered against a Render call's args.
//
// Every arg reference is written defensively — `or`/`with` rather than a
// bare `{{.x}}` — because an arg comes from a best-effort lookup that is
// allowed to fail (renderMessage treats a failed lookup as "leave the arg
// unset"). A bare reference would put the literal "<no value>" on a kiosk
// screen; TestNoTemplateRendersNoValue holds the line.
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
		Title: "Device scanned", Detail: `{{or .deviceName "That device"}} — now scan your ID card`, Tone: checkoutapi.ToneInfo,
	},
	machine.MsgDevicePendingOnLoan: {
		Title: "On loan", Detail: "On loan — scan your card", Tone: checkoutapi.ToneInfo,
	},
	machine.MsgDeviceUnavailable: {
		Title: "Not available",
		Detail: `{{or .deviceName "That device"}} is {{or .deviceStatus "not available"}} and ` +
			`cannot be borrowed.`,
		Tone: checkoutapi.ToneWarning,
	},
	machine.MsgUserIdentified: {
		Title: `Hello{{with .fullName}} {{.}}{{end}}`, Detail: `You have {{or .openLoanCount 0}} item(s) out.`,
		Tone: checkoutapi.ToneInfo,
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
		Title:  "Card replaced",
		Detail: `This card was replaced{{with .revokedAt}} on {{.}}{{end}}. Please use your current card.`,
		Tone:   checkoutapi.ToneWarning,
	},
	machine.MsgBorrowed: {
		Title: "Borrowed", Detail: "{{if .dueAt}}Due {{.dueAt}}{{else}}✓ Borrowed{{end}}", Tone: checkoutapi.ToneSuccess,
	},
	machine.MsgReturned: {
		Title: "Returned", Detail: "✓ Returned. Thank you.", Tone: checkoutapi.ToneSuccess,
	},
	machine.MsgDeviceHeldByOther: {
		Title: "Held by someone else",
		Detail: `{{or .deviceName "That device"}} is with {{or .holderName "a colleague"}}` +
			`{{with .holderDepartment}} ({{.}}){{end}}{{with .borrowedAt}}, out since {{.}}{{end}}. ` +
			`Please see the equipment desk.`,
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

// compiledTemplate holds one catalogue entry's parsed title and detail.
// Both are templates: a title like "Hello {{.fullName}}" is as much a
// rendered string as the detail under it.
type compiledTemplate struct {
	title  *template.Template
	detail *template.Template
}

func mustCompile(cat map[machine.MessageKey]Template) map[machine.MessageKey]compiledTemplate {
	out := make(map[machine.MessageKey]compiledTemplate, len(cat))
	for key, tmpl := range cat {
		title, err := template.New(string(key) + ".title").Parse(tmpl.Title)
		if err != nil {
			panic(fmt.Sprintf("messages: catalogue entry %q has an unparsable title: %v", key, err))
		}
		detail, err := template.New(string(key)).Parse(tmpl.Detail)
		if err != nil {
			panic(fmt.Sprintf("messages: catalogue entry %q does not parse: %v", key, err))
		}
		out[key] = compiledTemplate{title: title, detail: detail}
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
	c := compiled[key]
	return checkoutapi.Message{
		Title:  execute(key, c.title, args),
		Detail: execute(key, c.detail, args),
		Tone:   tmpl.Tone,
	}
}

func execute(key machine.MessageKey, t *template.Template, args map[string]any) string {
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		panic(fmt.Sprintf("messages: render key %q: %v", key, err))
	}
	return buf.String()
}
