package observability_test

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestTransactionsBorrowSuccessLabelSet pins the frozen label set for a
// successful kiosk borrow: action=borrow, source=scanner, outcome=success.
func TestTransactionsBorrowSuccessLabelSet(t *testing.T) {
	before := testutil.ToFloat64(observability.TransactionsTotal.WithLabelValues("borrow", "scanner", "success"))
	observability.ObserveTransaction("borrow", "scanner", "success")
	after := testutil.ToFloat64(observability.TransactionsTotal.WithLabelValues("borrow", "scanner", "success"))
	if after-before != 1 {
		t.Fatalf("borrow/scanner/success delta = %v, want 1", after-before)
	}
}

// TestTransactionsBorrowRejectedLabelSet pins the rejected borrow outcome.
func TestTransactionsBorrowRejectedLabelSet(t *testing.T) {
	before := testutil.ToFloat64(observability.TransactionsTotal.WithLabelValues("borrow", "scanner", "rejected"))
	observability.ObserveTransaction("borrow", "scanner", "rejected")
	after := testutil.ToFloat64(observability.TransactionsTotal.WithLabelValues("borrow", "scanner", "rejected"))
	if after-before != 1 {
		t.Fatalf("borrow/scanner/rejected delta = %v, want 1", after-before)
	}
}

// TestTransactionsReturnSuccessLabelSet pins the successful return outcome,
// including the tap-to-return path which reports source=manual.
func TestTransactionsReturnSuccessLabelSet(t *testing.T) {
	before := testutil.ToFloat64(observability.TransactionsTotal.WithLabelValues("return", "manual", "success"))
	observability.ObserveTransaction("return", "manual", "success")
	after := testutil.ToFloat64(observability.TransactionsTotal.WithLabelValues("return", "manual", "success"))
	if after-before != 1 {
		t.Fatalf("return/manual/success delta = %v, want 1", after-before)
	}
}

// TestScanRejectionsLabelSet pins the rejection counter label set.
func TestScanRejectionsLabelSet(t *testing.T) {
	before := testutil.ToFloat64(observability.ScanRejectionsTotal.WithLabelValues("unbound"))
	observability.IncScanRejection("unbound")
	after := testutil.ToFloat64(observability.ScanRejectionsTotal.WithLabelValues("unbound"))
	if after-before != 1 {
		t.Fatalf("scan rejections unbound delta = %v, want 1", after-before)
	}
}

// TestSessionExpiredIncrements pins the session-expiry counter.
func TestSessionExpiredIncrements(t *testing.T) {
	before := testutil.ToFloat64(observability.SessionExpiredTotal)
	observability.IncSessionExpired()
	after := testutil.ToFloat64(observability.SessionExpiredTotal)
	if after-before != 1 {
		t.Fatalf("session expired delta = %v, want 1", after-before)
	}
}

// TestNormalizeSourceBoundsCardinanality ensures unknown sources collapse to
// "other" rather than exploding the label space.
func TestNormalizeSourceBoundsCardinanality(t *testing.T) {
	if got := observability.NormalizeSource("scanner"); got != "scanner" {
		t.Fatalf("NormalizeSource(scanner) = %q, want scanner", got)
	}
	if got := observability.NormalizeSource("camera"); got != "camera" {
		t.Fatalf("NormalizeSource(camera) = %q, want camera", got)
	}
	if got := observability.NormalizeSource("manual"); got != "manual" {
		t.Fatalf("NormalizeSource(manual) = %q, want manual", got)
	}
	if got := observability.NormalizeSource("rfid-9000"); got != "other" {
		t.Fatalf("NormalizeSource(rfid-9000) = %q, want other", got)
	}
	if got := observability.NormalizeSource(""); got != "other" {
		t.Fatalf("NormalizeSource(\"\") = %q, want other", got)
	}
}

// TestNormalizeRejectionReasonBoundsCardinality ensures unknown reasons
// collapse to "other".
func TestNormalizeRejectionReasonBoundsCardinality(t *testing.T) {
	if got := observability.NormalizeRejectionReason("unbound"); got != "unbound" {
		t.Fatalf("NormalizeRejectionReason(unbound) = %q", got)
	}
	if got := observability.NormalizeRejectionReason("device_held_by_other"); got != "device_held_by_other" {
		t.Fatalf("NormalizeRejectionReason(device_held_by_other) = %q", got)
	}
	if got := observability.NormalizeRejectionReason("something-new"); got != "other" {
		t.Fatalf("NormalizeRejectionReason(something-new) = %q, want other", got)
	}
}

// TestReservationsMetricsIncrements tests the four reservation metric counters.
func TestReservationsMetricsIncrements(t *testing.T) {
	// 1. ReservationsMadeTotal
	beforeMade := testutil.ToFloat64(observability.ReservationsMadeTotal)
	observability.IncReservationMade()
	if after := testutil.ToFloat64(observability.ReservationsMadeTotal); after-beforeMade != 1 {
		t.Fatalf("ReservationsMade delta = %v, want 1", after-beforeMade)
	}

	// 2. ReservationsCollectedTotal
	beforeColl := testutil.ToFloat64(observability.ReservationsCollectedTotal)
	observability.IncReservationCollected()
	if after := testutil.ToFloat64(observability.ReservationsCollectedTotal); after-beforeColl != 1 {
		t.Fatalf("ReservationsCollected delta = %v, want 1", after-beforeColl)
	}

	// 3. ReservationsExpiredTotal (Inc + Add)
	beforeExp := testutil.ToFloat64(observability.ReservationsExpiredTotal)
	observability.IncReservationExpired()
	if after := testutil.ToFloat64(observability.ReservationsExpiredTotal); after-beforeExp != 1 {
		t.Fatalf("ReservationsExpired delta = %v, want 1", after-beforeExp)
	}
	observability.AddReservationsExpired(3)
	if after := testutil.ToFloat64(observability.ReservationsExpiredTotal); after-beforeExp != 4 {
		t.Fatalf("ReservationsExpired after Add(3) delta = %v, want 4", after-beforeExp)
	}

	// 4. ReservationConflictsRefusedTotal
	beforeConf := testutil.ToFloat64(observability.ReservationConflictsRefusedTotal)
	observability.IncReservationConflictRefused()
	if after := testutil.ToFloat64(observability.ReservationConflictsRefusedTotal); after-beforeConf != 1 {
		t.Fatalf("ReservationConflictsRefused delta = %v, want 1", after-beforeConf)
	}
}
