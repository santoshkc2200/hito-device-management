//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
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

func TestHTTPDepartmentCRUDAndDeleteGuard(t *testing.T) {
	h := newTestHarness(t)

	createResp := h.post(t, "/v1/departments", map[string]string{"name": "Cardiology"})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create department: status = %d", createResp.StatusCode)
	}
	created := decodeBody[gen.Department](t, createResp)
	if created.Name != "Cardiology" || created.Id == "" {
		t.Fatalf("created department = %+v", created)
	}

	updateResp := h.doJSON(t, http.MethodPatch, "/v1/departments/"+created.Id, "", map[string]string{"name": "Cardiology & Oncology"})
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update department: status = %d", updateResp.StatusCode)
	}
	updated := decodeBody[gen.Department](t, updateResp)
	if updated.Name != "Cardiology & Oncology" {
		t.Fatalf("updated department = %+v", updated)
	}

	unusedResp := h.post(t, "/v1/departments", map[string]string{"name": "Unused Department"})
	if unusedResp.StatusCode != http.StatusCreated {
		t.Fatalf("create unused department: status = %d", unusedResp.StatusCode)
	}
	unused := decodeBody[gen.Department](t, unusedResp)
	deleteResp := h.doJSON(t, http.MethodDelete, "/v1/departments/"+unused.Id, "", nil)
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete unused department: status = %d", deleteResp.StatusCode)
	}
	deleteResp.Body.Close()

	if _, err := h.identity.CreateUser(t.Context(), identityapi.CreateUserParams{
		EmployeeNo:   "DEPT-CRUD-001",
		FullName:     "Department Test User",
		DepartmentID: created.Id,
		RegisteredBy: "admin:test",
	}); err != nil {
		t.Fatalf("create user in department: %v", err)
	}

	usedDeleteResp := h.doJSON(t, http.MethodDelete, "/v1/departments/"+created.Id, "", nil)
	if usedDeleteResp.StatusCode != http.StatusConflict {
		t.Fatalf("delete used department: status = %d", usedDeleteResp.StatusCode)
	}
	problem := decodeBody[httpx.Problem](t, usedDeleteResp)
	if !strings.Contains(problem.Detail, "being used") {
		t.Fatalf("delete used department detail = %q", problem.Detail)
	}
}
