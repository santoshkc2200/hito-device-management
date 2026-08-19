package apiserver

import (
	"net/http"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// GetDashboard returns aggregated counts, category availability, overdue loans,
// turned-away scan statistics, and paper backfill nag.
func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()

	// 1. Overdue loans
	overdueLoans, err := s.lending.OverdueLoans(ctx, now)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	// 2. Query status counts and availability by category from database
	var availableCount, onLoanCount, maintenanceCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(count(*) FILTER (WHERE status = 'available'), 0),
			COALESCE(count(*) FILTER (WHERE status = 'on_loan'), 0),
			COALESCE(count(*) FILTER (WHERE status = 'maintenance'), 0)
		FROM devices
	`).Scan(&availableCount, &onLoanCount, &maintenanceCount); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	// Categories availability
	categories, err := s.catalog.ListCategories(ctx)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	catAvailMap := make(map[string]struct {
		available int
		total     int
	})
	rows, err := s.pool.Query(ctx, `
		SELECT category_id,
			COALESCE(count(*), 0) AS total,
			COALESCE(count(*) FILTER (WHERE status = 'available'), 0) AS available
		FROM devices
		GROUP BY category_id
	`)
	if err == nil {
		for rows.Next() {
			var catID string
			var total, avail int
			if err := rows.Scan(&catID, &total, &avail); err == nil {
				catAvailMap[catID] = struct {
					available int
					total     int
				}{available: avail, total: total}
			}
		}
		rows.Close()
	}

	categoryAvailabilities := make([]gen.CategoryAvailability, len(categories))
	for i, c := range categories {
		stats := catAvailMap[c.ID]
		categoryAvailabilities[i] = gen.CategoryAvailability{
			CategoryId:     c.ID,
			CategoryName:   c.Name,
			AvailableCount: stats.available,
			TotalCount:     stats.total,
		}
	}

	// 3. Turned-away counts since 24 hours ago
	rejectionCounts, err := s.checkout.CountScanRejectionsSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	turnedAway := make([]gen.ScanRejectionSummary, len(rejectionCounts))
	for i, rc := range rejectionCounts {
		turnedAway[i] = gen.ScanRejectionSummary{
			ResolvedType:   rc.ResolvedType,
			DistinctTokens: rc.DistinctTokens,
			TotalScans:     rc.TotalScans,
		}
	}

	// 4. Last paper entry
	lastPaper, err := s.lending.LastPaperEntry(ctx)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	var lastPaperEntry *gen.BackfillLastEntry
	if lastPaper != nil {
		lastPaperEntry = &gen.BackfillLastEntry{
			PaperRef:   strPtr(lastPaper.PaperRef),
			RecordedAt: lastPaper.RecordedAt,
			RecordedBy: strPtr(lastPaper.RecordedBy),
		}
	}

	dashboard := gen.Dashboard{
		OnLoanCount:            onLoanCount,
		OverdueCount:           len(overdueLoans),
		AvailableCount:         availableCount,
		MaintenanceCount:       maintenanceCount,
		AvailabilityByCategory: categoryAvailabilities,
		TurnedAwayCounts:       turnedAway,
		LastPaperEntry:         lastPaperEntry,
	}

	writeJSON(w, http.StatusOK, dashboard)
}
