# Cloud backup destinations (Google Drive and OneDrive) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An administrator connects a Google Drive or OneDrive account in the Backups console, adds it as a backup destination, and — on a lost server — `install.sh --restore --cloud=…` pulls the backups back from the cloud.

**Architecture:** The API runs the OAuth device-authorization flow (RFC 8628) and stores the client secret and tokens encrypted with `HDMS_CREDENTIAL_ENC_KEY` in a new `backup_cloud_accounts` table. The worker renders a private `rclone.conf` into a `0700` temp directory for each run, points restic at it with `RCLONE_CONFIG`, uploads `hdms-recovery.bin` beside the cloud repository with `rclone rcat`, writes any token rclone refreshed back to the database, and deletes the file. A cloud destination is an ordinary `backup_destinations` row (`kind = 'rclone'`) that references an account and a folder name, so fan-out, retention, snapshots cache, verify and test already work. Disaster recovery from the cloud reuses the existing `install.sh --restore` flow: a new `hdms-cli cloud fetch` signs in on the terminal and `rclone copy`s the folder into the drives folder, after which it is just another folder of backups.

**Tech Stack:** Go 1.24, pgx v5 + sqlc, oapi-codegen, restic + rclone (already in the worker image), React 19 + TanStack Query v5, vitest, bash.

**Spec:** `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md` (sections "Cloud sign-in", "Data model", "API", "Console"); disaster-recovery context in `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md`. Decisions below that depart from or add to the spec were made while planning on 2026-10-02.

## Decisions not in the spec

- **Cloud disaster recovery goes through `install.sh`, not a Cloud source on `/recovery`.** On a lost server the recovery page refuses to restore (`keys_mismatch`) until `install.sh --restore` has put the four secrets from `hdms-recovery.bin` into the env file, and `install.sh` only read local folders. So `install.sh --restore --cloud=google|onedrive` signs in on the terminal, downloads the folder under the drives folder, and the unchanged rest of the script and `/recovery` take over. The user chose this over a second source on the page.
- **API paths are `/v1/backup/cloud-accounts…`**, matching the rest of the console (the spec said `/v1/admin/backup`).
- **OneDrive gets a `tenant` field** (default `common`). A single-tenant Entra app registration rejects `/common/`, and rclone needs the same tenant in `token_url` to refresh. OneDrive also needs `drive_id`/`drive_type` in the rclone config; both are read once at sign-in from Microsoft Graph.
- **The folder is typed, not browsed.** Google's `drive.file` scope only shows files HDMS created, so there is nothing to browse. HDMS creates the folder (restic/rclone make it on first use).
- **Cloud destinations keep the existing layout**: `<folder>/repo` is the restic repository, `<folder>/hdms-recovery.bin` sits beside it. `backup_destinations.target` is set to `cloud:<account id>/<folder>` so the existing unique index `(kind, target)` stops two destinations sharing a folder.
- **Legacy hand-typed `rclone` destinations are left alone** (the API never created them). Only destinations with a `cloud_account_id` use the session; `HDMS_RCLONE_CONFIG` still applies to the legacy ones.
- **Revocation is detected from rclone's error text** (`invalid_grant`, `couldn't fetch token`), which moves the account to `revoked` so the console offers Reconnect.
- **`hdms-cli backup` (the manual CLI) does not back up to cloud destinations**; they report "cloud destinations are backed up by the worker only" and the run is `degraded`. The worker is the only process that holds a session.

## Global Constraints

- Google scope is exactly `https://www.googleapis.com/auth/drive.file`; OneDrive scope is exactly `Files.ReadWrite offline_access`.
- Client secret, device code and tokens are sealed with `backup.Encrypt(cfg.CredentialEncKey, …)`. They never appear in argv, logs, API responses, audit payloads or the Postgres `backup_requests.detail`.
- The rendered `rclone.conf` is written `0600` into a directory created by `os.MkdirTemp` (`0700`) and removed after every run, success or failure.
- Migrations after `0028`; this plan uses `0029`. Every new table gets `GRANT SELECT, INSERT, UPDATE, DELETE … TO hdms_app`.
- Every new route is classified in **both** `internal/platform/auth/roles.go` and `internal/platform/auth/kioskscope.go` (an unclassified route answers 403 — this bit the console-restore work).
- Every user-visible string is in `i18n/en.ts` and `i18n/ja.ts` (`no-literals.test.ts`); every TanStack table memoizes `columns` and `data`.
- Tests that need Postgres are tagged `integration` and run under `go test -tags=integration ./test/...`. Do not run integration and frontend suites in parallel (false timeouts).
- Gate before each commit: `gofmt -l` empty, `golangci-lint run ./...`, `go test ./...`; integration tasks add `go test -race -tags=integration ./test/... -run <name>`. The code blocks in this plan are not guaranteed gofmt-aligned: run `gofmt -w` on every Go file you touched before the gate.

## Review Focus

The spec says what to build, not what a real Drive or OneDrive does to it. These five are most likely to bite, most likely first; each has a test in the named task.

1. **A token that stops working mid-life** (user removes app access, password reset, Google test-mode 7-day expiry): the account must move to `revoked` with a readable message, that destination's `last_error` must say so, the other destinations in the same run must still be copied, and the run is `degraded`, not `failed`. → Task 3.
2. **The rendered rclone config outliving the run** (restic fails, worker killed mid-run is out of scope): the file holds live tokens; it must be `0600`, in a `0700` directory, and gone after `Close` even when every call failed. → Task 3.
3. **Polling the provider too fast or forever**: a second poll inside the provider's interval must make no outbound call; `slow_down` must lengthen the interval; an expired code must flip the account to `expired`; a network error must leave the sign-in `pending` and retryable. → Task 2.
4. **Secrets leaking out of the server**: the client secret and tokens must not be in any API response, any audit payload, or the `install.sh` docker command lines. → Tasks 5 and 7.
5. **Restoring over a working database must keep cloud accounts** (foreign key from destinations to accounts; the copy-forward list must insert accounts first), or the next restore silently drops the cloud copy. → Task 4.

Covered inside tasks, not repeated here: folder-name validation, deleting an account that a destination uses (refused), Google requiring a client secret, unknown providers.

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/platform/backup/deviceflow.go` (new) | Provider constants, RFC 8628 client: `Start`, `Exchange`, `Profile`, default endpoints, `Token.RcloneJSON` |
| `hdms-backend/test/cloudfake/cloudfake.go` (new) | Fake Google/Microsoft sign-in server with a controllable clock, used by unit and integration tests |
| `hdms-backend/migrations/0029_cloud_accounts.sql` (new) | `backup_cloud_accounts`; `backup_destinations.cloud_account_id`, `folder` |
| `hdms-backend/internal/platform/backup/cloud_accounts.go` (new) | `CloudService`: create/reconnect/poll/list/get/delete accounts, worker-side `credentials`, `StoreToken`, `SetStatus` |
| `hdms-backend/internal/platform/backup/rcloneconf.go` (new) | `RenderRcloneConfig`, `ParseRcloneTokens`, `Rclone` runner, `CloudSession`, `IsTokenRevoked`, `PutRecoveryBundle` |
| `hdms-backend/internal/platform/backup/store.go`, `destinations.go`, `dest.go` (modify) | `Destination` gains `CloudAccountID`, `Folder`; `CreateCloudDestination`; `CleanCloudFolder`; cloud `Resolve` |
| `hdms-backend/internal/platform/backup/runner.go`, `executor.go` (modify) | Use the session for cloud destinations; bundle upload; revocation handling |
| `hdms-backend/internal/platform/recovery/copyforward.go` (modify) | Accounts are copied forward before destinations |
| `hdms-backend/api/openapi.yaml`, `internal/apiserver/backup_cloud.go` (new), `backup.go`, `server.go`, `internal/platform/auth/roles.go`, `kioskscope.go` | Cloud-account endpoints; cloud destination create |
| `hdms-frontend/apps/admin/src/components/backups/*` | Accounts section, connect dialog, wizard cloud path |
| `hdms-backend/cmd/hdms-cli/cloud.go` (new), `main.go` | `hdms-cli cloud fetch` |
| `deploy/production/install.sh`, `install_test.sh` | `--restore --cloud=…` |
| `deploy/production/compose.yaml`, `docker-compose.staging.yml`, `docs/runbooks/*` | tmpfs for the worker's temp dir; IT and recovery runbooks |

---

### Task 1: Device-flow client and the fake sign-in server

**Files:**
- Create: `hdms-backend/internal/platform/backup/deviceflow.go`
- Create: `hdms-backend/test/cloudfake/cloudfake.go`
- Test: `hdms-backend/internal/platform/backup/deviceflow_test.go`

**Interfaces:**
- Produces (package `backup`): `ProviderGoogleDrive = "google_drive"`, `ProviderOneDrive = "onedrive"`, `ErrInvalidCloudAccount`, `Endpoints`, `DefaultEndpoints(provider, tenant)`, `DeviceFlow{HTTP *http.Client; Endpoints func(provider, tenant string) (Endpoints, error); Now func() time.Time}` with `Start(ctx, provider, clientID, tenant) (DeviceCode, error)`, `Exchange(ctx, provider, clientID, secret, tenant, deviceCode) (Token, error)`, `Profile(ctx, provider, tenant, accessToken) (Profile, error)`; `DeviceCode{DeviceCode, UserCode, VerificationURI string; ExpiresIn, Interval time.Duration}`; `Token{AccessToken, RefreshToken, TokenType string; Expiry time.Time}` with `RcloneJSON() (string, error)`; `Profile{Email, DriveID, DriveType string}`; sentinels `ErrAuthorizationPending`, `ErrSlowDown`, `ErrCodeExpired`, `ErrAccessDenied`, `ErrProviderRejected`, `ErrProviderUnreachable`.
- Produces (package `cloudfake`): `New(t testing.TB) *Provider`; `(*Provider).Flow() *backup.DeviceFlow`, `.Now() time.Time`, `.Advance(d)`, `.Answer(string)` (`pending|slow_down|approve|expired|denied|down`), `.RejectStart(code, description string)`, `.TokenCalls() int`, `.LastForm() url.Values`.

- [ ] **Step 1: Write the fake sign-in server**

Create `hdms-backend/test/cloudfake/cloudfake.go`:

```go
// Package cloudfake plays Google's and Microsoft's sign-in servers so the
// device flow can be tested without a network or a cloud account.
package cloudfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

const DeviceCode = "dev-123"

type Provider struct {
	Server *httptest.Server

	mu          sync.Mutex
	now         time.Time
	answer      string
	rejectCode  string
	rejectDesc  string
	tokenCalls  int
	lastForm    url.Values
	profileDown bool
}

// New starts a fake provider. Its clock starts at a fixed instant and only
// moves when Advance is called, so poll-interval behaviour is deterministic.
func New(t testing.TB) *Provider {
	t.Helper()
	p := &Provider{now: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), answer: "pending"}
	mux := http.NewServeMux()
	mux.HandleFunc("/device", p.device)
	mux.HandleFunc("/token", p.token)
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		if p.isProfileDown() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"emailAddress": "drive-owner@example.test"}})
	})
	mux.HandleFunc("/drive", func(w http.ResponseWriter, r *http.Request) {
		if p.isProfileDown() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "b!abc", "driveType": "business",
			"owner": map[string]any{"user": map[string]any{"email": "od-owner@example.test"}},
		})
	})
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Server.Close)
	return p
}

func (p *Provider) Flow() *backup.DeviceFlow {
	return &backup.DeviceFlow{
		HTTP: p.Server.Client(),
		Now:  p.Now,
		Endpoints: func(string, string) (backup.Endpoints, error) {
			return backup.Endpoints{
				DeviceURL: p.Server.URL + "/device", TokenURL: p.Server.URL + "/token",
				ProfileURL: p.Server.URL + "/about", DriveURL: p.Server.URL + "/drive", Scope: "test-scope",
			}, nil
		},
	}
}

func (p *Provider) Now() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.now
}

func (p *Provider) Advance(d time.Duration) {
	p.mu.Lock()
	p.now = p.now.Add(d)
	p.mu.Unlock()
}

// Answer sets what the token endpoint says next: pending, slow_down, approve,
// expired, denied or down (HTTP 503).
func (p *Provider) Answer(a string) {
	p.mu.Lock()
	p.answer = a
	p.mu.Unlock()
}

func (p *Provider) RejectStart(code, description string) {
	p.mu.Lock()
	p.rejectCode, p.rejectDesc = code, description
	p.mu.Unlock()
}

func (p *Provider) ProfileDown(down bool) {
	p.mu.Lock()
	p.profileDown = down
	p.mu.Unlock()
}

func (p *Provider) isProfileDown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.profileDown
}

func (p *Provider) TokenCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokenCalls
}

func (p *Provider) LastForm() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastForm
}

func (p *Provider) device(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	code, desc := p.rejectCode, p.rejectDesc
	p.mu.Unlock()
	if code != "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": code, "error_description": desc})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": DeviceCode, "user_code": "ABCD-EFGH",
		"verification_url": "https://example.test/device", "expires_in": 600, "interval": 5,
	})
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p.mu.Lock()
	p.tokenCalls++
	p.lastForm = r.PostForm
	answer := p.answer
	p.mu.Unlock()
	if r.PostForm.Get("device_code") != DeviceCode {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	switch answer {
	case "approve":
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "at-1", "refresh_token": "rt-1", "token_type": "Bearer", "expires_in": 3600,
		})
	case "slow_down":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "slow_down"})
	case "expired":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "expired_token"})
	case "denied":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "access_denied"})
	case "down":
		http.Error(w, "down", http.StatusServiceUnavailable)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "authorization_pending"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 2: Write the failing test**

Create `hdms-backend/internal/platform/backup/deviceflow_test.go`:

```go
package backup_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/cloudfake"
)

func TestDeviceFlowStartExchangeAndProfile(t *testing.T) {
	p := cloudfake.New(t)
	f := p.Flow()
	ctx := context.Background()

	dc, err := f.Start(ctx, backup.ProviderGoogleDrive, "client-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if dc.DeviceCode != cloudfake.DeviceCode || dc.UserCode != "ABCD-EFGH" ||
		dc.VerificationURI != "https://example.test/device" || dc.Interval != 5*time.Second || dc.ExpiresIn != 10*time.Minute {
		t.Fatalf("device code = %+v", dc)
	}

	for _, c := range []struct {
		answer string
		want   error
	}{
		{"pending", backup.ErrAuthorizationPending},
		{"slow_down", backup.ErrSlowDown},
		{"expired", backup.ErrCodeExpired},
		{"denied", backup.ErrAccessDenied},
		{"down", backup.ErrProviderUnreachable},
	} {
		p.Answer(c.answer)
		if _, err := f.Exchange(ctx, backup.ProviderGoogleDrive, "client-1", "secret-1", "", cloudfake.DeviceCode); !errors.Is(err, c.want) {
			t.Fatalf("answer %q: err = %v, want %v", c.answer, err, c.want)
		}
	}

	p.Answer("approve")
	tok, err := f.Exchange(ctx, backup.ProviderGoogleDrive, "client-1", "secret-1", "", cloudfake.DeviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" || !tok.Expiry.Equal(p.Now().Add(time.Hour)) {
		t.Fatalf("token = %+v", tok)
	}
	if got := p.LastForm().Get("client_secret"); got != "secret-1" {
		t.Fatalf("client_secret sent = %q", got)
	}
	if got := p.LastForm().Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Fatalf("grant_type = %q", got)
	}
	js, err := tok.RcloneJSON()
	if err != nil || !strings.Contains(js, `"access_token":"at-1"`) || !strings.Contains(js, `"expiry":"2026-10-02T10:00:00Z"`) {
		t.Fatalf("rclone json = %s (%v)", js, err)
	}

	g, err := f.Profile(ctx, backup.ProviderGoogleDrive, "", "at-1")
	if err != nil || g.Email != "drive-owner@example.test" {
		t.Fatalf("google profile = %+v (%v)", g, err)
	}
	o, err := f.Profile(ctx, backup.ProviderOneDrive, "contoso", "at-1")
	if err != nil || o.DriveID != "b!abc" || o.DriveType != "business" || o.Email != "od-owner@example.test" {
		t.Fatalf("onedrive profile = %+v (%v)", o, err)
	}
}

func TestExchangeOmitsAnEmptyClientSecret(t *testing.T) {
	p := cloudfake.New(t)
	p.Answer("approve")
	if _, err := p.Flow().Exchange(context.Background(), backup.ProviderOneDrive, "c", "", "common", cloudfake.DeviceCode); err != nil {
		t.Fatal(err)
	}
	if _, present := p.LastForm()["client_secret"]; present {
		t.Fatal("OneDrive is a public client; client_secret must not be sent")
	}
}

func TestStartRejectedByTheProviderCarriesItsReason(t *testing.T) {
	p := cloudfake.New(t)
	p.RejectStart("invalid_client", "The OAuth client was not found.")
	_, err := p.Flow().Start(context.Background(), backup.ProviderGoogleDrive, "nope", "")
	if !errors.Is(err, backup.ErrProviderRejected) || !strings.Contains(err.Error(), "The OAuth client was not found.") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAcceptsMicrosoftsVerificationUriField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device_code":"d","user_code":"U","verification_uri":"https://microsoft.test/devicelogin","expires_in":900}`))
	}))
	defer srv.Close()
	f := &backup.DeviceFlow{HTTP: srv.Client(), Endpoints: func(string, string) (backup.Endpoints, error) {
		return backup.Endpoints{DeviceURL: srv.URL}, nil
	}}
	dc, err := f.Start(context.Background(), backup.ProviderOneDrive, "c", "common")
	if err != nil || dc.VerificationURI != "https://microsoft.test/devicelogin" || dc.Interval != 5*time.Second {
		t.Fatalf("dc = %+v (%v)", dc, err)
	}
}

func TestDefaultEndpoints(t *testing.T) {
	g, err := backup.DefaultEndpoints(backup.ProviderGoogleDrive, "")
	if err != nil || g.Scope != "https://www.googleapis.com/auth/drive.file" {
		t.Fatalf("google = %+v (%v)", g, err)
	}
	o, err := backup.DefaultEndpoints(backup.ProviderOneDrive, "contoso.onmicrosoft.com")
	if err != nil || o.Scope != "Files.ReadWrite offline_access" ||
		o.TokenURL != "https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token" {
		t.Fatalf("onedrive = %+v (%v)", o, err)
	}
	if _, err := backup.DefaultEndpoints("dropbox", ""); !errors.Is(err, backup.ErrInvalidCloudAccount) {
		t.Fatalf("unknown provider err = %v", err)
	}
}
```

- [ ] **Step 3: Run it to see it fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'DeviceFlow|Exchange|StartRejected|StartAccepts|DefaultEndpoints'`
Expected: FAIL — compile error, `backup.DeviceFlow` undefined.

- [ ] **Step 4: Write the device-flow client**

Create `hdms-backend/internal/platform/backup/deviceflow.go`:

```go
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Device authorization grant (RFC 8628). The site address is a .local name
// that Google refuses as a web redirect URI, so the administrator signs in on
// any device with a short code instead.

const (
	ProviderGoogleDrive = "google_drive"
	ProviderOneDrive    = "onedrive"
)

var (
	ErrInvalidCloudAccount  = errors.New("backup: invalid cloud account")
	ErrAuthorizationPending = errors.New("backup: sign-in is not finished yet")
	ErrSlowDown             = errors.New("backup: the provider asked for slower polling")
	ErrCodeExpired          = errors.New("backup: the sign-in code expired")
	ErrAccessDenied         = errors.New("backup: sign-in was declined")
	// ErrProviderRejected: the provider answered and said no (wrong client ID
	// or secret, wrong tenant). Retrying the same request will not help.
	ErrProviderRejected = errors.New("backup: the provider rejected the request")
	// ErrProviderUnreachable: no usable answer (network, timeout, 5xx).
	ErrProviderUnreachable = errors.New("backup: cannot reach the sign-in provider")
)

// Endpoints are one provider's URLs. DefaultEndpoints has the real ones;
// tests substitute a fake server through DeviceFlow.Endpoints.
type Endpoints struct {
	DeviceURL  string
	TokenURL   string
	ProfileURL string // Google: Drive "about", for the account's email
	DriveURL   string // OneDrive: the signed-in user's drive
	Scope      string
}

func DefaultEndpoints(provider, tenant string) (Endpoints, error) {
	switch provider {
	case ProviderGoogleDrive:
		return Endpoints{
			DeviceURL:  "https://oauth2.googleapis.com/device/code",
			TokenURL:   "https://oauth2.googleapis.com/token",
			ProfileURL: "https://www.googleapis.com/drive/v3/about?fields=user(emailAddress)",
			// drive.file limits HDMS to files it created itself, which is all a
			// backup needs; the device flow does not allow the broader scope.
			Scope: "https://www.googleapis.com/auth/drive.file",
		}, nil
	case ProviderOneDrive:
		if tenant == "" {
			tenant = "common"
		}
		base := "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/"
		return Endpoints{
			DeviceURL: base + "devicecode",
			TokenURL:  base + "token",
			DriveURL:  "https://graph.microsoft.com/v1.0/me/drive",
			Scope:     "Files.ReadWrite offline_access",
		}, nil
	}
	return Endpoints{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidCloudAccount, provider)
}

type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
}

// RcloneJSON renders the token the way rclone stores it in its config file.
func (t Token) RcloneJSON() (string, error) {
	typ := t.TokenType
	if typ == "" {
		typ = "Bearer"
	}
	b, err := json.Marshal(struct {
		AccessToken  string    `json:"access_token"`
		TokenType    string    `json:"token_type"`
		RefreshToken string    `json:"refresh_token"`
		Expiry       time.Time `json:"expiry"`
	}{t.AccessToken, typ, t.RefreshToken, t.Expiry.UTC()})
	return string(b), err
}

// Profile is what is read from the provider right after sign-in: the account
// email for the console, and for OneDrive the drive rclone must be told about.
type Profile struct {
	Email     string
	DriveID   string
	DriveType string
}

type DeviceFlow struct {
	HTTP      *http.Client
	Endpoints func(provider, tenant string) (Endpoints, error)
	Now       func() time.Time
}

var defaultHTTP = &http.Client{Timeout: 20 * time.Second}

func (f *DeviceFlow) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return defaultHTTP
}

func (f *DeviceFlow) now() time.Time {
	if f.Now != nil {
		return f.Now().UTC()
	}
	return time.Now().UTC()
}

func (f *DeviceFlow) endpoints(provider, tenant string) (Endpoints, error) {
	if f.Endpoints != nil {
		return f.Endpoints(provider, tenant)
	}
	return DefaultEndpoints(provider, tenant)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (f *DeviceFlow) do(req *http.Request) (int, []byte, error) {
	resp, err := f.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return resp.StatusCode, body, nil
}

func (f *DeviceFlow) postForm(ctx context.Context, endpoint string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return f.do(req)
}

// providerFailure turns a non-success answer into a typed error: 5xx is the
// provider's trouble, anything else is a rejection with its stated reason.
func providerFailure(status int, errCode, description string) error {
	if status >= 500 {
		return fmt.Errorf("%w: HTTP %d", ErrProviderUnreachable, status)
	}
	return fmt.Errorf("%w: %s", ErrProviderRejected, firstNonEmpty(description, errCode, http.StatusText(status)))
}

func (f *DeviceFlow) Start(ctx context.Context, provider, clientID, tenant string) (DeviceCode, error) {
	ep, err := f.endpoints(provider, tenant)
	if err != nil {
		return DeviceCode{}, err
	}
	status, body, err := f.postForm(ctx, ep.DeviceURL, url.Values{"client_id": {clientID}, "scope": {ep.Scope}})
	if err != nil {
		return DeviceCode{}, err
	}
	var r struct {
		DeviceCode       string `json:"device_code"`
		UserCode         string `json:"user_code"`
		VerificationURI  string `json:"verification_uri"`
		VerificationURL  string `json:"verification_url"`
		ExpiresIn        int    `json:"expires_in"`
		Interval         int    `json:"interval"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &r)
	if status/100 != 2 || r.DeviceCode == "" || r.UserCode == "" {
		return DeviceCode{}, providerFailure(status, r.Error, r.ErrorDescription)
	}
	interval, expires := r.Interval, r.ExpiresIn
	if interval <= 0 {
		interval = 5
	}
	if expires <= 0 {
		expires = 900
	}
	return DeviceCode{
		DeviceCode: r.DeviceCode, UserCode: r.UserCode,
		VerificationURI: firstNonEmpty(r.VerificationURI, r.VerificationURL),
		ExpiresIn:       time.Duration(expires) * time.Second,
		Interval:        time.Duration(interval) * time.Second,
	}, nil
}

// Exchange asks once whether the person has finished signing in. Until they
// have, it returns ErrAuthorizationPending (or ErrSlowDown); callers wait the
// interval between calls.
func (f *DeviceFlow) Exchange(ctx context.Context, provider, clientID, secret, tenant, deviceCode string) (Token, error) {
	ep, err := f.endpoints(provider, tenant)
	if err != nil {
		return Token{}, err
	}
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	status, body, err := f.postForm(ctx, ep.TokenURL, form)
	if err != nil {
		return Token{}, err
	}
	var r struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &r)
	if status/100 == 2 && r.AccessToken != "" {
		return Token{
			AccessToken: r.AccessToken, RefreshToken: r.RefreshToken, TokenType: r.TokenType,
			Expiry: f.now().Add(time.Duration(r.ExpiresIn) * time.Second),
		}, nil
	}
	switch r.Error {
	case "authorization_pending":
		return Token{}, ErrAuthorizationPending
	case "slow_down":
		return Token{}, ErrSlowDown
	case "expired_token":
		return Token{}, ErrCodeExpired
	case "access_denied", "authorization_declined":
		return Token{}, ErrAccessDenied
	}
	return Token{}, providerFailure(status, r.Error, r.ErrorDescription)
}

func (f *DeviceFlow) getJSON(ctx context.Context, endpoint, accessToken string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	status, body, err := f.do(req)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return providerFailure(status, "", "")
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%w: unreadable answer", ErrProviderRejected)
	}
	return nil
}

// Profile reads who signed in. For Google that is only the email, which is
// cosmetic; for OneDrive the drive ID and type are required by rclone.
func (f *DeviceFlow) Profile(ctx context.Context, provider, tenant, accessToken string) (Profile, error) {
	ep, err := f.endpoints(provider, tenant)
	if err != nil {
		return Profile{}, err
	}
	if provider == ProviderGoogleDrive {
		var r struct {
			User struct {
				EmailAddress string `json:"emailAddress"`
			} `json:"user"`
		}
		if err := f.getJSON(ctx, ep.ProfileURL, accessToken, &r); err != nil {
			return Profile{}, err
		}
		return Profile{Email: r.User.EmailAddress}, nil
	}
	var r struct {
		ID        string `json:"id"`
		DriveType string `json:"driveType"`
		Owner     struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"owner"`
	}
	if err := f.getJSON(ctx, ep.DriveURL, accessToken, &r); err != nil {
		return Profile{}, err
	}
	if r.ID == "" || r.DriveType == "" {
		return Profile{}, fmt.Errorf("%w: OneDrive returned no drive", ErrProviderRejected)
	}
	return Profile{Email: r.Owner.User.Email, DriveID: r.ID, DriveType: r.DriveType}, nil
}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'DeviceFlow|Exchange|StartRejected|StartAccepts|DefaultEndpoints' -v`
Expected: PASS (5 tests).

- [ ] **Step 6: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./internal/platform/backup/... ./test/cloudfake/...`
Expected: no output.

```bash
git add hdms-backend/internal/platform/backup/deviceflow.go hdms-backend/internal/platform/backup/deviceflow_test.go hdms-backend/test/cloudfake/cloudfake.go
git commit -m "feat(backup): OAuth device-flow client for Google Drive and OneDrive"
```

---

### Task 2: Cloud accounts — migration, destination model, account service

**Files:**
- Create: `hdms-backend/migrations/0029_cloud_accounts.sql`
- Modify: `hdms-backend/queries/backup/backup.sql` (every destination query), then regenerate `hdms-backend/internal/platform/backup/store/`
- Modify: `hdms-backend/internal/platform/backup/store.go`, `destinations.go`
- Create: `hdms-backend/internal/platform/backup/cloud_accounts.go`
- Test: `hdms-backend/internal/platform/backup/cloud_folder_test.go`, `hdms-backend/test/integration/backup_cloud_accounts_test.go`

**Interfaces:**
- Consumes: Task 1 (`DeviceFlow`, `Token`, `Profile`, sentinels, provider constants).
- Produces: `CloudService{Q db.DBTX; Key []byte; Flow *DeviceFlow; Now func() time.Time}`; `CloudAccountInput{Provider, Name, ClientID, ClientSecret, Tenant string}`; `CloudAccount{ID uuid.UUID; Provider, Name, ClientID, Tenant, AccountEmail, Status, LastError string; ConnectedAt *time.Time; UpdatedAt time.Time}`; `SignIn{AccountID uuid.UUID; UserCode, VerificationURI string; ExpiresAt time.Time}`; methods `Start(ctx, in, actor) (SignIn, error)`, `Reconnect(ctx, id, actor) (SignIn, error)`, `Poll(ctx, id) (CloudAccount, error)`, `Get(ctx, id)`, `List(ctx)`, `Delete(ctx, id) error`, `StoreToken(ctx, id, tokenJSON string) error`, `SetStatus(ctx, id, status, reason string) error`; status constants `AccountPending|AccountConnected|AccountExpired|AccountRevoked`; errors `ErrCloudAccountNotFound`, `ErrCloudAccountInUse`, `ErrCloudAccountNotConnected`; `CleanCloudFolder(string) (string, error)`; `CloudDestinationInput{Name string; Folder string; Enabled bool; RetentionVersions int}`; `CreateCloudDestination(ctx, pool *db.Pool, acct CloudAccount, in CloudDestinationInput, actor string) (Destination, error)`; `Destination` gains `CloudAccountID *uuid.UUID` and `Folder string`; unexported `(*CloudService).credentials(ctx, id) (CloudCreds, error)` with `CloudCreds{ID uuid.UUID; Provider, ClientID, ClientSecret, Tenant, Token, DriveID, DriveType, Status string}`.

- [ ] **Step 1: Write the migration**

Create `hdms-backend/migrations/0029_cloud_accounts.sql`:

```sql
-- +goose Up
-- +goose StatementBegin

-- A Google Drive or OneDrive sign-in HDMS uses to copy backups to the cloud.
-- Client secret, device code and token are sealed with HDMS_CREDENTIAL_ENC_KEY
-- by the application; this table never holds one in the clear.
CREATE TABLE backup_cloud_accounts (
    id                  uuid PRIMARY KEY,
    provider            text NOT NULL CHECK (provider IN ('google_drive', 'onedrive')),
    name                text NOT NULL,
    client_id           text NOT NULL,
    client_secret_enc   bytea,
    tenant              text NOT NULL DEFAULT 'common',
    token_enc           bytea,
    account_email       text,
    drive_id            text,
    drive_type          text,
    -- In-flight sign-in only. next_poll is the earliest moment the provider
    -- may be asked again; claiming it atomically keeps two browsers from
    -- both exchanging one device code.
    device_code_enc     bytea,
    device_interval_s   integer,
    device_next_poll_at timestamptz,
    device_expires_at   timestamptz,
    status              text NOT NULL CHECK (status IN ('pending', 'connected', 'expired', 'revoked')),
    last_error          text,
    connected_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    updated_by          text NOT NULL
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE backup_cloud_accounts TO hdms_app;

-- A cloud destination names its account and a folder inside the drive. Legacy
-- hand-typed rclone destinations have neither and stay valid.
ALTER TABLE backup_destinations
    ADD COLUMN cloud_account_id uuid REFERENCES backup_cloud_accounts (id),
    ADD COLUMN folder text,
    ADD CONSTRAINT backup_destinations_cloud_shape CHECK (
        (cloud_account_id IS NULL) = (folder IS NULL)
        AND (kind <> 'path' OR cloud_account_id IS NULL)
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM backup_destinations WHERE cloud_account_id IS NOT NULL;
ALTER TABLE backup_destinations
    DROP CONSTRAINT backup_destinations_cloud_shape,
    DROP COLUMN folder,
    DROP COLUMN cloud_account_id;
DROP TABLE IF EXISTS backup_cloud_accounts;

-- +goose StatementEnd
```

- [ ] **Step 2: Update the sqlc queries and regenerate**

In `hdms-backend/queries/backup/backup.sql`, the model `BackupDestination` now has two more columns, so every query that returns a destination must list them (otherwise sqlc stops returning the model type). Replace the five queries `ListDestinations`, `ListEnabledDestinations`, `GetDestination`, `CreateDestination`, `UpdateDestination` with:

```sql
-- name: ListDestinations :many
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by,
       cloud_account_id, folder
FROM backup_destinations
ORDER BY name;

-- name: ListEnabledDestinations :many
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by,
       cloud_account_id, folder
FROM backup_destinations
WHERE enabled
ORDER BY name;

-- name: GetDestination :one
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by,
       cloud_account_id, folder
FROM backup_destinations
WHERE id = $1;

-- name: CreateDestination :one
INSERT INTO backup_destinations (
    id, name, kind, target, provider, enabled, retention_versions, updated_by,
    cloud_account_id, folder
) VALUES (
    @id, @name, @kind, @target, @provider, @enabled, @retention_versions, @updated_by,
    @cloud_account_id, @folder
)
RETURNING id, name, kind, target, provider, enabled, retention_versions,
          initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by,
          cloud_account_id, folder;

-- name: UpdateDestination :one
UPDATE backup_destinations
SET name               = @name,
    enabled            = @enabled,
    retention_versions = @retention_versions,
    updated_at         = now(),
    updated_by         = @updated_by
WHERE id = @id
RETURNING id, name, kind, target, provider, enabled, retention_versions,
          initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by,
          cloud_account_id, folder;
```

Run: `cd hdms-backend && sqlc generate`
Expected: no output; `internal/platform/backup/store/models.go` now has `BackupCloudAccount` and `BackupDestination.CloudAccountID pgtype.UUID`, `Folder pgtype.Text`.

- [ ] **Step 3: Teach `Destination` about the account and folder**

In `hdms-backend/internal/platform/backup/store.go`, add two fields to `Destination` (after `RetentionVersions`):

```go
	// CloudAccountID and Folder are set for Google Drive and OneDrive
	// destinations; the repository then lives at <Folder>/repo in that drive.
	CloudAccountID *uuid.UUID
	Folder         string
```

In `hdms-backend/internal/platform/backup/destinations.go`, replace `mapDestination`:

```go
func mapDestination(r backupstore.BackupDestination) Destination {
	var account *uuid.UUID
	if r.CloudAccountID.Valid {
		id := uuid.UUID(r.CloudAccountID.Bytes)
		account = &id
	}
	return Destination{
		ID:                r.ID,
		Name:              r.Name,
		Kind:              r.Kind,
		Target:            r.Target,
		Provider:          r.Provider,
		Enabled:           r.Enabled,
		RetentionVersions: int(r.RetentionVersions),
		CloudAccountID:    account,
		Folder:            pgtypeconv.TextString(r.Folder),
		InitializedAt:     pgtypeconv.TimePtr(r.InitializedAt),
		LastOkAt:          pgtypeconv.TimePtr(r.LastOkAt),
		LastError:         pgtypeconv.TextString(r.LastError),
		UpdatedAt:         pgtypeconv.Time(r.UpdatedAt),
	}
}
```

- [ ] **Step 4: Write the failing folder-validation test**

Create `hdms-backend/internal/platform/backup/cloud_folder_test.go`:

```go
package backup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestCleanCloudFolder(t *testing.T) {
	for in, want := range map[string]string{
		"hdms-backups":       "hdms-backups",
		"  /hdms-backups/ ":  "hdms-backups",
		"Hospital/IT/HDMS":   "Hospital/IT/HDMS",
		"病院 バックアップ":          "病院 バックアップ",
		"hdms_backups.v2":    "hdms_backups.v2",
	} {
		got, err := backup.CleanCloudFolder(in)
		if err != nil || got != want {
			t.Fatalf("CleanCloudFolder(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "   ", "/", "a:b", "remote:path", "../x", "a/../b", ".hidden", "a/.b", "a//b",
		"a/b/c/d", "a\\b", "a\nb", "a/ b", "x" + string(make([]byte, 201)),
	} {
		if _, err := backup.CleanCloudFolder(in); !errors.Is(err, backup.ErrInvalidDestination) {
			t.Fatalf("CleanCloudFolder(%q) err = %v, want ErrInvalidDestination", in, err)
		}
	}
}

func TestStartValidatesBeforeTouchingAnything(t *testing.T) {
	svc := &backup.CloudService{} // no database, no key: validation must come first
	for name, in := range map[string]backup.CloudAccountInput{
		"google needs a secret": {Provider: backup.ProviderGoogleDrive, Name: "n", ClientID: "c"},
		"unknown provider":      {Provider: "dropbox", Name: "n", ClientID: "c", ClientSecret: "s"},
		"empty name":            {Provider: backup.ProviderOneDrive, Name: " ", ClientID: "c"},
		"empty client id":       {Provider: backup.ProviderOneDrive, Name: "n", ClientID: " "},
		"newline in client id":  {Provider: backup.ProviderOneDrive, Name: "n", ClientID: "c\nd"},
		"bad tenant":            {Provider: backup.ProviderOneDrive, Name: "n", ClientID: "c", Tenant: "a b/c"},
	} {
		if _, err := svc.Start(context.Background(), in, "test"); !errors.Is(err, backup.ErrInvalidCloudAccount) {
			t.Fatalf("%s: err = %v, want ErrInvalidCloudAccount", name, err)
		}
	}
}
```

- [ ] **Step 5: Write the failing integration test for the service**

Create `hdms-backend/test/integration/backup_cloud_accounts_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/cloudfake"
	"github.com/hito-hospital/hdms/test/testdb"
)

var cloudKey = []byte("0123456789abcdef0123456789abcdef")

func newCloudService(t *testing.T) (*backup.CloudService, *db.Pool, *cloudfake.Provider) {
	t.Helper()
	pool := testdb.New(t)
	p := cloudfake.New(t)
	return &backup.CloudService{Q: pool.Pool, Key: cloudKey, Flow: p.Flow(), Now: p.Now}, pool, p
}

var googleInput = backup.CloudAccountInput{
	Provider: backup.ProviderGoogleDrive, Name: "Hospital Drive", ClientID: "cid-1", ClientSecret: "s3cret-value",
}

// connectCloud runs a whole sign-in and returns the connected account.
func connectCloud(t *testing.T, svc *backup.CloudService, p *cloudfake.Provider, in backup.CloudAccountInput) backup.CloudAccount {
	t.Helper()
	ctx := context.Background()
	si, err := svc.Start(ctx, in, "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.Answer("approve")
	p.Advance(6 * time.Second)
	acct, err := svc.Poll(ctx, si.AccountID)
	if err != nil || acct.Status != backup.AccountConnected {
		t.Fatalf("Poll = %+v, %v; want connected", acct, err)
	}
	return acct
}

func TestCloudStartSealsSecretsAndLeavesAPendingSignIn(t *testing.T) {
	svc, pool, _ := newCloudService(t)
	ctx := context.Background()

	si, err := svc.Start(ctx, googleInput, "admin@hospital.test")
	if err != nil {
		t.Fatal(err)
	}
	if si.UserCode != "ABCD-EFGH" || si.VerificationURI == "" || !si.ExpiresAt.After(time.Now().Add(-time.Hour)) {
		t.Fatalf("sign-in = %+v", si)
	}
	var status string
	var secretEnc, deviceEnc []byte
	if err := pool.QueryRow(ctx, `SELECT status, client_secret_enc, device_code_enc FROM backup_cloud_accounts WHERE id = $1`, si.AccountID).
		Scan(&status, &secretEnc, &deviceEnc); err != nil {
		t.Fatal(err)
	}
	if status != backup.AccountPending {
		t.Fatalf("status = %q", status)
	}
	for _, sealed := range [][]byte{secretEnc, deviceEnc} {
		if len(sealed) == 0 || strings.Contains(string(sealed), "s3cret-value") || strings.Contains(string(sealed), cloudfake.DeviceCode) {
			t.Fatalf("a secret is stored in the clear: %q", sealed)
		}
	}
	plain, err := backup.Decrypt(cloudKey, secretEnc)
	if err != nil || string(plain) != "s3cret-value" {
		t.Fatalf("client secret does not round-trip: %q, %v", plain, err)
	}
}

func TestCloudPollHonoursTheProvidersInterval(t *testing.T) {
	svc, _, p := newCloudService(t)
	ctx := context.Background()
	si, err := svc.Start(ctx, googleInput, "test")
	if err != nil {
		t.Fatal(err)
	}
	poll := func() backup.CloudAccount {
		a, err := svc.Poll(ctx, si.AccountID)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	poll() // inside the first 5 s: no outbound call
	if p.TokenCalls() != 0 {
		t.Fatalf("polled the provider %d times before the interval elapsed", p.TokenCalls())
	}
	p.Advance(5 * time.Second)
	poll()
	poll() // same instant: still only one call
	if p.TokenCalls() != 1 {
		t.Fatalf("token calls = %d, want 1", p.TokenCalls())
	}

	p.Answer("slow_down")
	p.Advance(5 * time.Second)
	poll() // call 2: the provider says slow down, interval becomes 10 s
	p.Advance(5 * time.Second)
	poll() // inside the new interval: no call
	if p.TokenCalls() != 2 {
		t.Fatalf("token calls after slow_down = %d, want 2 (the interval must have grown)", p.TokenCalls())
	}
	p.Answer("approve")
	p.Advance(5 * time.Second)
	if a := poll(); a.Status != backup.AccountConnected || p.TokenCalls() != 3 {
		t.Fatalf("account = %+v, calls = %d", a, p.TokenCalls())
	}
}

func TestCloudSignInStoresAnEncryptedTokenAndTheEmail(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	if acct.AccountEmail != "drive-owner@example.test" || acct.ConnectedAt == nil || acct.LastError != "" {
		t.Fatalf("account = %+v", acct)
	}
	var tokenEnc, deviceEnc []byte
	var nextPoll *time.Time
	if err := pool.QueryRow(ctx, `SELECT token_enc, device_code_enc, device_next_poll_at FROM backup_cloud_accounts WHERE id = $1`, acct.ID).
		Scan(&tokenEnc, &deviceEnc, &nextPoll); err != nil {
		t.Fatal(err)
	}
	plain, err := backup.Decrypt(cloudKey, tokenEnc)
	if err != nil || !strings.Contains(string(plain), `"access_token":"at-1"`) || !strings.Contains(string(plain), `"refresh_token":"rt-1"`) {
		t.Fatalf("stored token = %q, %v", plain, err)
	}
	if deviceEnc != nil || nextPoll != nil {
		t.Fatal("device-flow state survives a finished sign-in")
	}
}

func TestCloudOneDriveKeepsTheDriveItWillNeed(t *testing.T) {
	svc, pool, p := newCloudService(t)
	acct := connectCloud(t, svc, p, backup.CloudAccountInput{Provider: backup.ProviderOneDrive, Name: "OD", ClientID: "cid", Tenant: "contoso"})
	var driveID, driveType string
	_ = pool.QueryRow(context.Background(), `SELECT drive_id, drive_type FROM backup_cloud_accounts WHERE id = $1`, acct.ID).Scan(&driveID, &driveType)
	if driveID != "b!abc" || driveType != "business" || acct.Tenant != "contoso" {
		t.Fatalf("drive = %q/%q tenant = %q", driveID, driveType, acct.Tenant)
	}
}

func TestCloudPollKeepsASignInPendingOnANetworkError(t *testing.T) {
	svc, _, p := newCloudService(t)
	ctx := context.Background()
	si, _ := svc.Start(ctx, googleInput, "test")
	p.Answer("down")
	p.Advance(6 * time.Second)
	if _, err := svc.Poll(ctx, si.AccountID); !errors.Is(err, backup.ErrProviderUnreachable) {
		t.Fatalf("err = %v, want ErrProviderUnreachable", err)
	}
	a, _ := svc.Get(ctx, si.AccountID)
	if a.Status != backup.AccountPending {
		t.Fatalf("status = %q, want pending (a flaky network must not end a sign-in)", a.Status)
	}
	p.Answer("approve")
	p.Advance(6 * time.Second)
	if a, _ := svc.Poll(ctx, si.AccountID); a.Status != backup.AccountConnected {
		t.Fatalf("status after recovery = %q", a.Status)
	}
}

func TestCloudSignInEndsWhenTheCodeExpiresOrIsDeclined(t *testing.T) {
	svc, _, p := newCloudService(t)
	ctx := context.Background()

	si, _ := svc.Start(ctx, googleInput, "test")
	p.Advance(11 * time.Minute) // the code lasts 10
	a, err := svc.Poll(ctx, si.AccountID)
	if err != nil || a.Status != backup.AccountExpired || a.LastError == "" {
		t.Fatalf("expired: %+v, %v", a, err)
	}
	if p.TokenCalls() != 0 {
		t.Fatal("asked the provider about a code already known to be expired")
	}

	si2, err := svc.Reconnect(ctx, si.AccountID, "test")
	if err != nil {
		t.Fatal(err)
	}
	p.Answer("denied")
	p.Advance(6 * time.Second)
	a, _ = svc.Poll(ctx, si2.AccountID)
	if a.Status != backup.AccountRevoked || a.LastError == "" {
		t.Fatalf("declined: %+v", a)
	}
}

func TestCloudStartRollsBackWhenTheProviderRejectsTheClient(t *testing.T) {
	svc, pool, p := newCloudService(t)
	p.RejectStart("invalid_client", "The OAuth client was not found.")
	if _, err := svc.Start(context.Background(), googleInput, "test"); !errors.Is(err, backup.ErrProviderRejected) {
		t.Fatalf("err = %v", err)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM backup_cloud_accounts`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d account row(s) left behind by a rejected client", n)
	}
}

func TestCloudAccountCannotBeDeletedWhileADestinationUsesIt(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)

	d, err := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{
		Name: "Drive", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2,
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "rclone" || d.Provider != backup.ProviderGoogleDrive || d.Folder != "hdms-backups" ||
		d.CloudAccountID == nil || *d.CloudAccountID != acct.ID {
		t.Fatalf("destination = %+v", d)
	}
	if _, err := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{
		Name: "Again", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2,
	}, "test"); !errors.Is(err, backup.ErrDestinationExists) {
		t.Fatalf("second destination on the same folder: err = %v, want ErrDestinationExists", err)
	}

	if err := svc.Delete(ctx, acct.ID); !errors.Is(err, backup.ErrCloudAccountInUse) {
		t.Fatalf("Delete in use: err = %v", err)
	}
	if err := backup.DeleteDestination(ctx, pool, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, acct.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, acct.ID); !errors.Is(err, backup.ErrCloudAccountNotFound) {
		t.Fatalf("Get after delete: err = %v", err)
	}
}
```

- [ ] **Step 6: Run both to see them fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'CleanCloudFolder|StartValidates'`
Expected: FAIL — `backup.CleanCloudFolder` / `backup.CloudService` undefined.

- [ ] **Step 7: Write the account service**

Create `hdms-backend/internal/platform/backup/cloud_accounts.go`:

```go
package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

const (
	AccountPending   = "pending"
	AccountConnected = "connected"
	AccountExpired   = "expired"
	AccountRevoked   = "revoked"
)

var (
	ErrCloudAccountNotFound     = errors.New("backup: cloud account not found")
	ErrCloudAccountInUse        = errors.New("backup: cloud account is used by a destination")
	ErrCloudAccountNotConnected = errors.New("backup: cloud account is not connected")
)

var tenantPattern = regexp.MustCompile(`^[A-Za-z0-9.-]{1,100}$`)

// CloudAccount is what the console may see. It carries no secret, not even
// whether one is set.
type CloudAccount struct {
	ID           uuid.UUID
	Provider     string
	Name         string
	ClientID     string
	Tenant       string
	AccountEmail string
	Status       string
	LastError    string
	ConnectedAt  *time.Time
	UpdatedAt    time.Time
}

type CloudAccountInput struct {
	Provider     string
	Name         string
	ClientID     string
	ClientSecret string // Google only; OneDrive is a public client
	Tenant       string // OneDrive only; "" means common
}

// SignIn is what the administrator needs to finish signing in elsewhere.
type SignIn struct {
	AccountID       uuid.UUID
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
}

// CloudCreds is everything the worker needs to render one rclone remote.
type CloudCreds struct {
	ID           uuid.UUID
	Provider     string
	ClientID     string
	ClientSecret string
	Tenant       string
	Token        string // rclone token JSON; empty before sign-in
	DriveID      string
	DriveType    string
	Status       string
}

// CloudService keeps cloud accounts in Postgres. The API uses Start,
// Reconnect, Poll, List, Get and Delete (Flow set); the worker uses
// credentials, StoreToken and SetStatus (Flow nil).
type CloudService struct {
	Q    db.DBTX
	Key  []byte // HDMS_CREDENTIAL_ENC_KEY, 32 bytes
	Flow *DeviceFlow
	Now  func() time.Time
}

func (s *CloudService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (in *CloudAccountInput) normalise() error {
	in.Name = strings.TrimSpace(in.Name)
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.ClientSecret = strings.TrimSpace(in.ClientSecret)
	in.Tenant = strings.TrimSpace(in.Tenant)
	invalid := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidCloudAccount, fmt.Sprintf(format, a...))
	}
	switch in.Provider {
	case ProviderGoogleDrive:
		if in.ClientSecret == "" {
			return invalid("Google Drive needs the OAuth client secret")
		}
		in.Tenant = "common"
	case ProviderOneDrive:
		if in.Tenant == "" {
			in.Tenant = "common"
		}
		if !tenantPattern.MatchString(in.Tenant) {
			return invalid("the directory (tenant) ID may use letters, digits, dots and dashes")
		}
	default:
		return invalid("unknown provider %q", in.Provider)
	}
	if in.Name == "" || len(in.Name) > 100 {
		return invalid("name must be 1 to 100 characters")
	}
	if in.ClientID == "" || len(in.ClientID) > 300 || len(in.ClientSecret) > 300 {
		return invalid("client ID and secret must be 1 to 300 characters")
	}
	// They are written into an rclone config file: a newline would add a line.
	for _, v := range []string{in.Name, in.ClientID, in.ClientSecret} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return invalid("values may not contain line breaks")
		}
	}
	return nil
}

const cloudColumns = `id, provider, name, client_id, tenant, coalesce(account_email, ''), status, coalesce(last_error, ''), connected_at, updated_at`

func scanCloudAccount(row pgx.Row) (CloudAccount, error) {
	var a CloudAccount
	err := row.Scan(&a.ID, &a.Provider, &a.Name, &a.ClientID, &a.Tenant, &a.AccountEmail, &a.Status, &a.LastError, &a.ConnectedAt, &a.UpdatedAt)
	return a, err
}

func (s *CloudService) Get(ctx context.Context, id uuid.UUID) (CloudAccount, error) {
	a, err := scanCloudAccount(s.Q.QueryRow(ctx, `SELECT `+cloudColumns+` FROM backup_cloud_accounts WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudAccount{}, ErrCloudAccountNotFound
	}
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: get cloud account: %w", err)
	}
	return a, nil
}

func (s *CloudService) List(ctx context.Context) ([]CloudAccount, error) {
	rows, err := s.Q.Query(ctx, `SELECT `+cloudColumns+` FROM backup_cloud_accounts ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("backup: list cloud accounts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CloudAccount, error) { return scanCloudAccount(row) })
	if err != nil {
		return nil, fmt.Errorf("backup: list cloud accounts: %w", err)
	}
	return out, nil
}

// Start registers the OAuth client and begins the device sign-in. A provider
// that rejects the client leaves nothing behind.
func (s *CloudService) Start(ctx context.Context, in CloudAccountInput, actor string) (SignIn, error) {
	if err := in.normalise(); err != nil {
		return SignIn{}, err
	}
	var sealed []byte
	if in.ClientSecret != "" {
		b, err := Encrypt(s.Key, []byte(in.ClientSecret))
		if err != nil {
			return SignIn{}, fmt.Errorf("backup: seal client secret: %w", err)
		}
		sealed = b
	}
	id := ids.NewUUID()
	if _, err := s.Q.Exec(ctx,
		`INSERT INTO backup_cloud_accounts (id, provider, name, client_id, client_secret_enc, tenant, status, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)`,
		id, in.Provider, in.Name, in.ClientID, sealed, in.Tenant, actor); err != nil {
		return SignIn{}, fmt.Errorf("backup: create cloud account: %w", err)
	}
	si, err := s.begin(ctx, id, actor)
	if err != nil {
		_, _ = s.Q.Exec(ctx, `DELETE FROM backup_cloud_accounts WHERE id = $1`, id)
		return SignIn{}, err
	}
	return si, nil
}

// Reconnect restarts the sign-in for an account whose token expired or was
// revoked, keeping its destinations.
func (s *CloudService) Reconnect(ctx context.Context, id uuid.UUID, actor string) (SignIn, error) {
	return s.begin(ctx, id, actor)
}

func (s *CloudService) begin(ctx context.Context, id uuid.UUID, actor string) (SignIn, error) {
	c, err := s.credentials(ctx, id)
	if err != nil {
		return SignIn{}, err
	}
	dc, err := s.Flow.Start(ctx, c.Provider, c.ClientID, c.Tenant)
	if err != nil {
		return SignIn{}, err
	}
	sealed, err := Encrypt(s.Key, []byte(dc.DeviceCode))
	if err != nil {
		return SignIn{}, fmt.Errorf("backup: seal device code: %w", err)
	}
	now := s.now()
	expires := now.Add(dc.ExpiresIn)
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts
		 SET status = 'pending', device_code_enc = $2, device_interval_s = $3, device_next_poll_at = $4,
		     device_expires_at = $5, last_error = NULL, updated_at = now(), updated_by = $6
		 WHERE id = $1`,
		id, sealed, int32(dc.Interval/time.Second), now.Add(dc.Interval), expires, actor); err != nil {
		return SignIn{}, fmt.Errorf("backup: save sign-in: %w", err)
	}
	return SignIn{AccountID: id, UserCode: dc.UserCode, VerificationURI: dc.VerificationURI, ExpiresAt: expires}, nil
}

// Poll asks the provider at most once per interval whether the sign-in is
// done, and returns the account as it stands. Calls inside the interval cost
// nothing: the next-poll time is claimed atomically, so two browsers cannot
// exchange one device code twice.
func (s *CloudService) Poll(ctx context.Context, id uuid.UUID) (CloudAccount, error) {
	var status string
	var expires *time.Time
	var interval *int32
	err := s.Q.QueryRow(ctx, `SELECT status, device_expires_at, device_interval_s FROM backup_cloud_accounts WHERE id = $1`, id).
		Scan(&status, &expires, &interval)
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudAccount{}, ErrCloudAccountNotFound
	}
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: read sign-in: %w", err)
	}
	if status != AccountPending {
		return s.Get(ctx, id)
	}
	now := s.now()
	if expires != nil && now.After(*expires) {
		if err := s.endFlow(ctx, id, AccountExpired, "The sign-in code expired. Start again."); err != nil {
			return CloudAccount{}, err
		}
		return s.Get(ctx, id)
	}
	wait := 5 * time.Second
	if interval != nil && *interval > 0 {
		wait = time.Duration(*interval) * time.Second
	}
	var sealed []byte
	err = s.Q.QueryRow(ctx,
		`UPDATE backup_cloud_accounts SET device_next_poll_at = $2
		 WHERE id = $1 AND status = 'pending' AND device_code_enc IS NOT NULL
		   AND (device_next_poll_at IS NULL OR device_next_poll_at <= $3)
		 RETURNING device_code_enc`, id, now.Add(wait), now).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.Get(ctx, id) // not due yet, or someone else is asking
	}
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: claim sign-in poll: %w", err)
	}
	code, err := Decrypt(s.Key, sealed)
	if err != nil {
		return CloudAccount{}, fmt.Errorf("backup: open device code: %w", err)
	}
	c, err := s.credentials(ctx, id)
	if err != nil {
		return CloudAccount{}, err
	}
	tok, err := s.Flow.Exchange(ctx, c.Provider, c.ClientID, c.ClientSecret, c.Tenant, string(code))
	switch {
	case err == nil:
		if err := s.complete(ctx, id, c, tok); err != nil {
			return CloudAccount{}, err
		}
	case errors.Is(err, ErrAuthorizationPending):
	case errors.Is(err, ErrSlowDown):
		if _, err := s.Q.Exec(ctx,
			`UPDATE backup_cloud_accounts SET device_interval_s = coalesce(device_interval_s, 5) + 5, device_next_poll_at = $2 WHERE id = $1`,
			id, now.Add(wait+5*time.Second)); err != nil {
			return CloudAccount{}, fmt.Errorf("backup: slow down sign-in poll: %w", err)
		}
	case errors.Is(err, ErrCodeExpired):
		if err := s.endFlow(ctx, id, AccountExpired, "The sign-in code expired. Start again."); err != nil {
			return CloudAccount{}, err
		}
	case errors.Is(err, ErrAccessDenied):
		if err := s.endFlow(ctx, id, AccountRevoked, "Sign-in was declined."); err != nil {
			return CloudAccount{}, err
		}
	default:
		return CloudAccount{}, err // the sign-in stays pending and can be asked again
	}
	return s.Get(ctx, id)
}

func (s *CloudService) complete(ctx context.Context, id uuid.UUID, c CloudCreds, tok Token) error {
	prof, perr := s.Flow.Profile(ctx, c.Provider, c.Tenant, tok.AccessToken)
	if perr != nil && c.Provider == ProviderOneDrive {
		// The code is spent and rclone cannot work without the drive: start over.
		return s.endFlow(ctx, id, AccountExpired, "Signed in, but OneDrive's details could not be read. Reconnect to try again.")
	}
	js, err := tok.RcloneJSON()
	if err != nil {
		return fmt.Errorf("backup: encode token: %w", err)
	}
	sealed, err := Encrypt(s.Key, []byte(js))
	if err != nil {
		return fmt.Errorf("backup: seal token: %w", err)
	}
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts
		 SET status = 'connected', token_enc = $2, account_email = $3, drive_id = $4, drive_type = $5,
		     device_code_enc = NULL, device_interval_s = NULL, device_next_poll_at = NULL, device_expires_at = NULL,
		     last_error = NULL, connected_at = $6, updated_at = now()
		 WHERE id = $1`,
		id, sealed, nilIfEmpty(prof.Email), nilIfEmpty(prof.DriveID), nilIfEmpty(prof.DriveType), s.now()); err != nil {
		return fmt.Errorf("backup: save token: %w", err)
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// endFlow finishes a pending sign-in without a token.
func (s *CloudService) endFlow(ctx context.Context, id uuid.UUID, status, reason string) error {
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts
		 SET status = $2, last_error = $3, device_code_enc = NULL, device_interval_s = NULL,
		     device_next_poll_at = NULL, device_expires_at = NULL, updated_at = now()
		 WHERE id = $1 AND status = 'pending'`, id, status, reason); err != nil {
		return fmt.Errorf("backup: end sign-in: %w", err)
	}
	return nil
}

// Delete forgets an account. Destinations that use it must go first, so a
// scheduled backup never meets a destination without a sign-in.
func (s *CloudService) Delete(ctx context.Context, id uuid.UUID) error {
	var inUse bool
	if err := s.Q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM backup_destinations WHERE cloud_account_id = $1)`, id).Scan(&inUse); err != nil {
		return fmt.Errorf("backup: check cloud account use: %w", err)
	}
	if inUse {
		return ErrCloudAccountInUse
	}
	tag, err := s.Q.Exec(ctx, `DELETE FROM backup_cloud_accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("backup: delete cloud account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCloudAccountNotFound
	}
	return nil
}

func (s *CloudService) credentials(ctx context.Context, id uuid.UUID) (CloudCreds, error) {
	c := CloudCreds{ID: id}
	var secret, token []byte
	err := s.Q.QueryRow(ctx,
		`SELECT provider, client_id, client_secret_enc, tenant, token_enc, coalesce(drive_id, ''), coalesce(drive_type, ''), status
		 FROM backup_cloud_accounts WHERE id = $1`, id).
		Scan(&c.Provider, &c.ClientID, &secret, &c.Tenant, &token, &c.DriveID, &c.DriveType, &c.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudCreds{}, ErrCloudAccountNotFound
	}
	if err != nil {
		return CloudCreds{}, fmt.Errorf("backup: read cloud account: %w", err)
	}
	if secret != nil {
		plain, err := Decrypt(s.Key, secret)
		if err != nil {
			return CloudCreds{}, fmt.Errorf("backup: open client secret: %w", err)
		}
		c.ClientSecret = string(plain)
	}
	if token != nil {
		plain, err := Decrypt(s.Key, token)
		if err != nil {
			return CloudCreds{}, fmt.Errorf("backup: open token: %w", err)
		}
		c.Token = string(plain)
	}
	return c, nil
}

// StoreToken saves a token rclone refreshed during a run. It only touches a
// connected account: a sign-in restarted meanwhile must not be overwritten.
func (s *CloudService) StoreToken(ctx context.Context, id uuid.UUID, tokenJSON string) error {
	sealed, err := Encrypt(s.Key, []byte(tokenJSON))
	if err != nil {
		return fmt.Errorf("backup: seal token: %w", err)
	}
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts SET token_enc = $2, updated_at = now() WHERE id = $1 AND status = 'connected'`, id, sealed); err != nil {
		return fmt.Errorf("backup: store refreshed token: %w", err)
	}
	return nil
}

// SetStatus moves a connected account out of service, for example when the
// provider no longer accepts its token.
func (s *CloudService) SetStatus(ctx context.Context, id uuid.UUID, status, reason string) error {
	if _, err := s.Q.Exec(ctx,
		`UPDATE backup_cloud_accounts SET status = $2, last_error = $3, updated_at = now() WHERE id = $1 AND status = 'connected'`,
		id, status, reason); err != nil {
		return fmt.Errorf("backup: set cloud account status: %w", err)
	}
	return nil
}
```

- [ ] **Step 8: Cloud folder validation and cloud destinations**

Append to `hdms-backend/internal/platform/backup/destinations.go` (add `"unicode"`, `"github.com/jackc/pgx/v5/pgtype"` to its imports):

```go
var errCloudFolder = fmt.Errorf("%w: folder names use letters, digits, spaces, dots, dashes and underscores, up to three levels deep", ErrInvalidDestination)

// CleanCloudFolder checks a folder name inside a cloud drive. It becomes part
// of an rclone remote path, so a colon, a backslash, a dot-segment or a line
// break would change what the path means.
func CleanCloudFolder(s string) (string, error) {
	s = strings.Trim(strings.TrimSpace(s), "/")
	if s == "" || len(s) > 200 {
		return "", errCloudFolder
	}
	parts := strings.Split(s, "/")
	if len(parts) > 3 {
		return "", errCloudFolder
	}
	for _, p := range parts {
		if p == "" || strings.HasPrefix(p, ".") || strings.TrimSpace(p) != p {
			return "", errCloudFolder
		}
		for _, r := range p {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" ._-", r) {
				return "", errCloudFolder
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

type CloudDestinationInput struct {
	Name              string
	Folder            string
	Enabled           bool
	RetentionVersions int
}

// CreateCloudDestination stores a destination that copies backups into folder
// inside acct's drive. The account must be connected. target is derived, and
// only keeps two destinations from sharing one folder.
func CreateCloudDestination(ctx context.Context, pool *db.Pool, acct CloudAccount, in CloudDestinationInput, actor string) (Destination, error) {
	if err := (DestinationInput{Name: in.Name, Enabled: in.Enabled, RetentionVersions: in.RetentionVersions}).validate(); err != nil {
		return Destination{}, err
	}
	if acct.Status != AccountConnected {
		return Destination{}, ErrCloudAccountNotConnected
	}
	folder, err := CleanCloudFolder(in.Folder)
	if err != nil {
		return Destination{}, err
	}
	row, err := backupstore.New(db.Conn(ctx, pool)).CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID:                ids.NewUUID(),
		Name:              strings.TrimSpace(in.Name),
		Kind:              "rclone",
		Target:            "cloud:" + acct.ID.String() + "/" + folder,
		Provider:          acct.Provider,
		Enabled:           in.Enabled,
		RetentionVersions: int32(in.RetentionVersions),
		UpdatedBy:         actor,
		CloudAccountID:    pgtype.UUID{Bytes: acct.ID, Valid: true},
		Folder:            pgtype.Text{String: folder, Valid: true},
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Destination{}, ErrDestinationExists
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: create cloud destination: %w", err)
	}
	return mapDestination(row), nil
}
```

- [ ] **Step 9: Run unit and integration tests**

Run: `cd hdms-backend && go build ./... && go test ./internal/platform/backup/ && go test -race -tags=integration ./test/integration/ -run 'TestCloud|TestBackupDestinations'`
Expected: PASS. (`TestBackupDestinations…` re-runs the existing sqlc tests against the regenerated model.)

- [ ] **Step 10: Mutation check the interval claim**

Temporarily change the claim query's `device_next_poll_at <= $3` to `true` in `Poll` and run `TestCloudPollHonoursTheProvidersInterval`.
Expected: FAIL (`polled the provider … before the interval elapsed`). Revert the change.

- [ ] **Step 11: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./...`
Expected: no output.

```bash
git add hdms-backend/migrations/0029_cloud_accounts.sql hdms-backend/queries/backup/backup.sql hdms-backend/internal/platform/backup hdms-backend/test/integration/backup_cloud_accounts_test.go
git commit -m "feat(backup): cloud accounts table, sign-in service and cloud destinations"
```

---

### Task 3: rclone config, the worker's cloud session, and backing up to the cloud

**Files:**
- Create: `hdms-backend/internal/platform/backup/rcloneconf.go`
- Modify: `hdms-backend/internal/platform/backup/dest.go` (cloud `Resolve`), `runner.go`, `executor.go`
- Modify: `hdms-backend/cmd/hdms-cli/worker.go`
- Test: `hdms-backend/internal/platform/backup/rcloneconf_test.go`, `hdms-backend/test/integration/backup_cloud_run_test.go`

**Interfaces:**
- Consumes: Task 2 (`CloudService`, `CloudCreds`, `Destination.CloudAccountID/Folder`, `credentials`, `StoreToken`, `SetStatus`).
- Produces: `CloudRemote(id uuid.UUID) string` (`"acct_<uuid>"`); `RenderRcloneConfig([]CloudCreds) string`; `ParseRcloneTokens(string) map[string]string`; `Rclone{Binary string; Exec ExecFunc; Config string}` with `Rcat(ctx, target string, data []byte) error` and `Copy(ctx, src, dst string) error`; `(*CloudService).OpenSession(ctx, dests []Destination, rc Rclone) (*CloudSession, error)` (returns `nil, nil` when no destination uses an account); nil-safe `(*CloudSession).Apply(Restic) Restic`, `.Usable(Destination) error`, `.PutBundle(ctx, Destination, []byte) error`, `.NoteFailure(ctx, Destination, string)`, `.Close(ctx)`; `PutRecoveryBundle(ctx, *CloudSession, Destination, Repo, []byte) error`; `IsTokenRevoked(string) bool`; `Options.Cloud *CloudSession`; `Executor.Cloud *CloudService`, `Executor.Rclone Rclone`. Cloud `Resolve` yields `Repo{Location: "rclone:acct_<id>:<folder>/repo"}`.

- [ ] **Step 1: Write the failing unit tests**

Create `hdms-backend/internal/platform/backup/rcloneconf_test.go`:

```go
package backup_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	googleID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	oneID    = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func TestRenderRcloneConfig(t *testing.T) {
	conf := backup.RenderRcloneConfig([]backup.CloudCreds{
		{ID: googleID, Provider: backup.ProviderGoogleDrive, ClientID: "gid", ClientSecret: "gsec", Token: `{"access_token":"a"}`},
		{ID: oneID, Provider: backup.ProviderOneDrive, ClientID: "oid", Tenant: "contoso", DriveID: "b!abc", DriveType: "business", Token: `{"access_token":"b"}`},
	})
	for _, want := range []string{
		"[acct_11111111-1111-4111-8111-111111111111]\ntype = drive\nclient_id = gid\nclient_secret = gsec\nscope = drive.file\ntoken = {\"access_token\":\"a\"}\n",
		"[acct_22222222-2222-4222-8222-222222222222]\ntype = onedrive\nclient_id = oid\n",
		"token_url = https://login.microsoftonline.com/contoso/oauth2/v2.0/token\n",
		"drive_id = b!abc\ndrive_type = business\ntoken = {\"access_token\":\"b\"}\n",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("config lacks %q:\n%s", want, conf)
		}
	}
	common := backup.RenderRcloneConfig([]backup.CloudCreds{{ID: oneID, Provider: backup.ProviderOneDrive, Tenant: "common", DriveID: "d", DriveType: "personal", Token: "{}"}})
	if strings.Contains(common, "token_url") {
		t.Fatalf("the common tenant is rclone's default; no override expected:\n%s", common)
	}
}

func TestParseRcloneTokensSeesWhatRcloneRewrote(t *testing.T) {
	conf := backup.RenderRcloneConfig([]backup.CloudCreds{
		{ID: googleID, Provider: backup.ProviderGoogleDrive, ClientID: "g", ClientSecret: "s", Token: `{"access_token":"old"}`},
		{ID: oneID, Provider: backup.ProviderOneDrive, ClientID: "o", Tenant: "common", DriveID: "d", DriveType: "business", Token: `{"access_token":"keep"}`},
	})
	refreshed := strings.Replace(conf, `{"access_token":"old"}`, `{"access_token":"new"}`, 1)
	got := backup.ParseRcloneTokens(refreshed)
	if got[backup.CloudRemote(googleID)] != `{"access_token":"new"}` || got[backup.CloudRemote(oneID)] != `{"access_token":"keep"}` || len(got) != 2 {
		t.Fatalf("tokens = %v", got)
	}
}

func TestIsTokenRevoked(t *testing.T) {
	for msg, want := range map[string]bool{
		"couldn't fetch token: invalid_grant: Token has been expired or revoked.": true,
		"rclone: Failed to create file system: invalid_grant":                     true,
		"AADSTS70008: ... invalid_grant":                                          true,
		"googleapi: Error 403: The user's Drive storage quota has been exceeded.": false,
		"dial tcp: lookup oauth2.googleapis.com: no such host":                    false,
	} {
		if backup.IsTokenRevoked(msg) != want {
			t.Fatalf("IsTokenRevoked(%q) = %v, want %v", msg, !want, want)
		}
	}
}

func TestResolveCloudDestinationNamesTheAccountRemoteAndFolder(t *testing.T) {
	d := backup.Destination{Name: "Drive", Kind: "rclone", CloudAccountID: &googleID, Folder: "Hospital/hdms-backups"}
	repo, err := d.Resolve(nil)
	if err != nil || repo.Location != "rclone:acct_11111111-1111-4111-8111-111111111111:Hospital/hdms-backups/repo" {
		t.Fatalf("repo = %+v, %v", repo, err)
	}
}

func TestRcloneRcatSendsTheBundleOnStdinWithTheConfigInTheEnvironment(t *testing.T) {
	var gotArgs, gotEnv []string
	var gotStdin string
	rc := backup.Rclone{Config: "/tmp/x/rclone.conf", Exec: func(_ context.Context, name string, args, env []string, stdin io.Reader, _ io.Writer) error {
		gotArgs, gotEnv = append([]string{name}, args...), env
		b, _ := io.ReadAll(stdin)
		gotStdin = string(b)
		return nil
	}}
	if err := rc.Rcat(context.Background(), "acct_x:hdms-backups/hdms-recovery.bin", []byte("BUNDLE")); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotArgs, " ") != "rclone rcat acct_x:hdms-backups/hdms-recovery.bin" || gotStdin != "BUNDLE" ||
		strings.Join(gotEnv, " ") != "RCLONE_CONFIG=/tmp/x/rclone.conf" {
		t.Fatalf("args=%v env=%v stdin=%q", gotArgs, gotEnv, gotStdin)
	}
}

func TestACloudDestinationWithoutASessionFailsPlainlyAndTheRunIsDegraded(t *testing.T) {
	f := &fakeRestic{}
	opts := baseOptions(t, f)
	opts.Destinations = []backup.Destination{{Name: "Drive", Kind: "rclone", CloudAccountID: &googleID, Folder: "f", Enabled: true, RetentionVersions: 2}}
	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != backup.OutcomeDegraded || len(rep.Destinations) != 1 ||
		!strings.Contains(rep.Destinations[0].Error, "worker") {
		t.Fatalf("report = %+v", rep)
	}
	for _, call := range f.seen {
		if strings.Contains(call, "acct_") {
			t.Fatalf("restic was pointed at a cloud repository without a session: %s", call)
		}
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'RenderRclone|ParseRclone|IsTokenRevoked|ResolveCloud|RcloneRcat|WithoutASession'`
Expected: FAIL — `backup.RenderRcloneConfig` undefined.

- [ ] **Step 3: Write rclone config, runner and session**

Create `hdms-backend/internal/platform/backup/rcloneconf.go`:

```go
package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CloudRemote is the rclone remote name for an account.
func CloudRemote(id uuid.UUID) string { return "acct_" + id.String() }

// RenderRcloneConfig writes one remote per account. Values come from
// validated input (no line breaks) or from the provider.
func RenderRcloneConfig(accts []CloudCreds) string {
	var b strings.Builder
	for _, a := range accts {
		fmt.Fprintf(&b, "[%s]\n", CloudRemote(a.ID))
		switch a.Provider {
		case ProviderGoogleDrive:
			b.WriteString("type = drive\n")
			fmt.Fprintf(&b, "client_id = %s\nclient_secret = %s\n", a.ClientID, a.ClientSecret)
			b.WriteString("scope = drive.file\n")
		case ProviderOneDrive:
			b.WriteString("type = onedrive\n")
			fmt.Fprintf(&b, "client_id = %s\n", a.ClientID)
			if a.Tenant != "" && a.Tenant != "common" {
				base := "https://login.microsoftonline.com/" + a.Tenant + "/oauth2/v2.0/"
				fmt.Fprintf(&b, "auth_url = %sauthorize\ntoken_url = %stoken\n", base, base)
			}
			fmt.Fprintf(&b, "drive_id = %s\ndrive_type = %s\n", a.DriveID, a.DriveType)
		}
		fmt.Fprintf(&b, "token = %s\n\n", a.Token)
	}
	return b.String()
}

// ParseRcloneTokens returns each remote's current token line. rclone rewrites
// the file when it refreshes a token, so comparing before and after tells
// which accounts need their stored token replaced.
func ParseRcloneTokens(conf string) map[string]string {
	out := map[string]string{}
	section := ""
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = line[1 : len(line)-1]
		case section != "" && strings.HasPrefix(line, "token"):
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "token" {
				out[section] = strings.TrimSpace(v)
			}
		}
	}
	return out
}

// IsTokenRevoked reports whether an rclone/restic failure means the provider
// no longer accepts the saved sign-in (as opposed to quota or network trouble).
func IsTokenRevoked(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "invalid_grant") || strings.Contains(m, "couldn't fetch token")
}

// Rclone runs the rclone binary against a rendered config. Restic starts its
// own rclone for repositories; this is for the few direct operations: writing
// the recovery bundle and, in the CLI, downloading a folder.
type Rclone struct {
	Binary string
	Exec   ExecFunc
	Config string
}

func (r Rclone) run(ctx context.Context, stdin io.Reader, args ...string) error {
	bin, ex := r.Binary, r.Exec
	if bin == "" {
		bin = "rclone"
	}
	if ex == nil {
		ex = DefaultExec
	}
	return ex(ctx, bin, args, []string{"RCLONE_CONFIG=" + r.Config}, stdin, io.Discard)
}

// Rcat writes data to target ("remote:path/file").
func (r Rclone) Rcat(ctx context.Context, target string, data []byte) error {
	return r.run(ctx, bytes.NewReader(data), "rcat", target)
}

// Copy copies src to dst; either may be a remote ("remote:path").
func (r Rclone) Copy(ctx context.Context, src, dst string) error {
	return r.run(ctx, nil, "copy", src, dst)
}

var errCloudNeedsWorker = errors.New("backup: cloud destinations are backed up by the worker only")

// CloudSession is one run's rclone configuration: a private directory holding
// rclone.conf with a remote per account. Close writes refreshed tokens back
// and deletes the directory. Every method is safe on a nil session, which
// means "no cloud destinations in play".
type CloudSession struct {
	svc      *CloudService
	rclone   Rclone
	dir      string
	rendered map[string]string    // remote -> token JSON as written
	byRemote map[string]uuid.UUID // remote -> account
	blocked  map[uuid.UUID]string // account -> status that keeps it out of the config
}

// OpenSession renders the config for every account the destinations use.
// Accounts that are not connected stay out of it, and their destinations fail
// with a reconnect hint rather than an rclone error.
func (s *CloudService) OpenSession(ctx context.Context, dests []Destination, rc Rclone) (*CloudSession, error) {
	sess := &CloudSession{
		svc: s, rendered: map[string]string{}, byRemote: map[string]uuid.UUID{}, blocked: map[uuid.UUID]string{},
	}
	seen := map[uuid.UUID]bool{}
	var creds []CloudCreds
	for _, d := range dests {
		if d.CloudAccountID == nil || seen[*d.CloudAccountID] {
			continue
		}
		seen[*d.CloudAccountID] = true
		c, err := s.credentials(ctx, *d.CloudAccountID)
		if err != nil {
			return nil, err
		}
		if c.Status != AccountConnected {
			sess.blocked[c.ID] = c.Status
			continue
		}
		creds = append(creds, c)
	}
	if len(seen) == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "hdms-rclone-") // mode 0700
	if err != nil {
		return nil, fmt.Errorf("backup: rclone config directory: %w", err)
	}
	path := filepath.Join(dir, "rclone.conf")
	if err := os.WriteFile(path, []byte(RenderRcloneConfig(creds)), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("backup: write rclone config: %w", err)
	}
	sess.dir = dir
	sess.rclone = rc
	sess.rclone.Config = path
	for _, c := range creds {
		remote := CloudRemote(c.ID)
		sess.rendered[remote] = c.Token
		sess.byRemote[remote] = c.ID
	}
	return sess, nil
}

// Apply returns r configured to use this session's rclone config.
func (s *CloudSession) Apply(r Restic) Restic {
	if s == nil || s.dir == "" {
		return r
	}
	r.ExtraEnv = append(append([]string(nil), r.ExtraEnv...), "RCLONE_CONFIG="+s.rclone.Config)
	return r
}

// Usable reports whether d can be reached in this run.
func (s *CloudSession) Usable(d Destination) error {
	if d.CloudAccountID == nil {
		return nil
	}
	if s == nil {
		return errCloudNeedsWorker
	}
	if status, blocked := s.blocked[*d.CloudAccountID]; blocked {
		return fmt.Errorf("backup: the cloud account behind %q is %s; reconnect it in Backups → Destinations", d.Name, status)
	}
	return nil
}

// PutBundle writes the recovery bundle beside d's repository in the cloud.
func (s *CloudSession) PutBundle(ctx context.Context, d Destination, bundle []byte) error {
	if err := s.Usable(d); err != nil {
		return err
	}
	return s.rclone.Rcat(ctx, CloudRemote(*d.CloudAccountID)+":"+d.Folder+"/"+RecoveryBundleFile, bundle)
}

// NoteFailure moves d's account to "revoked" when the failure says the
// provider no longer accepts the saved sign-in.
func (s *CloudSession) NoteFailure(ctx context.Context, d Destination, cause string) {
	if s == nil || d.CloudAccountID == nil || !IsTokenRevoked(cause) {
		return
	}
	reason := "The provider no longer accepts the saved sign-in. Reconnect the account."
	if err := s.svc.SetStatus(ctx, *d.CloudAccountID, AccountRevoked, reason); err != nil {
		slog.Error("backup: mark cloud account revoked", "account", d.CloudAccountID.String(), "error", err)
	}
}

// Close stores any token rclone refreshed and deletes the config directory.
// The directory is removed even when nothing else here succeeds.
func (s *CloudSession) Close(ctx context.Context) {
	if s == nil || s.dir == "" {
		return
	}
	defer func() { _ = os.RemoveAll(s.dir) }()
	raw, err := os.ReadFile(filepath.Join(s.dir, "rclone.conf")) // #nosec G304 -- our own temp directory
	if err != nil {
		slog.Error("backup: read rclone config back", "error", err)
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for remote, tok := range ParseRcloneTokens(string(raw)) {
		id, ok := s.byRemote[remote]
		if !ok || tok == "" || tok == s.rendered[remote] {
			continue
		}
		if err := s.svc.StoreToken(wctx, id, tok); err != nil {
			slog.Error("backup: store refreshed cloud token", "account", id.String(), "error", err)
		}
	}
}

// PutRecoveryBundle keeps the sealed recovery bundle beside d's repository: a
// file next to a folder repository, an upload next to a cloud one.
func PutRecoveryBundle(ctx context.Context, cloud *CloudSession, d Destination, repo Repo, bundle []byte) error {
	if d.CloudAccountID != nil {
		return cloud.PutBundle(ctx, d, bundle)
	}
	return WriteRecoveryBundle(repo, bundle)
}
```

- [ ] **Step 4: Resolve cloud destinations**

In `hdms-backend/internal/platform/backup/dest.go`, in `Resolve`, make the `"rclone"` case start with the account branch:

```go
	case "rclone":
		if d.CloudAccountID != nil {
			return Repo{Location: "rclone:" + CloudRemote(*d.CloudAccountID) + ":" + d.Folder + "/repo"}, nil
		}
		target := strings.TrimSpace(d.Target)
```

(the legacy lines below it stay as they are).

- [ ] **Step 5: Use the session in the run**

In `hdms-backend/internal/platform/backup/runner.go`:

Add to `Options` (after `RecoveryBundle`):

```go
	// Cloud holds the rendered rclone config for cloud destinations. Nil when
	// the caller has no session (the manual CLI); cloud destinations then
	// fail with a plain message.
	Cloud *CloudSession
```

Replace `copyTo` with:

```go
func (opts Options) copyTo(ctx context.Context, local Repo, d Destination, res *DestinationResult) error {
	if err := opts.Cloud.Usable(d); err != nil {
		return err
	}
	repo, err := d.Resolve(opts.AllowedRoots)
	if err != nil {
		return err
	}
	restic := opts.Restic
	if d.CloudAccountID != nil {
		restic = opts.Cloud.Apply(restic)
	}
	if err := EnsureRepo(ctx, restic, repo, &local); err != nil {
		return err
	}
	if err := restic.Copy(ctx, repo, local); err != nil {
		return err
	}
	if opts.RecoveryBundle != nil {
		if err := PutRecoveryBundle(ctx, opts.Cloud, d, repo, opts.RecoveryBundle); err != nil {
			return err
		}
	}
	forgot, err := restic.Forget(ctx, repo, RetentionPolicy{KeepLast: d.RetentionVersions})
	if err != nil {
		return err
	}
	res.Forgot = forgot
	return nil
}
```

In `recordDestinationOutcome`, before the `if opts.Pool == nil` early return, add:

```go
	if res.Outcome != OutcomeSuccess {
		opts.Cloud.NoteFailure(ctx, d, res.Error)
	}
```

In `hdms-backend/internal/platform/backup/executor.go`:

Add to the `Executor` struct:

```go
	// Cloud and Rclone let the worker reach Google Drive and OneDrive
	// destinations. Without Cloud, cloud destinations fail with a plain message.
	Cloud  *CloudService
	Rclone Rclone
```

Add after the struct:

```go
// openCloud renders the rclone settings for every cloud destination. It
// returns nil when there is nothing to render or it cannot (logged); cloud
// destinations then fail individually instead of stopping the run.
func (e *Executor) openCloud(ctx context.Context) *CloudSession {
	if e.Cloud == nil {
		return nil
	}
	dests, err := LoadAllDestinations(ctx, e.Pool)
	if err != nil {
		e.Logger.Error("backup: list destinations for cloud session", "error", err)
		return nil
	}
	sess, err := e.Cloud.OpenSession(ctx, dests, e.Rclone)
	if err != nil {
		e.Logger.Error("backup: prepare cloud accounts", "error", err)
		return nil
	}
	return sess
}
```

Replace `RunBackup`, `repoTarget`, `targets`, `RefreshSnapshots`, `Verify`, and `Test` with:

```go
// RunBackup backs up to the local repository and every enabled destination,
// then refreshes the snapshot cache.
func (e *Executor) RunBackup(ctx context.Context) (RunReport, error) {
	dests, err := LoadEnabledDestinations(ctx, e.Pool)
	if err != nil {
		return RunReport{Outcome: OutcomeFailure}, err
	}
	bundle, err := LoadRecoveryBundle(ctx, e.Pool.Pool)
	if err != nil {
		// A missing bundle must not stop a backup; log and carry on.
		e.Logger.Error("backup: load recovery bundle", "error", err)
	}
	sess := e.openCloud(ctx)
	rep, err := RunBackup(ctx, Options{
		Pool:           e.Pool,
		DatabaseURL:    e.DatabaseURL,
		BackupDir:      e.BackupDir,
		AllowedRoots:   e.AllowedRoots,
		Restic:         e.Restic,
		Destinations:   dests,
		RecoveryBundle: bundle,
		MetricsDir:     e.MetricsDir,
		Cloud:          sess,
	}, e.Now().UTC())
	sess.Close(ctx)
	e.RefreshSnapshots(ctx)
	return rep, err
}

type repoTarget struct {
	key    string
	name   string
	repo   Repo
	restic Restic
}

// targets lists the local repository plus every enabled destination, or just
// the one destination named by only. Each target carries the restic client to
// use, which for a cloud destination points at the session's rclone config.
func (e *Executor) targets(ctx context.Context, only *uuid.UUID, sess *CloudSession) ([]repoTarget, []map[string]any, error) {
	var out []repoTarget
	var problems []map[string]any
	if only == nil {
		out = append(out, repoTarget{key: LocalRepoKey, name: "local", repo: LocalRepo(e.BackupDir), restic: e.Restic})
	}
	dests, err := LoadAllDestinations(ctx, e.Pool)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range dests {
		if only != nil && d.ID != *only {
			continue
		}
		if only == nil && !d.Enabled {
			continue
		}
		problem := func(err error) {
			problems = append(problems, map[string]any{"name": d.Name, "outcome": OutcomeFailure, "error": err.Error()})
		}
		if err := sess.Usable(d); err != nil {
			problem(err)
			continue
		}
		repo, err := d.Resolve(e.AllowedRoots)
		if err != nil {
			problem(err)
			continue
		}
		restic := e.Restic
		if d.CloudAccountID != nil {
			restic = sess.Apply(restic)
		}
		out = append(out, repoTarget{key: RepoKey(d.ID), name: d.Name, repo: repo, restic: restic})
	}
	return out, problems, nil
}

// RefreshSnapshots re-reads every repository's snapshot list into the cache.
// A repository that cannot be listed keeps its previous cache.
func (e *Executor) RefreshSnapshots(ctx context.Context) {
	sess := e.openCloud(ctx)
	defer sess.Close(ctx)
	targets, _, err := e.targets(ctx, nil, sess)
	if err != nil {
		e.Logger.Error("backup: list repositories for cache refresh", "error", err)
		return
	}
	for _, t := range targets {
		snaps, err := t.restic.Snapshots(ctx, t.repo)
		if err != nil {
			e.Logger.Warn("backup: list snapshots", "repository", t.name, "error", err)
			continue
		}
		if err := ReplaceSnapshots(ctx, e.Pool.Pool, t.key, snaps, e.Now().UTC()); err != nil {
			e.Logger.Error("backup: cache snapshots", "repository", t.name, "error", err)
		}
	}
}

// Verify runs restic check on the local repository and enabled destinations
// (or on one destination) and records a job_runs row "verify".
func (e *Executor) Verify(ctx context.Context, only *uuid.UUID) (string, map[string]any) {
	started := e.Now().UTC()
	sess := e.openCloud(ctx)
	defer sess.Close(ctx)
	targets, results, err := e.targets(ctx, only, sess)
	if err != nil {
		detail := map[string]any{"error": err.Error()}
		_ = RecordJobRun(ctx, e.Pool.Pool, "verify", started, e.Now().UTC(), OutcomeFailure, detail)
		return OutcomeFailure, detail
	}
	e.RefreshSnapshots(ctx)
	failed := len(results)
	for _, t := range targets {
		res := map[string]any{"name": t.name, "outcome": OutcomeSuccess}
		if err := t.restic.Check(ctx, t.repo, verifyReadPercent); err != nil {
			res["outcome"], res["error"] = OutcomeFailure, err.Error()
			failed++
		} else if err := MarkRepoVerified(ctx, e.Pool.Pool, t.key, e.Now().UTC()); err != nil {
			e.Logger.Error("backup: mark verified", "repository", t.name, "error", err)
		}
		results = append(results, res)
	}
	outcome := OutcomeSuccess
	if failed > 0 {
		outcome = OutcomeFailure
	}
	detail := map[string]any{"repositories": results, "readDataPercent": verifyReadPercent}
	if err := RecordJobRun(ctx, e.Pool.Pool, "verify", started, e.Now().UTC(), outcome, detail); err != nil {
		e.Logger.Error("backup: record verify run", "error", err)
	}
	return outcome, detail
}

// Test proves a destination is usable by initialising (or opening) its
// repository from the worker, and records the result on the destination.
func (e *Executor) Test(ctx context.Context, id uuid.UUID) (string, map[string]any) {
	d, err := GetDestination(ctx, e.Pool, id)
	if err != nil {
		return OutcomeFailure, map[string]any{"error": err.Error()}
	}
	sess := e.openCloud(ctx)
	defer sess.Close(ctx)
	store := backupstore.New(db.Conn(ctx, e.Pool))
	fail := func(err error) (string, map[string]any) {
		sess.NoteFailure(ctx, d, err.Error())
		_ = store.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{Ok: false, ErrorText: err.Error(), ID: id})
		return OutcomeFailure, map[string]any{"name": d.Name, "error": err.Error()}
	}
	if err := sess.Usable(d); err != nil {
		return fail(err)
	}
	repo, err := d.Resolve(e.AllowedRoots)
	if err != nil {
		return fail(err)
	}
	restic := e.Restic
	if d.CloudAccountID != nil {
		restic = sess.Apply(restic)
	}
	local := LocalRepo(e.BackupDir)
	if err := EnsureRepo(ctx, e.Restic, local, nil); err != nil {
		return fail(err)
	}
	if err := EnsureRepo(ctx, restic, repo, &local); err != nil {
		return fail(err)
	}
	if bundle, err := LoadRecoveryBundle(ctx, e.Pool.Pool); err != nil {
		return fail(err)
	} else if bundle != nil {
		if err := PutRecoveryBundle(ctx, sess, d, repo, bundle); err != nil {
			return fail(err)
		}
	}
	if err := store.MarkDestinationInitialized(ctx, id); err != nil {
		return fail(err)
	}
	if err := store.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{Ok: true, ID: id}); err != nil {
		e.Logger.Error("backup: record destination outcome", "destination", d.Name, "error", err)
	}
	return OutcomeSuccess, map[string]any{"name": d.Name, "repository": repo.Location}
}
```

(`ProcessNext` is unchanged.)

In `hdms-backend/cmd/hdms-cli/worker.go`, in the `exec := &backup.Executor{…}` literal add:

```go
		Cloud:        &backup.CloudService{Q: pool.Pool, Key: cfg.CredentialEncKey, Now: time.Now},
```

- [ ] **Step 6: Run the unit tests**

Run: `cd hdms-backend && go build ./... && go test ./internal/platform/backup/ ./cmd/hdms-cli/`
Expected: PASS — including the existing runner tests (legacy `rclone` destinations have no account, so they take the old path).

- [ ] **Step 7: Write the integration tests (Review Focus 1 and 2)**

Create `hdms-backend/test/integration/backup_cloud_run_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// scriptedRestic answers restic the way the unit tests' fake does, and lets a
// test react to each call: read the rendered config, rewrite a token the way
// rclone does, or fail the call.
type scriptedRestic struct {
	mu    sync.Mutex
	calls []string
	onCall func(joined string, env []string) error
}

func (s *scriptedRestic) exec(_ context.Context, _ string, args, env []string, stdin io.Reader, stdout io.Writer) error {
	joined := strings.Join(args, " ")
	s.mu.Lock()
	s.calls = append(s.calls, joined)
	s.mu.Unlock()
	if s.onCall != nil {
		if err := s.onCall(joined, env); err != nil {
			return err
		}
	}
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	switch {
	case strings.Contains(joined, "backup --stdin"):
		_, _ = io.WriteString(stdout, `{"message_type":"summary","snapshot_id":"snap1","total_bytes_processed":100,"data_added":40}`)
	case strings.Contains(joined, "forget"):
		_, _ = io.WriteString(stdout, `[]`)
	case strings.Contains(joined, "snapshots"):
		_, _ = io.WriteString(stdout, `[]`)
	}
	return nil
}

func rcloneConfigIn(env []string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "RCLONE_CONFIG="); ok {
			return v
		}
	}
	return ""
}

type rcatCall struct{ target, stdin string }

func fakeRcloneRcat(calls *[]rcatCall) backup.ExecFunc {
	return func(_ context.Context, _ string, args, _ []string, stdin io.Reader, _ io.Writer) error {
		b, _ := io.ReadAll(stdin)
		*calls = append(*calls, rcatCall{target: args[len(args)-1], stdin: string(b)})
		return nil
	}
}

func fakeDumpStream(_ context.Context, _ string, w io.Writer) error {
	_, err := io.WriteString(w, "dump-payload")
	return err
}

func TestCloudDestinationRunUsesTheRenderedConfigAndCleansUp(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	d, err := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{Name: "Drive", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	dests, _ := backup.LoadEnabledDestinations(ctx, pool)
	var rcats []rcatCall
	sess, err := svc.OpenSession(ctx, dests, backup.Rclone{Exec: fakeRcloneRcat(&rcats)})
	if err != nil || sess == nil {
		t.Fatalf("OpenSession = %v, %v", sess, err)
	}

	var seenConfig, configDir string
	var configMode os.FileMode
	repoPrefix := "-r rclone:" + backup.CloudRemote(acct.ID) + ":hdms-backups/repo"
	r := &scriptedRestic{}
	r.onCall = func(joined string, env []string) error {
		if !strings.HasPrefix(joined, repoPrefix) {
			return nil
		}
		path := rcloneConfigIn(env)
		if path == "" {
			return errors.New("the cloud repository was reached without RCLONE_CONFIG")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		st, _ := os.Stat(path)
		configMode, configDir = st.Mode().Perm(), filepath.Dir(path)
		if seenConfig == "" {
			seenConfig = string(raw)
			// rclone refreshes a token by rewriting its config file.
			refreshed := strings.Replace(string(raw), `"access_token":"at-1"`, `"access_token":"at-2"`, 1)
			return os.WriteFile(path, []byte(refreshed), 0o600)
		}
		return nil
	}

	bundle := []byte("SEALED-BUNDLE")
	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: "postgres://fake", BackupDir: t.TempDir(), Dump: fakeDumpStream,
		Restic:       backup.Restic{Binary: "restic", Password: "p", Exec: r.exec},
		Destinations: dests, RecoveryBundle: bundle, Cloud: sess,
	}, time.Now().UTC())
	if err != nil || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("RunBackup = %+v, %v", rep, err)
	}
	if !strings.Contains(seenConfig, "["+backup.CloudRemote(acct.ID)+"]") || !strings.Contains(seenConfig, "client_secret = s3cret-value") ||
		!strings.Contains(seenConfig, "scope = drive.file") {
		t.Fatalf("rendered config was:\n%s", seenConfig)
	}
	if configMode != 0o600 {
		t.Fatalf("rclone.conf mode = %o, want 600", configMode)
	}
	if len(rcats) != 1 || rcats[0].target != backup.CloudRemote(acct.ID)+":hdms-backups/"+backup.RecoveryBundleFile || rcats[0].stdin != "SEALED-BUNDLE" {
		t.Fatalf("recovery bundle upload = %+v", rcats)
	}

	sess.Close(ctx)
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("the config directory outlived the run: %v", err)
	}
	var tokenEnc []byte
	_ = pool.QueryRow(ctx, `SELECT token_enc FROM backup_cloud_accounts WHERE id = $1`, acct.ID).Scan(&tokenEnc)
	plain, _ := backup.Decrypt(cloudKey, tokenEnc)
	if !strings.Contains(string(plain), `"access_token":"at-2"`) {
		t.Fatalf("the token rclone refreshed was not stored: %s", plain)
	}
	var lastOK *time.Time
	_ = pool.QueryRow(ctx, `SELECT last_ok_at FROM backup_destinations WHERE id = $1`, d.ID).Scan(&lastOK)
	if lastOK == nil {
		t.Fatal("destination success not recorded")
	}
}

func TestCloudRevokedTokenMarksTheAccountAndKeepsOtherDestinations(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	cloud, _ := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{Name: "Drive", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2}, "test")
	root := t.TempDir()
	nasDir := filepath.Join(root, "nas")
	_ = os.Mkdir(nasDir, 0o750)
	nas, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{Name: "NAS", Target: nasDir, Enabled: true, RetentionVersions: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	dests, _ := backup.LoadEnabledDestinations(ctx, pool)
	sess, err := svc.OpenSession(ctx, dests, backup.Rclone{Exec: fakeRcloneRcat(new([]rcatCall))})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close(ctx)

	r := &scriptedRestic{onCall: func(joined string, _ []string) error {
		if strings.Contains(joined, "rclone:"+backup.CloudRemote(acct.ID)) && strings.Contains(joined, " copy ") {
			return errors.New("couldn't fetch token: invalid_grant: Token has been expired or revoked.")
		}
		return nil
	}}
	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: "postgres://fake", BackupDir: t.TempDir(), Dump: fakeDumpStream, AllowedRoots: []string{root},
		Restic: backup.Restic{Binary: "restic", Password: "p", Exec: r.exec}, Destinations: dests, Cloud: sess,
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != backup.OutcomeDegraded {
		t.Fatalf("outcome = %q, want degraded (local and the NAS are fine)", rep.Outcome)
	}
	byName := map[string]backup.DestinationResult{}
	for _, d := range rep.Destinations {
		byName[d.Name] = d
	}
	if byName["NAS"].Outcome != backup.OutcomeSuccess || byName["Drive"].Outcome != backup.OutcomeFailure {
		t.Fatalf("destinations = %+v", rep.Destinations)
	}
	got, _ := svc.Get(ctx, acct.ID)
	if got.Status != backup.AccountRevoked || got.LastError == "" {
		t.Fatalf("account = %+v, want revoked with a reason", got)
	}
	c, _ := backup.GetDestination(ctx, pool, cloud.ID)
	if !strings.Contains(c.LastError, "invalid_grant") {
		t.Fatalf("destination last_error = %q", c.LastError)
	}
	n, _ := backup.GetDestination(ctx, pool, nas.ID)
	if n.LastOkAt == nil {
		t.Fatal("the NAS copy was not recorded as successful")
	}

	// The next run does not even try the revoked account, and says what to do.
	dests, _ = backup.LoadEnabledDestinations(ctx, pool)
	sess2, err := svc.OpenSession(ctx, dests, backup.Rclone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close(ctx)
	r2 := &scriptedRestic{}
	rep, _ = backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: "postgres://fake", BackupDir: t.TempDir(), Dump: fakeDumpStream, AllowedRoots: []string{root},
		Restic: backup.Restic{Binary: "restic", Password: "p", Exec: r2.exec}, Destinations: dests, Cloud: sess2,
	}, time.Now().UTC())
	for _, call := range r2.calls {
		if strings.Contains(call, "acct_") {
			t.Fatalf("restic was pointed at a revoked account: %s", call)
		}
	}
	for _, d := range rep.Destinations {
		if d.Name == "Drive" && !strings.Contains(d.Error, "reconnect") {
			t.Fatalf("Drive error = %q, want a reconnect hint", d.Error)
		}
	}
}

func TestCloudSessionConfigIsPrivateAndAlwaysRemoved(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	_, _ = backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{Name: "Drive", Folder: "f", Enabled: true, RetentionVersions: 2}, "test")
	dests, _ := backup.LoadEnabledDestinations(ctx, pool)

	sess, err := svc.OpenSession(ctx, dests, backup.Rclone{})
	if err != nil {
		t.Fatal(err)
	}
	path := rcloneConfigIn(sess.Apply(backup.Restic{}).ExtraEnv)
	dir := filepath.Dir(path)
	di, _ := os.Stat(dir)
	fi, _ := os.Stat(path)
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("directory %o / file %o, want 700 / 600", di.Mode().Perm(), fi.Mode().Perm())
	}
	// A run that failed outright still closes the session.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	sess.Close(cancelled)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory still there after Close: %v", err)
	}

	// With no cloud destination there is no session and nothing on disk.
	none, err := svc.OpenSession(ctx, nil, backup.Rclone{})
	if err != nil || none != nil {
		t.Fatalf("OpenSession(no destinations) = %v, %v", none, err)
	}
}
```

- [ ] **Step 8: Run the integration tests**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestCloud'`
Expected: PASS.

- [ ] **Step 9: Mutation checks**

1. In `copyTo`, skip the `opts.Cloud.NoteFailure` effect by deleting the `NoteFailure` block in `recordDestinationOutcome`; run `TestCloudRevokedTokenMarksTheAccountAndKeepsOtherDestinations` → expect FAIL (account not revoked). Revert.
2. In `Close`, delete the `defer os.RemoveAll`; run `TestCloudSessionConfigIsPrivateAndAlwaysRemoved` → expect FAIL. Revert.
3. In `Close`, change `tok == s.rendered[remote]` to `true`; run `TestCloudDestinationRunUsesTheRenderedConfigAndCleansUp` → expect FAIL (refreshed token not stored). Revert.

- [ ] **Step 10: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./...`
Expected: no lint output, tests PASS.

```bash
git add hdms-backend
git commit -m "feat(backup): back up to Google Drive and OneDrive through a per-run rclone session"
```

---

### Task 4: A restore keeps the cloud accounts

**Files:**
- Modify: `hdms-backend/internal/platform/recovery/copyforward.go`
- Test: `hdms-backend/test/integration/recovery_restore_test.go`

**Interfaces:**
- Consumes: Task 2 tables. `replaceTables` already copies `backup_destinations`; its rows now reference `backup_cloud_accounts`.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append to `hdms-backend/test/integration/recovery_restore_test.go`:

```go
func TestRecoveryRestoreKeepsCloudAccountsAndTheirDestinations(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()

	// Connected after the snapshot was taken, so only the live database has it.
	live := connectTo(t, f.liveURL)
	const account = "11111111-1111-4111-8111-111111111111"
	if _, err := live.Exec(ctx,
		`INSERT INTO backup_cloud_accounts (id, provider, name, client_id, token_enc, status, updated_by)
		 VALUES ('`+account+`', 'google_drive', 'Hospital Drive', 'cid', $1, 'connected', 'test')`, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Exec(ctx,
		`INSERT INTO backup_destinations (id, name, kind, target, provider, enabled, retention_versions, updated_by, cloud_account_id, folder)
		 VALUES ('22222222-2222-4222-8222-222222222222', 'Drive', 'rclone', 'cloud:`+account+`/hdms-backups', 'google_drive', true, 2, 'test', '`+account+`', 'hdms-backups')`); err != nil {
		t.Fatal(err)
	}
	_ = live.Close(ctx)

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.Warning != "" {
		t.Fatalf("restore state = %+v", st)
	}
	back := connectTo(t, f.liveURL)
	if n := countOf(t, back, `SELECT count(*) FROM backup_cloud_accounts WHERE id = '`+account+`' AND status = 'connected' AND length(token_enc) = 3`); n != 1 {
		t.Fatalf("cloud account after restore: %d row(s), want 1 with its token", n)
	}
	if n := countOf(t, back, `SELECT count(*) FROM backup_destinations WHERE cloud_account_id = '`+account+`' AND folder = 'hdms-backups'`); n != 1 {
		t.Fatalf("cloud destination after restore: %d row(s), want 1", n)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestRecoveryRestoreKeepsCloudAccounts`
Expected: FAIL — the restore stops at `copy_forward_failed` (the destination row references an account the restored database does not have).

- [ ] **Step 3: Copy accounts forward before destinations**

In `hdms-backend/internal/platform/recovery/copyforward.go`, replace the `replaceTables` declaration:

```go
var replaceTables = []string{
	"backup_schedule", "backup_cloud_accounts", "backup_destinations", "backup_requests", "backup_snapshots",
	"backup_recovery_key", "system_state", "job_runs", "restore_history",
}
```

(Accounts sit before destinations: destinations reference them, so inserts go accounts first and the reversed deletes remove destinations first.)

- [ ] **Step 4: Run it to see it pass, then the neighbours**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestRecovery'`
Expected: PASS (all recovery integration tests).

- [ ] **Step 5: Mutation check and commit**

Swap `"backup_cloud_accounts"` after `"backup_destinations"` in the list and re-run the new test: expect FAIL (foreign key). Revert.

```bash
git add hdms-backend/internal/platform/recovery/copyforward.go hdms-backend/test/integration/recovery_restore_test.go
git commit -m "fix(recovery): carry cloud accounts through a restore before their destinations"
```

---

### Task 5: API — cloud accounts and cloud destinations

**Files:**
- Modify: `hdms-backend/api/openapi.yaml`
- Create: `hdms-backend/internal/apiserver/backup_cloud.go`
- Modify: `hdms-backend/internal/apiserver/backup.go`, `server.go`, `hdms-backend/cmd/hdms-api/main.go`
- Modify: `hdms-backend/internal/platform/auth/roles.go`, `kioskscope.go`
- Modify: `hdms-backend/test/integration/httpserver_test.go` (one line)
- Test: `hdms-backend/test/integration/backup_cloud_http_test.go`
- Regenerate: `hdms-backend/internal/platform/httpx/gen/`, `hdms-frontend/packages/api-client`

**Interfaces:**
- Consumes: Task 2 (`CloudService`, `CreateCloudDestination`, errors), Task 1 (`ErrProvider*`).
- Produces (HTTP): `GET/POST /v1/backup/cloud-accounts`, `GET/DELETE /v1/backup/cloud-accounts/{id}`, `POST /v1/backup/cloud-accounts/{id}/reconnect`; `POST /v1/backup/destinations` accepts `cloudAccountId` + `folder` instead of `target`; `BackupDestination` gains `provider`, `cloudAccountId`, `folder`. Generated types used below: `gen.BackupCloudProvider`, `gen.BackupCloudAccount`, `gen.BackupCloudAccountStatus`, `gen.BackupCloudAccountInput`, `gen.BackupCloudAccountList`, `gen.BackupCloudSignIn`. Problem types: `cloud-unavailable` 503, `cloud-account-in-use` 409, `cloud-account-not-connected` 409, `cloud-provider-rejected` 422, `cloud-provider-unreachable` 502.

- [ ] **Step 1: Extend the OpenAPI contract**

In `hdms-backend/api/openapi.yaml`, after the `/backup/destinations/{id}/test:` block (before `/backup/locations:`) add:

```yaml
  /backup/cloud-accounts:
    get:
      operationId: listBackupCloudAccounts
      summary: Google Drive and OneDrive sign-ins used for backup copies.
      tags: [backup]
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupCloudAccountList" }
        default:
          $ref: "#/components/responses/ProblemResponse"
    post:
      operationId: createBackupCloudAccount
      summary: Register an OAuth client and start the device sign-in.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupCloudAccountInput" }
      responses:
        "201":
          description: Sign-in started. Show the code; poll the account until it is connected.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupCloudSignIn" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/cloud-accounts/{id}:
    get:
      operationId: getBackupCloudAccount
      summary: Return the account, asking the provider once if a sign-in is waiting and due.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupCloudAccount" }
        default:
          $ref: "#/components/responses/ProblemResponse"
    delete:
      operationId: deleteBackupCloudAccount
      summary: Forget the sign-in. Refused while a destination uses it.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "204":
          description: Deleted.
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/cloud-accounts/{id}/reconnect:
    post:
      operationId: reconnectBackupCloudAccount
      summary: Restart the sign-in for an expired or revoked account.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          description: Sign-in restarted.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupCloudSignIn" }
        default:
          $ref: "#/components/responses/ProblemResponse"
```

In `components/schemas`, replace `BackupDestination` and `BackupDestinationInput` and add the cloud schemas:

```yaml
    BackupDestination:
      type: object
      required: [id, name, target, provider, enabled, retentionVersions]
      properties:
        id: { type: string }
        name: { type: string }
        target: { type: string }
        provider: { type: string, description: "lan, google_drive or onedrive" }
        cloudAccountId: { type: string }
        folder: { type: string }
        enabled: { type: boolean }
        retentionVersions: { type: integer }
        initializedAt: { type: string, format: date-time }
        lastOkAt: { type: string, format: date-time }
        lastError: { type: string }

    BackupDestinationInput:
      type: object
      description: Give either target (a folder on a connected drive), or cloudAccountId with folder.
      required: [name, retentionVersions]
      properties:
        name: { type: string, minLength: 1, maxLength: 100 }
        target: { type: string }
        cloudAccountId: { type: string }
        folder: { type: string }
        enabled: { type: boolean, default: true }
        retentionVersions: { type: integer, minimum: 1, maximum: 100 }

    BackupCloudProvider:
      type: string
      enum: [google_drive, onedrive]

    BackupCloudAccount:
      type: object
      required: [id, provider, name, clientId, tenant, status]
      properties:
        id: { type: string }
        provider: { $ref: "#/components/schemas/BackupCloudProvider" }
        name: { type: string }
        clientId: { type: string }
        tenant: { type: string }
        accountEmail: { type: string }
        status: { type: string, enum: [pending, connected, expired, revoked] }
        lastError: { type: string }
        connectedAt: { type: string, format: date-time }

    BackupCloudAccountList:
      type: object
      required: [items]
      properties:
        items: { type: array, items: { $ref: "#/components/schemas/BackupCloudAccount" } }

    BackupCloudAccountInput:
      type: object
      required: [provider, name, clientId]
      properties:
        provider: { $ref: "#/components/schemas/BackupCloudProvider" }
        name: { type: string, minLength: 1, maxLength: 100 }
        clientId: { type: string, minLength: 1, maxLength: 300 }
        clientSecret: { type: string, maxLength: 300, description: "Required for Google Drive." }
        tenant: { type: string, maxLength: 100, description: "OneDrive directory (tenant) ID; default common." }

    BackupCloudSignIn:
      type: object
      required: [id, userCode, verificationUri, expiresAt]
      properties:
        id: { type: string }
        userCode: { type: string }
        verificationUri: { type: string }
        expiresAt: { type: string, format: date-time }
```

Regenerate: `task generate:backend` (equivalent to `cd hdms-backend/api && oapi-codegen -config oapi-codegen.yaml openapi.yaml`) and `task generate:frontend`.
Expected: `go build ./...` now fails in `internal/apiserver` only (missing handler methods and the changed `Target` type) — that is the next steps' job.

- [ ] **Step 2: Classify the new routes**

In `hdms-backend/internal/platform/auth/roles.go`, after the `"POST /v1/backup/locations/check": "admin",` line add:

```go
	"GET /v1/backup/cloud-accounts":                 "admin",
	"POST /v1/backup/cloud-accounts":                "admin",
	"GET /v1/backup/cloud-accounts/{id}":            "admin",
	"DELETE /v1/backup/cloud-accounts/{id}":         "admin",
	"POST /v1/backup/cloud-accounts/{id}/reconnect": "admin",
```

In `hdms-backend/internal/platform/auth/kioskscope.go`, after the `"POST /v1/backup/locations/check": {},` line add:

```go
	"GET /v1/backup/cloud-accounts":                 {},
	"POST /v1/backup/cloud-accounts":                {},
	"GET /v1/backup/cloud-accounts/{id}":            {},
	"DELETE /v1/backup/cloud-accounts/{id}":         {},
	"POST /v1/backup/cloud-accounts/{id}/reconnect": {},
```

- [ ] **Step 3: Wire the service into the server**

In `hdms-backend/internal/apiserver/server.go`, add to `BackupConsoleConfig` (after `Locations`):

```go
	// Cloud signs in to and stores Google Drive and OneDrive accounts. Nil
	// means cloud backup is not configured; its endpoints then answer 503.
	Cloud *backup.CloudService
```

In `hdms-backend/cmd/hdms-api/main.go`, in the `apiserver.BackupConsoleConfig{…}` literal add:

```go
		Cloud: &backup.CloudService{
			Q:    pool.Pool,
			Key:  cfg.CredentialEncKey,
			Flow: &backup.DeviceFlow{},
			Now:  time.Now,
		},
```

- [ ] **Step 4: Write the failing HTTP test**

In `hdms-backend/test/integration/httpserver_test.go`, in the `apiserver.BackupConsoleConfig{…}` literal (next to `Locations:`) add `Cloud: newTestCloud(t, pool, credEncKey),`.

Create `hdms-backend/test/integration/backup_cloud_http_test.go`:

```go
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
		"both":    {Name: "x", Target: strPtr("/tmp"), CloudAccountId: &si.Id, Folder: strPtr("f"), RetentionVersions: 2},
		"neither": {Name: "x", RetentionVersions: 2},
		"no folder": {Name: "x", CloudAccountId: &si.Id, RetentionVersions: 2},
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
```

`strPtr` already exists in the `integration` package (`settings_test.go`); do not redefine it.

- [ ] **Step 5: Write the handlers**

Create `hdms-backend/internal/apiserver/backup_cloud.go`:

```go
package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// cloud returns the cloud service, or answers 503 and returns nil when cloud
// backup is not configured on this server.
func (s *Server) cloud(w http.ResponseWriter, r *http.Request) *backup.CloudService {
	if s.backupCfg.Cloud == nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("cloud-unavailable", "Cloud backup is not configured", http.StatusServiceUnavailable))
		return nil
	}
	return s.backupCfg.Cloud
}

func cloudDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mapCloudAccount(a backup.CloudAccount) gen.BackupCloudAccount {
	out := gen.BackupCloudAccount{
		Id:          a.ID.String(),
		Provider:    gen.BackupCloudProvider(a.Provider),
		Name:        a.Name,
		ClientId:    a.ClientID,
		Tenant:      a.Tenant,
		Status:      gen.BackupCloudAccountStatus(a.Status),
		ConnectedAt: a.ConnectedAt,
	}
	if a.AccountEmail != "" {
		out.AccountEmail = strPtr(a.AccountEmail)
	}
	if a.LastError != "" {
		out.LastError = strPtr(a.LastError)
	}
	return out
}

func mapCloudSignIn(si backup.SignIn) gen.BackupCloudSignIn {
	return gen.BackupCloudSignIn{
		Id: si.AccountID.String(), UserCode: si.UserCode, VerificationUri: si.VerificationURI, ExpiresAt: si.ExpiresAt,
	}
}

func (s *Server) ListBackupCloudAccounts(w http.ResponseWriter, r *http.Request) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	accounts, err := svc.List(r.Context())
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupCloudAccount, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, mapCloudAccount(a))
	}
	writeJSON(w, http.StatusOK, gen.BackupCloudAccountList{Items: items})
}

func (s *Server) CreateBackupCloudAccount(w http.ResponseWriter, r *http.Request) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	body, ok := decodeJSON[gen.BackupCloudAccountInput](w, r)
	if !ok {
		return
	}
	si, err := svc.Start(r.Context(), backup.CloudAccountInput{
		Provider: string(body.Provider), Name: body.Name, ClientID: body.ClientId,
		ClientSecret: cloudDeref(body.ClientSecret), Tenant: cloudDeref(body.Tenant),
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	// The payload names the account and client, never a secret.
	s.recordBackupAudit(r, "backup.cloud_account_created", "backup_cloud_account:"+si.AccountID.String(), map[string]any{
		"name": body.Name, "provider": string(body.Provider), "clientId": body.ClientId,
	})
	writeJSON(w, http.StatusCreated, mapCloudSignIn(si))
}

func (s *Server) GetBackupCloudAccount(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	before, err := svc.Get(r.Context(), id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	after, err := svc.Poll(r.Context(), id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if before.Status != backup.AccountConnected && after.Status == backup.AccountConnected {
		s.recordBackupAudit(r, "backup.cloud_account_connected", "backup_cloud_account:"+id.String(), map[string]any{
			"name": after.Name, "provider": after.Provider, "accountEmail": after.AccountEmail,
		})
	}
	writeJSON(w, http.StatusOK, mapCloudAccount(after))
}

func (s *Server) ReconnectBackupCloudAccount(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	si, err := svc.Reconnect(r.Context(), id, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.cloud_account_reconnect_requested", "backup_cloud_account:"+id.String(), nil)
	writeJSON(w, http.StatusOK, mapCloudSignIn(si))
}

func (s *Server) DeleteBackupCloudAccount(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	before, err := svc.Get(r.Context(), id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if err := svc.Delete(r.Context(), id); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.cloud_account_deleted", "backup_cloud_account:"+id.String(), map[string]any{
		"name": before.Name, "provider": before.Provider,
	})
	w.WriteHeader(http.StatusNoContent)
}

// createCloudBackupDestination handles POST /backup/destinations when the
// body names a cloud account instead of a drive folder.
func (s *Server) createCloudBackupDestination(w http.ResponseWriter, r *http.Request, body gen.BackupDestinationInput, enabled bool) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	accountID, ok := parseBackupID(w, r, *body.CloudAccountId)
	if !ok {
		return
	}
	acct, err := svc.Get(r.Context(), accountID)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	d, err := backup.CreateCloudDestination(r.Context(), s.pool, acct, backup.CloudDestinationInput{
		Name: body.Name, Folder: *body.Folder, Enabled: enabled, RetentionVersions: body.RetentionVersions,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_created", "backup_destination:"+d.ID.String(), map[string]any{
		"name": d.Name, "provider": d.Provider, "cloudAccountId": accountID.String(), "folder": d.Folder,
		"retentionVersions": d.RetentionVersions,
	})
	writeJSON(w, http.StatusCreated, mapBackupDestination(d))
}
```

- [ ] **Step 6: Update the destination handlers and error mapping**

In `hdms-backend/internal/apiserver/backup.go`:

1. In `writeBackupError`, extend the first case's list with `errors.Is(err, backup.ErrInvalidCloudAccount)`, extend the not-found case with `errors.Is(err, backup.ErrCloudAccountNotFound)`, and add before `default:`:

```go
	case errors.Is(err, backup.ErrCloudAccountInUse):
		httpx.WriteProblem(w, r, httpx.NewProblem("cloud-account-in-use", "A destination still uses this account", http.StatusConflict))
	case errors.Is(err, backup.ErrCloudAccountNotConnected):
		httpx.WriteProblem(w, r, httpx.NewProblem("cloud-account-not-connected", "The account is not connected", http.StatusConflict))
	case errors.Is(err, backup.ErrProviderRejected):
		p := httpx.NewProblem("cloud-provider-rejected", "The provider rejected the request", http.StatusUnprocessableEntity)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrProviderUnreachable):
		httpx.WriteProblem(w, r, httpx.NewProblem("cloud-provider-unreachable", "Cannot reach the sign-in provider", http.StatusBadGateway))
```

2. In `mapBackupDestination` add after `Target: d.Target,`:

```go
		Provider:          d.Provider,
```

and before `return out`:

```go
	if d.CloudAccountID != nil {
		out.CloudAccountId = strPtr(d.CloudAccountID.String())
		out.Folder = strPtr(d.Folder)
	}
```

3. Replace the start of `CreateBackupDestination` (from `body, ok := decodeJSON…` through the `check, err := …` line's use of `body.Target`) so it validates the shape and branches:

```go
	body, ok := decodeJSON[gen.BackupDestinationInput](w, r)
	if !ok {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	hasPath := body.Target != nil && *body.Target != ""
	hasCloud := body.CloudAccountId != nil
	if hasPath == hasCloud || (hasCloud && (body.Folder == nil || *body.Folder == "")) {
		p := httpx.NewProblem("validation-error", "Validation error", http.StatusUnprocessableEntity)
		p.Detail = "give either target, or cloudAccountId with folder"
		httpx.WriteProblem(w, r, p)
		return
	}
	if hasCloud {
		s.createCloudBackupDestination(w, r, body, enabled)
		return
	}
	target := *body.Target
	// The wizard already checked the folder; check again so the rules hold
	// for any caller, and so a folder that changed since is caught.
	check, err := s.backupCfg.Locations.Check(r.Context(), target)
```

and in the rest of that function replace the two remaining uses of `body.Target` (`Target: body.Target` in the input struct) with `Target: target`.

- [ ] **Step 7: Run the build, the new tests and the neighbours**

Run: `cd hdms-backend && go build ./... && go test ./... && go test -race -tags=integration ./test/integration/ -run 'TestHTTPCloud|TestHTTPBackup|TestEveryOperationHasAKioskScopeClassification|TestEveryRoute'`
Expected: PASS. If a route-classification test names a route, add it to the missing table from Step 2.

- [ ] **Step 8: Mutation check, gate, commit**

Make `mapCloudAccount` set `ClientSecret`-like leakage impossible by construction (no secret field exists in `CloudAccount`) — the audit test is the guard: temporarily add `"clientSecret": cloudDeref(body.ClientSecret)` to the create audit payload; run `TestHTTPCloudAccountSignInAndDestination` → expect FAIL (`audit event(s) contain a secret`). Revert.

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./...`
Expected: no output.

```bash
git add hdms-backend hdms-frontend/packages/api-client
git commit -m "feat(api): cloud accounts, device sign-in and cloud backup destinations"
```

---

### Task 6: Admin console — connect an account, add a cloud destination

**Files:**
- Create: `hdms-frontend/apps/admin/src/components/backups/use-cloud-accounts.ts`, `cloud-format.ts`, `cloud-connect-dialog.tsx`, `cloud-accounts-section.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/backups/destination-wizard.tsx`, `destinations-tab.tsx`
- Modify: `hdms-frontend/apps/admin/src/i18n/en.ts`, `ja.ts`
- Test: `hdms-frontend/apps/admin/src/__tests__/backups-cloud.test.tsx`

**Interfaces:**
- Consumes (generated by Task 5): `listBackupCloudAccounts`, `createBackupCloudAccount`, `getBackupCloudAccount`, `reconnectBackupCloudAccount`, `deleteBackupCloudAccount`, `createBackupDestination` (now with `cloudAccountId`, `folder`), types `BackupCloudAccount`, `BackupCloudSignIn`, `BackupCloudProvider`, `BackupDestination` (now with `provider`, `cloudAccountId?`, `folder?`).
- Produces: `useCloudAccounts()`, `CLOUD_ACCOUNTS_KEY`, `cloudPoll` (test seam: `{ ms: 5000 }`), `isCloudFolder(raw): boolean`, `destinationLocation(d, config, label): string`, `CloudConnectDialog`, `CloudAccountsSection`.

- [ ] **Step 1: Add the strings**

In `hdms-frontend/apps/admin/src/i18n/en.ts`, inside `backups`, add a sibling `cloud` object next to `destinations`, and inside `backups.wizard` add `cloud`; remove `wizard.where.comingSoon`:

```ts
    cloud: {
      title: "Google Drive and OneDrive accounts",
      description: "Sign-ins HDMS uses to copy backups to the cloud. Backups are encrypted with your backup key before they leave the server.",
      add: "Connect an account",
      empty: "No accounts connected yet.",
      providers: { google_drive: "Google Drive", onedrive: "OneDrive" },
      status: {
        pending: "Waiting for sign-in",
        connected: "Connected",
        expired: "Sign-in expired",
        revoked: "Access removed",
      },
      reconnect: "Reconnect",
      disconnect: "Disconnect",
      disconnectTitle: "Disconnect this account?",
      disconnectBody: "HDMS forgets the sign-in for {name}. Backups already in the cloud are not deleted.",
      inUse: "A backup destination still uses this account. Delete the destination first.",
      disconnected: "Account disconnected",
      connect: {
        title: "Connect a cloud account",
        provider: "Service",
        name: "Name",
        nameHelp: "A label for this sign-in, for example Hospital IT Drive.",
        clientId: "OAuth client ID",
        clientSecret: "OAuth client secret",
        clientSecretHelp: "Google needs the secret that came with the client ID.",
        tenant: "Directory (tenant) ID",
        tenantHelp: "Leave as common unless IT registered the app for one directory only.",
        help: "IT registers the OAuth client once. The steps are in the runbook cloud-backup.md.",
        start: "Get sign-in code",
        cancel: "Cancel",
        close: "Close",
        done: "Done",
        codeHeading: "Finish signing in on any device",
        codeSteps: "Open {uri} and enter this code:",
        waiting: "Waiting for you to finish signing in…",
        connectedAs: "Connected as {email}.",
        connected: "Connected.",
        retry: "Start again",
        errors: {
          rejected: "The provider did not accept this client. Check the client ID, the secret and the directory ID.",
          unreachable: "Could not reach the sign-in service. Check the server's internet connection and try again.",
          unavailable: "Cloud backup is not set up on this server.",
          generic: "Could not start the sign-in.",
          required: "Fill in the name and client ID (and the secret for Google).",
        },
      },
    },
```

and in `wizard`:

```ts
      cloud: {
        accountHeading: "Which cloud account?",
        noAccounts: "No account is connected yet.",
        connectNew: "Connect a new account",
        privacy: "Backups are encrypted before upload. Make sure data-protection approval exists before sending hospital data to a cloud service.",
        folderHeading: "Folder in the drive",
        folderHelp: "HDMS creates this folder and keeps its backups inside it. Google Drive only lets HDMS see folders it created itself, so there is nothing to browse.",
        folderName: "Folder name",
        invalidFolder: "Use letters, digits, spaces, dots, dashes and underscores, up to three levels separated by /.",
        useThis: "Use this folder",
        defaultName: "Cloud drive",
      },
```

In `hdms-frontend/apps/admin/src/i18n/ja.ts`, the matching entries:

```ts
    cloud: {
      title: "Google ドライブ・OneDrive のアカウント",
      description: "バックアップをクラウドへコピーするためのサインインです。バックアップはサーバーを出る前にバックアップ用の鍵で暗号化されます。",
      add: "アカウントを接続",
      empty: "接続されたアカウントはまだありません。",
      providers: { google_drive: "Google ドライブ", onedrive: "OneDrive" },
      status: {
        pending: "サインイン待ち",
        connected: "接続済み",
        expired: "サインインの期限切れ",
        revoked: "アクセスが解除されました",
      },
      reconnect: "再接続",
      disconnect: "接続を解除",
      disconnectTitle: "このアカウントの接続を解除しますか？",
      disconnectBody: "{name} のサインイン情報を HDMS から削除します。クラウド上のバックアップは削除されません。",
      inUse: "このアカウントを使うバックアップ先が残っています。先にバックアップ先を削除してください。",
      disconnected: "接続を解除しました",
      connect: {
        title: "クラウドアカウントを接続",
        provider: "サービス",
        name: "名前",
        nameHelp: "このサインインの呼び名です。例：情報システム部 ドライブ",
        clientId: "OAuth クライアント ID",
        clientSecret: "OAuth クライアント シークレット",
        clientSecretHelp: "Google ではクライアント ID に付属するシークレットが必要です。",
        tenant: "ディレクトリ（テナント）ID",
        tenantHelp: "特定のディレクトリ専用に登録した場合を除き、common のままにしてください。",
        help: "OAuth クライアントは IT 担当者が一度だけ登録します。手順は運用手順書 cloud-backup.md にあります。",
        start: "サインインコードを取得",
        cancel: "キャンセル",
        close: "閉じる",
        done: "完了",
        codeHeading: "別の端末でサインインを完了してください",
        codeSteps: "{uri} を開き、次のコードを入力します：",
        waiting: "サインインの完了を待っています…",
        connectedAs: "{email} として接続しました。",
        connected: "接続しました。",
        retry: "やり直す",
        errors: {
          rejected: "プロバイダーがこのクライアントを受け付けませんでした。クライアント ID、シークレット、ディレクトリ ID を確認してください。",
          unreachable: "サインインのサービスに接続できません。サーバーのインターネット接続を確認して、もう一度お試しください。",
          unavailable: "このサーバーではクラウドバックアップが設定されていません。",
          generic: "サインインを開始できませんでした。",
          required: "名前とクライアント ID（Google はシークレットも）を入力してください。",
        },
      },
    },
```

and in `wizard`:

```ts
      cloud: {
        accountHeading: "どのクラウドアカウントを使いますか？",
        noAccounts: "接続済みのアカウントはまだありません。",
        connectNew: "新しいアカウントを接続",
        privacy: "バックアップはアップロード前に暗号化されます。病院のデータをクラウドへ送る前に、個人情報保護の承認が取れていることを確認してください。",
        folderHeading: "ドライブ内のフォルダー",
        folderHelp: "HDMS がこのフォルダーを作成し、中にバックアップを保存します。Google ドライブでは HDMS が作成したフォルダーしか見えないため、一覧からの選択はできません。",
        folderName: "フォルダー名",
        invalidFolder: "文字・数字・空白・ドット・ハイフン・アンダースコアが使えます。/ で区切って3階層までです。",
        useThis: "このフォルダーを使う",
        defaultName: "クラウドドライブ",
      },
```

Remove the `comingSoon` line from both files' `wizard.where`.

- [ ] **Step 2: Write the failing tests**

Create `hdms-frontend/apps/admin/src/__tests__/backups-cloud.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { cloudPoll } from "@/components/backups/use-cloud-accounts";
import { baseConfig, renderWithClient } from "./backup-fixtures";

const c = ja.backups.cloud;
const w = ja.backups.wizard;

const connected: apiClient.BackupCloudAccount = {
  id: "acct-1", provider: "google_drive", name: "病院ドライブ", clientId: "cid", tenant: "common",
  status: "connected", accountEmail: "drive-owner@example.test",
};
const signIn: apiClient.BackupCloudSignIn = {
  id: "acct-1", userCode: "ABCD-EFGH", verificationUri: "https://example.test/device", expiresAt: new Date(Date.now() + 600_000).toISOString(),
};
const byText = (text: string) => (_: string, el: Element | null) => el?.textContent?.includes(text) ?? false;

describe("cloud backup", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    cloudPoll.ms = 5;
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ drivesHostPath: "/Volumes" }) } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({ data: { roots: [], folders: [] } } as any);
  });

  it("connects an account with the device code and lists it", async () => {
    let accounts: apiClient.BackupCloudAccount[] = [];
    vi.spyOn(apiClient, "listBackupCloudAccounts").mockImplementation((async () => ({ data: { items: accounts } })) as any);
    const create = vi.spyOn(apiClient, "createBackupCloudAccount").mockResolvedValue({ data: signIn } as any);
    const polls = [{ ...connected, status: "pending" as const }, connected];
    vi.spyOn(apiClient, "getBackupCloudAccount").mockImplementation((async () => {
      const next = polls.length > 1 ? polls.shift()! : polls[0];
      if (next.status === "connected") accounts = [connected];
      return { data: next };
    }) as any);

    const user = userEvent.setup();
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: c.add }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(c.connect.name), "病院ドライブ");
    await user.type(within(dialog).getByLabelText(c.connect.clientId), "cid");
    await user.type(within(dialog).getByLabelText(c.connect.clientSecret), "s3cret");
    await user.click(within(dialog).getByRole("button", { name: c.connect.start }));

    expect(await within(dialog).findByText("ABCD-EFGH")).toBeInTheDocument();
    expect(create).toHaveBeenCalledWith({
      body: { provider: "google_drive", name: "病院ドライブ", clientId: "cid", clientSecret: "s3cret" },
    });
    expect(await within(dialog).findByText(byText("drive-owner@example.test"), undefined, { timeout: 3000 })).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: c.connect.done }));
    expect(await screen.findByText("病院ドライブ")).toBeInTheDocument();
  });

  it("walks the wizard from a connected account to a saved cloud destination", async () => {
    vi.spyOn(apiClient, "listBackupCloudAccounts").mockResolvedValue({ data: { items: [connected] } } as any);
    const create = vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({
      data: { id: "d-1", name: w.cloud.defaultName, target: "cloud:acct-1/hdms-backups", provider: "google_drive", enabled: true, retentionVersions: 3 },
    } as any);
    const test = vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({ data: { id: "req-1", kind: "test", status: "pending", requestedAt: new Date().toISOString() } } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "req-1", kind: "test", status: "done", outcome: "success", requestedAt: new Date().toISOString() },
    } as any);

    const user = userEvent.setup();
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
    const dialog = await screen.findByRole("dialog");
    await user.click(await within(dialog).findByRole("button", { name: byText(w.where.cloud) }));
    await user.click(await within(dialog).findByRole("button", { name: byText("病院ドライブ") }));
    const folder = await within(dialog).findByLabelText(w.cloud.folderName);
    await user.clear(folder);
    await user.type(folder, "a:b");
    expect(within(dialog).getByText(w.cloud.invalidFolder)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: w.cloud.useThis })).toBeDisabled();
    await user.clear(folder);
    await user.type(folder, "hdms-backups");
    await user.click(within(dialog).getByRole("button", { name: w.cloud.useThis }));
    await user.click(await within(dialog).findByRole("button", { name: w.details.save }));

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(create).toHaveBeenCalledWith({
      body: { name: w.cloud.defaultName, cloudAccountId: "acct-1", folder: "hdms-backups", retentionVersions: 3, enabled: true },
    });
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: { id: "d-1" } }));
    expect(await within(dialog).findByText(w.saving.ready)).toBeInTheDocument();
  });

  it("explains why an account in use cannot be disconnected", async () => {
    vi.spyOn(apiClient, "listBackupCloudAccounts").mockResolvedValue({ data: { items: [connected] } } as any);
    vi.spyOn(apiClient, "deleteBackupCloudAccount").mockResolvedValue({
      error: { type: "https://hdms.local/problems/cloud-account-in-use", status: 409, title: "in use" },
    } as any);
    const user = userEvent.setup();
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: c.disconnect }));
    const confirm = await screen.findByRole("alertdialog");
    await user.click(within(confirm).getByRole("button", { name: c.disconnect }));
    expect(await screen.findByText(c.inUse)).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run it to see it fail**

Run: `cd hdms-frontend && pnpm --filter admin test -- backups-cloud`
Expected: FAIL — `@/components/backups/use-cloud-accounts` not found.

- [ ] **Step 4: Write the hook and helpers**

Create `hdms-frontend/apps/admin/src/components/backups/use-cloud-accounts.ts`:

```ts
import { listBackupCloudAccounts } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";

export const CLOUD_ACCOUNTS_KEY = ["backup", "cloud-accounts"] as const;

/**
 * Milliseconds between sign-in checks while the code is on screen. A test
 * seam; the server already refuses to ask the provider more often than the
 * provider allows, so a short interval is harmless.
 */
export const cloudPoll = { ms: 5000 };

export function useCloudAccounts() {
  return useQuery({
    queryKey: CLOUD_ACCOUNTS_KEY,
    queryFn: async () => {
      const res = await listBackupCloudAccounts();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
}
```

Create `hdms-frontend/apps/admin/src/components/backups/cloud-format.ts`:

```ts
import type { BackupCloudAccount, BackupConfig, BackupDestination } from "@hdms/api-client";
import { realLocation } from "./real-location";

const SEGMENT = /^[\p{L}\p{N}_-][\p{L}\p{N} ._-]*$/u;

/** Mirrors the server's CleanCloudFolder; the server stays the authority. */
export function isCloudFolder(raw: string): boolean {
  const s = raw.trim().replace(/^\/+|\/+$/g, "");
  if (s.length === 0 || s.length > 200) return false;
  const parts = s.split("/");
  return parts.length <= 3 && parts.every((p) => SEGMENT.test(p) && p === p.trim());
}

/** Where a destination keeps its copy, in words an administrator recognises. */
export function destinationLocation(
  d: BackupDestination,
  config: Pick<BackupConfig, "drivesDir" | "drivesHostPath"> | undefined,
  providerLabel: (provider: string) => string,
): string {
  if (d.cloudAccountId && d.folder) return `${providerLabel(d.provider)} · ${d.folder}`;
  return realLocation(d.target, config);
}

export function accountLabel(a: BackupCloudAccount, providerLabel: (provider: string) => string): string {
  return a.accountEmail ? `${providerLabel(a.provider)} · ${a.accountEmail}` : providerLabel(a.provider);
}
```

- [ ] **Step 5: Write the connect dialog**

Create `hdms-frontend/apps/admin/src/components/backups/cloud-connect-dialog.tsx`:

```tsx
import {
  createBackupCloudAccount,
  getBackupCloudAccount,
  reconnectBackupCloudAccount,
  type BackupCloudAccount,
  type BackupCloudProvider,
  type BackupCloudSignIn,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { CLOUD_ACCOUNTS_KEY, cloudPoll } from "./use-cloud-accounts";

type Problem = { type?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

const PROVIDERS: BackupCloudProvider[] = ["google_drive", "onedrive"];

/**
 * Connects a new account (no `account`), or restarts the sign-in of an
 * expired or revoked one (`account` given). onConnected receives the account
 * once the person presses Done.
 */
export function CloudConnectDialog({
  account,
  onClose,
  onConnected,
}: {
  account?: BackupCloudAccount;
  onClose: () => void;
  onConnected: (account: BackupCloudAccount) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [signIn, setSignIn] = useState<BackupCloudSignIn | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fail = (err: unknown) => {
    if (problemIs(err, "cloud-provider-rejected")) setError(t("backups.cloud.connect.errors.rejected"));
    else if (problemIs(err, "cloud-provider-unreachable")) setError(t("backups.cloud.connect.errors.unreachable"));
    else if (problemIs(err, "cloud-unavailable")) setError(t("backups.cloud.connect.errors.unavailable"));
    else setError(t("backups.cloud.connect.errors.generic"));
  };
  const started = (s: BackupCloudSignIn) => {
    setError(null);
    setSignIn(s);
    void queryClient.invalidateQueries({ queryKey: CLOUD_ACCOUNTS_KEY });
  };

  const create = useMutation({
    mutationFn: async (body: { provider: BackupCloudProvider; name: string; clientId: string; clientSecret?: string; tenant?: string }) => {
      const res = await createBackupCloudAccount({ body });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: started,
    onError: fail,
  });
  const reconnect = useMutation({
    mutationFn: async (id: string) => {
      const res = await reconnectBackupCloudAccount({ path: { id } });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: started,
    onError: fail,
  });

  const reconnected = useRef(false);
  useEffect(() => {
    if (account && !reconnected.current) {
      reconnected.current = true;
      reconnect.mutate(account.id);
    }
  }, [account, reconnect]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("backups.cloud.connect.title")}</DialogTitle>
        </DialogHeader>
        {signIn ? (
          <CodePhase
            signIn={signIn}
            onDone={onConnected}
            onRestart={() => reconnect.mutate(signIn.id)}
            onClose={onClose}
          />
        ) : account ? (
          <>
            {reconnect.isPending && <Loader2 className="size-4 animate-spin" />}
            {error && <p className="text-sm text-destructive">{error}</p>}
            <DialogFooter>
              <Button variant="outline" onClick={onClose}>{t("backups.cloud.connect.close")}</Button>
            </DialogFooter>
          </>
        ) : (
          <ConnectForm busy={create.isPending} error={error} onCancel={onClose} onSubmit={(b) => create.mutate(b)} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ConnectForm({
  busy,
  error,
  onCancel,
  onSubmit,
}: {
  busy: boolean;
  error: string | null;
  onCancel: () => void;
  onSubmit: (body: { provider: BackupCloudProvider; name: string; clientId: string; clientSecret?: string; tenant?: string }) => void;
}) {
  const t = useT();
  const [provider, setProvider] = useState<BackupCloudProvider>("google_drive");
  const [name, setName] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [tenant, setTenant] = useState("common"); // i18n-allow-literal: the directory ID Microsoft treats as "any", not prose
  const [local, setLocal] = useState<string | null>(null);
  const google = provider === "google_drive";

  const submit = () => {
    if (!name.trim() || !clientId.trim() || (google && !clientSecret.trim())) {
      setLocal(t("backups.cloud.connect.errors.required"));
      return;
    }
    setLocal(null);
    onSubmit(
      google
        ? { provider, name: name.trim(), clientId: clientId.trim(), clientSecret: clientSecret.trim() }
        : { provider, name: name.trim(), clientId: clientId.trim(), tenant: tenant.trim() || undefined },
    );
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label>{t("backups.cloud.connect.provider")}</Label>
          <div className="flex gap-2">
            {PROVIDERS.map((p) => (
              <Button key={p} type="button" variant={provider === p ? "default" : "outline"} aria-pressed={provider === p} onClick={() => setProvider(p)}>
                {t(`backups.cloud.providers.${p}` as never)}
              </Button>
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="cloud-name">{t("backups.cloud.connect.name")}</Label>
          <Input id="cloud-name" value={name} onChange={(e) => setName(e.target.value)} />
          <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.nameHelp")}</p>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="cloud-client-id">{t("backups.cloud.connect.clientId")}</Label>
          <Input id="cloud-client-id" value={clientId} autoComplete="off" onChange={(e) => setClientId(e.target.value)} />
        </div>
        {google ? (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="cloud-client-secret">{t("backups.cloud.connect.clientSecret")}</Label>
            <Input id="cloud-client-secret" type="password" autoComplete="off" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.clientSecretHelp")}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="cloud-tenant">{t("backups.cloud.connect.tenant")}</Label>
            <Input id="cloud-tenant" value={tenant} onChange={(e) => setTenant(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.tenantHelp")}</p>
          </div>
        )}
        <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.help")}</p>
        {(local ?? error) && <p className="text-sm text-destructive">{local ?? error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("backups.cloud.connect.cancel")}</Button>
        <Button onClick={submit} disabled={busy}>{t("backups.cloud.connect.start")}</Button>
      </DialogFooter>
    </>
  );
}

function CodePhase({
  signIn,
  onDone,
  onRestart,
  onClose,
}: {
  signIn: BackupCloudSignIn;
  onDone: (account: BackupCloudAccount) => void;
  onRestart: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  // Asking is a side-effecting GET: the server contacts the provider when due.
  const poll = useQuery({
    queryKey: [...CLOUD_ACCOUNTS_KEY, "poll", signIn.id, signIn.expiresAt],
    queryFn: async () => {
      const res = await getBackupCloudAccount({ path: { id: signIn.id } });
      if (res.error) throw res.error;
      return res.data!;
    },
    refetchInterval: (q) => (q.state.data && q.state.data.status !== "pending" ? false : cloudPoll.ms),
    refetchOnWindowFocus: false,
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
  const account = poll.data;
  const status = account?.status ?? "pending";

  useEffect(() => {
    if (status === "connected") void queryClient.invalidateQueries({ queryKey: CLOUD_ACCOUNTS_KEY });
  }, [status, queryClient]);

  if (account && status === "connected") {
    return (
      <>
        <p className="text-sm">
          {account.accountEmail
            ? t("backups.cloud.connect.connectedAs", { email: account.accountEmail })
            : t("backups.cloud.connect.connected")}
        </p>
        <DialogFooter>
          <Button onClick={() => onDone(account)}>{t("backups.cloud.connect.done")}</Button>
        </DialogFooter>
      </>
    );
  }
  if (account && (status === "expired" || status === "revoked")) {
    return (
      <>
        <p className="text-sm text-destructive">{account.lastError ?? t(`backups.cloud.status.${status}` as never)}</p>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("backups.cloud.connect.close")}</Button>
          <Button onClick={onRestart}>{t("backups.cloud.connect.retry")}</Button>
        </DialogFooter>
      </>
    );
  }
  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.cloud.connect.codeHeading")}</h3>
        <p className="text-sm">
          {t("backups.cloud.connect.codeSteps", { uri: signIn.verificationUri })}
        </p>
        <a className="text-sm underline" href={signIn.verificationUri} target="_blank" rel="noreferrer">
          {signIn.verificationUri}
        </a>
        <p className="font-identifier text-2xl tracking-widest">{signIn.userCode}</p>
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
          {t("backups.cloud.connect.waiting")}
        </p>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>{t("backups.cloud.connect.close")}</Button>
      </DialogFooter>
    </>
  );
}
```

- [ ] **Step 6: Write the accounts section**

Create `hdms-frontend/apps/admin/src/components/backups/cloud-accounts-section.tsx`:

```tsx
import { deleteBackupCloudAccount, type BackupCloudAccount } from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { accountLabel } from "./cloud-format";
import { CloudConnectDialog } from "./cloud-connect-dialog";
import { CLOUD_ACCOUNTS_KEY, useCloudAccounts } from "./use-cloud-accounts";

type Problem = { type?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

export function CloudAccountsSection() {
  const t = useT();
  const queryClient = useQueryClient();
  const accounts = useCloudAccounts();
  const [connecting, setConnecting] = useState<BackupCloudAccount | "new" | null>(null);
  const [removing, setRemoving] = useState<BackupCloudAccount | null>(null);
  const [error, setError] = useState<string | null>(null);
  const providerLabel = (p: string) => t(`backups.cloud.providers.${p}` as never);

  const remove = useMutation({
    mutationFn: async (id: string) => {
      const res = await deleteBackupCloudAccount({ path: { id } });
      if (res.error) throw res.error;
    },
    onSuccess: () => {
      setError(null);
      setRemoving(null);
      toast.success(t("backups.cloud.disconnected"));
      void queryClient.invalidateQueries({ queryKey: CLOUD_ACCOUNTS_KEY });
    },
    onError: (err: unknown) => {
      setRemoving(null);
      setError(problemIs(err, "cloud-account-in-use") ? t("backups.cloud.inUse") : t("backups.errors.save"));
    },
  });

  const items = accounts.data ?? [];
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-sm font-medium">{t("backups.cloud.title")}</h3>
          <p className="text-xs text-muted-foreground">{t("backups.cloud.description")}</p>
        </div>
        <Button variant="outline" onClick={() => setConnecting("new")}>{t("backups.cloud.add")}</Button>
      </div>
      {accounts.isSuccess && items.length === 0 && <p className="text-sm text-muted-foreground">{t("backups.cloud.empty")}</p>}
      <ul className="flex flex-col gap-2">
        {items.map((a) => (
          <li key={a.id} className="flex items-center gap-3 rounded-md border p-3">
            <span className="flex flex-1 flex-col">
              <span className="font-medium">{a.name}</span>
              <span className="text-xs text-muted-foreground">{accountLabel(a, providerLabel)}</span>
              {a.lastError && a.status !== "connected" && <span className="text-xs text-destructive">{a.lastError}</span>}
            </span>
            <Badge variant={a.status === "connected" ? "default" : "outline"}>{t(`backups.cloud.status.${a.status}` as never)}</Badge>
            {(a.status === "expired" || a.status === "revoked") && (
              <Button size="sm" variant="outline" onClick={() => setConnecting(a)}>{t("backups.cloud.reconnect")}</Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => setRemoving(a)}>{t("backups.cloud.disconnect")}</Button>
          </li>
        ))}
      </ul>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {connecting && (
        <CloudConnectDialog
          account={connecting === "new" ? undefined : connecting}
          onClose={() => setConnecting(null)}
          onConnected={() => setConnecting(null)}
        />
      )}
      <AlertDialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("backups.cloud.disconnectTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("backups.cloud.disconnectBody", { name: removing?.name ?? "" })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("backups.destinations.form.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => removing && remove.mutate(removing.id)}>{t("backups.cloud.disconnect")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
```

- [ ] **Step 7: Show cloud destinations in the table, mount the section**

In `hdms-frontend/apps/admin/src/components/backups/destinations-tab.tsx`:

1. Add imports: `import { CloudAccountsSection } from "./cloud-accounts-section";` and `import { destinationLocation } from "./cloud-format";`.
2. Replace the target column's cell (the line with `realLocation(row.original.target, config)`) with:

```tsx
        cell: ({ row }) => (
          <span className="font-identifier text-xs">
            {destinationLocation(row.original, config, (p) => t(`backups.cloud.providers.${p}` as never))}
          </span>
        ),
```

3. In `EditDestinationDialog`, replace `value={realLocation(destination.target, config)}` with `value={destinationLocation(destination, config, (p) => t(`backups.cloud.providers.${p}` as never))}`.
4. Remove the now-unused `realLocation` import if the compiler flags it.
5. Directly above `{adding && <DestinationWizard onClose={() => setAdding(false)} />}` add `<CloudAccountsSection />`.

- [ ] **Step 8: The wizard's cloud path**

In `hdms-frontend/apps/admin/src/components/backups/destination-wizard.tsx`:

1. Imports: add `type BackupCloudAccount` to the `@hdms/api-client` import list; add

```tsx
import { CloudConnectDialog } from "./cloud-connect-dialog";
import { accountLabel, isCloudFolder } from "./cloud-format";
import { useCloudAccounts } from "./use-cloud-accounts";
```

2. Replace the `Step`/`STEPS` declarations with:

```tsx
type Step = "where" | "account" | "cloudFolder" | "folder" | "check" | "details" | "saving";
const DRIVE_STEPS: Step[] = ["where", "folder", "check", "details", "saving"];
const CLOUD_STEPS: Step[] = ["where", "account", "cloudFolder", "details", "saving"];

/** What the destination will be created with: a folder on a drive, or a folder in a cloud account. */
type DestinationTarget = { path: string } | { cloudAccountId: string; folder: string };
```

3. Replace the whole `DestinationWizard` function with:

```tsx
export function DestinationWizard({ onClose }: { onClose: () => void }) {
  const t = useT();
  const [step, setStep] = useState<Step>("where");
  const [cloud, setCloud] = useState(false);
  const [root, setRoot] = useState<BackupLocationRoot | null>(null);
  const [path, setPath] = useState("");
  const [account, setAccount] = useState<BackupCloudAccount | null>(null);
  const [folder, setFolder] = useState("");
  const [saved, setSaved] = useState<{ id: string; name: string; retentionVersions: number; requestId: string } | null>(null);
  const steps = cloud ? CLOUD_STEPS : DRIVE_STEPS;
  const target: DestinationTarget = cloud && account ? { cloudAccountId: account.id, folder } : { path };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("backups.wizard.title")}</DialogTitle>
          <p className="text-xs text-muted-foreground">
            {t("backups.wizard.stepOf", { current: String(steps.indexOf(step) + 1), total: String(steps.length) })}
          </p>
        </DialogHeader>
        {step === "where" && (
          <WhereStep
            onCancel={onClose}
            onPickCloud={() => {
              setCloud(true);
              setStep("account");
            }}
            onPick={(r) => {
              setCloud(false);
              setRoot(r);
              setPath(r.path);
              setStep("folder");
            }}
          />
        )}
        {step === "account" && (
          <AccountStep
            onBack={() => setStep("where")}
            onPick={(a) => {
              setAccount(a);
              setStep("cloudFolder");
            }}
          />
        )}
        {step === "cloudFolder" && account && (
          <CloudFolderStep
            account={account}
            onBack={() => setStep("account")}
            onUse={(f) => {
              setFolder(f);
              setStep("details");
            }}
          />
        )}
        {step === "folder" && root && (
          <FolderStep path={path} onNavigate={setPath} onBack={() => setStep("where")} onUse={() => setStep("check")} />
        )}
        {step === "check" && <CheckStep path={path} onBack={() => setStep("folder")} onNext={() => setStep("details")} />}
        {step === "details" && (
          <DetailsStep
            target={target}
            defaultName={cloud ? t("backups.wizard.cloud.defaultName") : t("backups.wizard.details.defaultName")}
            onBack={() => setStep(cloud ? "cloudFolder" : "check")}
            onSaved={(s) => {
              setSaved(s);
              setStep("saving");
            }}
          />
        )}
        {step === "saving" && saved && <SavingStep saved={saved} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}
```

4. In `WhereStep`: change the props to `{ onPick, onPickCloud, onCancel }: { onPick: (root: BackupLocationRoot) => void; onPickCloud: () => void; onCancel: () => void }` and replace the disabled cloud button (the `<Button variant="outline" className="h-auto justify-start gap-3 py-3" disabled>…</Button>` with the `comingSoon` badge) with:

```tsx
        <Button variant="outline" className="h-auto justify-start gap-3 py-3" onClick={onPickCloud}>
          <Cloud className="size-5 shrink-0" />
          <span className="flex-1 text-left">{t("backups.wizard.where.cloud")}</span>
        </Button>
```

5. In `DetailsStep`: change the signature to `{ target, defaultName, onBack, onSaved }: { target: DestinationTarget; defaultName: string; onBack: () => void; onSaved: … }`, initialise `const [name, setName] = useState(defaultName);` (replacing the `t("backups.wizard.details.defaultName")` initialiser), and change the create call's body to

```tsx
        body: { name: name.trim(), ...target, retentionVersions: retentionNumber, enabled: true },
```

(`target` is `{ path }` for a drive, so for drives map it: replace the spread with `...("path" in target ? { target: target.path } : target)`.) The final line reads:

```tsx
        body: { name: name.trim(), ...("path" in target ? { target: target.path } : target), retentionVersions: retentionNumber, enabled: true },
```

6. Append these two components to the end of the file:

```tsx
function AccountStep({ onBack, onPick }: { onBack: () => void; onPick: (account: BackupCloudAccount) => void }) {
  const t = useT();
  const accounts = useCloudAccounts();
  const [connecting, setConnecting] = useState(false);
  const usable = (accounts.data ?? []).filter((a) => a.status === "connected");
  const providerLabel = (p: string) => t(`backups.cloud.providers.${p}` as never);

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.cloud.accountHeading")}</h3>
        <p className="text-xs text-muted-foreground">{t("backups.wizard.cloud.privacy")}</p>
        {accounts.isPending && <Loader2 className="size-4 animate-spin" />}
        {accounts.isSuccess && usable.length === 0 && <p className="text-sm text-muted-foreground">{t("backups.wizard.cloud.noAccounts")}</p>}
        {usable.map((a) => (
          <Button key={a.id} variant="outline" className="h-auto justify-start gap-3 py-3 text-left" onClick={() => onPick(a)}>
            <Cloud className="size-5 shrink-0" />
            <span className="flex flex-1 flex-col">
              <span className="font-medium">{a.name}</span>
              <span className="text-xs text-muted-foreground">{accountLabel(a, providerLabel)}</span>
            </span>
          </Button>
        ))}
        <Button variant="outline" onClick={() => setConnecting(true)}>{t("backups.wizard.cloud.connectNew")}</Button>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
      </DialogFooter>
      {connecting && (
        <CloudConnectDialog
          onClose={() => setConnecting(false)}
          onConnected={(a) => {
            setConnecting(false);
            onPick(a);
          }}
        />
      )}
    </>
  );
}

function CloudFolderStep({ account, onBack, onUse }: { account: BackupCloudAccount; onBack: () => void; onUse: (folder: string) => void }) {
  const t = useT();
  const [folder, setFolder] = useState("hdms-backups"); // i18n-allow-literal: suggested folder name, not prose
  const valid = isCloudFolder(folder);

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.cloud.folderHeading")}</h3>
        <p className="text-xs text-muted-foreground">{account.name}</p>
        <p className="text-xs text-muted-foreground">{t("backups.wizard.cloud.folderHelp")}</p>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="wizard-cloud-folder">{t("backups.wizard.cloud.folderName")}</Label>
          <Input id="wizard-cloud-folder" value={folder} aria-invalid={!valid} onChange={(e) => setFolder(e.target.value)} />
          {!valid && <p className="text-sm text-destructive">{t("backups.wizard.cloud.invalidFolder")}</p>}
        </div>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button onClick={() => onUse(folder.trim().replace(/^\/+|\/+$/g, ""))} disabled={!valid}>{t("backups.wizard.cloud.useThis")}</Button>
      </DialogFooter>
    </>
  );
}
```

- [ ] **Step 9: Run the new tests, then the whole admin suite**

Run: `cd hdms-frontend && pnpm --filter admin test -- backups-cloud` then `pnpm --filter admin test` then `pnpm --filter admin lint && pnpm --filter admin build`
Expected: PASS everywhere, including `no-literals.test.ts` and the existing `backups-wizard.test.tsx` (the drive path still passes `target`). If the existing wizard test asserted the "coming soon" badge, replace that assertion with a check that the cloud button is enabled.

- [ ] **Step 10: Mutation check and commit**

In `isCloudFolder`, change `parts.length <= 3` to `true` and run the wizard test: expect the "a:b" case still blocked (the regex) but add a quick manual check that `a/b/c/d` is accepted — then revert; the Go side remains the authority, covered in Task 2.

```bash
git add hdms-frontend/apps/admin
git commit -m "feat(admin): connect Google Drive and OneDrive and add cloud backup destinations"
```

---

### Task 7: Disaster recovery from the cloud — `hdms-cli cloud fetch` and `install.sh --restore --cloud=…`

**Files:**
- Create: `hdms-backend/cmd/hdms-cli/cloud.go`
- Modify: `hdms-backend/cmd/hdms-cli/main.go`
- Test: `hdms-backend/cmd/hdms-cli/cloud_test.go`
- Modify: `deploy/production/install.sh`, `deploy/production/install_test.sh`

**Interfaces:**
- Consumes: Task 1 (`DeviceFlow`), Task 3 (`Rclone`, `RenderRcloneConfig`, `CloudRemote`, `CloudCreds`), existing `backup.IsRecoverySource`, `backup.CleanCloudFolder`.
- Produces: `hdms-cli cloud fetch --provider google_drive|onedrive --client-id <id> [--tenant <id>] --folder <name> --to <dir>` (client secret on the first stdin line; empty for OneDrive); `install.sh --restore --cloud=google|onedrive`.

- [ ] **Step 1: Write the failing CLI test**

Create `hdms-backend/cmd/hdms-cli/cloud_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/cloudfake"
)

// fakeRcloneCopy plays `rclone copy remote:folder dst`: it checks the config
// it was given and lays down what a backup folder looks like.
func fakeRcloneCopy(t *testing.T, seen *[]string) backup.ExecFunc {
	return func(_ context.Context, _ string, args, env []string, _ io.Reader, _ io.Writer) error {
		*seen = append(*seen, strings.Join(args, " "))
		var conf string
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "RCLONE_CONFIG="); ok {
				conf = v
			}
		}
		raw, err := os.ReadFile(conf)
		if err != nil {
			t.Errorf("rclone ran without a readable config: %v", err)
			return err
		}
		if !strings.Contains(string(raw), "client_secret = gsec") || !strings.Contains(string(raw), `"access_token":"at-1"`) {
			t.Errorf("config = %s", raw)
		}
		dst := args[len(args)-1]
		_ = os.MkdirAll(filepath.Join(dst, "repo", "locks"), 0o750)
		_ = os.WriteFile(filepath.Join(dst, "repo", "config"), nil, 0o600)
		return os.WriteFile(filepath.Join(dst, backup.RecoveryBundleFile), []byte("b"), 0o600)
	}
}

func TestCloudFetchDownloadsTheFolderAfterSignIn(t *testing.T) {
	p := cloudfake.New(t)
	p.Answer("approve")
	var calls []string
	var out bytes.Buffer
	to := t.TempDir()
	c := cloudFetch{
		Provider: backup.ProviderGoogleDrive, ClientID: "cid", ClientSecret: "gsec", Folder: "hdms-backups", To: to,
		Flow: p.Flow(), Rclone: backup.Rclone{Exec: fakeRcloneCopy(t, &calls)}, Out: &out,
		Wait: func(context.Context, time.Duration) error { return nil },
	}
	if err := c.run(context.Background()); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ABCD-EFGH") || !strings.Contains(out.String(), "https://example.test/device") {
		t.Fatalf("the person was not told the code:\n%s", out.String())
	}
	if strings.Contains(out.String(), "gsec") || strings.Contains(out.String(), "at-1") {
		t.Fatalf("a secret reached the terminal:\n%s", out.String())
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "copy acct_") || !strings.HasSuffix(calls[0], ":hdms-backups "+filepath.Join(to, "hdms-backups")) {
		t.Fatalf("rclone calls = %v", calls)
	}
	if !backup.IsRecoverySource(filepath.Join(to, "hdms-backups")) {
		t.Fatal("the downloaded folder is not a recovery source")
	}
}

func TestCloudFetchKeepsWaitingThroughPendingAndStopsWhenDeclined(t *testing.T) {
	p := cloudfake.New(t)
	waits := 0
	c := cloudFetch{
		Provider: backup.ProviderGoogleDrive, ClientID: "cid", ClientSecret: "gsec", Folder: "f", To: t.TempDir(),
		Flow: p.Flow(), Rclone: backup.Rclone{}, Out: io.Discard,
		Wait: func(context.Context, time.Duration) error {
			waits++
			if waits == 3 {
				p.Answer("denied")
			}
			return nil
		},
	}
	err := c.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "declined") || waits != 3 {
		t.Fatalf("err = %v after %d waits", err, waits)
	}
}

func TestCloudFetchRefusesAFolderWithoutBackups(t *testing.T) {
	p := cloudfake.New(t)
	p.Answer("approve")
	c := cloudFetch{
		Provider: backup.ProviderGoogleDrive, ClientID: "cid", ClientSecret: "gsec", Folder: "empty", To: t.TempDir(),
		Flow: p.Flow(), Out: io.Discard, Wait: func(context.Context, time.Duration) error { return nil },
		Rclone: backup.Rclone{Exec: func(context.Context, string, []string, []string, io.Reader, io.Writer) error { return nil }},
	}
	if err := c.run(context.Background()); err == nil || !strings.Contains(err.Error(), "no HDMS backups") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run CloudFetch`
Expected: FAIL — `cloudFetch` undefined.

- [ ] **Step 3: Write the command**

Create `hdms-backend/cmd/hdms-cli/cloud.go`:

```go
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// runCloud handles `hdms-cli cloud fetch`. install.sh runs it on a new server
// before any env file exists, so it needs no configuration and no database:
// it signs in on the terminal and copies the backup folder out of the cloud.
func runCloud(ctx context.Context, args []string, stdin io.Reader, stderr io.Writer) error {
	const usage = "usage: hdms-cli cloud fetch --provider google_drive|onedrive --client-id <id> [--tenant <id>] --folder <name> --to <dir>  (client secret on the first line of stdin; empty for OneDrive)"
	if len(args) == 0 || args[0] != "fetch" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("cloud fetch", flag.ContinueOnError)
	provider := fs.String("provider", "", "google_drive or onedrive")
	clientID := fs.String("client-id", "", "OAuth client ID")
	tenant := fs.String("tenant", "", "OneDrive directory (tenant) ID")
	folder := fs.String("folder", "", "folder in the cloud drive that holds the HDMS backups")
	to := fs.String("to", "", "local directory to download into")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *provider == "" || *clientID == "" || *folder == "" || *to == "" {
		return errors.New(usage)
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read client secret: %w", err)
	}
	c := cloudFetch{
		Provider: *provider, ClientID: *clientID, ClientSecret: strings.TrimSpace(line), Tenant: *tenant,
		Folder: *folder, To: *to,
		Flow: &backup.DeviceFlow{HTTP: &http.Client{Timeout: 30 * time.Second}},
		Out:  stderr,
		Wait: func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		},
	}
	return c.run(ctx)
}

type cloudFetch struct {
	Provider, ClientID, ClientSecret, Tenant, Folder, To string
	Flow                                                  *backup.DeviceFlow
	Rclone                                                backup.Rclone
	Out                                                   io.Writer
	Wait                                                  func(ctx context.Context, d time.Duration) error
}

func (c cloudFetch) run(ctx context.Context) error {
	folder, err := backup.CleanCloudFolder(c.Folder)
	if err != nil {
		return err
	}
	tenant := c.Tenant
	if c.Provider == backup.ProviderGoogleDrive {
		tenant = "common"
	} else if tenant == "" {
		tenant = "common"
	}
	dc, err := c.Flow.Start(ctx, c.Provider, c.ClientID, tenant)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "\nOn any device, open %s and enter the code %s\nWaiting for you to sign in...\n", dc.VerificationURI, dc.UserCode)

	tok, err := c.waitForToken(ctx, dc, tenant)
	if err != nil {
		return err
	}
	prof, err := c.Flow.Profile(ctx, c.Provider, tenant, tok.AccessToken)
	if err != nil && c.Provider == backup.ProviderOneDrive {
		return fmt.Errorf("signed in, but could not read the OneDrive details: %w", err)
	}
	js, err := tok.RcloneJSON()
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "hdms-cloud-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	id := uuid.New()
	conf := filepath.Join(dir, "rclone.conf")
	rendered := backup.RenderRcloneConfig([]backup.CloudCreds{{
		ID: id, Provider: c.Provider, ClientID: c.ClientID, ClientSecret: c.ClientSecret, Tenant: tenant,
		Token: js, DriveID: prof.DriveID, DriveType: prof.DriveType,
	}})
	if err := os.WriteFile(conf, []byte(rendered), 0o600); err != nil {
		return err
	}

	dst := filepath.Join(c.To, path.Base(folder))
	fmt.Fprintf(c.Out, "Signed in. Downloading %q (this can take a while)...\n", folder)
	rc := c.Rclone
	rc.Config = conf
	if err := rc.Copy(ctx, backup.CloudRemote(id)+":"+folder, dst); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if !backup.IsRecoverySource(dst) {
		return fmt.Errorf("no HDMS backups found in %q (looked for repo and %s); check the folder name and that this is the account HDMS backed up to", folder, backup.RecoveryBundleFile)
	}
	fmt.Fprintf(c.Out, "Downloaded to %s\n", dst)
	return nil
}

func (c cloudFetch) waitForToken(ctx context.Context, dc backup.DeviceCode, tenant string) (backup.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, dc.ExpiresIn)
	defer cancel()
	interval := dc.Interval
	for {
		if err := c.Wait(ctx, interval); err != nil {
			return backup.Token{}, errors.New("the sign-in code expired before you finished; run install.sh --restore again")
		}
		tok, err := c.Flow.Exchange(ctx, c.Provider, c.ClientID, c.ClientSecret, tenant, dc.DeviceCode)
		switch {
		case err == nil:
			return tok, nil
		case errors.Is(err, backup.ErrAuthorizationPending):
		case errors.Is(err, backup.ErrSlowDown):
			interval += 5 * time.Second
		case errors.Is(err, backup.ErrCodeExpired):
			return backup.Token{}, errors.New("the sign-in code expired; run install.sh --restore again")
		case errors.Is(err, backup.ErrAccessDenied):
			return backup.Token{}, errors.New("sign-in was declined")
		default:
			return backup.Token{}, err
		}
	}
}
```

In `hdms-backend/cmd/hdms-cli/main.go`, in `run`, next to the `recovery` shortcut add:

```go
	if cmd == "cloud" {
		return runCloud(context.Background(), args, os.Stdin, os.Stderr)
	}
```

and add `cloud fetch|` to the usage string (before `reconcile|`).

- [ ] **Step 4: Run the CLI tests**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run 'CloudFetch|Recovery' -v`
Expected: PASS.

- [ ] **Step 5: Teach `install.sh` the `--cloud` option**

In `deploy/production/install.sh`:

1. Update the usage text — replace the `--restore` entry with:

```
  --restore    New server for an existing HDMS: read the secrets out of the
               backups with the recovery key, write /etc/hdms/hdms.env, start
               HDMS, then finish in the browser at https://<host>/recovery.
  --cloud=google|onedrive
               With --restore: the backups are in Google Drive or OneDrive.
               Signs in on this terminal and downloads them first.
```

and the first usage line to `Usage: install.sh [--restore [--cloud=google|onedrive]] [--force]`. Add `#   sudo deploy/production/install.sh --restore --cloud=google   ... from Google Drive` under the header comment's list.

2. Next to the other globals add `drives_real="" cloud=""` (e.g. extend the `source_dir="" service_uid="" service_gid=""` line).

3. Replace `choose_source` (the function from `choose_source() {` through its closing `}`) with these four functions:

```bash
# resolve_drives sets drives_real: the drives folder with symlinks resolved,
# which is where the worker sees everything under /drives.
resolve_drives() {
	[ -d "$drives_host_path" ] || die "the drives folder $drives_host_path does not exist on this server; create it or set HDMS_BACKUP_DRIVES_HOST_PATH"
	drives_real=$(cd "$drives_host_path" && pwd -P)
}

# select_source LIST sets source_dir to the one folder in LIST (one per line),
# asking when there are several.
select_source() {
	local list=$1 count choice
	count=$(printf '%s\n' "$list" | wc -l | tr -d ' ')
	if [ "$count" = 1 ]; then
		source_dir=$list
	else
		echo "Found $count backup folders:"
		printf '%s\n' "$list" | awk '{ printf "  %d) %s\n", NR, $0 }'
		while :; do
			ask choice "Which one (1-$count)"
			if [[ $choice =~ ^[0-9]+$ ]] && [ "$choice" -ge 1 ] && [ "$choice" -le "$count" ]; then
				break
			fi
			say "  Enter a number from 1 to $count."
		done
		source_dir=$(printf '%s\n' "$list" | sed -n "${choice}p")
	fi
	echo "Using the backups in $source_dir"
}

choose_source() {
	local folder list
	resolve_drives
	echo
	echo "Where are the backups? Mount the network drive or external disk under"
	echo "$drives_real on this server first, or copy the backup folder there."
	while :; do
		ask folder "Folder holding the HDMS backups"
		if [ ! -d "$folder" ]; then
			say "  $folder is not a folder on this server."
			continue
		fi
		folder=$(cd "$folder" && pwd -P)
		case $folder/ in
		"$drives_real"/*/*) ;;
		*)
			say "  $folder is not inside $drives_real. Mount or copy the backups under $drives_real, then try again."
			continue
			;;
		esac
		list=$(find_sources "$folder")
		if [ -z "$list" ]; then
			say "  No HDMS backups found in $folder or two folders below it (looked for repo/config next to hdms-recovery.bin)."
			continue
		fi
		select_source "$list"
		return 0
	done
}

# download_cloud PROVIDER signs in to Google Drive or OneDrive on this
# terminal and copies the HDMS backup folder into the drives folder, where
# the rest of --restore treats it like any other folder of backups.
download_cloud() {
	local provider=$1 client_id client_secret="" tenant="" folder dest ids list
	resolve_drives
	echo
	echo "The backups are in ${provider//_/ }. Use the OAuth client ID that HDMS was set up with"
	echo "(docs/runbooks/cloud-backup.md, step 1)."
	ask client_id "OAuth client ID"
	if [ "$provider" = google_drive ]; then
		while :; do
			ask_hidden client_secret "OAuth client secret (not shown)"
			[ -n "$client_secret" ] && break
			say "  Google needs the client secret."
		done
	else
		ask tenant "Directory (tenant) ID, or common" common
	fi
	ask folder "Folder name in the cloud drive" hdms-backups
	dest="$drives_real/cloud-restore"
	ids=$(docker run --rm --entrypoint sh "$worker_image" -c 'echo "$(id -u):$(id -g)"' </dev/null)
	mkdir -p "$dest"
	chown "$ids" "$dest" || die "could not give the HDMS worker ownership of $dest"
	# The client secret reaches the worker image on stdin, never in a command line.
	# shellcheck disable=SC2086 # tenant is empty for Google and a single word otherwise
	if ! printf '%s\n' "$client_secret" | docker run --rm -i -v "$dest:/download" --entrypoint hdms-cli "$worker_image" \
		cloud fetch --provider "$provider" --client-id "$client_id" ${tenant:+--tenant $tenant} --folder "$folder" --to /download; then
		die "downloading the backups failed; see the messages above, then run install.sh --restore --cloud=... again"
	fi
	client_secret=""
	list=$(find_sources "$dest")
	[ -n "$list" ] || die "nothing that looks like HDMS backups was downloaded into $dest"
	select_source "$list"
}
```

4. In `main()`: extend the argument loop with

```bash
		--cloud=google) cloud=google_drive ;;
		--cloud=onedrive) cloud=onedrive ;;
```

and, right after the loop's closing `done`, add:

```bash
	if [ -n "$cloud" ] && [ "$mode" != restore ]; then
		usage >&2
		exit 2
	fi
```

and in the restore branch replace `choose_source` with:

```bash
		if [ -n "$cloud" ]; then
			download_cloud "$cloud"
		else
			choose_source
		fi
```

- [ ] **Step 6: Extend `install_test.sh`**

In the stub `docker` script's `case`, before the `*'id -u'*` line, add:

```bash
*"cloud fetch"*)
	IFS= read -r secret || true
	printf '%s\n' "$secret" >>"$STUB_STATE/cloud.stdin"
	if [ -f "$STUB_STATE/cloud-fails" ]; then
		echo "hdms-cli: sign-in was declined" >&2
		exit 1
	fi
	host=$(printf '%s\n' "$*" | sed -n 's/.*-v \([^ ]*\):\/download.*/\1/p')
	mkdir -p "$host/hdms-backups/repo/locks"
	touch "$host/hdms-backups/repo/config" "$host/hdms-backups/hdms-recovery.bin"
	;;
```

Before the final `echo` / `if [ "$failures" -gt 0 ]` block at the end of the file, add:

```bash
# --- restore from the cloud ---------------------------------------------------
fresh_state
touch "$state/access-ok"
run_install "$(printf '%s\n' client-id-123 'g-secret-xyz' hdms-backups "$good_key")
$settings" --restore --cloud=google
check "cloud restore exits 0" exits_with 0
check "cloud restore: the client secret reaches the worker on stdin" has_line "$state/cloud.stdin" "g-secret-xyz"
check "cloud restore: the client secret is never in a docker command line" lacks "$state/docker.log" "g-secret-xyz"
check "cloud restore: fetch names the provider, client and folder" \
	grep -qF -- "cloud fetch --provider google_drive --client-id client-id-123 --folder hdms-backups --to /download" "$state/docker.log"
check "cloud restore: downloads into the drives folder" grep -qF -- "-v $drives_real/cloud-restore:/download" "$state/docker.log"
check "cloud restore: the downloaded folder is unwrapped like any other" \
	grep -qF -- "-v $drives_real/cloud-restore/hdms-backups:/restore-src:ro" "$state/docker.log"
check "cloud restore: the recovery key still decides the secrets" has_line "$env_file" "HDMS_BACKUP_ENC_KEY='b4ckup+Key/='"

fresh_state
touch "$state/access-ok"
run_install "$(printf '%s\n' client-id-456 contoso.onmicrosoft.com hdms-backups "$good_key")
$settings" --restore --cloud=onedrive
check "onedrive restore exits 0" exits_with 0
check "onedrive restore: the tenant is passed" \
	grep -qF -- "--provider onedrive --client-id client-id-456 --tenant contoso.onmicrosoft.com" "$state/docker.log"
check "onedrive restore: no client secret is asked for" [ "$(cat "$state/cloud.stdin")" = "" ]

fresh_state
touch "$state/cloud-fails"
run_install "$(printf '%s\n' client-id-123 'g-secret-xyz' hdms-backups)" --restore --cloud=google
check "a failed cloud download stops" exits_with 1
check "a failed cloud download says so" says "downloading the backups failed"
check "no env file after a failed cloud download" [ ! -e "$env_file" ]

fresh_state
run_install "" --cloud=google
check "--cloud without --restore is refused" exits_with 2
```

- [ ] **Step 7: Run the shell tests and linters**

Run: `bash deploy/production/install_test.sh && shellcheck deploy/production/install.sh deploy/production/install_test.sh`
Expected: `install_test: all checks passed`; shellcheck clean.

- [ ] **Step 8: Mutation check and commit**

In `download_cloud`, replace the stdin pipe with `--client-secret "$client_secret"` in the docker args and run `install_test.sh`: expect the "never in a docker command line" check to FAIL. Revert.

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./...`
Expected: clean.

```bash
git add hdms-backend/cmd/hdms-cli deploy/production/install.sh deploy/production/install_test.sh
git commit -m "feat(recovery): restore a lost server from Google Drive or OneDrive with install.sh --cloud"
```

---

### Task 8: Deployment, runbooks and the live check

**Files:**
- Modify: `deploy/production/compose.yaml`, `docker-compose.staging.yml`
- Create: `docs/runbooks/cloud-backup.md`
- Modify: `docs/runbooks/nightly-backup.md`, `docs/runbooks/disaster-recovery.md`

**Interfaces:** none.

- [ ] **Step 1: Keep the rendered config off disk**

In `deploy/production/compose.yaml`, under the `worker:` service (beside `volumes:`), add:

```yaml
    # The per-run rclone.conf holds live cloud tokens. A tmpfs /tmp means it
    # never reaches the container's disk layer and vanishes with the container.
    tmpfs:
      - /tmp:mode=1777
```

Add the same `tmpfs:` block to the worker service in `docker-compose.staging.yml`.

Run: `docker compose -f deploy/production/compose.yaml --env-file deploy/production/production.env.example config >/dev/null && echo ok` (substitute your staging invocation for the staging file).
Expected: `ok`. The worker's heartbeat file lives in `/tmp` and is written at runtime, so the healthcheck is unaffected.

- [ ] **Step 2: Write the IT runbook**

Create `docs/runbooks/cloud-backup.md`:

````markdown
# Cloud backup (Google Drive and OneDrive)

HDMS can keep an extra copy of every backup in Google Drive or OneDrive. The copy is
encrypted with the backup key before it leaves the server, so the cloud provider cannot
read it. The administrator connects an account in **Backups → Destinations**; IT registers
the OAuth client once.

## Before anything else

Backups leave the hospital network. Get data-protection approval (a name and a date) before
connecting an account. Record it with the hospital's other sign-offs.

## 1. Register an OAuth client (IT, once)

Keep the values in the hospital password manager **and** with the printed recovery sheet.
A new server cannot download the backups without them.

### Google Drive

1. Google Cloud Console → create a project (for example "HDMS backup").
2. APIs & Services → Library → enable **Google Drive API**.
3. APIs & Services → OAuth consent screen. Choose **Internal** if the hospital uses Google
   Workspace, otherwise **External**. Add the scope `.../auth/drive.file`.
   **If External, set the publishing status to "In production".** In "Testing" Google expires
   the refresh token after 7 days and backups stop a week later.
4. Credentials → Create credentials → OAuth client ID → application type
   **TVs and Limited Input devices**.
5. Copy the **client ID** and **client secret**.

### OneDrive

1. Microsoft Entra admin center → App registrations → New registration. Name it "HDMS
   backup". Supported account types: *Accounts in this organizational directory only*.
2. Copy the **Application (client) ID** and the **Directory (tenant) ID**.
3. Authentication → Advanced settings → **Allow public client flows: Yes**.
4. API permissions → Add → Microsoft Graph → Delegated → `Files.ReadWrite` and
   `offline_access`. Grant admin consent if your tenant requires it.

## 2. Connect the account (administrator)

1. Backups → Destinations → **Connect an account**.
2. Choose Google Drive or OneDrive, give it a name, paste the client ID (and secret for
   Google, the tenant ID for OneDrive).
3. Open the address shown on any device and enter the code. Sign in with the account whose
   Drive should hold the backups.
4. Add destination → Google Drive or OneDrive → pick the account → choose a folder name
   (default `hdms-backups`). HDMS creates it; it holds `repo/` and `hdms-recovery.bin`.
5. Wait for "First copy saved". Check the folder in the cloud drive.

HDMS can only see folders it created itself (Google's `drive.file` scope). Do not move or
rename the folder in the cloud drive.

## 3. When the status says "Sign-in expired" or "Access removed"

Backups to that account have stopped (the console and the dashboard say so). Press
**Reconnect** on the account and finish the sign-in again. Its destinations are kept.
Causes: the account's password changed, someone removed HDMS under the account's security
settings, or the app registration was deleted or its secret rotated.

## 4. A lost server: restore from the cloud

On the new server, with the repository checked out and the TLS certificate in place
(production-deployment.md steps 1–4):

```bash
sudo deploy/production/install.sh --restore --cloud=google     # or --cloud=onedrive
```

The script asks for the OAuth client ID (and secret / tenant), prints an address and a code to
open on any device, downloads the backup folder into `/mnt/cloud-restore`, then asks for the
**recovery key** from the printed sheet and carries on exactly as in disaster-recovery.md.
Finish in the browser at `https://<host>/recovery`.

Keep with the recovery sheet: the provider, the OAuth client ID (and Google secret), the
OneDrive tenant ID, and the cloud folder name.

## What HDMS stores

Client secret and tokens are encrypted with `HDMS_CREDENTIAL_ENC_KEY`. During a backup the
worker writes a private rclone configuration into a temporary directory on a tmpfs and
deletes it afterwards. Disconnecting an account deletes the stored sign-in but never deletes
backups in the cloud.

The manual command `hdms-cli backup` does not copy to cloud destinations; the worker does.
````

- [ ] **Step 3: Update the old runbooks**

In `docs/runbooks/nightly-backup.md`: replace the subsection that begins "Cloud storage destinations route through `rclone`. HDMS stores no cloud credentials…" and runs through its step 6 about `HDMS_RCLONE_CONFIG` with:

```markdown
Google Drive and OneDrive destinations are connected in the console, not with `rclone config`. See
[cloud-backup.md](cloud-backup.md). `HDMS_RCLONE_CONFIG` now applies only to hand-configured legacy `rclone` destinations.
```

and in the troubleshooting line that says to run `rclone config reconnect <remote>:` for `kind = 'rclone'`, replace the advice with: "For a cloud account, open Backups → Destinations and press **Reconnect** (see cloud-backup.md, section 3)."

In `docs/runbooks/disaster-recovery.md`, append a section:

```markdown
## Backups only in Google Drive or OneDrive

If no disk or network copy survived, use `sudo deploy/production/install.sh --restore --cloud=google` (or
`--cloud=onedrive`). It downloads the backup folder first, then follows the normal restore. Details and what to
keep with the recovery sheet: [cloud-backup.md](cloud-backup.md), section 4.
```

- [ ] **Step 4: Full gate**

Run, one after another (not in parallel):
`task lint` · `cd hdms-backend && go test ./... && go test -race -tags=integration ./test/...` · `cd hdms-frontend && pnpm -r build && pnpm -r test` · `bash deploy/production/install_test.sh`
Expected: all green. If a recovery-app test times out only under `pnpm -r`, re-run it alone (known load-sensitive).

- [ ] **Step 5: Live check with real accounts (a human, not CI)**

Record the result in the commit message or the runbook's drill table.

1. Bring up the dev stack on this branch. In Google Cloud and Entra register test OAuth clients as in the runbook (Google: **Internal** or published).
2. Backups → Connect an account (Google). Confirm: the code appears, signing in flips the dialog to "Connected as …", the list shows the account.
3. Add destination → Google Drive → folder `hdms-test`. Confirm "First copy saved", and in Drive a folder `hdms-test` holding `repo/` and `hdms-recovery.bin`.
4. Back up now twice; Verify now. Confirm snapshots appear under the destination and `degraded` is not reported.
5. **Token refresh:** wait over an hour, back up again, then check the account is still **Connected** and `backup_cloud_accounts.token_enc` changed (proves rclone's refreshed token was written back).
6. **Revocation:** remove HDMS in the Google account's third-party-access page, back up now. Confirm the account shows "Access removed", the run is degraded, and **Reconnect** restores it.
7. Repeat 2–4 for OneDrive.
8. **Server-lost drill** on a fresh VM, by someone who did not write the runbook: `install.sh --restore --cloud=google`, then the recovery page. Confirm the sign-in works with the *same* client ID on a new machine (drive.file shows files the same client created) and the restore completes. Time it against the 4-hour RTO.
9. Confirm `/tmp` inside the worker container is a tmpfs: `docker compose exec worker sh -c 'mount | grep " /tmp "'` and that no `hdms-rclone-*` directory remains after a run.

If step 8 shows the new machine cannot see the folder, the `drive.file` assumption is wrong for this client type; stop and revisit the scope before shipping.

- [ ] **Step 6: Commit**

```bash
git add deploy docker-compose.staging.yml docs/runbooks
git commit -m "docs(backup): cloud backup setup, recovery from the cloud, and tmpfs for the worker's rclone config"
```

---

## Self-Review

**Spec coverage** (spec sections "Cloud sign-in", "Data model", "API", "Console"):
- Device flow for Google (`drive.file`, secret required) and OneDrive (public client, `Files.ReadWrite offline_access`) → Task 1.
- `backup_cloud_accounts` (statuses, sealed secrets, in-flight device fields) and the `backup_destinations` columns → Task 2.
- Worker renders a `0600` config on tmpfs, runs restic with it, reads back refreshed tokens, revocation → `last_error` + `revoked` → Task 3 (+ tmpfs in Task 8).
- Account endpoints (list/create/poll/reconnect/delete-refused-in-use) and audit → Task 5; cloud connect dialog, accounts section, wizard cloud card replacing "coming soon" → Task 6.
- Restore keeps cloud accounts (spec "cloud accounts… copied forward") → Task 4.
- Not in the original spec but required by the user's answers: cloud disaster recovery → Task 7; IT OAuth registration and the data-protection sign-off → Task 8 runbook and "Needs a human" below.
- Gap, deliberate: the `/recovery` page has no Cloud source (the user chose the `install.sh` route); the spec's "Cloud storage as a restore source" is met by Task 7.

**Placeholder scan:** no TBD/TODO; every step shows its code.

**Type consistency:** `CloudService`, `CloudCreds`, `CloudSession`, `Rclone`, `CloudDestinationInput`, `CreateCloudDestination(ctx, pool, acct, in, actor)`, `OpenSession(ctx, dests, rc)`, `PutRecoveryBundle(ctx, sess, d, repo, bundle)`, `Destination.CloudAccountID/Folder`, `cloudFetch` fields, and the HTTP/generated names (`gen.BackupCloud*`, `BackupCloudProvider`) are used identically across tasks 1–8. `strPtr` exists in both `apiserver` and the integration package.

**Review Focus:** each of the five has a named test (Tasks 2, 3, 4, 5, 7) and a mutation check.

## Needs a human, not code

- One-time OAuth client registration in Google Cloud and Entra (runbook section 1). **Google consent screen must be Internal or "In production"**, or refresh tokens die after 7 days.
- Data-protection sign-off, with a name and date, for backups leaving the network.
- Native-speaker pass on the Japanese cloud strings.
- The live check in Task 8 Step 5, especially step 5 (token refresh persisted) and step 8 (server-lost drill from the cloud).

## Risks

| Risk | Mitigation |
|---|---|
| Microsoft rotates refresh tokens; if the worker dies before `Close` writes the refreshed token back, the stored one may be stale | Old refresh tokens stay valid for a grace period; `Reconnect` recovers; Task 8 step 5 watches for it |
| `drive.file` may not show the backup folder to a new machine | Task 8 step 8 is a hard stop before shipping |
| rclone changes how it saves refreshed tokens | The parse is by `[remote]` + `token =` only; the live check in step 5 would show it |
| Error-text revocation detection misses a provider phrasing | The account stays `connected`, the destination shows its `last_error`, and the run is `degraded`; add the phrase to `IsTokenRevoked` |
