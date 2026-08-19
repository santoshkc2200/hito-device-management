//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPCategoryCreateAndUpdate(t *testing.T) {
	h := newTestHarness(t)

	createResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{
		"name":                     "Pendrives",
		"defaultLoanPeriodSeconds": 86400,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create category: status = %d", createResp.StatusCode)
	}
	category := decodeBody[gen.Category](t, createResp)
	if category.DefaultLoanPeriodSeconds == nil || *category.DefaultLoanPeriodSeconds != 86400 {
		t.Fatalf("defaultLoanPeriodSeconds = %v, want 86400", category.DefaultLoanPeriodSeconds)
	}

	updateResp := h.doJSON(t, http.MethodPatch, "/v1/categories/"+category.Id, "", map[string]any{
		"name":                     "USB Drives",
		"defaultLoanPeriodSeconds": 172800,
		"requiresApproval":         true,
	})
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update category: status = %d", updateResp.StatusCode)
	}
	updated := decodeBody[gen.Category](t, updateResp)
	if updated.Name != "USB Drives" || updated.DefaultLoanPeriodSeconds == nil || *updated.DefaultLoanPeriodSeconds != 172800 || !updated.RequiresApproval {
		t.Fatalf("category after update = %+v", updated)
	}

	listResp := h.get(t, "/v1/categories")
	list := decodeBody[gen.CategoryList](t, listResp)
	found := false
	for _, c := range list.Items {
		if c.Id == category.Id && c.Name == "USB Drives" {
			found = true
		}
	}
	if !found {
		t.Fatal("updated category not present in list")
	}
}
