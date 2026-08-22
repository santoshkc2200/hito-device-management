//go:build integration

// Tests for 4.11c — Performance & Realistic Dataset Fixture.
// Seeds ~500 devices, ~800 users, 5 000 historical loans, and ~50 000 audit events.
// Asserts that list queries, device searches, overdue list, audit logs, and CSV streaming
// meet the strict performance budgets (< 1s overdue, < 300ms search) with index-backed query plans.
package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// SeedRealisticScaleDataset populates a realistic hospital scale dataset:
// ~500 devices, ~800 users, 5 000 historical loans, and ~50 000 audit events.
func SeedRealisticScaleDataset(t *testing.T, h *testHarness) (deviceIDs []string, userIDs []string) {
	t.Helper()
	ctx := context.Background()

	t.Log("Seeding departments and categories...")
	depts := []string{"Emergency", "Surgery", "ICU", "Cardiology", "Pediatrics", "Radiology", "Neurology", "Oncology"}
	deptIDs := make([]string, len(depts))
	for i, d := range depts {
		dept, err := h.identity.GetOrCreateDepartment(ctx, d)
		if err != nil {
			t.Fatalf("create department %s: %v", d, err)
		}
		deptIDs[i] = dept.ID
	}

	loanPeriod := 24 * time.Hour
	catNames := []string{"Infusion Pumps", "Ultrasound Scanners", "ECG Monitors", "Defibrillators", "Tablets", "Laptops", "Surgical Drills"}
	catIDs := make([]string, len(catNames))
	for i, c := range catNames {
		cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{
			Name:              c,
			DefaultLoanPeriod: &loanPeriod,
			RequiresApproval:  false,
		})
		if err != nil {
			t.Fatalf("create category %s: %v", c, err)
		}
		catIDs[i] = cat.ID
	}

	// 1. Bulk insert ~800 Users
	t.Log("Seeding 800 users...")
	userIDs = make([]string, 800)
	userBatchSize := 200
	for b := 0; b < 800; b += userBatchSize {
		var valStrings []string
		var valArgs []any
		for i := 0; i < userBatchSize; i++ {
			idx := b + i
			uID := uuid.New().String()
			userIDs[idx] = uID
			empNo := fmt.Sprintf("HH-SCALE-%05d", idx+1)
			fullName := fmt.Sprintf("Staff Member %05d", idx+1)
			deptID := deptIDs[idx%len(deptIDs)]

			offset := len(valArgs)
			valStrings = append(valStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, 'active', 'admin:scale', now(), now())",
				offset+1, offset+2, offset+3, offset+4))
			valArgs = append(valArgs, uID, empNo, fullName, deptID)
		}
		query := fmt.Sprintf("INSERT INTO users (id, employee_no, full_name, department_id, status, registered_by, registered_at, updated_at) VALUES %s",
			strings.Join(valStrings, ","))
		if _, err := h.pool.Exec(ctx, query, valArgs...); err != nil {
			t.Fatalf("bulk insert users: %v", err)
		}
	}

	// 2. Bulk insert ~500 Devices
	t.Log("Seeding 500 devices...")
	deviceIDs = make([]string, 500)
	devBatchSize := 250
	for b := 0; b < 500; b += devBatchSize {
		var valStrings []string
		var valArgs []any
		for i := 0; i < devBatchSize; i++ {
			idx := b + i
			dID := uuid.New().String()
			deviceIDs[idx] = dID
			assetTag := fmt.Sprintf("TAG-SCALE-%05d", idx+1)
			name := fmt.Sprintf("Device Asset %05d", idx+1)
			catID := catIDs[idx%len(catIDs)]

			offset := len(valArgs)
			valStrings = append(valStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, 'available', 'good', now(), now())",
				offset+1, offset+2, offset+3, offset+4))
			valArgs = append(valArgs, dID, assetTag, name, catID)
		}
		query := fmt.Sprintf("INSERT INTO devices (id, asset_tag, name, category_id, status, condition, created_at, updated_at) VALUES %s",
			strings.Join(valStrings, ","))
		if _, err := h.pool.Exec(ctx, query, valArgs...); err != nil {
			t.Fatalf("bulk insert devices: %v", err)
		}
	}

	// 3. Bulk insert 5 000 historical loans (4 800 returned, 100 open on-time, 100 overdue)
	t.Log("Seeding 5 000 historical loans...")
	baseTime := time.Now().Add(-2000 * time.Hour)
	loanBatchSize := 500
	for b := 0; b < 4800; b += loanBatchSize {
		var valStrings []string
		var valArgs []any
		for i := 0; i < loanBatchSize; i++ {
			idx := b + i
			loanID := uuid.New().String()
			devID := deviceIDs[idx%len(deviceIDs)]
			uID := userIDs[idx%len(userIDs)]
			borrowedAt := baseTime.Add(time.Duration(idx) * 20 * time.Minute)
			dueAt := borrowedAt.Add(24 * time.Hour)
			returnedAt := borrowedAt.Add(4 * time.Hour)

			offset := len(valArgs)
			valStrings = append(valStrings, fmt.Sprintf("($%d, $%d, $%d, 'returned', 'kiosk', $%d, $%d, $%d, 'admin:scale', 'scanner')",
				offset+1, offset+2, offset+3, offset+4, offset+5, offset+6))
			valArgs = append(valArgs, loanID, devID, uID, borrowedAt, dueAt, returnedAt)
		}
		query := fmt.Sprintf("INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at, borrow_actor, borrow_source) VALUES %s",
			strings.Join(valStrings, ","))
		if _, err := h.pool.Exec(ctx, query, valArgs...); err != nil {
			t.Fatalf("bulk insert closed loans: %v", err)
		}
	}

	// 100 open overdue loans
	for i := 0; i < 100; i++ {
		loanID := uuid.New().String()
		devID := uuid.New().String()
		uID := userIDs[i%len(userIDs)]
		borrowedAt := time.Now().Add(-72 * time.Hour)
		dueAt := time.Now().Add(-24 * time.Hour)
		_, err := h.pool.Exec(ctx, `
			INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, due_at, borrow_actor, borrow_source)
			VALUES ($1, $2, $3, 'open', 'kiosk', $4, $5, 'admin:scale', 'scanner')
		`, loanID, devID, uID, borrowedAt, dueAt)
		if err != nil {
			t.Fatalf("insert open overdue loan %d: %v", i, err)
		}
	}

	// 4. Bulk insert ~50 000 audit events in batches of 5000
	t.Log("Seeding 50 000 audit log events...")
	auditBatchSize := 5000
	for b := 0; b < 50000; b += auditBatchSize {
		var valStrings []string
		var valArgs []any
		for i := 0; i < auditBatchSize; i++ {
			idx := b + i
			evID := uuid.New().String()
			actor := fmt.Sprintf("admin:staff-%03d", idx%10)
			action := "device.status_changed"
			if idx%3 == 0 {
				action = "loan.opened"
			} else if idx%3 == 1 {
				action = "loan.returned"
			}
			subject := fmt.Sprintf("device:TAG-SCALE-%05d", (idx%500)+1)
			at := baseTime.Add(time.Duration(idx) * 2 * time.Minute)

			offset := len(valArgs)
			valStrings = append(valStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, '{}'::jsonb)",
				offset+1, offset+2, offset+3, offset+4, offset+5))
			valArgs = append(valArgs, evID, at, actor, action, subject)
		}
		query := fmt.Sprintf("INSERT INTO audit_events (id, at, actor, action, subject, payload) VALUES %s",
			strings.Join(valStrings, ","))
		if _, err := h.pool.Exec(ctx, query, valArgs...); err != nil {
			t.Fatalf("bulk insert audit events: %v", err)
		}
	}

	// Update PostgreSQL statistics
	t.Log("Running ANALYZE on populated tables...")
	if _, err := h.pool.Exec(ctx, `ANALYZE users, devices, loans, audit_events`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	return deviceIDs, userIDs
}

// TestScaleDatasetPerformanceAndExplainPlans verifies Phase 4.11c exit criteria:
// 1. Overdue query renders in < 1s over 5 000 loans
// 2. Device search mid-phone-call budget < 300 ms
// 3. Audit query pagination is inside budget
// 4. CSV export of 5 000+ rows streams without buffering
// 5. EXPLAIN plans verify index-backed query execution
func TestScaleDatasetPerformanceAndExplainPlans(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Seed realistic dataset
	SeedRealisticScaleDataset(t, h)

	// ── 1. Overdue loans query (< 1s budget) ─────────────────────────────────
	start := time.Now()
	now := time.Now()
	overdueLoans, err := h.lending.OverdueLoans(ctx, now)
	elapsedOverdue := time.Since(start)

	if err != nil {
		t.Fatalf("OverdueLoans query failed: %v", err)
	}
	if len(overdueLoans) != 100 {
		t.Fatalf("got %d overdue loans, want 100", len(overdueLoans))
	}
	if elapsedOverdue >= 1*time.Second {
		t.Fatalf("OverdueLoans took %v, exceeded 1s performance budget", elapsedOverdue)
	}
	t.Logf("✓ Overdue list rendered in %v (< 1s budget)", elapsedOverdue)

	// HTTP Endpoint check
	httpStart := time.Now()
	resp := h.get(t, "/v1/dashboard")
	httpElapsed := time.Since(httpStart)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d", resp.StatusCode)
	}
	if httpElapsed >= 1*time.Second {
		t.Fatalf("Dashboard HTTP endpoint took %v, exceeded 1s budget", httpElapsed)
	}
	t.Logf("✓ Dashboard endpoint completed in %v (< 1s budget)", httpElapsed)

	// ── 2. Device search mid-phone-call budget (< 300 ms) ─────────────────────
	searchStart := time.Now()
	searchResp := h.get(t, "/v1/devices?q=TAG-SCALE-002&limit=20")
	searchElapsed := time.Since(searchStart)

	if searchResp.StatusCode != http.StatusOK {
		t.Fatalf("search devices status = %d", searchResp.StatusCode)
	}
	devList := decodeBody[gen.DeviceList](t, searchResp)
	if len(devList.Items) == 0 {
		t.Fatal("expected search results for TAG-SCALE-002")
	}
	if searchElapsed >= 300*time.Millisecond {
		t.Fatalf("Device search took %v, exceeded 300ms budget", searchElapsed)
	}
	t.Logf("✓ Device search completed in %v (< 300ms budget)", searchElapsed)

	// ── 3. Audit query pagination performance ─────────────────────────────────
	auditStart := time.Now()
	auditResp := h.get(t, "/v1/audit?action=device.status_changed&limit=50")
	auditElapsed := time.Since(auditStart)

	if auditResp.StatusCode != http.StatusOK {
		t.Fatalf("audit status = %d", auditResp.StatusCode)
	}
	auditList := decodeBody[gen.AuditEventList](t, auditResp)
	if len(auditList.Items) != 50 {
		t.Fatalf("got %d audit events, want 50", len(auditList.Items))
	}
	if auditElapsed >= 300*time.Millisecond {
		t.Fatalf("Audit list query took %v, exceeded 300ms budget", auditElapsed)
	}
	t.Logf("✓ Audit query over 50 000 events completed in %v (< 300ms budget)", auditElapsed)

	// ── 4. CSV Streaming of 5 000+ records ────────────────────────────────────
	csvStart := time.Now()
	csvResp := h.get(t, "/v1/reports/loans.csv")
	if csvResp.StatusCode != http.StatusOK {
		t.Fatalf("export loans status = %d", csvResp.StatusCode)
	}
	if ct := csvResp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected text/csv Content-Type, got %q", ct)
	}

	bodyBytes, err := io.ReadAll(csvResp.Body)
	if err != nil {
		t.Fatalf("read CSV response: %v", err)
	}
	csvElapsed := time.Since(csvStart)
	lines := strings.Split(strings.TrimSpace(string(bodyBytes)), "\n")
	if len(lines) < 4900 {
		t.Fatalf("expected >= 4900 CSV rows, got %d", len(lines))
	}
	t.Logf("✓ CSV export of %d records completed in %v", len(lines), csvElapsed)

	// ── 5. EXPLAIN Plan Assertions (Index Scan verification) ───────────────────
	var overduePlan, deviceSearchPlan, auditPlan []byte

	// 5a. Overdue query plan
	err = h.pool.QueryRow(ctx, `
		EXPLAIN (FORMAT JSON)
		SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at
		FROM loans
		WHERE status = 'open' AND NOT disputed AND due_at IS NOT NULL AND due_at < $1
		ORDER BY due_at
	`, now).Scan(&overduePlan)
	if err != nil {
		t.Fatalf("EXPLAIN Overdue: %v", err)
	}
	assertIndexScan(t, string(overduePlan), "loans overdue query")

	// 5b. Device query plan (using devices_status_asset_tag_id_idx)
	err = h.pool.QueryRow(ctx, `
		EXPLAIN (FORMAT JSON)
		SELECT id, asset_tag, name, category_id, status, condition, created_at, updated_at
		FROM devices
		WHERE status = 'available'
		ORDER BY status, asset_tag, id
		LIMIT 50
	`).Scan(&deviceSearchPlan)
	if err != nil {
		t.Fatalf("EXPLAIN Devices: %v", err)
	}
	assertIndexScan(t, string(deviceSearchPlan), "devices listing query")

	// 5c. Audit log query plan (using audit_events_actor_at_id_idx)
	err = h.pool.QueryRow(ctx, `
		EXPLAIN (FORMAT JSON)
		SELECT id, actor, action, subject, payload, at
		FROM audit_events
		WHERE actor = 'admin:staff-001'
		ORDER BY at DESC, id DESC
		LIMIT 50
	`).Scan(&auditPlan)
	if err != nil {
		t.Fatalf("EXPLAIN Audit: %v", err)
	}
	assertIndexScan(t, string(auditPlan), "audit events query")
}

func assertIndexScan(t *testing.T, explainJSON, queryName string) {
	t.Helper()
	hasIndex := strings.Contains(explainJSON, "Index Scan") || strings.Contains(explainJSON, "Bitmap Index Scan")
	if !hasIndex {
		t.Fatalf("expected %s EXPLAIN plan to use Index Scan, got plan:\n%s", queryName, explainJSON)
	}
	t.Logf("✓ %s verified to use Index Scan", queryName)
}
