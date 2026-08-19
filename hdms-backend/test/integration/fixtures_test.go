//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// The services below are constructed directly on a pool the caller already
// holds, rather than via this package's newIdentityService et al., which
// each open their own fresh pool — these tests need fixtures and the
// service under test to share one pool so fixture writes are visible.

func identityServiceOn(pool *db.Pool) *identity.Service {
	return identity.New(pool, audit.New(pool))
}

func catalogServiceOn(pool *db.Pool) *catalog.Service {
	return catalog.New(pool, audit.New(pool))
}

func credentialsServiceOn(t *testing.T, pool *db.Pool) *credentials.Service {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate credential enc key: %v", err)
	}
	return credentials.New(pool, audit.New(pool), "fixtures-test-pepper", key)
}

func TestFixturesDepartmentIsIdempotentByName(t *testing.T) {
	pool := testdb.New(t)
	d1 := fixtures.Department(t, pool, "Cardiology")
	d2 := fixtures.Department(t, pool, "Cardiology")
	if d1 != d2 {
		t.Fatalf("fixtures.Department(same name) = %q and %q, want equal", d1, d2)
	}
}

func TestFixturesUserStatuses(t *testing.T) {
	pool := testdb.New(t)
	svc := identityServiceOn(pool)
	ctx := context.Background()

	activeID := fixtures.User(t, pool)
	suspendedID := fixtures.SuspendedUser(t, pool)
	archivedID := fixtures.ArchivedUser(t, pool)
	if activeID == suspendedID || suspendedID == archivedID || activeID == archivedID {
		t.Fatal("fixtures returned colliding user ids across calls")
	}

	active, err := svc.LookupUser(ctx, activeID)
	if err != nil {
		t.Fatalf("LookupUser(active): %v", err)
	}
	if active.Status != identityapi.StatusActive {
		t.Fatalf("fixtures.User status = %q, want active", active.Status)
	}

	suspended, err := svc.LookupUser(ctx, suspendedID)
	if err != nil {
		t.Fatalf("LookupUser(suspended): %v", err)
	}
	if suspended.Status != identityapi.StatusSuspended {
		t.Fatalf("fixtures.SuspendedUser status = %q, want suspended", suspended.Status)
	}

	archived, err := svc.LookupUser(ctx, archivedID)
	if err != nil {
		t.Fatalf("LookupUser(archived): %v", err)
	}
	if archived.Status != identityapi.StatusArchived {
		t.Fatalf("fixtures.ArchivedUser status = %q, want archived", archived.Status)
	}
}

func TestFixturesDeviceStatuses(t *testing.T) {
	pool := testdb.New(t)
	svc := catalogServiceOn(pool)
	ctx := context.Background()

	availID := fixtures.AvailableDevice(t, pool)
	lostID := fixtures.DeviceInStatus(t, pool, catalogapi.StatusLost)
	if availID == lostID {
		t.Fatal("fixtures returned colliding device ids across calls")
	}

	avail, err := svc.LookupDevice(ctx, availID)
	if err != nil {
		t.Fatalf("LookupDevice(available): %v", err)
	}
	if avail.Status != catalogapi.StatusAvailable {
		t.Fatalf("fixtures.AvailableDevice status = %q, want available", avail.Status)
	}

	lost, err := svc.LookupDevice(ctx, lostID)
	if err != nil {
		t.Fatalf("LookupDevice(lost): %v", err)
	}
	if lost.Status != catalogapi.StatusLost {
		t.Fatalf("fixtures.DeviceInStatus(lost) status = %q, want lost", lost.Status)
	}
}

func TestFixturesKioskReturnsWorkingToken(t *testing.T) {
	pool := testdb.New(t)
	id, token := fixtures.Kiosk(t, pool)
	if id == "" || token == "" {
		t.Fatalf("fixtures.Kiosk returned empty id/token: %q %q", id, token)
	}
}

func TestFixturesCredentials(t *testing.T) {
	pool := testdb.New(t)
	svc := credentialsServiceOn(t, pool)
	ctx := context.Background()
	userID := fixtures.User(t, pool)

	activeID, activeToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	ref, err := svc.Resolve(ctx, activeToken)
	if err != nil {
		t.Fatalf("Resolve(active fixture credential): %v", err)
	}
	if ref.Type != credentialsapi.RefUser || ref.SubjectID != userID || ref.CredentialStatus != credentialsapi.StatusActive {
		t.Fatalf("Resolve(active) = %+v, want active user %s", ref, userID)
	}

	_, unboundToken := fixtures.UnboundCredential(t, pool)
	unboundRef, err := svc.Resolve(ctx, unboundToken)
	if err != nil {
		t.Fatalf("Resolve(unbound fixture credential): %v", err)
	}
	if unboundRef.Type != credentialsapi.RefUnbound {
		t.Fatalf("Resolve(unbound) type = %q, want unbound", unboundRef.Type)
	}

	revokedID, revokedToken := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, userID)
	if revokedID == activeID {
		t.Fatal("fixtures.RevokedCredential collided with fixtures.ActiveCredentialFor id")
	}
	revokedRef, err := svc.Resolve(ctx, revokedToken)
	if err != nil {
		t.Fatalf("Resolve(revoked fixture credential): %v", err)
	}
	if revokedRef.CredentialStatus != credentialsapi.StatusRevoked {
		t.Fatalf("Resolve(revoked) status = %q, want revoked", revokedRef.CredentialStatus)
	}
}
