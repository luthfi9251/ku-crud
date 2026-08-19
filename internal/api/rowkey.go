package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"ku-crud/internal/meta"
)

// Row keys travel in the URL path as base64url(JSON array of key value
// strings): ["3"] → WyIzIl0. Composite keys are just longer arrays. The
// encoding is opaque to the router (no separators to collide with row data).
func encodeRowKey(vals []string) string {
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

// rowKeyVals decodes the path key and coerces each part to its column type.
func rowKeyVals(def *meta.TableDef, cols []meta.ColumnDef, raw string) ([]any, error) {
	parts, err := decodeRowKey(raw)
	if err != nil {
		return nil, err
	}
	if len(parts) != len(def.KeyColumns) {
		return nil, fmt.Errorf("key has %d values, definition has %d key columns", len(parts), len(def.KeyColumns))
	}
	vals := make([]any, len(parts))
	for i, col := range def.KeyColumns {
		v, err := coercePK(fieldTypeOf(cols, col), parts[i])
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

// rowKeyString renders a row's key values as the JSON array stored in audit
// row_pk, e.g. `["1","a"]`.
func rowKeyString(def *meta.TableDef, row map[string]any) string {
	vals := make([]string, len(def.KeyColumns))
	for i, c := range def.KeyColumns {
		if row[c] == nil {
			vals[i] = ""
			continue
		}
		vals[i] = fmt.Sprint(row[c])
	}
	return string(mustJSON(vals))
}
