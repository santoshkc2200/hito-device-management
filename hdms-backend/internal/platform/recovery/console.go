package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	ErrSnapshotNotFound     = errors.New("recovery: no such backup in that location")
	ErrRepositoryUnreadable = errors.New("recovery: the backup location cannot be read")
	errBadConsoleRequest    = errors.New("recovery: bad console request")
)

// consoleErrorCodes are the wire codes of the /internal/restore routes; the
// Client maps them back to the same errors.
var consoleErrorCodes = []struct {
	err    error
	status int
	code   string
}{
	{ErrRestoreActive, http.StatusConflict, "restore_running"},
	{ErrNothingToUndo, http.StatusConflict, "nothing_to_undo"},
	{ErrNothingToDiscard, http.StatusConflict, "nothing_to_discard"},
	{ErrServerDown, http.StatusConflict, "database_server_down"},
	{ErrSourceNotFound, http.StatusNotFound, "source_not_found"},
	{ErrSourceUnsupported, http.StatusUnprocessableEntity, "source_unsupported"},
	{ErrSnapshotNotFound, http.StatusNotFound, "snapshot_not_found"},
	{ErrRepositoryUnreadable, http.StatusBadGateway, "repository_unreadable"},
	{errBadConsoleRequest, http.StatusBadRequest, "bad_request"},
}

// ConsoleHandler serves the worker's /internal/restore routes: the admin
// console's restore, undo and discard on the same engine as /recovery. Only
// the API calls them, after checking the admin's role, password and code.
type ConsoleHandler struct {
	Engine    *Engine
	Source    func(ctx context.Context, repoKey string) (Source, error)
	Snapshots func(ctx context.Context, src Source) ([]backup.Snapshot, error)
	Logger    *slog.Logger
}

func (h *ConsoleHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/restore", h.state)
	mux.HandleFunc("POST /internal/restore", h.start)
	mux.HandleFunc("POST /internal/restore/undo", h.undo)
	mux.HandleFunc("POST /internal/restore/discard", h.discard)
	return mux
}

func (h *ConsoleHandler) state(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo        string `json:"repo"`
		SnapshotID  string `json:"snapshotId"`
		RequestedBy string `json:"requestedBy"`
	}
	if err := decode(w, r, &body); err != nil || body.RequestedBy == "" {
		h.fail(w, errBadConsoleRequest)
		return
	}
	src, err := h.Source(r.Context(), body.Repo)
	if err != nil {
		h.fail(w, err)
		return
	}
	snaps, err := h.Snapshots(r.Context(), src)
	if err != nil {
		h.Logger.Error("recovery: console list snapshots", "source", src.ID, "error", err)
		h.fail(w, ErrRepositoryUnreadable)
		return
	}
	i := slices.IndexFunc(snaps, func(sn backup.Snapshot) bool { return sn.ID == body.SnapshotID })
	if i < 0 {
		h.fail(w, ErrSnapshotNotFound)
		return
	}
	st, err := h.Engine.Start(Request{
		Source: src, SnapshotID: snaps[i].ID, SnapshotTakenAt: snaps[i].Time.UTC(), RequestedBy: body.RequestedBy,
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	h.Logger.Info("recovery: console restore started", "by", body.RequestedBy, "source", src.ID, "snapshot", st.SnapshotID, "live", st.LiveState)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) undo(w http.ResponseWriter, r *http.Request) {
	by, ok := h.requester(w, r)
	if !ok {
		return
	}
	if _, err := h.Engine.Undo(UndoRequest{RequestedBy: by}); err != nil {
		h.fail(w, err)
		return
	}
	h.Logger.Info("recovery: console undo started", "by", by)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) discard(w http.ResponseWriter, r *http.Request) {
	by, ok := h.requester(w, r)
	if !ok {
		return
	}
	if _, err := h.Engine.Discard(by); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) requester(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		RequestedBy string `json:"requestedBy"`
	}
	if err := decode(w, r, &body); err != nil || body.RequestedBy == "" {
		h.fail(w, errBadConsoleRequest)
		return "", false
	}
	return body.RequestedBy, true
}

func (h *ConsoleHandler) fail(w http.ResponseWriter, err error) {
	for _, m := range consoleErrorCodes {
		if errors.Is(err, m.err) {
			writeError(w, m.status, m.code)
			return
		}
	}
	h.Logger.Error("recovery: console", "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}

// Client is the API's side of the worker's /internal/restore routes.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient targets the worker at baseURL. Starting a restore lists the
// snapshots of a possibly slow network drive first, hence the minute.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: time.Minute}}
}

func (c *Client) State(ctx context.Context) (*View, error) {
	return c.do(ctx, http.MethodGet, "/internal/restore", nil, http.StatusOK)
}

func (c *Client) Start(ctx context.Context, repoKey, snapshotID, requestedBy string) (*View, error) {
	return c.do(ctx, http.MethodPost, "/internal/restore",
		map[string]string{"repo": repoKey, "snapshotId": snapshotID, "requestedBy": requestedBy}, http.StatusAccepted)
}

func (c *Client) Undo(ctx context.Context, requestedBy string) (*View, error) {
	return c.do(ctx, http.MethodPost, "/internal/restore/undo", map[string]string{"requestedBy": requestedBy}, http.StatusAccepted)
}

func (c *Client) Discard(ctx context.Context, requestedBy string) (*View, error) {
	return c.do(ctx, http.MethodPost, "/internal/restore/discard", map[string]string{"requestedBy": requestedBy}, http.StatusOK)
}

func (c *Client) do(ctx context.Context, method, path string, body any, want int) (*View, error) {
	if c == nil {
		return nil, backup.ErrWorkerUnavailable
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", backup.ErrWorkerUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == want {
		var out struct {
			Restore *View `json:"restore"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return out.Restore, nil
	}
	var problem struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&problem)
	for _, m := range consoleErrorCodes {
		if m.code == problem.Error {
			return nil, m.err
		}
	}
	return nil, fmt.Errorf("recovery: worker restore: HTTP %d %s", resp.StatusCode, problem.Error)
}
