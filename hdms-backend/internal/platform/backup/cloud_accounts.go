package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

const (
	AccountPending   = "pending"
	AccountConnected = "connected"
	AccountExpired   = "expired"
	AccountRevoked   = "revoked"
)

var (
	ErrCloudAccountNotFound     = errors.New("backup: cloud account not found")
	ErrCloudAccountInUse        = errors.New("backup: cloud account is used by a destination")
	ErrCloudAccountNotConnected = errors.New("backup: cloud account is not connected")
)

var tenantPattern = regexp.MustCompile(`^[A-Za-z0-9.-]{1,100}$`)

// CloudAccount is what the console may see. It carries no secret, not even
// whether one is set.
type CloudAccount struct {
	ID           uuid.UUID
	Provider     string
	Name         string
	ClientID     string
	Tenant       string
	AccountEmail string
	Status       string
	LastError    string
	ConnectedAt  *time.Time
	UpdatedAt    time.Time
}

type CloudAccountInput struct {
	Provider     string
	Name         string
	ClientID     string
	ClientSecret string // Google only; OneDrive is a public client
	Tenant       string // OneDrive only; "" means common
}

// SignIn is what the administrator needs to finish signing in elsewhere.
type SignIn struct {
	AccountID       uuid.UUID
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
}

// CloudCreds is everything the worker needs to render one rclone remote.
type CloudCreds struct {
	ID           uuid.UUID
	Provider     string
	ClientID     string
	ClientSecret string
	Tenant       string
	Token        string // rclone token JSON; empty before sign-in
	DriveID      string
	DriveType    string
	Status       string
}

// CloudService keeps cloud accounts in Postgres. The API uses Start,
// Reconnect, Poll, List, Get and Delete (Flow set); the worker uses
// credentials, StoreToken and SetStatus (Flow nil).
type CloudService struct {
	Q    db.DBTX
	Key  []byte // HDMS_CREDENTIAL_ENC_KEY, 32 bytes
	Flow *DeviceFlow
	Now  func() time.Time
}

func (s *CloudService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (in *CloudAccountInput) normalise() error {
	in.Name = strings.TrimSpace(in.Name)
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.ClientSecret = strings.TrimSpace(in.ClientSecret)
	in.Tenant = strings.TrimSpace(in.Tenant)
	invalid := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidCloudAccount, fmt.Sprintf(format, a...))
	}
	switch in.Provider {
	case ProviderGoogleDrive:
		if in.ClientSecret == "" {
			return invalid("Google Drive needs the OAuth client secret")
		}
		in.Tenant = "common"
	case ProviderOneDrive:
		if in.Tenant == "" {
			in.Tenant = "common"
		}
		if !tenantPattern.MatchString(in.Tenant) {
			return invalid("the directory (tenant) ID may use letters, digits, dots and dashes")
		}
	default:
		return invalid("unknown provider %q", in.Provider)
	}
	if in.Name == "" || len(in.Name) > 100 {
		return invalid("name must be 1 to 100 characters")
	}
	if in.ClientID == "" || len(in.ClientID) > 300 || len(in.ClientSecret) > 300 {
		return invalid("client ID and secret must be 1 to 300 characters")
	}
	// They are written into an rclone config file: a newline would add a line.
	for _, v := range []string{in.Name, in.ClientID, in.ClientSecret} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return invalid("values may not contain line breaks")
		}
	}
	return nil
}

const cloudColumns = `id, provider, name, client_id, tenant, coalesce(account_email, ''), status, coalesce(last_error, ''), connected_at, updated_at`

func scanCloudAccount(row pgx.Row) (CloudAccount, error) {
	var a CloudAccount
	err := row.Scan(&a.ID, &a.Provider, &a.Name, &a.ClientID, &a.Tenant, &a.AccountEmail, &a.Status, &a.LastError, &a.ConnectedAt, &a.UpdatedAt)
	return a, err
}

func (s *CloudService) Get(ctx context.Context, id uuid.UUID) (CloudAccount, error) {
	a, err := scanCloudAccount(s.Q.QueryRow(ctx, `SELECT `+cloudColumns+` FROM backup_cloud_accounts WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudAccount{}, ErrCloudAccountNotFound
	}
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: get cloud account: %w", err)
	}
	return a, nil
}

func (s *CloudService) List(ctx context.Context) ([]CloudAccount, error) {
	rows, err := s.Q.Query(ctx, `SELECT `+cloudColumns+` FROM backup_cloud_accounts ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("backup: list cloud accounts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CloudAccount, error) { return scanCloudAccount(row) })
	if err != nil {
		return nil, fmt.Errorf("backup: list cloud accounts: %w", err)
	}
	return out, nil
}

// Start registers the OAuth client and begins the device sign-in. A provider
// that rejects the client leaves nothing behind.
func (s *CloudService) Start(ctx context.Context, in CloudAccountInput, actor string) (SignIn, error) {
	if err := in.normalise(); err != nil {
		return SignIn{}, err
	}
	var sealed []byte
	if in.ClientSecret != "" {
		b, err := Encrypt(s.Key, []byte(in.ClientSecret))
		if err != nil {
			return SignIn{}, fmt.Errorf("backup: seal client secret: %w", err)
		}
		sealed = b
	}
	id := ids.NewUUID()
	if _, err := s.Q.Exec(ctx,
		`INSERT INTO backup_cloud_accounts (id, provider, name, client_id, client_secret_enc, tenant, status, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)`,
		id, in.Provider, in.Name, in.ClientID, sealed, in.Tenant, actor); err != nil {
		return SignIn{}, fmt.Errorf("backup: create cloud account: %w", err)
	}
	si, err := s.begin(ctx, id, actor)
	if err != nil {
		_, _ = s.Q.Exec(ctx, `DELETE FROM backup_cloud_accounts WHERE id = $1`, id)
		return SignIn{}, err
	}
	return si, nil
}

// Reconnect restarts the sign-in for an account whose token expired or was
// revoked, keeping its destinations.
func (s *CloudService) Reconnect(ctx context.Context, id uuid.UUID, actor string) (SignIn, error) {
	return s.begin(ctx, id, actor)
}

func (s *CloudService) begin(ctx context.Context, id uuid.UUID, actor string) (SignIn, error) {
	c, err := s.credentials(ctx, id)
	if err != nil {
		return SignIn{}, err
	}
	dc, err := s.Flow.Start(ctx, c.Provider, c.ClientID, c.Tenant)
	if err != nil {
		return SignIn{}, err
	}
	sealed, err := Encrypt(s.Key, []byte(dc.DeviceCode))
	if err != nil {
		return SignIn{}, fmt.Errorf("backup: seal device code: %w", err)
	}
	now := s.now()
	expires := now.Add(dc.ExpiresIn)
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts
		 SET status = 'pending', device_code_enc = $2, device_interval_s = $3, device_next_poll_at = $4,
		     device_expires_at = $5, last_error = NULL, updated_at = now(), updated_by = $6
		 WHERE id = $1`,
		id, sealed, int32(dc.Interval/time.Second), now.Add(dc.Interval), expires, actor); err != nil {
		return SignIn{}, fmt.Errorf("backup: save sign-in: %w", err)
	}
	return SignIn{AccountID: id, UserCode: dc.UserCode, VerificationURI: dc.VerificationURI, ExpiresAt: expires}, nil
}

// Poll asks the provider at most once per interval whether the sign-in is
// done, and returns the account as it stands. Calls inside the interval cost
// nothing: the next-poll time is claimed atomically, so two browsers cannot
// exchange one device code twice.
func (s *CloudService) Poll(ctx context.Context, id uuid.UUID) (CloudAccount, error) {
	var status string
	var expires *time.Time
	var interval *int32
	err := s.Q.QueryRow(ctx, `SELECT status, device_expires_at, device_interval_s FROM backup_cloud_accounts WHERE id = $1`, id).
		Scan(&status, &expires, &interval)
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudAccount{}, ErrCloudAccountNotFound
	}
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: read sign-in: %w", err)
	}
	if status != AccountPending {
		return s.Get(ctx, id)
	}
	now := s.now()
	if expires != nil && now.After(*expires) {
		if err := s.endFlow(ctx, id, AccountExpired, "The sign-in code expired. Start again."); err != nil {
			return CloudAccount{}, err
		}
		return s.Get(ctx, id)
	}
	wait := 5 * time.Second
	if interval != nil && *interval > 0 {
		wait = time.Duration(*interval) * time.Second
	}
	var sealed []byte
	err = s.Q.QueryRow(ctx,
		`UPDATE backup_cloud_accounts SET device_next_poll_at = $2
		 WHERE id = $1 AND status = 'pending' AND device_code_enc IS NOT NULL
		   AND (device_next_poll_at IS NULL OR device_next_poll_at <= $3)
		 RETURNING device_code_enc`, id, now.Add(wait), now).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.Get(ctx, id) // not due yet, or someone else is asking
	}
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: claim sign-in poll: %w", err)
	}
	code, err := Decrypt(s.Key, sealed)
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: open device code: %w", err)
	}
	c, err := s.credentials(ctx, id)
	if err != nil {
		return CloudAccount{}, err
	}
	tok, err := s.Flow.Exchange(ctx, c.Provider, c.ClientID, c.ClientSecret, c.Tenant, string(code))
	switch {
	case err == nil:
		if err := s.complete(ctx, id, c, tok); err != nil {
			return CloudAccount{}, err
		}
	case errors.Is(err, ErrAuthorizationPending):
	case errors.Is(err, ErrSlowDown):
		if _, err := s.Q.Exec(ctx,
			`UPDATE backup_cloud_accounts SET device_interval_s = coalesce(device_interval_s, 5) + 5, device_next_poll_at = $2 WHERE id = $1`,
			id, now.Add(wait+5*time.Second)); err != nil {
			return CloudAccount{}, fmt.Errorf("backup: slow down sign-in poll: %w", err)
		}
	case errors.Is(err, ErrCodeExpired):
		if err := s.endFlow(ctx, id, AccountExpired, "The sign-in code expired. Start again."); err != nil {
			return CloudAccount{}, err
		}
	case errors.Is(err, ErrAccessDenied):
		if err := s.endFlow(ctx, id, AccountRevoked, "Sign-in was declined."); err != nil {
			return CloudAccount{}, err
		}
	default:
		return CloudAccount{}, err // the sign-in stays pending and can be asked again
	}
	return s.Get(ctx, id)
}

func (s *CloudService) complete(ctx context.Context, id uuid.UUID, c CloudCreds, tok Token) error {
	prof, perr := s.Flow.Profile(ctx, c.Provider, c.Tenant, tok.AccessToken)
	if perr != nil && c.Provider == ProviderOneDrive {
		// The code is spent and rclone cannot work without the drive: start over.
		return s.endFlow(ctx, id, AccountExpired, "Signed in, but OneDrive's details could not be read. Reconnect to try again.")
	}
	js, err := tok.RcloneJSON()
	if err != nil {
		return fmt.Errorf("backup: encode token: %w", err)
	}
	sealed, err := Encrypt(s.Key, []byte(js))
	if err != nil {
		return fmt.Errorf("backup: seal token: %w", err)
	}
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts
		 SET status = 'connected', token_enc = $2, account_email = $3, drive_id = $4, drive_type = $5,
		     device_code_enc = NULL, device_interval_s = NULL, device_next_poll_at = NULL, device_expires_at = NULL,
		     last_error = NULL, connected_at = $6, updated_at = now()
		 WHERE id = $1`,
		id, sealed, nilIfEmpty(prof.Email), nilIfEmpty(prof.DriveID), nilIfEmpty(prof.DriveType), s.now()); err != nil {
		return fmt.Errorf("backup: save token: %w", err)
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// endFlow finishes a pending sign-in without a token.
func (s *CloudService) endFlow(ctx context.Context, id uuid.UUID, status, reason string) error {
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts
		 SET status = $2, last_error = $3, device_code_enc = NULL, device_interval_s = NULL,
		     device_next_poll_at = NULL, device_expires_at = NULL, updated_at = now()
		 WHERE id = $1 AND status = 'pending'`, id, status, reason); err != nil {
		return fmt.Errorf("backup: end sign-in: %w", err)
	}
	return nil
}

// Delete forgets an account. Destinations that use it must go first, so a
// scheduled backup never meets a destination without a sign-in.
func (s *CloudService) Delete(ctx context.Context, id uuid.UUID) error {
	var inUse bool
	if err := s.Q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM backup_destinations WHERE cloud_account_id = $1)`, id).Scan(&inUse); err != nil {
		return fmt.Errorf("backup: check cloud account use: %w", err)
	}
	if inUse {
		return ErrCloudAccountInUse
	}
	tag, err := s.Q.Exec(ctx, `DELETE FROM backup_cloud_accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("backup: delete cloud account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCloudAccountNotFound
	}
	return nil
}

func (s *CloudService) credentials(ctx context.Context, id uuid.UUID) (CloudCreds, error) {
	c := CloudCreds{ID: id}
	var secret, token []byte
	err := s.Q.QueryRow(ctx,
		`SELECT provider, client_id, client_secret_enc, tenant, token_enc, coalesce(drive_id, ''), coalesce(drive_type, ''), status
		 FROM backup_cloud_accounts WHERE id = $1`, id).
		Scan(&c.Provider, &c.ClientID, &secret, &c.Tenant, &token, &c.DriveID, &c.DriveType, &c.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudCreds{}, ErrCloudAccountNotFound
	}
	if err != nil {
		return CloudCreds{}, fmt.Errorf("backup: read cloud account: %w", err)
	}
	if secret != nil {
		plain, err := Decrypt(s.Key, secret)
		if err != nil {
			return CloudCreds{}, fmt.Errorf("backup: open client secret: %w", err)
		}
		c.ClientSecret = string(plain)
	}
	if token != nil {
		plain, err := Decrypt(s.Key, token)
		if err != nil {
			return CloudCreds{}, fmt.Errorf("backup: open token: %w", err)
		}
		c.Token = string(plain)
	}
	return c, nil
}

// StoreToken saves a token rclone refreshed during a run. It only touches a
// connected account: a sign-in restarted meanwhile must not be overwritten.
func (s *CloudService) StoreToken(ctx context.Context, id uuid.UUID, tokenJSON string) error {
	sealed, err := Encrypt(s.Key, []byte(tokenJSON))
	if err != nil {
		return fmt.Errorf("backup: seal token: %w", err)
	}
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts SET token_enc = $2, updated_at = now() WHERE id = $1 AND status = 'connected'`, id, sealed); err != nil {
		return fmt.Errorf("backup: store refreshed token: %w", err)
	}
	return nil
}

// SetStatus moves a connected account out of service, for example when the
// provider no longer accepts its token.
func (s *CloudService) SetStatus(ctx context.Context, id uuid.UUID, status, reason string) error {
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts SET status = $2, last_error = $3, updated_at = now() WHERE id = $1 AND status = 'connected'`,
		id, status, reason); err != nil {
		return fmt.Errorf("backup: set cloud account status: %w", err)
	}
	return nil
}
