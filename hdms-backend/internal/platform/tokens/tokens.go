// Package tokens implements the HDMS credential token format described in
// docs/05-credentials-and-labeling.md:
//
//	HD-U-7K3M9QXA2F-4
//	│  │ │            └── check character (Crockford Base32 mod-37)
//	│  │ └─────────────── 10-char random payload, Crockford Base32
//	│  └───────────────── subject hint: U = user, D = device
//	└──────────────────── namespace, so a foreign barcode is rejected instantly
//
// packages/domain/src/token.ts is the TypeScript twin of Parse and Validate,
// kept in sync by a golden fixture both sides check
// (fixtures/token-fixtures.json). Generate is Go-only: tokens are always
// minted server-side, never by the kiosk.
//
// The subject hint is a hint, not authority — Resolve always trusts the
// stored subject_type, never the scanned prefix. Mutating the hint character
// alone therefore still parses; only the namespace, payload and check
// character are what Validate actually enforces.
package tokens

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Namespace prefixes every token so a foreign barcode (a product UPC, a
// visitor's own QR code) is rejected before it ever reaches the database.
const Namespace = "HD"

// alphabet is Crockford Base32: excludes I, L, O, U to avoid confusion
// between similar-looking characters when a human reads or types a token.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// checkAlphabet extends alphabet with five check-only symbols so the check
// character can encode all 37 values of a mod-37 checksum. This is
// Crockford's published check-symbol alphabet; U reappears here only as a
// checksum digit, never as data, so it never collides with a payload
// character.
const checkAlphabet = alphabet + "*~$=U"

const payloadLength = 10

// payloadBits is the number of random bits consumed per token: 10 symbols ×
// 5 bits, exactly the 50 bits of entropy documented in
// docs/05-credentials-and-labeling.md.
const payloadBits = payloadLength * 5

// SubjectHint is the single-character routing hint embedded in a token.
type SubjectHint byte

const (
	HintUser   SubjectHint = 'U'
	HintDevice SubjectHint = 'D'
)

// Errors returned by Parse. Each names the specific field that failed so
// callers (and the API's problem-detail mapping) can be precise about what
// was wrong with a scan.
var (
	ErrInvalidFormat    = errors.New("tokens: does not have namespace-hint-payload-check structure")
	ErrInvalidNamespace = errors.New("tokens: unrecognised namespace")
	ErrInvalidHint      = errors.New("tokens: unrecognised subject hint")
	ErrInvalidPayload   = errors.New("tokens: payload is the wrong length or contains invalid characters")
	ErrInvalidChecksum  = errors.New("tokens: check character does not match the payload")
)

// Token is a parsed, validated token. The zero value is never returned by
// Parse on success.
type Token struct {
	Hint    SubjectHint
	Payload string
	Check   byte
}

// String renders the canonical, uppercase, hyphenated form.
func (t Token) String() string {
	return fmt.Sprintf("%s-%c-%s-%c", Namespace, byte(t.Hint), t.Payload, t.Check)
}

// IsUser reports whether the token's hint routes to a user. This is a hint
// only — see the package doc.
func (t Token) IsUser() bool { return t.Hint == HintUser }

// IsDevice reports whether the token's hint routes to a device.
func (t Token) IsDevice() bool { return t.Hint == HintDevice }

// Generate mints a fresh, cryptographically random token for the given
// subject hint. The 50 bits of payload entropy are read directly from
// crypto/rand 5 bits at a time, so every symbol is uniform with no modulo
// bias to correct for.
func Generate(hint SubjectHint) (Token, error) {
	if hint != HintUser && hint != HintDevice {
		return Token{}, fmt.Errorf("tokens: generate: %w: %q", ErrInvalidHint, hint)
	}

	var buf [8]byte
	if _, err := rand.Read(buf[1:]); err != nil {
		return Token{}, fmt.Errorf("tokens: generate: read random bytes: %w", err)
	}
	n := binary.BigEndian.Uint64(buf[:]) & (1<<payloadBits - 1)

	payload := make([]byte, payloadLength)
	for i := payloadLength - 1; i >= 0; i-- {
		payload[i] = alphabet[n&0x1F]
		n >>= 5
	}

	check, err := checkCharFor(string(payload))
	if err != nil {
		return Token{}, fmt.Errorf("tokens: generate: %w", err)
	}
	return Token{Hint: hint, Payload: string(payload), Check: check}, nil
}

// Parse validates raw against the token grammar and checksum, tolerating
// the transcription noise a human or a misfiring scanner introduces:
// leading/trailing whitespace, lowercase input, and Crockford's standard
// I/L→1, O→0 substitution for characters that are easy to misread.
func Parse(raw string) (Token, error) {
	s := normalize(raw)

	parts := strings.Split(s, "-")
	if len(parts) != 4 {
		return Token{}, ErrInvalidFormat
	}
	namespace, hintPart, payload, checkPart := parts[0], parts[1], parts[2], parts[3]

	if namespace != Namespace {
		return Token{}, ErrInvalidNamespace
	}

	if len(hintPart) != 1 {
		return Token{}, ErrInvalidHint
	}
	hint := SubjectHint(hintPart[0])
	if hint != HintUser && hint != HintDevice {
		return Token{}, ErrInvalidHint
	}

	if len(payload) != payloadLength {
		return Token{}, ErrInvalidPayload
	}
	for i := 0; i < len(payload); i++ {
		if strings.IndexByte(alphabet, payload[i]) < 0 {
			return Token{}, ErrInvalidPayload
		}
	}

	if len(checkPart) != 1 || strings.IndexByte(checkAlphabet, checkPart[0]) < 0 {
		return Token{}, ErrInvalidChecksum
	}

	want, err := checkCharFor(payload)
	if err != nil {
		return Token{}, ErrInvalidPayload
	}
	if checkPart[0] != want {
		return Token{}, ErrInvalidChecksum
	}

	return Token{Hint: hint, Payload: payload, Check: checkPart[0]}, nil
}

// Validate reports whether raw is a structurally and checksum-valid token,
// without needing the parsed value. The kiosk uses this (via the
// TypeScript twin) to reject garbage scans before making a network call.
func Validate(raw string) bool {
	_, err := Parse(raw)
	return err == nil
}

// checkCharFor computes the Crockford mod-37 check character for a payload
// already confirmed to contain only alphabet characters. The running
// remainder is updated one base-32 digit at a time so the 50-bit value
// never needs to be materialised as a single integer.
func checkCharFor(payload string) (byte, error) {
	acc := 0
	for i := 0; i < len(payload); i++ {
		v := strings.IndexByte(alphabet, payload[i])
		if v < 0 {
			return 0, ErrInvalidPayload
		}
		acc = (acc*32 + v) % 37
	}
	return checkAlphabet[acc], nil
}

// normalize uppercases, trims surrounding whitespace, and applies
// Crockford's ambiguous-character substitution (I/L→1, O→0). Every
// character this can produce is disjoint from the check-only symbols
// (*~$=U), so it never corrupts a valid check character.
func normalize(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'I', 'L':
			b.WriteRune('1')
		case 'O':
			b.WriteRune('0')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maxForeignLength bounds a foreign credential value (an employee-ID
// barcode, an RFID UID) so a stray scan cannot push an arbitrarily long
// string at the database.
const maxForeignLength = 64

// Scannable reports whether raw is worth resolving against credentials: an
// HDMS token with a valid structure and checksum, or a foreign credential
// value (employee-ID barcode, card UID) of 1 to 64 printable ASCII
// characters. A string in the HDMS namespace that fails Parse is a damaged
// HDMS token, not a foreign credential, so it is rejected rather than
// looked up. Whether a scannable value matches anything is for Resolve.
func Scannable(raw string) bool {
	s := strings.TrimSpace(raw)
	if len(s) >= len(Namespace)+1 && strings.EqualFold(s[:len(Namespace)+1], Namespace+"-") {
		return Validate(s)
	}
	if s == "" || len(s) > maxForeignLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7E {
			return false
		}
	}
	return true
}
