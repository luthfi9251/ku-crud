package api

import "testing"

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
