//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

// TestKeysetPagingStableUnderConcurrentWrites proves that keyset cursor pagination
// does not skip or duplicate rows when new rows are concurrently inserted between page fetches.
func TestKeysetPagingStableUnderConcurrentWrites(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// 1. Create a category
	catResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{"name": "Laptops"})
	cat := decodeBody[gen.Category](t, catResp)

	// 2. Create initial 10 devices with sequential timestamps
	baseTime := time.Now().Add(-10 * time.Minute)
	initialIDs := make([]string, 10)
	for i := 0; i < 10; i++ {
		tag := fmt.Sprintf("CONCUR-%02d", i)
		name := fmt.Sprintf("Device %02d", i)
		id := uuid.New().String()
		initialIDs[i] = id
		ts := baseTime.Add(time.Duration(i) * time.Minute)

		_, err := h.pool.Exec(ctx, `
			INSERT INTO devices (id, asset_tag, name, category_id, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'available', $5, $5)
		`, id, tag, name, cat.Id, ts)
		if err != nil {
			t.Fatalf("insert device %d: %v", i, err)
		}
	}

	// 3. Fetch Page 1 (limit 4, sorted created_at desc)
	resp1 := h.get(t, "/v1/devices?limit=4")
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("page 1 status = %d", resp1.StatusCode)
	}
	page1 := decodeBody[gen.DeviceList](t, resp1)
	if len(page1.Items) != 4 {
		t.Fatalf("page 1 len = %d, want 4", len(page1.Items))
	}
	if page1.NextCursor == nil || *page1.NextCursor == "" {
		t.Fatal("expected non-empty nextCursor on page 1")
	}

	seenIDs := make(map[string]bool)
	for _, item := range page1.Items {
		seenIDs[item.Id] = true
	}

	// 4. Concurrently insert 5 NEW devices with current timestamp (newer than all previous)
	for i := 10; i < 15; i++ {
		tag := fmt.Sprintf("CONCUR-NEW-%02d", i)
		name := fmt.Sprintf("New Device %02d", i)
		id := uuid.New().String()
		_, err := h.pool.Exec(ctx, `
			INSERT INTO devices (id, asset_tag, name, category_id, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'available', now(), now())
		`, id, tag, name, cat.Id)
		if err != nil {
			t.Fatalf("insert new concurrent device: %v", err)
		}
	}

	// 5. Fetch Page 2 using cursor from Page 1 (limit 4)
	resp2 := h.get(t, "/v1/devices?limit=4&cursor="+*page1.NextCursor)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("page 2 status = %d", resp2.StatusCode)
	}
	page2 := decodeBody[gen.DeviceList](t, resp2)
	if len(page2.Items) != 4 {
		t.Fatalf("page 2 len = %d, want 4", len(page2.Items))
	}

	// Assert NO duplicates between Page 1 and Page 2
	for _, item := range page2.Items {
		if seenIDs[item.Id] {
			t.Fatalf("duplicate item ID %s found on page 2", item.Id)
		}
		seenIDs[item.Id] = true
	}

	// 6. Fetch Page 3 (remaining 2 items from initial 10)
	if page2.NextCursor == nil || *page2.NextCursor == "" {
		t.Fatal("expected non-empty nextCursor on page 2")
	}
	resp3 := h.get(t, "/v1/devices?limit=4&cursor="+*page2.NextCursor)
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("page 3 status = %d", resp3.StatusCode)
	}
	page3 := decodeBody[gen.DeviceList](t, resp3)
	if len(page3.Items) != 2 {
		t.Fatalf("page 3 len = %d, want 2 (remaining of initial 10)", len(page3.Items))
	}
	for _, item := range page3.Items {
		if seenIDs[item.Id] {
			t.Fatalf("duplicate item ID %s found on page 3", item.Id)
		}
		seenIDs[item.Id] = true
	}

	// Assert that all 10 initial items were visited without any dropped/skipped rows
	for _, initialID := range initialIDs {
		if !seenIDs[initialID] {
			t.Fatalf("initial item ID %s was skipped during pagination", initialID)
		}
	}
}

// TestKeysetSortTiebreakers proves that rows with identical sort keys are deterministically
// paginated via the ID tiebreaker without skipping or repeating any rows at page boundaries.
func TestKeysetSortTiebreakers(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	catResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{"name": "Tablets"})
	cat := decodeBody[gen.Category](t, catResp)

	// Create 10 devices sharing the EXACT same created_at timestamp
	sharedTimestamp := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	createdIDs := make(map[string]bool)
	for i := 0; i < 10; i++ {
		tag := fmt.Sprintf("TIE-%02d", i)
		name := fmt.Sprintf("Tablet %02d", i)
		id := uuid.New().String()
		createdIDs[id] = true

		_, err := h.pool.Exec(ctx, `
			INSERT INTO devices (id, asset_tag, name, category_id, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'available', $5, $5)
		`, id, tag, name, cat.Id, sharedTimestamp)
		if err != nil {
			t.Fatalf("insert tie device %d: %v", i, err)
		}
	}

	// Paginate through with limit=3
	var cursor string
	retrievedIDs := make(map[string]bool)
	totalPages := 0

	for {
		url := "/v1/devices?limit=3"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		resp := h.get(t, url)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d at page %d", resp.StatusCode, totalPages)
		}
		list := decodeBody[gen.DeviceList](t, resp)
		totalPages++

		for _, item := range list.Items {
			if retrievedIDs[item.Id] {
				t.Fatalf("item %s was repeated at page %d", item.Id, totalPages)
			}
			retrievedIDs[item.Id] = true
		}

		if list.NextCursor == nil || *list.NextCursor == "" || len(list.Items) < 3 {
			break
		}
		cursor = *list.NextCursor
	}

	// Verify all 10 items were visited exactly once
	if len(retrievedIDs) != 10 {
		t.Fatalf("total retrieved items = %d, want 10", len(retrievedIDs))
	}
	for id := range createdIDs {
		if !retrievedIDs[id] {
			t.Fatalf("item %s was dropped at tie boundary", id)
		}
	}
}

// TestOverdue5000LoansQueryBenchmarkAndPlan verifies:
// 1. A dataset of 5 000 loans is seeded into Postgres.
// 2. The overdue query finishes in < 1 second.
// 3. EXPLAIN plan verifies that an Index Scan on covering index is used (not sequential scan).
func TestOverdue5000LoansQueryBenchmarkAndPlan(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Seed 1 department, 1 user, 1 category, 1 device
	dept, err := h.identity.GetOrCreateDepartment(ctx, "Emergency")
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	user, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "E5000-01",
		FullName:     "Overdue Benchmark User",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	loanPeriod := 24 * time.Hour
	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{
		Name:              "OverdueCat",
		DefaultLoanPeriod: &loanPeriod,
		RequiresApproval:  false,
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	device, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "DEV-OVERDUE-01",
		Name:       "Benchmark Device",
		CategoryID: cat.ID,
	}, "admin:test")
	if err != nil {
		t.Fatalf("create device: %v", err)
	}

	// Batch insert 5 000 historical returned loans and 100 open overdue loans
	t.Log("Seeding 5 000 loans for benchmark and plan verification...")
	baseTime := time.Now().Add(-1000 * time.Hour)

	// Insert 4 900 closed loans using direct bulk INSERT in batches
	batchSize := 500
	for b := 0; b < 4900; b += batchSize {
		var valueStrings []string
		var valueArgs []any
		for i := 0; i < batchSize; i++ {
			idx := b + i
			loanID := uuid.New()
			devID := uuid.New() // unique device ID per closed loan so no temporal conflict
			borrowedAt := baseTime.Add(time.Duration(idx) * 10 * time.Minute)
			returnedAt := borrowedAt.Add(2 * time.Hour)
			dueAt := borrowedAt.Add(24 * time.Hour)

			argOffset := len(valueArgs)
			valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, 'returned', 'kiosk', $%d, $%d, $%d, 'admin:bench', 'scanner')",
				argOffset+1, argOffset+2, argOffset+3, argOffset+4, argOffset+5, argOffset+6))
			valueArgs = append(valueArgs, loanID, devID, user.ID, borrowedAt, dueAt, returnedAt)
		}
		query := fmt.Sprintf("INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at, borrow_actor, borrow_source) VALUES %s",
			strings.Join(valueStrings, ","))
		if _, err := h.pool.Exec(ctx, query, valueArgs...); err != nil {
			t.Fatalf("bulk insert closed loans batch: %v", err)
		}
	}

	// Insert 100 open overdue loans
	for i := 0; i < 100; i++ {
		loanID := uuid.New()
		devID := uuid.New()
		borrowedAt := time.Now().Add(-48 * time.Hour)
		dueAt := time.Now().Add(-24 * time.Hour)
		_, err := h.pool.Exec(ctx, `
			INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, due_at, borrow_actor, borrow_source)
			VALUES ($1, $2, $3, 'open', 'kiosk', $4, $5, 'admin:bench', 'scanner')
		`, loanID, devID, user.ID, borrowedAt, dueAt)
		if err != nil {
			t.Fatalf("insert open overdue loan: %v", err)
		}
	}

	// Ensure statistics are updated
	if _, err := h.pool.Exec(ctx, `ANALYZE loans`); err != nil {
		t.Fatalf("ANALYZE loans: %v", err)
	}

	// 1. Assert Query Performance (< 1 s budget)
	start := time.Now()
	now := time.Now()
	overdueLoans, err := h.lending.OverdueLoans(ctx, now)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("OverdueLoans query: %v", err)
	}
	if len(overdueLoans) != 100 {
		t.Fatalf("got %d overdue loans, want 100", len(overdueLoans))
	}
	if elapsed >= 1*time.Second {
		t.Fatalf("OverdueLoans took %v, exceeded 1s performance budget", elapsed)
	}
	t.Logf("OverdueLoans query over 5 000 records completed in %v (< 1s budget)", elapsed)

	// 2. Assert EXPLAIN plan shows Index Scan (not Seq Scan)
	var explainPlan []byte
	err = h.pool.QueryRow(ctx, `
		EXPLAIN (FORMAT JSON)
		SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at
		FROM loans
		WHERE status = 'open' AND NOT disputed AND due_at IS NOT NULL AND due_at < $1
		ORDER BY due_at
	`, now).Scan(&explainPlan)
	if err != nil {
		t.Fatalf("EXPLAIN Overdue query: %v", err)
	}

	explainStr := string(explainPlan)
	t.Logf("EXPLAIN Output: %s", explainStr)

	// Verify that the query plan uses an index scan
	hasIndexScan := strings.Contains(explainStr, "Index Scan") || strings.Contains(explainStr, "Bitmap Index Scan")
	if !hasIndexScan {
		t.Fatalf("expected EXPLAIN plan to use Index Scan on loans, got plan: %s", explainStr)
	}

	// Suppress unused variable warning
	_ = device
}

// TestHTTPListValidationErrors tests HTTP 400 Problem responses for invalid list parameters:
// unknown sort column, unknown filter, over-max limit, and tampered cursor.
func TestHTTPListValidationErrors(t *testing.T) {
	h := newTestHarness(t)

	tests := []struct {
		name        string
		url         string
		wantStatus  int
		wantProblem string
	}{
		{
			name:        "devices unknown sort column",
			url:         "/v1/devices?sort=nonexistent_column",
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/validation-failed",
		},
		{
			name:        "devices unknown filter",
			url:         "/v1/devices?unknownFilter=123",
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/validation-failed",
		},
		{
			name:        "devices over-max limit (>200)",
			url:         "/v1/devices?limit=500",
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/validation-failed",
		},
		{
			name:        "devices tampered cursor",
			url:         "/v1/devices?cursor=invalid-corrupted-cursor!@",
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/invalid-cursor",
		},
		{
			name:        "devices stale cursor version",
			url:         "/v1/devices?cursor=" + listing.EncodeCursor(99, "created_at", "2026-08-21T00:00:00Z", uuid.New().String()),
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/invalid-cursor",
		},
		{
			name:        "users unknown sort",
			url:         "/v1/users?sort=salary",
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/validation-failed",
		},
		{
			name:        "loans unknown filter",
			url:         "/v1/loans?secretFilter=xyz",
			wantStatus:  http.StatusBadRequest,
			wantProblem: "https://hdms.hito.local/errors/validation-failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.get(t, tt.url)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			prob := decodeBody[httpx.Problem](t, resp)
			if prob.Type != tt.wantProblem {
				t.Fatalf("problem.type = %q, want %q (detail: %s)", prob.Type, tt.wantProblem, prob.Detail)
			}
		})
	}
}
