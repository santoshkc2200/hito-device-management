package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// Phase 4 landed its whole contract surface in one additive pass (4.0a) so
// that no later task has to reopen openapi.yaml mid-feature and so the kiosk
// scope suite classifies every new operation the moment its spec exists.
//
// Every Phase 4 operation is now fully implemented.
func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request, task string) {
	p := httpx.NewProblem("not-implemented", "Not implemented yet", http.StatusNotImplemented)
	p.Detail = "This endpoint's contract is frozen but its implementation lands in Phase " + task + "."
	p.Extensions = map[string]any{"task": task}
	httpx.WriteProblem(w, r, p)
}
