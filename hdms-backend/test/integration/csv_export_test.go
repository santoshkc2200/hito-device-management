//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
)

func TestStreamingCSVExports(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	dept, err := h.identity.GetOrCreateDepartment(ctx, "Pediatrics")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	user, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-CSV-01",
		FullName:     "Tanaka, Taro", // Test RFC 4180 comma quoting and non-ASCII characters
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Tablets"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	device, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-CSV-01",
		Name:       "Clinical iPad \"Mini\"", // Test quote escaping
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	_, err = h.lending.OpenLoan(ctx, device.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}

	// 1. Loans CSV Export
	resp := h.get(t, "/v1/reports/loans.csv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /reports/loans.csv status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("Content-Type = %q, want text/csv", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", cd)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll loans.csv: %v", err)
	}
	resp.Body.Close()

	// Check UTF-8 BOM
	if !bytes.HasPrefix(body, []byte("\xef\xbb\xbf")) {
		t.Fatalf("loans.csv is missing UTF-8 BOM")
	}

	csvReader := csv.NewReader(bytes.NewReader(body[3:])) // Strip BOM
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll loans: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("loans.csv record count = %d, want >= 2 (header + data)", len(records))
	}
	header := records[0]
	if header[0] != "id" || header[2] != "asset_tag" {
		t.Fatalf("loans.csv header = %+v", header)
	}

	// 2. Devices CSV Export
	resp = h.get(t, "/v1/reports/devices.csv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /reports/devices.csv status = %d, want 200", resp.StatusCode)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll devices.csv: %v", err)
	}
	resp.Body.Close()

	if !bytes.HasPrefix(body, []byte("\xef\xbb\xbf")) {
		t.Fatalf("devices.csv is missing UTF-8 BOM")
	}
	csvReader = csv.NewReader(bytes.NewReader(body[3:]))
	records, err = csvReader.ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll devices: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("devices.csv record count = %d, want >= 2", len(records))
	}

	// 3. Users CSV Export
	resp = h.get(t, "/v1/reports/users.csv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /reports/users.csv status = %d, want 200", resp.StatusCode)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll users.csv: %v", err)
	}
	resp.Body.Close()

	if !bytes.HasPrefix(body, []byte("\xef\xbb\xbf")) {
		t.Fatalf("users.csv is missing UTF-8 BOM")
	}
	csvReader = csv.NewReader(bytes.NewReader(body[3:]))
	records, err = csvReader.ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll users: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("users.csv record count = %d, want >= 2", len(records))
	}
}
