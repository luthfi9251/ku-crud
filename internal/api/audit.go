package api

import (
	"net/http"
	"strconv"

	"ku-crud/internal/meta"
)

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := meta.AuditFilter{}
	if v, err := strconv.ParseInt(q.Get("tableDefId"), 10, 64); err == nil {
		f.TableDefID = v
	}
	if v, err := strconv.ParseInt(q.Get("userId"), 10, 64); err == nil {
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
	writeJSON(w, 200, map[string]any{"entries": entries, "total": total, "page": page, "pageSize": f.Limit})
}
