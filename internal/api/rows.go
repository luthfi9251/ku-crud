package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

func colNames(cols []meta.ColumnDef) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

// resolveSort picks the effective sort for a list query: an explicit sortable
// column from the request wins; otherwise the definition's default sort when
// it is still a defined, sortable column; otherwise the first key column ASC.
func resolveSort(def *meta.TableDef, cols []meta.ColumnDef, sortCol, sortDir string) (string, string) {
	byName := map[string]meta.ColumnDef{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if byName[sortCol].Sortable {
		if sortDir != "ASC" && sortDir != "DESC" {
			sortDir = "ASC"
		}
		return sortCol, sortDir
	}
	if c, ok := byName[def.DefaultSortCol]; ok && c.Sortable {
		d := def.DefaultSortDir
		if d != "ASC" && d != "DESC" {
			d = "ASC"
		}
		return def.DefaultSortCol, d
	}
	return def.KeyColumns[0], "ASC"
}

func (s *Server) handleRowList(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if !s.hasTablePerm(userFrom(r), def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer a.Close()

	var searchable []string
	for _, c := range cols {
		if c.Searchable {
			searchable = append(searchable, c.Name)
		}
	}

	q := r.URL.Query()
	sortCol, sortDir := resolveSort(def, cols, q.Get("sort"), q.Get("dir"))
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	names := colNames(cols)
	lp := ds.ListParams{Schema: def.SchemaName, Table: def.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: sortDir,
		Limit: def.PageSize, Offset: (page - 1) * def.PageSize}

	rows, err := a.ListRows(lp)
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	total, err := a.CountRows(lp)
	if err != nil {
		writeErr(w, 502, "CONN", "count failed", err.Error())
		return
	}
	rels := s.buildRels(userFrom(r), cols, rows)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "total": total, "page": page,
		"pageSize": def.PageSize, "rels": rels})
}

func (s *Server) handleRowGet(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if !s.hasTablePerm(userFrom(r), def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer a.Close()

	names := colNames(cols)
	keyVals, err := rowKeyVals(def, cols, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	rowsOut, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, keyVals, names)
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	if len(rowsOut) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	row := rowsOut[0]
	rels := s.buildRels(userFrom(r), cols, []map[string]any{row})
	writeJSON(w, 200, map[string]any{"row": row, "rels": rels})
}

// editablePayload validates body against editable columns and returns
// (cols, vals) in column-definition order. requireAll=true enforces required
// columns for INSERT / UPDATE. Any non-editable/unknown key is rejected unless it is a primary key during insert.
func editablePayload(body map[string]any, cols []meta.ColumnDef, keyCols []string, isInsert bool) ([]string, []any, error) {
	editable := map[string]meta.ColumnDef{}
	isKey := map[string]bool{}
	for _, k := range keyCols {
		isKey[k] = true
	}

	for _, c := range cols {
		if c.Editable || (isInsert && isKey[c.Name]) {
			editable[c.Name] = c
		}
	}
	for k := range body {
		if _, ok := editable[k]; !ok {
			return nil, nil, fmt.Errorf("column %q is not editable/known", k)
		}
	}
	var names []string
	var vals []any
	for _, c := range cols {
		if !c.Editable && !(isInsert && isKey[c.Name]) {
			continue
		}
		v, present := body[c.Name]
		if present {
			ft := c.FieldType
			if ft == "fk" {
				ft = c.BaseType
			}
			if err := validateValue(ft, v, c.EnumOptions); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", c.Name, err)
			}
			if c.FieldType == "json" {
				s, err := normalizeJSONValue(v)
				if err != nil {
					return nil, nil, fmt.Errorf("%s: %w", c.Name, err)
				}
				v = s
			}
			names = append(names, c.Name)
			vals = append(vals, v)
		} else if isInsert && c.Required && !isKey[c.Name] {
			return nil, nil, fmt.Errorf("%s is required", c.Name)
		}
	}
	return names, vals, nil
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
	if !s.hasTablePerm(u, def.ID, "create") {
		writeErr(w, 403, "FORBIDDEN", "no create access to this table", nil)
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	names, vals, err := editablePayload(body, cols, def.KeyColumns, true)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer a.Close()
	if err := s.checkFKValues(cols, names, vals); err != nil {
		if errors.Is(err, errFKRefNotFound) {
			writeErr(w, 400, "VALIDATION", err.Error(), nil)
			return
		}
		writeErr(w, 502, "CONN", "reference check failed", err.Error())
		return
	}
	if err := a.Insert(def.SchemaName, def.TableName, names, vals); err != nil {
		if a.IsFKViolation(err) {
			writeErr(w, 409, "CONFLICT", "row is referenced by other rows (database constraint)", nil)
			return
		}
		writeErr(w, 502, "CONN", "insert failed", err.Error())
		return
	}
	s.auditBestEffort(u, def.ID, "INSERT", "", nil, body)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleRowUpdate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if !s.hasTablePerm(u, def.ID, "update") {
		writeErr(w, 403, "FORBIDDEN", "no update access to this table", nil)
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	names, vals, err := editablePayload(body, cols, def.KeyColumns, false)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	pkVals, err := rowKeyVals(def, cols, r.PathValue("pk"))
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

	if err := s.checkFKValues(cols, names, vals); err != nil {
		if errors.Is(err, errFKRefNotFound) {
			writeErr(w, 400, "VALIDATION", err.Error(), nil)
			return
		}
		writeErr(w, 502, "CONN", "reference check failed", err.Error())
		return
	}

	oldRows, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals, colNames(cols))
	if err != nil {
		writeErr(w, 502, "CONN", "fetch old failed", err.Error())
		return
	}
	if len(oldRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}

	if _, err := a.UpdateByKey(def.SchemaName, def.TableName, names, vals, def.KeyColumns, pkVals); err != nil {
		if a.IsFKViolation(err) {
			writeErr(w, 409, "CONFLICT", "row is referenced by other rows (database constraint)", nil)
			return
		}
		writeErr(w, 502, "CONN", "update failed", err.Error())
		return
	}
	// ponytail: per-row new = old merged with payload (computed cols not re-read)
	for _, old := range oldRows {
		merged := map[string]any{}
		for k, v := range old {
			merged[k] = v
		}
		for k, v := range body {
			merged[k] = v
		}
		s.auditBestEffort(u, def.ID, "UPDATE", rowKeyString(def, old), old, merged)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "affected": len(oldRows)})
}

func (s *Server) handleRowDelete(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if !s.hasTablePerm(u, def.ID, "delete") {
		writeErr(w, 403, "FORBIDDEN", "no delete access to this table", nil)
		return
	}
	pkVals, err := rowKeyVals(def, cols, r.PathValue("pk"))
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

	oldRows, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals, colNames(cols))
	if err != nil {
		writeErr(w, 502, "CONN", "fetch old failed", err.Error())
		return
	}
	if len(oldRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	var conflicts []map[string]any
	for _, old := range oldRows {
		cs, err := s.referencedBy(def, old)
		if err != nil {
			writeErr(w, 502, "CONN", "reference check failed", err.Error())
			return
		}
		conflicts = mergeConflicts(conflicts, cs)
	}
	if len(conflicts) > 0 {
		writeErr(w, 409, "CONFLICT", "row is referenced by other rows", conflicts)
		return
	}
	if _, err := a.DeleteByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals); err != nil {
		if a.IsFKViolation(err) {
			writeErr(w, 409, "CONFLICT", "row is referenced by other rows (database constraint)", nil)
			return
		}
		writeErr(w, 502, "CONN", "delete failed", err.Error())
		return
	}
	for _, old := range oldRows {
		s.auditBestEffort(u, def.ID, "DELETE", rowKeyString(def, old), old, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "affected": len(oldRows)})
}

// errFKRefNotFound marks an fk payload value that has no matching target row.
var errFKRefNotFound = errors.New("referenced row not found")

// checkFKValues verifies each non-null fk payload value exists on the target
// table (batched per fk column via the ref-value IN query).
func (s *Server) checkFKValues(cols []meta.ColumnDef, names []string, vals []any) error {
	for i, name := range names {
		var c *meta.ColumnDef
		for j := range cols {
			if cols[j].Name == name && cols[j].FieldType == "fk" {
				c = &cols[j]
				break
			}
		}
		if c == nil || vals[i] == nil {
			continue
		}
		target, _, err := s.store.GetTableDef(c.FKTableDefID)
		if err != nil {
			return fmt.Errorf("%s: fk target unavailable", name)
		}
		a, err := s.liveAdapter(target.DatasourceID)
		if err != nil {
			return err
		}
		m, err := a.FetchByRefValues(target.SchemaName, target.TableName, c.FKRefColumn, nil, []any{vals[i]})
		a.Close()
		if err != nil {
			return err
		}
		if len(m) == 0 {
			return fmt.Errorf("%s: %w", name, errFKRefNotFound)
		}
	}
	return nil
}

// referencedBy reports defined tables whose fk columns point at def and hold
// rows referencing the given old row. Detail rows feed the 409 body.
func (s *Server) referencedBy(def *meta.TableDef, old map[string]any) ([]map[string]any, error) {
	srcs, err := s.store.FKRefSources(def.ID)
	if err != nil {
		return nil, err
	}
	var conflicts []map[string]any
	for _, src := range srcs {
		refVal := old[src.RefColumn]
		if refVal == nil {
			continue
		}
		srcDef, _, err := s.store.GetTableDef(src.DefID)
		if err != nil {
			return nil, err
		}
		a, err := s.liveAdapter(srcDef.DatasourceID)
		if err != nil {
			return nil, err
		}
		n, err := a.CountByRefEq(srcDef.SchemaName, srcDef.TableName, src.Column, refVal)
		a.Close()
		if err != nil {
			return nil, err
		}
		if n > 0 {
			conflicts = append(conflicts, map[string]any{"table": src.DefLabel, "column": src.Column, "count": n})
		}
	}
	return conflicts, nil
}

// mergeConflicts unions two delete-protection conflict lists, summing count
// for entries sharing the same table+column pair.
func mergeConflicts(existing []map[string]any, add []map[string]any) []map[string]any {
	for _, c := range add {
		merged := false
		for _, e := range existing {
			if e["table"] == c["table"] && e["column"] == c["column"] {
				e["count"] = e["count"].(int) + c["count"].(int)
				merged = true
				break
			}
		}
		if !merged {
			existing = append(existing, c)
		}
	}
	return existing
}
