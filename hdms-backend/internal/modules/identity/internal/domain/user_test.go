package domain_test

import (
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/identity/internal/domain"
)

func TestValidateEmployeeNo(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  HH-2407  ", "HH-2407", nil},
		{"plain alnum", "A1234", "A1234", nil},
		{"empty", "", "", domain.ErrEmployeeNoRequired},
		{"whitespace only", "   ", "", domain.ErrEmployeeNoRequired},
		{"starts with hyphen", "-HH2407", "", domain.ErrEmployeeNoInvalid},
		{"contains space", "HH 2407", "", domain.ErrEmployeeNoInvalid},
		{"too long", "A123456789012345678901234567890123", "", domain.ErrEmployeeNoInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.ValidateEmployeeNo(tc.raw)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("got = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateFullName(t *testing.T) {
	if _, err := domain.ValidateFullName("   "); !errors.Is(err, domain.ErrFullNameRequired) {
		t.Fatalf("want ErrFullNameRequired, got %v", err)
	}
	got, err := domain.ValidateFullName("  Dr. A. Sharma  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Dr. A. Sharma" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateRegisteredBy(t *testing.T) {
	valid := []string{"admin:0192a1", "import", "import:0192a1"}
	for _, v := range valid {
		if _, err := domain.ValidateRegisteredBy(v); err != nil {
			t.Errorf("ValidateRegisteredBy(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "kiosk:0192a1", "kiosk:", "someone", "Admin:1"}
	for _, v := range invalid {
		if _, err := domain.ValidateRegisteredBy(v); !errors.Is(err, domain.ErrRegisteredByInvalid) {
			t.Errorf("ValidateRegisteredBy(%q) = %v, want ErrRegisteredByInvalid", v, err)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	cases := []struct {
		from, to domain.UserStatus
		wantErr  bool
	}{
		{domain.StatusActive, domain.StatusActive, false},
		{domain.StatusActive, domain.StatusSuspended, false},
		{domain.StatusActive, domain.StatusArchived, false},
		{domain.StatusSuspended, domain.StatusActive, false},
		{domain.StatusSuspended, domain.StatusArchived, false},
		{domain.StatusArchived, domain.StatusActive, true},
		{domain.StatusArchived, domain.StatusSuspended, true},
		{domain.StatusArchived, domain.StatusArchived, false},
	}
	for _, tc := range cases {
		err := domain.ValidateTransition(tc.from, tc.to)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateTransition(%s, %s) error = %v, wantErr %v", tc.from, tc.to, err, tc.wantErr)
		}
	}
}
