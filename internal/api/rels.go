package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

// buildRels batch-resolves fk display data for the given rows. Result:
// rels[fkColName][String(value)] = map of ref+display fields. Columns whose
// target the user cannot read are skipped (grid falls back to raw values).
func (s *Server) buildRels(u CtxUser, cols []meta.ColumnDef, rows []map[string]any) map[string]map[string]map[string]any {
	var rels map[string]map[string]map[string]any
	for _, c := range cols {
		if c.FieldType != "fk" || c.FKTableDefID <= 0 {
			continue
		}
		vals := map[string]any{} // String(value) → raw value
		for _, row := range rows {
			v, ok := row[c.Name]
			if !ok || v == nil {
				continue
			}
			vals[rowValKey(v)] = v
		}
		if len(vals) == 0 {
			continue
		}
		if !s.hasTablePerm(u, c.FKTableDefID, "read") {
			continue
		}
		target, _, err := s.store.GetTableDef(c.FKTableDefID)
		if err != nil {
			continue
		}
		db, err := s.liveDB(target.DatasourceID)
		if err != nil {
			continue
		}
		m, err := fetchRelRows(db, target, c, vals)
		db.Close()
		if err != nil {
			continue
		}
		if rels == nil {
			rels = map[string]map[string]map[string]any{}
		}
		rels[c.Name] = m
	}
	return rels
}

func rowValKey(v any) string {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return strconv.FormatFloat(f64(v), 'f', -1, 64)
	}
}

func f64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int64:
		return float64(x)
	case int:
		return float64(x)
	default:
		return 0
	}
}

func fetchRelRows(db *sql.DB, target *meta.TableDef, c meta.ColumnDef, vals map[string]any) (map[string]map[string]any, error) {
	keys := make([]string, 0, len(vals))
	args := make([]any, 0, len(vals))
	for k, v := range vals {
		keys = append(keys, k)
		args = append(args, v)
	}
	sqlText, err := ds.BuildFetchByRefValues(target.SchemaName, target.TableName,
		c.FKRefColumn, c.FKDisplayColumns, len(args))
	if err != nil {
		return nil, err
	}
	names := []string{c.FKRefColumn}
	for _, d := range c.FKDisplayColumns {
		if d != c.FKRefColumn {
			names = append(names, d)
		}
	}
	rows, err := db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		scan := scanTargets(names)
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		m := rowToMap(names, deref(scan))
		out[rowValKey(m[c.FKRefColumn])] = m
	}
	return out, rows.Err()
}

// handleFKOptions lists target-table rows for the fk picker modal.
func (s *Server) handleFKOptions(w http.ResponseWriter, r *http.Request) {
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
	var fk *meta.ColumnDef
	for i, c := range cols {
		if c.Name == r.PathValue("column") {
			if c.FieldType == "fk" {
				fk = &cols[i]
			}
			break
		}
	}
	if fk == nil {
		writeErr(w, 404, "NOT_FOUND", "fk column not found", nil)
		return
	}
	if !s.hasTablePerm(u, fk.FKTableDefID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to the related table", nil)
		return
	}
	target, tcols, err := s.store.GetTableDef(fk.FKTableDefID)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	db, err := s.liveDB(target.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()

	var searchable []string
	for _, c := range tcols {
		if c.Searchable {
			searchable = append(searchable, c.Name)
		}
	}
	q := r.URL.Query()
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	names := []string{fk.FKRefColumn}
	for _, d := range fk.FKDisplayColumns {
		if d != fk.FKRefColumn {
			names = append(names, d)
		}
	}
	lp := ds.ListParams{Schema: target.SchemaName, Table: target.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: target.KeyColumns[0], SortDir: "ASC",
		Limit: target.PageSize, Offset: (page - 1) * target.PageSize}
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
	writeJSON(w, 200, map[string]any{"rows": out, "total": total, "page": page, "pageSize": target.PageSize})
}
