//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPCreateDeviceIssuesQRCredential(t *testing.T) {
	h := newTestHarness(t)

	catResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{"name": "Monitors"})
	cat := decodeBody[gen.Category](t, catResp)

	devResp := h.doJSON(t, http.MethodPost, "/v1/devices", "", map[string]any{
		"assetTag":   "MON-AUTO-01",
		"categoryId": cat.Id,
		"name":       "Bedside monitor",
	})
	if devResp.StatusCode != http.StatusCreated {
		t.Fatalf("create device: status = %d", devResp.StatusCode)
	}
	device := decodeBody[gen.Device](t, devResp)

	creds, err := h.credentials.ListBySubject(t.Context(), credentialsapi.SubjectDevice, device.Id)
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("credentials = %d, want exactly 1 issued at creation", len(creds))
	}
	if creds[0].Kind != credentialsapi.KindQR || creds[0].Status != credentialsapi.StatusActive {
		t.Fatalf("credential = kind %q status %q, want active qr", creds[0].Kind, creds[0].Status)
	}

	// A rejected create must not leave a credential behind.
	dup := h.doJSON(t, http.MethodPost, "/v1/devices", "", map[string]any{
		"assetTag":   "MON-AUTO-01",
		"categoryId": cat.Id,
		"name":       "Duplicate tag",
	})
	if dup.StatusCode == http.StatusCreated {
		t.Fatalf("duplicate asset tag: status = %d, want a rejection", dup.StatusCode)
	}
}
