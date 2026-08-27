package engine

import (
	"reflect"
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
)

func TestRowKeyCodec(t *testing.T) {
	enc := EncodeRowKey([]string{"3"})
	if enc != "WyIzIl0" {
		t.Fatalf("single key: %q", enc)
	}
	got, err := decodeRowKey(enc)
	if err != nil || !reflect.DeepEqual(got, []string{"3"}) {
		t.Fatalf("decode: %v %v", got, err)
	}

	enc = EncodeRowKey([]string{"1", "a b/c"})
	got, err = decodeRowKey(enc)
	if err != nil || !reflect.DeepEqual(got, []string{"1", "a b/c"}) {
		t.Fatalf("composite: %v %v", got, err)
	}

	for _, bad := range []string{"", "!!!", "Ww", "e30", "WyIzLDMsNV0"} {
		if _, err := decodeRowKey(bad); err == nil {
			t.Fatalf("decode(%q) should fail", bad)
		}
	}
}

func TestRowKeyVals(t *testing.T) {
	tbl := &defs.Table{Keys: []string{"id", "code"}, Columns: []defs.Column{
		{Name: "id", Label: "id", FieldType: "number"},
		{Name: "code", Label: "code", FieldType: "text"},
	}}
	vals, err := DecodeKey(tbl, EncodeRowKey([]string{"42", "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(vals, []any{int64(42), "x"}) {
		t.Fatalf("vals=%#v", vals)
	}
	// wrong arity rejected
	if _, err := DecodeKey(tbl, EncodeRowKey([]string{"42"})); err == nil {
		t.Fatal("wrong arity accepted")
	}
}

func TestRowKeyString(t *testing.T) {
	tbl := &defs.Table{Keys: []string{"a", "b"}}
	row := map[string]any{"a": "1", "b": "x", "c": "other"}
	got, err := EncodeKey(tbl, row)
	if err != nil || got != `["1","x"]` {
		t.Fatalf("got %q %v", got, err)
	}
}
