//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newIdentityService(t *testing.T) (*identity.Service, *audit.Service) {
	t.Helper()
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	return identity.New(pool, auditSvc), auditSvc
}

func TestIdentityCreateAndLookupUser(t *testing.T) {
	svc, auditSvc := newIdentityService(t)
	ctx := context.Background()

	created, err := svc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "HH-2407",
		FullName:     "Dr. A. Sharma",
		Email:        "sharma@example.org",
		RegisteredBy: "admin:0192a1",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.Status != identityapi.StatusActive {
		t.Fatalf("new user status = %q, want active", created.Status)
	}

	byID, err := svc.LookupUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("LookupUser: %v", err)
	}
	if byID.EmployeeNo != "HH-2407" {
		t.Fatalf("LookupUser employee no = %q", byID.EmployeeNo)
	}

	byEmployeeNo, err := svc.LookupUserByEmployeeNo(ctx, "hh-2407")
	if err != nil {
		t.Fatalf("LookupUserByEmployeeNo (case-insensitive): %v", err)
	}
	if byEmployeeNo.ID != created.ID {
		t.Fatalf("LookupUserByEmployeeNo returned a different user")
	}

	// Every mutation is audited (Phase 1 task 1.1).
	entries, err := auditSvc.List(ctx, auditapi.ListParams{})
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Action == "user.created" && e.Subject == "user:"+created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a user.created audit event for %s, got %+v", created.ID, entries)
	}
}

func TestIdentityDuplicateEmployeeNoRejected(t *testing.T) {
	svc, _ := newIdentityService(t)
	ctx := context.Background()

	params := identityapi.CreateUserParams{
		EmployeeNo: "HH-3000", FullName: "First Person", RegisteredBy: "admin:1",
	}
	if _, err := svc.CreateUser(ctx, params); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}

	params.FullName = "Second Person"
	_, err := svc.CreateUser(ctx, params)
	if !errors.Is(err, identityapi.ErrEmployeeNoTaken) {
		t.Fatalf("second CreateUser error = %v, want ErrEmployeeNoTaken", err)
	}
}

func TestIdentityKioskCannotRegisterUser(t *testing.T) {
	// INV-11: a user is only ever created by an administrator or an
	// import — never a kiosk.
	svc, _ := newIdentityService(t)
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo: "HH-9999", FullName: "Someone", RegisteredBy: "kiosk:lobby-1",
	})
	if err == nil {
		t.Fatal("expected CreateUser to reject a kiosk registrar, got nil error")
	}
}

func TestIdentityStatusLifecycle(t *testing.T) {
	svc, _ := newIdentityService(t)
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo: "HH-4000", FullName: "Lifecycle Test", RegisteredBy: "admin:1",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	suspended, err := svc.SuspendUser(ctx, user.ID, "on leave", "admin:1")
	if err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}
	if suspended.Status != identityapi.StatusSuspended {
		t.Fatalf("status = %q, want suspended", suspended.Status)
	}

	reactivated, err := svc.ReactivateUser(ctx, user.ID, "admin:1")
	if err != nil {
		t.Fatalf("ReactivateUser: %v", err)
	}
	if reactivated.Status != identityapi.StatusActive {
		t.Fatalf("status = %q, want active", reactivated.Status)
	}

	archived, err := svc.ArchiveUser(ctx, user.ID, "left the hospital", "admin:1")
	if err != nil {
		t.Fatalf("ArchiveUser: %v", err)
	}
	if archived.Status != identityapi.StatusArchived {
		t.Fatalf("status = %q, want archived", archived.Status)
	}

	// Archived is terminal (INV-10): no route back.
	if _, err := svc.ReactivateUser(ctx, user.ID, "admin:1"); err == nil {
		t.Fatal("expected reactivating an archived user to fail")
	}
}

func TestIdentityArchivedEmployeeNoCanBeReused(t *testing.T) {
	// The live-uniqueness index allows a re-hire's new record to reuse an
	// archived predecessor's employee number without collision.
	svc, _ := newIdentityService(t)
	ctx := context.Background()

	first, err := svc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo: "HH-5000", FullName: "Original Hire", RegisteredBy: "admin:1",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.ArchiveUser(ctx, first.ID, "left", "admin:1"); err != nil {
		t.Fatalf("ArchiveUser: %v", err)
	}

	second, err := svc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo: "HH-5000", FullName: "Re-hire", RegisteredBy: "admin:1",
	})
	if err != nil {
		t.Fatalf("CreateUser (re-hire reusing archived employee no): %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new user record for the re-hire")
	}
}

func TestIdentityListUsersPagination(t *testing.T) {
	svc, _ := newIdentityService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := svc.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo: "PG-" + string(rune('A'+i)), FullName: "Page Test", RegisteredBy: "admin:1",
		})
		if err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
	}

	page1, err := svc.ListUsers(ctx, identityapi.ListUsersParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListUsers page 1: %v", err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 items and a next cursor", page1)
	}

	page2, err := svc.ListUsers(ctx, identityapi.ListUsersParams{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("ListUsers page 2: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("page2 = %+v, want 2 items", page2)
	}
	for _, a := range page1.Items {
		for _, b := range page2.Items {
			if a.ID == b.ID {
				t.Fatalf("page1 and page2 both contain user %s", a.ID)
			}
		}
	}
}

func TestIdentityDepartments(t *testing.T) {
	svc, _ := newIdentityService(t)
	ctx := context.Background()

	d1, err := svc.GetOrCreateDepartment(ctx, "Radiology")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}
	d2, err := svc.GetOrCreateDepartment(ctx, "Radiology")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment (repeat): %v", err)
	}
	if d1.ID != d2.ID {
		t.Fatalf("expected GetOrCreateDepartment to be idempotent by name, got %s and %s", d1.ID, d2.ID)
	}

	depts, err := svc.ListDepartments(ctx)
	if err != nil {
		t.Fatalf("ListDepartments: %v", err)
	}
	if len(depts) != 1 {
		t.Fatalf("ListDepartments = %+v, want 1", depts)
	}
}
