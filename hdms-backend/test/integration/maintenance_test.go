//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestMaintenanceDefaultsOff(t *testing.T) {
	pool := testdb.New(t)
	m, err := backup.GetMaintenance(context.Background(), pool.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if m.On || m.Since != nil {
		t.Fatalf("maintenance = %+v, want off", m)
	}
}

func TestMaintenanceGate(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := apiserver.MaintenanceGate(pool, func() time.Time { return now })(ok)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	if got := get("/v1/devices").Code; got != http.StatusOK {
		t.Fatalf("maintenance off: /v1/devices = %d", got)
	}

	if err := backup.SetMaintenance(ctx, pool.Pool, true, "restore"); err != nil {
		t.Fatal(err)
	}
	// Within the cache window the gate has not seen the switch yet.
	if got := get("/v1/devices").Code; got != http.StatusOK {
		t.Fatalf("inside the 2s cache: /v1/devices = %d, want the cached 200", got)
	}
	now = now.Add(3 * time.Second)

	rec := get("/v1/devices")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "15" {
		t.Fatalf("maintenance on: /v1/devices = %d Retry-After %q, want 503 and 15", rec.Code, rec.Header().Get("Retry-After"))
	}
	var problem struct{ Type string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if !strings.HasSuffix(problem.Type, "/maintenance") {
		t.Fatalf("problem type = %q, want …/maintenance", problem.Type)
	}
	for _, path := range []string{"/v1/healthz", "/v1/readyz", "/v1/auth/login", "/v1/auth/me", "/v1/backup/config", "/metrics"} {
		if got := get(path).Code; got != http.StatusOK {
			t.Errorf("maintenance on: %s = %d, want it exempt", path, got)
		}
	}
	for _, path := range []string{"/v1/auth/me/locale", "/v1/kiosk/scan", "/v1/backups"} {
		if got := get(path).Code; got != http.StatusServiceUnavailable {
			t.Errorf("maintenance on: %s = %d, want 503", path, got)
		}
	}

	m, _ := backup.GetMaintenance(ctx, pool.Pool)
	if !m.On || m.Reason != "restore" || m.Since == nil {
		t.Fatalf("stored maintenance = %+v", m)
	}
	if err := backup.SetMaintenance(ctx, pool.Pool, false, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	if got := get("/v1/devices").Code; got != http.StatusOK {
		t.Fatalf("maintenance off again: /v1/devices = %d", got)
	}
	if m, _ := backup.GetMaintenance(ctx, pool.Pool); m.Since != nil || m.Reason != "" {
		t.Fatalf("switching off must clear reason and since: %+v", m)
	}
}
