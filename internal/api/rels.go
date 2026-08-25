package api

import (
	"errors"
	"net/http"
	"strconv"

	"ku-crud/internal/ds"
	"ku-crud/internal/engine"
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

// buildM2MRels resolves many-to-many display data for the given rows.
// Result: m2mRels[colName][String(srcRefValue)] = []target display rows.
// Requires read grants on both junction and target; otherwise skipped.
func (s *Server) buildM2MRels(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef, rows []map[string]any) map[string]map[string][]map[string]any {
	var out map[string]map[string][]map[string]any
	for _, c := range cols {
		if c.FieldType != "m2m" || c.M2MJunctionDefID <= 0 || def.ID == 0 {
			continue
		}
		cfg, msg := s.resolveM2M(def, c)
		if cfg == nil {
			_ = msg // broken config (drifted junction) — render nothing
			continue
		}
		if !s.hasTablePerm(u, cfg.Junction.ID, "read") || !s.hasTablePerm(u, cfg.TargetID, "read") {
			continue
		}
		// collect distinct source ref values from the page
		srcVals := map[string]any{}
		for _, row := range rows {
			if v := row[cfg.SrcRef]; v != nil {
				srcVals[rowValKey(v)] = v
			}
		}
		if len(srcVals) == 0 {
			continue
		}
		ja, err := s.liveAdapter(cfg.Junction.DatasourceID)
		if err != nil {
			continue
		}
		// junction links (chunked to keep IN-lists small)
		links := map[string][]any{} // srcKey → target values
		vals := make([]any, 0, len(srcVals))
		for _, v := range srcVals {
			vals = append(vals, v)
		}
		const chunk = 500
		for i := 0; i < len(vals); i += chunk {
			end := i + chunk
			if end > len(vals) {
				end = len(vals)
			}
			pairs, err := ja.FetchPairsByRef(cfg.Junction.SchemaName, cfg.Junction.TableName,
				c.M2MJunctionSrcCol, c.M2MJunctionTgtCol, vals[i:end])
			if err != nil {
				continue
			}
			for _, p := range pairs {
				if p.Col == nil || p.Ret == nil {
					continue
				}
				links[rowValKey(p.Col)] = append(links[rowValKey(p.Col)], p.Ret)
			}
		}
		ja.Close()
		if len(links) == 0 {
			continue
		}
		// resolve target display rows for all linked values
		target, _, err := s.store.GetTableDef(cfg.TargetID)
		if err != nil {
			continue
		}
		ta, err := s.liveAdapter(target.DatasourceID)
		if err != nil {
			continue
		}
		tgtDistinct := map[string]any{}
		for _, tvs := range links {
			for _, tv := range tvs {
				tgtDistinct[rowValKey(tv)] = tv
			}
		}
		tgtVals := make([]any, 0, len(tgtDistinct))
		for _, v := range tgtDistinct {
			tgtVals = append(tgtVals, v)
		}
		tgtRows, err := ta.FetchByRefValues(target.SchemaName, target.TableName,
			cfg.TargetRef, c.M2MDisplayColumns, tgtVals)
		ta.Close()
		if err != nil {
			continue
		}
		if out == nil {
			out = map[string]map[string][]map[string]any{}
		}
		col := map[string][]map[string]any{}
		for srcKey, tvs := range links {
			list := []map[string]any{}
			for _, tv := range tvs {
				if tr, ok := tgtRows[rowValKey(tv)]; ok {
					list = append(list, tr)
				}
			}
			col[srcKey] = list
		}
		out[c.Name] = col
	}
	return out
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

// m2mLinks returns the current target values + display rows for one row's
// m2m column (used by the edit form).
func (s *Server) m2mLinks(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef, c meta.ColumnDef, srcVal any) ([]any, []map[string]any, error) {
	cfg, msg := s.resolveM2M(def, c)
	if cfg == nil {
		return nil, nil, newAPIErr(400, "VALIDATION", msg)
	}
	if !s.hasTablePerm(u, cfg.Junction.ID, "read") || !s.hasTablePerm(u, cfg.TargetID, "read") {
		return nil, nil, newAPIErr(403, "FORBIDDEN", "no read access to the related tables")
	}
	ja, err := s.liveAdapter(cfg.Junction.DatasourceID)
	if err != nil {
		return nil, nil, err
	}
	pairs, err := ja.FetchPairsByRef(cfg.Junction.SchemaName, cfg.Junction.TableName,
		c.M2MJunctionSrcCol, c.M2MJunctionTgtCol, []any{srcVal})
	ja.Close()
	if err != nil {
		return nil, nil, err
	}
	var values []any
	for _, p := range pairs {
		if p.Ret != nil {
			values = append(values, p.Ret)
		}
	}
	target, _, err := s.store.GetTableDef(cfg.TargetID)
	if err != nil {
		return nil, nil, err
	}
	var rows []map[string]any
	if len(values) > 0 {
		ta, err := s.liveAdapter(target.DatasourceID)
		if err != nil {
			return nil, nil, err
		}
		rowsByKey, err := ta.FetchByRefValues(target.SchemaName, target.TableName,
			cfg.TargetRef, c.M2MDisplayColumns, values)
		ta.Close()
		if err != nil {
			return nil, nil, err
		}
		for _, v := range values {
			if r, ok := rowsByKey[rowValKey(v)]; ok {
				rows = append(rows, r)
			}
		}
	}
	return values, rows, nil
}

type apiError struct {
	status int
	code   string
	msg    string
}

func (e *apiError) Error() string { return e.msg }
func newAPIErr(status int, code, msg string) *apiError {
	return &apiError{status: status, code: code, msg: msg}
}

// handleM2MLinks GET /api/tables/{id}/rows/{pk}/m2m/{column}
func (s *Server) handleM2MLinks(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	var col *meta.ColumnDef
	for i, c := range cols {
		if c.Name == r.PathValue("column") {
			if c.FieldType == "m2m" {
				col = &cols[i]
			}
			break
		}
	}
	if col == nil {
		writeErr(w, 404, "NOT_FOUND", "m2m column not found", nil)
		return
	}
	cfg, msg := s.resolveM2M(def, *col)
	if cfg == nil {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	pkVals, err := engine.DecodeKey(toCore(def, cols), r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	srcRows, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, pkVals, realColNames(cols))
	a.Close()
	if err != nil {
		writeErr(w, 502, "CONN", "fetch failed", err.Error())
		return
	}
	if len(srcRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	srcVal := srcRows[0][cfg.SrcRef]
	if srcVal == nil {
		writeJSON(w, 200, map[string]any{"values": []any{}, "rows": []map[string]any{}})
		return
	}
	values, rows, err := s.m2mLinks(u, def, cols, *col, srcVal)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeErr(w, ae.status, ae.code, ae.msg, nil)
			return
		}
		writeErr(w, 502, "CONN", "links fetch failed", err.Error())
		return
	}
	if values == nil {
		values = []any{}
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"values": values, "rows": rows})
}

// handleM2MOptions lists target-table rows for the m2m picker (mirror of
// handleFKOptions). Requires read on source, junction and target.
func (s *Server) handleM2MOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
		return
	}
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	var col *meta.ColumnDef
	for i, c := range cols {
		if c.Name == r.PathValue("column") {
			if c.FieldType == "m2m" {
				col = &cols[i]
			}
			break
		}
	}
	if col == nil {
		writeErr(w, 404, "NOT_FOUND", "m2m column not found", nil)
		return
	}
	cfg, msg := s.resolveM2M(def, *col)
	if cfg == nil {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if !s.hasTablePerm(u, cfg.Junction.ID, "read") || !s.hasTablePerm(u, cfg.TargetID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to the related tables", nil)
		return
	}
	target, tcols, err := s.store.GetTableDef(cfg.TargetID)
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
	names := []string{cfg.TargetRef}
	for _, d := range col.M2MDisplayColumns {
		if d != cfg.TargetRef {
			names = append(names, d)
		}
	}
	sortCol, sortDir := resolveSort(target, tcols, "", "")
	if !containsStr(names, sortCol) {
		names = append(names, sortCol)
	}
	lp := ds.ListParams{Schema: target.SchemaName, Table: target.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: sortDir,
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
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "total": total, "page": page, "pageSize": target.PageSize})
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// handleFKOptions lists target-table rows for the fk picker modal.
func (s *Server) handleFKOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	if writeQueryReadOnly(w, def) {
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
	sortCol, sortDir := resolveSort(target, tcols, "", "")
	if !containsStr(names, sortCol) {
		names = append(names, sortCol)
	}
	lp := ds.ListParams{Schema: target.SchemaName, Table: target.TableName, Columns: names,
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: sortDir,
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
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "total": total, "page": page, "pageSize": target.PageSize})
}
