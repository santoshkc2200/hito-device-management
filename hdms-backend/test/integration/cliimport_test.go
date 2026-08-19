//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/cliimport"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newImportDeps(t *testing.T) (cliimport.DeviceImportDeps, cliimport.UserImportDeps) {
	t.Helper()
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	catalogSvc := catalog.New(pool, auditSvc)
	identitySvc := identity.New(pool, auditSvc)
	credentialsSvc := credentials.New(pool, auditSvc, "dev-only-pepper", random32(t))

	return cliimport.DeviceImportDeps{Catalog: catalogSvc, Credentials: credentialsSvc},
		cliimport.UserImportDeps{Identity: identitySvc, Credentials: credentialsSvc}
}

const devicesCSV = `asset_tag,name,category,manufacturer,model,serial_no,home_location,notes,acquired_on
LAPTOP-01,Dell Latitude 5420,Laptop,Dell,Latitude 5420,SN-1,Desk,,2025-03-14
LAPTOP-02,Dell Latitude 5420,Laptop,Dell,Latitude 5420,SN-2,Desk,,
`

const devicesCSVMissingField = `asset_tag,name,category
LAPTOP-01,Dell Latitude 5420,Laptop
,Missing Tag,Laptop
`

const usersCSV = `employee_no,full_name,department,email,phone,notes
HH-2401,Anjali Sharma,Radiology,anjali@example.org,,
HH-2402,Bimala Lama,Pharmacy,,,
`

func TestCliImportDevicesDryRunWritesNothing(t *testing.T) {
	deviceDeps, _ := newImportDeps(t)
	ctx := context.Background()

	report, err := cliimport.ImportDevices(ctx, deviceDeps, strings.NewReader(devicesCSV), true, false)
	if err != nil {
		t.Fatalf("ImportDevices dry-run: %v", err)
	}
	created, updated, rejected := report.Counts()
	if created != 2 || updated != 0 || rejected != 0 {
		t.Fatalf("dry-run counts = created:%d updated:%d rejected:%d, want 2/0/0", created, updated, rejected)
	}

	if _, err := deviceDeps.Catalog.LookupDeviceByAssetTag(ctx, "LAPTOP-01"); err == nil {
		t.Fatal("dry-run must not have created LAPTOP-01")
	}
}

func TestCliImportDevicesCreateThenIdempotentUpdate(t *testing.T) {
	deviceDeps, _ := newImportDeps(t)
	ctx := context.Background()

	report, err := cliimport.ImportDevices(ctx, deviceDeps, strings.NewReader(devicesCSV), false, false)
	if err != nil {
		t.Fatalf("ImportDevices first run: %v", err)
	}
	created, updated, rejected := report.Counts()
	if created != 2 || updated != 0 || rejected != 0 {
		t.Fatalf("first run counts = created:%d updated:%d rejected:%d, want 2/0/0", created, updated, rejected)
	}

	first, err := deviceDeps.Catalog.LookupDeviceByAssetTag(ctx, "LAPTOP-01")
	if err != nil {
		t.Fatalf("lookup after first run: %v", err)
	}

	// Second run on the same file: no duplicates, everything resolves as
	// an update of the rows the first run created.
	report, err = cliimport.ImportDevices(ctx, deviceDeps, strings.NewReader(devicesCSV), false, false)
	if err != nil {
		t.Fatalf("ImportDevices second run: %v", err)
	}
	created, updated, rejected = report.Counts()
	if created != 0 || updated != 2 || rejected != 0 {
		t.Fatalf("second run counts = created:%d updated:%d rejected:%d, want 0/2/0", created, updated, rejected)
	}

	second, err := deviceDeps.Catalog.LookupDeviceByAssetTag(ctx, "LAPTOP-01")
	if err != nil {
		t.Fatalf("lookup after second run: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("second run created a new device instead of updating: %s != %s", first.ID, second.ID)
	}
}

func TestCliImportDevicesRejectsRowWithoutAbortingOthers(t *testing.T) {
	deviceDeps, _ := newImportDeps(t)
	ctx := context.Background()

	report, err := cliimport.ImportDevices(ctx, deviceDeps, strings.NewReader(devicesCSVMissingField), false, false)
	if err != nil {
		t.Fatalf("ImportDevices: %v", err)
	}
	created, updated, rejected := report.Counts()
	if created != 1 || updated != 0 || rejected != 1 {
		t.Fatalf("counts = created:%d updated:%d rejected:%d, want 1/0/1", created, updated, rejected)
	}

	if _, err := deviceDeps.Catalog.LookupDeviceByAssetTag(ctx, "LAPTOP-01"); err != nil {
		t.Fatalf("the valid row should still have been created: %v", err)
	}
}

func TestCliImportDevicesMintCredentials(t *testing.T) {
	deviceDeps, _ := newImportDeps(t)
	ctx := context.Background()

	report, err := cliimport.ImportDevices(ctx, deviceDeps, strings.NewReader(devicesCSV), false, true)
	if err != nil {
		t.Fatalf("ImportDevices: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(report.Rows))
	}
	for _, row := range report.Rows {
		if row.CredentialToken == "" {
			t.Fatalf("row %d: expected a minted credential token", row.Line)
		}
	}

	device, err := deviceDeps.Catalog.LookupDeviceByAssetTag(ctx, "LAPTOP-01")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	ref, err := deviceDeps.Credentials.Resolve(ctx, report.Rows[0].CredentialToken)
	if err != nil {
		t.Fatalf("Resolve minted token: %v", err)
	}
	if ref.Type != credentialsapi.RefDevice || ref.SubjectID != device.ID {
		t.Fatalf("minted token resolved to %+v, want device %s", ref, device.ID)
	}
}

func TestCliImportUsersCreateThenIdempotentUpdate(t *testing.T) {
	_, userDeps := newImportDeps(t)
	ctx := context.Background()

	report, err := cliimport.ImportUsers(ctx, userDeps, strings.NewReader(usersCSV), false, false)
	if err != nil {
		t.Fatalf("ImportUsers first run: %v", err)
	}
	created, updated, rejected := report.Counts()
	if created != 2 || updated != 0 || rejected != 0 {
		t.Fatalf("first run counts = created:%d updated:%d rejected:%d, want 2/0/0", created, updated, rejected)
	}

	first, err := userDeps.Identity.LookupUserByEmployeeNo(ctx, "HH-2401")
	if err != nil {
		t.Fatalf("lookup after first run: %v", err)
	}

	report, err = cliimport.ImportUsers(ctx, userDeps, strings.NewReader(usersCSV), false, false)
	if err != nil {
		t.Fatalf("ImportUsers second run: %v", err)
	}
	created, updated, rejected = report.Counts()
	if created != 0 || updated != 2 || rejected != 0 {
		t.Fatalf("second run counts = created:%d updated:%d rejected:%d, want 0/2/0", created, updated, rejected)
	}

	second, err := userDeps.Identity.LookupUserByEmployeeNo(ctx, "HH-2401")
	if err != nil {
		t.Fatalf("lookup after second run: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("second run created a new user instead of updating: %s != %s", first.ID, second.ID)
	}
	if first.RegisteredBy != "import" {
		t.Fatalf("registeredBy = %q, want %q (INV-11)", first.RegisteredBy, "import")
	}
}

func TestCliImportUsersMintCredentials(t *testing.T) {
	_, userDeps := newImportDeps(t)
	ctx := context.Background()

	report, err := cliimport.ImportUsers(ctx, userDeps, strings.NewReader(usersCSV), false, true)
	if err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	user, err := userDeps.Identity.LookupUserByEmployeeNo(ctx, "HH-2401")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	ref, err := userDeps.Credentials.Resolve(ctx, report.Rows[0].CredentialToken)
	if err != nil {
		t.Fatalf("Resolve minted token: %v", err)
	}
	if ref.Type != credentialsapi.RefUser || ref.SubjectID != user.ID {
		t.Fatalf("minted token resolved to %+v, want user %s", ref, user.ID)
	}
}

func TestCliImportRejectsFileMissingRequiredColumn(t *testing.T) {
	deviceDeps, _ := newImportDeps(t)
	ctx := context.Background()

	_, err := cliimport.ImportDevices(ctx, deviceDeps, strings.NewReader("name,category\nFoo,Bar\n"), false, false)
	if err == nil {
		t.Fatal("expected an error for a CSV missing the required asset_tag column")
	}
}
