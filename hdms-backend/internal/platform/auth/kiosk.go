package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	authstore "github.com/hito-hospital/hdms/internal/platform/auth/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// pairingCodeTTL is how long a pairing code minted by IssuePairingCode stays
// redeemable (docs/phases/phase-2/2.0-preflight.md).
const pairingCodeTTL = 10 * time.Minute

// Kiosk is the read model for a registered kiosk.
type Kiosk struct {
	ID             string
	Name           string
	Location       string
	EnabledSources []string
	Status         string // "active" | "disabled"
	DefaultLocale  string // "ja" | "en"
	LastSeenAt     *time.Time
	CreatedAt      time.Time
}

func mapKioskRow(id pgtype.UUID, name string, location pgtype.Text, sources []string, status authstore.KioskStatus, defaultLocale string, lastSeen pgtype.Timestamptz, createdAt pgtype.Timestamptz) Kiosk {
	var lastSeenAt *time.Time
	if lastSeen.Valid {
		t := pgtypeconv.Time(lastSeen)
		lastSeenAt = &t
	}
	return Kiosk{
		ID:             pgtypeconv.UUIDString(id),
		Name:           name,
		Location:       pgtypeconv.TextString(location),
		EnabledSources: sources,
		Status:         string(status),
		DefaultLocale:  defaultLocale,
		LastSeenAt:     lastSeenAt,
		CreatedAt:      pgtypeconv.Time(createdAt),
	}
}

// ListKiosks returns all kiosks ordered by creation time.
func (s *Service) ListKiosks(ctx context.Context) ([]Kiosk, error) {
	q := authstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListKiosks(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: list kiosks: %w", err)
	}
	items := make([]Kiosk, len(rows))
	for i, r := range rows {
		items[i] = mapKioskRow(r.ID, r.Name, r.Location, r.EnabledSources, r.Status, r.DefaultLocale, r.LastSeenAt, r.CreatedAt)
	}
	return items, nil
}

// GetKiosk fetches a kiosk by its ID.
func (s *Service) GetKiosk(ctx context.Context, kioskID string) (Kiosk, error) {
	pid, err := pgtypeconv.UUID(kioskID)
	if err != nil {
		return Kiosk{}, fmt.Errorf("auth: invalid kiosk id: %w", err)
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetKioskByID(ctx, pid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Kiosk{}, ErrKioskNotFound
		}
		return Kiosk{}, fmt.Errorf("auth: get kiosk: %w", err)
	}
	return mapKioskRow(row.ID, row.Name, row.Location, row.EnabledSources, row.Status, row.DefaultLocale, row.LastSeenAt, row.CreatedAt), nil
}

// DisableKiosk marks a kiosk as disabled.
func (s *Service) DisableKiosk(ctx context.Context, kioskID string) (Kiosk, error) {
	pid, err := pgtypeconv.UUID(kioskID)
	if err != nil {
		return Kiosk{}, fmt.Errorf("auth: invalid kiosk id: %w", err)
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.DisableKiosk(ctx, pid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Kiosk{}, ErrKioskNotFound
		}
		return Kiosk{}, fmt.Errorf("auth: disable kiosk: %w", err)
	}
	return mapKioskRow(row.ID, row.Name, row.Location, row.EnabledSources, row.Status, row.DefaultLocale, row.LastSeenAt, row.CreatedAt), nil
}

// EnableKiosk marks a kiosk as active.
func (s *Service) EnableKiosk(ctx context.Context, kioskID string) (Kiosk, error) {
	pid, err := pgtypeconv.UUID(kioskID)
	if err != nil {
		return Kiosk{}, fmt.Errorf("auth: invalid kiosk id: %w", err)
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.EnableKiosk(ctx, pid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Kiosk{}, ErrKioskNotFound
		}
		return Kiosk{}, fmt.Errorf("auth: enable kiosk: %w", err)
	}
	return mapKioskRow(row.ID, row.Name, row.Location, row.EnabledSources, row.Status, row.DefaultLocale, row.LastSeenAt, row.CreatedAt), nil
}

// UpdateKiosk updates a kiosk's mutable attributes (name, location, defaultLocale, enabledSources).
func (s *Service) UpdateKiosk(ctx context.Context, kioskID string, name, location, defaultLocale *string, enabledSources []string) (Kiosk, error) {
	pid, err := pgtypeconv.UUID(kioskID)
	if err != nil {
		return Kiosk{}, fmt.Errorf("auth: invalid kiosk id: %w", err)
	}
	params := authstore.UpdateKioskParams{
		ID: pid,
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return Kiosk{}, fmt.Errorf("name cannot be empty")
		}
		params.SetName = true
		params.Name = trimmed
	}
	if location != nil {
		params.SetLocation = true
		params.Location = strings.TrimSpace(*location)
	}
	if defaultLocale != nil {
		params.SetDefaultLocale = true
		params.DefaultLocale = strings.TrimSpace(*defaultLocale)
	}
	if enabledSources != nil {
		params.SetEnabledSources = true
		params.EnabledSources = enabledSources
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.UpdateKiosk(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Kiosk{}, ErrKioskNotFound
		}
		return Kiosk{}, fmt.Errorf("auth: update kiosk: %w", err)
	}
	return mapKioskRow(row.ID, row.Name, row.Location, row.EnabledSources, row.Status, row.DefaultLocale, row.LastSeenAt, row.CreatedAt), nil
}

// RegisterKiosk mints a new kiosk row and its bearer token, returning the
// plaintext token — the only time it is ever available again.
func (s *Service) RegisterKiosk(ctx context.Context, name, location string) (id, plainToken string, err error) {
	plainToken, err = randomToken(32)
	if err != nil {
		return "", "", err
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.CreateKiosk(ctx, authstore.CreateKioskParams{
		ID:        pgtypeconv.NewUUID(),
		Name:      name,
		Location:  pgtypeconv.Text(location),
		TokenHash: hashToken(plainToken, s.pepper),
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: register kiosk: %w", err)
	}
	return pgtypeconv.UUIDString(row.ID), plainToken, nil
}

// RotateKioskToken mints a fresh bearer token for an existing kiosk,
// invalidating the old one, and returns the new plaintext token.
func (s *Service) RotateKioskToken(ctx context.Context, kioskID string) (plainToken string, err error) {
	pid, err := pgtypeconv.UUID(kioskID)
	if err != nil {
		return "", fmt.Errorf("auth: invalid kiosk id: %w", err)
	}
	plainToken, err = randomToken(32)
	if err != nil {
		return "", err
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	if _, err := q.UpdateKioskTokenHash(ctx, authstore.UpdateKioskTokenHashParams{
		ID:        pid,
		TokenHash: hashToken(plainToken, s.pepper),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrKioskNotFound
		}
		return "", fmt.Errorf("auth: rotate kiosk token: %w", err)
	}
	return plainToken, nil
}

// IssuePairingCode mints a short single-use code an operator can hand to a
// kiosk device instead of typing its long-lived bearer token by hand
// (docs/phases/phase-2/2.0-preflight.md). It is stored hashed, the same
// scheme every other token in this package uses.
func (s *Service) IssuePairingCode(ctx context.Context, kioskID string) (code string, expiresAt time.Time, err error) {
	pid, err := pgtypeconv.UUID(kioskID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: invalid kiosk id: %w", err)
	}
	code, err = randomDigits(6)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = s.clock.Now().Add(pairingCodeTTL)

	q := authstore.New(db.Conn(ctx, s.pool))
	if _, err := q.SetKioskPairingCode(ctx, authstore.SetKioskPairingCodeParams{
		ID:                   pid,
		PairingCodeHash:      hashToken(code, s.pepper),
		PairingCodeExpiresAt: pgtypeconv.Timestamptz(expiresAt),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, ErrKioskNotFound
		}
		return "", time.Time{}, fmt.Errorf("auth: issue pairing code: %w", err)
	}
	return code, expiresAt, nil
}

// RedeemPairingCode consumes a pairing code and mints a fresh bearer token
// for the kiosk it names, returning the plaintext token. A consumed code
// and an expired one fail identically (ErrPairingCodeInvalid), so the
// endpoint built on this in 2.6 cannot be used to probe which kiosk ids or
// codes exist.
func (s *Service) RedeemPairingCode(ctx context.Context, code string) (kioskID, name, plainToken, defaultLocale string, err error) {
	if len(code) != 6 {
		return "", "", "", "", ErrPairingCodeInvalid
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return "", "", "", "", ErrPairingCodeInvalid
		}
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	codeHash := hashToken(code, s.pepper)
	row, err := q.GetKioskByPairingCodeHash(ctx, codeHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", "", ErrPairingCodeInvalid
		}
		return "", "", "", "", fmt.Errorf("auth: redeem pairing code: %w", err)
	}
	if !row.PairingCodeExpiresAt.Valid || s.clock.Now().After(pgtypeconv.Time(row.PairingCodeExpiresAt)) {
		return "", "", "", "", ErrPairingCodeInvalid
	}

	plainToken, err = randomToken(32)
	if err != nil {
		return "", "", "", "", err
	}
	// The UPDATE re-checks the pairing hash it just read, so two
	// simultaneous redemptions of one code cannot both succeed: the loser
	// matches no row and is told the code is invalid, rather than being
	// handed a token the winner's UPDATE has already replaced.
	redeemed, err := q.RedeemKioskPairingCode(ctx, authstore.RedeemKioskPairingCodeParams{
		ID:              row.ID,
		TokenHash:       hashToken(plainToken, s.pepper),
		PairingCodeHash: codeHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", "", ErrPairingCodeInvalid
		}
		return "", "", "", "", fmt.Errorf("auth: redeem pairing code: %w", err)
	}
	return pgtypeconv.UUIDString(redeemed.ID), redeemed.Name, plainToken, redeemed.DefaultLocale, nil
}

// randomDigits returns a base-10 string of n random digits from a
// crypto-random source. The pairing code it backs is short-lived,
// single-use and rejected identically whether wrong, consumed or expired,
// so the small modulo bias in digit selection is not a meaningful
// weakness.
func randomDigits(n int) (string, error) {
	const digits = "0123456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate pairing code: %w", err)
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = digits[int(b)%len(digits)]
	}
	return string(out), nil
}
