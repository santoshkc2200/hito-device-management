//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
	"github.com/hito-hospital/hdms/test/testutil"
)

// TestConcurrentBorrow_OnlyOneSucceeds proves that when two kiosks race to borrow
// the same device simultaneously, exactly one succeeds and exactly one open loan
// row exists in the database (INV-1, docs/phases/phase-2/2.8-testing.md § 2.8.2).
func TestConcurrentBorrow_OnlyOneSucceeds(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kiosk1, _ := fixtures.Kiosk(t, pool)
	kiosk2, _ := fixtures.Kiosk(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)

	_, u1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user1)
	_, u2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user2)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	sess1 := createSession(t, ctx, svc, kiosk1)
	sess2 := createSession(t, ctx, svc, kiosk2)

	// Step 1: Identify users on both kiosks first.
	scan(t, ctx, svc, sess1.ID, u1Token)
	scan(t, ctx, svc, sess2.ID, u2Token)

	// Step 2: Concurrently scan the same device at both kiosks.
	var res1, res2 checkoutapi.ScanResult
	var err1, err2 error

	errs := testutil.Race(t,
		func() error {
			res1, err1 = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sess1.ID, Token: devToken, Source: "scanner", Actor: "kiosk:" + kiosk1,
			})
			return err1
		},
		func() error {
			res2, err2 = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sess2.ID, Token: devToken, Source: "scanner", Actor: "kiosk:" + kiosk2,
			})
			return err2
		},
	)

	for _, err := range errs {
		if err != nil {
			t.Fatalf("unexpected execution error from Scan: %v", err)
		}
	}

	outcomes := []checkoutapi.OutcomeKind{res1.Outcome.Kind, res2.Outcome.Kind}
	borrowCount := 0
	rejectCount := 0
	for _, kind := range outcomes {
		if kind == checkoutapi.OutcomeBorrowed {
			borrowCount++
		} else if kind == checkoutapi.OutcomeRejected {
			rejectCount++
		}
	}

	if borrowCount != 1 || rejectCount != 1 {
		t.Fatalf("concurrent borrow outcomes: got res1=%s res2=%s, want exactly 1 borrowed and 1 rejected",
			res1.Outcome.Kind, res2.Outcome.Kind)
	}

	// Verify database invariant: exactly 1 open loan row exists.
	var openLoanCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, deviceID).Scan(&openLoanCount)
	if err != nil {
		t.Fatalf("count open loans: %v", err)
	}
	if openLoanCount != 1 {
		t.Fatalf("open loans in DB = %d, want 1", openLoanCount)
	}

	// Verify device status is 'on_loan'.
	var devStatus string
	err = pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&devStatus)
	if err != nil {
		t.Fatalf("get device status: %v", err)
	}
	if devStatus != string(catalogapi.StatusOnLoan) {
		t.Fatalf("device status = %q, want 'on_loan'", devStatus)
	}
}

// TestConcurrentBorrowReturnsHolderInMessage proves that when a borrow race is lost,
// the loser's message names the winning holder in one round trip (savepoint-and-reread).
func TestConcurrentBorrowReturnsHolderInMessage(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kiosk1, _ := fixtures.Kiosk(t, pool)
	kiosk2, _ := fixtures.Kiosk(t, pool)
	u1ID, u1Name := fixtures.UserInDepartment(t, pool, "Cardiology")
	u2ID, u2Name := fixtures.UserInDepartment(t, pool, "Radiology")
	deviceID := fixtures.AvailableDevice(t, pool)

	_, u1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, u1ID)
	_, u2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, u2ID)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	sess1 := createSession(t, ctx, svc, kiosk1)
	sess2 := createSession(t, ctx, svc, kiosk2)

	scan(t, ctx, svc, sess1.ID, u1Token)
	scan(t, ctx, svc, sess2.ID, u2Token)

	var res1, res2 checkoutapi.ScanResult
	testutil.Race(t,
		func() error {
			var err error
			res1, err = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sess1.ID, Token: devToken, Source: "scanner", Actor: "kiosk:" + kiosk1,
			})
			return err
		},
		func() error {
			var err error
			res2, err = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sess2.ID, Token: devToken, Source: "scanner", Actor: "kiosk:" + kiosk2,
			})
			return err
		},
	)

	var loserResult checkoutapi.ScanResult
	var winningUserName string
	if res1.Outcome.Kind == checkoutapi.OutcomeRejected {
		loserResult = res1
		winningUserName = u2Name
	} else if res2.Outcome.Kind == checkoutapi.OutcomeRejected {
		loserResult = res2
		winningUserName = u1Name
	} else {
		t.Fatalf("expected one outcome to be rejected, got res1=%s res2=%s", res1.Outcome.Kind, res2.Outcome.Kind)
	}

	// The message must mention the winner
	combinedMsg := loserResult.Message.Title + " " + loserResult.Message.Detail
	if !strings.Contains(combinedMsg, winningUserName) {
		t.Errorf("loser message %q does not contain winner name %q", combinedMsg, winningUserName)
	}
}

// TestBorrowRacingReturnOnOneDevice proves that simultaneous borrow and return
// on one device leaves the database in a consistent state with no partial corruption.
func TestBorrowRacingReturnOnOneDevice(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kiosk1, _ := fixtures.Kiosk(t, pool)
	kiosk2, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	holderID := fixtures.User(t, pool)
	borrowerID := fixtures.User(t, pool)

	_, holderToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, holderID)
	_, borrowerToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, borrowerID)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	borrowViaCheckout(t, ctx, svc, pool, kiosk1, devToken, holderToken)

	sessReturn := createSession(t, ctx, svc, kiosk1)
	sessBorrow := createSession(t, ctx, svc, kiosk2)

	scan(t, ctx, svc, sessReturn.ID, holderToken)
	scan(t, ctx, svc, sessBorrow.ID, borrowerToken)

	var returnRes, borrowRes checkoutapi.ScanResult
	testutil.Race(t,
		func() error {
			var err error
			returnRes, err = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sessReturn.ID, Token: devToken, Source: "scanner", Actor: "kiosk:" + kiosk1,
			})
			return err
		},
		func() error {
			var err error
			borrowRes, err = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sessBorrow.ID, Token: devToken, Source: "scanner", Actor: "kiosk:" + kiosk2,
			})
			return err
		},
	)

	// Case A: Return committed first -> Return succeeds (returned), Borrow succeeds (borrowed).
	// Case B: Borrow attempted first -> Borrow rejected (held by someone else), Return succeeds (returned).
	// Either way, returnRes must be 'returned' and DB state must be coherent.
	if returnRes.Outcome.Kind != checkoutapi.OutcomeReturned {
		t.Errorf("return outcome = %s, want returned", returnRes.Outcome.Kind)
	}

	var openLoanCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, deviceID).Scan(&openLoanCount)
	if err != nil {
		t.Fatalf("count open loans: %v", err)
	}

	if borrowRes.Outcome.Kind == checkoutapi.OutcomeBorrowed {
		if openLoanCount != 1 {
			t.Fatalf("borrow succeeded but open loans in DB = %d, want 1", openLoanCount)
		}
	} else {
		if openLoanCount != 0 {
			t.Fatalf("borrow was rejected and return succeeded, open loans in DB = %d, want 0", openLoanCount)
		}
	}
}

// TestConcurrentScansOnOneSession asserts that row locking serialises concurrent scans
// on the same session without lost updates.
func TestConcurrentScansOnOneSession(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	dev1 := fixtures.AvailableDevice(t, pool)
	dev2 := fixtures.AvailableDevice(t, pool)
	_, dev1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev1)
	_, dev2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev2)

	sess := createSession(t, ctx, svc, kioskID)

	var res1, res2 checkoutapi.ScanResult
	errs := testutil.Race(t,
		func() error {
			var err error
			res1, err = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sess.ID, Token: dev1Token, Source: "scanner", Actor: "kiosk:test",
			})
			return err
		},
		func() error {
			var err error
			res2, err = svc.Scan(ctx, checkoutapi.ScanParams{
				SessionID: sess.ID, Token: dev2Token, Source: "scanner", Actor: "kiosk:test",
			})
			return err
		},
	)

	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent scan on session error: %v", err)
		}
	}

	// Both scans should succeed in device_pending
	if res1.Outcome.Kind != checkoutapi.OutcomeDevicePending || res2.Outcome.Kind != checkoutapi.OutcomeDevicePending {
		t.Fatalf("outcomes: res1=%s res2=%s, want both device_pending", res1.Outcome.Kind, res2.Outcome.Kind)
	}

	// Verify that scan_events recorded both scans
	var eventCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_events WHERE session_id = $1`, sess.ID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("count scan events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("scan_events count = %d, want 2", eventCount)
	}
}

// TestConcurrentIdempotentReplay asserts that concurrent requests with the same Idempotency-Key
// produce exactly one execution and one stored response replayed to the caller.
func TestConcurrentIdempotentReplay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	handler := newLoanOpeningHandler(pool, deviceID, userID)
	key := "test-race-idempotency-key"

	var resp1, resp2 *http.Response
	var body1, body2 string

	doReq := func(targetResp **http.Response, targetBody *string) func() error {
		return func() error {
			req := httptest.NewRequest(http.MethodPost, "/v1/test/open", strings.NewReader(`{}`))
			req.Header.Set("Idempotency-Key", key)
			req.Header.Set("X-Test-Actor", "kiosk:test")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			res := rec.Result()
			*targetResp = res
			b, _ := io.ReadAll(res.Body)
			*targetBody = string(b)
			return nil
		}
	}

	testutil.Race(t, doReq(&resp1, &body1), doReq(&resp2, &body2))

	if resp1.StatusCode != http.StatusCreated && resp1.StatusCode != http.StatusOK {
		t.Fatalf("resp1 status = %d, body = %s", resp1.StatusCode, body1)
	}
	if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
		t.Fatalf("resp2 status = %d, body = %s", resp2.StatusCode, body2)
	}

	var p1, p2 struct {
		LoanID string `json:"loanId"`
	}
	if err := json.Unmarshal([]byte(body1), &p1); err != nil {
		t.Fatalf("unmarshal body1 %q: %v", body1, err)
	}
	if err := json.Unmarshal([]byte(body2), &p2); err != nil {
		t.Fatalf("unmarshal body2 %q: %v", body2, err)
	}

	if p1.LoanID == "" || p1.LoanID != p2.LoanID {
		t.Fatalf("idempotent replay loan IDs differ: p1=%s, p2=%s", p1.LoanID, p2.LoanID)
	}

	// Exactly 1 open loan row in DB
	var loanCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1`, deviceID).Scan(&loanCount)
	if err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if loanCount != 1 {
		t.Fatalf("loans in DB = %d, want 1", loanCount)
	}
}

// TestConcurrentBackfillAndLiveBorrow proves that when a paper backfill and a live borrow
// race for the same device at the same time, the database exclusion constraint guarantees
// that exactly one wins (INV-13).
func TestConcurrentBackfillAndLiveBorrow(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()

	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userLive := fixtures.User(t, pool)
	userBackfill := fixtures.User(t, pool)

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	var liveErr, backfillErr error
	var liveLoan lendingapi.Loan
	var backfillRes checkoutapi.PaperBatchResult

	lendingSvc := lending.New(pool, audit.New(pool), clock.NewFake(now))

	testutil.Race(t,
		func() error {
			liveLoan, liveErr = lendingSvc.OpenLoan(ctx, deviceID, userLive, nil, lendingapi.OpenMeta{
				Actor: "kiosk:test", Source: "scanner",
			})
			return nil
		},
		func() error {
			// Paper backfill an overlapping open loan
			row := rowAt("row-1", assetTag, userBackfill, now.Add(-10*time.Minute), nil)
			backfillRes, backfillErr = svc.RecordPaperBatch(ctx, checkoutapi.PaperBatch{
				PaperRef: "page-1",
				Rows:     []checkoutapi.PaperRow{row},
			}, "admin:test")
			return nil
		},
	)

	// Exactly one must succeed, the other must fail due to overlapping custody.
	liveOk := liveErr == nil && liveLoan.ID != ""
	backfillOk := backfillErr == nil && backfillRes.Committed

	if liveOk && backfillOk {
		t.Fatalf("both live borrow and paper backfill succeeded for overlapping custody!")
	}
	if !liveOk && !backfillOk {
		t.Fatalf("neither live borrow nor paper backfill succeeded: liveErr=%v, backfillErr=%v", liveErr, backfillErr)
	}

	// Verify in DB that only 1 open loan row exists
	var openCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, deviceID).Scan(&openCount)
	if err != nil {
		t.Fatalf("count open loans: %v", err)
	}
	if openCount != 1 {
		t.Fatalf("open loans in DB = %d, want 1", openCount)
	}
}
