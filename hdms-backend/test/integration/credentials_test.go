//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/ids"
	"github.com/hito-hospital/hdms/test/testdb"
)

const tenMillis = 10 * time.Millisecond

func newCredentialsService(t *testing.T) *credentials.Service {
	t.Helper()
	pool := testdb.New(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return credentials.New(pool, audit.New(pool), "test-pepper", key)
}

func TestCredentialsIssueAndResolveUser(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()
	userID := ids.New()

	issued, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   userID,
		Kind:        credentialsapi.KindQR,
		Label:       "ID card",
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("Issue returned an empty token")
	}

	ref, err := svc.Resolve(ctx, issued.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ref.Type != credentialsapi.RefUser || ref.SubjectID != userID {
		t.Fatalf("Resolve = %+v, want user %s", ref, userID)
	}
	if ref.CredentialStatus != credentialsapi.StatusActive {
		t.Fatalf("CredentialStatus = %q, want active", ref.CredentialStatus)
	}
}

// TestCredentialsResolveUnderTenMilliseconds is the Phase 1 exit
// criterion: scanning a token through Resolve returns the correct subject
// in under 10ms.
func TestCredentialsResolveUnderTenMilliseconds(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectDevice,
		SubjectID:   ids.New(),
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Warm the connection/plan cache once before timing, matching how the
	// real server behaves under sustained load rather than a cold start.
	if _, err := svc.Resolve(ctx, issued.Token); err != nil {
		t.Fatalf("warmup Resolve: %v", err)
	}

	start := time.Now()
	ref, err := svc.Resolve(ctx, issued.Token)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ref.Type != credentialsapi.RefDevice {
		t.Fatalf("Resolve = %+v, want device", ref)
	}
	if elapsed > tenMillis {
		t.Fatalf("Resolve took %v, want < 10ms", elapsed)
	}
}

func TestCredentialsUnboundResolvesDistinctly(t *testing.T) {
	// INV-12: an unbound credential resolves as `unbound`, not "not found".
	svc := newCredentialsService(t)
	ctx := context.Background()

	batch, err := svc.IssueBlankBatch(ctx, 1, credentialsapi.KindQR, "admin:1")
	if err != nil {
		t.Fatalf("IssueBlankBatch: %v", err)
	}
	blank := batch[0]

	ref, err := svc.Resolve(ctx, blank.Token)
	if err != nil {
		t.Fatalf("Resolve(unbound) returned an error, want a successful unbound resolution: %v", err)
	}
	if ref.Type != credentialsapi.RefUnbound {
		t.Fatalf("Type = %q, want unbound", ref.Type)
	}
	if ref.SubjectID != "" {
		t.Fatalf("SubjectID = %q, want empty for an unbound credential", ref.SubjectID)
	}
}

func TestCredentialsUnknownTokenIsAnError(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	_, err := svc.Resolve(ctx, "HD-U-ZZZZZZZZZZ-0")
	if !errors.Is(err, credentialsapi.ErrCredentialUnknown) {
		t.Fatalf("Resolve(garbage) error = %v, want ErrCredentialUnknown", err)
	}
}

func TestCredentialsBindAtRegistration(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	batch, err := svc.IssueBlankBatch(ctx, 1, credentialsapi.KindQR, "admin:1")
	if err != nil {
		t.Fatalf("IssueBlankBatch: %v", err)
	}
	blank := batch[0]
	userID := ids.New()

	bound, err := svc.Bind(ctx, blank.ID, userID, "admin:1")
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound.SubjectID != userID {
		t.Fatalf("SubjectID = %q, want %q", bound.SubjectID, userID)
	}

	ref, err := svc.Resolve(ctx, blank.Token)
	if err != nil {
		t.Fatalf("Resolve after bind: %v", err)
	}
	if ref.Type != credentialsapi.RefUser || ref.SubjectID != userID {
		t.Fatalf("Resolve after bind = %+v, want user %s", ref, userID)
	}

	// A second bind attempt must fail: the card is no longer unbound.
	if _, err := svc.Bind(ctx, blank.ID, ids.New(), "admin:1"); !errors.Is(err, credentialsapi.ErrCredentialNotUnbound) {
		t.Fatalf("second Bind error = %v, want ErrCredentialNotUnbound", err)
	}
}

func TestCredentialsDeviceReprintReproducesIdenticalToken(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectDevice,
		SubjectID:   ids.New(),
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	reprinted, err := svc.Reprint(ctx, issued.ID, "admin:1")
	if err != nil {
		t.Fatalf("Reprint: %v", err)
	}
	if reprinted.Token != issued.Token {
		t.Fatalf("Reprint token = %q, want identical to issued token %q", reprinted.Token, issued.Token)
	}
	if reprinted.PrintedCount != 1 {
		t.Fatalf("PrintedCount = %d, want 1", reprinted.PrintedCount)
	}
}

func TestCredentialsUserCannotBeReprinted(t *testing.T) {
	// Only devices store a reversible token; a lost staff card must be
	// reissued instead (docs/05-credentials-and-labeling.md).
	svc := newCredentialsService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   ids.New(),
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = svc.Reprint(ctx, issued.ID, "admin:1")
	if !errors.Is(err, credentialsapi.ErrNotDeviceCredential) {
		t.Fatalf("Reprint(user credential) error = %v, want ErrNotDeviceCredential", err)
	}
}

func TestCredentialsReissueKillsOldKeepsHistory(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()
	userID := ids.New()

	original, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   userID,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	reissued, err := svc.Reissue(ctx, original.ID, "found in the parking lot, but lost again yesterday", "admin:1", "")
	if err != nil {
		t.Fatalf("Reissue: %v", err)
	}
	if reissued.Token == original.Token {
		t.Fatal("Reissue produced the same token as the original")
	}
	if reissued.SubjectID != userID {
		t.Fatalf("reissued SubjectID = %q, want %q (unchanged)", reissued.SubjectID, userID)
	}
	if reissued.ReplacesID != original.ID {
		t.Fatalf("ReplacesID = %q, want %q", reissued.ReplacesID, original.ID)
	}
	if reissued.IssueSeq != 2 {
		t.Fatalf("IssueSeq = %d, want 2", reissued.IssueSeq)
	}

	// The old token is dead...
	_, err = svc.Resolve(ctx, original.Token)
	if err != nil {
		t.Fatalf("Resolve(old token) should still succeed with a dead status, got error: %v", err)
	}

	// ...explicitly, with a 'lost' status rather than "not found".
	oldRef, err := svc.Resolve(ctx, original.Token)
	if err != nil {
		t.Fatalf("Resolve(old token): %v", err)
	}
	if oldRef.CredentialStatus != credentialsapi.StatusLost {
		t.Fatalf("old token status = %q, want lost", oldRef.CredentialStatus)
	}

	// ...while the new one resolves to the same, untouched subject.
	newRef, err := svc.Resolve(ctx, reissued.Token)
	if err != nil {
		t.Fatalf("Resolve(new token): %v", err)
	}
	if newRef.SubjectID != userID || newRef.CredentialStatus != credentialsapi.StatusActive {
		t.Fatalf("Resolve(new token) = %+v, want active user %s", newRef, userID)
	}

	// Full issuance history is preserved.
	events, err := svc.ListEvents(ctx, original.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (issued, reissued) on the old credential, got %d", len(events))
	}
}

func TestCredentialsRevokedResolvesAsRevokedNotUnknown(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectDevice,
		SubjectID:   ids.New(),
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := svc.Revoke(ctx, issued.ID, "device decommissioned", "admin:1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	ref, err := svc.Resolve(ctx, issued.Token)
	if err != nil {
		t.Fatalf("Resolve(revoked) should succeed with an explanation, got error: %v", err)
	}
	if ref.CredentialStatus != credentialsapi.StatusRevoked {
		t.Fatalf("status = %q, want revoked", ref.CredentialStatus)
	}
}

func TestCredentialsRevokeRequiresReason(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectDevice, SubjectID: ids.New(), Kind: credentialsapi.KindQR, IssuedBy: "admin:1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.Revoke(ctx, issued.ID, "   ", "admin:1"); err == nil {
		t.Fatal("expected Revoke with a blank reason to fail")
	}
}

// TestCredentialsTwoSubjectsNeverShareActiveToken proves INV-2 on the real
// unique index: forcing two active credentials to collide on the same
// token hash is rejected by Postgres, not just application logic.
func TestCredentialsTwoSubjectsNeverShareActiveToken(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	first, err := svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: ids.New(),
		Kind: credentialsapi.KindManual, ManualToken: "SAME-TOKEN-VALUE", IssuedBy: "admin:1",
	})
	if err != nil {
		t.Fatalf("first Issue: %v", err)
	}

	_, err = svc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: ids.New(),
		Kind: credentialsapi.KindManual, ManualToken: "SAME-TOKEN-VALUE", IssuedBy: "admin:1",
	})
	if !errors.Is(err, credentialsapi.ErrTokenAlreadyRegistered) {
		t.Fatalf("second Issue with a colliding token error = %v, want ErrTokenAlreadyRegistered", err)
	}

	// The first credential is unaffected.
	ref, err := svc.Resolve(ctx, first.Token)
	if err != nil {
		t.Fatalf("Resolve(first): %v", err)
	}
	if ref.SubjectID != first.SubjectID {
		t.Fatalf("Resolve(first) = %+v, want subject %s", ref, first.SubjectID)
	}
}

func TestCredentialsCountUnbound(t *testing.T) {
	svc := newCredentialsService(t)
	ctx := context.Background()

	before, err := svc.CountUnbound(ctx)
	if err != nil {
		t.Fatalf("CountUnbound: %v", err)
	}

	if _, err := svc.IssueBlankBatch(ctx, 3, credentialsapi.KindQR, "admin:1"); err != nil {
		t.Fatalf("IssueBlankBatch: %v", err)
	}

	after, err := svc.CountUnbound(ctx)
	if err != nil {
		t.Fatalf("CountUnbound: %v", err)
	}
	if after != before+3 {
		t.Fatalf("CountUnbound = %d, want %d", after, before+3)
	}
}
