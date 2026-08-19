package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"

	"github.com/jackc/pgx/v5/pgconn"
)

func scanTargets(cols []string) []any {
	out := make([]any, len(cols))
	for i := range out {
		out[i] = new(any)
	}
	return out
}

func deref(scan []any) []any {
	out := make([]any, len(scan))
	for i, p := range scan {
		v := p.(*any)
		out[i] = *v
	}
	return out
}

func rowToMap(cols []string, scan []any) map[string]any {
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		v := scan[i]
		if b, ok := v.([]byte); ok {
			v = string(b) // e.g. numeric arrives as bytes; base64 in JSON otherwise
		}
		m[c] = v
	}
	return m
}

func colNames(cols []meta.ColumnDef) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
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
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()

	byName := map[string]meta.ColumnDef{}
	var searchable []string
	for _, c := range cols {
		byName[c.Name] = c
		if c.Searchable {
			searchable = append(searchable, c.Name)
		}
	}

	q := r.URL.Query()
	sortCol, sortDir := q.Get("sort"), q.Get("dir")
	if !byName[sortCol].Sortable { // includes empty sortCol
		sortCol, sortDir = def.KeyColumns[0], "ASC"
	}
	if sortDir != "ASC" && sortDir != "DESC" {
		sortDir = "ASC"
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	names := colNames(cols)
	lp := ds.ListParams{Schema: def.SchemaName, Table: def.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: sortDir,
		Limit: def.PageSize, Offset: (page - 1) * def.PageSize}

	listSQL, args, err := ds.BuildList(lp)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad query params", err.Error())
		return
	}
	rows, err := db.Query(listSQL, args...)
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		scan := scanTargets(names)
		if err := rows.Scan(scan...); err != nil {
			writeErr(w, 502, "CONN", "scan failed", err.Error())
			return
		}
		out = append(out, rowToMap(names, deref(scan)))
	}
	if err := rows.Err(); err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}

	countSQL, cargs, _ := ds.BuildCount(lp)
	var total int
	if err := db.QueryRow(countSQL, cargs...).Scan(&total); err != nil {
		writeErr(w, 502, "CONN", "count failed", err.Error())
		return
	}
	rels := s.buildRels(userFrom(r), cols, out)
	writeJSON(w, 200, map[string]any{"rows": out, "total": total, "page": page,
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
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()

	names := colNames(cols)
	keyVals, err := rowKeyVals(def, cols, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	sqlText, err := ds.BuildFetchByKey(def.SchemaName, def.TableName, def.KeyColumns, names)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad identifiers", err.Error())
		return
	}
	scan := scanTargets(names)
	err = db.QueryRow(sqlText, keyVals...).Scan(scan...)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	row := rowToMap(names, deref(scan))
	rels := s.buildRels(userFrom(r), cols, []map[string]any{row})
	writeJSON(w, 200, map[string]any{"row": row, "rels": rels})
}

// editablePayload validates body against editable columns and returns
// (cols, vals) in column-definition order. requireAll=true enforces required
// columns for INSERT. Any non-editable/unknown key is rejected.
func editablePayload(body map[string]any, cols []meta.ColumnDef, requireAll bool) ([]string, []any, error) {
	editable := map[string]meta.ColumnDef{}
	for _, c := range cols {
		if c.Editable {
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
		if !c.Editable {
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
			names = append(names, c.Name)
			vals = append(vals, v)
		} else if requireAll && c.Required {
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

func fetchRows(db *sql.DB, def *meta.TableDef, cols []meta.ColumnDef, keyVals []any) ([]map[string]any, error) {
	names := colNames(cols)
	sqlText, err := ds.BuildFetchByKey(def.SchemaName, def.TableName, def.KeyColumns, names)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(sqlText, keyVals...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		scan := scanTargets(names)
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		out = append(out, rowToMap(names, deref(scan)))
	}
	return out, rows.Err()
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
	names, vals, err := editablePayload(body, cols, true)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	if err := s.checkFKValues(cols, names, vals); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	sqlText, _, err := ds.BuildInsert(def.SchemaName, def.TableName, names)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad columns", err.Error())
		return
	}
	if _, err := db.Exec(sqlText, vals...); err != nil {
		if fkViolation(err) {
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
	names, vals, err := editablePayload(body, cols, false)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	pkVals, err := rowKeyVals(def, cols, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()

	if err := s.checkFKValues(cols, names, vals); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}

	oldRows, err := fetchRows(db, def, cols, pkVals)
	if err != nil {
		writeErr(w, 502, "CONN", "fetch old failed", err.Error())
		return
	}
	if len(oldRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}

	sqlText, _, err := ds.BuildUpdateByKey(def.SchemaName, def.TableName, names, def.KeyColumns)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad columns", err.Error())
		return
	}
	args := append(append([]any{}, vals...), pkVals...)
	if _, err := db.Exec(sqlText, args...); err != nil {
		if fkViolation(err) {
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
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()

	oldRows, err := fetchRows(db, def, cols, pkVals)
	if err != nil {
		writeErr(w, 502, "CONN", "fetch old failed", err.Error())
		return
	}
	if len(oldRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	conflicts, err := s.referencedBy(def, oldRows[0])
	if err != nil {
		writeErr(w, 502, "CONN", "reference check failed", err.Error())
		return
	}
	if len(conflicts) > 0 {
		writeErr(w, 409, "CONFLICT", "row is referenced by other rows", conflicts)
		return
	}
	sqlText, err := ds.BuildDeleteByKey(def.SchemaName, def.TableName, def.KeyColumns)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad identifiers", err.Error())
		return
	}
	if _, err := db.Exec(sqlText, pkVals...); err != nil {
		if fkViolation(err) {
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
		db, err := s.liveDB(target.DatasourceID)
		if err != nil {
			return err
		}
		sqlText, err := ds.BuildFetchByRefValues(target.SchemaName, target.TableName,
			c.FKRefColumn, nil, 1)
		if err != nil {
			db.Close()
			return err
		}
		var one any
		err = db.QueryRow(sqlText, vals[i]).Scan(&one)
		db.Close()
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s: referenced row not found", name)
		}
		if err != nil {
			return err
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
		db, err := s.liveDB(srcDef.DatasourceID)
		if err != nil {
			return nil, err
		}
		countSQL, err := ds.BuildCountByRefEq(srcDef.SchemaName, srcDef.TableName, src.Column)
		if err != nil {
			db.Close()
			return nil, err
		}
		var n int
		err = db.QueryRow(countSQL, refVal).Scan(&n)
		db.Close()
		if err != nil {
			return nil, err
		}
		if n > 0 {
			conflicts = append(conflicts, map[string]any{"table": src.DefLabel, "column": src.Column, "count": n})
		}
	}
	return conflicts, nil
}

// fkViolation detects a Postgres FK constraint failure (SQLSTATE 23503).
func fkViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23503"
}
