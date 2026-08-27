package engine

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/luthfi9251/ku-crud/core/defs"
	"github.com/luthfi9251/ku-crud/core/hooks"
)

// importRowError is one validation problem on one CSV row, keyed by the
// mapped column name.
type importRowError struct {
	Column  string `json:"column"`
	Message string `json:"message"`
}

type importRow struct {
	Values map[string]string `json:"values"`
	Valid  bool              `json:"valid"`
	Errors []importRowError  `json:"errors,omitempty"`
}

// ImportService renders the CSV import endpoints (preview/apply). Like
// ReadService/WriteService it is auth-free: the platform checks the
// create grant before calling. Hooks run through the engine Hooks
// interface — Guard once up-front, before-hooks synchronously per row
// (preview runs them to surface rejections; apply runs them exactly once
// at insert time), after-hooks scheduled per inserted row.
type ImportService struct {
	R Resolver
	H Hooks // may be nil
}

func (s *ImportService) guard(t *defs.Table) error {
	if s.H == nil {
		return nil
	}
	return s.H.Guard(t)
}

func (s *ImportService) runBefore(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error) {
	if s.H == nil {
		return row, nil
	}
	return s.H.RunBefore(ev, t, row)
}

func (s *ImportService) runAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload) {
	if s.H == nil {
		return
	}
	if err := s.H.RunAfter(ev, t, row); err != nil {
		slog.Warn("after-hook scheduling failed", "event", ev, "table", t.Name, "err", err.Error())
	}
}

// readImportFile parses the multipart request and returns the mapped
// validation context. mapping may be nil for preview (auto-mapping is used).
func readImportFile(w http.ResponseWriter, r *http.Request) (data []byte, mapping map[string]string, mode string, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, ImportMaxFile+(1<<20))
	if err := r.ParseMultipartForm(ImportMaxFile); err != nil {
		return nil, nil, "", errors.New("file too large or malformed multipart body")
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, nil, "", errors.New("missing file field")
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, ImportMaxFile+(1<<20)))
	if err != nil {
		return nil, nil, "", err
	}
	if len(data) > ImportMaxFile {
		return nil, nil, "", errors.New("file exceeds 5 MB limit")
	}
	if m := r.FormValue("mapping"); m != "" {
		mapping = map[string]string{}
		if err := json.Unmarshal([]byte(m), &mapping); err != nil {
			return nil, nil, "", errors.New("mapping must be a JSON object of header→column")
		}
	}
	mode = r.FormValue("mode")
	return data, mapping, mode, nil
}

// validateImportRows maps each CSV row onto column values, validates them,
// and returns per-row results plus the typed payloads for valid rows.
// runHooks controls before-create hook execution: preview runs hooks to
// surface rejections; apply skips them here (hooks run exactly once at
// insert time) so mutating hooks cannot compound.
func (s *ImportService) validateImportRows(t *defs.Table,
	headers []string, mapping map[string]string, records [][]string, runHooks bool,
) ([]importRow, []map[string]any) {
	byName := map[string]defs.Column{}
	for _, c := range t.Columns {
		byName[c.Name] = c
	}
	isKey := map[string]bool{}
	for _, k := range t.Keys {
		isKey[k] = true
	}
	// validate the mapping itself against the definition
	sanitized := map[string]string{}
	for h, colName := range mapping {
		if colName == "" {
			continue // explicitly ignored header
		}
		c, ok := byName[colName]
		if !ok || c.FieldType == "m2m" {
			continue // unknown target or virtual relation: treated as ignored
		}
		sanitized[h] = colName
	}

	// required columns that were never mapped to any header
	mappedTargets := map[string]bool{}
	for _, colName := range sanitized {
		mappedTargets[colName] = true
	}

	rows := make([]importRow, len(records))
	payloads := make([]map[string]any, len(records))
	for i, rec := range records {
		row := importRow{Values: map[string]string{}, Valid: true}
		for j, h := range headers {
			v := ""
			if j < len(rec) {
				v = rec[j]
			}
			row.Values[h] = v
		}
		payload := map[string]any{}
		for h, colName := range sanitized {
			raw, ok := row.Values[h]
			if !ok {
				continue
			}
			c := byName[colName]
			v, err := CoerceValidate(c, raw)
			if err != nil {
				row.Valid = false
				row.Errors = append(row.Errors, importRowError{Column: colName, Message: err.Error()})
				continue
			}
			if v == nil {
				if c.Required && !isKey[c.Name] {
					row.Valid = false
					row.Errors = append(row.Errors, importRowError{Column: colName, Message: "required"})
				}
				continue
			}
			payload[colName] = v
		}
		for _, c := range t.Columns {
			if c.FieldType == "m2m" {
				continue // virtual relation columns are not importable
			}
			if c.Required && !isKey[c.Name] && !mappedTargets[c.Name] {
				row.Valid = false
				row.Errors = append(row.Errors, importRowError{Column: c.Name, Message: "required column is not mapped"})
			}
		}
		rows[i] = row
		payloads[i] = payload
	}
	s.checkImportFKs(t, sanitized, rows, payloads)
	if runHooks {
		for i := range payloads {
			if !rows[i].Valid {
				continue
			}
			if _, err := s.runBefore(hooks.BeforeCreate, t, hooks.RowPayload{Values: payloads[i]}); err != nil {
				rows[i].Valid = false
				rows[i].Errors = append(rows[i].Errors, importRowError{Message: "hook: " + err.Error()})
			}
		}
	}
	return rows, payloads
}

// checkImportFKs batch-verifies fk values across all rows: one IN query per
// fk column, then per-row membership marking. Any resolution/adapter
// failure skips that column's check (historical best-effort semantics).
func (s *ImportService) checkImportFKs(t *defs.Table, mapping map[string]string, rows []importRow, payloads []map[string]any) {
	for _, c := range t.Columns {
		if c.FieldType != "fk" || c.FK == nil {
			continue
		}
		distinct := map[string]any{}
		for i := range payloads {
			if v, ok := payloads[i][c.Name]; ok && v != nil {
				distinct[rowValKey(v)] = v
			}
		}
		if len(distinct) == 0 {
			continue
		}
		target := t
		if c.FK.Table == defs.MissingTable {
			continue // drifted target def — historical best-effort skip
		}
		if c.FK.Table != "" {
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
		vals := make([]any, 0, len(distinct))
		for _, v := range distinct {
			vals = append(vals, v)
		}
		m, err := a.FetchByRefValues(target.Schema, target.PhysTab, c.FK.RefColumn, nil, vals)
		a.Close()
		if err != nil {
			continue
		}
		for i := range payloads {
			v, ok := payloads[i][c.Name]
			if !ok || v == nil {
				continue
			}
			if _, exists := m[rowValKey(v)]; !exists {
				rows[i].Valid = false
				rows[i].Errors = append(rows[i].Errors, importRowError{
					Column: c.Name, Message: "referenced row not found"})
			}
		}
	}
}

// PreviewImport renders POST import/preview: parse and auto-map the file,
// validate every row (before-hooks included so rejections surface) and
// report per-row results without writing anything.
func (s *ImportService) PreviewImport(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	if err := s.guard(t); err != nil {
		writeHookErr(w, err)
		return
	}
	data, mapping, _, err := readImportFile(w, r)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	comma, headers, records, err := ParseCSV(data)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	if mapping == nil {
		mapping = AutoMap(headers, t.Columns)
	}
	rows, _ := s.validateImportRows(t, headers, mapping, records, true)
	total, valid := len(rows), 0
	for _, row := range rows {
		if row.Valid {
			valid++
		}
	}
	writeJSON(w, 200, map[string]any{
		"delimiter": string(comma),
		"headers":   headers,
		"mapping":   mapping,
		"counts":    map[string]int{"total": total, "valid": valid, "invalid": total - valid},
		"rows":      rows,
	})
}

// ApplyImport renders POST import/apply: re-validate the mapped rows and
// insert them — mode "valid" skips invalid rows, "all" attempts every row
// best-effort. Hooks run exactly once per row, at insert time.
func (s *ImportService) ApplyImport(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	if err := s.guard(t); err != nil {
		writeHookErr(w, err)
		return
	}
	data, mapping, mode, err := readImportFile(w, r)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	if mapping == nil {
		writeErr(w, 400, "VALIDATION", "mapping is required", nil)
		return
	}
	if mode != "valid" && mode != "all" {
		writeErr(w, 400, "VALIDATION", `mode must be "valid" or "all"`, nil)
		return
	}
	_, headers, records, err := ParseCSV(data)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	rows, payloads := s.validateImportRows(t, headers, mapping, records, false)

	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	type failure struct {
		Row    int              `json:"row"`
		Errors []importRowError `json:"errors"`
	}
	inserted, failed := 0, 0
	var failures []failure
	var insertedPayloads []map[string]any
	for i := range records {
		if !rows[i].Valid && mode == "valid" {
			continue
		}
		// hooks run exactly once per row, here — before editablePayload so
		// mutations are part of the inserted values
		hooked, err := s.runBefore(hooks.BeforeCreate, t, hooks.RowPayload{Values: payloads[i]})
		if err != nil {
			failed++
			failures = append(failures, failure{Row: i, Errors: []importRowError{{Column: "", Message: err.Error()}}})
			continue
		}
		payloads[i] = hooked.Values
		names, vals, err := EditablePayload(t, payloads[i], true)
		if err != nil {
			failed++
			failures = append(failures, failure{Row: i, Errors: []importRowError{{Column: "", Message: err.Error()}}})
			continue
		}
		p := map[string]any{}
		for j := range names {
			p[names[j]] = vals[j]
		}
		if err := a.Insert(t.Schema, t.PhysTab, names, vals); err != nil {
			failed++
			msg := err.Error()
			if a.IsFKViolation(err) {
				msg = "referenced row not found (database constraint)"
			}
			failures = append(failures, failure{Row: i, Errors: []importRowError{{Column: "", Message: msg}}})
			continue
		}
		inserted++
		insertedPayloads = append(insertedPayloads, p)
		runSyncAfter(s.H, hooks.AfterCreate, t, hooks.RowPayload{Values: payloads[i]}, "")
	}
	for _, p := range insertedPayloads {
		s.runAfter(hooks.AfterCreate, t, hooks.RowPayload{Values: p})
	}
	if failures == nil {
		failures = []failure{}
	}
	writeJSON(w, 200, map[string]any{"inserted": inserted, "failed": failed, "failures": failures})
}
