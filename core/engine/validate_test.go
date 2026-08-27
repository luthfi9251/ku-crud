package engine

import (
	"strings"
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
)

func TestValidateValueUUID(t *testing.T) {
	ok := []any{
		"123e4567-e89b-12d3-a456-426614174000",
		"00000000-0000-0000-0000-000000000000", // nil UUID
		"123E4567-E89B-12D3-A456-426614174000", // uppercase
	}
	for _, v := range ok {
		if err := validateValue("uuid", v, nil); err != nil {
			t.Errorf("uuid %v: unexpected error %v", v, err)
		}
	}
	bad := []any{
		"not-a-uuid",
		"123e4567e89b12d3a456426614174000",      // no dashes
		"123e4567-e89b-12d3-a456-42661417400",   // 35 chars
		"123e4567-e89b-12d3-a456-4266141740000", // 37 chars
		123,
		nil, // nil handled by caller (required-ness), validateValue(nil) returns nil early — see below
	}
	for _, v := range bad[:len(bad)-1] {
		if err := validateValue("uuid", v, nil); err == nil {
			t.Errorf("uuid %v: expected error", v)
		}
	}
}

func TestValidateValueJSON(t *testing.T) {
	ok := []any{
		`{"a":1}`,
		`[1,2,3]`,
		`"plain string is valid json"`,
		`null`,
		`  {"spaced": true}  `,
		map[string]any{"a": 1}, // decoded JSON object from request body
		[]any{1, 2},
	}
	for _, v := range ok {
		if err := validateValue("json", v, nil); err != nil {
			t.Errorf("json %v: unexpected error %v", v, err)
		}
	}
	bad := []any{
		`{a}`,
		`{"a":}`,
		`[1,2`,
		123,  // scalars must arrive as JSON text or object/array
		true, // ditto
	}
	for _, v := range bad {
		if err := validateValue("json", v, nil); err == nil {
			t.Errorf("json %v: expected error", v)
		}
	}
}

func TestNormalizeJSONValue(t *testing.T) {
	// strings pass through untouched
	s, err := normalizeJSONValue(`{"a": 1}`)
	if err != nil || s != `{"a": 1}` {
		t.Fatalf("string passthrough: %q %v", s, err)
	}
	// objects/arrays are marshaled to compact JSON text
	m, err := normalizeJSONValue(map[string]any{"b": 2})
	if err != nil || m != `{"b":2}` {
		t.Fatalf("object marshal: %q %v", m, err)
	}
	sl, err := normalizeJSONValue([]any{1, "x"})
	if err != nil || sl != `[1,"x"]` {
		t.Fatalf("array marshal: %q %v", sl, err)
	}
}

func TestApplyColumnValidations(t *testing.T) {
	cases := []struct {
		name  string
		rules []defs.ValidationRule
		v     any
		ok    bool
	}{
		{"email ok", []defs.ValidationRule{{Type: "email"}}, "a@b.co", true},
		{"email bad", []defs.ValidationRule{{Type: "email"}}, "nope", false},
		{"min_len ok", []defs.ValidationRule{{Type: "min_len", Param: 3}}, "abc", true},
		{"min_len fail", []defs.ValidationRule{{Type: "min_len", Param: 3}}, "ab", false},
		{"max_len fail", []defs.ValidationRule{{Type: "max_len", Param: 2}}, "abc", false},
		{"number ok", []defs.ValidationRule{{Type: "number"}}, "-12.5", true},
		{"number bad", []defs.ValidationRule{{Type: "number"}}, "12a", false},
		{"text ok", []defs.ValidationRule{{Type: "text"}}, "Héllo World", true},
		{"text bad digit", []defs.ValidationRule{{Type: "text"}}, "abc1", false},
		{"empty skipped", []defs.ValidationRule{{Type: "email"}}, "", true},
		{"nil skipped", []defs.ValidationRule{{Type: "email"}}, nil, true},
		{"float stringForm", []defs.ValidationRule{{Type: "number"}}, 3.5, true},
	}
	for _, c := range cases {
		err := applyColumnValidations(defs.Column{Name: "c", FieldType: "text", Validations: c.rules}, c.v)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestValidateColumn(t *testing.T) {
	// fk columns validate against their base type; errors carry the column name
	fk := defs.Column{Name: "customer_id", FieldType: "fk", BaseType: "number"}
	if err := ValidateColumn(fk, float64(3)); err != nil {
		t.Fatalf("fk number: %v", err)
	}
	err := ValidateColumn(fk, "x")
	if err == nil || !strings.Contains(err.Error(), "customer_id:") {
		t.Fatalf("fk type error should carry the column name: %v", err)
	}
	// per-column rules run through the same funnel
	c := defs.Column{Name: "email", FieldType: "text", Validations: []defs.ValidationRule{{Type: "email"}}}
	if err := ValidateColumn(c, "nope"); err == nil {
		t.Fatal("rule violation must fail")
	}
	if err := ValidateColumn(c, nil); err != nil {
		t.Fatalf("nil skipped: %v", err)
	}
}

func TestEditablePayload(t *testing.T) {
	tbl := &defs.Table{Keys: []string{"id"}, Columns: []defs.Column{
		{Name: "id", FieldType: "number", Editable: false},
		{Name: "name", FieldType: "text", Editable: true, Required: true},
		{Name: "calc", FieldType: "number", IsComputed: true},
		{Name: "rel", FieldType: "m2m"},
	}}
	// insert accepts key columns and enforces required ones
	names, vals, err := EditablePayload(tbl, map[string]any{"id": float64(1), "name": "jo"}, true)
	if err != nil || len(names) != 2 || vals[1] != "jo" {
		t.Fatalf("insert: %v %v %v", names, vals, err)
	}
	// unknown / non-editable keys rejected
	if _, _, err := EditablePayload(tbl, map[string]any{"name": "jo", "calc": 1.0}, false); err == nil {
		t.Fatal("computed column must not be editable")
	}
	if _, _, err := EditablePayload(tbl, map[string]any{"nope": 1}, false); err == nil {
		t.Fatal("unknown column must be rejected")
	}
	// update rejects missing required column, insert requires it
	if _, _, err := EditablePayload(tbl, map[string]any{"id": float64(1)}, true); err == nil {
		t.Fatal("insert must enforce required columns")
	}
	if _, _, err := EditablePayload(tbl, map[string]any{}, false); err != nil {
		t.Fatalf("partial update allowed: %v", err)
	}
}
