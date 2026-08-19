package domain_test

import (
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/credentials/internal/domain"
)

func TestValidateReason(t *testing.T) {
	if _, err := domain.ValidateReason("   "); !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("want ErrReasonRequired, got %v", err)
	}
	got, err := domain.ValidateReason("  card lost on the bus  ")
	if err != nil || got != "card lost on the bus" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNormalizeUID(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		order   domain.ByteOrder
		want    string
		wantErr error
	}{
		{"strips colons and uppercases", "04:a2:1c:9e", domain.ByteOrderAsIs, "04A21C9E", nil},
		{"strips hyphens", "04-a2-1c-9e", domain.ByteOrderAsIs, "04A21C9E", nil},
		{"strips spaces", "04 a2 1c 9e", domain.ByteOrderAsIs, "04A21C9E", nil},
		{"already normalized", "04A21C9E", domain.ByteOrderAsIs, "04A21C9E", nil},
		{"reversed byte order", "04:a2:1c:9e", domain.ByteOrderReversed, "9E1CA204", nil},
		{"empty", "", domain.ByteOrderAsIs, "", domain.ErrUIDEmpty},
		{"only separators", "::--  ", domain.ByteOrderAsIs, "", domain.ErrUIDEmpty},
		{"odd length", "04A21C9", domain.ByteOrderAsIs, "", domain.ErrUIDOddLength},
		{"non-hex", "04G21C9E", domain.ByteOrderAsIs, "", domain.ErrUIDInvalidHex},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.NormalizeUID(tc.raw, tc.order)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("got = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeUIDStableAndDistinguishesCards(t *testing.T) {
	// Mirrors the real-world test docs/05 calls for before committing to
	// RFID: the same card read repeatedly normalizes identically, and
	// different cards normalize differently.
	a1, err := domain.NormalizeUID("04:A2:1C:9E", domain.ByteOrderAsIs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a2, err := domain.NormalizeUID("04a21c9e", domain.ByteOrderAsIs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a1 != a2 {
		t.Fatalf("same UID normalized differently: %q vs %q", a1, a2)
	}

	b, err := domain.NormalizeUID("04:A2:1C:9F", domain.ByteOrderAsIs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a1 == b {
		t.Fatalf("different UIDs normalized identically: %q", a1)
	}
}
