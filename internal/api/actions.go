package api

import (
	"errors"
	"net/http"

	"github.com/luthfi9251/ku-crud/core/engine"
	"github.com/luthfi9251/ku-crud/core/hooks"
	"ku-crud/internal/meta"
)

// runRowAction runs one custom row action synchronously (v1.9):
// POST /api/data/{name}/rows/{pk}/action  body {"actionId":"..."}
// The row is fetched fresh from the database (never trusted from the
// client), the acting user must hold the action's grant, and every run —
// success or failure — writes an ACTION audit entry. The def arrives
// already resolved by name (the /api/data dispatcher).
func (s *Server) runRowAction(w http.ResponseWriter, r *http.Request, def *meta.TableDef, cols []meta.ColumnDef) {
	u := userFrom(r)
	if writeQueryReadOnly(w, def) {
		return
	}
	var in struct {
		ActionID string `json:"actionId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	cfg, err := hooks.ParseActions(def.Actions)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	act := cfg.Find(in.ActionID)
	if act == nil {
		writeErr(w, 404, "ACTION_NOT_FOUND", "unknown action", nil)
		return
	}
	if !s.hasTablePerm(u, def.ID, act.Grant) {
		writeErr(w, 403, "FORBIDDEN", "no "+act.Grant+" access to this table", nil)
		return
	}
	keyVals, err := engine.DecodeKey(toCore(def, cols), r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer a.Close()
	rowsOut, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, keyVals, realColNames(cols))
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	if len(rowsOut) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	row := rowsOut[0]
	res := s.metaRes(def)
	ct := meta.ToCoreDef(*def, cols, res.idToName)
	msg, err := s.hooks.RunAction(hooks.WithActor(r.Context(), u.Username),
		s.hookCtx(u.Username, res, &ct),
		hooks.RowPayload{Values: row},
		hooks.Assignment{Hook: act.Hook, Config: act.Config, Order: act.Order})
	status := "ok"
	if err != nil {
		status = "error"
	}
	rowPK, _ := engine.EncodeKey(toCore(def, cols), row)
	s.auditBestEffort(u, def.ID, "ACTION", rowPK, nil, map[string]any{
		"actionId": act.ID, "label": act.Label, "message": msg, "status": status,
	})
	if err != nil {
		var me *hooks.MissingError
		if errors.As(err, &me) {
			writeErr(w, 400, "HOOK_MISSING", err.Error(), nil)
			return
		}
		writeErr(w, 400, "ACTION_FAILED", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]any{"message": msg})
}
