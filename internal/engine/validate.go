package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"ku-crud/internal/defs"
)

var datetimeLayouts = []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateValue(ft string, v any, enum []string) error {
	if v == nil {
		return nil // NULL allowed; required-ness checked separately
	}
	switch ft {
	case "number":
		switch v.(type) {
		case float64, int, int64:
			return nil
		}
		return fmt.Errorf("expected number, got %T", v)
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", v)
		}
	case "text":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected text, got %T", v)
		}
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected datetime string, got %T", v)
		}
		for _, l := range datetimeLayouts {
			if _, err := time.Parse(l, s); err == nil {
				return nil
			}
		}
		return fmt.Errorf("datetime %q not ISO-8601-like", s)
	case "enum":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected enum string, got %T", v)
		}
		for _, o := range enum {
			if o == s {
				return nil
			}
		}
		return fmt.Errorf("%q not in enum options", s)
	case "uuid":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected uuid string, got %T", v)
		}
		if !uuidRe.MatchString(s) {
			return fmt.Errorf("%q is not a valid UUID", s)
		}
	case "json":
		switch v.(type) {
		case string:
			if !json.Valid([]byte(v.(string))) {
				return fmt.Errorf("invalid JSON")
			}
		case map[string]any, []any: // decoded JSON document from the request body
		default:
			return fmt.Errorf("expected JSON string or object/array, got %T", v)
		}
	default:
		return fmt.Errorf("unknown field type %q", ft)
	}
	return nil
}

// normalizeJSONValue converts a decoded JSON object/array into its compact
// text form so adapters always receive a string for json columns. Strings
// pass through untouched.
func normalizeJSONValue(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case map[string]any, []any:
		b, err := json.Marshal(t)
		if err != nil {
			return "", fmt.Errorf("json marshal: %w", err)
		}
		return string(b), nil
	}
	return "", fmt.Errorf("expected JSON string or object/array, got %T", v)
}

var (
	ruleEmailRe  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	ruleNumberRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
	ruleTextRe   = regexp.MustCompile(`^[A-Za-zÀ-ÿ ]+$`)
)

// stringForm renders a decoded JSON value the way validation rules see it.
func stringForm(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(v)
	}
}

// applyColumnValidations enforces the def's optional per-column rules on the
// string form of a value. Empty/nil values are skipped (required-ness is a
// separate flag). Called from EditablePayload (row writes) and CoerceValidate
// (CSV import) — the two validation funnels.
func applyColumnValidations(c defs.Column, v any) error {
	if v == nil || len(c.Validations) == 0 {
		return nil
	}
	s := stringForm(v)
	if s == "" {
		return nil // empty values are skipped like NULL (required-ness is separate)
	}
	for _, r := range c.Validations {
		var bad bool
		switch r.Type {
		case "email":
			bad = !ruleEmailRe.MatchString(s)
		case "min_len":
			bad = utf8.RuneCountInString(s) < r.Param
		case "max_len":
			bad = utf8.RuneCountInString(s) > r.Param
		case "number":
			bad = !ruleNumberRe.MatchString(s)
		case "text":
			bad = !ruleTextRe.MatchString(s)
		default:
			bad = true
		}
		if bad {
			return fmt.Errorf("%s: failed %q validation (%q)", c.Name, r.Type, s)
		}
	}
	return nil
}

// ValidateColumn type-checks v against c's field type (fk columns validate
// as their base type) and applies c's optional validation rules. It is the
// row-write validation funnel; errors carry the column name.
func ValidateColumn(c defs.Column, v any) error {
	ft := c.FieldType
	if ft == "fk" {
		ft = c.BaseType
	}
	if err := validateValue(ft, v, c.EnumOptions); err != nil {
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	return applyColumnValidations(c, v)
}

// EditablePayload validates body against editable columns and returns
// (cols, vals) in column-definition order. requireAll=true enforces required
// columns for INSERT / UPDATE. Any non-editable/unknown key is rejected unless it is a primary key during insert.
func EditablePayload(t *defs.Table, body map[string]any, isInsert bool) ([]string, []any, error) {
	editable := map[string]defs.Column{}
	isKey := map[string]bool{}
	for _, k := range t.Keys {
		isKey[k] = true
	}

	for _, c := range t.Columns {
		if c.FieldType == "m2m" {
			continue // virtual relation column — handled by syncM2MLinks
		}
		if c.Editable || (isInsert && isKey[c.Name]) {
			editable[c.Name] = c
		}
	}
	for k := range body {
		if _, ok := editable[k]; !ok {
			return nil, nil, fmt.Errorf("column %q is not editable/known", k)
		}
	}
	var names []string
	var vals []any
	for _, c := range t.Columns {
		if c.FieldType == "m2m" {
			continue // virtual relation column — handled by syncM2MLinks
		}
		if !c.Editable && !(isInsert && isKey[c.Name]) {
			continue
		}
		v, present := body[c.Name]
		if present {
			if err := ValidateColumn(c, v); err != nil {
				return nil, nil, err
			}
			if c.FieldType == "json" {
				s, err := normalizeJSONValue(v)
				if err != nil {
					return nil, nil, fmt.Errorf("%s: %w", c.Name, err)
				}
				v = s
			}
			names = append(names, c.Name)
			vals = append(vals, v)
		} else if isInsert && c.Required && !isKey[c.Name] {
			return nil, nil, fmt.Errorf("%s is required", c.Name)
		}
	}
	return names, vals, nil
}
