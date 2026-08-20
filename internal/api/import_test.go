package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/meta"
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
	comma, headers, rows, err := parseCSV([]byte("name;balance\njo;1.5\njoe;2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if comma != ';' || headers[0] != "name" || len(rows) != 2 || rows[0][0] != "jo" {
		t.Fatalf("parse: %q %v %v", comma, headers, rows)
	}
	// quoted field with delimiter and newline
	_, _, rows, err = parseCSV([]byte("a,b\n\"x,y\nz\",2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "x,y\nz" || rows[0][1] != "2" {
		t.Fatalf("quoted: %v", rows)
	}
	// empty file
	if _, _, _, err = parseCSV([]byte("")); err == nil {
		t.Fatal("empty must error")
	}
}

func TestAutoMap(t *testing.T) {
	cols := []meta.ColumnDef{
		{Name: "name", Label: "Full Name"},
		{Name: "balance", Label: "Balance"},
		{Name: "born", Label: "Born"},
	}
	m := autoMap([]string{"Name", "BALANCE ", "Full Name", "unknown"}, cols)
	if m["Name"] != "name" || m["BALANCE "] != "balance" || m["Full Name"] != "name" || m["unknown"] != "" {
		t.Fatalf("autoMap: %v", m)
	}
}

func TestCoerceValidate(t *testing.T) {
	ok := []struct {
		col  meta.ColumnDef
		raw  string
		want any
	}{
		{meta.ColumnDef{FieldType: "boolean"}, "true", true},
		{meta.ColumnDef{FieldType: "boolean"}, "0", false},
		{meta.ColumnDef{FieldType: "boolean"}, "YES", true},
		{meta.ColumnDef{FieldType: "number"}, "42", float64(42)},
		{meta.ColumnDef{FieldType: "number"}, "10.5", 10.5},
		{meta.ColumnDef{FieldType: "datetime"}, "1990-01-02", "1990-01-02"},
		{meta.ColumnDef{FieldType: "uuid"}, "123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174000"},
		{meta.ColumnDef{FieldType: "json"}, `{"a": 1}`, `{"a":1}`},
		{meta.ColumnDef{FieldType: "json"}, `  [1, 2] `, `[1,2]`},
		{meta.ColumnDef{FieldType: "enum", EnumOptions: []string{"a"}}, "a", "a"},
		{meta.ColumnDef{FieldType: "fk", BaseType: "number"}, "7", float64(7)},
		{meta.ColumnDef{FieldType: "text"}, "", nil},
	}
	for _, c := range ok {
		got, err := coerceValidate(c.col, c.raw)
		if err != nil {
			t.Errorf("coerce(%s, %q): %v", c.col.FieldType, c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("coerce(%s, %q) = %v want %v", c.col.FieldType, c.raw, got, c.want)
		}
	}
	bad := []struct {
		col meta.ColumnDef
		raw string
	}{
		{meta.ColumnDef{FieldType: "boolean"}, "maybe"},
		{meta.ColumnDef{FieldType: "number"}, "1,5"},
		{meta.ColumnDef{FieldType: "number"}, "abc"},
		{meta.ColumnDef{FieldType: "datetime"}, "02/01/1990"},
		{meta.ColumnDef{FieldType: "uuid"}, "nope"},
		{meta.ColumnDef{FieldType: "json"}, "{a}"},
		{meta.ColumnDef{FieldType: "enum", EnumOptions: []string{"a"}}, "z"},
	}
	for _, c := range bad {
		if _, err := coerceValidate(c.col, c.raw); err == nil {
			t.Errorf("coerce(%s, %q) should fail", c.col.FieldType, c.raw)
		}
	}
}

// importRequest builds a multipart request for the import endpoints.
func importRequest(t *testing.T, path, csv string, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if csv != "" {
		fw, _ := mw.CreateFormFile("file", "import.csv")
		fw.Write([]byte(csv))
	}
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestImportPreviewAndApply(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s) // customers: name required, status enum, balance numeric
	tok := tdTok(s, 1)

	csv := "name,balance,born,status\nnia,7.25,1990-01-02,rainy\nbad,notanumber,1990-01-02,sunny\nmissing,,1990-01-02,snowy\n"
	req := importRequest(t, "/api/tables/"+tok+"/import/preview", csv, nil)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"total":3`) || !strings.Contains(body, `"valid":1`) || !strings.Contains(body, `"invalid":2`) {
		t.Fatalf("counts = %s", body)
	}
	if !strings.Contains(body, "not a number") || !strings.Contains(body, "not in enum options") {
		t.Fatalf("per-row errors = %s", body)
	}

	// apply valid-only: one row inserted, others skipped
	req = importRequest(t, "/api/tables/"+tok+"/import/apply", csv, map[string]string{
		"mapping": `{"name":"name","balance":"balance","born":"born","status":"status"}`,
		"mode":    "valid",
	})
	req.Header.Set("Cookie", *c)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":1`) || !strings.Contains(w.Body.String(), `"failed":0`) {
		t.Fatalf("apply valid = %d %s", w.Code, w.Body)
	}

	// row actually landed
	if w := do(s, "GET", "/api/tables/"+tok+"/rows?search=nia", "", c); !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("inserted row missing: %s", w.Body)
	}

	// audit got an INSERT for the import
	if w := do(s, "GET", "/api/audit?tableDefId="+tok, "", c); !strings.Contains(w.Body.String(), `"INSERT"`) {
		t.Fatalf("audit missing import insert: %s", w.Body)
	}

	// apply mode=all: invalid cells are omitted (best-effort NULL) and the
	// rows are still attempted — all three rows insert (nia again), none fail
	req = importRequest(t, "/api/tables/"+tok+"/import/apply", csv, map[string]string{
		"mapping": `{"name":"name","balance":"balance","born":"born","status":"status"}`,
		"mode":    "all",
	})
	req.Header.Set("Cookie", *c)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("apply all = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"inserted":3`) || !strings.Contains(w.Body.String(), `"failed":0`) {
		t.Fatalf("apply all result = %s", w.Body)
	}
}

func TestImportSemicolonFile(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)
	tok := tdTok(s, 1)

	req := importRequest(t, "/api/tables/"+tok+"/import/preview", "name;status\nsemi;sunny\n", nil)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":1`) || !strings.Contains(w.Body.String(), `";"`) {
		t.Fatalf("semicolon preview = %d %s", w.Code, w.Body)
	}
}

func TestImportRequiresCreateGrant(t *testing.T) {
	s := newTestServer(t)
	seedLive(t, s)
	tok := tdTok(s, 1)

	reader := loginAs(t, s, "reader2", &meta.Role{Name: "Reader2"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true, CanCreate: false}})
	req := importRequest(t, "/api/tables/"+tok+"/import/preview", "name\nx\n", nil)
	req.Header.Set("Cookie", *reader)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("no-create preview = %d %s", w.Code, w.Body)
	}
}

func TestImportFKValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s) // orders def 2: note text, customer_id fk → customers.id

	csv := "note,customer_id\no9,1\no10,999\n"
	req := importRequest(t, "/api/tables/"+tdTok(s, 2)+"/import/preview", csv, nil)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"valid":1`) || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("fk validation = %s", w.Body)
	}
}
