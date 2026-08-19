// Package pgtypeconv bridges the plain Go types every module's API and
// domain layers use (string UUIDs, time.Time, string) and the pgtype
// wrappers sqlc generates for nullable Postgres columns. Every module's
// service converts at its store boundary rather than leaking pgtype
// outside internal/store, so identityapi, catalogapi and credentialsapi
// stay plain Go.
package pgtypeconv

import (
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/platform/ids"
	"github.com/jackc/pgx/v5/pgtype"
)

// NewUUID mints a fresh UUIDv7 (internal/platform/ids) already wrapped for
// a NOT NULL uuid column — the common case for every store's Create query.
func NewUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: ids.NewUUID(), Valid: true}
}

// UUID parses a string UUID into pgtype.UUID for a NOT NULL column.
func UUID(s string) (pgtype.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

// NullUUID parses s into pgtype.UUID for a nullable column; an empty string
// maps to SQL NULL.
func NullUUID(s string) (pgtype.UUID, error) {
	if s == "" {
		return pgtype.UUID{}, nil
	}
	return UUID(s)
}

// UUIDString renders a pgtype.UUID back to its canonical string form, or
// "" if it was NULL.
func UUIDString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

// Text wraps s as a nullable text column value; an empty string maps to
// SQL NULL, since every optional text field in this schema treats "not
// provided" and "empty" as the same thing.
func Text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// TextString unwraps a nullable text column back to a plain string, "" if
// it was NULL.
func TextString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// Timestamptz wraps t for a NOT NULL timestamptz column.
func Timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// Time unwraps a timestamptz column, returning the zero time if it was
// NULL.
func Time(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

// NullTimestamptz wraps a *time.Time for a nullable timestamptz column.
func NullTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// NullBool wraps a *bool for a nullable boolean filter parameter; nil maps
// to SQL NULL, which the sqlc.narg filters read as "no constraint".
func NullBool(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

// TimePtr unwraps a nullable timestamptz column to *time.Time, nil if it
// was NULL.
func TimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// Interval wraps a *time.Duration for a nullable interval column
// (device_categories.default_loan_period), storing it purely as
// microseconds. Every interval this codebase writes comes from here, so
// there is no days/months component to lose on the round trip back.
func Interval(d *time.Duration) pgtype.Interval {
	if d == nil {
		return pgtype.Interval{}
	}
	return pgtype.Interval{Microseconds: int64(*d / time.Microsecond), Valid: true}
}

// DurationPtr unwraps a nullable interval column to *time.Duration, nil if
// it was NULL. Days and months are included (as 24h and 30-day
// approximations respectively) for robustness against an interval entered
// directly in SQL, even though this codebase never writes one that way.
func DurationPtr(i pgtype.Interval) *time.Duration {
	if !i.Valid {
		return nil
	}
	d := time.Duration(i.Microseconds)*time.Microsecond +
		time.Duration(i.Days)*24*time.Hour +
		time.Duration(i.Months)*30*24*time.Hour
	return &d
}

// Date wraps a *time.Time for a nullable date column (devices.acquired_on).
func Date(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

// DatePtr unwraps a nullable date column to *time.Time, nil if it was
// NULL.
func DatePtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	v := d.Time
	return &v
}
