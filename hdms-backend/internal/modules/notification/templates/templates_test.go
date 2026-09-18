package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenFileRenderingPerTemplate(t *testing.T) {
	// 1. Overdue reminder
	overdueData := OverdueReminderData{
		BorrowerName:   "Nurse Alice Smith",
		DeviceName:     "iPad Air 5th Gen",
		AssetTag:       "HD-DEV-001042",
		BorrowedAt:     "2026-09-10 09:00 JST",
		DueAt:          "2026-09-15 17:00 JST",
		OverdueDays:    3,
		ReturnLocation: "Ward 4 Equipment Counter (Building B, 2F)",
		HumanContact:   "Equipment Counter, Ext. 4100 (counter@hospital.local)",
	}
	sub, txt, ht, err := RenderOverdueReminder(overdueData)
	if err != nil {
		t.Fatalf("RenderOverdueReminder: %v", err)
	}
	if !strings.Contains(sub, "iPad Air 5th Gen") || !strings.Contains(sub, "HD-DEV-001042") {
		t.Errorf("unexpected subject: %q", sub)
	}
	assertGolden(t, "overdue_reminder.txt.golden", txt)
	assertGolden(t, "overdue_reminder.html.golden", ht)

	// 2. Weekly digest
	digestData := WeeklyDigestData{
		GeneratedAt:  "2026-09-18 08:00 JST",
		TotalOverdue: 2,
		Items: []OverdueDigestItem{
			{
				AssetTag:     "HD-DEV-001042",
				DeviceName:   "iPad Air 5th Gen",
				BorrowerName: "Nurse Alice Smith",
				DueAt:        "2026-09-15 17:00 JST",
				OverdueDays:  3,
			},
			{
				AssetTag:     "HD-DEV-002088",
				DeviceName:   "Pulse Oximeter Pro",
				BorrowerName: "Dr. Ken Sato",
				DueAt:        "2026-09-12 12:00 JST",
				OverdueDays:  6,
			},
		},
		HumanContact: "Equipment Management Office, Ext. 4100 (counter@hospital.local)",
	}
	sub, txt, ht, err = RenderWeeklyDigest(digestData)
	if err != nil {
		t.Fatalf("RenderWeeklyDigest: %v", err)
	}
	if !strings.Contains(sub, "2 item(s)") {
		t.Errorf("unexpected digest subject: %q", sub)
	}
	assertGolden(t, "weekly_digest.txt.golden", txt)
	assertGolden(t, "weekly_digest.html.golden", ht)

	// 3. Return confirmation
	returnData := ReturnConfirmationData{
		BorrowerName: "Nurse Alice Smith",
		DeviceName:   "iPad Air 5th Gen",
		AssetTag:     "HD-DEV-001042",
		ReturnedAt:   "2026-09-18 14:30 JST",
		HumanContact: "Equipment Counter, Ext. 4100 (counter@hospital.local)",
	}
	sub, txt, ht, err = RenderReturnConfirmation(returnData)
	if err != nil {
		t.Fatalf("RenderReturnConfirmation: %v", err)
	}
	if !strings.Contains(sub, "iPad Air 5th Gen") {
		t.Errorf("unexpected return subject: %q", sub)
	}
	assertGolden(t, "return_confirmation.txt.golden", txt)
	assertGolden(t, "return_confirmation.html.golden", ht)
}

func TestRenderedMessageContainsNoTokenAndNoOtherBorrowersData(t *testing.T) {
	// Assert that template output for borrower A never contains credentials, tokens,
	// or another borrower's identifiable data.
	data := OverdueReminderData{
		BorrowerName:   "Alice Smith",
		DeviceName:     "Telemetry Monitor",
		AssetTag:       "HD-DEV-005511",
		BorrowedAt:     "2026-09-14 10:00 JST",
		DueAt:          "2026-09-16 10:00 JST",
		OverdueDays:    2,
		ReturnLocation: "Biomedical Storage Desk",
		HumanContact:   "Equipment Counter, Ext. 4100 (counter@hospital.local)",
	}

	sub, txt, ht, err := RenderOverdueReminder(data)
	if err != nil {
		t.Fatalf("RenderOverdueReminder: %v", err)
	}

	combined := sub + "\n" + txt + "\n" + ht

	// Forbidden tokens/credentials
	forbiddenTokens := []string{
		"HD-U-",
		"HD-D-",
		"bearer",
		"session_token",
		"totp",
		"password",
		"secret",
	}
	for _, tok := range forbiddenTokens {
		if strings.Contains(strings.ToLower(combined), tok) {
			t.Errorf("rendered message contains forbidden token/credential string %q", tok)
		}
	}

	// Must contain borrower A
	if !strings.Contains(combined, "Alice Smith") {
		t.Errorf("rendered message should contain borrower A's name 'Alice Smith'")
	}

	// Must NOT contain other borrower data
	otherBorrowerData := []string{"Bob Jones", "Carol White", "FX-U-009999", "bob@hospital.local"}
	for _, other := range otherBorrowerData {
		if strings.Contains(combined, other) {
			t.Errorf("rendered message leaked another borrower's data: %q", other)
		}
	}
}

func assertGolden(t *testing.T, goldenFileName, actual string) {
	t.Helper()
	path := filepath.Join("testdata", goldenFileName)
	expectedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenFileName, err)
	}
	expected := string(expectedBytes)
	if actual != expected {
		t.Errorf("golden file mismatch for %s:\n--- EXPECTED ---\n%s\n--- ACTUAL ---\n%s", goldenFileName, expected, actual)
	}
}
