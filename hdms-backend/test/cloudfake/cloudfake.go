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
