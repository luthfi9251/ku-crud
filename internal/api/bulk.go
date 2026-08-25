package api

import (
	"net/http"
)

// handleRowBulkDelete deletes many rows by encoded keys. Partial success
// by design: each key is conflict-checked and hook-checked independently,
// mirroring the single-row delete path (engine.WriteService.BulkDelete).
func (s *Server) handleRowBulkDelete(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "delete") {
		writeErr(w, 403, "FORBIDDEN", "no delete access to this table", nil)
		return
	}
	svc, ct := s.writeService(u, r, def, cols)
	svc.BulkDelete(w, r, ct)
}
