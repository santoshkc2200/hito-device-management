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
