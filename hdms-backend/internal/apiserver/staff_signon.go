package apiserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

const selfSignupActor = "self:microsoft"

// resolveOrProvision is the sign-on ladder from the Phase 7 design: an
// existing identity link wins, then a matching employee number, then a
// matching email, and only then is a user provisioned. Every rung runs in one
// transaction so a racing second callback either sees the first one's rows or
// loses on a unique constraint — never produces a duplicate person.
func (s *Server) resolveOrProvision(ctx context.Context, claims staffauth.Claims) (identityapi.UserSummary, staffauth.Account, error) {
	var user identityapi.UserSummary
	var account staffauth.Account

	err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		// Rung 1 — this Entra identity has signed in before.
		if linked, err := s.staffAuth.AccountForIdentity(ctx, "microsoft", claims.Subject); err == nil {
			found, err := s.identity.LookupUser(ctx, linked.UserID)
			if err != nil {
				return err
			}
			user, account = found, linked
			return nil
		} else if !errors.Is(err, staffauth.ErrAccountNotFound) {
			return err
		}

		// Rung 2 — the token names an employee number we already hold.
		if claims.EmployeeNo != "" {
			if found, err := s.identity.LookupUserByEmployeeNo(ctx, claims.EmployeeNo); err == nil {
				return s.linkTo(ctx, found, claims, &user, &account)
			} else if !errors.Is(err, identityapi.ErrUserNotFound) {
				return err
			}
		}

		// Rung 3 — the verified email belongs to a live user.
		if claims.Email != "" {
			if found, err := s.identity.LookupUserByEmail(ctx, claims.Email); err == nil {
				return s.linkTo(ctx, found, claims, &user, &account)
			} else if !errors.Is(err, identityapi.ErrUserNotFound) {
				return err
			}
		}

		// Rung 4 — provision. When Entra supplies no employee number the
		// profile stays incomplete and no credential is minted, so an
		// unfinished account can never scan at a kiosk.
		employeeNo := strings.TrimSpace(claims.EmployeeNo)
		complete := employeeNo != ""
		if !complete {
			employeeNo = placeholderEmployeeNo(claims.Subject)
		}
		created, err := s.identity.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo:   employeeNo,
			FullName:     fallbackName(claims),
			Email:        claims.Email,
			RegisteredBy: selfSignupActor,
		})
		if err != nil {
			return err
		}
		newAccount, err := s.staffAuth.EnsureAccount(ctx, created.ID, selfSignupActor, complete)
		if err != nil {
			return err
		}
		if err := s.staffAuth.LinkIdentity(ctx, newAccount.ID, "microsoft", claims.Subject, claims.TenantID, claims.Email); err != nil {
			return err
		}
		if complete {
			if _, err := s.credentials.Issue(ctx, credentialsapi.IssueParams{
				SubjectType: credentialsapi.SubjectUser,
				SubjectID:   created.ID,
				Kind:        credentialsapi.KindQR,
				IssuedBy:    selfSignupActor,
			}); err != nil {
				return err
			}
		}
		user, account = created, newAccount
		return nil
	})
	if err != nil {
		return identityapi.UserSummary{}, staffauth.Account{}, err
	}
	return user, account, nil
}

// linkTo attaches this Entra identity to a user who already exists. It never
// mints a credential: an existing user either has a card already or an
// administrator will issue one.
func (s *Server) linkTo(ctx context.Context, found identityapi.UserSummary, claims staffauth.Claims,
	user *identityapi.UserSummary, account *staffauth.Account) error {
	acct, err := s.staffAuth.EnsureAccount(ctx, found.ID, selfSignupActor, true)
	if err != nil {
		return err
	}
	if err := s.staffAuth.LinkIdentity(ctx, acct.ID, "microsoft", claims.Subject, claims.TenantID, claims.Email); err != nil {
		return err
	}
	*user, *account = found, acct
	return nil
}

// placeholderEmployeeNo produces a value that satisfies the employee-number
// format rules and is obviously provisional to an administrator reading the
// users list. It is replaced the first time the person opens the app.
func placeholderEmployeeNo(subject string) string {
	trimmed := strings.ReplaceAll(subject, "-", "")
	if len(trimmed) > 8 {
		trimmed = trimmed[:8]
	}
	return fmt.Sprintf("MS-%s", strings.ToUpper(trimmed))
}

func fallbackName(claims staffauth.Claims) string {
	if name := strings.TrimSpace(claims.DisplayName); name != "" {
		return name
	}
	if local, _, found := strings.Cut(claims.Email, "@"); found && local != "" {
		return local
	}
	return "Unnamed staff member"
}

// ResolveOrProvisionForTest exposes the sign-on ladder to integration tests.
// Production code calls the unexported resolveOrProvision through the callback
// handler; this wrapper exists so the ladder's rungs can be tested one at a
// time without standing up a fake identity provider.
func (s *Server) ResolveOrProvisionForTest(ctx context.Context, claims staffauth.Claims) (identityapi.UserSummary, staffauth.Account, error) {
	return s.resolveOrProvision(ctx, claims)
}
