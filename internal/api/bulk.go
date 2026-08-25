package api

import (
	"net/http"

	"ku-crud/internal/engine"
	"ku-crud/internal/hooks"
)

// bulkDeleteCap bounds one bulk-delete request; clients chunk beyond this.
const bulkDeleteCap = 1000

type bulkFailure struct {
	Key     string `json:"key"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

// handleRowBulkDelete deletes many rows by encoded keys. Partial success by
// design: each key is conflict-checked and audited independently, mirroring
// the single-row delete path.
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
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if len(body.Keys) == 0 {
		writeErr(w, 400, "VALIDATION", "keys is required", nil)
		return
	}
	if len(body.Keys) > bulkDeleteCap {
		writeErr(w, 400, "BULK_TOO_LARGE", "bulk delete is limited to 1000 keys per request", nil)
		return
	}
	if err := s.hookGuard(def); err != nil {
		writeHookErr(w, err)
		return
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer a.Close()

	deleted := 0
	var failures []bulkFailure
	var deletedOlds []map[string]any
	seen := map[string]bool{}
	ct := toCore(def, cols)
	for _, key := range body.Keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		pkVals, err := engine.DecodeKey(ct, key)
		if err != nil {
			failures = append(failures, bulkFailure{Key: key, Code: "VALIDATION", Message: "bad row key"})
			continue
		}
		oldRows, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals, realColNames(cols))
		if err != nil {
			failures = append(failures, bulkFailure{Key: key, Code: "CONN", Message: "fetch failed"})
			continue
		}
		if len(oldRows) == 0 {
			failures = append(failures, bulkFailure{Key: key, Code: "NOT_FOUND", Message: "row not found"})
			continue
		}
		var conflicts []map[string]any
		for _, old := range oldRows {
			cs, err := s.referencedBy(def, old)
			if err != nil {
				failures = append(failures, bulkFailure{Key: key, Code: "CONN", Message: "reference check failed"})
				conflicts = nil
				break
			}
			conflicts = mergeConflicts(conflicts, cs)
		}
		if conflicts != nil {
			failures = append(failures, bulkFailure{Key: key, Code: "CONFLICT",
				Message: "row is referenced by other rows", Detail: conflicts})
			continue
		}
		blocked := false
		for _, old := range oldRows {
			if _, err := s.runBefore(r.Context(), u, def, cols, hooks.BeforeDelete, nil, old); err != nil {
				failures = append(failures, bulkFailure{Key: key, Code: "HOOK_REJECTED", Message: err.Error()})
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if _, err := a.DeleteByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals); err != nil {
			code, msg := "CONN", "delete failed"
			if a.IsFKViolation(err) {
				code, msg = "CONFLICT", "row is referenced by other rows (database constraint)"
			}
			failures = append(failures, bulkFailure{Key: key, Code: code, Message: msg})
			continue
		}
		for _, old := range oldRows {
			rowPK, _ := engine.EncodeKey(ct, old)
			s.auditBestEffort(u, def.ID, "DELETE", rowPK, old, nil)
			deletedOlds = append(deletedOlds, old)
		}
		deleted++
	}
	for _, old := range deletedOlds {
		s.enqueueAfter(u, def, hooks.AfterDelete, old, nil)
	}
	if failures == nil {
		failures = []bulkFailure{}
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted, "failed": len(failures), "failures": failures})
}
