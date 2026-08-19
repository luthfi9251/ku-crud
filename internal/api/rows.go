package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
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
		sortCol, sortDir = def.PKColumn, "ASC"
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
	writeJSON(w, 200, map[string]any{"rows": out, "total": total, "page": page, "pageSize": def.PageSize})
}

func (s *Server) handleRowGet(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()

	names := colNames(cols)
	pkVal, err := coercePK(fieldTypeOf(cols, def.PKColumn), r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad pk value", err.Error())
		return
	}
	sqlText, err := ds.BuildFetchByPK(def.SchemaName, def.TableName, def.PKColumn, names)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad identifiers", err.Error())
		return
	}
	scan := scanTargets(names)
	err = db.QueryRow(sqlText, pkVal).Scan(scan...)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	writeJSON(w, 200, rowToMap(names, deref(scan)))
}
