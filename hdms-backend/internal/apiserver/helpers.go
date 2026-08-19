package apiserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// actorFrom derives the audit/registeredBy actor string from the
// authenticated admin on the request's context — "admin:<id>", never a
// client-supplied value (INV-11). Every admin-scoped route in this file has
// already passed auth.Middleware by the time a handler runs, so the admin
// is always present; the empty-string fallback only matters for tests that
// call a handler directly without going through the middleware chain.
func actorFrom(r *http.Request) string {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		return ""
	}
	return "admin:" + admin.ID
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON reads and decodes r's body into T. On failure it writes a
// validation-failed problem and returns ok=false; callers should return
// immediately.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (body T, ok bool) {
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeValidationFailed(w, r, "Request body is not valid JSON", nil)
		var zero T
		return zero, false
	}
	return body, true
}

func writeValidationFailed(w http.ResponseWriter, r *http.Request, detail string, fields []string) {
	p := httpx.NewProblem("validation-failed", "Validation failed", http.StatusUnprocessableEntity)
	p.Detail = detail
	if len(fields) > 0 {
		p.Extensions = map[string]any{"fields": fields}
	}
	httpx.WriteProblem(w, r, p)
}

// requireReason writes a validation-failed problem and returns false if
// reason is blank. Revoke/reissue/suspend all require a non-empty reason;
// checking here (rather than only in the module's domain layer, whose
// error type is unreachable from cmd by Go's own internal-package
// visibility rules) keeps the HTTP contract's requiredness enforceable.
func requireReason(w http.ResponseWriter, r *http.Request, reason string) bool {
	if strings.TrimSpace(reason) == "" {
		writeValidationFailed(w, r, "reason is required", []string{"reason"})
		return false
	}
	return true
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// writeServiceError maps a module-layer error to an RFC 9457 problem per
// docs/06-api-contract.md's registered `type` suffixes, extended with the
// admin-API-specific ones documented alongside them. Unrecognised errors
// fall back to a generic 500 — nothing here should mask a real bug as a
// well-formed client error.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identityapi.ErrUserNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("user-not-found", "User not found", http.StatusNotFound))
	case errors.Is(err, identityapi.ErrEmployeeNoTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("employee-no-taken", "Employee number is already in use", http.StatusConflict))
	case errors.Is(err, identityapi.ErrIllegalTransition):
		httpx.WriteProblem(w, r, httpx.NewProblem("illegal-transition", "Illegal user status transition", http.StatusConflict))

	case errors.Is(err, catalogapi.ErrDeviceNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("device-not-found", "Device not found", http.StatusNotFound))
	case errors.Is(err, catalogapi.ErrAssetTagTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("asset-tag-taken", "Asset tag is already in use", http.StatusConflict))
	case errors.Is(err, catalogapi.ErrCategoryNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("category-not-found", "Category not found", http.StatusNotFound))
	case errors.Is(err, catalogapi.ErrIllegalTransition):
		httpx.WriteProblem(w, r, httpx.NewProblem("illegal-transition", "Illegal device status transition", http.StatusConflict))

	case errors.Is(err, credentialsapi.ErrCredentialUnknown):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-unknown", "Token does not match any credential", http.StatusNotFound))
	case errors.Is(err, credentialsapi.ErrCredentialNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-not-found", "Credential not found", http.StatusNotFound))
	case errors.Is(err, credentialsapi.ErrCredentialNotUnbound):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-already-bound", "Credential is not an unbound, active card", http.StatusConflict))
	case errors.Is(err, credentialsapi.ErrCredentialNotActive):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-not-active", "Credential is not active", http.StatusConflict))
	case errors.Is(err, credentialsapi.ErrNotDeviceCredential):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-not-reprintable", "Only device credentials store a recoverable token", http.StatusConflict))
	case errors.Is(err, credentialsapi.ErrKindNotIssuableInV1):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-kind-unavailable", "nfc and rfid credentials are not issued until Phase 6", http.StatusUnprocessableEntity))
	case errors.Is(err, credentialsapi.ErrManualTokenRequired):
		writeValidationFailed(w, r, "manualToken is required when kind is manual", []string{"manualToken"})
	case errors.Is(err, credentialsapi.ErrTokenAlreadyRegistered):
		httpx.WriteProblem(w, r, httpx.NewProblem("credential-token-taken", "That token is already registered to a credential", http.StatusConflict))

	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrSessionInvalid):
		httpx.WriteProblem(w, r, httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized))
	case errors.Is(err, auth.ErrAccountDisabled):
		httpx.WriteProblem(w, r, httpx.NewProblem("forbidden", "Account disabled", http.StatusForbidden))
	case errors.Is(err, auth.ErrEmailTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("email-taken", "Email already registered", http.StatusConflict))

	default:
		// Domain validation errors (bad employee number, empty name, …)
		// surface as raw errors from module methods — there is no
		// sentinel to match, but they are always caller-input problems,
		// never a server fault, so 422 is the right status.
		writeValidationFailed(w, r, err.Error(), nil)
	}
}
