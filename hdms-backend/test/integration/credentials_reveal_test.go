//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
)

func TestAUserCardTokenCanBeRevealedAfterIssue(t *testing.T) {
	env := newCredentialsTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-REVEAL-1", "Reveal Person", "reveal@hospital.example")
	issued, err := env.Credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: user.ID,
		Kind: credentialsapi.KindQR, IssuedBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	revealed, err := env.Credentials.Reveal(ctx, issued.Credential.ID, "admin:test")
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if revealed.Token != issued.Token {
		t.Fatalf("revealed token = %q, want the issued token %q — reveal must not mint a new one", revealed.Token, issued.Token)
	}
}

func TestEveryRevealWritesACredentialEvent(t *testing.T) {
	env := newCredentialsTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-REVEAL-2", "Audited Person", "audited@hospital.example")
	issued, err := env.Credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: user.ID,
		Kind: credentialsapi.KindQR, IssuedBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := env.Credentials.Reveal(ctx, issued.Credential.ID, "admin:alice"); err != nil {
		t.Fatalf("Reveal: %v", err)
	}

	var count int
	if err := env.Pool.QueryRow(ctx,
		`SELECT count(*) FROM credential_events WHERE credential_id = $1 AND kind = 'revealed' AND actor = 'admin:alice'`,
		issued.Credential.ID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("revealed events = %d, want 1", count)
	}
}

func TestRevealingARevokedCredentialIsRefused(t *testing.T) {
	env := newCredentialsTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-REVEAL-3", "Revoked Person", "revoked@hospital.example")
	issued, err := env.Credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: user.ID,
		Kind: credentialsapi.KindQR, IssuedBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := env.Credentials.Revoke(ctx, issued.Credential.ID, "lost", "admin:test"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := env.Credentials.Reveal(ctx, issued.Credential.ID, "admin:test"); err == nil {
		t.Fatal("revealing a revoked credential succeeded; a dead card must not be printable")
	}
}

func TestRevealIsRefusedForNonAdminRoles(t *testing.T) {
	env := newHTTPTestEnv(t)

	user := env.SeedUser(t, "E-REVEAL-4", "Role Person", "role@hospital.example")
	credentialID := env.SeedActiveUserCard(t, user.ID)

	for _, role := range []string{"technician", "viewer"} {
		session := env.AdminSessionForRole(t, role)
		req := httptest.NewRequest(http.MethodPost, "/v1/credentials/"+credentialID+"/reveal", nil)
		req.AddCookie(&http.Cookie{Name: "hdms_session", Value: session.Token})
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("reveal as %s = %d, want 403", role, rec.Code)
		}
	}
}
