package checkout

import (
	"context"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// CountScanRejectionsSince answers the administrator's "how many people
// did we turn away, and why" question (2.4.2): rejections grouped by
// resolved type, each counted both as total scans and as distinct token
// previews — the number worth acting on is distinct tokens, because the
// same person re-scanning three times is one person to go and register.
// Feeds GET /v1/dashboard in 2.6.
func (s *Service) CountScanRejectionsSince(ctx context.Context, since time.Time) ([]checkoutapi.ScanRejectionCount, error) {
	rows, err := checkoutstore.New(s.pool.Pool).CountScanRejectionsSince(ctx, pgtypeconv.Timestamptz(since))
	if err != nil {
		return nil, fmt.Errorf("checkout: count scan rejections: %w", err)
	}
	counts := make([]checkoutapi.ScanRejectionCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, checkoutapi.ScanRejectionCount{
			ResolvedType:   pgtypeconv.TextString(row.ResolvedType),
			DistinctTokens: int(row.DistinctTokens),
			TotalScans:     int(row.TotalScans),
		})
	}
	return counts, nil
}

// GetOperationalHealth returns scan metrics, breakdown by source, and rejection reasons for a date range.
func (s *Service) GetOperationalHealth(ctx context.Context, from, to time.Time) (checkoutapi.OperationalHealthStats, error) {
	q := checkoutstore.New(s.pool.Pool)
	fromTz := pgtypeconv.Timestamptz(from)
	toTz := pgtypeconv.Timestamptz(to)

	statsRow, err := q.GetOperationalHealthStats(ctx, checkoutstore.GetOperationalHealthStatsParams{
		FromAt: fromTz,
		ToAt:   toTz,
	})
	if err != nil {
		return checkoutapi.OperationalHealthStats{}, fmt.Errorf("checkout: get operational health stats: %w", err)
	}

	sourceRows, err := q.GetScansBySource(ctx, checkoutstore.GetScansBySourceParams{
		FromAt: fromTz,
		ToAt:   toTz,
	})
	if err != nil {
		return checkoutapi.OperationalHealthStats{}, fmt.Errorf("checkout: get scans by source: %w", err)
	}

	rejectionRows, err := q.GetScanRejectionReasons(ctx, checkoutstore.GetScanRejectionReasonsParams{
		FromAt: fromTz,
		ToAt:   toTz,
	})
	if err != nil {
		return checkoutapi.OperationalHealthStats{}, fmt.Errorf("checkout: get scan rejection reasons: %w", err)
	}

	scansBySource := make([]checkoutapi.ScanSourceCount, 0, len(sourceRows))
	for _, sr := range sourceRows {
		scansBySource = append(scansBySource, checkoutapi.ScanSourceCount{
			Source: sr.Source,
			Count:  int(sr.Count),
		})
	}

	rejectionReasons := make([]checkoutapi.ScanRejectionReasonCount, 0, len(rejectionRows))
	for _, rr := range rejectionRows {
		rejectionReasons = append(rejectionReasons, checkoutapi.ScanRejectionReasonCount{
			Reason:       rr.Reason,
			ResolvedType: rr.ResolvedType,
			Count:        int(rr.Count),
		})
	}

	return checkoutapi.OperationalHealthStats{
		TotalScans:          int(statsRow.TotalScans),
		ManualEntryCount:    int(statsRow.ManualEntryCount),
		CameraFallbackCount: int(statsRow.CameraFallbackCount),
		ScansBySource:       scansBySource,
		RejectionReasons:    rejectionReasons,
	}, nil
}

