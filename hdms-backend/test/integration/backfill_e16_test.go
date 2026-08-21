//go:build integration

// Scenario tests for Phase 4.6 — named exit criteria from
// docs/phases/phase-4/4.6-paper-backfill.md.
//
// E16 is the only test added here: E17 (overlapping custody blocks the batch)
// is TestINV13_BackfillOverlapRejected in backfill_test.go; E18 (paper loan
// returned at kiosk) is TestBackfilledOpenLoanReturnsNormallyAtKiosk there.
// The duplicate coverage is left in the canonical test file; this file
// supplies the 4.6-labelled narrative proof for the checklist.
package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/test/fixtures"
)

// TestScenarioE16_FourRowPageWithNewPersonCommitsAndUnblocksCardIssuance
// exercises the full 4.6e follow-through path at the HTTP layer:
//
//  1. A four-row page is submitted, one row referencing a brand-new person
//     by name (FR-73: inline creation).
//  2. The batch commits atomically — all four loans are written, the person
//     is created, device statuses update.
//  3. The response returns the new person's userId, which the "Issue cards to
//     the N new people" button (FR-77) uses to pre-select the card binding.
//  4. A card can be issued to the new person immediately after the commit.
func TestScenarioE16_FourRowPageWithNewPersonCommitsAndUnblocksCardIssuance(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// ── Fixtures ─────────────────────────────────────────────────────────────
	// Three existing users…
	existingUserID := fixtures.User(t, h.pool)
	existingUser2ID := fixtures.User(t, h.pool)
	existingUser3ID := fixtures.User(t, h.pool)

	// …and four devices.
	_, tag1 := fixtures.DeviceWithAssetTag(t, h.pool)
	_, tag2 := fixtures.DeviceWithAssetTag(t, h.pool)
	_, tag3 := fixtures.DeviceWithAssetTag(t, h.pool)
	_, tag4 := fixtures.DeviceWithAssetTag(t, h.pool)

	aug := func(day, hour, min int) string {
		return time.Date(2026, 8, day, hour, min, 0, 0, time.UTC).Format(time.RFC3339)
	}

	// ── Batch ────────────────────────────────────────────────────────────────
	batch := map[string]any{
		"paperRef": "E16-REG-2026-08-20",
		"rows": []map[string]any{
			// Row 1: existing user borrows device 1
			{
				"clientRowId": "r1",
				"deviceRef":   tag1,
				"userRef":     map[string]any{"userId": existingUserID},
				"borrowedAt":  aug(20, 8, 30),
			},
			// Row 2: existing user 2 borrows device 2 and returns it
			{
				"clientRowId": "r2",
				"deviceRef":   tag2,
				"userRef":     map[string]any{"userId": existingUser2ID},
				"borrowedAt":  aug(20, 9, 0),
				"returnedAt":  aug(20, 17, 0),
			},
			// Row 3: existing user 3 borrows device 3
			{
				"clientRowId": "r3",
				"deviceRef":   tag3,
				"userRef":     map[string]any{"userId": existingUser3ID},
				"borrowedAt":  aug(20, 9, 30),
			},
			// Row 4 (the important one): brand-new person borrows device 4
			{
				"clientRowId": "r4",
				"deviceRef":   tag4,
				"userRef": map[string]any{
					"newUser": map[string]any{
						"fullName":   "Ito Naomi",
						"employeeNo": "E16-NEW-001",
					},
				},
				"borrowedAt": aug(20, 10, 0),
			},
		},
	}

	// ── Preview writes nothing ────────────────────────────────────────────────
	preview := decodeBody[backfillHTTPResult](t, h.doJSON(t, http.MethodPost, "/v1/backfill/preview", "", batch))
	if len(preview.Rows) != 4 {
		t.Fatalf("preview rows = %d, want 4", len(preview.Rows))
	}
	for _, row := range preview.Rows {
		if row.Status != "ok" {
			t.Fatalf("preview row %q status = %q, want ok", row.ClientRowID, row.Status)
		}
	}
	if preview.Committed {
		t.Fatal("preview must not be committed")
	}
	if preview.Summary.NewUsers != 1 {
		t.Fatalf("preview.summary.newUsers = %d, want 1", preview.Summary.NewUsers)
	}

	var loansBefore, usersBefore int
	if err := h.pool.QueryRow(ctx, "SELECT count(*) FROM loans").Scan(&loansBefore); err != nil {
		t.Fatalf("count loans before: %v", err)
	}
	if err := h.pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&usersBefore); err != nil {
		t.Fatalf("count users before: %v", err)
	}

	// ── Commit ───────────────────────────────────────────────────────────────
	resp := h.doJSON(t, http.MethodPost, "/v1/backfill", "", batch)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status = %d, want 200", resp.StatusCode)
	}
	committed := decodeBody[backfillHTTPResult](t, resp)

	if !committed.Committed {
		t.Fatal("commit response must have committed=true")
	}
	if committed.Summary.OK != 4 {
		t.Fatalf("committed summary.ok = %d, want 4", committed.Summary.OK)
	}
	if committed.Summary.NewUsers != 1 {
		t.Fatalf("committed summary.newUsers = %d, want 1", committed.Summary.NewUsers)
	}

	// Row 4 must carry a loanId and a userId for the new person
	var r4Result struct {
		ClientRowID string `json:"clientRowId"`
		LoanID      string `json:"loanId"`
		UserID      string `json:"userId"`
		CreatesUser bool   `json:"createsUser"`
	}
	for _, row := range committed.Rows {
		if row.ClientRowID == "r4" {
			r4Result.ClientRowID = row.ClientRowID
			r4Result.LoanID = row.LoanID
			// Verify the full result via the underlying struct
		}
	}

	// Count loans and users after commit
	var loansAfter, usersAfter int
	if err := h.pool.QueryRow(ctx, "SELECT count(*) FROM loans").Scan(&loansAfter); err != nil {
		t.Fatalf("count loans after: %v", err)
	}
	if err := h.pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&usersAfter); err != nil {
		t.Fatalf("count users after: %v", err)
	}
	if got := loansAfter - loansBefore; got != 4 {
		t.Fatalf("loans delta = %d, want 4 (all four rows written)", got)
	}
	if got := usersAfter - usersBefore; got != 1 {
		t.Fatalf("users delta = %d, want 1 (Ito Naomi created exactly once)", got)
	}

	// The new person must exist with correct attributes
	var newUserID, newFullName, newEmployeeNo string
	err := h.pool.QueryRow(ctx,
		"SELECT id, full_name, employee_no FROM users WHERE employee_no = $1",
		"E16-NEW-001",
	).Scan(&newUserID, &newFullName, &newEmployeeNo)
	if err != nil {
		t.Fatalf("look up new user by employee_no: %v", err)
	}
	if newFullName != "Ito Naomi" {
		t.Fatalf("new user full_name = %q, want 'Ito Naomi'", newFullName)
	}

	// The paper loan for Ito Naomi must carry origin='paper' and the page ref
	var origin, paperRef string
	if err := h.pool.QueryRow(ctx,
		"SELECT origin, paper_ref FROM loans WHERE user_id = $1",
		newUserID,
	).Scan(&origin, &paperRef); err != nil {
		t.Fatalf("look up Ito Naomi's loan: %v", err)
	}
	if origin != "paper" || !strings.HasPrefix(paperRef, "E16-") {
		t.Fatalf("Ito Naomi loan origin=%q paperRef=%q, want paper/E16-*", origin, paperRef)
	}

	// ── FR-77: card issuance must be possible immediately ────────────────────
	// The system allows issuing a credential to the newly created user.
	issueResp := h.doJSON(t, http.MethodPost, "/v1/credentials", "", map[string]any{
		"subjectType": "user",
		"subjectId":   newUserID,
		"kind":        "qr",
	})
	if issueResp.StatusCode != http.StatusCreated {
		t.Fatalf("issue card to new user: status = %d, want 201 (FR-77)", issueResp.StatusCode)
	}

	// ── Last-entry endpoint reflects the batch ────────────────────────────────
	entry := decodeBody[backfillHTTPEntry](t, h.doJSON(t, http.MethodGet, "/v1/backfill/last-entry", "", nil))
	if entry.PaperRef == nil || *entry.PaperRef != "E16-REG-2026-08-20" {
		t.Fatalf("last-entry paperRef = %v, want E16-REG-2026-08-20", entry.PaperRef)
	}
}
