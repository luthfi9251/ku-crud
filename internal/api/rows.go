package api

import (
	"encoding/json"
	"log/slog"

	"github.com/luthfi9251/ku-crud/core/defs"
	"ku-crud/internal/meta"
)

// Row endpoints are served by core/httpapi under /api/data/{name}
// (defsource.go). This file keeps the def/audit helpers the remaining
// platform handlers (actions, groups, metatransfer, saved filters) share.

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
