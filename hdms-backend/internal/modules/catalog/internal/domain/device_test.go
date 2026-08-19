package domain_test

import (
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/catalog/internal/domain"
)

func TestValidateAssetTag(t *testing.T) {
	if _, err := domain.ValidateAssetTag("   "); !errors.Is(err, domain.ErrAssetTagRequired) {
		t.Fatalf("want ErrAssetTagRequired, got %v", err)
	}
	got, err := domain.ValidateAssetTag("  LAPTOP-07  ")
	if err != nil || got != "LAPTOP-07" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestValidateDeviceName(t *testing.T) {
	if _, err := domain.ValidateDeviceName(""); !errors.Is(err, domain.ErrNameRequired) {
		t.Fatalf("want ErrNameRequired, got %v", err)
	}
}

func TestValidateTransition(t *testing.T) {
	cases := []struct {
		from, to domain.DeviceStatus
		wantErr  bool
	}{
		{domain.StatusAvailable, domain.StatusAvailable, false},
		{domain.StatusAvailable, domain.StatusOnLoan, false},
		{domain.StatusAvailable, domain.StatusMaintenance, false},
		{domain.StatusAvailable, domain.StatusLost, false},
		{domain.StatusAvailable, domain.StatusRetired, false},
		{domain.StatusOnLoan, domain.StatusAvailable, false},
		{domain.StatusOnLoan, domain.StatusLost, false},
		{domain.StatusOnLoan, domain.StatusMaintenance, true},
		{domain.StatusOnLoan, domain.StatusRetired, true},
		{domain.StatusMaintenance, domain.StatusAvailable, false},
		{domain.StatusMaintenance, domain.StatusRetired, false},
		{domain.StatusMaintenance, domain.StatusOnLoan, true},
		{domain.StatusLost, domain.StatusAvailable, false},
		{domain.StatusLost, domain.StatusRetired, false},
		{domain.StatusLost, domain.StatusOnLoan, true},
		// retired is terminal (docs/03-domain-model.md's device lifecycle).
		{domain.StatusRetired, domain.StatusAvailable, true},
		{domain.StatusRetired, domain.StatusOnLoan, true},
		{domain.StatusRetired, domain.StatusRetired, false},
	}
	for _, tc := range cases {
		err := domain.ValidateTransition(tc.from, tc.to)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateTransition(%s, %s) error = %v, wantErr %v", tc.from, tc.to, err, tc.wantErr)
		}
	}
}
