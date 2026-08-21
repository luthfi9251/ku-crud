package api

import (
	"testing"

	"ku-crud/internal/meta"
)

func TestCompileComputed(t *testing.T) {
	cols := []meta.ColumnDef{
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
		ft, fn, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: tc.formula}, cols)
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
	cols := []meta.ColumnDef{
		{Name: "price", FieldType: "number"},
		{Name: "name", FieldType: "text"},
	}
	row := map[string]any{"price": nil, "name": "x"}

	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "bogus("}, cols); err == nil {
		t.Fatal("unbalanced paren must fail compile")
	}
	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "missing * 2"}, cols); err == nil {
		t.Fatal("unknown ident must fail compile")
	}
	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "name * 2"}, cols); err == nil {
		t.Fatal("arithmetic over a text column must fail type-check")
	}
	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: `CONCAT(name, 5)`}, cols); err == nil {
		t.Fatal("concat with a number literal must fail type-check")
	}
	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "DROP TABLE users"}, cols); err == nil {
		t.Fatal("non-formula token must be rejected")
	}

	_, fn, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "price * 2"}, cols)
	if err != nil {
		t.Fatal(err)
	}
	if v := fn(row); v != nil {
		t.Fatalf("NULL operand must yield nil result, got %v", v)
	}
	_, fn, _ = compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "1 / 0"}, cols)
	if v := fn(map[string]any{}); v != nil {
		t.Fatalf("division by zero must yield nil, got %v", v)
	}
}

func TestApplyComputed(t *testing.T) {
	cols := []meta.ColumnDef{
		{Name: "price", FieldType: "number"},
		{Name: "total", FieldType: "number", IsComputed: true, ComputedFormula: "price * 2"},
	}
	rows := []map[string]any{
		{"price": float64(5)},
		{"price": nil},
	}
	applyComputed(cols, rows)
	if rows[0]["total"] != float64(10) {
		t.Fatalf("row0 total = %v", rows[0]["total"])
	}
	if rows[1]["total"] != nil {
		t.Fatalf("row1 total = %v want nil", rows[1]["total"])
	}
}

func TestComputedRejectsInjectionName(t *testing.T) {
	cols := []meta.ColumnDef{{Name: "a", FieldType: "number"}}
	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: "a + 1; -- x"}, cols); err == nil {
		t.Fatal("semicolon comment must be rejected")
	}
	if _, _, err := compileComputed(meta.ColumnDef{Name: "c", ComputedFormula: `CONCAT("x")`}, cols); err == nil {
		t.Fatal("concat needs at least 2 args")
	}
}
