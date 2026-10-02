//go:build integration

// Package fixtures builds test data for integration tests through the real
// module services — never direct SQL — so a fixture can never construct
// state the application itself would reject. Every helper takes a fresh
// *testing.T and *db.Pool (from test/testdb) and fails the calling test on
// error via t.Fatalf rather than returning one, matching docs/10-testing-
// strategy.md's example usage.
package fixtures

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

const fixtureActor = "admin:fixtures"

// seq backs every unique-per-call value (asset tags, employee numbers) with
// a counter rather than rand, so two calls in the same test can never
// collide on a live-uniqueness index (devices_asset_tag_live_uk and its
// identity equivalent).
var seq int64

func nextSeq() int64 {
	return atomic.AddInt64(&seq, 1)
}

// Department creates (or reuses, by name) a department and returns its id.
func Department(t *testing.T, pool *db.Pool, name string) string {
	t.Helper()
	svc := identity.New(pool, audit.New(pool))
	dept, err := svc.GetOrCreateDepartment(context.Background(), name)
	if err != nil {
		t.Fatalf("fixtures: create department %q: %v", name, err)
	}
	return dept.ID
}

// User creates an active user and returns its id.
func User(t *testing.T, pool *db.Pool) string {
	t.Helper()
	return createUser(t, pool).ID
}

// UserInDepartment creates an active user attached to department (created
// by name if it does not exist) and returns the user id. Distinct from
// User because most tests do not care about departments, and the ones that
// do — the "held by someone else" message names the holder's department —
// need a real one rather than the empty default.
func UserInDepartment(t *testing.T, pool *db.Pool, department string) (id, fullName string) {
	t.Helper()
	svc := identity.New(pool, audit.New(pool))
	deptID := Department(t, pool, department)
	u := createUserWith(t, svc)
	updated, err := svc.UpdateUser(context.Background(), u.ID, identityapi.UpdateUserParams{
		FullName:     u.FullName,
		DepartmentID: deptID,
	}, fixtureActor)
	if err != nil {
		t.Fatalf("fixtures: attach user %s to department %q: %v", u.ID, department, err)
	}
	return updated.ID, updated.FullName
}

// SuspendedUser creates a user and immediately suspends it, returning its
// id.
func SuspendedUser(t *testing.T, pool *db.Pool) string {
	t.Helper()
	svc := identity.New(pool, audit.New(pool))
	u := createUserWith(t, svc)
	if _, err := svc.SuspendUser(context.Background(), u.ID, "fixture: suspended for test", fixtureActor); err != nil {
		t.Fatalf("fixtures: suspend user %s: %v", u.ID, err)
	}
	return u.ID
}

// ArchivedUser creates a user and immediately archives it, returning its
// id.
func ArchivedUser(t *testing.T, pool *db.Pool) string {
	t.Helper()
	svc := identity.New(pool, audit.New(pool))
	u := createUserWith(t, svc)
	if _, err := svc.ArchiveUser(context.Background(), u.ID, "fixture: archived for test", fixtureActor); err != nil {
		t.Fatalf("fixtures: archive user %s: %v", u.ID, err)
	}
	return u.ID
}

func createUser(t *testing.T, pool *db.Pool) identityapi.UserSummary {
	t.Helper()
	return createUserWith(t, identity.New(pool, audit.New(pool)))
}

func createUserWith(t *testing.T, svc *identity.Service) identityapi.UserSummary {
	t.Helper()
	n := nextSeq()
	u, err := svc.CreateUser(context.Background(), identityapi.CreateUserParams{
		EmployeeNo:   fmt.Sprintf("FX-U-%06d", n),
		FullName:     fmt.Sprintf("Fixture User %d", n),
		RegisteredBy: fixtureActor,
	})
	if err != nil {
		t.Fatalf("fixtures: create user: %v", err)
	}
	return u
}

// Category creates a device category with no default loan period and
// returns its id.
func Category(t *testing.T, pool *db.Pool) string {
	t.Helper()
	svc := catalog.New(pool, audit.New(pool))
	n := nextSeq()
	cat, err := svc.CreateCategory(context.Background(), catalogapi.CreateCategoryParams{
		Name: fmt.Sprintf("Fixture Category %d", n),
	})
	if err != nil {
		t.Fatalf("fixtures: create category: %v", err)
	}
	return cat.ID
}

// AvailableDevice creates a device (in its own fresh category) and returns
// its id. A freshly created device is already 'available', so no status
// transition is needed.
func AvailableDevice(t *testing.T, pool *db.Pool) string {
	t.Helper()
	return createDevice(t, pool).ID
}

// DeviceWithAssetTag creates a fresh available device and returns its id
// together with the asset tag — what the paper-backfill tests type into a
// row's deviceRef.
func DeviceWithAssetTag(t *testing.T, pool *db.Pool) (id, assetTag string) {
	t.Helper()
	dev := createDevice(t, pool)
	return dev.ID, dev.AssetTag
}

// UserWithEmployeeNo creates a fresh active user and returns the id with
// the employee number — what a backfill row's employeeNo userRef carries.
func UserWithEmployeeNo(t *testing.T, pool *db.Pool) (id, employeeNo string) {
	t.Helper()
	u := createUser(t, pool)
	return u.ID, u.EmployeeNo
}

// DeviceInStatus creates a device and drives it directly to status,
// returning its id. Every non-available status is reachable in a single
// transition from a freshly created (available) device.
func DeviceInStatus(t *testing.T, pool *db.Pool, status catalogapi.DeviceStatus) string {
	t.Helper()
	svc := catalog.New(pool, audit.New(pool))
	dev := createDeviceWith(t, svc)
	if status == catalogapi.StatusAvailable {
		return dev.ID
	}
	if _, err := svc.SetStatus(context.Background(), dev.ID, status, "fixture: set for test", fixtureActor); err != nil {
		t.Fatalf("fixtures: set device %s to status %s: %v", dev.ID, status, err)
	}
	return dev.ID
}

func createDevice(t *testing.T, pool *db.Pool) catalogapi.DeviceSummary {
	t.Helper()
	return createDeviceWith(t, catalog.New(pool, audit.New(pool)))
}

func createDeviceWith(t *testing.T, svc *catalog.Service) catalogapi.DeviceSummary {
	t.Helper()
	n := nextSeq()
	cat, err := svc.CreateCategory(context.Background(), catalogapi.CreateCategoryParams{
		Name: fmt.Sprintf("Fixture Category %d", n),
	})
	if err != nil {
		t.Fatalf("fixtures: create category: %v", err)
	}
	dev, err := svc.CreateDevice(context.Background(), catalogapi.CreateDeviceParams{
		AssetTag:   fmt.Sprintf("FX-D-%06d", n),
		Name:       fmt.Sprintf("Fixture Device %d", n),
		CategoryID: cat.ID,
	}, fixtureActor)
	if err != nil {
		t.Fatalf("fixtures: create device: %v", err)
	}
	return dev
}

func lendingServiceOn(pool *db.Pool) *lending.Service {
	return lending.New(pool, audit.New(pool), clock.System{})
}

// OpenLoan creates a fresh device and user and opens a loan between them,
// returning the loan id together with the device and user ids so the
// caller can act on any of the three.
func OpenLoan(t *testing.T, pool *db.Pool) (loanID, deviceID, userID string) {
	t.Helper()
	deviceID = createDevice(t, pool).ID
	userID = createUser(t, pool).ID
	loan, err := lendingServiceOn(pool).OpenLoan(context.Background(), deviceID, userID, nil, lendingapi.OpenMeta{
		Actor: fixtureActor, Source: "manual",
	})
	if err != nil {
		t.Fatalf("fixtures: open loan: %v", err)
	}
	return loan.ID, deviceID, userID
}

// ClosedLoan creates a fresh device and user, opens a loan and immediately
// closes it, returning the loan id together with the device and user ids.
func ClosedLoan(t *testing.T, pool *db.Pool) (loanID, deviceID, userID string) {
	t.Helper()
	loanID, deviceID, userID = OpenLoan(t, pool)
	loan, err := lendingServiceOn(pool).CloseLoan(context.Background(), loanID, lendingapi.CloseMeta{
		Actor: fixtureActor, Source: "manual",
	})
	if err != nil {
		t.Fatalf("fixtures: close loan: %v", err)
	}
	return loan.ID, deviceID, userID
}

// Kiosk registers a kiosk and returns its id and plaintext bearer token.
func Kiosk(t *testing.T, pool *db.Pool) (id, plainToken string) {
	t.Helper()
	n := nextSeq()
	id, plainToken, err := newAuthService(t, pool).RegisterKiosk(context.Background(), fmt.Sprintf("Fixture Kiosk %d", n), "")
	if err != nil {
		t.Fatalf("fixtures: register kiosk: %v", err)
	}
	return id, plainToken
}

func newAuthService(t *testing.T, pool *db.Pool) *auth.Service {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("fixtures: generate totp enc key: %v", err)
	}
	return auth.New(pool, "fixtures-test-pepper", key, 0)
}

// ActiveCredentialFor issues an active credential bound to an existing
// subject and returns its id and plaintext token.
func ActiveCredentialFor(t *testing.T, pool *db.Pool, subjectType credentialsapi.SubjectType, subjectID string) (credentialID, token string) {
	t.Helper()
	issued, err := newCredentialsService(t, pool).Issue(context.Background(), credentialsapi.IssueParams{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    fixtureActor,
	})
	if err != nil {
		t.Fatalf("fixtures: issue active credential: %v", err)
	}
	return issued.ID, issued.Token
}

// ManualCredentialFor issues an active manual credential carrying token
// verbatim — how a foreign barcode such as an employee ID is adopted —
// and returns its id.
func ManualCredentialFor(t *testing.T, pool *db.Pool, subjectType credentialsapi.SubjectType, subjectID, token string) (credentialID string) {
	t.Helper()
	issued, err := newCredentialsService(t, pool).Issue(context.Background(), credentialsapi.IssueParams{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Kind:        credentialsapi.KindManual,
		ManualToken: token,
		IssuedBy:    fixtureActor,
	})
	if err != nil {
		t.Fatalf("fixtures: issue manual credential: %v", err)
	}
	return issued.ID
}

// UnboundCredential issues a blank, unbound credential (card stock) and
// returns its id and plaintext token.
func UnboundCredential(t *testing.T, pool *db.Pool) (credentialID, token string) {
	t.Helper()
	batch, err := newCredentialsService(t, pool).IssueBlankBatch(context.Background(), 1, credentialsapi.KindQR, fixtureActor)
	if err != nil {
		t.Fatalf("fixtures: issue unbound credential: %v", err)
	}
	return batch[0].ID, batch[0].Token
}

// RevokedCredential issues a credential bound to an existing subject and
// immediately revokes it, returning its id and its (now-dead) plaintext
// token.
func RevokedCredential(t *testing.T, pool *db.Pool, subjectType credentialsapi.SubjectType, subjectID string) (credentialID, token string) {
	t.Helper()
	svc := newCredentialsService(t, pool)
	issued, err := svc.Issue(context.Background(), credentialsapi.IssueParams{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    fixtureActor,
	})
	if err != nil {
		t.Fatalf("fixtures: issue credential to revoke: %v", err)
	}
	if _, err := svc.Revoke(context.Background(), issued.ID, "fixture: revoked for test", fixtureActor); err != nil {
		t.Fatalf("fixtures: revoke credential %s: %v", issued.ID, err)
	}
	return issued.ID, issued.Token
}

func newCredentialsService(t *testing.T, pool *db.Pool) *credentials.Service {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("fixtures: generate credential enc key: %v", err)
	}
	return credentials.New(pool, audit.New(pool), "fixtures-test-pepper", key)
}
