//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/test/testdb"
)

// TestSyncIsIdempotentAcrossRuns proves 6.3b:
// Running directory sync repeatedly produces the same end state, updating on the first
// run and reporting unchanged on subsequent runs with the same directory data.
func TestSyncIsIdempotentAcrossRuns(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	identSvc := identity.New(pool, audit.New(pool))

	user, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-SYNC-01",
		FullName:     "Alice Original",
		Email:        "alice.original@example.com",
		Phone:        "111-0001",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	subject := "uid=alice,ou=staff,dc=example,dc=com"
	if err := identSvc.LinkExternalIdentity(ctx, user.ID, "ldap", subject, now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("LinkExternalIdentity: %v", err)
	}

	fakeClient := identity.NewFakeDirectoryClient(identity.DirectoryEntry{
		Subject:    subject,
		EmployeeNo: "EMP-SYNC-01",
		FullName:   "Alice Updated",
		Department: "Cardiology",
		Email:      "alice.updated@example.com",
		Phone:      "111-9999",
	})

	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 1.0,
		GracePeriod:       7 * 24 * time.Hour,
	}

	// 1. First run: applies updates
	rep1, err := jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now, opts, "")
	if err != nil {
		t.Fatalf("sync run 1: %v", err)
	}
	if rep1.Updated != 1 {
		t.Fatalf("run 1 updated = %d, want 1", rep1.Updated)
	}
	if rep1.Unchanged != 0 {
		t.Fatalf("run 1 unchanged = %d, want 0", rep1.Unchanged)
	}

	u1, err := identSvc.LookupUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if u1.FullName != "Alice Updated" {
		t.Fatalf("full name = %q, want 'Alice Updated'", u1.FullName)
	}
	if u1.Email != "alice.updated@example.com" {
		t.Fatalf("email = %q, want 'alice.updated@example.com'", u1.Email)
	}
	if u1.DepartmentID == "" {
		t.Fatalf("expected department to be resolved and set")
	}

	// 2. Second run: directory data unchanged -> 0 updates, 1 unchanged
	rep2, err := jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now.Add(1*time.Hour), opts, "")
	if err != nil {
		t.Fatalf("sync run 2: %v", err)
	}
	if rep2.Updated != 0 {
		t.Fatalf("run 2 updated = %d, want 0", rep2.Updated)
	}
	if rep2.Unchanged != 1 {
		t.Fatalf("run 2 unchanged = %d, want 1", rep2.Unchanged)
	}

	// 3. Reinstatement: manually suspend user, then run sync again
	if _, err := identSvc.SuspendUser(ctx, user.ID, "manual test suspension", "admin:test"); err != nil {
		t.Fatalf("suspend user: %v", err)
	}

	rep3, err := jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now.Add(2*time.Hour), opts, "")
	if err != nil {
		t.Fatalf("sync run 3 (reinstatement): %v", err)
	}
	if rep3.Reinstated != 1 {
		t.Fatalf("run 3 reinstated = %d, want 1", rep3.Reinstated)
	}

	u3, err := identSvc.LookupUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("lookup user after reinstatement: %v", err)
	}
	if u3.Status != identityapi.StatusActive {
		t.Fatalf("user status = %q, want active", u3.Status)
	}
}

// TestSyncNeverWritesToTheDirectory proves 6.3b:
// Sync is strictly read-only against the directory. It never issues an add,
// modify, or delete call against LDAP.
func TestSyncNeverWritesToTheDirectory(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	identSvc := identity.New(pool, audit.New(pool))
	user, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-RO-01",
		FullName:     "Read Only Test",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	subject := "uid=readonly,ou=staff,dc=example,dc=com"
	if err := identSvc.LinkExternalIdentity(ctx, user.ID, "ldap", subject, now); err != nil {
		t.Fatalf("LinkExternalIdentity: %v", err)
	}

	fakeClient := identity.NewFakeDirectoryClient(identity.DirectoryEntry{
		Subject:    subject,
		EmployeeNo: "EMP-RO-01",
		FullName:   "Read Only Changed",
	})

	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 1.0,
	}

	_, err = jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now, opts, "")
	if err != nil {
		t.Fatalf("RunDirectorySync: %v", err)
	}

	searchCalls, writeCalls := fakeClient.Stats()
	if searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", searchCalls)
	}
	if writeCalls != 0 {
		t.Fatalf("write calls = %d, want 0 (sync must NEVER write to directory)", writeCalls)
	}
}

// TestAMassChangeIsRefusedAndReported proves 6.3b:
// If a sync proposes to change more than a configurable fraction of the roster (default 10%),
// it refuses to execute, writes a failure job_runs row, and reports what it would have changed.
func TestAMassChangeIsRefusedAndReported(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	identSvc := identity.New(pool, audit.New(pool))

	// Create 10 linked users
	for i := 1; i <= 10; i++ {
		empNo := fmt.Sprintf("EMP-MASS-%02d", i)
		u, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo:   empNo,
			FullName:     "User " + empNo,
			RegisteredBy: "admin:bootstrap",
		})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		subject := "uid=" + empNo + ",ou=staff,dc=example,dc=com"
		if err := identSvc.LinkExternalIdentity(ctx, u.ID, "ldap", subject, now); err != nil {
			t.Fatalf("LinkExternalIdentity: %v", err)
		}
	}

	// Propose changes to 3 of them (3/10 = 30% > 10% threshold)
	entries := make([]identity.DirectoryEntry, 10)
	for i := 1; i <= 10; i++ {
		empNo := fmt.Sprintf("EMP-MASS-%02d", i)
		name := "User " + empNo
		if i <= 3 {
			name = "User " + empNo + " Renamed"
		}
		entries[i-1] = identity.DirectoryEntry{
			Subject:    "uid=" + empNo + ",ou=staff,dc=example,dc=com",
			EmployeeNo: empNo,
			FullName:   name,
		}
	}

	fakeClient := identity.NewFakeDirectoryClient(entries...)

	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 0.10, // 10% limit
		GracePeriod:       7 * 24 * time.Hour,
	}

	report, err := jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now, opts, "")
	if err == nil {
		t.Fatalf("expected mass-change error, got nil")
	}
	if !errors.Is(err, identity.ErrMassChangeRefused) {
		t.Fatalf("expected ErrMassChangeRefused, got %v", err)
	}
	if !report.MassChangeRefused {
		t.Fatalf("expected report.MassChangeRefused = true")
	}
	if len(report.ProposedChanges) != 3 {
		t.Fatalf("proposed changes count = %d, want 3", len(report.ProposedChanges))
	}

	// Assert a failure row was recorded in job_runs
	var outcome, detailStr string
	err = pool.QueryRow(ctx, `
		SELECT outcome, detail::text FROM job_runs
		WHERE job = 'directory-sync'
		ORDER BY started_at DESC LIMIT 1
	`).Scan(&outcome, &detailStr)
	if err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if outcome != "failure" {
		t.Fatalf("job_runs outcome = %q, want 'failure'", outcome)
	}

	// Verify database was NOT changed
	var unchangedNameCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM users WHERE full_name LIKE '%Renamed'
	`).Scan(&unchangedNameCount)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if unchangedNameCount != 0 {
		t.Fatalf("expected 0 users modified in database on mass-change refusal, got %d", unchangedNameCount)
	}
}

// TestLocalOnlyUsersAreNeverTouchedBySync proves 6.3b:
// Users without an external identity link (contractors, volunteers, local accounts)
// are explicitly out of scope of sync and are never modified or suspended.
func TestLocalOnlyUsersAreNeverTouchedBySync(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	identSvc := identity.New(pool, audit.New(pool))

	// 1. Local-only contractor (NO directory link)
	contractor, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "CONT-001",
		FullName:     "Bob Contractor",
		Email:        "bob@contractor.local",
		Phone:        "555-0000",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser (contractor): %v", err)
	}

	// 2. Permanent staff member (HAS directory link)
	perm, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "PERM-001",
		FullName:     "Carol Perm",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser (perm): %v", err)
	}
	subject := "uid=carol,ou=staff,dc=example,dc=com"
	if err := identSvc.LinkExternalIdentity(ctx, perm.ID, "ldap", subject, now); err != nil {
		t.Fatalf("LinkExternalIdentity: %v", err)
	}

	// Directory contains only Carol with an updated name. Bob is not in directory.
	fakeClient := identity.NewFakeDirectoryClient(identity.DirectoryEntry{
		Subject:    subject,
		EmployeeNo: "PERM-001",
		FullName:   "Carol Perm Updated",
	})

	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 1.0,
		GracePeriod:       0, // Zero grace period: any missing linked user would be suspended
	}

	_, err = jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now, opts, "")
	if err != nil {
		t.Fatalf("RunDirectorySync: %v", err)
	}

	// Verify Carol was updated
	cUser, err := identSvc.LookupUser(ctx, perm.ID)
	if err != nil {
		t.Fatalf("LookupUser(perm): %v", err)
	}
	if cUser.FullName != "Carol Perm Updated" {
		t.Fatalf("Carol name = %q, want 'Carol Perm Updated'", cUser.FullName)
	}

	// Verify Bob (contractor) is completely untouched
	bUser, err := identSvc.LookupUser(ctx, contractor.ID)
	if err != nil {
		t.Fatalf("LookupUser(contractor): %v", err)
	}
	if bUser.Status != identityapi.StatusActive {
		t.Fatalf("contractor status = %q, want active (never suspended)", bUser.Status)
	}
	if bUser.FullName != "Bob Contractor" || bUser.Email != "bob@contractor.local" || bUser.Phone != "555-0000" {
		t.Fatalf("contractor details modified by sync: %+v", bUser)
	}
}

// TestLeaverIsSuspendedAndCredentialsRevoked proves 6.3c:
// When a staff member disappears from the directory and the grace period expires,
// they are suspended (never deleted), and all active credentials are revoked.
func TestLeaverIsSuspendedAndCredentialsRevoked(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	identSvc := identity.New(pool, audit.New(pool))
	credsSvc := credentialsServiceOn(t, pool)

	user, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "LEAVER-01",
		FullName:     "Dave Leaver",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Issue active credential to Dave
	cred, err := credsSvc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   user.ID,
		Kind:        credentialsapi.KindQR,
		Label:       "Staff Badge",
		IssuedBy:    "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("Issue credential: %v", err)
	}

	// Link user with last seen 8 days ago (> 7-day grace period)
	subject := "uid=dave,ou=staff,dc=example,dc=com"
	lastSeen := now.Add(-8 * 24 * time.Hour)
	if err := identSvc.LinkExternalIdentity(ctx, user.ID, "ldap", subject, lastSeen); err != nil {
		t.Fatalf("LinkExternalIdentity: %v", err)
	}

	// Directory returns empty list: Dave is gone
	fakeClient := identity.NewFakeDirectoryClient()

	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 1.0,
		GracePeriod:       7 * 24 * time.Hour,
	}

	rep, err := jobs.RunDirectorySync(ctx, pool, audit.New(pool), fakeClient, now, opts, "")
	if err != nil {
		t.Fatalf("RunDirectorySync: %v", err)
	}
	if rep.Suspended != 1 {
		t.Fatalf("suspended count = %d, want 1", rep.Suspended)
	}
	if rep.CredentialsRevoked != 1 {
		t.Fatalf("credentials revoked = %d, want 1", rep.CredentialsRevoked)
	}

	// Assert user is suspended, not deleted
	u, err := identSvc.LookupUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("LookupUser: %v (user must not be deleted)", err)
	}
	if u.Status != identityapi.StatusSuspended {
		t.Fatalf("user status = %q, want suspended", u.Status)
	}

	// Assert credential is now revoked
	clist, err := credsSvc.ListBySubject(ctx, credentialsapi.SubjectUser, user.ID)
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	if len(clist) != 1 {
		t.Fatalf("creds count = %d, want 1", len(clist))
	}
	if clist[0].Status != credentialsapi.StatusRevoked {
		t.Fatalf("cred status = %q, want revoked", clist[0].Status)
	}
	if clist[0].ID != cred.ID {
		t.Fatalf("revoked cred id mismatch")
	}
}

// TestLeaverWithAnOpenLoanIsEscalatedNotSilentlyClosed proves 6.3c:
// A leaver holding an open loan is suspended, the loan is NOT closed, and the open loan
// appears on the admin leaver escalations list with the leaver's last known department.
func TestLeaverWithAnOpenLoanIsEscalatedNotSilentlyClosed(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Create department and user
	dept, err := h.identity.GetOrCreateDepartment(ctx, "Emergency Medicine")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	user, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "LEAVER-LOAN-01",
		FullName:     "Dr. Frank Escalate",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// 2. Create device and open a loan
	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Diagnostic Tablet"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	device, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-999",
		Name:       "ED Clinical Tablet",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	loan, err := h.lending.OpenLoan(ctx, device.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:        "admin:bootstrap",
		Source:       "manual",
		ConditionOut: "good",
	})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}

	// 3. Link user as leaver past grace period
	identSvc := identity.New(h.pool, h.audit)
	subject := "uid=frank,ou=staff,dc=example,dc=com"
	if err := identSvc.LinkExternalIdentity(ctx, user.ID, "ldap", subject, now.Add(-10*24*time.Hour)); err != nil {
		t.Fatalf("LinkExternalIdentity: %v", err)
	}

	fakeClient := identity.NewFakeDirectoryClient()
	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 1.0,
		GracePeriod:       7 * 24 * time.Hour,
	}

	rep, err := jobs.RunDirectorySync(ctx, h.pool, h.audit, fakeClient, now, opts, "")
	if err != nil {
		t.Fatalf("RunDirectorySync: %v", err)
	}
	if rep.Suspended != 1 {
		t.Fatalf("suspended = %d, want 1", rep.Suspended)
	}
	if rep.OpenLoansEscalated != 1 {
		t.Fatalf("open loans escalated = %d, want 1", rep.OpenLoansEscalated)
	}

	// 4. Assert loan is STILL open (not closed or written off)
	l, err := h.lending.GetLoan(ctx, loan.ID)
	if err != nil {
		t.Fatalf("GetLoan: %v", err)
	}
	if l.Status != lendingapi.StatusOpen {
		t.Fatalf("loan status = %q, want open", l.Status)
	}
	if l.ReturnedAt != nil {
		t.Fatalf("loan returned_at is not nil: %v", l.ReturnedAt)
	}

	// 5. Query the admin leaver escalations endpoint: GET /v1/reports/leaver-escalations
	res := h.get(t, "/v1/reports/leaver-escalations")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/reports/leaver-escalations status = %d, want 200", res.StatusCode)
	}

	var escalationList gen.LeaverEscalationList
	if err := json.NewDecoder(res.Body).Decode(&escalationList); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(escalationList.Items) != 1 {
		t.Fatalf("escalation items = %d, want 1", len(escalationList.Items))
	}

	item := escalationList.Items[0]
	if item.LoanId != loan.ID {
		t.Fatalf("item.LoanId = %s, want %s", item.LoanId, loan.ID)
	}
	if item.DeviceId != device.ID {
		t.Fatalf("item.DeviceId = %s, want %s", item.DeviceId, device.ID)
	}
	if item.DeviceAssetTag != "TAB-999" {
		t.Fatalf("item.DeviceAssetTag = %s, want TAB-999", item.DeviceAssetTag)
	}
	if item.BorrowerName != "Dr. Frank Escalate" {
		t.Fatalf("item.BorrowerName = %s, want 'Dr. Frank Escalate'", item.BorrowerName)
	}
	if item.DepartmentName != "Emergency Medicine" {
		t.Fatalf("item.DepartmentName = %q, want 'Emergency Medicine'", item.DepartmentName)
	}
}

// TestHistoryIsUnchangedBySuspension proves 6.3c:
// Suspending a leaver never alters, deletes, or scrubs previous loan history.
func TestHistoryIsUnchangedBySuspension(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	auditSvc := audit.New(pool)
	identSvc := identity.New(pool, auditSvc)
	catSvc := catalog.New(pool, auditSvc)
	lendingSvc := lending.New(pool, auditSvc, clock.System{})

	user, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "HIST-USER-01",
		FullName:     "Grace History",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cat, err := catSvc.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Thermometer"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	dev1, err := catSvc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag: "TH-001", Name: "Infrared Thermometer 1", CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 1: %v", err)
	}
	dev2, err := catSvc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag: "TH-002", Name: "Infrared Thermometer 2", CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 2: %v", err)
	}

	// Create loan 1: borrowed and returned in the past
	loan1, err := lendingSvc.OpenLoan(ctx, dev1.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:        "admin:bootstrap",
		Source:       "manual",
		ConditionOut: "good",
	})
	if err != nil {
		t.Fatalf("OpenLoan 1: %v", err)
	}
	loan1Returned, err := lendingSvc.CloseLoan(ctx, loan1.ID, lendingapi.CloseMeta{
		Actor:       "admin:bootstrap",
		Source:      "manual",
		ConditionIn: "good",
	})
	if err != nil {
		t.Fatalf("CloseLoan 1: %v", err)
	}

	// Create loan 2: open
	loan2, err := lendingSvc.OpenLoan(ctx, dev2.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:        "admin:bootstrap",
		Source:       "manual",
		ConditionOut: "fair",
	})
	if err != nil {
		t.Fatalf("OpenLoan 2: %v", err)
	}

	// Link Grace and suspend via directory sync
	subject := "uid=grace,ou=staff,dc=example,dc=com"
	if err := identSvc.LinkExternalIdentity(ctx, user.ID, "ldap", subject, now.Add(-14*24*time.Hour)); err != nil {
		t.Fatalf("LinkExternalIdentity: %v", err)
	}

	fakeClient := identity.NewFakeDirectoryClient()
	opts := identity.DirectorySyncOptions{
		Issuer:            "ldap",
		Apply:             true,
		DryRun:            false,
		MaxChangeFraction: 1.0,
		GracePeriod:       7 * 24 * time.Hour,
	}

	if _, err := jobs.RunDirectorySync(ctx, pool, auditSvc, fakeClient, now, opts, ""); err != nil {
		t.Fatalf("RunDirectorySync: %v", err)
	}

	// Verify Loan 1 (returned) is completely unchanged
	l1After, err := lendingSvc.GetLoan(ctx, loan1.ID)
	if err != nil {
		t.Fatalf("GetLoan 1: %v", err)
	}
	if l1After.Status != lendingapi.StatusReturned {
		t.Fatalf("loan 1 status = %q, want returned", l1After.Status)
	}
	if l1After.ReturnedAt == nil || !l1After.ReturnedAt.Equal(*loan1Returned.ReturnedAt) {
		t.Fatalf("loan 1 returned_at changed: was %v, now %v", loan1Returned.ReturnedAt, l1After.ReturnedAt)
	}
	if l1After.ConditionOut != "good" || l1After.ConditionIn != "good" {
		t.Fatalf("loan 1 condition corrupted")
	}

	// Verify Loan 2 (open) is still open
	l2After, err := lendingSvc.GetLoan(ctx, loan2.ID)
	if err != nil {
		t.Fatalf("GetLoan 2: %v", err)
	}
	if l2After.Status != lendingapi.StatusOpen {
		t.Fatalf("loan 2 status = %q, want open", l2After.Status)
	}
}

// TestDirectoryOutageDoesNotAffectKioskScanResolution proves 6.3c / 6.3 exit criteria:
// Kiosk scan resolution resolves solely against the local credentials table and HMAC index.
// When the directory is down (LDAP connection failure / outage), kiosk borrowing/returning
// scan resolution continues completely unaffected.
func TestDirectoryOutageDoesNotAffectKioskScanResolution(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	auditSvc := audit.New(pool)
	identSvc := identity.New(pool, auditSvc)
	credsSvc := credentialsServiceOn(t, pool)

	user, err := identSvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "KIOSK-STAFF-01",
		FullName:     "Counter Staff",
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cred, err := credsSvc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   user.ID,
		Kind:        credentialsapi.KindQR,
		Label:       "Staff ID Card",
		IssuedBy:    "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("Issue credential: %v", err)
	}

	// Simulate complete directory outage
	errOutage := errors.New("dial tcp 10.0.0.5:389: i/o timeout (active directory offline)")
	brokenClient := &identity.FakeDirectoryClient{
		Err: errOutage,
	}

	// Verify directory sync fails due to outage
	opts := identity.DirectorySyncOptions{Issuer: "ldap", DryRun: true}
	_, err = jobs.RunDirectorySync(ctx, pool, auditSvc, brokenClient, time.Now().UTC(), opts, "")
	if err == nil {
		t.Fatalf("expected directory sync to fail during outage")
	}

	// Kiosk scan resolution: resolves the issued plaintext token
	ref, err := credsSvc.Resolve(ctx, cred.Token)
	if err != nil {
		t.Fatalf("Resolve failed during directory outage: %v (counter MUST NOT depend on directory)", err)
	}

	if ref.Type != credentialsapi.RefUser {
		t.Fatalf("ref.Type = %v, want user", ref.Type)
	}
	if ref.SubjectID != user.ID {
		t.Fatalf("ref.SubjectID = %s, want %s", ref.SubjectID, user.ID)
	}
	if ref.CredentialStatus != credentialsapi.StatusActive {
		t.Fatalf("ref.CredentialStatus = %v, want active", ref.CredentialStatus)
	}
}
