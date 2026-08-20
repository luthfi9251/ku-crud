package api

import (
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
		a, err := s.liveAdapter(target.DatasourceID)
		if err != nil {
			continue
		}
		valsSlice := make([]any, 0, len(vals))
		for _, v := range vals {
			valsSlice = append(valsSlice, v)
		}
		m, err := a.FetchByRefValues(target.SchemaName, target.TableName, c.FKRefColumn, c.FKDisplayColumns, valsSlice)
		a.Close()
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
	a, err := s.liveAdapter(target.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer a.Close()

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
	sortCol := target.KeyColumns[0]
	found := false
	for _, n := range names {
		if n == sortCol {
			found = true
			break
		}
	}
	if !found {
		names = append(names, sortCol)
	}
	lp := ds.ListParams{Schema: target.SchemaName, Table: target.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: "ASC",
		Limit: target.PageSize, Offset: (page - 1) * target.PageSize}
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
	writeJSON(w, 200, map[string]any{"rows": rows, "total": total, "page": page, "pageSize": target.PageSize})
}
