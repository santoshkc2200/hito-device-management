package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) RevealCredential(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	issued, err := s.credentials.Reveal(r.Context(), id, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, issuedCredentialToGen(issued))
}
