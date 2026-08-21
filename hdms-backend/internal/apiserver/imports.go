package apiserver

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

type userImportPreviewCacheItem struct {
	Preview   gen.ImportPreview
	Rows      []map[string]string
	ExpiresAt time.Time
	Result    *gen.ImportResult
}

type deviceImportPreviewCacheItem struct {
	Preview   gen.ImportPreview
	Rows      []map[string]string
	ExpiresAt time.Time
	Result    *gen.ImportResult
}

// PreviewUserImport parses, validates, and reports the per-row outcome of a users CSV file.
func (s *Server) PreviewUserImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB limit
	cr := csv.NewReader(r.Body)
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			writeValidationFailed(w, r, "CSV file is empty", nil)
			return
		}
		writeValidationFailed(w, r, "CSV header is malformed: "+err.Error(), nil)
		return
	}

	colMap := make(map[string]int, len(header))
	for i, col := range header {
		colMap[strings.TrimSpace(col)] = i
	}

	var missing []string
	for _, req := range []string{"employee_no", "full_name"} {
		if _, ok := colMap[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		writeValidationFailed(w, r, fmt.Sprintf("missing required column(s): %s", strings.Join(missing, ", ")), missing)
		return
	}

	var (
		rowPreviews  []gen.ImportRowPreview
		rawRows      []map[string]string
		seenInFile   = make(map[string]int) // normalized employee_no -> lineNo
		createCount  int
		updateCount  int
		skipCount    int
		invalidCount int
	)

	lineNo := 1 // header is line 1
	for {
		record, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		lineNo++
		if err != nil {
			writeValidationFailed(w, r, fmt.Sprintf("line %d: malformed CSV row: %v", lineNo, err), nil)
			return
		}

		rowValues := make(map[string]string, len(header))
		for col, idx := range colMap {
			if idx < len(record) {
				rowValues[col] = strings.TrimSpace(record[idx])
			} else {
				rowValues[col] = ""
			}
		}
		rawRows = append(rawRows, rowValues)

		var problems []gen.ImportProblem

		empNo := rowValues["employee_no"]
		if empNo == "" {
			problems = append(problems, gen.ImportProblem{
				Field:   strPtr("employee_no"),
				Code:    "required",
				Message: "employee_no is required",
			})
		} else if _, err := identityapi.ValidateEmployeeNo(empNo); err != nil {
			problems = append(problems, gen.ImportProblem{
				Field:   strPtr("employee_no"),
				Code:    "invalid_format",
				Message: err.Error(),
			})
		} else {
			normEmp := strings.ToLower(empNo)
			if prevLine, exists := seenInFile[normEmp]; exists {
				problems = append(problems, gen.ImportProblem{
					Field:   strPtr("employee_no"),
					Code:    "duplicate_in_file",
					Message: fmt.Sprintf("employee_no %q is duplicated from line %d", empNo, prevLine),
				})
			} else {
				seenInFile[normEmp] = lineNo
			}
		}

		fullName := rowValues["full_name"]
		if fullName == "" {
			problems = append(problems, gen.ImportProblem{
				Field:   strPtr("full_name"),
				Code:    "required",
				Message: "full_name is required",
			})
		}

		var action gen.ImportRowAction
		if len(problems) > 0 {
			action = gen.Invalid
			invalidCount++
		} else {
			// Check database
			_, err := s.identity.LookupUserByEmployeeNo(r.Context(), empNo)
			if err == nil {
				action = gen.Update
				updateCount++
			} else if errors.Is(err, identityapi.ErrUserNotFound) {
				action = gen.Create
				createCount++
			} else {
				action = gen.Invalid
				problems = append(problems, gen.ImportProblem{
					Code:    "lookup_failed",
					Message: err.Error(),
				})
				invalidCount++
			}
		}

		rowPreview := gen.ImportRowPreview{
			LineNo: lineNo,
			Action: action,
			Values: rowValues,
		}
		if len(problems) > 0 {
			rowPreview.Problems = &problems
		}
		rowPreviews = append(rowPreviews, rowPreview)
	}

	if len(rowPreviews) == 0 {
		writeValidationFailed(w, r, "CSV file contains no data rows", nil)
		return
	}

	previewID := ids.New()
	expiresAt := time.Now().Add(30 * time.Minute)

	preview := gen.ImportPreview{
		PreviewId: previewID,
		ExpiresAt: expiresAt,
		Columns:   header,
		Rows:      rowPreviews,
		Summary: gen.ImportSummary{
			TotalRows:    len(rowPreviews),
			CreateCount:  createCount,
			UpdateCount:  updateCount,
			SkipCount:    skipCount,
			InvalidCount: invalidCount,
		},
	}

	s.importMu.Lock()
	s.userImportPreviews[previewID] = &userImportPreviewCacheItem{
		Preview:   preview,
		Rows:      rawRows,
		ExpiresAt: expiresAt,
	}
	s.importMu.Unlock()

	writeJSON(w, http.StatusOK, preview)
}

// CommitUserImport commits a previewed users import batch atomically.
func (s *Server) CommitUserImport(w http.ResponseWriter, r *http.Request, _ gen.CommitUserImportParams) {
	req, ok := decodeJSON[gen.CommitImportRequest](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.PreviewId) == "" {
		writeValidationFailed(w, r, "previewId is required", []string{"previewId"})
		return
	}

	s.importMu.Lock()
	item, exists := s.userImportPreviews[req.PreviewId]
	s.importMu.Unlock()

	if !exists || time.Now().After(item.ExpiresAt) {
		p := httpx.NewProblem("preview-not-found", "Import preview not found or expired", http.StatusNotFound)
		httpx.WriteProblem(w, r, p)
		return
	}

	if item.Result != nil {
		writeJSON(w, http.StatusOK, *item.Result)
		return
	}

	if item.Preview.Summary.InvalidCount > 0 {
		p := httpx.NewProblem("invalid-batch", "Cannot commit import batch containing invalid rows", http.StatusUnprocessableEntity)
		p.Detail = fmt.Sprintf("The batch has %d invalid row(s). Fix all errors before committing.", item.Preview.Summary.InvalidCount)
		httpx.WriteProblem(w, r, p)
		return
	}

	actor := actorFrom(r)
	if actor == "" {
		actor = "admin"
	}
	batchID := ids.New()
	createdSubjectIds := make([]string, 0, item.Preview.Summary.CreateCount)

	_, err := s.identity.CreateImportBatch(r.Context(), identityapi.CreateImportBatchParams{
		ID:           batchID,
		Kind:         "users",
		Actor:        actor,
		Filename:     "users.csv",
		TotalRows:    item.Preview.Summary.TotalRows,
		CreatedCount: item.Preview.Summary.CreateCount,
		UpdatedCount: item.Preview.Summary.UpdateCount,
		SkippedCount: item.Preview.Summary.SkipCount,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	for i, rowData := range item.Rows {
		rowPreview := item.Preview.Rows[i]
		var deptID string
		if deptName := strings.TrimSpace(rowData["department"]); deptName != "" {
			d, err := s.identity.GetOrCreateDepartment(r.Context(), deptName)
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
			deptID = d.ID
		}

		switch rowPreview.Action {
		case gen.Create:
			user, err := s.identity.CreateUser(r.Context(), identityapi.CreateUserParams{
				EmployeeNo:    rowData["employee_no"],
				FullName:      rowData["full_name"],
				DepartmentID:  deptID,
				Email:         rowData["email"],
				Phone:         rowData["phone"],
				Notes:         rowData["notes"],
				RegisteredBy:  "import:" + batchID,
				ImportBatchID: batchID,
			})
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
			createdSubjectIds = append(createdSubjectIds, user.ID)

		case gen.Update:
			existing, err := s.identity.LookupUserByEmployeeNo(r.Context(), rowData["employee_no"])
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
			_, err = s.identity.UpdateUser(r.Context(), existing.ID, identityapi.UpdateUserParams{
				FullName:     rowData["full_name"],
				DepartmentID: deptID,
				Email:        rowData["email"],
				Phone:        rowData["phone"],
				Notes:        rowData["notes"],
			}, actor)
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
		}
	}

	result := gen.ImportResult{
		ImportId:          batchID,
		CreatedCount:      item.Preview.Summary.CreateCount,
		UpdatedCount:      item.Preview.Summary.UpdateCount,
		SkippedCount:      item.Preview.Summary.SkipCount,
		CreatedSubjectIds: &createdSubjectIds,
	}

	s.importMu.Lock()
	item.Result = &result
	s.importMu.Unlock()

	writeJSON(w, http.StatusOK, result)
}

// PreviewDeviceImport parses, validates, and reports the per-row outcome of a devices CSV file.
func (s *Server) PreviewDeviceImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB limit
	cr := csv.NewReader(r.Body)
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			writeValidationFailed(w, r, "CSV file is empty", nil)
			return
		}
		writeValidationFailed(w, r, "CSV header is malformed: "+err.Error(), nil)
		return
	}

	colMap := make(map[string]int, len(header))
	for i, col := range header {
		colMap[strings.TrimSpace(col)] = i
	}

	var missing []string
	for _, req := range []string{"asset_tag", "name", "category"} {
		if _, ok := colMap[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		writeValidationFailed(w, r, fmt.Sprintf("missing required column(s): %s", strings.Join(missing, ", ")), missing)
		return
	}

	var (
		rowPreviews  []gen.ImportRowPreview
		rawRows      []map[string]string
		seenInFile   = make(map[string]int) // normalized upper asset_tag -> lineNo
		createCount  int
		updateCount  int
		skipCount    int
		invalidCount int
	)

	lineNo := 1 // header is line 1
	for {
		record, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		lineNo++
		if err != nil {
			writeValidationFailed(w, r, fmt.Sprintf("line %d: malformed CSV row: %v", lineNo, err), nil)
			return
		}

		rowValues := make(map[string]string, len(header))
		for col, idx := range colMap {
			if idx < len(record) {
				rowValues[col] = strings.TrimSpace(record[idx])
			} else {
				rowValues[col] = ""
			}
		}
		rawRows = append(rawRows, rowValues)

		var problems []gen.ImportProblem

		assetTag := rowValues["asset_tag"]
		if assetTag == "" {
			problems = append(problems, gen.ImportProblem{
				Field:   strPtr("asset_tag"),
				Code:    "required",
				Message: "asset_tag is required",
			})
		} else {
			normTag := strings.ToUpper(assetTag)
			if prevLine, exists := seenInFile[normTag]; exists {
				problems = append(problems, gen.ImportProblem{
					Field:   strPtr("asset_tag"),
					Code:    "duplicate_in_file",
					Message: fmt.Sprintf("asset_tag %q is duplicated from line %d", assetTag, prevLine),
				})
			} else {
				seenInFile[normTag] = lineNo
			}
		}

		name := rowValues["name"]
		if name == "" {
			problems = append(problems, gen.ImportProblem{
				Field:   strPtr("name"),
				Code:    "required",
				Message: "name is required",
			})
		}

		category := rowValues["category"]
		if category == "" {
			problems = append(problems, gen.ImportProblem{
				Field:   strPtr("category"),
				Code:    "required",
				Message: "category is required",
			})
		}

		if rawAcquired := rowValues["acquired_on"]; rawAcquired != "" {
			if _, err := time.Parse("2006-01-02", rawAcquired); err != nil {
				problems = append(problems, gen.ImportProblem{
					Field:   strPtr("acquired_on"),
					Code:    "invalid_format",
					Message: fmt.Sprintf("acquired_on %q is invalid, expected YYYY-MM-DD", rawAcquired),
				})
			}
		}

		var action gen.ImportRowAction
		if len(problems) > 0 {
			action = gen.Invalid
			invalidCount++
		} else {
			// Check database
			_, err := s.catalog.LookupDeviceByAssetTag(r.Context(), assetTag)
			if err == nil {
				action = gen.Update
				updateCount++
			} else if errors.Is(err, catalogapi.ErrDeviceNotFound) {
				action = gen.Create
				createCount++
			} else {
				action = gen.Invalid
				problems = append(problems, gen.ImportProblem{
					Code:    "lookup_failed",
					Message: err.Error(),
				})
				invalidCount++
			}
		}

		rowPreview := gen.ImportRowPreview{
			LineNo: lineNo,
			Action: action,
			Values: rowValues,
		}
		if len(problems) > 0 {
			rowPreview.Problems = &problems
		}
		rowPreviews = append(rowPreviews, rowPreview)
	}

	if len(rowPreviews) == 0 {
		writeValidationFailed(w, r, "CSV file contains no data rows", nil)
		return
	}

	previewID := ids.New()
	expiresAt := time.Now().Add(30 * time.Minute)

	preview := gen.ImportPreview{
		PreviewId: previewID,
		ExpiresAt: expiresAt,
		Columns:   header,
		Rows:      rowPreviews,
		Summary: gen.ImportSummary{
			TotalRows:    len(rowPreviews),
			CreateCount:  createCount,
			UpdateCount:  updateCount,
			SkipCount:    skipCount,
			InvalidCount: invalidCount,
		},
	}

	s.importMu.Lock()
	s.deviceImportPreviews[previewID] = &deviceImportPreviewCacheItem{
		Preview:   preview,
		Rows:      rawRows,
		ExpiresAt: expiresAt,
	}
	s.importMu.Unlock()

	writeJSON(w, http.StatusOK, preview)
}

// CommitDeviceImport commits a previewed devices import batch atomically.
func (s *Server) CommitDeviceImport(w http.ResponseWriter, r *http.Request, _ gen.CommitDeviceImportParams) {
	req, ok := decodeJSON[gen.CommitImportRequest](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.PreviewId) == "" {
		writeValidationFailed(w, r, "previewId is required", []string{"previewId"})
		return
	}

	s.importMu.Lock()
	item, exists := s.deviceImportPreviews[req.PreviewId]
	s.importMu.Unlock()

	if !exists || time.Now().After(item.ExpiresAt) {
		p := httpx.NewProblem("preview-not-found", "Import preview not found or expired", http.StatusNotFound)
		httpx.WriteProblem(w, r, p)
		return
	}

	if item.Result != nil {
		writeJSON(w, http.StatusOK, *item.Result)
		return
	}

	if item.Preview.Summary.InvalidCount > 0 {
		p := httpx.NewProblem("invalid-batch", "Cannot commit import batch containing invalid rows", http.StatusUnprocessableEntity)
		p.Detail = fmt.Sprintf("The batch has %d invalid row(s). Fix all errors before committing.", item.Preview.Summary.InvalidCount)
		httpx.WriteProblem(w, r, p)
		return
	}

	actor := actorFrom(r)
	if actor == "" {
		actor = "admin"
	}
	batchID := ids.New()
	createdSubjectIds := make([]string, 0, item.Preview.Summary.CreateCount)

	_, err := s.identity.CreateImportBatch(r.Context(), identityapi.CreateImportBatchParams{
		ID:           batchID,
		Kind:         "devices",
		Actor:        actor,
		Filename:     "devices.csv",
		TotalRows:    item.Preview.Summary.TotalRows,
		CreatedCount: item.Preview.Summary.CreateCount,
		UpdatedCount: item.Preview.Summary.UpdateCount,
		SkippedCount: item.Preview.Summary.SkipCount,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	for i, rowData := range item.Rows {
		rowPreview := item.Preview.Rows[i]
		cat, err := s.catalog.GetOrCreateCategory(r.Context(), rowData["category"])
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}

		var acquiredOn *time.Time
		if rawAcquired := rowData["acquired_on"]; rawAcquired != "" {
			if t, err := time.Parse("2006-01-02", rawAcquired); err == nil {
				acquiredOn = &t
			}
		}

		switch rowPreview.Action {
		case gen.Create:
			dev, err := s.catalog.CreateDevice(r.Context(), catalogapi.CreateDeviceParams{
				AssetTag:     rowData["asset_tag"],
				Name:         rowData["name"],
				CategoryID:   cat.ID,
				Manufacturer: rowData["manufacturer"],
				Model:        rowData["model"],
				SerialNo:     rowData["serial_no"],
				HomeLocation: rowData["home_location"],
				Notes:        rowData["notes"],
				AcquiredOn:   acquiredOn,
			}, "import:"+batchID)
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
			createdSubjectIds = append(createdSubjectIds, dev.ID)

		case gen.Update:
			existing, err := s.catalog.LookupDeviceByAssetTag(r.Context(), rowData["asset_tag"])
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
			_, err = s.catalog.UpdateDevice(r.Context(), existing.ID, catalogapi.UpdateDeviceParams{
				Name:         rowData["name"],
				CategoryID:   cat.ID,
				Manufacturer: rowData["manufacturer"],
				Model:        rowData["model"],
				SerialNo:     rowData["serial_no"],
				HomeLocation: rowData["home_location"],
				Notes:        rowData["notes"],
				AcquiredOn:   acquiredOn,
			}, "import:"+batchID)
			if err != nil {
				s.writeServiceError(w, r, err)
				return
			}
		}
	}

	result := gen.ImportResult{
		ImportId:          batchID,
		CreatedCount:      item.Preview.Summary.CreateCount,
		UpdatedCount:      item.Preview.Summary.UpdateCount,
		SkippedCount:      item.Preview.Summary.SkipCount,
		CreatedSubjectIds: &createdSubjectIds,
	}

	s.importMu.Lock()
	item.Result = &result
	s.importMu.Unlock()

	writeJSON(w, http.StatusOK, result)
}
