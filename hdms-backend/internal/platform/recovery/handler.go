package recovery

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

const (
	sessionCookie   = "hdms_recovery"
	sessionLifetime = 30 * time.Minute
	maxRecoveryBody = 4096
	confirmWord     = "RESTORE"
)

// Handler serves /recovery/api/*, the browser-only restore. The recovery key
// is the only credential: it is checked against a bundle on disk, never
// logged, never stored, and its bytes are cleared once the bundle is open.
type Handler struct {
	Engine    *Engine
	Status    func(ctx context.Context) LiveState
	Worker    func() string
	Sources   func(ctx context.Context) []Source
	Snapshots func(ctx context.Context, src Source) ([]backup.Snapshot, error)
	// Secrets are the worker's running secrets; a bundle holding others
	// cannot be restored here (keys_mismatch).
	Secrets backup.RecoverySecrets
	Limiter *Limiter
	Now     func() time.Time
	Logger  *slog.Logger

	once     sync.Once
	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	source     Source
	keysMatch  bool
	ip         string
	unlockedAt time.Time
	lastUsed   time.Time
}

func (h *Handler) Routes() http.Handler {
	h.once.Do(func() { h.sessions = map[string]*session{} })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /recovery/api/status", h.status)
	mux.HandleFunc("GET /recovery/api/sources", h.sources)
	mux.HandleFunc("POST /recovery/api/unlock", h.unlock)
	mux.HandleFunc("GET /recovery/api/snapshots", h.withSession(h.snapshots))
	mux.HandleFunc("POST /recovery/api/restore", h.withSession(h.startRestore))
	mux.HandleFunc("GET /recovery/api/restore", h.withSession(h.restoreState))
	mux.HandleFunc("POST /recovery/api/restore/undo", h.withSession(h.undo))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"database":       h.Status(r.Context()),
		"worker":         h.Worker(),
		"restoreRunning": h.Engine.Active(),
	})
}

func (h *Handler) sources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": h.Sources(r.Context())})
}

func (h *Handler) unlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		Key    string `json:"key"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	key, err := backup.ParseRecoveryKey(body.Key)
	body.Key = ""
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyChecksum):
		writeError(w, http.StatusUnprocessableEntity, "key_typo")
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, "key_format")
		return
	}
	defer clear(key[:])

	ip := httpx.ClientIP(r)
	src, ok := findSource(h.Sources(r.Context()), body.Source)
	if !ok {
		writeError(w, http.StatusNotFound, "source_not_found")
		return
	}
	bundle, err := backup.ReadRecoveryBundle(src.Folder)
	if errors.Is(err, backup.ErrRecoveryBundleMissing) {
		writeError(w, http.StatusNotFound, "no_bundle")
		return
	}
	if err != nil {
		h.Logger.Error("recovery: read bundle", "source", src.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if allowed, wait := h.Limiter.Take(ip); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		h.Logger.Warn("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "rate_limited")
		writeError(w, http.StatusTooManyRequests, "too_many_attempts")
		return
	}
	secrets, err := backup.OpenRecoveryBundle(key, bundle)
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyWrong):
		h.Logger.Warn("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "wrong_key")
		writeError(w, http.StatusUnauthorized, "key_wrong")
		return
	case err != nil:
		h.Logger.Warn("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "bundle_damaged")
		writeError(w, http.StatusUnprocessableEntity, "bundle_damaged")
		return
	}
	match := secrets.Fingerprint() == h.Secrets.Fingerprint()
	token, err := h.newSession(&session{source: src, keysMatch: match, ip: ip, unlockedAt: h.Now().UTC()})
	if err != nil {
		h.Logger.Error("recovery: create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/recovery",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	h.Logger.Info("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "unlocked", "keysMatch", match)
	if !match {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keysMatch": true})
}

func (h *Handler) snapshots(w http.ResponseWriter, r *http.Request, s session) {
	if !s.keysMatch {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	snaps, err := h.Snapshots(r.Context(), s.source)
	if err != nil {
		h.Logger.Error("recovery: list snapshots", "source", s.source.ID, "error", err)
		writeError(w, http.StatusBadGateway, "repository_unreadable")
		return
	}
	slices.SortFunc(snaps, func(a, b backup.Snapshot) int { return b.Time.Compare(a.Time) })
	type view struct {
		ID        string    `json:"id"`
		TakenAt   time.Time `json:"takenAt"`
		SizeBytes int64     `json:"sizeBytes"`
	}
	out := make([]view, 0, len(snaps))
	for _, sn := range snaps {
		v := view{ID: sn.ID, TakenAt: sn.Time.UTC()}
		if sn.Summary != nil {
			v.SizeBytes = sn.Summary.TotalBytesProcessed
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}

func (h *Handler) startRestore(w http.ResponseWriter, r *http.Request, s session) {
	var body struct {
		SnapshotID   string `json:"snapshotId"`
		Confirmation string `json:"confirmation"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if body.Confirmation != confirmWord {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required")
		return
	}
	if !s.keysMatch {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	snaps, err := h.Snapshots(r.Context(), s.source)
	if err != nil {
		h.Logger.Error("recovery: list snapshots", "source", s.source.ID, "error", err)
		writeError(w, http.StatusBadGateway, "repository_unreadable")
		return
	}
	i := slices.IndexFunc(snaps, func(sn backup.Snapshot) bool { return sn.ID == body.SnapshotID })
	if i < 0 {
		writeError(w, http.StatusNotFound, "snapshot_not_found")
		return
	}
	st, err := h.Engine.Start(Request{
		Source: s.source, SnapshotID: snaps[i].ID, SnapshotTakenAt: snaps[i].Time.UTC(),
		UnlockIP: s.ip, UnlockedAt: s.unlockedAt,
	})
	if !h.writeEngineError(w, err) {
		return
	}
	h.Logger.Info("recovery: restore started", "ip", s.ip, "source", s.source.ID, "snapshot", snaps[i].ID, "live", st.LiveState)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": viewOf(st)})
}

func (h *Handler) restoreState(w http.ResponseWriter, r *http.Request, _ session) {
	writeJSON(w, http.StatusOK, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *Handler) undo(w http.ResponseWriter, r *http.Request, s session) {
	var body struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if body.Confirmation != confirmWord {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required")
		return
	}
	if !s.keysMatch {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	st, err := h.Engine.Undo(UndoRequest{UnlockIP: s.ip, UnlockedAt: s.unlockedAt})
	if !h.writeEngineError(w, err) {
		return
	}
	h.Logger.Info("recovery: undo started", "ip", s.ip)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": viewOf(st)})
}

// writeEngineError writes the response for an engine error and reports
// whether the caller should carry on (err was nil).
func (h *Handler) writeEngineError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrRestoreActive):
		writeError(w, http.StatusConflict, "restore_running")
	case errors.Is(err, ErrNothingToUndo):
		writeError(w, http.StatusConflict, "nothing_to_undo")
	case errors.Is(err, ErrServerDown):
		writeError(w, http.StatusConflict, "database_server_down")
	default:
		h.Logger.Error("recovery: start", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
	return false
}

func (h *Handler) newSession(s *session) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	s.lastUsed = h.Now()
	h.mu.Lock()
	h.sessions[token] = s
	h.mu.Unlock()
	return token, nil
}

// withSession resolves the cookie to a session, sliding its 30 minutes.
func (h *Handler) withSession(next func(http.ResponseWriter, *http.Request, session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		now := h.Now()
		h.mu.Lock()
		for token, s := range h.sessions {
			if now.Sub(s.lastUsed) > sessionLifetime {
				delete(h.sessions, token)
			}
		}
		s, ok := h.sessions[c.Value]
		var snapshot session
		if ok {
			s.lastUsed = now
			snapshot = *s
		}
		h.mu.Unlock()
		if !ok {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		next(w, r, snapshot)
	}
}

func findSource(list []Source, id string) (Source, bool) {
	i := slices.IndexFunc(list, func(s Source) bool { return s.ID == id })
	if i < 0 {
		return Source{}, false
	}
	return list[i], true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRecoveryBody)
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
