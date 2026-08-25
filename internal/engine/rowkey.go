package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"ku-crud/internal/defs"
)

// Row keys travel in the URL path as base64url(JSON array of key value
// strings): ["3"] → WyIzIl0. Composite keys are just longer arrays. The
// encoding is opaque to the router (no separators to collide with row data).
func EncodeRowKey(vals []string) string {
	b, _ := json.Marshal(vals)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeRowKey(raw string) ([]string, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) == 0 {
		return nil, fmt.Errorf("bad key encoding")
	}
	var vals []string
	if err := json.Unmarshal(b, &vals); err != nil {
		return nil, fmt.Errorf("bad key encoding")
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("empty key")
	}
	return vals, nil
}

// DecodeKey decodes the path key and coerces each part to its column type.
func DecodeKey(t *defs.Table, raw string) ([]any, error) {
	parts, err := decodeRowKey(raw)
	if err != nil {
		return nil, err
	}
	if len(parts) != len(t.Keys) {
		return nil, fmt.Errorf("key has %d values, definition has %d key columns", len(parts), len(t.Keys))
	}
	vals := make([]any, len(parts))
	for i, col := range t.Keys {
		v, err := coercePK(fieldTypeOf(t.Columns, col), parts[i])
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

// EncodeKey renders a row's key values as the JSON array stored in audit
// row_pk, e.g. `["1","a"]`.
func EncodeKey(t *defs.Table, vals map[string]any) (string, error) {
	out := make([]string, len(t.Keys))
	for i, c := range t.Keys {
		if vals[c] == nil {
			out[i] = ""
			continue
		}
		out[i] = fmt.Sprint(vals[c])
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fieldTypeOf returns a key column's field type ("text" when unknown).
func fieldTypeOf(cols []defs.Column, name string) string {
	for _, c := range cols {
		if c.Name == name {
			return c.FieldType
		}
	}
	return "text"
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
