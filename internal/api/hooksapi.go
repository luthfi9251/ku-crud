package api

import (
	"errors"
	"net/http"
	"strconv"

	"ku-crud/internal/meta"
)

func (s *Server) handleHooksList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"hooks": s.hooks.Names()})
}

type outboxDTO struct {
	ID          string `json:"id"`
	TableDefID  string `json:"tableDefId"`
	Event       string `json:"event"`
	HookName    string `json:"hookName"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	NextRetryAt string `json:"nextRetryAt,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func (s *Server) handleOutboxList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	var defID int64
	if tok := q.Get("tableDefId"); tok != "" {
		var err error
		defID, err = s.ids.Decode("td", tok)
		if err != nil {
			writeErr(w, 400, "VALIDATION", "bad tableDefId", nil)
			return
		}
	}
	entries, total, err := s.store.ListOutbox(q.Get("status"), defID, 50, (page-1)*50)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "outbox query failed", nil)
		return
	}
	out := make([]outboxDTO, len(entries))
	for i, e := range entries {
		out[i] = outboxDTO{
			ID: s.ids.Encode("ob", e.ID), TableDefID: s.ids.Encode("td", e.TableDefID),
			Event: e.Event, HookName: e.HookName, Status: e.Status, Attempts: e.Attempts,
			NextRetryAt: e.NextRetryAt, LastError: e.LastError,
			CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		}
	}
	writeJSON(w, 200, map[string]any{"entries": out, "total": total, "page": page})
}

func (s *Server) handleOutboxRetry(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("ob", r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad outbox id", nil)
		return
	}
	if err := s.store.RetryOutbox(id); err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, 404, "NOT_FOUND", "outbox entry not found", nil)
			return
		}
		writeErr(w, 500, "INTERNAL", "retry failed", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
