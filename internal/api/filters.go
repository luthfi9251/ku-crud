package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

// maxFilters bounds one request's filter count (AND combination).
const maxFilters = 10

// filterOpsByType is the operator allowlist per column field type. The
// frontend mirrors this matrix exactly.
var filterOpsByType = map[string][]string{
	"text":     {"eq", "neq", "contains", "in"},
	"uuid":     {"eq", "neq", "contains", "in"},
	"number":   {"eq", "neq", "gt", "gte", "lt", "lte", "between", "in"},
	"datetime": {"eq", "gt", "lt", "between"},
	"boolean":  {"eq"},
	"enum":     {"eq", "neq", "in"},
	"fk":       {"contains", "eq"},
}

type filterInput struct {
	Column string   `json:"column"`
	Op     string   `json:"op"`
	Values []string `json:"values"`
}

// parseFilters validates the `filters` query param (JSON array) against the
// stored definition and coerces values per column type. Column names resolve
// ONLY from metadata; operators from the allowlist; values become bind
// parameters downstream — same anti-injection posture as sort/search.
func (s *Server) parseFilters(def *meta.TableDef, cols []meta.ColumnDef, u CtxUser, raw string) ([]ds.ColumnFilter, string) {
	if strings.TrimSpace(raw) == "" {
		return nil, ""
	}
	var in []filterInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, "filters must be a JSON array of {column, op, values}"
	}
	if len(in) > maxFilters {
		return nil, fmt.Sprintf("at most %d filters per request", maxFilters)
	}
	byName := map[string]meta.ColumnDef{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	out := make([]ds.ColumnFilter, 0, len(in))
	for _, f := range in {
		c, ok := byName[f.Column]
		if !ok {
			return nil, fmt.Sprintf("filter: unknown column %q", f.Column)
		}
		if c.FieldType == "m2m" || c.FieldType == "json" || c.IsComputed {
			return nil, fmt.Sprintf("filter: column %q cannot be filtered", f.Column)
		}
		okOp := false
		for _, o := range filterOpsByType[c.FieldType] {
			if o == f.Op {
				okOp = true
			}
		}
		if !okOp {
			return nil, fmt.Sprintf("filter: op %q not supported for column %q (%s)", f.Op, f.Column, c.FieldType)
		}
		if f.Op == "in" {
			if len(f.Values) < 1 || len(f.Values) > 50 {
				return nil, fmt.Sprintf("filter: column %q in-list needs 1..50 values", f.Column)
			}
		} else if need := map[string]int{"between": 2}[f.Op]; need == 2 {
			if len(f.Values) != 2 {
				return nil, fmt.Sprintf("filter: column %q op %q needs exactly 2 values", f.Column, f.Op)
			}
		} else if len(f.Values) != 1 {
			return nil, fmt.Sprintf("filter: column %q op %q needs exactly 1 value", f.Column, f.Op)
		}

		ft := c.FieldType
		if ft == "fk" {
			ft = "text" // fk ops match the target's display value (text), not the raw ref
		}
		vals := make([]any, len(f.Values))
		for i, raw := range f.Values {
			switch ft {
			case "number":
				n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
				if err != nil {
					return nil, fmt.Sprintf("filter: value %q for column %q is not a number", raw, f.Column)
				}
				vals[i] = n
			case "boolean":
				if raw != "true" && raw != "false" {
					return nil, fmt.Sprintf("filter: value %q for column %q must be true/false", raw, f.Column)
				}
				vals[i] = raw == "true"
			case "datetime":
				valid := false
				for _, l := range datetimeLayouts {
					if _, err := time.Parse(l, raw); err == nil {
						valid = true
						break
					}
				}
				if !valid {
					return nil, fmt.Sprintf("filter: value %q for column %q is not a datetime", raw, f.Column)
				}
				vals[i] = raw
			default:
				vals[i] = raw
			}
		}
		// date-range inclusivity: a date-only upper bound matches the whole day
		if c.FieldType == "datetime" && (f.Op == "between" || f.Op == "lte") {
			last := len(vals) - 1
			if sv, ok := vals[last].(string); ok && len(sv) == 10 {
				vals[last] = sv + " 23:59:59"
			}
		}

		cf := ds.ColumnFilter{Column: c.Name, Op: f.Op, Values: vals}
		if c.FieldType == "fk" {
			targetID := c.FKTableDefID
			if targetID == meta.SelfRef || targetID == def.ID {
				targetID = def.ID
			}
			if !s.hasTablePerm(u, targetID, "read") {
				return nil, fmt.Sprintf("filter: column %q requires read access to its target table", f.Column)
			}
			var schema, table string
			if targetID == def.ID {
				schema, table = def.SchemaName, def.TableName
			} else {
				tdef, _, err := s.store.GetTableDef(targetID)
				if err != nil {
					return nil, fmt.Sprintf("filter: column %q target definition not found", f.Column)
				}
				schema, table = tdef.SchemaName, tdef.TableName
			}
			cf.Join = &ds.FKJoin{Schema: schema, Table: table,
				RefColumn: c.FKRefColumn, DisplayColumns: c.FKDisplayColumns}
		}
		out = append(out, cf)
	}
	return out, ""
}
