package lending

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestTranslateLoanErrConstraintMapping covers the 23514 (CHECK violation)
// branch, which needs no database access to test: it only inspects the
// error's constraint name. The 23505/23P01 branches re-read the
// conflicting row and are covered instead by the integration tests that
// exercise INV-1 and INV-13 against a real Postgres.
func TestTranslateLoanErrConstraintMapping(t *testing.T) {
	s := &Service{}
	ctx := context.Background()

	cases := []struct {
		name       string
		constraint string
		want       error
	}{
		{"return after borrow", "loans_return_after_borrow", lendingapi.ErrInvalidReturnTime},
		{"paper needs provenance", "loans_paper_needs_provenance", lendingapi.ErrPaperProvenanceRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: "23514", ConstraintName: tc.constraint}
			got := s.translateLoanErr(ctx, pgErr, "", time.Now(), nil)
			if !errors.Is(got, tc.want) {
				t.Fatalf("translateLoanErr(%s) = %v, want %v", tc.constraint, got, tc.want)
			}
		})
	}
}

func TestTranslateLoanErrNoRowsMapsToNotFound(t *testing.T) {
	s := &Service{}
	got := s.translateLoanErr(context.Background(), pgx.ErrNoRows, "", time.Now(), nil)
	if !errors.Is(got, lendingapi.ErrLoanNotFound) {
		t.Fatalf("translateLoanErr(pgx.ErrNoRows) = %v, want ErrLoanNotFound", got)
	}
}

func TestTranslateLoanErrPassesThroughUnknownCode(t *testing.T) {
	s := &Service{}
	pgErr := &pgconn.PgError{Code: "42601"} // syntax_error — not one this module maps
	got := s.translateLoanErr(context.Background(), pgErr, "", time.Now(), nil)
	if !errors.Is(got, pgErr) {
		t.Fatalf("translateLoanErr(unmapped code) = %v, want the original error unwrapped", got)
	}
}
