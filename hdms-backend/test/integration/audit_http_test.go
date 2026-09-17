//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPAuditEndpoints(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Perform some actions that produce audit records
	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Diagnostic ECG"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	_, err = h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "ECG-AUDIT-1",
		Name:       "ECG Machine",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	_ = h.audit.Record(ctx, auditapi.Event{
		Actor:   "admin:super",
		Action:  "device.overridden",
		Subject: "device:ECG-AUDIT-1",
		Payload: map[string]any{"reason": "routine calibration"},
	})

	// 1. GET /v1/audit as admin
	resp := h.get(t, "/v1/audit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/audit status = %d, want 200", resp.StatusCode)
	}

	list := decodeBody[gen.AuditEventList](t, resp)
	if len(list.Items) == 0 {
		t.Fatalf("list.Items is empty")
	}

	// 2. Filter by actor / action / subject
	resp = h.get(t, "/v1/audit?action=device.overridden")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/audit?action=device.overridden status = %d, want 200", resp.StatusCode)
	}
	filteredList := decodeBody[gen.AuditEventList](t, resp)
	if len(filteredList.Items) == 0 {
		t.Fatalf("filteredList.Items is empty")
	}
	if filteredList.Items[0].Action != "device.overridden" {
		t.Fatalf("item action = %s, want device.overridden", filteredList.Items[0].Action)
	}

	// 3. Export audit CSV
	resp = h.get(t, "/v1/audit.csv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/audit.csv status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll audit.csv: %v", err)
	}
	resp.Body.Close()

	if !bytes.HasPrefix(body, []byte("\xef\xbb\xbf")) {
		t.Fatalf("audit.csv is missing UTF-8 BOM")
	}

	csvReader := csv.NewReader(bytes.NewReader(body[3:]))
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll audit.csv: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("audit.csv record count = %d, want >= 2", len(records))
	}
	if records[0][0] != "id" || records[0][2] != "actor" {
		t.Fatalf("audit.csv header = %+v", records[0])
	}
}

// TestStoredAuditPayloadCarriesNoSecret proves the redaction runs on the write
// path itself, not only in the SanitizePayload unit test: an event recorded
// with a credential token must come back out of the database redacted.
func TestStoredAuditPayloadCarriesNoSecret(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	const plaintextToken = "HD-U-9Q4M2XZA7F-3"

	if err := h.audit.Record(ctx, auditapi.Event{
		Actor:   "admin:super",
		Action:  "credential.issued",
		Subject: "user:redaction-probe",
		Payload: map[string]any{
			"token":    plaintextToken,
			"password": "secretPassword123",
			"nested":   map[string]any{"kioskToken": plaintextToken},
			"reason":   "redaction probe",
		},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := h.audit.List(ctx, auditapi.ListParams{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var found bool
	for _, e := range entries {
		if e.Subject != "user:redaction-probe" {
			continue
		}
		found = true
		raw, err := json.Marshal(e.Payload)
		if err != nil {
			t.Fatalf("marshal stored payload: %v", err)
		}
		if strings.Contains(string(raw), plaintextToken) {
			t.Fatalf("stored audit payload contains the plaintext token: %s", raw)
		}
		if strings.Contains(string(raw), "secretPassword123") {
			t.Fatalf("stored audit payload contains the plaintext password: %s", raw)
		}
		if !strings.Contains(string(raw), "redaction probe") {
			t.Fatalf("stored audit payload lost its non-sensitive field: %s", raw)
		}
	}
	if !found {
		t.Fatal("recorded audit event was not returned by List")
	}
}
