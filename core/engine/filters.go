package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/luthfi9251/ku-crud/core/defs"
	"github.com/luthfi9251/ku-crud/core/ds"
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

// FKJoinResolver resolves one fk column's join target. The caller owns it:
// it enforces the read grant on the target table and resolves the target's
// physical schema/table names.
type FKJoinResolver func(column string) (*ds.FKJoin, error)

// ParseFilters validates the `filters` param (JSON array) against the
// stored definition and coerces values per column type. Column names resolve
// ONLY from metadata; operators from the allowlist; values become bind
// parameters downstream — same anti-injection posture as sort/search.
// fk columns obtain their join via fkJoin; a nil fkJoin rejects them like a
// missing read grant.
func ParseFilters(t *defs.Table, raw string, fkJoin FKJoinResolver) ([]ds.ColumnFilter, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var in []filterInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("filters must be a JSON array of {column, op, values}")
	}
	if len(in) > maxFilters {
		return nil, fmt.Errorf("at most %d filters per request", maxFilters)
	}
	byName := map[string]defs.Column{}
	for _, c := range t.Columns {
		byName[c.Name] = c
	}
	out := make([]ds.ColumnFilter, 0, len(in))
	for _, f := range in {
		c, ok := byName[f.Column]
		if !ok {
			return nil, fmt.Errorf("filter: unknown column %q", f.Column)
		}
		if c.FieldType == "m2m" || c.FieldType == "json" || c.IsComputed {
			return nil, fmt.Errorf("filter: column %q cannot be filtered", f.Column)
		}
		okOp := false
		for _, o := range filterOpsByType[c.FieldType] {
			if o == f.Op {
				okOp = true
			}
		}
		if !okOp {
			return nil, fmt.Errorf("filter: op %q not supported for column %q (%s)", f.Op, f.Column, c.FieldType)
		}
		if f.Op == "in" {
			if len(f.Values) < 1 || len(f.Values) > 50 {
				return nil, fmt.Errorf("filter: column %q in-list needs 1..50 values", f.Column)
			}
		} else if need := map[string]int{"between": 2}[f.Op]; need == 2 {
			if len(f.Values) != 2 {
				return nil, fmt.Errorf("filter: column %q op %q needs exactly 2 values", f.Column, f.Op)
			}
		} else if len(f.Values) != 1 {
			return nil, fmt.Errorf("filter: column %q op %q needs exactly 1 value", f.Column, f.Op)
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
					return nil, fmt.Errorf("filter: value %q for column %q is not a number", raw, f.Column)
				}
				vals[i] = n
			case "boolean":
				if raw != "true" && raw != "false" {
					return nil, fmt.Errorf("filter: value %q for column %q must be true/false", raw, f.Column)
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
					return nil, fmt.Errorf("filter: value %q for column %q is not a datetime", raw, f.Column)
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
			if fkJoin == nil {
				return nil, fmt.Errorf("filter: column %q requires read access to its target table", f.Column)
			}
			join, err := fkJoin(c.Name)
			if err != nil {
				return nil, err
			}
			cf.Join = join
		}
		out = append(out, cf)
	}
	return out, nil
}

// ResolveSort picks the effective sort for a list query: an explicit sortable
// column from the request wins; otherwise the definition's default sort when
// it is still a defined, sortable column; otherwise the first key column ASC.
// Direction strings are the platform's persisted uppercase "ASC"/"DESC"
// (ds adapters reject anything else); other casings fall back to ASC —
// behavior is pinned, do not normalize.
func ResolveSort(t *defs.Table, sortCol, sortDir string) (string, string) {
	byName := map[string]defs.Column{}
	for _, c := range t.Columns {
		byName[c.Name] = c
	}
	if byName[sortCol].Sortable {
		if sortDir != "ASC" && sortDir != "DESC" {
			sortDir = "ASC"
		}
		return sortCol, sortDir
	}
	if c, ok := byName[t.DefaultSortCol]; ok && c.Sortable {
		d := t.DefaultSortDir
		if d != "ASC" && d != "DESC" {
			d = "ASC"
		}
		return t.DefaultSortCol, d
	}
	if len(t.Keys) > 0 {
		return t.Keys[0], "ASC"
	}
	for _, c := range t.Columns {
		if c.Sortable && c.FieldType != "m2m" && !c.IsComputed {
			return c.Name, "ASC"
		}
	}
	return "", ""
}
