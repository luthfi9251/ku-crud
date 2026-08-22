package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

// seedMetaFixture seeds 1 datasource, 2 defs (customers, orders with fk -> customers + email validation).
func seedMetaFixture(t *testing.T, s *Server) {
	t.Helper()
	ds := &meta.Datasource{
		Name: "pg1", Host: "h", Port: 5432, DBName: "db", Username: "u", Password: "SECRET", Driver: "postgres",
	}
	if err := s.store.CreateDatasource(ds); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.store.CreateGroup("Sales")
	cust := &meta.TableDef{DatasourceID: ds.ID, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 50, DefaultSortCol: "id", GroupID: gid}
	if err := s.store.SaveTableDef(cust, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "email", Label: "Email", FieldType: "text", Editable: true, Visible: true, Position: 2,
			Validations: []meta.ValidationRule{{Type: "email"}}},
	}); err != nil {
		t.Fatal(err)
	}
	orders := &meta.TableDef{DatasourceID: ds.ID, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 50}
	if err := s.store.SaveTableDef(orders, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Visible: true, Position: 1},
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			Editable: true, Visible: true, Position: 2,
			FKTableDefID: cust.ID, FKRefColumn: "id", FKDisplayColumns: []string{"email"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMetaExport(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedMetaFixture(t, s)

	w := do(s, "GET", "/api/meta/export", "", c)
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if strings.Contains(body, "SECRET") || strings.Contains(strings.ToLower(body), "password") {
		t.Fatalf("password leaked in export: %s", body)
	}
	var f metaFile
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("not valid meta file: %v\n%s", err, body)
	}
	if f.Format != "ku-crud-meta" || f.Version != 1 || len(f.Datasources) != 1 || len(f.Tables) != 2 || len(f.Groups) != 1 {
		t.Fatalf("header wrong: %+v", f)
	}
	var ord *metaFileTable
	for i := range f.Tables {
		if f.Tables[i].Table == "orders" {
			ord = &f.Tables[i]
		}
	}
	if ord == nil || ord.Columns[1].FKTableRef == nil ||
		ord.Columns[1].FKTableRef.DatasourceRef != "pg1" ||
		ord.Columns[1].FKTableRef.Table != "customers" {
		t.Fatalf("fkTableRef not natural-keyed: %+v", ord)
	}
	if f.Tables[0].GroupRef != "Sales" {
		t.Fatalf("groupRef missing: %+v", f.Tables[0])
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "ku-crud-meta-") {
		t.Fatalf("attachment header: %q", cd)
	}
}

func metaFileFor(t *testing.T, s *Server) string {
	// seed like TestMetaExport, then export and return the file body
	c := login(s)
	seedMetaFixture(t, s)
	w := do(s, "GET", "/api/meta/export", "", c)
	return w.Body.String()
}

func TestMetaImportPreview(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	file := metaFileFor(t, s)

	// same store -> everything duplicate-identical, deps resolved
	req := multipartBody(t, "/api/meta/import/preview", "file", "x.json", []byte(file))
	req.Header.Set("Cookie", *c)
	resp := httptest.NewRecorder()
	s.Routes().ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("preview = %d %s", resp.Code, resp.Body)
	}
	var pr importPreviewRes
	json.Unmarshal(resp.Body.Bytes(), &pr)
	if len(pr.Datasources) != 1 || pr.Datasources[0].Status != "duplicate-identical" {
		t.Fatalf("ds statuses: %+v", pr.Datasources)
	}
	if len(pr.Tables) != 2 {
		t.Fatalf("tables: %+v", pr.Tables)
	}
	for _, tb := range pr.Tables {
		if tb.Status != "duplicate-identical" {
			t.Fatalf("table %s status %s", tb.Ref, tb.Status)
		}
		for _, d := range tb.Dependencies {
			if !d.Resolved {
				t.Fatalf("dep %s unresolved in %s", d.Ref, tb.Ref)
			}
		}
	}

	// fresh store -> everything new
	s2 := newTestServer(t)
	c2 := login(s2)
	req = multipartBody(t, "/api/meta/import/preview", "file", "x.json", []byte(file))
	req.Header.Set("Cookie", *c2)
	resp = httptest.NewRecorder()
	s2.Routes().ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("preview2 = %d %s", resp.Code, resp.Body)
	}
	pr = importPreviewRes{}
	json.Unmarshal(resp.Body.Bytes(), &pr)
	if pr.Datasources[0].Status != "new" || pr.Tables[0].Status != "new" {
		t.Fatalf("fresh statuses: %+v %+v", pr.Datasources, pr.Tables)
	}

	// bad format -> 400 META_FILE_INVALID
	req = multipartBody(t, "/api/meta/import/preview", "file", "x.json", []byte(`{"format":"other"}`))
	req.Header.Set("Cookie", *c2)
	resp = httptest.NewRecorder()
	s2.Routes().ServeHTTP(resp, req)
	if resp.Code != 400 || !strings.Contains(resp.Body.String(), "META_FILE_INVALID") {
		t.Fatalf("bad format = %d %s", resp.Code, resp.Body)
	}
}

func TestMetaImportPreviewMissingDependency(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	// a file whose fk target table is absent everywhere
	file := `{"format":"ku-crud-meta","version":1,"groups":[],
		"datasources":[{"name":"pg1","adapter":"postgres","host":"h","port":5432,"database":"db","user":"u","sslMode":"prefer"}],
		"tables":[{"datasourceRef":"pg1","schema":"public","table":"orders","label":"Orders",
			"keyColumns":["id"],"pageSize":50,"defaultSortCol":"","defaultSortDir":"ASC",
			"columns":[{"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,"visible":true,"searchable":true,"sortable":true,"position":1},
				{"name":"customer_id","label":"C","fieldType":"fk","baseType":"number","editable":true,"visible":true,"position":2,
				 "fkTableRef":{"datasourceRef":"pg1","schema":"public","table":"ghost"},"fkRefColumn":"id","fkDisplayColumns":["id"],"fkDisplay":null,"m2mJunctionTableRef":null,"m2mDisplayColumns":[]}]}]}`
	req := multipartBody(t, "/api/meta/import/preview", "file", "x.json", []byte(file))
	req.Header.Set("Cookie", *c)
	resp := httptest.NewRecorder()
	s.Routes().ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("preview = %d %s", resp.Code, resp.Body)
	}
	if !strings.Contains(resp.Body.String(), "invalid-dependency") {
		t.Fatalf("dependency not flagged: %s", resp.Body)
	}
}

func multipartBody(t *testing.T, path, field, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile(field, filename)
	fw.Write(content)
	mw.Close()
	req, _ := http.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func applyMeta(t *testing.T, s *Server, c *string, file, selections string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "x.json")
	fw.Write([]byte(file))
	mw.WriteField("selections", selections)
	mw.Close()
	req, _ := http.NewRequest("POST", "/api/meta/import/apply", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c != nil {
		req.Header.Set("Cookie", *c)
	}
	resp := httptest.NewRecorder()
	s.Routes().ServeHTTP(resp, req)
	return resp
}

func TestMetaImportRejectsInvalidComputedColumns(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	base := metaFile{
		Format: "ku-crud-meta", Version: 1, Groups: []string{},
		Datasources: []metaFileDatasource{{Name: "pg1", Adapter: "postgres", Host: "h", Port: 5432, Database: "db", User: "u", SSLMode: "prefer"}},
		Tables: []metaFileTable{{
			DatasourceRef: "pg1", Schema: "public", Table: "orders", Label: "Orders",
			KeyColumns: []string{"id"}, PageSize: 50,
			Columns: []metaFileColumn{
				{Name: "id", Label: "ID", FieldType: "number", Editable: true, Required: true, Visible: true, Searchable: true, Sortable: true, Position: 1},
				{Name: "total", Label: "Total", FieldType: "number", IsComputed: true, ComputedFml: "id * 2", Visible: true, Position: 2},
			},
		}},
	}
	sel := `{"datasources":[{"ref":"pg1","mode":"overwrite","password":"pw"}],"tables":[{"ref":"pg1/public/orders","mode":"overwrite"}],"groups":false}`

	// control: a well-formed computed column imports fine
	good, _ := json.Marshal(base)
	if resp := applyMeta(t, s, c, string(good), sel); resp.Code != 200 {
		t.Fatalf("valid computed import = %d %s", resp.Code, resp.Body)
	}

	cases := []struct {
		name   string
		want   string
		mutate func(tb *metaFileTable)
	}{
		{"computed sortable", "cannot be editable/searchable/sortable", func(tb *metaFileTable) { tb.Columns[1].Sortable = true }},
		{"computed editable", "cannot be editable/searchable/sortable", func(tb *metaFileTable) { tb.Columns[1].Editable = true }},
		{"computed searchable", "cannot be editable/searchable/sortable", func(tb *metaFileTable) { tb.Columns[1].Searchable = true }},
		{"computed key column", "cannot be key columns", func(tb *metaFileTable) { tb.KeyColumns = []string{"total"} }},
		{"computed result type mismatch", "produces number but the column type is", func(tb *metaFileTable) { tb.Columns[1].FieldType = "text" }},
		{"computed non-number/text", "computed columns must be number or text", func(tb *metaFileTable) { tb.Columns[1].FieldType = "boolean" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// deep-copy via JSON so slice mutations never leak into base
			f := metaFile{}
			raw, _ := json.Marshal(base)
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			tb := f.Tables[0]
			tc.mutate(&tb)
			f.Tables = []metaFileTable{tb}
			bad, _ := json.Marshal(f)
			resp := applyMeta(t, s, c, string(bad), sel)
			if resp.Code != 400 || !strings.Contains(resp.Body.String(), "META_IMPORT_INVALID") || !strings.Contains(resp.Body.String(), tc.want) {
				t.Fatalf("%s: %d %s (want 400 containing %q)", tc.name, resp.Code, resp.Body, tc.want)
			}
		})
	}
}

func TestMetaImportApplyEndToEnd(t *testing.T) {
	src := newTestServer(t)
	file := metaFileFor(t, src) // seeded + exported

	dst := newTestServer(t)
	c := login(dst)
	resp := applyMeta(t, dst, c, file,
		`{"datasources":[{"ref":"pg1","mode":"overwrite","password":"pw2"}],"tables":[],"groups":true}`)
	if resp.Code != 200 {
		t.Fatalf("ds-only apply = %d %s", resp.Code, resp.Body)
	}
	// now the full import (ds identical now; tables new) — deps to in-file targets must pass
	resp = applyMeta(t, dst, c, file,
		`{"datasources":[{"ref":"pg1","mode":"skip"}],
		  "tables":[{"ref":"pg1/public/customers","mode":"overwrite"},{"ref":"pg1/public/orders","mode":"overwrite"}],
		  "groups":true}`)
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"createdDefs":2`) {
		t.Fatalf("full apply = %d %s", resp.Code, resp.Body)
	}
	// roundtrip: re-export from dst, orders fk must point at dst's customers
	w := do(dst, "GET", "/api/meta/export", "", c)
	if !strings.Contains(w.Body.String(), `"fkTableRef":{"datasourceRef":"pg1","schema":"public","table":"customers"}`) {
		t.Fatalf("fk relation lost after import: %s", w.Body)
	}
	// password prompt required for a NEW ds with empty password
	s3 := newTestServer(t)
	c3 := login(s3)
	resp = applyMeta(t, s3, c3, file, `{"datasources":[{"ref":"pg1","mode":"overwrite","password":""}],"tables":[],"groups":false}`)
	if resp.Code != 400 || !strings.Contains(resp.Body.String(), "META_IMPORT_INVALID") {
		t.Fatalf("missing password must be rejected: %d %s", resp.Code, resp.Body)
	}
}
