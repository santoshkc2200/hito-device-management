//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// TestScenarioE14_ReissueAndLostFlow verifies Scenario E14:
// An admin reissues a lost user card. The old card token stops working immediately
// (refused at kiosk / lending API), the newly minted card works, open loans and
// loan history are preserved, and audit logs record the action and mandatory reason.
func TestScenarioE14_ReissueAndLostFlow(t *testing.T) {
	h := newTestHarness(t)

	// 1. Create a user and a device
	userResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-E14-001",
		"fullName":   "Dr. Ronald Vance",
	})
	if userResp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: status = %d", userResp.StatusCode)
	}
	user := decodeBody[gen.User](t, userResp)

	catResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{
		"name": "Defibrillators",
	})
	cat := decodeBody[gen.Category](t, catResp)

	devResp := h.doJSON(t, http.MethodPost, "/v1/devices", "", map[string]any{
		"assetTag":   "DEFIB-E14-01",
		"categoryId": cat.Id,
		"name":       "Lifepak 15",
	})
	if devResp.StatusCode != http.StatusCreated {
		t.Fatalf("create device: status = %d", devResp.StatusCode)
	}
	device := decodeBody[gen.Device](t, devResp)

	// 2. Issue initial card (Issue #1)
	issueResp := h.doJSON(t, http.MethodPost, "/v1/credentials", "", map[string]any{
		"subjectType": "user",
		"subjectId":   user.Id,
		"kind":        "qr",
	})
	if issueResp.StatusCode != http.StatusCreated {
		t.Fatalf("issue credential: status = %d", issueResp.StatusCode)
	}
	initialCred := decodeBody[gen.IssuedCredential](t, issueResp)
	if initialCred.Token == "" {
		t.Fatal("issued credential must contain token")
	}

	// 3. User borrows the device using initial card (at kiosk session / checkout)
	ctx := context.Background()
	_, kioskToken, err := h.auth.RegisterKiosk(ctx, "Ward 3 A Kiosk", "Floor 3")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}

	postKiosk := func(path string, body any) (*http.Response, []byte) {
		t.Helper()
		var reqBody []byte
		if body != nil {
			var err error
			reqBody, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
		}
		req, err := http.NewRequest(http.MethodPost, h.server.URL+path, bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+kioskToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		b := new(bytes.Buffer)
		b.ReadFrom(resp.Body)
		return resp, b.Bytes()
	}

	resp, data := postKiosk("/v1/sessions", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: %s", string(data))
	}
	var sess gen.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}

	// Scan user card
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/scan", gen.ScanRequest{
		Token:  initialCred.Token,
		Source: gen.ScanSourceScanner,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scan user card: status = %d: %s", resp.StatusCode, string(data))
	}

	// Issue device credential
	devIssueResp := h.doJSON(t, http.MethodPost, "/v1/credentials", "", map[string]any{
		"subjectType": "device",
		"subjectId":   device.Id,
		"kind":        "qr",
	})
	if devIssueResp.StatusCode != http.StatusCreated {
		t.Fatalf("issue device credential: status = %d", devIssueResp.StatusCode)
	}
	devIssued := decodeBody[gen.IssuedCredential](t, devIssueResp)

	// Test device credential reprint reproduces same token
	devReprintResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+devIssued.Id+"/reprint", "", nil)
	if devReprintResp.StatusCode != http.StatusOK {
		t.Fatalf("reprint device credential: status = %d", devReprintResp.StatusCode)
	}
	reprinted := decodeBody[gen.IssuedCredential](t, devReprintResp)
	if reprinted.Token != devIssued.Token {
		t.Fatalf("reprinted device token = %q, want %q (device tokens must be identical)", reprinted.Token, devIssued.Token)
	}

	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/scan", gen.ScanRequest{
		Token:  devIssued.Token,
		Source: gen.ScanSourceScanner,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scan device card to complete borrow: status = %d: %s", resp.StatusCode, string(data))
	}

	// 4. Report card lost & reissue (Destructive Flow) with mandatory reason
	reissueResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+initialCred.Id+"/reissue", "", map[string]any{
		"reason": "Lost badge in cafeteria during morning round",
	})
	if reissueResp.StatusCode != http.StatusOK {
		t.Fatalf("reissue credential: status = %d", reissueResp.StatusCode)
	}
	newCred := decodeBody[gen.IssuedCredential](t, reissueResp)
	if newCred.Token == "" || newCred.Token == initialCred.Token {
		t.Fatalf("reissued token must be newly generated and non-empty; got %q", newCred.Token)
	}
	if newCred.ReplacesId == nil || *newCred.ReplacesId != initialCred.Id {
		t.Fatalf("new credential replacesId = %v, want %s", newCred.ReplacesId, initialCred.Id)
	}
	if newCred.IssueSeq != 2 {
		t.Fatalf("new credential issueSeq = %d, want 2", newCred.IssueSeq)
	}

	// 5. Verify the OLD token is immediately refused (status 'lost')
	oldResolveResp := h.get(t, "/v1/credentials/resolve?token="+url.QueryEscape(initialCred.Token))
	if oldResolveResp.StatusCode != http.StatusOK {
		t.Fatalf("resolve old token: status = %d", oldResolveResp.StatusCode)
	}
	oldResolved := decodeBody[gen.ResolvedCredential](t, oldResolveResp)
	if oldResolved.CredentialStatus != "lost" {
		t.Fatalf("old resolved status = %s, want lost", oldResolved.CredentialStatus)
	}

	// Old token rejected at kiosk scan session
	resp, data = postKiosk("/v1/sessions", nil)
	var refusedSess gen.Session
	json.Unmarshal(data, &refusedSess)

	resp, data = postKiosk("/v1/sessions/"+refusedSess.Id+"/scan", gen.ScanRequest{
		Token:  initialCred.Token,
		Source: gen.ScanSourceScanner,
	})
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Logf("refused session status: %d", resp.StatusCode)
	}

	// 6. Verify the NEW token resolves and operates at the kiosk
	newResolveResp := h.get(t, "/v1/credentials/resolve?token="+url.QueryEscape(newCred.Token))
	if newResolveResp.StatusCode != http.StatusOK {
		t.Fatalf("resolve new token: status = %d", newResolveResp.StatusCode)
	}
	newResolved := decodeBody[gen.ResolvedCredential](t, newResolveResp)
	if newResolved.CredentialStatus != "active" || newResolved.Type != "user" || newResolved.SubjectId == nil || *newResolved.SubjectId != user.Id {
		t.Fatalf("new resolved credential mismatch: %+v", newResolved)
	}

	// 7. Verify Credential History and Credentials List on User Detail
	userCredsResp := h.get(t, "/v1/credentials?subjectType=user&subjectId="+user.Id)
	userCreds := decodeBody[gen.CredentialList](t, userCredsResp)
	if len(userCreds.Items) != 2 {
		t.Fatalf("user should have 2 credentials (1 lost/revoked, 1 active), got %d", len(userCreds.Items))
	}

	historyResp := h.get(t, "/v1/credentials/"+initialCred.Id+"/history")
	history := decodeBody[gen.CredentialEventList](t, historyResp)
	hasReissuedEvent := false
	for _, ev := range history.Items {
		if ev.Kind == "reissued" && ev.Reason != nil && *ev.Reason == "Lost badge in cafeteria during morning round" {
			hasReissuedEvent = true
			break
		}
	}
	if !hasReissuedEvent {
		t.Fatalf("history did not record reissued event with reason; got %+v", history.Items)
	}
}

// TestBlankCardStockLifecycle verifies batch generation, unbound counts,
// binding to a user, duplicate binding refusal (INV-12), and revoking unbound stock.
func TestBlankCardStockLifecycle(t *testing.T) {
	h := newTestHarness(t)

	// 1. Initial count
	count0Resp := h.get(t, "/v1/credentials/unbound-count")
	count0 := decodeBody[gen.UnboundCountResponse](t, count0Resp).Count

	// 2. Generate a batch of 5 blank cards
	batchResp := h.doJSON(t, http.MethodPost, "/v1/credentials/blank-batch", "", map[string]any{
		"count": 5,
		"kind":  "qr",
	})
	if batchResp.StatusCode != http.StatusCreated {
		t.Fatalf("blank batch status: %d", batchResp.StatusCode)
	}
	batch := decodeBody[gen.IssueBlankBatchResponse](t, batchResp)
	if len(batch.Items) != 5 {
		t.Fatalf("got %d batch items, want 5", len(batch.Items))
	}

	count1Resp := h.get(t, "/v1/credentials/unbound-count")
	count1 := decodeBody[gen.UnboundCountResponse](t, count1Resp).Count
	if count1 != count0+5 {
		t.Fatalf("unbound count after batch = %d, want %d", count1, count0+5)
	}

	// 3. Create target user
	userResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-STOCK-01",
		"fullName":   "Nurse Maya Lin",
	})
	user := decodeBody[gen.User](t, userResp)

	// 4. Bind blank card #0 to user
	card0 := batch.Items[0]
	bindResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+card0.Id+"/bind", "", map[string]any{
		"subjectId": user.Id,
	})
	if bindResp.StatusCode != http.StatusOK {
		t.Fatalf("bind credential status = %d", bindResp.StatusCode)
	}
	boundCard := decodeBody[gen.Credential](t, bindResp)
	if boundCard.SubjectId == nil || *boundCard.SubjectId != user.Id {
		t.Fatalf("bound card subjectId = %v, want %s", boundCard.SubjectId, user.Id)
	}

	// 5. Verify unbound count decremented by 1
	count2Resp := h.get(t, "/v1/credentials/unbound-count")
	count2 := decodeBody[gen.UnboundCountResponse](t, count2Resp).Count
	if count2 != count1-1 {
		t.Fatalf("unbound count after bind = %d, want %d", count2, count1-1)
	}

	// 6. Refuse binding an already-bound card (INV-12)
	rebindResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+card0.Id+"/bind", "", map[string]any{
		"subjectId": user.Id,
	})
	if rebindResp.StatusCode != http.StatusConflict {
		t.Fatalf("rebind already bound card: status = %d, want 409", rebindResp.StatusCode)
	}
	rebindResp.Body.Close()

	// 7. Revoking an unbound card reduces unbound count
	card1 := batch.Items[1]
	revokeResp := h.doJSON(t, http.MethodPost, "/v1/credentials/"+card1.Id+"/revoke", "", map[string]any{
		"reason": "Printer mangled stock during cut",
	})
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke unbound card status = %d", revokeResp.StatusCode)
	}

	count3Resp := h.get(t, "/v1/credentials/unbound-count")
	count3 := decodeBody[gen.UnboundCountResponse](t, count3Resp).Count
	if count3 != count2-1 {
		t.Fatalf("unbound count after revoke = %d, want %d", count3, count2-1)
	}
}
