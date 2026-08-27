package engine

import (
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
)

func TestCompileComputed(t *testing.T) {
	cols := []defs.Column{
		{Name: "price", FieldType: "number"},
		{Name: "qty", FieldType: "number"},
		{Name: "first", FieldType: "text"},
		{Name: "last", FieldType: "text"},
	}
	row := map[string]any{"price": float64(10), "qty": float64(3), "first": "Jane", "last": "Doe"}

	cases := []struct {
		formula string
		want    any
	}{
		{"price * qty", float64(30)},
		{"price + qty * 2", float64(16)},
		{"(price + qty) * 2", float64(26)},
		{"price - qty", float64(7)},
		{"price / qty", float64(10) / 3},
		{"-price + 5", float64(-5)},
		{`CONCAT(first, " ", last)`, "Jane Doe"},
		{`CONCAT("a", first)`, "aJane"},
	}
	for _, tc := range cases {
		ft, fn, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: tc.formula}, cols)
		if err != nil {
			t.Fatalf("%q: compile err %v", tc.formula, err)
		}
		got := fn(row)
		if got != tc.want {
			t.Fatalf("%q: got %v want %v (ft=%s)", tc.formula, got, tc.want, ft)
		}
	}
}

func TestComputedErrorsAndNulls(t *testing.T) {
	cols := []defs.Column{
		{Name: "price", FieldType: "number"},
		{Name: "name", FieldType: "text"},
	}
	row := map[string]any{"price": nil, "name": "x"}

	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: "bogus("}, cols); err == nil {
		t.Fatal("unbalanced paren must fail compile")
	}
	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: "missing * 2"}, cols); err == nil {
		t.Fatal("unknown ident must fail compile")
	}
	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: "name * 2"}, cols); err == nil {
		t.Fatal("arithmetic over a text column must fail type-check")
	}
	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: `CONCAT(name, 5)`}, cols); err == nil {
		t.Fatal("concat with a number literal must fail type-check")
	}
	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: "DROP TABLE users"}, cols); err == nil {
		t.Fatal("non-formula token must be rejected")
	}

	_, fn, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: "price * 2"}, cols)
	if err != nil {
		t.Fatal(err)
	}
	if v := fn(row); v != nil {
		t.Fatalf("NULL operand must yield nil result, got %v", v)
	}
	_, fn, _ = CompileComputed(defs.Column{Name: "c", ComputedFormula: "1 / 0"}, cols)
	if v := fn(map[string]any{}); v != nil {
		t.Fatalf("division by zero must yield nil, got %v", v)
	}
}

func TestApplyComputed(t *testing.T) {
	cols := []defs.Column{
		{Name: "price", FieldType: "number"},
		{Name: "total", FieldType: "number", IsComputed: true, ComputedFormula: "price * 2"},
	}
	rows := []map[string]any{
		{"price": float64(5)},
		{"price": nil},
	}
	ApplyComputed(cols, rows)
	if rows[0]["total"] != float64(10) {
		t.Fatalf("row0 total = %v", rows[0]["total"])
	}
	if rows[1]["total"] != nil {
		t.Fatalf("row1 total = %v want nil", rows[1]["total"])
	}
}

func TestComputedRejectsInjectionName(t *testing.T) {
	cols := []defs.Column{{Name: "a", FieldType: "number"}}
	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: "a + 1; -- x"}, cols); err == nil {
		t.Fatal("semicolon comment must be rejected")
	}
	if _, _, err := CompileComputed(defs.Column{Name: "c", ComputedFormula: `CONCAT("x")`}, cols); err == nil {
		t.Fatal("concat needs at least 2 args")
	}
}

func TestRowNumStringAndBytes(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{"10.5", 10.5, true},
		{"7", 7, true},
		{[]byte("3.25"), 3.25, true},
		{[]byte("12"), 12, true},
		{"abc", 0, false},
		{[]byte("xyz"), 0, false},
		{float64(4), 4, true},
		{nil, 0, false},
	}
	for _, tc := range cases {
		got, ok := rowNum(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("rowNum(%v) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestComputedArithmeticFromDecimalStrings(t *testing.T) {
	cols := []defs.Column{
		{Name: "price", FieldType: "number"},
		{Name: "qty", FieldType: "number"},
	}
	_, fn, err := CompileComputed(defs.Column{Name: "total", ComputedFormula: "price * qty"}, cols)
	if err != nil {
		t.Fatal(err)
	}
	// MySQL DECIMAL (and some sqlite scans) surface numeric columns as
	// strings / []byte — arithmetic must still evaluate instead of yielding nil.
	if v := fn(map[string]any{"price": "10", "qty": "3"}); v != float64(30) {
		t.Fatalf("string operands: got %v want 30", v)
	}
	if v := fn(map[string]any{"price": []byte("10"), "qty": []byte("3")}); v != float64(30) {
		t.Fatalf("[]byte operands: got %v want 30", v)
	}
	if v := fn(map[string]any{"price": "x", "qty": "3"}); v != nil {
		t.Fatalf("unparseable operand must yield nil, got %v", v)
	}
}
