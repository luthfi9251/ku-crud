package api

import (
	"net/http"

	"ku-crud/internal/meta"
)

// importCtx loads def/cols and enforces the create grant.
func (s *Server) importCtx(w http.ResponseWriter, r *http.Request) (*meta.TableDef, []meta.ColumnDef, bool) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return nil, nil, false
	}
	if writeQueryReadOnly(w, def) {
		return nil, nil, false
	}
	if !s.hasTablePerm(u, def.ID, "create") {
		writeErr(w, 403, "FORBIDDEN", "no create access to this table", nil)
		return nil, nil, false
	}
	return def, cols, true
}

// The CSV import pipeline (multipart parse, per-row validation, fk checks,
// hook execution, insert loop) lives in the engine (internal/engine/
// importcsv.go, ImportService). The api keeps the def lookup, the
// query-view write guard and the create grant.
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	def, cols, ok := s.importCtx(w, r)
	if !ok {
		return
	}
	svc, ct := s.importService(r, def, cols)
	svc.PreviewImport(w, r, ct)
}

func (s *Server) handleImportApply(w http.ResponseWriter, r *http.Request) {
	def, cols, ok := s.importCtx(w, r)
	if !ok {
		return
	}
	svc, ct := s.importService(r, def, cols)
	svc.ApplyImport(w, r, ct)
}
