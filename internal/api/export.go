package api

import (
	"net/http"
)

// handleRowExport streams the full result set matching the active
// search/sort as a CSV download (all pages, not just the current one).
func (s *Server) handleRowExport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	svc, ct := s.readService(u, def, cols)
	svc.ExportCSV(w, r, ct)
}
