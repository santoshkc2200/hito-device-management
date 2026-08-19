//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPCredentialIssueHistoryRevokeReissue(t *testing.T) {
	h := newTestHarness(t)

	userResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-3001",
		"fullName":   "B. Lama",
	})
	user := decodeBody[gen.User](t, userResp)

	issueResp := h.doJSON(t, http.MethodPost, "/v1/credentials", "", map[string]any{
		"subjectType": "user",
		"subjectId":   user.Id,
		"kind":        "qr",
	})
	if issueResp.StatusCode != http.StatusCreated {
		t.Fatalf("issue credential: status = %d", issueResp.StatusCode)
	}
	if cc := issueResp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("issue Cache-Control = %q, want no-store", cc)
	}
	issued := decodeBody[gen.IssuedCredential](t, issueResp)
	if issued.Token == "" {
		t.Fatal("issued credential must carry the plaintext token")
	}

	listResp := h.get(t, "/v1/credentials?subjectType=user&subjectId="+user.Id)
	list := decodeBody[gen.CredentialList](t, listResp)
	if len(list.Items) != 1 || list.Items[0].Id != issued.Id {
		t.Fatalf("list by subject: got %d items, want 1 matching", len(list.Items))
	}

	historyResp := h.get(t, "/v1/credentials/"+issued.Id+"/history")
	history := decodeBody[gen.CredentialEventList](t, historyResp)
	if len(history.Items) != 1 || history.Items[0].Kind != "issued" {
		t.Fatalf("history after issue = %+v, want one 'issued' event", history.Items)
	}

	revokeResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+issued.Id+"/revoke", "", map[string]any{
		"reason": "card damaged",
	})
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: status = %d", revokeResp.StatusCode)
	}
	revoked := decodeBody[gen.Credential](t, revokeResp)
	if revoked.Status != "revoked" {
		t.Fatalf("status after revoke = %q, want revoked", revoked.Status)
	}

	// A revoked credential cannot be reissued (Reissue requires 'active').
	reissueRevokedResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+issued.Id+"/reissue", "", map[string]any{
		"reason": "lost",
	})
	if reissueRevokedResp.StatusCode != http.StatusConflict {
		t.Fatalf("reissue a revoked credential: status = %d, want 409", reissueRevokedResp.StatusCode)
	}
	reissueRevokedResp.Body.Close()

	// Issue a fresh one and reissue it (lost-card flow): old dies, new works.
	freshResp := h.doJSON(t, http.MethodPost, "/v1/credentials", "", map[string]any{
		"subjectType": "user", "subjectId": user.Id, "kind": "qr",
	})
	fresh := decodeBody[gen.IssuedCredential](t, freshResp)

	reissueResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+fresh.Id+"/reissue", "", map[string]any{
		"reason": "lost card",
	})
	if reissueResp.StatusCode != http.StatusOK {
		t.Fatalf("reissue: status = %d", reissueResp.StatusCode)
	}
	replacement := decodeBody[gen.IssuedCredential](t, reissueResp)
	if replacement.Token == "" || replacement.Token == fresh.Token {
		t.Fatal("reissue must mint a genuinely new token")
	}
	if replacement.ReplacesId == nil || *replacement.ReplacesId != fresh.Id {
		t.Fatalf("replacement.replacesId = %v, want %q", replacement.ReplacesId, fresh.Id)
	}
}

func TestHTTPBlankBatchAndBind(t *testing.T) {
	h := newTestHarness(t)

	batchResp := h.doJSON(t, http.MethodPost, "/v1/credentials/blank-batch", "", map[string]any{
		"count": 3, "kind": "qr",
	})
	if batchResp.StatusCode != http.StatusCreated {
		t.Fatalf("blank batch: status = %d", batchResp.StatusCode)
	}
	batch := decodeBody[gen.IssueBlankBatchResponse](t, batchResp)
	if len(batch.Items) != 3 {
		t.Fatalf("blank batch size = %d, want 3", len(batch.Items))
	}

	userResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-4001", "fullName": "New Borrower",
	})
	user := decodeBody[gen.User](t, userResp)

	blank := batch.Items[0]
	bindResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+blank.Id+"/bind", "", map[string]any{
		"subjectId": user.Id,
	})
	if bindResp.StatusCode != http.StatusOK {
		t.Fatalf("bind: status = %d", bindResp.StatusCode)
	}
	bound := decodeBody[gen.Credential](t, bindResp)
	if bound.SubjectId == nil || *bound.SubjectId != user.Id {
		t.Fatalf("bound.subjectId = %v, want %q", bound.SubjectId, user.Id)
	}

	// Binding the same (now non-unbound) card again must fail.
	rebindResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+blank.Id+"/bind", "", map[string]any{
		"subjectId": user.Id,
	})
	if rebindResp.StatusCode != http.StatusConflict {
		t.Fatalf("rebind an already-bound card: status = %d, want 409", rebindResp.StatusCode)
	}
	rebindResp.Body.Close()
}

func TestHTTPUnboundCountAndResolve(t *testing.T) {
	h := newTestHarness(t)

	beforeResp := h.get(t, "/v1/credentials/unbound-count")
	before := decodeBody[gen.UnboundCountResponse](t, beforeResp)

	batchResp := h.doJSON(t, http.MethodPost, "/v1/credentials/blank-batch", "", map[string]any{
		"count": 2, "kind": "qr",
	})
	batch := decodeBody[gen.IssueBlankBatchResponse](t, batchResp)

	afterResp := h.get(t, "/v1/credentials/unbound-count")
	after := decodeBody[gen.UnboundCountResponse](t, afterResp)
	if after.Count != before.Count+2 {
		t.Fatalf("unbound count = %d, want %d", after.Count, before.Count+2)
	}

	blank := batch.Items[0]
	resolveResp := h.get(t, "/v1/credentials/resolve?token="+url.QueryEscape(blank.Token))
	if resolveResp.StatusCode != http.StatusOK {
		t.Fatalf("resolve unbound token: status = %d", resolveResp.StatusCode)
	}
	resolved := decodeBody[gen.ResolvedCredential](t, resolveResp)
	if resolved.Type != "unbound" || resolved.CredentialId != blank.Id || resolved.CredentialStatus != "active" {
		t.Fatalf("resolve unbound token = %+v", resolved)
	}

	unknownResp := h.get(t, "/v1/credentials/resolve?token=HD-U-0000000000-0")
	if unknownResp.StatusCode != http.StatusNotFound {
		t.Fatalf("resolve unknown token: status = %d, want 404", unknownResp.StatusCode)
	}
	unknownResp.Body.Close()

	userResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-5001", "fullName": "Resolve Target",
	})
	user := decodeBody[gen.User](t, userResp)
	h.doJSON(t, http.MethodPost, "/v1/credentials/"+blank.Id+"/bind", "", map[string]any{
		"subjectId": user.Id,
	}).Body.Close()

	boundResolveResp := h.get(t, "/v1/credentials/resolve?token="+url.QueryEscape(blank.Token))
	boundResolved := decodeBody[gen.ResolvedCredential](t, boundResolveResp)
	if boundResolved.Type != "user" || boundResolved.SubjectId == nil || *boundResolved.SubjectId != user.Id {
		t.Fatalf("resolve bound token = %+v", boundResolved)
	}
}
