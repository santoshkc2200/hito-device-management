// Package ids generates the UUIDv7 identifiers used throughout HDMS:
// time-ordered, so B-tree primary key inserts stay sequential, while
// remaining a native `uuid` column with no Postgres extension required
// (docs/03-domain-model.md).
package ids

import "github.com/google/uuid"

// New returns a new UUIDv7 as a string.
func New() string {
	return NewUUID().String()
}

// NewUUID returns a new UUIDv7.
func NewUUID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// Only fails if the OS entropy source is broken, which is not a
		// condition any caller can recover from.
		panic(err)
	}
	return id
}
