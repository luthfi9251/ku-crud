package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"ku-crud/internal/hooks"
	"ku-crud/internal/meta"
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

// readImportFile parses the multipart request and returns the mapped
// validation context. mapping may be nil for preview (auto-mapping is used).
func (s *Server) readImportFile(w http.ResponseWriter, r *http.Request) (data []byte, mapping map[string]string, mode string, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, importMaxFile+(1<<20))
	if err := r.ParseMultipartForm(importMaxFile); err != nil {
		return nil, nil, "", errors.New("file too large or malformed multipart body")
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, nil, "", errors.New("missing file field")
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, importMaxFile+(1<<20)))
	if err != nil {
		return nil, nil, "", err
	}
	if len(data) > importMaxFile {
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

// importCtx loads def/cols and enforces the create grant.
func (s *Server) importCtx(w http.ResponseWriter, r *http.Request) (*meta.TableDef, []meta.ColumnDef, bool) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return nil, nil, false
	}
	if writeQueryReadOnly(w, def) {
		return nil, nil, false
	}
	if !s.hasTablePerm(u, def.ID, "create") {
		writeErr(w, 403, "FORBIDDEN", "no create access to this table", nil)
		return nil, nil, false
	}
	return def, cols, true
}

// validateImportRows maps each CSV row onto column values, validates them,
// and returns per-row results plus the typed payloads for valid rows.
// runHooks controls before-create hook execution: preview runs hooks to
// surface rejections; apply skips them here (hooks run exactly once at
// insert time) so mutating hooks cannot compound.
func (s *Server) validateImportRows(ctx context.Context, u CtxUser, def *meta.TableDef, cols []meta.ColumnDef,
	headers []string, mapping map[string]string, records [][]string, runHooks bool,
) ([]importRow, []map[string]any) {
	byName := map[string]meta.ColumnDef{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	isKey := map[string]bool{}
	for _, k := range def.KeyColumns {
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
			v, err := coerceValidate(c, raw)
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
		for _, c := range cols {
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
	s.checkImportFKs(cols, sanitized, rows, payloads)
	if runHooks {
		for i := range payloads {
			if !rows[i].Valid {
				continue
			}
			if _, err := s.runBefore(ctx, u, def, cols, hooks.BeforeCreate, payloads[i], nil); err != nil {
				rows[i].Valid = false
				rows[i].Errors = append(rows[i].Errors, importRowError{Message: "hook: " + err.Error()})
			}
		}
	}
	return rows, payloads
}

// checkImportFKs batch-verifies fk values across all rows: one IN query per
// fk column, then per-row membership marking.
func (s *Server) checkImportFKs(cols []meta.ColumnDef, mapping map[string]string, rows []importRow, payloads []map[string]any) {
	for _, c := range cols {
		if c.FieldType != "fk" || c.FKTableDefID <= 0 {
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
		target, _, err := s.store.GetTableDef(c.FKTableDefID)
		if err != nil {
			continue
		}
		a, err := s.liveAdapter(target.DatasourceID)
		if err != nil {
			continue
		}
		vals := make([]any, 0, len(distinct))
		for _, v := range distinct {
			vals = append(vals, v)
		}
		m, err := a.FetchByRefValues(target.SchemaName, target.TableName, c.FKRefColumn, nil, vals)
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

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, ok := s.importCtx(w, r)
	if !ok {
		return
	}
	if err := s.hookGuard(def); err != nil {
		writeHookErr(w, err)
		return
	}
	data, mapping, _, err := s.readImportFile(w, r)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	comma, headers, records, err := parseCSV(data)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	if mapping == nil {
		mapping = autoMap(headers, cols)
	}
	rows, _ := s.validateImportRows(r.Context(), u, def, cols, headers, mapping, records, true)
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

func (s *Server) handleImportApply(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, ok := s.importCtx(w, r)
	if !ok {
		return
	}
	if err := s.hookGuard(def); err != nil {
		writeHookErr(w, err)
		return
	}
	data, mapping, mode, err := s.readImportFile(w, r)
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
	_, headers, records, err := parseCSV(data)
	if err != nil {
		writeErr(w, 400, "IMPORT_BAD_CSV", err.Error(), nil)
		return
	}
	rows, payloads := s.validateImportRows(r.Context(), u, def, cols, headers, mapping, records, false)

	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
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
		hooked, err := s.runBefore(r.Context(), u, def, cols, hooks.BeforeCreate, payloads[i], nil)
		if err != nil {
			failed++
			failures = append(failures, failure{Row: i, Errors: []importRowError{{Column: "", Message: err.Error()}}})
			continue
		}
		payloads[i] = hooked
		names, vals, err := editablePayload(payloads[i], cols, def.KeyColumns, true)
		if err != nil {
			failed++
			failures = append(failures, failure{Row: i, Errors: []importRowError{{Column: "", Message: err.Error()}}})
			continue
		}
		p := map[string]any{}
		for j := range names {
			p[names[j]] = vals[j]
		}
		if err := a.Insert(def.SchemaName, def.TableName, names, vals); err != nil {
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
		s.auditBestEffort(u, def.ID, "INSERT", "", nil, payloads[i])
	}
	for _, p := range insertedPayloads {
		s.enqueueAfter(u, def, hooks.AfterCreate, nil, p)
	}
	if failures == nil {
		failures = []failure{}
	}
	writeJSON(w, 200, map[string]any{"inserted": inserted, "failed": failed, "failures": failures})
}
