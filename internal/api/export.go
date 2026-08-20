package api

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

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

// handleRowExport streams the full result set matching the active
// search/sort as a CSV download (all pages, not just the current one).
func (s *Server) handleRowExport(w http.ResponseWriter, r *http.Request) {
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
	lp := ds.ListParams{Schema: def.SchemaName, Table: def.TableName, Columns: realColNames(cols),
		Searchable: searchable, Search: q.Get("search"),
		SortCol: sortCol, SortDir: sortDir, Limit: exportRowCap + 1, Offset: 0}

	total, err := a.CountRows(lp)
	if err != nil {
		writeErr(w, 502, "CONN", "count failed", err.Error())
		return
	}
	if total > exportRowCap {
		writeErr(w, 400, "EXPORT_TOO_LARGE",
			fmt.Sprintf("export is limited to %d rows; this query matches %d — narrow the search", exportRowCap, total), nil)
		return
	}
	rows, err := a.ListRows(lp)
	if err != nil {
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}

	// resolve fk display values in bounded chunks (IN-lists stay small)
	var visible []meta.ColumnDef
	for _, c := range cols {
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
		for col, m := range s.buildRels(u, visible, rows[i:end]) {
			if rels[col] == nil {
				rels[col] = map[string]map[string]any{}
			}
			for k, v := range m {
				rels[col][k] = v
			}
		}
		for col, m := range s.buildM2MRels(u, def, visible, rows[i:end]) {
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
		fmt.Sprintf(`attachment; filename="%s-%s.csv"`, def.TableName, time.Now().Format("20060102-150405")))
	// UTF-8 BOM so Excel renders Unicode correctly
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	header := make([]string, len(visible))
	m2mCfgs := map[string]*m2mConfig{}
	for i, c := range visible {
		header[i] = c.Name
		if c.FieldType == "m2m" {
			if cfg, _ := s.resolveM2M(def, c); cfg != nil {
				m2mCfgs[c.Name] = cfg
			}
		}
	}
	cw.Write(header)
	for _, row := range rows {
		rec := make([]string, len(visible))
		for i, c := range visible {
			v := row[c.Name]
			if c.FieldType == "fk" && v != nil {
				if rel, ok := rels[c.Name][rowValKey(v)]; ok {
					rec[i] = joinDisplay(rel, c.FKDisplayColumns, c.FKRefColumn)
					continue
				}
			}
			if c.FieldType == "m2m" {
				if cfg := m2mCfgs[c.Name]; cfg != nil {
					if list, ok := m2mRels[c.Name][rowValKey(row[cfg.SrcRef])]; ok {
						parts := make([]string, len(list))
						for j, tr := range list {
							parts[j] = joinDisplay(tr, c.M2MDisplayColumns, cfg.TargetRef)
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
