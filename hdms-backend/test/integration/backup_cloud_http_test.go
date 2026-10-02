//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/test/cloudfake"
)

// cloudProviders lets a test reach the fake sign-in server the harness built
// for its database pool.
var cloudProviders sync.Map // *db.Pool -> *cloudfake.Provider

func newTestCloud(t *testing.T, pool *db.Pool, key []byte) *backup.CloudService {
	t.Helper()
	p := cloudfake.New(t)
	cloudProviders.Store(pool, p)
	t.Cleanup(func() { cloudProviders.Delete(pool) })
	return &backup.CloudService{Q: pool.Pool, Key: key, Flow: p.Flow(), Now: p.Now}
}

func providerFor(t *testing.T, pool *db.Pool) *cloudfake.Provider {
	t.Helper()
	v, ok := cloudProviders.Load(pool)
	if !ok {
		t.Fatal("no fake provider for this harness")
	}
	return v.(*cloudfake.Provider)
}

func TestHTTPCloudAccountSignInAndDestination(t *testing.T) {
	h := newTestHarness(t)
	p := providerFor(t, h.pool)
	const secret = "s3cret-value"

	resp := h.post(t, "/v1/backup/cloud-accounts", gen.BackupCloudAccountInput{
		Provider: "google_drive", Name: "Hospital Drive", ClientId: "cid-1", ClientSecret: strPtr(secret),
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	si := decodeBody[gen.BackupCloudSignIn](t, resp)
	if si.UserCode != "ABCD-EFGH" || si.Id == "" {
		t.Fatalf("sign-in = %+v", si)
	}

	// The list is readable and carries no secret.
	listResp := h.get(t, "/v1/backup/cloud-accounts")
	raw, _ := io.ReadAll(listResp.Body)
	_ = listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"pending"`) || strings.Contains(string(raw), secret) {
		t.Fatalf("list = %d %s", listResp.StatusCode, raw)
	}

	// Finish the sign-in.
	p.Answer("approve")
	p.Advance(6 * time.Second)
	acct := decodeBody[gen.BackupCloudAccount](t, h.get(t, "/v1/backup/cloud-accounts/"+si.Id))
	if acct.Status != "connected" || acct.AccountEmail == nil || *acct.AccountEmail != "drive-owner@example.test" {
		t.Fatalf("account = %+v", acct)
	}

	// A cloud destination: account + folder, not a path.
	resp = h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{
		Name: "Drive", CloudAccountId: &si.Id, Folder: strPtr("hdms-backups"), RetentionVersions: 2,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create destination status = %d", resp.StatusCode)
	}
	d := decodeBody[gen.BackupDestination](t, resp)
	if d.Provider != "google_drive" || d.Folder == nil || *d.Folder != "hdms-backups" || d.CloudAccountId == nil || *d.CloudAccountId != si.Id {
		t.Fatalf("destination = %+v", d)
	}
	for name, body := range map[string]gen.BackupDestinationInput{
		"both":       {Name: "x", Target: strPtr("/tmp"), CloudAccountId: &si.Id, Folder: strPtr("f"), RetentionVersions: 2},
		"neither":    {Name: "x", RetentionVersions: 2},
		"no folder":  {Name: "x", CloudAccountId: &si.Id, RetentionVersions: 2},
		"bad folder": {Name: "x", CloudAccountId: &si.Id, Folder: strPtr("a:b"), RetentionVersions: 2},
	} {
		if r := h.post(t, "/v1/backup/destinations", body); r.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", name, r.StatusCode)
		}
	}

	// In use: refused. Then free: allowed.
	if r := h.doJSON(t, http.MethodDelete, "/v1/backup/cloud-accounts/"+si.Id, "", nil); r.StatusCode != http.StatusConflict {
		t.Fatalf("delete in use status = %d, want 409", r.StatusCode)
	}
	if r := h.doJSON(t, http.MethodDelete, "/v1/backup/destinations/"+d.Id, "", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete destination status = %d", r.StatusCode)
	}
	if r := h.doJSON(t, http.MethodDelete, "/v1/backup/cloud-accounts/"+si.Id, "", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete account status = %d", r.StatusCode)
	}

	// Review Focus 4: the secret is nowhere in the audit trail.
	var leaked int
	_ = h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE payload::text LIKE '%' || $1 || '%' OR payload::text LIKE '%at-1%'`, secret).Scan(&leaked)
	if leaked != 0 {
		t.Fatalf("%d audit event(s) contain a secret", leaked)
	}
	var created, connected int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'backup.cloud_account_created'`).Scan(&created)
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'backup.cloud_account_connected'`).Scan(&connected)
	if created != 1 || connected != 1 {
		t.Fatalf("audit events created=%d connected=%d, want 1 and 1", created, connected)
	}
}

func TestHTTPCloudAccountProviderProblems(t *testing.T) {
	h := newTestHarness(t)
	p := providerFor(t, h.pool)

	p.RejectStart("invalid_client", "The OAuth client was not found.")
	r := h.post(t, "/v1/backup/cloud-accounts", gen.BackupCloudAccountInput{Provider: "google_drive", Name: "n", ClientId: "bad", ClientSecret: strPtr("s")})
	if r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("rejected client status = %d, want 422", r.StatusCode)
	}
	r = h.post(t, "/v1/backup/cloud-accounts", gen.BackupCloudAccountInput{Provider: "google_drive", Name: "n", ClientId: "c"})
	if r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("Google without a secret status = %d, want 422", r.StatusCode)
	}
	if r := h.get(t, "/v1/backup/cloud-accounts/00000000-0000-4000-8000-000000000000"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown account status = %d, want 404", r.StatusCode)
	}
}
