package engine

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
)

// Resolver hands the engine the physical datasource behind a definition,
// plus related definitions (fk/m2m targets) by name. The platform
// implements it over meta; the core App implements it over registered defs.
type Resolver interface {
	Adapter(t *defs.Table) (ds.Adapter, error)
	Resolve(name string) (*defs.Table, error) // "" = self handled by caller
}

// Adapter-open failures the platform maps onto its historical responses.
var (
	ErrDSNotFound = errors.New("datasource not found")
	ErrConn       = errors.New("connection failed")
)

// ReadService renders the read endpoints (list/get/export). It is
// auth-free: grant filtering rides on the per-request callbacks the
// platform supplies — FKJoin (perm-checked fk filter joins) and CanRead
// (per-target read grants for relation visibility). Nil CanRead allows
// every target; nil FKJoin rejects fk filter columns.
type ReadService struct {
	R       Resolver
	FKJoin  FKJoinResolver
	CanRead func(name string) bool
}

// errStash mirrors the platform's logging response writer so non-2xx
// bodies also reach the access log with code/message (duck-typed,
// optional — plain writers work too).
type errStash interface{ setError(code, msg string) }

func writeErr(w http.ResponseWriter, status int, code, msg string, detail any) {
	if ri, ok := w.(errStash); ok {
		ri.setError(code, msg)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": msg, "detail": detail})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeQueryErr(w http.ResponseWriter, err error) {
	if ds.IsQueryTimeout(err) {
		writeErr(w, 502, "QUERY_TIMEOUT", "query exceeded the execution time limit", err.Error())
		return
	}
	writeErr(w, 502, "CONN", "query failed", err.Error())
}

func writeLiveErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDSNotFound) {
		writeErr(w, 404, "NOT_FOUND", "datasource not found", nil)
		return
	}
	if errors.Is(err, ErrConn) {
		writeErr(w, 502, "CONN", "could not connect to datasource", err.Error())
		return
	}
	writeErr(w, 500, "INTERNAL", "server error", nil)
}

// realColNames drops virtual columns (m2m relations and computed) — they
// have no live counterpart and must never reach SQL SELECT lists.
func realColNames(cols []defs.Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c.FieldType != "m2m" && !c.IsComputed {
			out = append(out, c.Name)
		}
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

func (s *ReadService) canRead(name string) bool {
	return s.CanRead == nil || s.CanRead(name)
}

// List renders GET rows: one page of rows plus total, enriched with fk
// rels and m2m rels the caller may read.
func (s *ReadService) List(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	var searchable []string
	for _, c := range t.Columns {
		if c.Searchable {
			searchable = append(searchable, c.Name)
		}
	}

	q := r.URL.Query()
	sortCol, sortDir := ResolveSort(t, q.Get("sort"), q.Get("dir"))
	filters, ferr := ParseFilters(t, q.Get("filters"), s.FKJoin)
	if ferr != nil {
		writeErr(w, 400, "FILTER_INVALID", ferr.Error(), nil)
		return
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	names := realColNames(t.Columns)
	var rows []map[string]any
	var total int
	if t.SourceType == "query" {
		if sortCol == "" {
			writeErr(w, 400, "VALIDATION", "query view has no sortable column", nil)
			return
		}
		qp := ds.QueryParams{Query: t.QuerySQL, Columns: names,
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir, Filters: filters,
			Limit: t.PageSize, Offset: (page - 1) * t.PageSize}
		rows, err = a.ListQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
		total, err = a.CountQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
	} else {
		lp := ds.ListParams{Schema: t.Schema, Table: t.PhysTab, Columns: names,
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir,
			Filters: filters,
			Limit:   t.PageSize, Offset: (page - 1) * t.PageSize}
		rows, err = a.ListRows(lp)
		if err != nil {
			writeErr(w, 502, "CONN", "query failed", err.Error())
			return
		}
		total, err = a.CountRows(lp)
		if err != nil {
			writeErr(w, 502, "CONN", "count failed", err.Error())
			return
		}
	}
	ApplyComputed(t.Columns, rows)
	rels := s.buildRels(t, t.Columns, rows)
	m2mRels := s.buildM2MRels(t, t.Columns, rows)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "total": total, "page": page,
		"pageSize": t.PageSize, "rels": rels, "m2mRels": m2mRels})
}

// aggFuncs is the stats allowlist; the ds builder carries its own copy so
// only these names can ever reach SQL text.
var aggFuncs = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

// Stats renders GET stats: one aggregate value over the def with optional
// grid-format filters (dashboard cards).
func (s *ReadService) Stats(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	q := r.URL.Query()
	fn := q.Get("func")
	colName := q.Get("column")
	if !aggFuncs[fn] {
		writeErr(w, 400, "STATS_INVALID", "func must be one of count|sum|avg|min|max", nil)
		return
	}
	var col *defs.Column
	if colName != "" {
		for i := range t.Columns {
			if t.Columns[i].Name == colName {
				col = &t.Columns[i]
				break
			}
		}
		if col == nil || col.FieldType == "m2m" || col.IsComputed {
			writeErr(w, 400, "STATS_INVALID", "unknown or virtual column "+colName, nil)
			return
		}
	}
	switch fn {
	case "count":
		if colName != "" {
			writeErr(w, 400, "STATS_INVALID", "count takes no column", nil)
			return
		}
	case "sum", "avg":
		if col == nil || col.FieldType != "number" {
			writeErr(w, 400, "STATS_INVALID", fn+" requires a number column", nil)
			return
		}
	case "min", "max":
		if col == nil || (col.FieldType != "number" && col.FieldType != "datetime") {
			writeErr(w, 400, "STATS_INVALID", fn+" requires a number or datetime column", nil)
			return
		}
	}
	filters, ferr := ParseFilters(t, q.Get("filters"), s.FKJoin)
	if ferr != nil {
		writeErr(w, 400, "FILTER_INVALID", ferr.Error(), nil)
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()
	ap := ds.AggregateParams{Func: fn, Column: colName, Filters: filters}
	if t.SourceType == "query" {
		ap.Query = t.QuerySQL
	} else {
		ap.Schema, ap.Table = t.Schema, t.PhysTab
	}
	res, err := a.AggregateRows(ap)
	if err != nil {
		if t.SourceType == "query" {
			writeQueryErr(w, err)
			return
		}
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	v := res.Value
	if col != nil && col.FieldType == "number" {
		if sv, ok := v.(string); ok {
			if f, perr := strconv.ParseFloat(sv, 64); perr == nil {
				v = f // numeric aggregates arrive as strings on some drivers
			}
		}
	}
	writeJSON(w, 200, map[string]any{"func": fn, "column": colName, "value": v, "hasRows": res.HasRows})
}

// Get renders one row by key, enriched with fk rels.
func (s *ReadService) Get(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	// checked before the adapter so no-grant callers can't probe whether a
	// query view has key columns (perm itself was checked by the caller)
	if t.SourceType == "query" && len(t.Keys) == 0 {
		writeErr(w, 400, "QUERY_NO_KEY", "this query view has no key columns", nil)
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	names := realColNames(t.Columns)
	keyVals, err := DecodeKey(t, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	var row map[string]any
	if t.SourceType == "query" {
		kf := make([]ds.ColumnFilter, len(keyVals))
		for i, k := range t.Keys {
			kf[i] = ds.ColumnFilter{Column: k, Op: "eq", Values: []any{keyVals[i]}}
		}
		qp := ds.QueryParams{Query: t.QuerySQL, Columns: names,
			SortCol: t.Keys[0], SortDir: "ASC", Filters: kf, Limit: 1}
		rowsOut, err := a.ListQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
		if len(rowsOut) == 0 {
			writeErr(w, 404, "NOT_FOUND", "row not found", nil)
			return
		}
		row = rowsOut[0]
	} else {
		rowsOut, err := a.FetchByKey(t.Schema, t.PhysTab, t.Keys, keyVals, names)
		if err != nil {
			writeErr(w, 502, "CONN", "query failed", err.Error())
			return
		}
		if len(rowsOut) == 0 {
			writeErr(w, 404, "NOT_FOUND", "row not found", nil)
			return
		}
		row = rowsOut[0]
	}
	ApplyComputed(t.Columns, []map[string]any{row})
	rels := s.buildRels(t, t.Columns, []map[string]any{row})
	writeJSON(w, 200, map[string]any{"row": row, "rels": rels})
}

// exportRowCap bounds CSV exports so a giant table cannot exhaust memory.
const exportRowCap = 100000

// csvCell renders one scanned value for CSV output.
func csvCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return t
	case []byte:
		return string(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

// joinDisplay renders a resolved fk target row as the display columns joined
// with an em-dash (mirrors the grid's cell).
func joinDisplay(rel map[string]any, display []string, refCol string) string {
	if len(display) == 0 {
		display = []string{refCol}
	}
	out := ""
	for i, d := range display {
		if i > 0 {
			out += " — "
		}
		out += csvCell(rel[d])
	}
	return out
}

// ExportCSV streams the full result set matching the active search/sort as
// a CSV download (all pages, not just the current one).
func (s *ReadService) ExportCSV(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	var searchable []string
	for _, c := range t.Columns {
		if c.Searchable {
			searchable = append(searchable, c.Name)
		}
	}
	q := r.URL.Query()
	sortCol, sortDir := ResolveSort(t, q.Get("sort"), q.Get("dir"))
	filters, ferr := ParseFilters(t, q.Get("filters"), s.FKJoin)
	if ferr != nil {
		writeErr(w, 400, "FILTER_INVALID", ferr.Error(), nil)
		return
	}
	var total int
	var rows []map[string]any
	if t.SourceType == "query" {
		if sortCol == "" {
			writeErr(w, 400, "VALIDATION", "query view has no sortable column", nil)
			return
		}
		qp := ds.QueryParams{Query: t.QuerySQL, Columns: realColNames(t.Columns),
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir, Filters: filters,
			Limit: exportRowCap + 1, Offset: 0}
		total, err = a.CountQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
		if total > exportRowCap {
			writeErr(w, 400, "EXPORT_TOO_LARGE",
				fmt.Sprintf("export is limited to %d rows; this query matches %d — narrow the search", exportRowCap, total), nil)
			return
		}
		rows, err = a.ListQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
	} else {
		lp := ds.ListParams{Schema: t.Schema, Table: t.PhysTab, Columns: realColNames(t.Columns),
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir, Filters: filters,
			Limit: exportRowCap + 1, Offset: 0}
		total, err = a.CountRows(lp)
		if err != nil {
			writeErr(w, 502, "CONN", "count failed", err.Error())
			return
		}
		if total > exportRowCap {
			writeErr(w, 400, "EXPORT_TOO_LARGE",
				fmt.Sprintf("export is limited to %d rows; this query matches %d — narrow the search", exportRowCap, total), nil)
			return
		}
		rows, err = a.ListRows(lp)
		if err != nil {
			writeErr(w, 502, "CONN", "query failed", err.Error())
			return
		}
	}
	ApplyComputed(t.Columns, rows)

	// resolve fk display values in bounded chunks (IN-lists stay small)
	var visible []defs.Column
	for _, c := range t.Columns {
		if c.Visible {
			visible = append(visible, c)
		}
	}
	rels := map[string]map[string]map[string]any{}
	m2mRels := map[string]map[string][]map[string]any{}
	const chunk = 500
	for i := 0; i < len(rows); i += chunk {
		end := i + chunk
		if end > len(rows) {
			end = len(rows)
		}
		for col, m := range s.buildRels(t, visible, rows[i:end]) {
			if rels[col] == nil {
				rels[col] = map[string]map[string]any{}
			}
			for k, v := range m {
				rels[col][k] = v
			}
		}
		for col, m := range s.buildM2MRels(t, visible, rows[i:end]) {
			if m2mRels[col] == nil {
				m2mRels[col] = map[string][]map[string]any{}
			}
			for k, v := range m {
				m2mRels[col][k] = v
			}
		}
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-%s.csv"`, t.PhysTab, time.Now().Format("20060102-150405")))
	// UTF-8 BOM so Excel renders Unicode correctly
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	header := make([]string, len(visible))
	m2mCfgs := map[string]*M2MCfg{}
	for i, c := range visible {
		header[i] = c.Name
		if c.FieldType == "m2m" {
			if p, _ := ResolveM2M(s.R, t, c); p != nil {
				m2mCfgs[c.Name] = p
			}
		}
	}
	cw.Write(header)
	for _, row := range rows {
		rec := make([]string, len(visible))
		for i, c := range visible {
			v := row[c.Name]
			if c.FieldType == "fk" && c.FK != nil && v != nil {
				if rel, ok := rels[c.Name][rowValKey(v)]; ok {
					rec[i] = joinDisplay(rel, c.FK.DisplayColumns, c.FK.RefColumn)
					continue
				}
			}
			if c.FieldType == "m2m" {
				if p := m2mCfgs[c.Name]; p != nil {
					if list, ok := m2mRels[c.Name][rowValKey(row[p.SrcRef])]; ok {
						parts := make([]string, len(list))
						for j, tr := range list {
							parts[j] = joinDisplay(tr, c.M2M.DisplayColumns, p.TargetRef)
						}
						rec[i] = strings.Join(parts, ", ")
						continue
					}
				}
				rec[i] = ""
				continue
			}
			rec[i] = csvCell(v)
		}
		cw.Write(rec)
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		slog.Warn("csv export write failed", "err", err.Error())
	}
}

// buildRels batch-resolves fk display data for the given rows. Result:
// rels[fkColName][String(value)] = map of ref+display fields. Columns whose
// target the caller cannot read are skipped (grid falls back to raw values).
// Self-referencing fk columns (empty target name) resolve against t itself.
func (s *ReadService) buildRels(t *defs.Table, cols []defs.Column, rows []map[string]any) map[string]map[string]map[string]any {
	var rels map[string]map[string]map[string]any
	for _, c := range cols {
		if c.FieldType != "fk" || c.FK == nil {
			continue
		}
		if c.FK.Table == defs.MissingTable {
			continue // drifted target def — the old flow skipped these columns
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
		target := t
		if c.FK.Table != "" {
			if !s.canRead(c.FK.Table) {
				continue
			}
			resolved, err := s.R.Resolve(c.FK.Table)
			if err != nil {
				continue
			}
			target = resolved
		}
		a, err := s.R.Adapter(target)
		if err != nil {
			continue
		}
		valsSlice := make([]any, 0, len(vals))
		for _, v := range vals {
			valsSlice = append(valsSlice, v)
		}
		m, err := a.FetchByRefValues(target.Schema, target.PhysTab, c.FK.RefColumn, c.FK.DisplayColumns, valsSlice)
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
// (Junction/target resolution is the shared ResolveM2M; broken configs
// render nothing rather than failing the request.)
func (s *ReadService) buildM2MRels(t *defs.Table, cols []defs.Column, rows []map[string]any) map[string]map[string][]map[string]any {
	var out map[string]map[string][]map[string]any
	for _, c := range cols {
		if c.M2M == nil {
			continue
		}
		cfg, _ := ResolveM2M(s.R, t, c)
		if cfg == nil || cfg.TargetMissing {
			continue // broken config (drifted junction/target) — render nothing
		}
		if !s.canRead(cfg.Junction.Name) || !s.canRead(cfg.Target.Name) {
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
		ja, err := s.R.Adapter(cfg.Junction)
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
			pairs, err := ja.FetchPairsByRef(cfg.Junction.Schema, cfg.Junction.PhysTab,
				c.M2M.SrcCol, c.M2M.TgtCol, vals[i:end])
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
		ta, err := s.R.Adapter(cfg.Target)
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
		tgtRows, err := ta.FetchByRefValues(cfg.Target.Schema, cfg.Target.PhysTab,
			cfg.TargetRef, c.M2M.DisplayColumns, tgtVals)
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
