package apiserver

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// GetReportSummary returns aggregate utilization, duration, top borrowers, and category stats.
func (s *Server) GetReportSummary(w http.ResponseWriter, r *http.Request, params gen.GetReportSummaryParams) {
	if params.From == nil || params.To == nil || params.From.After(*params.To) {
		p := httpx.NewProblem("invalid-date-range", "Invalid date range", http.StatusBadRequest)
		p.Detail = "Both 'from' and 'to' parameters are required and 'from' must be <= 'to'."
		httpx.WriteProblem(w, r, p)
		return
	}

	from := *params.From
	to := *params.To

	stats, err := s.lending.GetReportSummaryStats(r.Context(), from, to)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	categories, err := s.catalog.ListCategories(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	liveDeviceCounts, err := s.catalog.CountLiveDevicesByCategory(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	windowDurationSeconds := to.Sub(from).Seconds()

	catStatMap := make(map[string]lendingapi.CategoryLoanStat, len(stats.CategoryStats))
	for _, cs := range stats.CategoryStats {
		catStatMap[cs.CategoryID] = cs
	}

	catUtilisations := make([]gen.CategoryUtilisation, 0, len(categories))
	for _, c := range categories {
		deviceCount := liveDeviceCounts[c.ID]
		cs := catStatMap[c.ID]

		var utilPct float32
		if deviceCount > 0 && windowDurationSeconds > 0 {
			utilPct = float32((cs.TotalLoanSeconds / (float64(deviceCount) * windowDurationSeconds)) * 100.0)
			if utilPct > 100.0 {
				utilPct = 100.0
			}
			if utilPct < 0.0 {
				utilPct = 0.0
			}
		}

		catUtilisations = append(catUtilisations, gen.CategoryUtilisation{
			CategoryId:           c.ID,
			CategoryName:         c.Name,
			DeviceCount:          deviceCount,
			LoanCount:            cs.LoanCount,
			AverageDurationHours: cs.AvgDurationHours,
			UtilisationPct:       utilPct,
		})
	}

	topBorrowers := make([]gen.TopBorrower, 0, len(stats.TopBorrowers))
	for _, tb := range stats.TopBorrowers {
		fullName := "Unknown User"
		empNo := ""
		if user, uErr := s.identity.LookupUser(r.Context(), tb.UserID); uErr == nil {
			fullName = user.FullName
			empNo = user.EmployeeNo
		}
		topBorrowers = append(topBorrowers, gen.TopBorrower{
			UserId:     tb.UserID,
			FullName:   fullName,
			EmployeeNo: empNo,
			LoanCount:  tb.LoanCount,
		})
	}

	writeJSON(w, http.StatusOK, gen.ReportSummary{
		From:                     from,
		To:                       to,
		TotalLoans:               stats.TotalLoans,
		OpenLoans:                stats.OpenLoans,
		OverdueCount:             stats.OverdueCount,
		OverdueRate:              stats.OverdueRate,
		AverageLoanDurationHours: stats.AvgDurationHours,
		UtilisationByCategory:    catUtilisations,
		TopBorrowers:             topBorrowers,
	})
}

// GetTransactionsByOrigin aggregates transaction counts bucketed by day, week, or month.
func (s *Server) GetTransactionsByOrigin(w http.ResponseWriter, r *http.Request, params gen.GetTransactionsByOriginParams) {
	if params.From == nil || params.To == nil || params.From.After(*params.To) {
		p := httpx.NewProblem("invalid-date-range", "Invalid date range", http.StatusBadRequest)
		p.Detail = "Both 'from' and 'to' parameters are required and 'from' must be <= 'to'."
		httpx.WriteProblem(w, r, p)
		return
	}

	from := *params.From
	to := *params.To
	bucket := "day"
	if params.Bucket != nil && string(*params.Bucket) != "" {
		bucket = string(*params.Bucket)
	}

	buckets, err := s.lending.GetTransactionsByOrigin(r.Context(), from, to, bucket)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	genBuckets := make([]gen.OriginBucket, 0, len(buckets))
	for _, b := range buckets {
		counts := make([]gen.OriginCount, 0, len(b.Counts))
		for _, c := range b.Counts {
			counts = append(counts, gen.OriginCount{
				Origin: gen.LoanOrigin(c.Origin),
				Count:  c.Count,
			})
		}
		genBuckets = append(genBuckets, gen.OriginBucket{
			PeriodStart: b.PeriodStart,
			Counts:      counts,
			Total:       b.Total,
		})
	}

	writeJSON(w, http.StatusOK, gen.OriginReport{
		From:    from,
		To:      to,
		Bucket:  gen.ReportBucket(bucket),
		Buckets: genBuckets,
	})
}

// GetOperationalHealth returns scan metrics, source breakdown, and rejection reasons.
func (s *Server) GetOperationalHealth(w http.ResponseWriter, r *http.Request, params gen.GetOperationalHealthParams) {
	if params.From == nil || params.To == nil || params.From.After(*params.To) {
		p := httpx.NewProblem("invalid-date-range", "Invalid date range", http.StatusBadRequest)
		p.Detail = "Both 'from' and 'to' parameters are required and 'from' must be <= 'to'."
		httpx.WriteProblem(w, r, p)
		return
	}

	from := *params.From
	to := *params.To

	health, err := s.checkout.GetOperationalHealth(r.Context(), from, to)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	scansBySource := make([]gen.ScanSourceCount, 0, len(health.ScansBySource))
	for _, s := range health.ScansBySource {
		scansBySource = append(scansBySource, gen.ScanSourceCount{
			Source: gen.ScanSource(s.Source),
			Count:  s.Count,
		})
	}

	rejectionReasons := make([]gen.ScanRejectionReasonCount, 0, len(health.RejectionReasons))
	for _, rr := range health.RejectionReasons {
		var reasonPtr, typePtr *string
		if rr.Reason != "" {
			rCopy := rr.Reason
			reasonPtr = &rCopy
		}
		if rr.ResolvedType != "" {
			tCopy := rr.ResolvedType
			typePtr = &tCopy
		}
		rejectionReasons = append(rejectionReasons, gen.ScanRejectionReasonCount{
			Reason:       reasonPtr,
			ResolvedType: typePtr,
			Count:        rr.Count,
		})
	}

	writeJSON(w, http.StatusOK, gen.OperationalHealth{
		From:                from,
		To:                  to,
		TotalScans:          health.TotalScans,
		ManualEntryCount:    health.ManualEntryCount,
		CameraFallbackCount: health.CameraFallbackCount,
		ScansBySource:       scansBySource,
		RejectionReasons:    rejectionReasons,
	})
}

// ExportLoansCsv streams CSV data for loans matching filters.
func (s *Server) ExportLoansCsv(w http.ResponseWriter, r *http.Request, params gen.ExportLoansCsvParams) {
	var status lendingapi.Status
	if params.Status != nil {
		status = lendingapi.Status(*params.Status)
	}
	var origin lendingapi.Origin
	if params.Origin != nil {
		origin = lendingapi.Origin(*params.Origin)
	}

	listParams := lendingapi.ListLoansParams{
		Status:   status,
		Origin:   origin,
		UserID:   fromPtr(params.UserId),
		DeviceID: fromPtr(params.DeviceId),
		From:     params.From,
		To:       params.To,
		Disputed: params.Disputed,
	}

	rows, err := s.lending.StreamLoansForExport(r.Context(), listParams)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	filename := fmt.Sprintf("loans-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Write UTF-8 BOM
	_, _ = w.Write([]byte("\xef\xbb\xbf"))

	csvWriter := csv.NewWriter(w)
	_ = csvWriter.Write([]string{
		"id", "device_id", "asset_tag", "device_name",
		"user_id", "employee_no", "user_name", "status", "origin",
		"borrowed_at", "due_at", "returned_at",
		"borrow_kiosk", "return_kiosk", "borrow_actor", "return_actor",
		"borrow_source", "return_source", "condition_out", "condition_in",
		"notes", "paper_ref", "recorded_at", "recorded_by", "disputed",
	})
	csvWriter.Flush()

	flusher, _ := w.(http.Flusher)

	for i, row := range rows {
		dueStr := ""
		if row.DueAt != nil {
			dueStr = row.DueAt.UTC().Format(time.RFC3339)
		}
		retStr := ""
		if row.ReturnedAt != nil {
			retStr = row.ReturnedAt.UTC().Format(time.RFC3339)
		}
		recStr := ""
		if row.RecordedAt != nil {
			recStr = row.RecordedAt.UTC().Format(time.RFC3339)
		}

		_ = csvWriter.Write([]string{
			row.ID,
			row.DeviceID,
			row.DeviceAssetTag,
			row.DeviceName,
			row.UserID,
			row.UserEmployeeNo,
			row.UserFullName,
			string(row.Status),
			string(row.Origin),
			row.BorrowedAt.UTC().Format(time.RFC3339),
			dueStr,
			retStr,
			row.BorrowKioskName,
			row.ReturnKioskName,
			row.BorrowActor,
			row.ReturnActor,
			row.BorrowSource,
			row.ReturnSource,
			row.ConditionOut,
			row.ConditionIn,
			row.Notes,
			row.PaperRef,
			recStr,
			row.RecordedBy,
			strconv.FormatBool(row.Disputed),
		})

		if i%100 == 0 && flusher != nil {
			csvWriter.Flush()
			flusher.Flush()
		}
	}
	csvWriter.Flush()
	if flusher != nil {
		flusher.Flush()
	}
}

// ExportDevicesCsv streams CSV data for devices matching filters.
func (s *Server) ExportDevicesCsv(w http.ResponseWriter, r *http.Request, params gen.ExportDevicesCsvParams) {
	var status catalogapi.DeviceStatus
	if params.Status != nil {
		status = catalogapi.DeviceStatus(*params.Status)
	}

	listParams := catalogapi.ListDevicesParams{
		Status:     status,
		CategoryID: fromPtr(params.Category),
		Query:      fromPtr(params.Q),
	}

	rows, err := s.catalog.StreamDevicesForExport(r.Context(), listParams)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	filename := fmt.Sprintf("devices-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Write UTF-8 BOM
	_, _ = w.Write([]byte("\xef\xbb\xbf"))

	csvWriter := csv.NewWriter(w)
	_ = csvWriter.Write([]string{
		"id", "asset_tag", "name", "category", "manufacturer", "model", "serial_no",
		"status", "condition", "home_location", "notes", "acquired_on", "created_at", "updated_at",
	})
	csvWriter.Flush()

	flusher, _ := w.(http.Flusher)

	for i, row := range rows {
		acqStr := ""
		if row.AcquiredOn != nil {
			acqStr = row.AcquiredOn.Format("2006-01-02")
		}

		_ = csvWriter.Write([]string{
			row.ID,
			row.AssetTag,
			row.Name,
			row.CategoryName,
			row.Manufacturer,
			row.Model,
			row.SerialNo,
			string(row.Status),
			string(row.Condition),
			row.HomeLocation,
			row.Notes,
			acqStr,
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.UpdatedAt.UTC().Format(time.RFC3339),
		})

		if i%100 == 0 && flusher != nil {
			csvWriter.Flush()
			flusher.Flush()
		}
	}
	csvWriter.Flush()
	if flusher != nil {
		flusher.Flush()
	}
}

// ExportUsersCsv streams CSV data for users matching filters.
func (s *Server) ExportUsersCsv(w http.ResponseWriter, r *http.Request, params gen.ExportUsersCsvParams) {
	var status identityapi.UserStatus
	if params.Status != nil {
		status = identityapi.UserStatus(*params.Status)
	}

	listParams := identityapi.ListUsersParams{
		Status:        status,
		DepartmentID:  fromPtr(params.Department),
		Query:         fromPtr(params.Q),
		HasCredential: params.HasCredential,
	}

	rows, err := s.identity.StreamUsersForExport(r.Context(), listParams)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	filename := fmt.Sprintf("users-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Write UTF-8 BOM
	_, _ = w.Write([]byte("\xef\xbb\xbf"))

	csvWriter := csv.NewWriter(w)
	_ = csvWriter.Write([]string{
		"id", "employee_no", "full_name", "department", "email", "phone",
		"status", "notes", "has_credential", "registered_at", "registered_by", "updated_at",
	})
	csvWriter.Flush()

	flusher, _ := w.(http.Flusher)

	for i, row := range rows {
		_ = csvWriter.Write([]string{
			row.ID,
			row.EmployeeNo,
			row.FullName,
			row.DepartmentName,
			row.Email,
			row.Phone,
			string(row.Status),
			row.Notes,
			strconv.FormatBool(row.HasCredential),
			row.RegisteredAt.UTC().Format(time.RFC3339),
			row.RegisteredBy,
			row.UpdatedAt.UTC().Format(time.RFC3339),
		})

		if i%100 == 0 && flusher != nil {
			csvWriter.Flush()
			flusher.Flush()
		}
	}
	csvWriter.Flush()
	if flusher != nil {
		flusher.Flush()
	}
}
