package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ku-crud/internal/meta"
)

// importMaxRows / importMaxFile bound CSV imports.
const importMaxRows = 10000
const importMaxFile = 5 << 20 // 5 MB

// sniffDelimiter picks the CSV delimiter for the first line: the candidate
// (comma, semicolon, tab) occurring most often outside quoted sections wins;
// ties fall back to comma.
func sniffDelimiter(firstLine string) rune {
	counts := map[rune]int{}
	inQuote := false
	for _, r := range firstLine {
		switch {
		case r == '"':
			inQuote = !inQuote
		case !inQuote && (r == ',' || r == ';' || r == '\t'):
			counts[r]++
		}
	}
	best, bestN := ',', 0
	for _, cand := range []rune{',', ';', '\t'} {
		if counts[cand] > bestN {
			best, bestN = cand, counts[cand]
		}
	}
	return best
}

// parseCSV sniffs the delimiter from the first line and parses the whole
// file. Returns headers and data rows (all values as strings).
func parseCSV(data []byte) (rune, []string, [][]string, error) {
	firstLine := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		firstLine = data[:i]
	}
	comma := sniffDelimiter(string(firstLine))
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = comma
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return comma, nil, nil, fmt.Errorf("csv parse: %w", err)
	}
	if len(records) < 1 {
		return comma, nil, nil, fmt.Errorf("csv file is empty")
	}
	if len(records)-1 > importMaxRows {
		return comma, nil, nil, fmt.Errorf("csv has %d data rows; the limit is %d", len(records)-1, importMaxRows)
	}
	return comma, records[0], records[1:], nil
}

// autoMap proposes header→columnName mappings: exact name, then trimmed
// case-insensitive name, then column label (case-insensitive). Unmapped
// headers map to "". Virtual m2m columns are not importable.
func autoMap(headers []string, cols []meta.ColumnDef) map[string]string {
	byLowerName := map[string]string{}
	byLowerLabel := map[string]string{}
	for _, c := range cols {
		if c.FieldType == "m2m" {
			continue
		}
		byLowerName[strings.ToLower(strings.TrimSpace(c.Name))] = c.Name
		byLowerLabel[strings.ToLower(strings.TrimSpace(c.Label))] = c.Name
	}
	out := map[string]string{}
	for _, h := range headers {
		key := strings.ToLower(strings.TrimSpace(h))
		if name, ok := byLowerName[key]; ok {
			out[h] = name
		} else if name, ok := byLowerLabel[key]; ok {
			out[h] = name
		} else {
			out[h] = ""
		}
	}
	return out
}

// coerceValidate converts one CSV cell into the typed value for the column,
// then applies the column's optional validation rules (same rules as the row
// write path).
func coerceValidate(c meta.ColumnDef, raw string) (any, error) {
	v, err := coerceValidateTyped(c, raw)
	if err != nil {
		return nil, err
	}
	if err := applyColumnValidations(c, v); err != nil {
		return nil, err
	}
	return v, nil
}

// coerceValidateTyped converts one CSV cell (string) into the typed value for
// the column, applying the same per-type validation as the row write path.
// Empty strings become nil (NULL); required-ness is checked separately.
func coerceValidateTyped(c meta.ColumnDef, raw string) (any, error) {
	if raw == "" {
		return nil, nil
	}
	ft := c.FieldType
	if ft == "fk" {
		ft = c.BaseType
	}
	switch ft {
	case "boolean":
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true", "t", "1", "yes":
			return true, nil
		case "false", "f", "0", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a boolean (true/false/1/0)", raw)
	case "number":
		s := strings.TrimSpace(raw)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return float64(i), nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return f, nil
	case "datetime":
		s := strings.TrimSpace(raw)
		for _, l := range datetimeLayouts {
			if _, err := time.Parse(l, s); err == nil {
				return s, nil
			}
		}
		return nil, fmt.Errorf("%q is not an ISO-8601-like datetime", raw)
	case "uuid", "text", "enum":
		if err := validateValue(ft, raw, c.EnumOptions); err != nil {
			return nil, err
		}
		return raw, nil
	case "json":
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			switch v.(type) {
			case map[string]any, []any:
				b, err := json.Marshal(v)
				if err != nil {
					return nil, err
				}
				return string(b), nil // compact normalized form
			}
		}
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("invalid JSON")
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("unsupported field type %q for import", c.FieldType)
	}
}
