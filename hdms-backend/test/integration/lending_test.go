//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
	"github.com/jackc/pgx/v5/pgconn"
)

func newLendingService(t *testing.T) (*lending.Service, *db.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return lending.New(pool, audit.New(pool), clock.System{}), pool
}

func timePtr(t time.Time) *time.Time { return &t }

func TestINV1_OneOpenLoanPerDevice(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)

	first, err := svc.OpenLoan(ctx, deviceID, user1, nil, lendingapi.OpenMeta{Actor: "kiosk:1", Source: "scanner"})
	if err != nil {
		t.Fatalf("first OpenLoan: %v", err)
	}

	_, err = svc.OpenLoan(ctx, deviceID, user2, nil, lendingapi.OpenMeta{Actor: "kiosk:1", Source: "scanner"})
	var conflict *lendingapi.DeviceAlreadyOnLoanError
	if !errors.As(err, &conflict) {
		t.Fatalf("second OpenLoan error = %v, want *DeviceAlreadyOnLoanError", err)
	}
	if !errors.Is(err, lendingapi.ErrDeviceAlreadyOnLoan) {
		t.Fatalf("second OpenLoan error does not match ErrDeviceAlreadyOnLoan via errors.Is: %v", err)
	}
	if conflict.Existing.ID != first.ID {
		t.Fatalf("conflict.Existing.ID = %q, want the first loan's id %q", conflict.Existing.ID, first.ID)
	}
	if conflict.Existing.UserID != user1 {
		t.Fatalf("conflict.Existing.UserID = %q, want the first holder %q", conflict.Existing.UserID, user1)
	}
}

func TestINV5_ReturnedAtNullIffOpen(t *testing.T) {
	_, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	// Direct SQL: a 'returned' loan with no returned_at must violate
	// loans_status_matches_return.
	_, err := pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, returned_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'returned', now(), NULL, 'admin:1', 'manual')`,
		deviceID, userID)
	if err == nil {
		t.Fatal("expected a CHECK violation inserting a 'returned' loan with a NULL returned_at")
	}

	// And the converse: an 'open' loan with returned_at set must also
	// violate it.
	_, err = pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, returned_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'open', now() - interval '1 hour', now(), 'admin:1', 'manual')`,
		deviceID, userID)
	if err == nil {
		t.Fatal("expected a CHECK violation inserting an 'open' loan with a non-NULL returned_at")
	}
}

func TestINV13_OverlappingCustodyRejected(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	first, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: user1, Origin: lendingapi.OriginPaper,
		BorrowedAt: start, ReturnedAt: &end,
		BorrowActor: "admin:1", ReturnActor: "admin:1", BorrowSource: "paper", ReturnSource: "paper",
		PaperRef: "SLIP-1", RecordedAt: timePtr(time.Now()), RecordedBy: "admin:1",
	})
	if err != nil {
		t.Fatalf("first RecordHistorical: %v", err)
	}

	overlapStart := start.Add(30 * time.Minute)
	overlapEnd := end.Add(30 * time.Minute)
	_, err = svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: user2, Origin: lendingapi.OriginPaper,
		BorrowedAt: overlapStart, ReturnedAt: &overlapEnd,
		BorrowActor: "admin:1", ReturnActor: "admin:1", BorrowSource: "paper", ReturnSource: "paper",
		PaperRef: "SLIP-2", RecordedAt: timePtr(time.Now()), RecordedBy: "admin:1",
	})
	var conflict *lendingapi.OverlappingCustodyError
	if !errors.As(err, &conflict) {
		t.Fatalf("second RecordHistorical error = %v, want *OverlappingCustodyError", err)
	}
	if !errors.Is(err, lendingapi.ErrOverlappingCustody) {
		t.Fatalf("second RecordHistorical error does not match ErrOverlappingCustody via errors.Is: %v", err)
	}
	if conflict.Existing.ID != first.ID {
		t.Fatalf("conflict.Existing.ID = %q, want the first loan's id %q", conflict.Existing.ID, first.ID)
	}
}

func TestINV13_AdjacentCustodyAllowed(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)

	borrow1 := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	return1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: user1, Origin: lendingapi.OriginPaper,
		BorrowedAt: borrow1, ReturnedAt: &return1,
		BorrowActor: "admin:1", ReturnActor: "admin:1", BorrowSource: "paper", ReturnSource: "paper",
		PaperRef: "S1", RecordedAt: &borrow1, RecordedBy: "admin:1",
	}); err != nil {
		t.Fatalf("first RecordHistorical: %v", err)
	}

	// Re-borrowed at exactly the instant the first loan was returned:
	// '[)' bounds mean this is not a conflict.
	return2 := return1.Add(2 * time.Hour)
	if _, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: user2, Origin: lendingapi.OriginPaper,
		BorrowedAt: return1, ReturnedAt: &return2,
		BorrowActor: "admin:1", ReturnActor: "admin:1", BorrowSource: "paper", ReturnSource: "paper",
		PaperRef: "S2", RecordedAt: &return1, RecordedBy: "admin:1",
	}); err != nil {
		t.Fatalf("adjacent RecordHistorical (borrowed exactly at the prior return instant) should be allowed, got: %v", err)
	}
}

func TestINV13_EmptyRangeRejected(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	_, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: userID, Origin: lendingapi.OriginPaper,
		BorrowedAt: at, ReturnedAt: &at,
		BorrowActor: "admin:1", ReturnActor: "admin:1", BorrowSource: "paper", ReturnSource: "paper",
		PaperRef: "S1", RecordedAt: &at, RecordedBy: "admin:1",
	})
	if !errors.Is(err, lendingapi.ErrInvalidReturnTime) {
		t.Fatalf("RecordHistorical(borrowedAt == returnedAt) error = %v, want ErrInvalidReturnTime", err)
	}
}

// TestExitCriteria_BackdatedOverlapRejectedByDatabase proves the
// loans_no_overlapping_custody exclusion constraint is enforced by Postgres
// itself, not merely by application logic a future change could bypass —
// issuing the INSERT directly in SQL, not through the service (2.1's own
// exit criterion).
func TestExitCriteria_BackdatedOverlapRejectedByDatabase(t *testing.T) {
	_, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)

	insert := `
		INSERT INTO loans (
			id, device_id, user_id, status, origin, borrowed_at, returned_at,
			borrow_actor, borrow_source, recorded_at, recorded_by, paper_ref
		) VALUES (
			gen_random_uuid(), $1, $2, 'returned', 'paper', $3, $4,
			'admin:1', 'paper', now(), 'admin:1', $5
		)`
	if _, err := pool.Exec(ctx, insert, deviceID, user1,
		time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), "S1"); err != nil {
		t.Fatalf("seed first historical loan via direct SQL: %v", err)
	}

	_, err := pool.Exec(ctx, insert, deviceID, user2,
		time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC), "S2")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23P01" {
		t.Fatalf("direct overlapping INSERT error = %v, want a 23P01 exclusion violation", err)
	}
}

func TestINV14_PaperProvenanceRequired(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	_, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: userID, Origin: lendingapi.OriginPaper,
		BorrowedAt:   time.Now().Add(-2 * time.Hour),
		BorrowActor:  "admin:1",
		BorrowSource: "paper",
		PaperRef:     "S1",
		// RecordedAt/RecordedBy intentionally omitted.
	})
	if !errors.Is(err, lendingapi.ErrPaperProvenanceRequired) {
		t.Fatalf("RecordHistorical(paper, no provenance) error = %v, want ErrPaperProvenanceRequired", err)
	}
}

func TestINV15_OriginNeverRewritten(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()

	openPaperLoan := func(t *testing.T) lendingapi.Loan {
		t.Helper()
		deviceID := fixtures.AvailableDevice(t, pool)
		userID := fixtures.User(t, pool)
		loan, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
			DeviceID: deviceID, UserID: userID, Origin: lendingapi.OriginPaper,
			BorrowedAt:   time.Now().Add(-time.Hour),
			BorrowActor:  "admin:1",
			BorrowSource: "paper",
			RecordedAt:   timePtr(time.Now()), RecordedBy: "admin:1", PaperRef: "S1",
		})
		if err != nil {
			t.Fatalf("RecordHistorical: %v", err)
		}
		if loan.Origin != lendingapi.OriginPaper {
			t.Fatalf("seeded loan origin = %q, want paper", loan.Origin)
		}
		return loan
	}

	t.Run("CloseLoan", func(t *testing.T) {
		loan := openPaperLoan(t)
		closed, err := svc.CloseLoan(ctx, loan.ID, lendingapi.CloseMeta{Actor: "admin:1", Source: "manual"})
		if err != nil {
			t.Fatalf("CloseLoan: %v", err)
		}
		if closed.Origin != lendingapi.OriginPaper {
			t.Fatalf("origin after CloseLoan = %q, want paper", closed.Origin)
		}
	})
	t.Run("ForceReturn", func(t *testing.T) {
		loan := openPaperLoan(t)
		forced, err := svc.ForceReturn(ctx, loan.ID, "test", "good", nil, "admin:1")
		if err != nil {
			t.Fatalf("ForceReturn: %v", err)
		}
		if forced.Origin != lendingapi.OriginPaper {
			t.Fatalf("origin after ForceReturn = %q, want paper", forced.Origin)
		}
	})
	t.Run("WriteOff", func(t *testing.T) {
		loan := openPaperLoan(t)
		written, err := svc.WriteOff(ctx, loan.ID, "test", "admin:1")
		if err != nil {
			t.Fatalf("WriteOff: %v", err)
		}
		if written.Origin != lendingapi.OriginPaper {
			t.Fatalf("origin after WriteOff = %q, want paper", written.Origin)
		}
	})
}

func TestWriteOffLeavesLastHolderOnRecord(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	loanID, deviceID, userID := fixtures.OpenLoan(t, pool)

	written, err := svc.WriteOff(ctx, loanID, "device reported lost", "admin:1")
	if err != nil {
		t.Fatalf("WriteOff: %v", err)
	}
	if written.Status != lendingapi.StatusWrittenOff {
		t.Fatalf("status = %q, want written_off", written.Status)
	}
	if written.DeviceID != deviceID || written.UserID != userID {
		t.Fatalf("WriteOff did not preserve the last holder: got device=%s user=%s, want device=%s user=%s",
			written.DeviceID, written.UserID, deviceID, userID)
	}
	if written.ReturnedAt == nil {
		t.Fatal("expected returned_at to be set on a written-off loan (loans_status_matches_return applies to any non-open status)")
	}
}

func TestForceReturnRequiresReason(t *testing.T) {
	svc, pool := newLendingService(t)
	loanID, _, _ := fixtures.OpenLoan(t, pool)

	if _, err := svc.ForceReturn(context.Background(), loanID, "   ", "good", nil, "admin:1"); err == nil {
		t.Fatal("expected ForceReturn with a blank reason to fail")
	}
}

func TestForceReturnHonoursExplicitReturnedAt(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	loanID, _, _ := fixtures.OpenLoan(t, pool)

	original, err := svc.GetLoan(ctx, loanID)
	if err != nil {
		t.Fatalf("GetLoan: %v", err)
	}
	explicit := original.BorrowedAt.Add(time.Hour)

	loan, err := svc.ForceReturn(ctx, loanID, "left early", "good", &explicit, "admin:1")
	if err != nil {
		t.Fatalf("ForceReturn: %v", err)
	}
	if loan.ReturnedAt == nil || !loan.ReturnedAt.Equal(explicit) {
		t.Fatalf("ReturnedAt = %v, want %v", loan.ReturnedAt, explicit)
	}
}

func TestOpenLoanRejectsBackdatedBorrowedAt(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	_, err := svc.OpenLoan(ctx, deviceID, userID, nil, lendingapi.OpenMeta{
		Actor: "kiosk:1", Source: "scanner",
		BorrowedAt: time.Now().Add(-time.Hour),
	})
	if !errors.Is(err, lendingapi.ErrBackdatedNotPermitted) {
		t.Fatalf("OpenLoan(backdated) error = %v, want ErrBackdatedNotPermitted", err)
	}
}

func TestListLoansCursorPaginationStable(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()

	for range 3 {
		fixtures.ClosedLoan(t, pool)
	}

	page1, err := svc.ListLoans(ctx, lendingapi.ListLoansParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListLoans page 1: %v", err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 items and a next cursor", page1)
	}

	// Insert a new (newest) loan "mid-pagination" — after fetching page 1,
	// before fetching page 2.
	newLoanID, _, _ := fixtures.ClosedLoan(t, pool)

	page2, err := svc.ListLoans(ctx, lendingapi.ListLoansParams{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("ListLoans page 2: %v", err)
	}
	for _, a := range page1.Items {
		for _, b := range page2.Items {
			if a.ID == b.ID {
				t.Fatalf("page1 and page2 both contain loan %s", a.ID)
			}
		}
	}
	for _, b := range page2.Items {
		if b.ID == newLoanID {
			t.Fatal("page2 must not include a loan inserted after the cursor was taken (it sorts newest-first, ahead of the cursor)")
		}
	}
}
