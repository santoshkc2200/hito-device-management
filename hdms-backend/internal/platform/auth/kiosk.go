package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	authstore "github.com/hito-hospital/hdms/internal/platform/auth/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
)

// pairingCodeTTL is how long a pairing code minted by IssuePairingCode stays
// redeemable (docs/phases/phase-2/2.0-preflight.md).
const pairingCodeTTL = 10 * time.Minute

var (
	ErrKioskNotFound      = errors.New("auth: kiosk not found")
	ErrPairingCodeInvalid = errors.New("auth: pairing code invalid or expired")
)

// RegisterKiosk mints a new kiosk row and its bearer token, returning the
// plaintext token — the only time it is ever available again. There is no
// HTTP endpoint for this in Phase 2; only hdms-cli kiosk register calls it.
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
	code, err = randomDigits(8)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(pairingCodeTTL)

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
func (s *Service) RedeemPairingCode(ctx context.Context, code string) (kioskID, name, plainToken string, err error) {
	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetKioskByPairingCodeHash(ctx, hashToken(code, s.pepper))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", ErrPairingCodeInvalid
		}
		return "", "", "", fmt.Errorf("auth: redeem pairing code: %w", err)
	}
	if !row.PairingCodeExpiresAt.Valid || time.Now().After(pgtypeconv.Time(row.PairingCodeExpiresAt)) {
		return "", "", "", ErrPairingCodeInvalid
	}

	plainToken, err = randomToken(32)
	if err != nil {
		return "", "", "", err
	}
	if err := q.RedeemKioskPairingCode(ctx, authstore.RedeemKioskPairingCodeParams{
		ID:        row.ID,
		TokenHash: hashToken(plainToken, s.pepper),
	}); err != nil {
		return "", "", "", fmt.Errorf("auth: redeem pairing code: %w", err)
	}
	return pgtypeconv.UUIDString(row.ID), row.Name, plainToken, nil
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
