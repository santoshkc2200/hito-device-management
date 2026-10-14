package checkout

import (
	"context"
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
)

type barcodeUsers struct {
	UserLookup
	user identityapi.UserSummary
	err  error
	got  string
}

func (f *barcodeUsers) LookupUserByEmployeeNo(_ context.Context, employeeNo string) (identityapi.UserSummary, error) {
	f.got = employeeNo
	return f.user, f.err
}

type barcodeTokens struct {
	TokenResolver
	ref   credentialsapi.SubjectRef
	err   error
	calls int
}

func (f *barcodeTokens) Resolve(context.Context, string) (credentialsapi.SubjectRef, error) {
	f.calls++
	return f.ref, f.err
}

func TestResolveEmployeeBarcode(t *testing.T) {
	for _, status := range []identityapi.UserStatus{identityapi.StatusActive, identityapi.StatusSuspended} {
		t.Run(string(status), func(t *testing.T) {
			users := &barcodeUsers{user: identityapi.UserSummary{ID: "user-1", Status: status}}
			// Even an old revoked credential with the same number cannot revoke the permanent employee barcode.
			tokens := &barcodeTokens{ref: credentialsapi.SubjectRef{Type: credentialsapi.RefUser, CredentialStatus: credentialsapi.StatusRevoked}}
			svc := &Service{deps: Deps{Users: users, Tokens: tokens}}
			in, err := svc.resolveInput(t.Context(), "  001234  ")
			if err != nil || in.Kind != machine.KindUser || in.UserID != "user-1" || in.UserStatus != string(status) {
				t.Fatalf("resolve employee number: input=%+v err=%v", in, err)
			}
			if users.got != "001234" || tokens.calls != 0 {
				t.Fatalf("employee number=%q credential lookups=%d", users.got, tokens.calls)
			}
		})
	}
}

func TestResolveUnknownEmployeeBarcode(t *testing.T) {
	users := &barcodeUsers{err: identityapi.ErrUserNotFound}
	tokens := &barcodeTokens{err: credentialsapi.ErrCredentialUnknown}
	svc := &Service{deps: Deps{Users: users, Tokens: tokens}}
	in, err := svc.resolveInput(t.Context(), "HH-UNKNOWN")
	if err != nil || in.Kind != machine.KindUnknown || tokens.calls != 1 {
		t.Fatalf("unknown employee: input=%+v err=%v lookups=%d", in, err, tokens.calls)
	}
}

func TestResolveEmployeeBarcodePropagatesLookupFailure(t *testing.T) {
	want := errors.New("database unavailable")
	users := &barcodeUsers{err: want}
	tokens := &barcodeTokens{err: credentialsapi.ErrCredentialUnknown}
	svc := &Service{deps: Deps{Users: users, Tokens: tokens}}
	_, err := svc.resolveInput(t.Context(), "HH-2407")
	if !errors.Is(err, want) || tokens.calls != 0 {
		t.Fatalf("lookup error=%v credential lookups=%d", err, tokens.calls)
	}
}

func TestResolveLegacyTokenKeepsRevocation(t *testing.T) {
	users := &barcodeUsers{}
	tokens := &barcodeTokens{ref: credentialsapi.SubjectRef{Type: credentialsapi.RefUser, CredentialStatus: credentialsapi.StatusRevoked}}
	svc := &Service{deps: Deps{Users: users, Tokens: tokens}}
	in, err := svc.resolveInput(t.Context(), "HD-U-7K3M9QXA2F-4")
	if err != nil || in.Kind != machine.KindRevoked || users.got != "" {
		t.Fatalf("legacy token: input=%+v err=%v employee lookup=%q", in, err, users.got)
	}
}
