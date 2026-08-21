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


// --- 4.3c / 4.4d CSV import --------------------------------------------------

func (s *Server) PreviewDeviceImport(w http.ResponseWriter, r *http.Request) {
	s.notImplemented(w, r, "4.3c")
}

func (s *Server) CommitDeviceImport(w http.ResponseWriter, r *http.Request, _ gen.CommitDeviceImportParams) {
	s.notImplemented(w, r, "4.3c")
}

// --- 4.8c Loan overrides -----------------------------------------------------

func (s *Server) CorrectLoanAttribution(w http.ResponseWriter, r *http.Request, _ gen.IDParam, _ gen.CorrectLoanAttributionParams) {
	s.notImplemented(w, r, "4.8c")
}

// --- 4.9 Reports and audit ---------------------------------------------------

func (s *Server) GetReportSummary(w http.ResponseWriter, r *http.Request, _ gen.GetReportSummaryParams) {
	s.notImplemented(w, r, "4.9a")
}

func (s *Server) GetTransactionsByOrigin(w http.ResponseWriter, r *http.Request, _ gen.GetTransactionsByOriginParams) {
	s.notImplemented(w, r, "4.9a")
}

func (s *Server) GetOperationalHealth(w http.ResponseWriter, r *http.Request, _ gen.GetOperationalHealthParams) {
	s.notImplemented(w, r, "4.9a")
}

func (s *Server) ListDisputedLoans(w http.ResponseWriter, r *http.Request, _ gen.ListDisputedLoansParams) {
	s.notImplemented(w, r, "4.9a")
}

func (s *Server) ExportLoansCsv(w http.ResponseWriter, r *http.Request, _ gen.ExportLoansCsvParams) {
	s.notImplemented(w, r, "4.9c")
}

func (s *Server) ExportDevicesCsv(w http.ResponseWriter, r *http.Request, _ gen.ExportDevicesCsvParams) {
	s.notImplemented(w, r, "4.9c")
}

func (s *Server) ExportUsersCsv(w http.ResponseWriter, r *http.Request, _ gen.ExportUsersCsvParams) {
	s.notImplemented(w, r, "4.9c")
}

func (s *Server) ListAuditEvents(w http.ResponseWriter, r *http.Request, _ gen.ListAuditEventsParams) {
	s.notImplemented(w, r, "4.9d")
}

func (s *Server) ExportAuditCsv(w http.ResponseWriter, r *http.Request, _ gen.ExportAuditCsvParams) {
	s.notImplemented(w, r, "4.9d")
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
