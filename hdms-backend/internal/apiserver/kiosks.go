package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// ListKiosks lists all registered kiosks.
func (s *Server) ListKiosks(w http.ResponseWriter, r *http.Request) {
	kiosks, err := s.auth.ListKiosks(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Kiosk, len(kiosks))
	for i, k := range kiosks {
		items[i] = mapKiosk(k)
	}

	writeJSON(w, http.StatusOK, gen.KioskList{Items: items})
}

// CreateKiosk registers a new kiosk, returning the plaintext token exactly once.
func (s *Server) CreateKiosk(w http.ResponseWriter, r *http.Request, _ gen.CreateKioskParams) {
	body, ok := decodeJSON[gen.CreateKioskRequest](w, r)
	if !ok {
		return
	}

	if body.Name == "" {
		writeValidationFailed(w, r, "name is required", []string{"name"})
		return
	}

	id, token, err := s.auth.RegisterKiosk(r.Context(), body.Name, fromPtr(body.Location))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	kiosk, err := s.auth.GetKiosk(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	setNoStore(w)
	writeJSON(w, http.StatusCreated, gen.KioskWithToken{
		Id:             kiosk.ID,
		Name:           kiosk.Name,
		Location:       strPtr(kiosk.Location),
		EnabledSources: kiosk.EnabledSources,
		Status:         gen.KioskStatus(kiosk.Status),
		LastSeenAt:     kiosk.LastSeenAt,
		CreatedAt:      kiosk.CreatedAt,
		Token:          token,
	})
}

// RotateKioskToken mints a fresh token for an existing kiosk.
func (s *Server) RotateKioskToken(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.RotateKioskTokenParams) {
	token, err := s.auth.RotateKioskToken(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	kiosk, err := s.auth.GetKiosk(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	setNoStore(w)
	writeJSON(w, http.StatusOK, gen.KioskWithToken{
		Id:             kiosk.ID,
		Name:           kiosk.Name,
		Location:       strPtr(kiosk.Location),
		EnabledSources: kiosk.EnabledSources,
		Status:         gen.KioskStatus(kiosk.Status),
		LastSeenAt:     kiosk.LastSeenAt,
		CreatedAt:      kiosk.CreatedAt,
		Token:          token,
	})
}

// DisableKiosk disables a kiosk, immediately rejecting future requests from it.
func (s *Server) DisableKiosk(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.DisableKioskParams) {
	kiosk, err := s.auth.DisableKiosk(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapKiosk(kiosk))
}

// CreateKioskPairingCode issues a short-lived single-use pairing code for an iPad.
func (s *Server) CreateKioskPairingCode(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.CreateKioskPairingCodeParams) {
	code, expiresAt, err := s.auth.IssuePairingCode(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	setNoStore(w)
	writeJSON(w, http.StatusOK, gen.KioskPairingCode{
		Code:      code,
		ExpiresAt: expiresAt,
	})
}

// PairKiosk redeems a pairing code for a working kiosk token.
func (s *Server) PairKiosk(w http.ResponseWriter, r *http.Request, _ gen.PairKioskParams) {
	body, ok := decodeJSON[gen.PairKioskRequest](w, r)
	if !ok {
		return
	}

	kioskID, name, token, err := s.auth.RedeemPairingCode(r.Context(), body.Code)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	setNoStore(w)
	writeJSON(w, http.StatusOK, gen.PairKioskResponse{
		KioskId: kioskID,
		Name:    name,
		Token:   token,
	})
}

// GetKiosk fetches one kiosk.
func (s *Server) GetKiosk(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	kiosk, err := s.auth.GetKiosk(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapKiosk(kiosk))
}

// UpdateKiosk edits a kiosk's name, location or enabled scan sources.
func (s *Server) UpdateKiosk(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	body, ok := decodeJSON[gen.UpdateKioskRequest](w, r)
	if !ok {
		return
	}

	var enabledSources []string
	if body.EnabledSources != nil {
		enabledSources = *body.EnabledSources
	}

	kiosk, err := s.auth.UpdateKiosk(r.Context(), id, body.Name, body.Location, enabledSources)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapKiosk(kiosk))
}

// EnableKiosk re-enables a disabled kiosk.
func (s *Server) EnableKiosk(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	kiosk, err := s.auth.EnableKiosk(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapKiosk(kiosk))
}

func mapKiosk(k auth.Kiosk) gen.Kiosk {
	return gen.Kiosk{
		Id:             k.ID,
		Name:           k.Name,
		Location:       strPtr(k.Location),
		EnabledSources: k.EnabledSources,
		Status:         gen.KioskStatus(k.Status),
		LastSeenAt:     k.LastSeenAt,
		CreatedAt:      k.CreatedAt,
	}
}
