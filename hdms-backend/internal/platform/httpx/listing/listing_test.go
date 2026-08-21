package listing_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.New().String()
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Run("time cursor", func(t *testing.T) {
		encoded := listing.EncodeTimeCursor("created_at", now, id)
		if encoded == "" {
			t.Fatal("expected non-empty encoded cursor")
		}

		decoded, err := listing.DecodeCursor(encoded, "created_at")
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		if decoded.Version != listing.CurrentCursorVersion {
			t.Fatalf("version = %d, want %d", decoded.Version, listing.CurrentCursorVersion)
		}
		if decoded.SortKey != "created_at" {
			t.Fatalf("sortKey = %q, want created_at", decoded.SortKey)
		}
		if decoded.ID != id {
			t.Fatalf("id = %q, want %q", decoded.ID, id)
		}

		parsedTime, err := decoded.TimeVal()
		if err != nil {
			t.Fatalf("timeVal: %v", err)
		}
		if !parsedTime.Equal(now) {
			t.Fatalf("parsedTime = %v, want %v", parsedTime, now)
		}

		parsedUUID, err := decoded.UUIDVal()
		if err != nil {
			t.Fatalf("uuidVal: %v", err)
		}
		if parsedUUID.String() != id {
			t.Fatalf("parsedUUID = %s, want %s", parsedUUID, id)
		}
	})

	t.Run("string cursor", func(t *testing.T) {
		encoded := listing.EncodeStringCursor("asset_tag", "LAPTOP-001", id)
		decoded, err := listing.DecodeCursor(encoded, "asset_tag")
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		if decoded.SortVal != "LAPTOP-001" {
			t.Fatalf("sortVal = %q, want LAPTOP-001", decoded.SortVal)
		}
	})

	t.Run("empty raw cursor", func(t *testing.T) {
		decoded, err := listing.DecodeCursor("", "created_at")
		if err != nil {
			t.Fatalf("expected nil error for empty string, got %v", err)
		}
		if decoded != nil {
			t.Fatalf("expected nil cursor for empty string, got %v", decoded)
		}
	})
}

func TestCursorRejections(t *testing.T) {
	id := uuid.New().String()

	t.Run("stale version byte rejected", func(t *testing.T) {
		// Version 2 instead of CurrentCursorVersion (1)
		encoded := listing.EncodeCursor(2, "created_at", "2026-08-21T00:00:00Z", id)
		_, err := listing.DecodeCursor(encoded, "created_at")
		if !errors.Is(err, listing.ErrStaleCursor) {
			t.Fatalf("expected ErrStaleCursor, got %v", err)
		}
	})

	t.Run("tampered base64 rejected", func(t *testing.T) {
		_, err := listing.DecodeCursor("not-valid-base64!!@#$", "created_at")
		if !errors.Is(err, listing.ErrInvalidCursor) {
			t.Fatalf("expected ErrInvalidCursor, got %v", err)
		}
	})

	t.Run("payload too short", func(t *testing.T) {
		short := base64.RawURLEncoding.EncodeToString([]byte{1})
		_, err := listing.DecodeCursor(short, "created_at")
		if !errors.Is(err, listing.ErrInvalidCursor) {
			t.Fatalf("expected ErrInvalidCursor, got %v", err)
		}
	})

	t.Run("corrupted json payload", func(t *testing.T) {
		corrupted := base64.RawURLEncoding.EncodeToString([]byte{1, '{', 'b', 'a', 'd'})
		_, err := listing.DecodeCursor(corrupted, "created_at")
		if !errors.Is(err, listing.ErrInvalidCursor) {
			t.Fatalf("expected ErrInvalidCursor, got %v", err)
		}
	})

	t.Run("missing id tiebreaker", func(t *testing.T) {
		encoded := listing.EncodeCursor(listing.CurrentCursorVersion, "created_at", "2026-08-21T00:00:00Z", "")
		_, err := listing.DecodeCursor(encoded, "created_at")
		if !errors.Is(err, listing.ErrInvalidCursor) {
			t.Fatalf("expected ErrInvalidCursor for empty id, got %v", err)
		}
	})

	t.Run("mismatched sort key", func(t *testing.T) {
		encoded := listing.EncodeCursor(listing.CurrentCursorVersion, "created_at", "2026-08-21T00:00:00Z", id)
		_, err := listing.DecodeCursor(encoded, "asset_tag")
		if !errors.Is(err, listing.ErrInvalidCursor) {
			t.Fatalf("expected ErrInvalidCursor for mismatched sort key, got %v", err)
		}
	})
}

func TestParseListingParams(t *testing.T) {
	spec := listing.DevicesSpec

	t.Run("default params when empty query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/devices", nil)
		p, err := listing.Parse(req, spec)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if p.Limit != listing.DefaultLimit {
			t.Fatalf("limit = %d, want %d", p.Limit, listing.DefaultLimit)
		}
		if p.Sort != spec.DefaultSort {
			t.Fatalf("sort = %q, want %q", p.Sort, spec.DefaultSort)
		}
		if p.Order != listing.OrderDesc {
			t.Fatalf("order = %q, want desc", p.Order)
		}
		if p.Cursor != nil {
			t.Fatalf("expected nil cursor, got %v", p.Cursor)
		}
	})

	t.Run("explicit valid params", func(t *testing.T) {
		id := uuid.New().String()
		cursorStr := listing.EncodeStringCursor("asset_tag", "LAPTOP-01", id)

		req := httptest.NewRequest(http.MethodGet, "/v1/devices?limit=25&sort=asset_tag&order=asc&status=available&q=Dell&cursor="+cursorStr, nil)
		p, err := listing.Parse(req, spec)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if p.Limit != 25 {
			t.Fatalf("limit = %d, want 25", p.Limit)
		}
		if p.Sort != "asset_tag" {
			t.Fatalf("sort = %q, want asset_tag", p.Sort)
		}
		if p.Order != listing.OrderAsc {
			t.Fatalf("order = %q, want asc", p.Order)
		}
		if p.Filter("status") != "available" {
			t.Fatalf("status filter = %q, want available", p.Filter("status"))
		}
		if p.Filter("q") != "Dell" {
			t.Fatalf("q filter = %q, want Dell", p.Filter("q"))
		}
		if p.Cursor == nil || p.Cursor.ID != id {
			t.Fatalf("cursor id = %v, want %s", p.Cursor, id)
		}
	})

	t.Run("unknown sort column rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/devices?sort=malicious_sql_column", nil)
		_, err := listing.Parse(req, spec)
		if !errors.Is(err, listing.ErrInvalidSort) {
			t.Fatalf("expected ErrInvalidSort, got %v", err)
		}
	})

	t.Run("invalid order rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/devices?order=sideways", nil)
		_, err := listing.Parse(req, spec)
		if !errors.Is(err, listing.ErrInvalidOrder) {
			t.Fatalf("expected ErrInvalidOrder, got %v", err)
		}
	})

	t.Run("over-max limit rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/devices?limit=201", nil)
		_, err := listing.Parse(req, spec)
		if !errors.Is(err, listing.ErrInvalidLimit) {
			t.Fatalf("expected ErrInvalidLimit, got %v", err)
		}
	})

	t.Run("zero and negative limit rejected", func(t *testing.T) {
		for _, invalid := range []string{"0", "-1", "abc"} {
			req := httptest.NewRequest(http.MethodGet, "/v1/devices?limit="+invalid, nil)
			_, err := listing.Parse(req, spec)
			if !errors.Is(err, listing.ErrInvalidLimit) {
				t.Fatalf("limit=%s: expected ErrInvalidLimit, got %v", invalid, err)
			}
		}
	})

	t.Run("unknown filter rejected when strict", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/devices?unknownParam=xyz", nil)
		_, err := listing.Parse(req, spec)
		if !errors.Is(err, listing.ErrUnknownFilter) {
			t.Fatalf("expected ErrUnknownFilter, got %v", err)
		}
	})
}

func TestResourceSpecsDefinitions(t *testing.T) {
	specs := []listing.ResourceSpec{
		listing.DevicesSpec,
		listing.UsersSpec,
		listing.LoansSpec,
		listing.AuditSpec,
		listing.CategoriesSpec,
		listing.AdminsSpec,
		listing.KiosksSpec,
	}

	for _, s := range specs {
		t.Run(s.Name, func(t *testing.T) {
			if s.DefaultSort == "" {
				t.Errorf("spec %s has empty DefaultSort", s.Name)
			}
			if !s.SortColumns[s.DefaultSort] {
				t.Errorf("spec %s DefaultSort %q is not in SortColumns whitelist", s.Name, s.DefaultSort)
			}
			if s.MaxLimit != 200 {
				t.Errorf("spec %s MaxLimit = %d, want 200", s.Name, s.MaxLimit)
			}
			if s.DefaultLimit != 50 {
				t.Errorf("spec %s DefaultLimit = %d, want 50", s.Name, s.DefaultLimit)
			}
		})
	}
}
