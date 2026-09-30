package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrWorkerUnavailable means the API could not reach the worker's internal
// listener. The console says "Backup worker not responding".
var ErrWorkerUnavailable = errors.New("backup: worker not reachable")

var errBadRequest = errors.New("backup: bad request")

// maxLocationBody bounds a request body; a path or folder name is far smaller.
const maxLocationBody = 4096

// Wire codes for errors on the internal routes; the client maps them back.
var locationErrorCodes = []struct {
	err    error
	status int
	code   string
}{
	{ErrPathNotAllowed, http.StatusUnprocessableEntity, CodeOutsideRoots},
	{ErrLocationNotFound, http.StatusNotFound, CodeNotFound},
	{ErrLocationNotWritable, http.StatusUnprocessableEntity, CodeNotWritable},
	{ErrInvalidFolderName, http.StatusUnprocessableEntity, "invalid_name"},
	{ErrFolderExists, http.StatusConflict, "folder_exists"},
	{errBadRequest, http.StatusBadRequest, "bad_request"},
}

// InternalHandler serves the worker's /internal/locations routes. Only the API
// calls them, over the compose network; caddy never routes /internal.
func (l *Locator) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/locations", func(w http.ResponseWriter, r *http.Request) {
		listing, err := l.Browse(r.URL.Query().Get("path"))
		if err != nil {
			writeLocationError(w, err)
			return
		}
		writeLocationJSON(w, http.StatusOK, listing)
	})
	mux.HandleFunc("POST /internal/locations/folders", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Parent string `json:"parent"`
			Name   string `json:"name"`
		}
		if err := decodeLocationBody(w, r, &body); err != nil {
			writeLocationError(w, err)
			return
		}
		f, err := l.CreateFolder(body.Parent, body.Name)
		if err != nil {
			writeLocationError(w, err)
			return
		}
		writeLocationJSON(w, http.StatusCreated, f)
	})
	mux.HandleFunc("POST /internal/locations/check", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := decodeLocationBody(w, r, &body); err != nil {
			writeLocationError(w, err)
			return
		}
		writeLocationJSON(w, http.StatusOK, l.Check(body.Path))
	})
	return mux
}

func decodeLocationBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxLocationBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", errBadRequest, err)
	}
	return nil
}

func writeLocationError(w http.ResponseWriter, err error) {
	for _, m := range locationErrorCodes {
		if errors.Is(err, m.err) {
			writeLocationJSON(w, m.status, map[string]string{"error": m.code})
			return
		}
	}
	slog.Error("worker: locations", "error", err)
	writeLocationJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

func writeLocationJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// LocationClient is the API's side of the worker's internal location routes.
type LocationClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewLocationClient targets the worker at baseURL. The timeout covers a check
// on a slow network share, which walks the local repository and probes a write.
func NewLocationClient(baseURL string) *LocationClient {
	return &LocationClient{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *LocationClient) Browse(ctx context.Context, path string) (LocationListing, error) {
	var out LocationListing
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	err := c.do(ctx, http.MethodGet, "/internal/locations?"+q.Encode(), nil, http.StatusOK, &out)
	return out, err
}

func (c *LocationClient) CreateFolder(ctx context.Context, parent, name string) (Folder, error) {
	var out Folder
	err := c.do(ctx, http.MethodPost, "/internal/locations/folders", map[string]string{"parent": parent, "name": name}, http.StatusCreated, &out)
	return out, err
}

func (c *LocationClient) Check(ctx context.Context, path string) (LocationCheck, error) {
	var out LocationCheck
	err := c.do(ctx, http.MethodPost, "/internal/locations/check", map[string]string{"path": path}, http.StatusOK, &out)
	return out, err
}

func (c *LocationClient) do(ctx context.Context, method, pathAndQuery string, body any, want int, out any) error {
	if c == nil {
		return ErrWorkerUnavailable
	}
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+pathAndQuery, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == want {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var problem struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&problem)
	for _, m := range locationErrorCodes {
		if m.code == problem.Error {
			return m.err
		}
	}
	return fmt.Errorf("backup: worker locations: HTTP %d %s", resp.StatusCode, problem.Error)
}
