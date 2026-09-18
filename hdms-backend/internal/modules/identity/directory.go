package identity

import (
	"context"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/identity/internal/directory"
	"github.com/hito-hospital/hdms/internal/modules/identity/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// Re-export directory types so jobs, CLI, and integration tests can configure
// and execute directory sync without importing internal/ packages directly.
type DirectoryClient = directory.Client
type DirectoryEntry = directory.Entry
type FakeDirectoryClient = directory.FakeClient
type DirectorySyncReport = directory.Report
type DirectorySyncOptions = directory.SyncOptions
type LDAPConfig = directory.LDAPConfig

var (
	NewFakeDirectoryClient = directory.NewFakeClient
	NewLDAPDirectoryClient = directory.NewLDAPClient
	ErrMassChangeRefused   = directory.ErrMassChangeRefused
)

// SyncDirectory executes directory sync using the identity module's store and the provided directory client.
func (s *Service) SyncDirectory(ctx context.Context, client directory.Client, now time.Time, opts directory.SyncOptions) (directory.Report, error) {
	return directory.Sync(ctx, s.pool, s.audit, client, now, opts)
}

// LinkExternalIdentity associates an existing user with an authoritative external directory subject.
func (s *Service) LinkExternalIdentity(ctx context.Context, userID, issuer, subject string, lastSeen time.Time) error {
	q := identitystore.New(s.pool)
	uID, err := pgtypeconv.UUID(userID)
	if err != nil {
		return err
	}
	_, err = q.UpsertDirectoryLink(ctx, identitystore.UpsertDirectoryLinkParams{
		ID:                  pgtypeconv.NewUUID(),
		UserID:              uID,
		Issuer:              issuer,
		Subject:             subject,
		LastSeenInDirectory: pgtypeconv.Timestamptz(lastSeen.UTC()),
		SyncState:           "synced",
	})
	return err
}
