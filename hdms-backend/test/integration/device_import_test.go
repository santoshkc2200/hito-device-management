//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestDeviceImport_PreviewAndCommit(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	csvContent := `asset_tag,name,category,manufacturer,model,serial_no,home_location,notes,acquired_on
DEV-101,Vital Signs Monitor,Monitors,Philips,VS-4,SN-1001,Ward 4,New unit,2025-01-15
DEV-102,Infusion Pump,Pumps,Baxter,Sigma Spectrum,SN-1002,ICU,,2025-02-20
`

	// 1. Preview
	prevResp := h.doCSV(t, "/v1/imports/devices/preview", csvContent)
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
	commitResp := h.doJSON(t, http.MethodPost, "/v1/imports/devices", "", gen.CommitImportRequest{
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

	// 3. Verify devices in DB
	d1, err := h.catalog.LookupDeviceByAssetTag(ctx, "DEV-101")
	if err != nil {
		t.Fatalf("lookup DEV-101: %v", err)
	}
	if d1.Name != "Vital Signs Monitor" || d1.Manufacturer != "Philips" || d1.Model != "VS-4" {
		t.Fatalf("device 1 mismatch: %+v", d1)
	}

	d2, err := h.catalog.LookupDeviceByAssetTag(ctx, "DEV-102")
	if err != nil {
		t.Fatalf("lookup DEV-102: %v", err)
	}
	if d2.Name != "Infusion Pump" || d2.Manufacturer != "Baxter" {
		t.Fatalf("device 2 mismatch: %+v", d2)
	}

	// 4. Verify import_batches record
	batch, err := h.identity.GetImportBatch(ctx, result.ImportId)
	if err != nil {
		t.Fatalf("get import batch: %v", err)
	}
	if batch.Kind != "devices" || batch.TotalRows != 2 || batch.CreatedCount != 2 {
		t.Fatalf("batch record mismatch: %+v", batch)
	}
}

func TestDeviceImport_InvalidRowRefusesCommit(t *testing.T) {
	h := newTestHarness(t)

	// One valid row, one invalid row (missing name)
	csvContent := `asset_tag,name,category
DEV-201,Valid Device,Laptops
DEV-202,,Laptops
`

	prevResp := h.doCSV(t, "/v1/imports/devices/preview", csvContent)
	if prevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", prevResp.StatusCode)
	}
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	if preview.Summary.TotalRows != 2 || preview.Summary.CreateCount != 1 || preview.Summary.InvalidCount != 1 {
		t.Fatalf("expected 1 create and 1 invalid, got %+v", preview.Summary)
	}

	// Commit must be refused
	commitResp := h.doJSON(t, http.MethodPost, "/v1/imports/devices", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("commit status = %d, want 422", commitResp.StatusCode)
	}

	// Corrected CSV
	correctedCSV := `asset_tag,name,category
DEV-201,Valid Device,Laptops
DEV-202,Fixed Device,Laptops
`
	prevResp2 := h.doCSV(t, "/v1/imports/devices/preview", correctedCSV)
	if prevResp2.StatusCode != http.StatusOK {
		t.Fatalf("second preview status = %d", prevResp2.StatusCode)
	}
	preview2 := decodeBody[gen.ImportPreview](t, prevResp2)
	if preview2.Summary.InvalidCount != 0 || preview2.Summary.CreateCount != 2 {
		t.Fatalf("unexpected summary for corrected preview: %+v", preview2.Summary)
	}

	commitResp2 := h.doJSON(t, http.MethodPost, "/v1/imports/devices", "", gen.CommitImportRequest{
		PreviewId: preview2.PreviewId,
	})
	if commitResp2.StatusCode != http.StatusOK {
		t.Fatalf("second commit status = %d, want 200", commitResp2.StatusCode)
	}
}

func TestDeviceImport_InFileDuplicatesDetected(t *testing.T) {
	h := newTestHarness(t)

	csvContent := `asset_tag,name,category
DEV-301,First Device,Tablets
dev-301,Duplicate Device (case insensitive),Tablets
`

	prevResp := h.doCSV(t, "/v1/imports/devices/preview", csvContent)
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

func TestDeviceImport_UpdatesExistingLiveDevice(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Handhelds"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	// Pre-create device
	_, err = h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:     "DEV-401",
		Name:         "Initial Name",
		CategoryID:   cat.ID,
		Manufacturer: "OldMake",
		Model:        "OldModel",
		SerialNo:     "SN-401",
		HomeLocation: "Storage",
		Notes:        "Initial notes",
	}, "admin:pre-existing")
	if err != nil {
		t.Fatalf("create initial device: %v", err)
	}

	csvContent := `asset_tag,name,category,manufacturer,model,serial_no,home_location,notes
DEV-401,Updated Device Name,Handhelds,NewMake,NewModel,SN-401-UPD,Desk,Updated notes
`

	prevResp := h.doCSV(t, "/v1/imports/devices/preview", csvContent)
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
	commitResp := h.doJSON(t, http.MethodPost, "/v1/imports/devices", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp.StatusCode != http.StatusOK {
		t.Fatalf("commit status = %d", commitResp.StatusCode)
	}

	updatedDev, err := h.catalog.LookupDeviceByAssetTag(ctx, "DEV-401")
	if err != nil {
		t.Fatalf("lookup updated device: %v", err)
	}
	if updatedDev.Name != "Updated Device Name" || updatedDev.Manufacturer != "NewMake" || updatedDev.Model != "NewModel" {
		t.Fatalf("device was not updated correctly: %+v", updatedDev)
	}
}

func TestDeviceImport_DuplicateCommitIdempotent(t *testing.T) {
	h := newTestHarness(t)

	csvContent := `asset_tag,name,category
DEV-501,Idempotent Device,Laptops
`

	prevResp := h.doCSV(t, "/v1/imports/devices/preview", csvContent)
	preview := decodeBody[gen.ImportPreview](t, prevResp)

	// First commit
	commitResp1 := h.doJSON(t, http.MethodPost, "/v1/imports/devices", "", gen.CommitImportRequest{
		PreviewId: preview.PreviewId,
	})
	if commitResp1.StatusCode != http.StatusOK {
		t.Fatalf("first commit status = %d", commitResp1.StatusCode)
	}
	res1 := decodeBody[gen.ImportResult](t, commitResp1)

	// Second commit
	commitResp2 := h.doJSON(t, http.MethodPost, "/v1/imports/devices", "", gen.CommitImportRequest{
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
