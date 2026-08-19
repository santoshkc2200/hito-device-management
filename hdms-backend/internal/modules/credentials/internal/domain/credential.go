// Package domain holds credentials' business rules: kind/status types,
// validation, and RFID/NFC UID normalisation. It is pure — no database, no
// I/O — so these rules are tested without a Postgres instance.
package domain

import (
	"errors"
	"strings"
)

// Kind mirrors the credential_kind Postgres enum.
type Kind string

const (
	KindQR      Kind = "qr"
	KindCode128 Kind = "code128"
	KindNFC     Kind = "nfc"
	KindRFID    Kind = "rfid"
	KindManual  Kind = "manual"
)

// Generated reports whether tokens of this kind are minted by
// platform/tokens.Generate. nfc and rfid are read from a physical card's
// UID instead (docs/05-credentials-and-labeling.md); manual is typed in by
// an administrator, e.g. to adopt an existing pre-printed asset label.
func (k Kind) Generated() bool {
	return k == KindQR || k == KindCode128
}

// SubjectType mirrors the subject_type Postgres enum.
type SubjectType string

const (
	SubjectUser   SubjectType = "user"
	SubjectDevice SubjectType = "device"
)

// Status mirrors the credential_status Postgres enum.
type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
	StatusLost    Status = "lost"
)

var (
	ErrReasonRequired      = errors.New("credentials: a reason is required")
	ErrKindNotIssuableInV1 = errors.New("credentials: nfc and rfid credentials are not issued until the Phase 6 reader integration")
	ErrManualTokenRequired = errors.New("credentials: a manual credential requires an explicit token value")
	ErrNotDeviceCredential = errors.New("credentials: reprint recovers a plaintext token only for device credentials")
	ErrUIDEmpty            = errors.New("credentials: UID is empty")
	ErrUIDOddLength        = errors.New("credentials: UID must have an even number of hex digits")
	ErrUIDInvalidHex       = errors.New("credentials: UID contains non-hex characters")
)

// ValidateReason trims and requires a non-empty reason, for the mandatory
// reasons on revoke and reissue (docs/05-credentials-and-labeling.md).
func ValidateReason(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ErrReasonRequired
	}
	return v, nil
}

// ByteOrder controls whether NormalizeUID reverses byte pairs, for readers
// that emit a card's UID little-endian relative to the convention this
// system stores it in.
type ByteOrder int

const (
	ByteOrderAsIs ByteOrder = iota
	ByteOrderReversed
)

// NormalizeUID canonicalises an RFID/NFC UID read from a reader: uppercase,
// strip common separators (colon, hyphen, space), and optionally reverse
// byte order. Built now per the Phase 1 task list so the Phase 6
// integration needs no further design, though nothing issues nfc/rfid
// credentials until then.
func NormalizeUID(raw string, order ByteOrder) (string, error) {
	stripped := strings.NewReplacer(":", "", "-", "", " ", "").Replace(strings.TrimSpace(raw))
	if stripped == "" {
		return "", ErrUIDEmpty
	}
	if len(stripped)%2 != 0 {
		return "", ErrUIDOddLength
	}
	upper := strings.ToUpper(stripped)
	for _, r := range upper {
		if (r < '0' || r > '9') && (r < 'A' || r > 'F') {
			return "", ErrUIDInvalidHex
		}
	}

	if order == ByteOrderAsIs {
		return upper, nil
	}

	bytePairs := len(upper) / 2
	reversed := make([]byte, len(upper))
	for i := range bytePairs {
		src := upper[i*2 : i*2+2]
		dstStart := (bytePairs - 1 - i) * 2
		copy(reversed[dstStart:dstStart+2], src)
	}
	return string(reversed), nil
}
