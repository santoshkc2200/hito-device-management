package directory

import "context"

// Entry represents a staff record retrieved from the directory.
type Entry struct {
	Subject    string // Stable unique ID in the directory (e.g. entryUUID, objectGUID, or DN)
	EmployeeNo string
	FullName   string
	Department string
	Email      string
	Phone      string
}

// Client is a read-only directory client for staff roster synchronization.
// Implementations MUST NEVER perform write operations against the external directory.
type Client interface {
	// SearchStaff returns all eligible staff entries from the directory.
	SearchStaff(ctx context.Context) ([]Entry, error)
}
