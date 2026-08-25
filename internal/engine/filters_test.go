package engine

import (
	"errors"
	"strings"
	"testing"

	"ku-crud/internal/defs"
	"ku-crud/internal/ds"
)

func fcol(name, ft string) defs.Column {
	return defs.Column{Name: name, Label: name, FieldType: ft, Visible: true, Position: 1}
}

func TestParseFiltersMatrix(t *testing.T) {
	cols := []defs.Column{fcol("name", "text"), fcol("age", "number"), fcol("ok", "boolean"),
		fcol("created", "datetime"), fcol("status", "enum"), fcol("tags", "json"), fcol("rel", "m2m"),
		{Name: "calc", Label: "calc", FieldType: "number", IsComputed: true, Visible: true, Position: 1}}
	tbl := &defs.Table{Schema: "public", PhysTab: "t", Columns: cols}

	f, err := ParseFilters(tbl, `[{"column":"name","op":"contains","values":["jo"]}]`, nil)
	if err != nil || len(f) != 1 || f[0].Values[0] != "jo" {
		t.Fatalf("contains: f=%#v err=%v", f, err)
	}
	f, err = ParseFilters(tbl, `[{"column":"age","op":"between","values":["18","30"]}]`, nil)
	if err != nil || f[0].Values[0] != 18.0 || f[0].Values[1] != 30.0 {
		t.Fatalf("between number: f=%#v err=%v", f, err)
	}
	f, err = ParseFilters(tbl, `[{"column":"ok","op":"eq","values":["true"]}]`, nil)
	if err != nil || f[0].Values[0] != true {
		t.Fatalf("boolean: f=%#v err=%v", f, err)
	}
	f, err = ParseFilters(tbl, `[{"column":"created","op":"between","values":["2026-01-01","2026-01-31"]}]`, nil)
	if err != nil || f[0].Values[1] != "2026-01-31 23:59:59" {
		t.Fatalf("date range inclusive upper: f=%#v err=%v", f, err)
	}
	f, err = ParseFilters(tbl, `[{"column":"age","op":"in","values":["1","2","3"]}]`, nil)
	if err != nil || len(f[0].Values) != 3 {
		t.Fatalf("in list: f=%#v err=%v", f, err)
	}
	if f, err = ParseFilters(tbl, "", nil); err != nil || f != nil {
		t.Fatalf("empty param must pass: f=%#v err=%v", f, err)
	}

	for _, bad := range []struct{ raw, want string }{
		{`not json`, "JSON array"},
		{`[{"column":"nope","op":"eq","values":["1"]}]`, "unknown column"},
		{`[{"column":"name","op":"gt","values":["a"]}]`, "not supported"},
		{`[{"column":"age","op":"between","values":["1"]}]`, "exactly 2"},
		{`[{"column":"age","op":"in","values":[]}]`, "1..50"},
		{`[{"column":"age","op":"eq","values":["abc"]}]`, "not a number"},
		{`[{"column":"ok","op":"eq","values":["maybe"]}]`, "true/false"},
		{`[{"column":"created","op":"eq","values":["tomorrow"]}]`, "not a datetime"},
		{`[{"column":"tags","op":"eq","values":["x"]}]`, "cannot be filtered"},
		{`[{"column":"rel","op":"eq","values":["x"]}]`, "cannot be filtered"},
		{`[{"column":"calc","op":"eq","values":["5"]}]`, "cannot be filtered"},
	} {
		_, err = ParseFilters(tbl, bad.raw, nil)
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("bad %s: err=%v want~%q", bad.raw, err, bad.want)
		}
	}

	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, `{"column":"name","op":"eq","values":["x"]}`)
	}
	if _, err = ParseFilters(tbl, "["+strings.Join(many, ",")+"]", nil); err == nil || !strings.Contains(err.Error(), "at most 10") {
		t.Errorf("cap: %v", err)
	}
}

func TestParseFiltersFKJoin(t *testing.T) {
	tbl := &defs.Table{Schema: "public", PhysTab: "t", Columns: []defs.Column{
		{Name: "customer_id", Label: "customer_id", FieldType: "fk"},
	}}
	raw := `[{"column":"customer_id","op":"contains","values":["jo"]}]`

	// the resolver supplies the join and its errors surface verbatim
	got, err := ParseFilters(tbl, raw, func(column string) (*ds.FKJoin, error) {
		if column != "customer_id" {
			t.Fatalf("resolver called with %q", column)
		}
		return &ds.FKJoin{Schema: "public", Table: "customers",
			RefColumn: "id", DisplayColumns: []string{"name"}}, nil
	})
	if err != nil || got[0].Join == nil || got[0].Join.Table != "customers" {
		t.Fatalf("fk join: f=%#v err=%v", got, err)
	}
	_, err = ParseFilters(tbl, raw, func(string) (*ds.FKJoin, error) {
		return nil, errFKDenied
	})
	if err != errFKDenied {
		t.Fatalf("resolver error must pass through: %v", err)
	}
	// nil resolver rejects fk filters like a missing read grant
	if _, err = ParseFilters(tbl, raw, nil); err == nil || !strings.Contains(err.Error(), "requires read access") {
		t.Fatalf("nil resolver: %v", err)
	}
}

var errFKDenied = errors.New("fk target denied")

// TestResolveSort pins the platform's uppercase direction casing: ds adapters
// reject anything but "ASC"/"DESC", and DefaultSortDir carries the persisted
// casing — lowercase values are NOT normalized, they fall back to ASC.
func TestResolveSort(t *testing.T) {
	tbl := &defs.Table{Keys: []string{"id"},
		Columns: []defs.Column{
			{Name: "id", FieldType: "number", Sortable: true},
			{Name: "created", Label: "created", FieldType: "datetime", Sortable: true},
			{Name: "rel", FieldType: "m2m"},
			{Name: "calc", FieldType: "number", IsComputed: true},
		}}
	if c, d := ResolveSort(tbl, "created", "DESC"); c != "created" || d != "DESC" {
		t.Fatalf("explicit: %q %q", c, d)
	}
	if c, d := ResolveSort(tbl, "created", "asc"); c != "created" || d != "ASC" {
		t.Fatalf("lowercase dir is invalid, falls back to ASC: %q %q", c, d)
	}
	if c, d := ResolveSort(tbl, "nope", "DESC"); c != "id" || d != "ASC" {
		t.Fatalf("unknown column falls back to first key ASC: %q %q", c, d)
	}

	def := &defs.Table{Keys: []string{"id"}, DefaultSortCol: "created", DefaultSortDir: "DESC",
		Columns: tbl.Columns}
	if c, d := ResolveSort(def, "", ""); c != "created" || d != "DESC" {
		t.Fatalf("default sort uses persisted casing: %q %q", c, d)
	}
	def.DefaultSortDir = "desc" // lowercase is not accepted
	if c, d := ResolveSort(def, "", ""); c != "created" || d != "ASC" {
		t.Fatalf("lowercase default dir falls back to ASC: %q %q", c, d)
	}
	def.DefaultSortCol = "gone" // dropped column → key fallback
	if c, d := ResolveSort(def, "", ""); c != "id" || d != "ASC" {
		t.Fatalf("dropped default column falls back to key: %q %q", c, d)
	}
}
