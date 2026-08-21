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

// realCols drops virtual m2m columns — they have no live counterpart and
// must never reach SQL SELECT lists.
func realCols(cols []meta.ColumnDef) []meta.ColumnDef {
	out := make([]meta.ColumnDef, 0, len(cols))
	for _, c := range cols {
		if c.FieldType != "m2m" {
			out = append(out, c)
		}
	}
	return out
}

func realColNames(cols []meta.ColumnDef) []string { return colNames(realCols(cols)) }

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
	u := userFrom(r)
	if !s.hasTablePerm(u, def.ID, "read") {
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
	filters, fmsg := s.parseFilters(def, cols, u, q.Get("filters"))
	if fmsg != "" {
		writeErr(w, 400, "FILTER_INVALID", fmsg, nil)
		return
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	names := realColNames(cols)
	lp := ds.ListParams{Schema: def.SchemaName, Table: def.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: sortDir,
		Filters: filters,
		Limit:   def.PageSize, Offset: (page - 1) * def.PageSize}

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
	rels := s.buildRels(u, cols, rows)
	m2mRels := s.buildM2MRels(u, def, cols, rows)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "total": total, "page": page,
		"pageSize": def.PageSize, "rels": rels, "m2mRels": m2mRels})
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

	names := realColNames(cols)
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
		if c.FieldType == "m2m" {
			continue // virtual relation column — handled by syncM2MLinks
		}
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
		if c.FieldType == "m2m" {
			continue // virtual relation column — handled by syncM2MLinks
		}
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
			if err := applyColumnValidations(c, v); err != nil {
				return nil, nil, err
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
	selections, err := stripM2MPayload(cols, body)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if err := s.precheckM2M(u, def, cols, selections); err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 500, "INTERNAL", "relation check failed", nil)
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
	if err := s.applyM2MPayload(u, def, cols, body, nil, selections); err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 502, "CONN", "relation sync failed", err.Error())
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
	selections, err := stripM2MPayload(cols, body)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if err := s.precheckM2M(u, def, cols, selections); err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 500, "INTERNAL", "relation check failed", nil)
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

	oldRows, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals, realColNames(cols))
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
	if err := s.applyM2MPayload(u, def, cols, body, oldRows[0], selections); err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 502, "CONN", "relation sync failed", err.Error())
		return
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

	oldRows, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals, realColNames(cols))
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

// m2mSelection extracts []any target ref values from a request body value.
func m2mSelection(v any) ([]any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case []any:
		return t, nil
	default:
		return nil, fmt.Errorf("many-to-many selection must be an array of target values")
	}
}

// syncM2MLinks diffs the wanted target values against the current junction
// rows for one source value and applies inserts/deletes. Requires create and
// delete grants on the junction definition; audits every link change.
func (s *Server) syncM2MLinks(u CtxUser, def *meta.TableDef, c meta.ColumnDef, srcVal any, want []any) error {
	if srcVal == nil {
		return newAPIErr(400, "VALIDATION",
			"column "+c.Name+": cannot manage relations without a source key value")
	}
	cfg, msg := s.resolveM2M(def, c)
	if cfg == nil {
		return newAPIErr(400, "VALIDATION", msg)
	}
	if !s.hasTablePerm(u, cfg.Junction.ID, "create") || !s.hasTablePerm(u, cfg.Junction.ID, "delete") {
		return newAPIErr(403, "FORBIDDEN", "managing "+cfg.Junction.Label+
			" relations requires create and delete grants on that table")
	}
	ja, err := s.liveAdapter(cfg.Junction.DatasourceID)
	if err != nil {
		return err
	}
	defer ja.Close()
	pairs, err := ja.FetchPairsByRef(cfg.Junction.SchemaName, cfg.Junction.TableName,
		c.M2MJunctionSrcCol, c.M2MJunctionTgtCol, []any{srcVal})
	if err != nil {
		return err
	}
	current := map[string]any{}
	for _, p := range pairs {
		if p.Ret != nil {
			current[rowValKey(p.Ret)] = p.Ret
		}
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[rowValKey(w)] = true
	}
	for _, w := range want { // added links
		if _, exists := current[rowValKey(w)]; exists {
			continue
		}
		if err := ja.Insert(cfg.Junction.SchemaName, cfg.Junction.TableName,
			[]string{c.M2MJunctionSrcCol, c.M2MJunctionTgtCol}, []any{srcVal, w}); err != nil {
			return err
		}
		s.auditBestEffort(u, cfg.Junction.ID, "INSERT", "", nil,
			map[string]any{c.M2MJunctionSrcCol: srcVal, c.M2MJunctionTgtCol: w})
	}
	for k, v := range current { // removed links
		if wantSet[k] {
			continue
		}
		if _, err := ja.DeletePairs(cfg.Junction.SchemaName, cfg.Junction.TableName,
			c.M2MJunctionSrcCol, srcVal, c.M2MJunctionTgtCol, v); err != nil {
			return err
		}
		s.auditBestEffort(u, cfg.Junction.ID, "DELETE", "",
			map[string]any{c.M2MJunctionSrcCol: srcVal, c.M2MJunctionTgtCol: v}, nil)
	}
	return nil
}

// stripM2MPayload removes m2m selections from the body (they must never
// reach editablePayload) and returns them per column.
func stripM2MPayload(cols []meta.ColumnDef, body map[string]any) (map[string][]any, error) {
	var out map[string][]any
	for _, c := range cols {
		if c.FieldType != "m2m" {
			continue
		}
		v, present := body[c.Name]
		if !present {
			continue
		}
		delete(body, c.Name)
		want, err := m2mSelection(v)
		if err != nil {
			return nil, newAPIErr(400, "VALIDATION", "column "+c.Name+": "+err.Error())
		}
		if out == nil {
			out = map[string][]any{}
		}
		out[c.Name] = want
	}
	return out, nil
}

// precheckM2M validates configs and junction grants before any parent write,
// so permission failures reject the whole request atomically.
func (s *Server) precheckM2M(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef, selections map[string][]any) error {
	for _, c := range cols {
		if _, ok := selections[c.Name]; !ok {
			continue
		}
		cfg, msg := s.resolveM2M(def, c)
		if cfg == nil {
			return newAPIErr(400, "VALIDATION", msg)
		}
		if !s.hasTablePerm(u, cfg.Junction.ID, "create") || !s.hasTablePerm(u, cfg.Junction.ID, "delete") {
			return newAPIErr(403, "FORBIDDEN", "managing "+cfg.Junction.Label+
				" relations requires create and delete grants on that table")
		}
	}
	return nil
}

// applyM2MPayload syncs stripped selections after the row write committed.
// srcRow (old row, update path) supplies the source ref value; on insert it
// comes from the payload via the resolved config.
func (s *Server) applyM2MPayload(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef,
	body map[string]any, srcRow map[string]any, selections map[string][]any,
) error {
	for _, c := range cols {
		want, ok := selections[c.Name]
		if !ok {
			continue
		}
		cfg, msg := s.resolveM2M(def, c)
		if cfg == nil {
			return newAPIErr(400, "VALIDATION", msg)
		}
		var srcVal any
		if srcRow != nil {
			srcVal = srcRow[cfg.SrcRef]
		}
		if srcVal == nil {
			srcVal = body[cfg.SrcRef]
		}
		if srcVal == nil {
			return newAPIErr(400, "VALIDATION",
				"column "+c.Name+": provide the "+cfg.SrcRef+" value so relations can be created")
		}
		if err := s.syncM2MLinks(u, def, c, srcVal, want); err != nil {
			return err
		}
	}
	return nil
}

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
