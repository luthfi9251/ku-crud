package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func fcol(name, ft string) meta.ColumnDef {
	return meta.ColumnDef{Name: name, Label: name, FieldType: ft, Visible: true, Position: 1}
}

func meCtx(s *Server) CtxUser {
	u, _, _ := s.store.GetUserContext("alice")
	return u
}

func TestParseFiltersMatrix(t *testing.T) {
	s := newTestServer(t)
	_ = login(s)
	u := meCtx(s)
	cols := []meta.ColumnDef{fcol("name", "text"), fcol("age", "number"), fcol("ok", "boolean"),
		fcol("created", "datetime"), fcol("status", "enum"), fcol("tags", "json"), fcol("rel", "m2m")}
	def := &meta.TableDef{SchemaName: "public", TableName: "t"}

	f, msg := s.parseFilters(def, cols, u, `[{"column":"name","op":"contains","values":["jo"]}]`)
	if msg != "" || len(f) != 1 || f[0].Values[0] != "jo" {
		t.Fatalf("contains: f=%#v msg=%q", f, msg)
	}
	f, msg = s.parseFilters(def, cols, u, `[{"column":"age","op":"between","values":["18","30"]}]`)
	if msg != "" || f[0].Values[0] != 18.0 || f[0].Values[1] != 30.0 {
		t.Fatalf("between number: f=%#v msg=%q", f, msg)
	}
	f, msg = s.parseFilters(def, cols, u, `[{"column":"ok","op":"eq","values":["true"]}]`)
	if msg != "" || f[0].Values[0] != true {
		t.Fatalf("boolean: f=%#v msg=%q", f, msg)
	}
	f, msg = s.parseFilters(def, cols, u, `[{"column":"created","op":"between","values":["2026-01-01","2026-01-31"]}]`)
	if msg != "" || f[0].Values[1] != "2026-01-31 23:59:59" {
		t.Fatalf("date range inclusive upper: f=%#v msg=%q", f, msg)
	}
	f, msg = s.parseFilters(def, cols, u, `[{"column":"age","op":"in","values":["1","2","3"]}]`)
	if msg != "" || len(f[0].Values) != 3 {
		t.Fatalf("in list: f=%#v msg=%q", f, msg)
	}
	if f, msg = s.parseFilters(def, cols, u, ""); msg != "" || f != nil {
		t.Fatalf("empty param must pass: f=%#v msg=%q", f, msg)
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
	} {
		_, msg = s.parseFilters(def, cols, u, bad.raw)
		if msg == "" || !strings.Contains(msg, bad.want) {
			t.Errorf("bad %s: msg=%q want~%q", bad.raw, msg, bad.want)
		}
	}

	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, `{"column":"name","op":"eq","values":["x"]}`)
	}
	if _, msg = s.parseFilters(def, cols, u, "["+strings.Join(many, ",")+"]"); !strings.Contains(msg, "at most 10") {
		t.Errorf("cap: %q", msg)
	}
}
