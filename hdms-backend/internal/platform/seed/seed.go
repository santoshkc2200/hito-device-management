package seed

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// SeedRealisticScaleDataset populates a realistic hospital scale dataset:
// ~800 staff users, ~500 devices, 5 000 historical loans, and ~50 000 audit events.
// Used for performance testing, staging drills, and deploy soak rehearsals.
func SeedRealisticScaleDataset(ctx context.Context, pool *db.Pool) error {
	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)

	// 1. Departments
	depts := []string{"Emergency", "Surgery", "ICU", "Cardiology", "Pediatrics", "Radiology", "Neurology", "Oncology"}
	deptIDs := make([]string, len(depts))
	for i, d := range depts {
		dept, err := identitySvc.GetOrCreateDepartment(ctx, d)
		if err != nil {
			return fmt.Errorf("create department %s: %w", d, err)
		}
		deptIDs[i] = dept.ID
	}

	// 2. Categories
	catNames := []string{"Infusion Pumps", "Ultrasound Scanners", "ECG Monitors", "Defibrillators", "Tablets", "Laptops", "Surgical Drills"}
	catIDs := make([]string, len(catNames))
	for i, c := range catNames {
		cat, err := catalogSvc.GetOrCreateCategory(ctx, c)
		if err != nil {
			return fmt.Errorf("create or get category %s: %w", c, err)
		}
		catIDs[i] = cat.ID
	}

	// 3. Bulk insert ~800 Users
	userIDs := make([]string, 800)
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
		query := fmt.Sprintf("INSERT INTO users (id, employee_no, full_name, department_id, status, registered_by, registered_at, updated_at) VALUES %s ON CONFLICT (lower(employee_no)) WHERE status <> 'archived' DO NOTHING",
			strings.Join(valStrings, ","))
		if _, err := pool.Exec(ctx, query, valArgs...); err != nil {
			return fmt.Errorf("bulk insert users: %w", err)
		}
	}

	// 4. Bulk insert ~500 Devices
	deviceIDs := make([]string, 500)
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
		query := fmt.Sprintf("INSERT INTO devices (id, asset_tag, name, category_id, status, condition, created_at, updated_at) VALUES %s ON CONFLICT (upper(asset_tag)) WHERE status <> 'retired' DO NOTHING",
			strings.Join(valStrings, ","))
		if _, err := pool.Exec(ctx, query, valArgs...); err != nil {
			return fmt.Errorf("bulk insert devices: %w", err)
		}
	}

	// 5. Bulk insert 5 000 historical loans (4 800 returned, 100 open on-time, 100 overdue)
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
		if _, err := pool.Exec(ctx, query, valArgs...); err != nil {
			return fmt.Errorf("bulk insert closed loans: %w", err)
		}
	}

	// 100 open on-time loans
	for i := 0; i < 100; i++ {
		loanID := uuid.New().String()
		devID := deviceIDs[i%len(deviceIDs)]
		uID := userIDs[i%len(userIDs)]
		borrowedAt := time.Now().Add(-2 * time.Hour)
		dueAt := time.Now().Add(22 * time.Hour)
		_, err := pool.Exec(ctx, `
			INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, due_at, borrow_actor, borrow_source)
			VALUES ($1, $2, $3, 'open', 'kiosk', $4, $5, 'admin:scale', 'scanner')
		`, loanID, devID, uID, borrowedAt, dueAt)
		if err != nil {
			return fmt.Errorf("insert open on-time loan %d: %w", i, err)
		}
	}

	// 100 open overdue loans
	for i := 0; i < 100; i++ {
		loanID := uuid.New().String()
		devID := uuid.New().String()
		uID := userIDs[(i+100)%len(userIDs)]
		borrowedAt := time.Now().Add(-72 * time.Hour)
		dueAt := time.Now().Add(-24 * time.Hour)
		_, err := pool.Exec(ctx, `
			INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, due_at, borrow_actor, borrow_source)
			VALUES ($1, $2, $3, 'open', 'kiosk', $4, $5, 'admin:scale', 'scanner')
		`, loanID, devID, uID, borrowedAt, dueAt)
		if err != nil {
			return fmt.Errorf("insert open overdue loan %d: %w", i, err)
		}
	}

	// 6. Bulk insert ~50 000 audit events in batches of 5000
	auditBatchSize := 5000
	for b := 0; b < 50000; b += auditBatchSize {
		var valStrings []string
		var valArgs []any
		for i := 0; i < auditBatchSize; i++ {
			idx := b + i
			evID := uuid.New().String()
			actor := fmt.Sprintf("admin:staff-%03d", idx%10)
			action := "device.status_changed"
			switch idx % 3 {
			case 0:
				action = "loan.opened"
			case 1:
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
		if _, err := pool.Exec(ctx, query, valArgs...); err != nil {
			return fmt.Errorf("bulk insert audit events: %w", err)
		}
	}

	// 7. Update PostgreSQL statistics
	if _, err := pool.Exec(ctx, `ANALYZE users, devices, loans, audit_events`); err != nil {
		return fmt.Errorf("ANALYZE: %w", err)
	}

	return nil
}
