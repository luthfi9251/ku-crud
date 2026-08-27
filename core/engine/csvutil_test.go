package engine

import (
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
)

func TestSniffDelimiter(t *testing.T) {
	cases := []struct {
		line string
		want rune
	}{
		{"a,b,c", ','},
		{"a;b;c;d", ';'},
		{"a\tb\tc\td\te", '\t'},
		{`"a;b",c`, ','}, // semicolon inside quotes doesn't count
		{"a;b,c", ','},   // tie (1:1) → comma
		{"single", ','},  // no delimiter at all → comma
	}
	for _, c := range cases {
		if got := sniffDelimiter(c.line); got != c.want {
			t.Errorf("sniff(%q) = %q want %q", c.line, got, c.want)
		}
	}
}

func TestParseCSV(t *testing.T) {
	comma, headers, rows, err := ParseCSV([]byte("name;balance\njo;1.5\njoe;2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if comma != ';' || headers[0] != "name" || len(rows) != 2 || rows[0][0] != "jo" {
		t.Fatalf("parse: %q %v %v", comma, headers, rows)
	}
	// quoted field with delimiter and newline
	_, _, rows, err = ParseCSV([]byte("a,b\n\"x,y\nz\",2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "x,y\nz" || rows[0][1] != "2" {
		t.Fatalf("quoted: %v", rows)
	}
	// empty file
	if _, _, _, err = ParseCSV([]byte("")); err == nil {
		t.Fatal("empty must error")
	}
}

func TestAutoMap(t *testing.T) {
	cols := []defs.Column{
		{Name: "name", Label: "Full Name"},
		{Name: "balance", Label: "Balance"},
		{Name: "born", Label: "Born"},
	}
	m := AutoMap([]string{"Name", "BALANCE ", "Full Name", "unknown"}, cols)
	if m["Name"] != "name" || m["BALANCE "] != "balance" || m["Full Name"] != "name" || m["unknown"] != "" {
		t.Fatalf("autoMap: %v", m)
	}
}

func TestCoerceValidate(t *testing.T) {
	ok := []struct {
		col  defs.Column
		raw  string
		want any
	}{
		{defs.Column{FieldType: "boolean"}, "true", true},
		{defs.Column{FieldType: "boolean"}, "0", false},
		{defs.Column{FieldType: "boolean"}, "YES", true},
		{defs.Column{FieldType: "number"}, "42", float64(42)},
		{defs.Column{FieldType: "number"}, "10.5", 10.5},
		{defs.Column{FieldType: "datetime"}, "1990-01-02", "1990-01-02"},
		{defs.Column{FieldType: "uuid"}, "123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174000"},
		{defs.Column{FieldType: "json"}, `{"a": 1}`, `{"a":1}`},
		{defs.Column{FieldType: "json"}, `  [1, 2] `, `[1,2]`},
		{defs.Column{FieldType: "enum", EnumOptions: []string{"a"}}, "a", "a"},
		{defs.Column{FieldType: "fk", BaseType: "number"}, "7", float64(7)},
		{defs.Column{FieldType: "text"}, "", nil},
	}
	for _, c := range ok {
		got, err := CoerceValidate(c.col, c.raw)
		if err != nil {
			t.Errorf("coerce(%s, %q): %v", c.col.FieldType, c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("coerce(%s, %q) = %v want %v", c.col.FieldType, c.raw, got, c.want)
		}
	}
	bad := []struct {
		col defs.Column
		raw string
	}{
		{defs.Column{FieldType: "boolean"}, "maybe"},
		{defs.Column{FieldType: "number"}, "1,5"},
		{defs.Column{FieldType: "number"}, "abc"},
		{defs.Column{FieldType: "datetime"}, "02/01/1990"},
		{defs.Column{FieldType: "uuid"}, "nope"},
		{defs.Column{FieldType: "json"}, "{a}"},
		{defs.Column{FieldType: "enum", EnumOptions: []string{"a"}}, "z"},
	}
	for _, c := range bad {
		if _, err := CoerceValidate(c.col, c.raw); err == nil {
			t.Errorf("coerce(%s, %q) should fail", c.col.FieldType, c.raw)
		}
	}
}
