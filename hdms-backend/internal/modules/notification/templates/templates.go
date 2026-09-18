package templates

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	"strings"
	texttmpl "text/template"
)

// Data models for rendering each notification template.
// These structures ensure no credential tokens or extraneous PII are exposed,
// and that human contact details are always present (Phase 6.2c).

type OverdueReminderData struct {
	BorrowerName   string
	DeviceName     string
	AssetTag       string
	BorrowedAt     string
	DueAt          string
	OverdueDays    int
	ReturnLocation string
	HumanContact   string
}

type OverdueDigestItem struct {
	AssetTag     string
	DeviceName   string
	BorrowerName string
	DueAt        string
	OverdueDays  int
}

type WeeklyDigestData struct {
	GeneratedAt  string
	TotalOverdue int
	Items        []OverdueDigestItem
	HumanContact string
}

type ReturnConfirmationData struct {
	BorrowerName string
	DeviceName   string
	AssetTag     string
	ReturnedAt   string
	HumanContact string
}

const overdueReminderSubject = "[HITO HOSPITAL] Overdue Equipment Reminder: {{.DeviceName}} ({{.AssetTag}})"

const overdueReminderText = `Hello {{.BorrowerName}},

This is a reminder from Hito Hospital Equipment Management that the following device borrowed under your account is overdue:

  Device:    {{.DeviceName}}
  Asset Tag: {{.AssetTag}}
  Borrowed:  {{.BorrowedAt}}
  Due Date:  {{.DueAt}}
  Overdue:   {{.OverdueDays}} day(s)

Please return this device as soon as possible to:
  {{.ReturnLocation}}

If you have already returned this device, or if this record appears in error, please contact:
  {{.HumanContact}}

Thank you for helping us keep equipment available for clinical care.
`

const overdueReminderHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.5; color: #333; }
.card { border: 1px solid #e2e8f0; border-radius: 6px; padding: 16px; background-color: #f8fafc; margin: 16px 0; }
.label { font-weight: 600; color: #475569; }
.footer { margin-top: 24px; font-size: 0.875rem; color: #64748b; }
</style>
</head>
<body>
<p>Hello {{.BorrowerName}},</p>
<p>This is a reminder from Hito Hospital Equipment Management that the following device borrowed under your account is overdue:</p>
<div class="card">
  <p><span class="label">Device:</span> {{.DeviceName}}</p>
  <p><span class="label">Asset Tag:</span> {{.AssetTag}}</p>
  <p><span class="label">Borrowed:</span> {{.BorrowedAt}}</p>
  <p><span class="label">Due Date:</span> {{.DueAt}}</p>
  <p><span class="label">Overdue:</span> {{.OverdueDays}} day(s)</p>
</div>
<p>Please return this device as soon as possible to: <strong>{{.ReturnLocation}}</strong></p>
<p>If you have already returned this device, or if this record appears in error, please contact: <strong>{{.HumanContact}}</strong></p>
<div class="footer">
  <p>Thank you for helping us keep equipment available for clinical care.</p>
</div>
</body>
</html>
`

const weeklyDigestSubject = "[HITO HOSPITAL] Weekly Overdue Equipment Digest ({{.TotalOverdue}} item(s))"

const weeklyDigestText = `Equipment Administration Weekly Digest
Generated: {{.GeneratedAt}}

Total Overdue Items: {{.TotalOverdue}}

{{range .Items}}- [{{.AssetTag}}] {{.DeviceName}} - Borrower: {{.BorrowerName}} - Due: {{.DueAt}} ({{.OverdueDays}} day(s) overdue)
{{end}}
If any of these records need reconciliation or status adjustments, please visit the admin console or contact:
  {{.HumanContact}}
`

const weeklyDigestHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.5; color: #333; }
table { width: 100%; border-collapse: collapse; margin: 16px 0; }
th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #e2e8f0; }
th { background-color: #f1f5f9; color: #475569; }
.footer { margin-top: 24px; font-size: 0.875rem; color: #64748b; }
</style>
</head>
<body>
<h2>Equipment Administration Weekly Digest</h2>
<p>Generated: {{.GeneratedAt}}</p>
<p><strong>Total Overdue Items:</strong> {{.TotalOverdue}}</p>
<table>
  <thead>
    <tr>
      <th>Asset Tag</th>
      <th>Device</th>
      <th>Borrower</th>
      <th>Due Date</th>
      <th>Overdue</th>
    </tr>
  </thead>
  <tbody>
{{range .Items}}    <tr>
      <td>{{.AssetTag}}</td>
      <td>{{.DeviceName}}</td>
      <td>{{.BorrowerName}}</td>
      <td>{{.DueAt}}</td>
      <td>{{.OverdueDays}} day(s)</td>
    </tr>
{{end}}  </tbody>
</table>
<div class="footer">
  <p>If any of these records need reconciliation or status adjustments, please contact {{.HumanContact}}.</p>
</div>
</body>
</html>
`

const returnConfirmationSubject = "[HITO HOSPITAL] Equipment Returned: {{.DeviceName}} ({{.AssetTag}})"

const returnConfirmationText = `Hello {{.BorrowerName}},

Thank you for returning the following device to Hito Hospital Equipment Management:

  Device:      {{.DeviceName}}
  Asset Tag:   {{.AssetTag}}
  Returned At: {{.ReturnedAt}}

The custody record has been closed successfully.

If you have any questions, please contact:
  {{.HumanContact}}
`

const returnConfirmationHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.5; color: #333; }
.card { border: 1px solid #e2e8f0; border-radius: 6px; padding: 16px; background-color: #f8fafc; margin: 16px 0; }
.label { font-weight: 600; color: #475569; }
.footer { margin-top: 24px; font-size: 0.875rem; color: #64748b; }
</style>
</head>
<body>
<p>Hello {{.BorrowerName}},</p>
<p>Thank you for returning the following device to Hito Hospital Equipment Management:</p>
<div class="card">
  <p><span class="label">Device:</span> {{.DeviceName}}</p>
  <p><span class="label">Asset Tag:</span> {{.AssetTag}}</p>
  <p><span class="label">Returned At:</span> {{.ReturnedAt}}</p>
</div>
<p>The custody record has been closed successfully.</p>
<div class="footer">
  <p>If you have any questions, please contact: <strong>{{.HumanContact}}</strong></p>
</div>
</body>
</html>
`

// Parsed templates
var (
	tOverdueSub  = texttmpl.Must(texttmpl.New("overdue_sub").Parse(overdueReminderSubject))
	tOverdueText = texttmpl.Must(texttmpl.New("overdue_text").Parse(overdueReminderText))
	hOverdueHTML = htmltmpl.Must(htmltmpl.New("overdue_html").Parse(overdueReminderHTML))

	tDigestSub  = texttmpl.Must(texttmpl.New("digest_sub").Parse(weeklyDigestSubject))
	tDigestText = texttmpl.Must(texttmpl.New("digest_text").Parse(weeklyDigestText))
	hDigestHTML = htmltmpl.Must(htmltmpl.New("digest_html").Parse(weeklyDigestHTML))

	tReturnSub  = texttmpl.Must(texttmpl.New("return_sub").Parse(returnConfirmationSubject))
	tReturnText = texttmpl.Must(texttmpl.New("return_text").Parse(returnConfirmationText))
	hReturnHTML = htmltmpl.Must(htmltmpl.New("return_html").Parse(returnConfirmationHTML))
)

func renderText(t *texttmpl.Template, data any) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("templates: render text: %w", err)
	}
	return buf.String(), nil
}

func renderHTML(t *htmltmpl.Template, data any) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("templates: render html: %w", err)
	}
	return buf.String(), nil
}

// RenderOverdueReminder renders the overdue reminder email (plain text and minimal HTML).
func RenderOverdueReminder(data OverdueReminderData) (subject, textBody, htmlBody string, err error) {
	sub, err := renderText(tOverdueSub, data)
	if err != nil {
		return "", "", "", err
	}
	txt, err := renderText(tOverdueText, data)
	if err != nil {
		return "", "", "", err
	}
	ht, err := renderHTML(hOverdueHTML, data)
	if err != nil {
		return "", "", "", err
	}
	return strings.TrimSpace(sub), txt, ht, nil
}

// RenderWeeklyDigest renders the weekly digest for equipment administrators.
func RenderWeeklyDigest(data WeeklyDigestData) (subject, textBody, htmlBody string, err error) {
	sub, err := renderText(tDigestSub, data)
	if err != nil {
		return "", "", "", err
	}
	txt, err := renderText(tDigestText, data)
	if err != nil {
		return "", "", "", err
	}
	ht, err := renderHTML(hDigestHTML, data)
	if err != nil {
		return "", "", "", err
	}
	return strings.TrimSpace(sub), txt, ht, nil
}

// RenderReturnConfirmation renders the return receipt notification.
func RenderReturnConfirmation(data ReturnConfirmationData) (subject, textBody, htmlBody string, err error) {
	sub, err := renderText(tReturnSub, data)
	if err != nil {
		return "", "", "", err
	}
	txt, err := renderText(tReturnText, data)
	if err != nil {
		return "", "", "", err
	}
	ht, err := renderHTML(hReturnHTML, data)
	if err != nil {
		return "", "", "", err
	}
	return strings.TrimSpace(sub), txt, ht, nil
}
