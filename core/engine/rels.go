package engine

import (
	"net/http"
	"strconv"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
)

// M2MCfg is one m2m column's resolved link topology: the junction and
// target definitions plus the junction link columns and the ref columns
// the links hang on. It is the single canonical m2m resolution shared by
// the read, write and import paths — and by the platform's def-save
// validation, which calls in over the name-based core contract.
type M2MCfg struct {
	Junction  *defs.Table
	Target    *defs.Table
	SrcCol    string // junction fk column → this table
	TgtCol    string // junction fk column → target
	SrcRef    string // this table's column the junction references
	TargetRef string // target ref column
	// TargetMissing marks a dangling target definition (the junction's
	// target fk points at a deleted def): Target is nil then. The old
	// id-based flow deferred the failure to each endpoint, so callers
	// reproduce their historical errors.
	TargetMissing bool
}

// ResolveM2M loads and cross-checks one m2m column against its junction
// definition, with the platform's historical error messages. Returns a
// nil config + message on any inconsistency (an unresolvable target
// included); callers that prefer silent skips just drop the message.
// A dangling target def (defs.MissingTable) yields a non-nil config with
// TargetMissing set and a nil Target, mirroring the old id-based flow
// which resolved the target lazily per endpoint.
func ResolveM2M(r Resolver, t *defs.Table, c defs.Column) (*M2MCfg, string) {
	if c.M2M == nil || c.M2M.JunctionTable == "" || c.M2M.JunctionTable == defs.MissingTable {
		return nil, "column " + c.Name + ": junction definition not found (save it first)"
	}
	if c.M2M.JunctionTable == t.Name {
		return nil, "column " + c.Name + ": junction cannot be this table itself"
	}
	junction, err := r.Resolve(c.M2M.JunctionTable)
	if err != nil {
		return nil, "column " + c.Name + ": junction definition not found (save it first)"
	}
	var src, tgt *defs.Column
	for i, jc := range junction.Columns {
		if jc.Name == c.M2M.SrcCol && jc.FieldType == "fk" {
			src = &junction.Columns[i]
		}
		if jc.Name == c.M2M.TgtCol && jc.FieldType == "fk" {
			tgt = &junction.Columns[i]
		}
	}
	if src == nil || tgt == nil {
		return nil, "column " + c.Name + ": junction source/target columns must be defined fk columns"
	}
	if src.Name == tgt.Name {
		return nil, "column " + c.Name + ": junction source and target columns must differ"
	}
	if src.FK == nil || src.FK.Table != t.Name {
		return nil, "column " + c.Name + ": junction source column must reference this table"
	}
	// every required junction column must be one of the two link columns —
	// otherwise link inserts would violate NOT NULL
	for _, jc := range junction.Columns {
		if jc.Required && jc.Name != src.Name && jc.Name != tgt.Name {
			return nil, "column " + c.Name + ": junction has required column " + jc.Name +
				" outside the two link columns"
		}
	}
	if tgt.FK == nil {
		return nil, "column " + c.Name + ": m2m target definition not found"
	}
	// a junction fk targeting the junction itself ("") resolves to the
	// junction, mirroring id-based resolution on the platform side
	target := junction
	if tgt.FK.Table == defs.MissingTable {
		// dangling target def: the old flow kept the config (target def id
		// unresolved) and let each endpoint fail its own way
		return &M2MCfg{Junction: junction, Target: nil, TargetMissing: true,
			SrcCol: src.Name, TgtCol: tgt.Name,
			SrcRef: src.FK.RefColumn, TargetRef: tgt.FK.RefColumn}, ""
	}
	if tgt.FK.Table != "" {
		target, err = r.Resolve(tgt.FK.Table)
		if err != nil {
			return nil, "column " + c.Name + ": m2m target definition not found"
		}
	}
	return &M2MCfg{Junction: junction, Target: target,
		SrcCol: src.Name, TgtCol: tgt.Name,
		SrcRef: src.FK.RefColumn, TargetRef: tgt.FK.RefColumn}, ""
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// listOptions renders one picker page from a target table: ref column
// first, then display columns, searched over the target's searchable
// columns and sorted by its resolved default sort.
func (s *ReadService) listOptions(w http.ResponseWriter, r *http.Request, target *defs.Table, refCol string, display []string) {
	a, err := s.R.Adapter(target)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	var searchable []string
	for _, c := range target.Columns {
		if c.Searchable {
			searchable = append(searchable, c.Name)
		}
	}
	q := r.URL.Query()
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	names := []string{refCol}
	for _, d := range display {
		if d != refCol {
			names = append(names, d)
		}
	}
	sortCol, sortDir := ResolveSort(target, "", "")
	if !containsStr(names, sortCol) {
		names = append(names, sortCol)
	}
	lp := ds.ListParams{Schema: target.Schema, Table: target.PhysTab, Columns: names,
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

// FKOptions renders GET fkoptions/{column}: target-table rows for the fk
// picker modal. The caller checked read on the source table; read on the
// fk target rides on CanRead (self-references point at the source table
// itself, already permitted).
func (s *ReadService) FKOptions(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	var fk *defs.Column
	for i, c := range t.Columns {
		if c.Name == r.PathValue("column") {
			if c.FieldType == "fk" {
				fk = &t.Columns[i]
			}
			break
		}
	}
	if fk == nil {
		writeErr(w, 404, "NOT_FOUND", "fk column not found", nil)
		return
	}
	if fk.FK == nil {
		writeErr(w, 403, "FORBIDDEN", "no read access to the related table", nil)
		return
	}
	if fk.FK.Table == defs.MissingTable {
		// drifted target def: the old flow checked the grant on the dangling
		// def id first, then the def lookup failed
		if !s.canRead(defs.MissingTable) {
			writeErr(w, 403, "FORBIDDEN", "no read access to the related table", nil)
			return
		}
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	}
	target := t
	if fk.FK.Table != "" {
		if !s.canRead(fk.FK.Table) {
			writeErr(w, 403, "FORBIDDEN", "no read access to the related table", nil)
			return
		}
		resolved, err := s.R.Resolve(fk.FK.Table)
		if err != nil {
			writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
			return
		}
		target = resolved
	}
	s.listOptions(w, r, target, fk.FK.RefColumn, fk.FK.DisplayColumns)
}

// M2MOptions renders GET m2moptions/{column}: target-table rows for the
// m2m picker (mirror of FKOptions). The caller checked read on the source
// table; junction and target read grants ride on CanRead.
func (s *ReadService) M2MOptions(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	var col *defs.Column
	for i, c := range t.Columns {
		if c.Name == r.PathValue("column") {
			if c.FieldType == "m2m" {
				col = &t.Columns[i]
			}
			break
		}
	}
	if col == nil {
		writeErr(w, 404, "NOT_FOUND", "m2m column not found", nil)
		return
	}
	cfg, msg := ResolveM2M(s.R, t, *col)
	if cfg == nil {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if cfg.TargetMissing {
		// drifted target def: the old flow checked junction+target grants
		// (the dangling id passes only for admins), then the target def
		// lookup failed
		if !s.canRead(cfg.Junction.Name) || !s.canRead(defs.MissingTable) {
			writeErr(w, 403, "FORBIDDEN", "no read access to the related tables", nil)
			return
		}
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	}
	if !s.canRead(cfg.Junction.Name) || !s.canRead(cfg.Target.Name) {
		writeErr(w, 403, "FORBIDDEN", "no read access to the related tables", nil)
		return
	}
	s.listOptions(w, r, cfg.Target, cfg.TargetRef, col.M2M.DisplayColumns)
}

// M2MLinks renders GET rows/{pk}/m2m/{column}: the current target values
// plus display rows for one row's m2m column (the edit form's chip list).
// The caller checked read on the source table; junction and target read
// grants ride on CanRead.
func (s *ReadService) M2MLinks(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	var col *defs.Column
	for i, c := range t.Columns {
		if c.Name == r.PathValue("column") {
			if c.FieldType == "m2m" {
				col = &t.Columns[i]
			}
			break
		}
	}
	if col == nil {
		writeErr(w, 404, "NOT_FOUND", "m2m column not found", nil)
		return
	}
	cfg, msg := ResolveM2M(s.R, t, *col)
	if cfg == nil {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	pkVals, err := DecodeKey(t, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	srcRows, err := a.FetchByKey(t.Schema, t.PhysTab, t.Keys, pkVals, realColNames(t.Columns))
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
	tgtName := defs.MissingTable
	if !cfg.TargetMissing {
		tgtName = cfg.Target.Name
	}
	if !s.canRead(cfg.Junction.Name) || !s.canRead(tgtName) {
		writeErr(w, 403, "FORBIDDEN", "no read access to the related tables", nil)
		return
	}
	ja, err := s.R.Adapter(cfg.Junction)
	if err != nil {
		writeErr(w, 502, "CONN", "links fetch failed", err.Error())
		return
	}
	pairs, err := ja.FetchPairsByRef(cfg.Junction.Schema, cfg.Junction.PhysTab,
		col.M2M.SrcCol, col.M2M.TgtCol, []any{srcVal})
	ja.Close()
	if err != nil {
		writeErr(w, 502, "CONN", "links fetch failed", err.Error())
		return
	}
	var values []any
	for _, p := range pairs {
		if p.Ret != nil {
			values = append(values, p.Ret)
		}
	}
	if cfg.TargetMissing {
		// drifted target def: the old helper fetched the junction links,
		// then the target def lookup failed and surfaced as 502
		writeErr(w, 502, "CONN", "links fetch failed", "not found")
		return
	}
	var rows []map[string]any
	if len(values) > 0 {
		ta, err := s.R.Adapter(cfg.Target)
		if err != nil {
			writeErr(w, 502, "CONN", "links fetch failed", err.Error())
			return
		}
		rowsByKey, err := ta.FetchByRefValues(cfg.Target.Schema, cfg.Target.PhysTab,
			cfg.TargetRef, col.M2M.DisplayColumns, values)
		ta.Close()
		if err != nil {
			writeErr(w, 502, "CONN", "links fetch failed", err.Error())
			return
		}
		for _, v := range values {
			if row, ok := rowsByKey[rowValKey(v)]; ok {
				rows = append(rows, row)
			}
		}
	}
	if values == nil {
		values = []any{}
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"values": values, "rows": rows})
}
