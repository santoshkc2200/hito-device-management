package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// Phase 4 landed its whole contract surface in one additive pass (4.0a) so
// that no later task has to reopen openapi.yaml mid-feature and so the kiosk
// scope suite classifies every new operation the moment its spec exists. The
// handlers below are the other half of that trade: gen.ServerInterface is a
// single interface, so the spec cannot land without something implementing
// every operation on it.
//
// Each stub answers 501 with a problem naming the task that fills it in. That
// is a deliberate choice over silently returning empty data: a screen wired to
// an unimplemented endpoint fails loudly in development rather than rendering
// a plausible-looking empty table, which is exactly the "empty table with no
// explanation" the phase's quality section calls a support call.
//
// Deleting a stub is how a Phase 4 task starts. The file is empty when the
// phase is done.
func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request, task string) {
	p := httpx.NewProblem("not-implemented", "Not implemented yet", http.StatusNotImplemented)
	p.Detail = "This endpoint's contract is frozen but its implementation lands in Phase " + task + "."
	p.Extensions = map[string]any{"task": task}
	httpx.WriteProblem(w, r, p)
}

// --- 4.10 Settings and kiosk management --------------------------------------

func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	s.notImplemented(w, r, "4.10a")
}

func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	s.notImplemented(w, r, "4.10a")
}

func (s *Server) GetKiosk(w http.ResponseWriter, r *http.Request, _ gen.IDParam) {
	s.notImplemented(w, r, "4.10b")
}

func (s *Server) UpdateKiosk(w http.ResponseWriter, r *http.Request, _ gen.IDParam) {
	s.notImplemented(w, r, "4.10b")
}

func (s *Server) EnableKiosk(w http.ResponseWriter, r *http.Request, _ gen.IDParam) {
	s.notImplemented(w, r, "4.10b")
}
