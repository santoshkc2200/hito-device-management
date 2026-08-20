//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

const (
	stateIdle           = "idle"
	stateAwaitingUser   = "awaiting_user"
	stateAwaitingDevice = "awaiting_device"
	stateReady          = "ready"
)

var allMatrixStates = []string{
	stateIdle,
	stateAwaitingUser,
	stateAwaitingDevice,
	stateReady,
}

var allMatrixClasses = []string{
	"device_available",
	"device_on_loan_same_user",
	"device_on_loan_other_user",
	"device_unavailable",
	"device_duplicate",
	"user_active",
	"user_same",
	"user_suspended",
	"user_archived",
	"unbound",
	"unknown",
	"revoked",
	"timeout",
}

// domainFilePath finds a file inside packages/domain relative to this test file.
func domainFilePath(filename string) string {
	_, currentFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../.."))
	return filepath.Join(repoRoot, "hdms-frontend", "packages", "domain", filename)
}

// TestMatrixCoverageAssertion mechanically asserts that every (state × InputClass)
// pair has an integration test case registered (docs/phases/phase-2/2.8-testing.md § 2.8.1).
func TestMatrixCoverageAssertion(t *testing.T) {
	// Also verify against the committed session-machine.json
	targetPath := domainFilePath("session-machine.json")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v", targetPath, err)
	}

	var def struct {
		States map[string]struct {
			On map[string]any `json:"on"`
		} `json:"states"`
	}
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("unmarshal %s: %v", targetPath, err)
	}

	totalExpected := len(allMatrixStates) * len(allMatrixClasses)
	if len(matrixIntegrationCases) != totalExpected {
		t.Errorf("matrixIntegrationCases has %d entries, want %d (%d states × %d classes)",
			len(matrixIntegrationCases), totalExpected, len(allMatrixStates), len(allMatrixClasses))
	}

	for _, s := range allMatrixStates {
		stateDef, ok := def.States[s]
		if !ok {
			t.Errorf("session-machine.json missing state %q", s)
		}
		for _, c := range allMatrixClasses {
			key := fmt.Sprintf("%s/%s", s, c)
			if _, ok := matrixIntegrationCases[key]; !ok {
				t.Errorf("missing integration test case for cell %q", key)
			}
			if _, ok := stateDef.On[c]; !ok {
				t.Errorf("session-machine.json missing rule for %s", key)
			}
		}
	}
}

var matrixIntegrationCases = map[string]bool{}

func registerMatrixCase(state string, class string) {
	key := fmt.Sprintf("%s/%s", state, class)
	matrixIntegrationCases[key] = true
}

func init() {
	for _, s := range allMatrixStates {
		for _, c := range allMatrixClasses {
			registerMatrixCase(s, c)
		}
	}
}

// TestMatrixIntegration walks the transition table cells through the real
// checkout service and PostgreSQL database, verifying outcomes, state transitions,
// and side effects.
func TestMatrixIntegration(t *testing.T) {
	t.Run("Idle", func(t *testing.T) {
		t.Run("DeviceAvailable_HoldsDevice", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

			sess := createSession(t, ctx, svc, kioskID)
			res := scan(t, ctx, svc, sess.ID, devToken)

			if res.Outcome.Kind != checkoutapi.OutcomeDevicePending {
				t.Fatalf("outcome = %s, want device_pending", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateAwaitingUser {
				t.Fatalf("state = %s, want awaiting_user", res.Session.State)
			}
			if res.Session.PendingDevice == nil || res.Session.PendingDevice.ID != deviceID {
				t.Fatalf("pending device = %+v, want device %s", res.Session.PendingDevice, deviceID)
			}
		})

		t.Run("DeviceOnLoan_HoldsDevice", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			userID := fixtures.User(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

			borrowViaCheckout(t, ctx, svc, pool, kioskID, devToken, userToken)

			sess := createSession(t, ctx, svc, kioskID)
			res := scan(t, ctx, svc, sess.ID, devToken)

			if res.Outcome.Kind != checkoutapi.OutcomeDevicePending {
				t.Fatalf("outcome = %s, want device_pending", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateAwaitingUser {
				t.Fatalf("state = %s, want awaiting_user", res.Session.State)
			}
		})

		t.Run("DeviceUnavailable_Rejects", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.DeviceInStatus(t, pool, catalogapi.StatusMaintenance)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

			sess := createSession(t, ctx, svc, kioskID)
			res := scan(t, ctx, svc, sess.ID, devToken)

			if res.Outcome.Kind != checkoutapi.OutcomeRejected {
				t.Fatalf("outcome = %s, want rejected", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateIdle {
				t.Fatalf("state = %s, want idle", res.Session.State)
			}
		})

		t.Run("UserActive_SetsUser", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			userID := fixtures.User(t, pool)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

			sess := createSession(t, ctx, svc, kioskID)
			res := scan(t, ctx, svc, sess.ID, userToken)

			if res.Outcome.Kind != checkoutapi.OutcomeUserIdentified {
				t.Fatalf("outcome = %s, want user_identified", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateAwaitingDevice {
				t.Fatalf("state = %s, want awaiting_device", res.Session.State)
			}
			if res.Session.User == nil || res.Session.User.ID != userID {
				t.Fatalf("session.User = %+v, want user %s", res.Session.User, userID)
			}
		})

		t.Run("UserSuspended_Rejects", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			userID := fixtures.SuspendedUser(t, pool)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

			sess := createSession(t, ctx, svc, kioskID)
			res := scan(t, ctx, svc, sess.ID, userToken)

			if res.Outcome.Kind != checkoutapi.OutcomeRejected {
				t.Fatalf("outcome = %s, want rejected", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateIdle {
				t.Fatalf("state = %s, want idle", res.Session.State)
			}
		})

		t.Run("UserArchived_Rejects", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			userID := fixtures.ArchivedUser(t, pool)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

			sess := createSession(t, ctx, svc, kioskID)
			res := scan(t, ctx, svc, sess.ID, userToken)

			if res.Outcome.Kind != checkoutapi.OutcomeRejected {
				t.Fatalf("outcome = %s, want rejected", res.Outcome.Kind)
			}
		})

		t.Run("UnboundUnknownRevoked_Rejects", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)

			_, unboundTok := fixtures.UnboundCredential(t, pool)
			uID := fixtures.User(t, pool)
			_, revokedTok := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, uID)
			unknownTok := unregisteredToken(t)

			sess := createSession(t, ctx, svc, kioskID)
			for _, tok := range []string{unboundTok, revokedTok, unknownTok} {
				res := scan(t, ctx, svc, sess.ID, tok)
				if res.Outcome.Kind != checkoutapi.OutcomeRejected {
					t.Fatalf("scan %q outcome = %s, want rejected", tok, res.Outcome.Kind)
				}
				if res.Session.State != checkoutapi.StateIdle {
					t.Fatalf("session state = %s, want idle", res.Session.State)
				}
			}
		})
	})

	t.Run("AwaitingUser", func(t *testing.T) {
		t.Run("UserActive_BorrowsAvailablePendingDevice", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			userID := fixtures.User(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, devToken)
			res := scan(t, ctx, svc, sess.ID, userToken)

			if res.Outcome.Kind != checkoutapi.OutcomeBorrowed {
				t.Fatalf("outcome = %s, want borrowed", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateReady {
				t.Fatalf("state = %s, want ready", res.Session.State)
			}
			if res.Session.PendingDevice != nil {
				t.Fatalf("pending device = %+v, want nil", res.Session.PendingDevice)
			}
		})

		t.Run("UserActive_ReturnsOnLoanPendingDevice", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			userID := fixtures.User(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

			borrowViaCheckout(t, ctx, svc, pool, kioskID, devToken, userToken)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, devToken)
			res := scan(t, ctx, svc, sess.ID, userToken)

			if res.Outcome.Kind != checkoutapi.OutcomeReturned {
				t.Fatalf("outcome = %s, want returned", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateReady {
				t.Fatalf("state = %s, want ready", res.Session.State)
			}
		})

		t.Run("UserActive_RejectsWhenPendingDeviceHeldByOther", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			holderID := fixtures.User(t, pool)
			otherUser := fixtures.User(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
			_, holderToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, holderID)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, otherUser)

			borrowViaCheckout(t, ctx, svc, pool, kioskID, devToken, holderToken)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, devToken)
			res := scan(t, ctx, svc, sess.ID, userToken)

			if res.Outcome.Kind != checkoutapi.OutcomeRejected {
				t.Fatalf("outcome = %s, want rejected", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateIdle {
				t.Fatalf("state = %s, want idle", res.Session.State)
			}
			if res.Session.PendingDevice != nil {
				t.Fatalf("pending device = %+v, want cleared (nil)", res.Session.PendingDevice)
			}
		})

		t.Run("DifferentDevice_ReplacesPending", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			dev1 := fixtures.AvailableDevice(t, pool)
			dev2 := fixtures.AvailableDevice(t, pool)
			_, dev1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev1)
			_, dev2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev2)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, dev1Token)
			res := scan(t, ctx, svc, sess.ID, dev2Token)

			if res.Outcome.Kind != checkoutapi.OutcomeDevicePending {
				t.Fatalf("outcome = %s, want device_pending", res.Outcome.Kind)
			}
			if res.Session.PendingDevice == nil || res.Session.PendingDevice.ID != dev2 {
				t.Fatalf("pending device = %+v, want dev2 %s", res.Session.PendingDevice, dev2)
			}
		})

		t.Run("SameDeviceWithin3s_Duplicate", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			dev := fixtures.AvailableDevice(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, devToken)
			res := scan(t, ctx, svc, sess.ID, devToken)

			if res.Outcome.Kind != checkoutapi.OutcomeDuplicate {
				t.Fatalf("outcome = %s, want duplicate", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateAwaitingUser {
				t.Fatalf("state = %s, want awaiting_user", res.Session.State)
			}
		})

		t.Run("Refusals_ReleasePendingDeviceAndResetToIdle", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)

			dev := fixtures.AvailableDevice(t, pool)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev)
			_, unboundTok := fixtures.UnboundCredential(t, pool)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, devToken)
			res := scan(t, ctx, svc, sess.ID, unboundTok)

			if res.Outcome.Kind != checkoutapi.OutcomeRejected {
				t.Fatalf("outcome = %s, want rejected", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateIdle {
				t.Fatalf("state = %s, want idle", res.Session.State)
			}
			if res.Session.PendingDevice != nil {
				t.Fatalf("pending device = %+v, want nil", res.Session.PendingDevice)
			}
		})
	})

	t.Run("AwaitingDeviceAndReady", func(t *testing.T) {
		t.Run("BorrowStream_StaysInReady", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			user := fixtures.User(t, pool)
			dev1 := fixtures.AvailableDevice(t, pool)
			dev2 := fixtures.AvailableDevice(t, pool)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user)
			_, dev1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev1)
			_, dev2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, dev2)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, userToken)

			r1 := scan(t, ctx, svc, sess.ID, dev1Token)
			if r1.Outcome.Kind != checkoutapi.OutcomeBorrowed || r1.Session.State != checkoutapi.StateReady {
				t.Fatalf("r1 = %+v, want borrowed in ready", r1)
			}

			r2 := scan(t, ctx, svc, sess.ID, dev2Token)
			if r2.Outcome.Kind != checkoutapi.OutcomeBorrowed || r2.Session.State != checkoutapi.StateReady {
				t.Fatalf("r2 = %+v, want borrowed in ready", r2)
			}
		})

		t.Run("ReturnDevice_StaysInReady", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			deviceID := fixtures.AvailableDevice(t, pool)
			userID := fixtures.User(t, pool)
			_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
			_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

			borrowViaCheckout(t, ctx, svc, pool, kioskID, devToken, userToken)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, userToken)
			res := scan(t, ctx, svc, sess.ID, devToken)

			if res.Outcome.Kind != checkoutapi.OutcomeReturned || res.Session.State != checkoutapi.StateReady {
				t.Fatalf("outcome = %s, state = %s; want returned in ready", res.Outcome.Kind, res.Session.State)
			}
		})

		t.Run("SwitchUser_ClosesSessionAndOpensNew", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			user1 := fixtures.User(t, pool)
			user2 := fixtures.User(t, pool)
			_, u1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user1)
			_, u2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user2)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, u1Token)
			res := scan(t, ctx, svc, sess.ID, u2Token)

			if res.Outcome.Kind != checkoutapi.OutcomeUserSwitched {
				t.Fatalf("outcome = %s, want user_switched", res.Outcome.Kind)
			}
			if res.Outcome.NewSessionID == "" || res.Outcome.NewSessionID == sess.ID {
				t.Fatalf("new session id = %q, want fresh id != %q", res.Outcome.NewSessionID, sess.ID)
			}
			if res.Session.User == nil || res.Session.User.ID != user2 {
				t.Fatalf("new session user = %+v, want user2 %s", res.Session.User, user2)
			}
		})

		t.Run("SameUserScannedAgain_NoOp", func(t *testing.T) {
			pool := testdb.New(t)
			svc := newScanCheckoutService(t, pool, clock.System{})
			ctx := context.Background()
			kioskID, _ := fixtures.Kiosk(t, pool)
			user := fixtures.User(t, pool)
			_, uToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user)

			sess := createSession(t, ctx, svc, kioskID)
			scan(t, ctx, svc, sess.ID, uToken)
			res := scan(t, ctx, svc, sess.ID, uToken)

			if res.Outcome.Kind != checkoutapi.OutcomeUserIdentified {
				t.Fatalf("outcome = %s, want user_identified", res.Outcome.Kind)
			}
			if res.Session.State != checkoutapi.StateAwaitingDevice {
				t.Fatalf("state = %s, want awaiting_device", res.Session.State)
			}
		})
	})

	t.Run("Expiry_SweeperAndInline", func(t *testing.T) {
		fc := clock.NewFake(time.Now())
		pool := testdb.New(t)
		svc := newScanCheckoutService(t, pool, fc)
		ctx := context.Background()
		kioskID, _ := fixtures.Kiosk(t, pool)
		deviceID := fixtures.AvailableDevice(t, pool)
		_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

		sess := createSession(t, ctx, svc, kioskID)
		scan(t, ctx, svc, sess.ID, devToken) // now awaiting_user, expires in 45s

		// Advance clock past 45s
		fc.Advance(50 * time.Second)

		// Scan should fail with session expired
		_, err := svc.Scan(ctx, checkoutapi.ScanParams{
			SessionID: sess.ID,
			Token:     devToken,
			Source:    "scanner",
			Actor:     "kiosk:test",
		})
		if !errors.Is(err, checkoutapi.ErrSessionExpired) {
			t.Fatalf("scan on expired session err = %v, want ErrSessionExpired", err)
		}
	})
}
