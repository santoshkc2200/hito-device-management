package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// GetEventsStream handles GET /v1/events/stream, streaming Server-Sent Events.
func (s *Server) GetEventsStream(w http.ResponseWriter, r *http.Request, _ gen.GetEventsStreamParams) {
	if s.sseHub == nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("events-unavailable", "Event stream is not available", http.StatusInternalServerError))
		return
	}
	s.sseHub.Stream(w, r)
}
