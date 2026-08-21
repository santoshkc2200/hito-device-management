package apiserver

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

// ListAuditEvents returns recent audit events matching query filters with keyset pagination.
func (s *Server) ListAuditEvents(w http.ResponseWriter, r *http.Request, params gen.ListAuditEventsParams) {
	limit := listing.DefaultLimit
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
		if limit > listing.MaxLimit {
			limit = listing.MaxLimit
		}
	}

	var cursorAt time.Time
	var cursorID string
	if params.Cursor != nil && *params.Cursor != "" {
		c, err := listing.DecodeCursor(*params.Cursor, "at")
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}
		if c != nil {
			tVal, tErr := c.TimeVal()
			if tErr != nil {
				s.writeServiceError(w, r, listing.ErrInvalidCursor)
				return
			}
			if tVal != nil {
				cursorAt = *tVal
			}
			cursorID = c.ID
		}
	}

	var fromTime, toTime time.Time
	if params.From != nil {
		fromTime = *params.From
	}
	if params.To != nil {
		toTime = *params.To
	}

	listParams := auditapi.ListParams{
		Actor:    fromPtr(params.Actor),
		Action:   fromPtr(params.Action),
		Subject:  fromPtr(params.Subject),
		From:     fromTime,
		To:       toTime,
		CursorAt: cursorAt,
		CursorID: cursorID,
		Limit:    limit + 1,
	}

	entries, err := s.audit.List(r.Context(), listParams)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var nextCursor *string
	if len(entries) > limit {
		last := entries[limit-1]
		cStr := listing.EncodeTimeCursor("at", last.At, last.ID)
		nextCursor = &cStr
		entries = entries[:limit]
	}

	items := make([]gen.AuditEvent, 0, len(entries))
	for _, e := range entries {
		var actorIPPtr, reqIDPtr *string
		if e.ActorIP != "" {
			aCopy := e.ActorIP
			actorIPPtr = &aCopy
		}
		if e.RequestID != "" {
			rCopy := e.RequestID
			reqIDPtr = &rCopy
		}

		items = append(items, gen.AuditEvent{
			Id:        e.ID,
			At:        e.At,
			Actor:     e.Actor,
			ActorIp:   actorIPPtr,
			Action:    e.Action,
			Subject:   e.Subject,
			Payload:   e.Payload,
			RequestId: reqIDPtr,
		})
	}

	writeJSON(w, http.StatusOK, gen.AuditEventList{
		Items:      items,
		NextCursor: nextCursor,
	})
}

// ExportAuditCsv streams CSV data for audit events matching filters.
func (s *Server) ExportAuditCsv(w http.ResponseWriter, r *http.Request, params gen.ExportAuditCsvParams) {
	var fromTime, toTime time.Time
	if params.From != nil {
		fromTime = *params.From
	}
	if params.To != nil {
		toTime = *params.To
	}

	listParams := auditapi.ListParams{
		Actor:   fromPtr(params.Actor),
		Action:  fromPtr(params.Action),
		Subject: fromPtr(params.Subject),
		From:    fromTime,
		To:      toTime,
	}

	entries, err := s.audit.StreamForExport(r.Context(), listParams)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	filename := fmt.Sprintf("audit-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Write UTF-8 BOM
	_, _ = w.Write([]byte("\xef\xbb\xbf"))

	csvWriter := csv.NewWriter(w)
	_ = csvWriter.Write([]string{
		"id", "at", "actor", "actor_ip", "action", "subject", "payload", "request_id",
	})
	csvWriter.Flush()

	flusher, _ := w.(http.Flusher)

	for i, e := range entries {
		payloadBytes, _ := json.Marshal(e.Payload)

		_ = csvWriter.Write([]string{
			e.ID,
			e.At.UTC().Format(time.RFC3339),
			e.Actor,
			e.ActorIP,
			e.Action,
			e.Subject,
			string(payloadBytes),
			e.RequestID,
		})

		if i%100 == 0 && flusher != nil {
			csvWriter.Flush()
			flusher.Flush()
		}
	}
	csvWriter.Flush()
	if flusher != nil {
		flusher.Flush()
	}
}
