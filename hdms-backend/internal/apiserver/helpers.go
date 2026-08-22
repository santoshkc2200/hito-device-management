package apiserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

// setNoStore sets Cache-Control: no-store on sensitive responses containing tokens.
func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

// actorFrom derives the audit/registeredBy actor string from the
// authenticated admin on the request's context — "admin:<id>", never a
// client-supplied value (INV-11). Every admin-scoped route in this file has
// already passed auth.Middleware by the time a handler runs, so the admin
// is always present; the empty-string fallback only matters for tests that
// call a handler directly without going through the middleware chain.
func actorFrom(r *http.Request) string {
	if admin, ok := auth.AdminFromContext(r.Context()); ok {
		return "admin:" + admin.ID
	}
	if kiosk, ok := auth.KioskFromContext(r.Context()); ok {
		return "kiosk:" + kiosk.ID
	}
	return ""
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
func (s *Server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var devOnLoanErr *lendingapi.DeviceAlreadyOnLoanError
	if errors.As(err, &devOnLoanErr) {
		p := httpx.NewProblem("device-on-loan", "Device is already on loan", http.StatusConflict)
		p.Detail = "This device is currently held by another staff member."
		ext := map[string]any{"deviceId": devOnLoanErr.Existing.DeviceID}
		if s.identity != nil {
			if u, uErr := s.identity.LookupUser(r.Context(), devOnLoanErr.Existing.UserID); uErr == nil && u.DepartmentID != "" {
				if dept, dErr := s.identity.LookupDepartment(r.Context(), u.DepartmentID); dErr == nil {
					ext["holderDepartment"] = dept.Name
				}
			}
		}
		p.Extensions = ext
		httpx.WriteProblem(w, r, p)
		return
	}

	var overlapErr *lendingapi.OverlappingCustodyError
	if errors.As(err, &overlapErr) {
		p := httpx.NewProblem("overlapping-custody", "Overlapping custody period", http.StatusConflict)
		p.Detail = "The loan would overlap an existing custody window for this device."
		p.Extensions = map[string]any{"deviceId": overlapErr.Existing.DeviceID}
		httpx.WriteProblem(w, r, p)
		return
	}

	switch {
	// Lending module errors
	case errors.Is(err, lendingapi.ErrLoanNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("loan-not-found", "Loan not found", http.StatusNotFound))
	case errors.Is(err, lendingapi.ErrLoanNotOpen):
		httpx.WriteProblem(w, r, httpx.NewProblem("loan-not-open", "Loan is not open", http.StatusConflict))
	case errors.Is(err, lendingapi.ErrDeviceAlreadyOnLoan):
		httpx.WriteProblem(w, r, httpx.NewProblem("device-on-loan", "Device is already on loan", http.StatusConflict))
	case errors.Is(err, lendingapi.ErrOverlappingCustody):
		httpx.WriteProblem(w, r, httpx.NewProblem("overlapping-custody", "Overlapping custody period", http.StatusConflict))
	case errors.Is(err, lendingapi.ErrBackdatedNotPermitted):
		httpx.WriteProblem(w, r, httpx.NewProblem("backdated-not-permitted", "Backdated timestamps are only permitted via historical backfill", http.StatusUnprocessableEntity))
	case errors.Is(err, lendingapi.ErrInvalidReturnTime):
		writeValidationFailed(w, r, "returnedAt must be after borrowedAt", []string{"returnedAt"})
	case errors.Is(err, lendingapi.ErrPaperProvenanceRequired):
		writeValidationFailed(w, r, "paper loans require paperRef, recordedAt and recordedBy", nil)

	// Checkout module errors
	case errors.Is(err, checkoutapi.ErrSessionNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("session-not-found", "Session not found", http.StatusNotFound))
	case errors.Is(err, checkoutapi.ErrSessionExpired):
		httpx.WriteProblem(w, r, httpx.NewProblem("session-expired", "Session has expired", http.StatusGone))
	case errors.Is(err, checkoutapi.ErrSessionClosed):
		httpx.WriteProblem(w, r, httpx.NewProblem("session-conflict", "Session is already closed", http.StatusConflict))
	case errors.Is(err, checkoutapi.ErrSessionConflict):
		httpx.WriteProblem(w, r, httpx.NewProblem("session-conflict", "Concurrent modification of session", http.StatusConflict))
	case errors.Is(err, checkoutapi.ErrInvalidTokenFormat):
		httpx.WriteProblem(w, r, httpx.NewProblem("invalid-token-format", "Invalid token format or checksum", http.StatusBadRequest))
	case errors.Is(err, checkoutapi.ErrHistoricalTimeInFuture):
		writeValidationFailed(w, r, "historical timestamps cannot be in the future", nil)
	case errors.Is(err, checkoutapi.ErrPaperRefRequired):
		writeValidationFailed(w, r, "paperRef is required", []string{"paperRef"})
	case errors.Is(err, checkoutapi.ErrEmptyPaperBatch):
		writeValidationFailed(w, r, "batch must contain at least one row", []string{"rows"})
	case errors.Is(err, checkoutapi.ErrPaperRowMalformed):
		writeValidationFailed(w, r, "paper row is malformed", nil)

	// Identity module errors
	case errors.Is(err, identityapi.ErrUserNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("user-not-found", "User not found", http.StatusNotFound))
	case errors.Is(err, identityapi.ErrDepartmentNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("department-not-found", "Department not found", http.StatusNotFound))
	case errors.Is(err, identityapi.ErrDepartmentNameTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("department-name-taken", "Department name is already in use", http.StatusConflict))
	case errors.Is(err, identityapi.ErrDepartmentInUse):
		p := httpx.NewProblem("department-in-use", "Department cannot be deleted", http.StatusConflict)
		p.Detail = "You cannot delete this department because it is being used by users."
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, identityapi.ErrDepartmentNameRequired):
		writeValidationFailed(w, r, "department name is required", []string{"name"})
	case errors.Is(err, identityapi.ErrEmployeeNoTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("employee-no-taken", "Employee number is already in use", http.StatusConflict))
	case errors.Is(err, identityapi.ErrIllegalTransition):
		httpx.WriteProblem(w, r, httpx.NewProblem("illegal-transition", "Illegal user status transition", http.StatusConflict))

	// Catalog module errors
	case errors.Is(err, catalogapi.ErrDeviceNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("device-not-found", "Device not found", http.StatusNotFound))
	case errors.Is(err, catalogapi.ErrAssetTagTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("asset-tag-taken", "Asset tag is already in use", http.StatusConflict))
	case errors.Is(err, catalogapi.ErrCategoryNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("category-not-found", "Category not found", http.StatusNotFound))
	case errors.Is(err, catalogapi.ErrIllegalTransition):
		httpx.WriteProblem(w, r, httpx.NewProblem("illegal-transition", "Illegal device status transition", http.StatusConflict))

	// Credentials module errors
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

	// Auth errors
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrSessionInvalid):
		httpx.WriteProblem(w, r, httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized))
	case errors.Is(err, auth.ErrAccountDisabled):
		httpx.WriteProblem(w, r, httpx.NewProblem("forbidden", "Account disabled", http.StatusForbidden))
	case errors.Is(err, auth.ErrAccountLocked):
		httpx.WriteProblem(w, r, httpx.NewProblem("account-locked", "Account locked", http.StatusForbidden))
	case errors.Is(err, auth.ErrEmailTaken):
		httpx.WriteProblem(w, r, httpx.NewProblem("email-taken", "Email already registered", http.StatusConflict))
	case errors.Is(err, auth.ErrAdminNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("admin-not-found", "Admin not found", http.StatusNotFound))
	case errors.Is(err, auth.ErrNoPendingTotp):
		httpx.WriteProblem(w, r, httpx.NewProblem("no-pending-totp", "No pending TOTP re-enrolment", http.StatusBadRequest))
	case errors.Is(err, auth.ErrInvalidPassword):
		httpx.WriteProblem(w, r, httpx.NewProblem("invalid-password", "Current password is incorrect", http.StatusUnprocessableEntity))
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeValidationFailed(w, r, "password must be at least 12 characters", []string{"password"})
	case errors.Is(err, auth.ErrKioskNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("kiosk-not-found", "Kiosk not found", http.StatusNotFound))
	// Listing errors
	case errors.Is(err, listing.ErrInvalidCursor), errors.Is(err, listing.ErrStaleCursor):
		p := httpx.NewProblem("invalid-cursor", "Invalid pagination cursor", http.StatusBadRequest)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, listing.ErrInvalidSort), errors.Is(err, listing.ErrInvalidOrder),
		errors.Is(err, listing.ErrInvalidLimit), errors.Is(err, listing.ErrUnknownFilter):
		p := httpx.NewProblem("validation-failed", "Validation failed", http.StatusBadRequest)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)

	default:
		// Domain validation errors surface as raw errors from module methods
		writeValidationFailed(w, r, err.Error(), nil)
	}
}
