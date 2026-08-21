package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"ku-crud/internal/meta"
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

func coercePK(ft, raw string) (any, error) {
	if ft == "number" {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i, nil
		}
		return strconv.ParseFloat(raw, 64)
	}
	return raw, nil
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
// separate flag). Called from editablePayload (row writes) and coerceValidate
// (CSV import) — the two validation funnels.
func applyColumnValidations(c meta.ColumnDef, v any) error {
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
