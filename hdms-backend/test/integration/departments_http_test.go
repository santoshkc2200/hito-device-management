//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPListDepartments(t *testing.T) {
	h := newTestHarness(t)

	dept, err := h.identity.GetOrCreateDepartment(t.Context(), "Radiology")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	listResp := h.get(t, "/v1/departments")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list departments: status = %d", listResp.StatusCode)
	}
	list := decodeBody[gen.DepartmentList](t, listResp)
	found := false
	for _, d := range list.Items {
		if d.Id == dept.ID && d.Name == "Radiology" {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeded department not present in list: %+v", list.Items)
	}
}
