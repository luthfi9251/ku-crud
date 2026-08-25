package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"ku-crud/internal/defs"
	"ku-crud/internal/meta"
)

func colNames(cols []meta.ColumnDef) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

// realCols drops virtual columns (m2m relations and computed) — they have no
// live counterpart and must never reach SQL SELECT lists.
func realCols(cols []meta.ColumnDef) []meta.ColumnDef {
	out := make([]meta.ColumnDef, 0, len(cols))
	for _, c := range cols {
		if c.FieldType != "m2m" && !c.IsComputed {
			out = append(out, c)
		}
	}
	return out
}

func realColNames(cols []meta.ColumnDef) []string { return colNames(realCols(cols)) }

// toCore converts a persisted definition to the ID-free core contract for
// the pure engine helpers. FK/M2M name resolution needs an id→name map,
// which the helpers used here don't require (joins resolve in api).
func toCore(def *meta.TableDef, cols []meta.ColumnDef) *defs.Table {
	t := meta.ToCoreDef(*def, cols, nil)
	return &t
}

func (s *Server) handleRowList(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	u := userFrom(r)
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	svc, ct := s.readService(u, def, cols)
	svc.List(w, r, ct)
}

func (s *Server) handleRowGet(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	// perm check before the engine's QUERY_NO_KEY so no-grant users can't
	// probe whether a query view has key columns
	u := userFrom(r)
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	svc, ct := s.readService(u, def, cols)
	svc.Get(w, r, ct)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// ponytail: best-effort audit — action succeeds even if this fails
func (s *Server) auditBestEffort(u CtxUser, defID int64, action, rowPK string, oldV, newV any) {
	var oldB, newB []byte
	if oldV != nil {
		oldB = mustJSON(oldV)
	}
	if newV != nil {
		newB = mustJSON(newV)
	}
	if err := s.store.InsertAudit(&meta.AuditEntry{
		UserID: u.ID, TableDefID: defID, Action: action, RowPK: rowPK,
		OldValues: oldB, NewValues: newB,
	}); err != nil {
		slog.Warn("audit write failed", "def", defID, "action", action, "rowPk", rowPK, "err", err.Error())
	}
}

func (s *Server) handleRowCreate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "create") {
		writeErr(w, 403, "FORBIDDEN", "no create access to this table", nil)
		return
	}
	svc, ct := s.writeService(u, r, def, cols)
	svc.Insert(w, r, ct)
}

func (s *Server) handleRowUpdate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "update") {
		writeErr(w, 403, "FORBIDDEN", "no update access to this table", nil)
		return
	}
	svc, ct := s.writeService(u, r, def, cols)
	svc.Update(w, r, ct)
}

func (s *Server) handleRowDelete(w http.ResponseWriter, r *http.Request) {
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
	svc.Delete(w, r, ct)
}
