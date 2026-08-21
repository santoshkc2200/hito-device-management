//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (h *testHarness) doCSV(t *testing.T, path, csvData string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.server.URL+path, strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "text/csv")
	if h.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", h.csrfToken)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestUserImport_PreviewAndCommit(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	csvContent := `employee_no,full_name,department,email,phone,notes
EMP-101,Nurse Alice,Emergency,alice@example.org,555-0101,Shift A
EMP-102,Dr. Bob,Cardiology,bob@example.org,555-0102,Shift B
`

	// 1. Preview
	prevResp := h.doCSV(t, "/v1/imports/users/preview", csvContent)
	if prevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", prevResp.StatusCode)
	}
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	if preview.PreviewId == "" {
		t.Fatal("expected non-empty previewId")
	}
	if preview.Summary.TotalRows != 2 || preview.Summary.CreateCount != 2 || preview.Summary.InvalidCount != 0 {
		t.Fatalf("unexpected preview summary: %+v", preview.Summary)
	}
	if len(preview.Rows) != 2 {
		t.Fatalf("expected 2 preview rows, got %d", len(preview.Rows))
	}
	if preview.Rows[0].Action != gen.Create || preview.Rows[1].Action != gen.Create {
		t.Fatalf("expected both rows to be create action, got %v and %v", preview.Rows[0].Action, preview.Rows[1].Action)
	}

	// 2. Commit
	commitResp := h.doJSON(t, http.MethodPost, "/v1/imports/users", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp.StatusCode != http.StatusOK {
		t.Fatalf("commit status = %d, want 200", commitResp.StatusCode)
	}
	result := decodeBody[gen.ImportResult](t, commitResp)

	if result.ImportId == "" {
		t.Fatal("expected non-empty importId")
	}
	if result.CreatedCount != 2 || result.UpdatedCount != 0 || result.SkippedCount != 0 {
		t.Fatalf("unexpected import result counts: %+v", result)
	}
	if result.CreatedSubjectIds == nil || len(*result.CreatedSubjectIds) != 2 {
		t.Fatalf("expected 2 created subject IDs, got %+v", result.CreatedSubjectIds)
	}

	// 3. Verify users in DB with provenance
	u1, err := h.identity.LookupUserByEmployeeNo(ctx, "EMP-101")
	if err != nil {
		t.Fatalf("lookup EMP-101: %v", err)
	}
	if u1.FullName != "Nurse Alice" || u1.Email != "alice@example.org" {
		t.Fatalf("user 1 mismatch: %+v", u1)
	}
	if !strings.HasPrefix(u1.RegisteredBy, "import:"+result.ImportId) {
		t.Fatalf("user 1 registeredBy = %q, want prefix import:%s", u1.RegisteredBy, result.ImportId)
	}
	if u1.ImportBatchID != result.ImportId {
		t.Fatalf("user 1 importBatchId = %q, want %q", u1.ImportBatchID, result.ImportId)
	}

	u2, err := h.identity.LookupUserByEmployeeNo(ctx, "EMP-102")
	if err != nil {
		t.Fatalf("lookup EMP-102: %v", err)
	}
	if u2.FullName != "Dr. Bob" {
		t.Fatalf("user 2 mismatch: %+v", u2)
	}

	// 4. Verify import_batches record
	batch, err := h.identity.GetImportBatch(ctx, result.ImportId)
	if err != nil {
		t.Fatalf("get import batch: %v", err)
	}
	if batch.Kind != "users" || batch.TotalRows != 2 || batch.CreatedCount != 2 {
		t.Fatalf("batch record mismatch: %+v", batch)
	}
}

func TestUserImport_InvalidRowRefusesCommit(t *testing.T) {
	h := newTestHarness(t)

	// One valid row, one invalid row (missing employee_no)
	csvContent := `employee_no,full_name,department
EMP-201,Valid User,Pediatrics
,Missing EmpNo User,Pediatrics
`

	prevResp := h.doCSV(t, "/v1/imports/users/preview", csvContent)
	if prevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", prevResp.StatusCode)
	}
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	if preview.Summary.TotalRows != 2 || preview.Summary.CreateCount != 1 || preview.Summary.InvalidCount != 1 {
		t.Fatalf("expected 1 create and 1 invalid, got %+v", preview.Summary)
	}

	// Commit must be refused
	commitResp := h.doJSON(t, http.MethodPost, "/v1/imports/users", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("commit status = %d, want 422", commitResp.StatusCode)
	}

	// Corrected CSV
	correctedCSV := `employee_no,full_name,department
EMP-201,Valid User,Pediatrics
EMP-202,Fixed User,Pediatrics
`
	prevResp2 := h.doCSV(t, "/v1/imports/users/preview", correctedCSV)
	if prevResp2.StatusCode != http.StatusOK {
		t.Fatalf("second preview status = %d", prevResp2.StatusCode)
	}
	preview2 := decodeBody[gen.ImportPreview](t, prevResp2)
	if preview2.Summary.InvalidCount != 0 || preview2.Summary.CreateCount != 2 {
		t.Fatalf("unexpected summary for corrected preview: %+v", preview2.Summary)
	}

	commitResp2 := h.doJSON(t, http.MethodPost, "/v1/imports/users", "", gen.CommitImportRequest{
		PreviewId: preview2.PreviewId,
	})
	if commitResp2.StatusCode != http.StatusOK {
		t.Fatalf("second commit status = %d, want 200", commitResp2.StatusCode)
	}
}

func TestUserImport_InFileDuplicatesDetected(t *testing.T) {
	h := newTestHarness(t)

	csvContent := `employee_no,full_name,department
EMP-301,First Instance,Emergency
EMP-301,Duplicate Instance,ICU
`

	prevResp := h.doCSV(t, "/v1/imports/users/preview", csvContent)
	if prevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", prevResp.StatusCode)
	}
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	if preview.Summary.InvalidCount != 1 || preview.Summary.CreateCount != 1 {
		t.Fatalf("expected 1 create and 1 invalid for in-file duplicate, got %+v", preview.Summary)
	}

	if preview.Rows[1].Action != gen.Invalid {
		t.Fatalf("expected row 2 action to be invalid, got %s", preview.Rows[1].Action)
	}
	if preview.Rows[1].Problems == nil || len(*preview.Rows[1].Problems) == 0 {
		t.Fatal("expected problems reported on row 2")
	}
	if (*preview.Rows[1].Problems)[0].Code != "duplicate_in_file" {
		t.Fatalf("expected code duplicate_in_file, got %q", (*preview.Rows[1].Problems)[0].Code)
	}
}

func TestUserImport_UpdatesExistingLiveUser(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	// Pre-create user
	_, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-401",
		FullName:     "Initial Name",
		RegisteredBy: "admin:pre-existing",
	})
	if err != nil {
		t.Fatalf("create initial user: %v", err)
	}

	csvContent := `employee_no,full_name,department,email
EMP-401,Updated Full Name,Radiology,updated@example.org
`

	prevResp := h.doCSV(t, "/v1/imports/users/preview", csvContent)
	if prevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d", prevResp.StatusCode)
	}
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	if preview.Summary.UpdateCount != 1 || preview.Summary.CreateCount != 0 {
		t.Fatalf("expected 1 update, got %+v", preview.Summary)
	}
	if preview.Rows[0].Action != gen.Update {
		t.Fatalf("expected row action update, got %s", preview.Rows[0].Action)
	}

	// Commit
	commitResp := h.doJSON(t, http.MethodPost, "/v1/imports/users", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp.StatusCode != http.StatusOK {
		t.Fatalf("commit status = %d", commitResp.StatusCode)
	}

	updatedUser, err := h.identity.LookupUserByEmployeeNo(ctx, "EMP-401")
	if err != nil {
		t.Fatalf("lookup updated user: %v", err)
	}
	if updatedUser.FullName != "Updated Full Name" || updatedUser.Email != "updated@example.org" {
		t.Fatalf("user was not updated correctly: %+v", updatedUser)
	}
}

func TestUserImport_DuplicateCommitIdempotent(t *testing.T) {
	h := newTestHarness(t)

	csvContent := `employee_no,full_name,department
EMP-501,Idempotent User,Surgery
`

	prevResp := h.doCSV(t, "/v1/imports/users/preview", csvContent)
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	// First commit
	commitResp1 := h.doJSON(t, http.MethodPost, "/v1/imports/users", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp1.StatusCode != http.StatusOK {
		t.Fatalf("first commit status = %d", commitResp1.StatusCode)
	}
	res1 := decodeBody[gen.ImportResult](t, commitResp1)

	// Second commit
	commitResp2 := h.doJSON(t, http.MethodPost, "/v1/imports/users", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp2.StatusCode != http.StatusOK {
		t.Fatalf("second commit status = %d", commitResp2.StatusCode)
	}
	res2 := decodeBody[gen.ImportResult](t, commitResp2)

	if res1.ImportId != res2.ImportId {
		t.Fatalf("expected same import ID on repeated commit, got %q vs %q", res1.ImportId, res2.ImportId)
	}
}
