//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/test/testdb"
)

func TestStaffIdentityIsUniquePerProviderSubject(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	var userID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (id, employee_no, full_name, registered_by)
		VALUES (gen_random_uuid(), 'E-STAFF-1', 'Test Staff', 'admin:test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO staff_accounts (id, user_id, created_by)
		VALUES (gen_random_uuid(), $1, 'admin:test') RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert staff account: %v", err)
	}

	insert := `INSERT INTO staff_identities (id, staff_account_id, provider, subject, tenant_id)
	           VALUES (gen_random_uuid(), $1, 'microsoft', 'oid-1', 'tenant-1')`
	if _, err := pool.Exec(ctx, insert, accountID); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, accountID); err == nil {
		t.Fatal("second link with the same (provider, subject) succeeded; the unique constraint is missing")
	}
}

func TestLiveUserEmailsAreUniqueButArchivedOnesAreNot(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	mk := `INSERT INTO users (id, employee_no, full_name, email, status, registered_by)
	       VALUES (gen_random_uuid(), $1, 'Test Staff', $2, $3, 'admin:test')`

	if _, err := pool.Exec(ctx, mk, "E-MAIL-1", "person@hospital.example", "active"); err != nil {
		t.Fatalf("first live user: %v", err)
	}
	if _, err := pool.Exec(ctx, mk, "E-MAIL-2", "PERSON@hospital.example", "active"); err == nil {
		t.Fatal("a second live user with the same email (different case) was allowed")
	}
	if _, err := pool.Exec(ctx, mk, "E-MAIL-3", "person@hospital.example", "archived"); err != nil {
		t.Fatalf("an archived duplicate must still be allowed: %v", err)
	}
}
