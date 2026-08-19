// Package cliimport holds the bulk-import logic behind `hdms-cli import
// devices|users` (docs/phases/phase-1-identity-catalog-credentials.md,
// task 1.6). It is a plain library package, not a module — it orchestrates
// identityapi/catalogapi/credentialsapi the same way cmd/hdms-api's
// apiserver package does, and exists as a separate package (rather than
// living directly in cmd/hdms-cli) so test/integration can exercise it
// without shelling out to the built binary.
package cliimport

import (
	"fmt"
	"io"
	"strings"
)

// Outcome is what happened (or would happen, in a dry run) to one row.
type Outcome string

const (
	OutcomeCreated  Outcome = "created"
	OutcomeUpdated  Outcome = "updated"
	OutcomeRejected Outcome = "rejected"
)

// RowResult is the per-row outcome of an import run, keyed by its 1-based
// line number in the source file (the header is line 1, so the first data
// row is line 2 — matching how an administrator would count rows in a
// spreadsheet).
type RowResult struct {
	Line            int
	Ref             string // asset tag or employee number
	Outcome         Outcome
	Reason          string // populated only when Outcome is Rejected
	CredentialToken string // populated only when a credential was minted
}

// Report is the full outcome of one import run.
type Report struct {
	DryRun bool
	Rows   []RowResult
}

// Counts summarises Rows by outcome.
func (r Report) Counts() (created, updated, rejected int) {
	for _, row := range r.Rows {
		switch row.Outcome {
		case OutcomeCreated:
			created++
		case OutcomeUpdated:
			updated++
		case OutcomeRejected:
			rejected++
		}
	}
	return created, updated, rejected
}

// Print writes a per-row report followed by a summary line, in the format
// `hdms-cli import` shows on stdout. The returned error is the first write
// failure encountered, if any — callers writing to os.Stdout can safely
// ignore it, but the return value exists so a caller writing to a file can
// check it.
func (r Report) Print(w io.Writer) error {
	var lines []string
	if r.DryRun {
		lines = append(lines, "DRY RUN — no changes were written.")
	}
	for _, row := range r.Rows {
		switch row.Outcome {
		case OutcomeRejected:
			lines = append(lines, fmt.Sprintf("line %d: %s — REJECTED: %s", row.Line, row.Ref, row.Reason))
		default:
			line := fmt.Sprintf("line %d: %s — %s", row.Line, row.Ref, strings.ToUpper(string(row.Outcome)))
			if row.CredentialToken != "" {
				line += fmt.Sprintf(" (credential minted: %s)", row.CredentialToken)
			}
			lines = append(lines, line)
		}
	}
	created, updated, rejected := r.Counts()
	lines = append(lines, "", fmt.Sprintf("%d created, %d updated, %d rejected (%d rows total)", created, updated, rejected, len(r.Rows)))

	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}
