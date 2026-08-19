//go:build integration

// Tests for 2.4 — unrecognised cards: the three distinct refusals
// (unbound, unknown, revoked), each naming the paper fallback, each
// releasing any pending device, each logged as a scan_events row.
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// lastScanEvent returns the newest scan_events row's resolved_type,
// result and reason for a session.
func lastScanEvent(t *testing.T, pool *db.Pool, ctx context.Context, sessionID string) (resolvedType, result, reason string) {
	t.Helper()
	err := pool.QueryRow(ctx,
		`SELECT resolved_type, result, coalesce(reason, '') FROM scan_events WHERE session_id = $1 ORDER BY at DESC LIMIT 1`,
		sessionID).Scan(&resolvedType, &result, &reason)
	if err != nil {
		t.Fatalf("query scan_events: %v", err)
	}
	return resolvedType, result, reason
}

func TestUnboundCardRejectedWithGuidance(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	_, unboundToken := fixtures.UnboundCredential(t, pool)

	session := createSession(t, ctx, svc, kioskID)
	r := scan(t, ctx, svc, session.ID, unboundToken)

	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}
	if !strings.Contains(r.Message.Detail, "attendant") || !strings.Contains(r.Message.Detail, "register") {
		t.Fatalf("unbound message = %q, want it to name the paper fallback (attendant + register)", r.Message.Detail)
	}
	resolvedType, result, _ := lastScanEvent(t, pool, ctx, session.ID)
	if resolvedType != "unbound" || result != "rejected" {
		t.Fatalf("scan_events = (resolved_type=%q, result=%q), want (unbound, rejected)", resolvedType, result)
	}
}

func TestUnknownTokenRejectedAndLogged(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)

	var usersBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersBefore); err != nil {
		t.Fatalf("count users before: %v", err)
	}

	session := createSession(t, ctx, svc, kioskID)
	r := scan(t, ctx, svc, session.ID, unregisteredToken(t))

	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}
	resolvedType, result, reason := lastScanEvent(t, pool, ctx, session.ID)
	if resolvedType != "unknown" || result != "rejected" {
		t.Fatalf("scan_events = (resolved_type=%q, result=%q), want (unknown, rejected)", resolvedType, result)
	}
	if reason == "" {
		t.Fatal("unknown-token rejection logged no reason")
	}

	// No kiosk enrollment, ever (FR-40, INV-11): an unrecognised token must
	// not silently materialise a user anywhere in the process.
	var usersAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersAfter); err != nil {
		t.Fatalf("count users after: %v", err)
	}
	if usersAfter != usersBefore {
		t.Fatalf("users count changed from %d to %d — an unknown token created a user somewhere", usersBefore, usersAfter)
	}
}

func TestRevokedCardRejectedWithDate(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	_, revokedToken := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, userID)

	session := createSession(t, ctx, svc, kioskID)
	r := scan(t, ctx, svc, session.ID, revokedToken)

	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}
	// The revocation date must reach the kiosk: "replaced on <date>" with a
	// real, formatted timestamp (the fixture revokes at ~now, so the
	// current year must appear).
	if !strings.Contains(r.Message.Detail, "replaced on ") {
		t.Fatalf("revoked message = %q, want it to include the replacement date", r.Message.Detail)
	}
	if !strings.Contains(r.Message.Detail, time.Now().Format("2006")) {
		t.Fatalf("revoked message = %q, want it to include the current year as the revocation date", r.Message.Detail)
	}
	resolvedType, result, _ := lastScanEvent(t, pool, ctx, session.ID)
	if resolvedType != "revoked" || result != "rejected" {
		t.Fatalf("scan_events = (resolved_type=%q, result=%q), want (revoked, rejected)", resolvedType, result)
	}
}

// TestRefusalReleasesPendingDevice is the 2.3b table's runtime
// assertion: from awaiting_user, every one of the three refusals sends
// the session back to idle with the pending device released — still
// available, no loan, untouched.
func TestRefusalReleasesPendingDevice(t *testing.T) {
	for _, tc := range []struct {
		name        string
		resolvedTyp string
		token       func(t *testing.T, pool *db.Pool) string
	}{
		{name: "unbound", resolvedTyp: "unbound", token: func(t *testing.T, pool *db.Pool) string {
			_, tok := fixtures.UnboundCredential(t, pool)
			return tok
		}},
		{name: "unknown", resolvedTyp: "unknown", token: func(t *testing.T, _ *db.Pool) string {
			return unregisteredToken(t)
		}},
		{name: "revoked", resolvedTyp: "revoked", token: func(t *testing.T, pool *db.Pool) string {
			userID := fixtures.User(t, pool)
			_, tok := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, userID)
			return tok
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

			session := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, session.ID, deviceToken) // -> awaiting_user
			r := scan(t, ctx, svc, session.ID, tc.token(t, pool))

			if r.Outcome.Kind != checkoutapi.OutcomeRejected {
				t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
			}
			if r.Session.State != checkoutapi.StateIdle || r.Session.PendingDevice != nil {
				t.Fatalf("session after %s refusal = %+v, want idle with pendingDevice released", tc.name, r.Session)
			}
			var status string
			var openLoans int
			if err := pool.QueryRow(ctx, `SELECT status::text FROM devices WHERE id = $1`, deviceID).Scan(&status); err != nil {
				t.Fatalf("query device status: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, deviceID).Scan(&openLoans); err != nil {
				t.Fatalf("query open loans: %v", err)
			}
			if status != "available" || openLoans != 0 {
				t.Fatalf("device after %s refusal: status=%q openLoans=%d, want available/0 (untouched)", tc.name, status, openLoans)
			}
			resolvedType, result, _ := lastScanEvent(t, pool, ctx, session.ID)
			if resolvedType != tc.resolvedTyp || result != "rejected" {
				t.Fatalf("scan_events = (resolved_type=%q, result=%q), want (%q, rejected)", resolvedType, result, tc.resolvedTyp)
			}
		})
	}
}

// TestCountScanRejectionsSince proves the turned-away count: grouped by
// resolved type, and counted by distinct token preview — the same person
// re-scanning three times is one person to go and register, not three.
func TestCountScanRejectionsSince(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	_, unboundToken := fixtures.UnboundCredential(t, pool)
	_, revokedToken := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, userID)
	unknownToken := unregisteredToken(t)

	// One person re-scanning their unknown token twice...
	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, unknownToken)
	scan(t, ctx, svc, session.ID, unknownToken)
	// ...one unbound card, one revoked card.
	session2 := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session2.ID, unboundToken)
	session3 := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session3.ID, revokedToken)

	counts, err := svc.CountScanRejectionsSince(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountScanRejectionsSince: %v", err)
	}
	byType := map[string]checkoutapi.ScanRejectionCount{}
	for _, c := range counts {
		byType[c.ResolvedType] = c
	}
	if got := byType["unknown"]; got.DistinctTokens != 1 || got.TotalScans != 2 {
		t.Fatalf("unknown rejections = %+v, want distinctTokens=1 totalScans=2 (one person, two scans)", got)
	}
	if got := byType["unbound"]; got.DistinctTokens != 1 || got.TotalScans != 1 {
		t.Fatalf("unbound rejections = %+v, want distinctTokens=1 totalScans=1", got)
	}
	if got := byType["revoked"]; got.DistinctTokens != 1 || got.TotalScans != 1 {
		t.Fatalf("revoked rejections = %+v, want distinctTokens=1 totalScans=1", got)
	}
}
