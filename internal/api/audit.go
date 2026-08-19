package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"ku-crud/internal/meta"
)

type auditDTO struct {
	ID         string          `json:"id"`
	UserID     string          `json:"userId"`
	Username   string          `json:"username"`
	TableDefID string          `json:"tableDefId"`
	Action     string          `json:"action"`
	RowPK      string          `json:"rowPk"`
	OldValues  json.RawMessage `json:"oldValues"`
	NewValues  json.RawMessage `json:"newValues"`
	CreatedAt  string          `json:"createdAt"`
}

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := meta.AuditFilter{}
	if tok := q.Get("tableDefId"); tok != "" {
		v, err := s.ids.Decode("td", tok)
		if err != nil {
			writeErr(w, 400, "VALIDATION", "bad tableDefId", nil)
			return
		}
		f.TableDefID = v
	}
	if tok := q.Get("userId"); tok != "" {
		v, err := s.ids.Decode("user", tok)
		if err != nil {
			writeErr(w, 400, "VALIDATION", "bad userId", nil)
			return
		}
		f.UserID = v
	}
	f.Action = q.Get("action")
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	f.Limit, f.Offset = 50, (page-1)*50
	entries, total, err := s.store.ListAudit(f)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := make([]auditDTO, len(entries))
	for i, e := range entries {
		out[i] = auditDTO{
			ID:         s.ids.Encode("audit", e.ID),
			UserID:     s.ids.Encode("user", e.UserID),
			Username:   e.Username,
			TableDefID: s.ids.Encode("td", e.TableDefID),
			Action:     e.Action,
			RowPK:      e.RowPK,
			OldValues:  e.OldValues,
			NewValues:  e.NewValues,
			CreatedAt:  e.CreatedAt,
		}
	}
	writeJSON(w, 200, map[string]any{"entries": out, "total": total, "page": page, "pageSize": f.Limit})
}
