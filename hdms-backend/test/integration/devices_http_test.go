//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPDeviceCRUDAndStatusTransition(t *testing.T) {
	h := newTestHarness(t)

	catResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{"name": "Laptops"})
	category := decodeBody[gen.Category](t, catResp)
	if catResp.StatusCode != http.StatusCreated {
		t.Fatalf("create category: status = %d", catResp.StatusCode)
	}

	createResp := h.doJSON(t, http.MethodPost, "/v1/devices", "", map[string]any{
		"assetTag":   "LAPTOP-07",
		"name":       "Dell Latitude 5420",
		"categoryId": category.Id,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create device: status = %d", createResp.StatusCode)
	}
	device := decodeBody[gen.Device](t, createResp)
	if device.Status != "available" {
		t.Fatalf("new device status = %q, want available", device.Status)
	}

	getResp := h.get(t, "/v1/devices/"+device.Id)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get device: status = %d", getResp.StatusCode)
	}
	getResp.Body.Close()

	listResp := h.get(t, "/v1/devices?q=LAPTOP-07")
	list := decodeBody[gen.DeviceList](t, listResp)
	if len(list.Items) != 1 || list.Items[0].Id != device.Id {
		t.Fatalf("list devices by query: got %d items, want 1 matching", len(list.Items))
	}

	// available -> maintenance is legal.
	statusResp := h.doJSON(t, http.MethodPost, "/v1/devices/"+device.Id+"/status", "", map[string]any{
		"status": "maintenance", "reason": "annual service",
	})
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("legal transition: status = %d", statusResp.StatusCode)
	}
	statusResp.Body.Close()

	// retired -> on_loan is illegal. First retire it (maintenance -> retired is legal).
	retireResp := h.doJSON(t, http.MethodPost, "/v1/devices/"+device.Id+"/status", "", map[string]any{
		"status": "retired", "reason": "decommissioned",
	})
	if retireResp.StatusCode != http.StatusOK {
		t.Fatalf("retire: status = %d", retireResp.StatusCode)
	}
	retireResp.Body.Close()

	illegalResp := h.doJSON(t, http.MethodPost, "/v1/devices/"+device.Id+"/status", "", map[string]any{
		"status": "on_loan",
	})
	if illegalResp.StatusCode != http.StatusConflict {
		t.Fatalf("illegal transition retired->on_loan: status = %d, want 409", illegalResp.StatusCode)
	}
	problem := decodeBody[gen.Problem](t, illegalResp)
	if problem.Type == "" {
		t.Fatal("expected a populated problem type for the illegal transition")
	}
}
