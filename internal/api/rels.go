package api

import (
	"net/http"
)

// The relation endpoints (fk picker, m2m picker, m2m links) render in the
// engine (internal/engine/rels.go, ReadService.FKOptions/M2MOptions/
// M2MLinks). The api keeps the def lookup, the query-view write guard and
// the per-table read grant; grants on the related tables ride on the
// ReadService.CanRead callback wired in defresolver.go.

func (s *Server) handleM2MLinks(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	svc, ct := s.readService(u, def, cols)
	svc.M2MLinks(w, r, ct)
}

// handleM2MOptions lists target-table rows for the m2m picker (mirror of
// handleFKOptions). Requires read on source (checked here), junction and
// target (checked via ReadService.CanRead).
func (s *Server) handleM2MOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	svc, ct := s.readService(u, def, cols)
	svc.M2MOptions(w, r, ct)
}

// handleFKOptions lists target-table rows for the fk picker modal.
// Requires read on source (checked here) and on the fk target (checked
// via ReadService.CanRead).
func (s *Server) handleFKOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	svc, ct := s.readService(u, def, cols)
	svc.FKOptions(w, r, ct)
}
