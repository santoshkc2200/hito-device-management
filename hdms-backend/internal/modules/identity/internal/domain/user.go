// Package domain holds identity's business rules: validation and the user
// status lifecycle. It is pure — no database, no I/O — so these rules are
// tested without a Postgres instance.
package domain

import (
	"errors"
	"regexp"
	"strings"
)

// UserStatus mirrors the user_status Postgres enum.
type UserStatus string

const (
	StatusActive    UserStatus = "active"
	StatusSuspended UserStatus = "suspended"
	StatusArchived  UserStatus = "archived"
)

// employeeNoPattern requires an alphanumeric-first token of up to 32
// characters, letters/digits/hyphens only — permissive enough for formats
// like "HH-2407" (docs/06-api-contract.md) without accepting stray
// whitespace or punctuation that would make a printed label ambiguous.
var employeeNoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,31}$`)

var (
	ErrEmployeeNoRequired  = errors.New("identity: employee number is required")
	ErrEmployeeNoInvalid   = errors.New("identity: employee number must be 1-32 characters, starting alphanumeric, with only letters, digits and hyphens")
	ErrFullNameRequired    = errors.New("identity: full name is required")
	ErrRegisteredByInvalid = errors.New("identity: registered_by must be 'admin:<id>' or 'import', never 'kiosk:*'")
	ErrInvalidTransition   = errors.New("identity: illegal user status transition")
)

// ValidateEmployeeNo trims and checks an employee number, returning the
// canonical (trimmed) form.
func ValidateEmployeeNo(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ErrEmployeeNoRequired
	}
	if !employeeNoPattern.MatchString(v) {
		return "", ErrEmployeeNoInvalid
	}
	return v, nil
}

// ValidateFullName trims and checks a full name.
func ValidateFullName(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ErrFullNameRequired
	}
	return v, nil
}

// ValidateRegisteredBy enforces INV-11: a user is only ever created by an
// administrator or an import, never by a kiosk.
func ValidateRegisteredBy(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" || strings.HasPrefix(v, "kiosk:") {
		return "", ErrRegisteredByInvalid
	}
	if v != "import" && !strings.HasPrefix(v, "admin:") && !strings.HasPrefix(v, "import:") {
		return "", ErrRegisteredByInvalid
	}
	return v, nil
}

// ValidateTransition enforces the user status lifecycle: archived is
// terminal (INV-10 — archive, never delete), and active/suspended are
// otherwise freely reversible by an administrator.
func ValidateTransition(from, to UserStatus) error {
	if from == to {
		return nil
	}
	switch from {
	case StatusActive:
		if to == StatusSuspended || to == StatusArchived {
			return nil
		}
	case StatusSuspended:
		if to == StatusActive || to == StatusArchived {
			return nil
		}
	case StatusArchived:
		// terminal
	}
	return ErrInvalidTransition
}
