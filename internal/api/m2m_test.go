package api

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-crud/core/engine"
	"ku-crud/internal/meta"
)

// seedM2M creates customers/tags/customer_tags (junction) and stores defs:
// customers(id=1), tags(id=2), junction customer_tags(id=3), then updates
// customers with an m2m virtual column via the API.
func seedM2M(t *testing.T, s *Server) (string, string) {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	db, err := sql.Open("pgx", cs)
	if err != nil {
		t.Skipf("no PG: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("no PG: %v", err)
	}
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;
		CREATE TABLE customers(id serial PRIMARY KEY, name text NOT NULL);
		CREATE TABLE tags(id serial PRIMARY KEY, label text NOT NULL);
		CREATE TABLE customer_tags(customer_id int NOT NULL REFERENCES customers(id),
			tag_id int NOT NULL REFERENCES tags(id), PRIMARY KEY(customer_id, tag_id));
		INSERT INTO customers(name) VALUES ('jo'), ('ana');
		INSERT INTO tags(label) VALUES ('vip'), ('beta'), ('lead');
		INSERT INTO customer_tags(customer_id, tag_id) VALUES (1,1), (1,2), (2,3);`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	base := []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
	}
	for _, d := range []struct {
		table, label string
		cols         []meta.ColumnDef
	}{
		{"customers", "Customers", base},
		{"tags", "Tags", []meta.ColumnDef{
			{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
				Visible: true, Searchable: true, Sortable: true, Position: 0},
			{Name: "label", Label: "Label", FieldType: "text", Editable: true, Required: true,
				Visible: true, Searchable: true, Sortable: true, Position: 1},
		}},
	} {
		def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: d.table,
			Label: d.label, KeyColumns: []string{"id"}, PageSize: 20}
		if err := s.store.SaveTableDef(def, d.cols); err != nil {
			t.Fatal(err)
		}
	}
	// junction def (3) with two fk columns
	jdef := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customer_tags",
		Label: "Customer Tags", KeyColumns: []string{"customer_id", "tag_id"}, PageSize: 20}
	jcols := []meta.ColumnDef{
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			FKTableDefID: 1, FKRefColumn: "id", FKDisplayColumns: []string{"name"},
			Editable: true, Required: true, Visible: true, Sortable: true, Position: 0},
		{Name: "tag_id", Label: "Tag", FieldType: "fk", BaseType: "number",
			FKTableDefID: 2, FKRefColumn: "id", FKDisplayColumns: []string{"label"},
			Editable: true, Required: true, Visible: true, Sortable: true, Position: 1},
	}
	if err := s.store.SaveTableDef(jdef, jcols); err != nil {
		t.Fatal(err)
	}
	return tdTok(s, 1), tdTok(s, 2)
}

// addM2MColumn updates the customers definition to include the m2m virtual
// column via the public API (masked ids exercised end to end).
func addM2MColumn(t *testing.T, s *Server, c *string, custTok, tagTok string) {
	t.Helper()
	w := do(s, "GET", "/api/tables/"+custTok, "", c)
	if w.Code != 200 {
		t.Fatalf("get def = %d %s", w.Code, w.Body)
	}
	var def struct {
		KeyColumns []string `json:"keyColumns"`
		PageSize   int      `json:"pageSize"`
		Columns    []struct {
			Name      string `json:"name"`
			Label     string `json:"label"`
			FieldType string `json:"fieldType"`
			Position  int    `json:"position"`
		} `json:"columns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"datasourceId": datasourceTokOf(t, s),
		"schemaName":   "public",
		"tableName":    "customers",
		"label":        "Customers",
		"keyColumns":   def.KeyColumns,
		"pageSize":     def.PageSize,
		"columns": []map[string]any{
			{"name": "id", "label": "ID", "fieldType": "number", "editable": false,
				"required": true, "visible": true, "searchable": true, "sortable": true, "position": 0},
			{"name": "name", "label": "Name", "fieldType": "text", "editable": true,
				"required": true, "visible": true, "searchable": true, "sortable": true, "position": 1},
			{"name": "m2m_tags", "label": "Tags", "fieldType": "m2m",
				"visible": true, "position": 2,
				"m2mJunctionDefId":  junctionTok(t, s, 3),
				"m2mJunctionSrcCol": "customer_id",
				"m2mJunctionTgtCol": "tag_id",
				"m2mDisplayColumns": []string{"label"}},
		},
	}
	w = do(s, "PUT", "/api/tables/"+custTok, jsonStr(body), c)
	if w.Code != 200 {
		t.Fatalf("add m2m def = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"m2mRefColumn":"id"`) {
		t.Fatalf("m2mRefColumn missing: %s", w.Body)
	}
}

func datasourceTokOf(t *testing.T, s *Server) string {
	t.Helper()
	w := do(s, "GET", "/api/datasources", "", login(s))
	var list []struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) == 0 {
		t.Fatal("no datasource")
	}
	return list[0].ID
}

func junctionTok(t *testing.T, s *Server, id int64) string {
	t.Helper()
	return tdTok(s, id)
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestM2MEndToEnd(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	custTok, _ := seedM2M(t, s)
	addM2MColumn(t, s, c, custTok, "")

	// 1. list carries m2mRels: jo → [vip, beta], ana → [lead]
	w := do(s, "GET", "/api/data/customers/rows", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"m2mRels":{"m2m_tags"`) {
		t.Fatalf("m2mRels missing: %s", body)
	}
	if !strings.Contains(body, `"label":"vip"`) || !strings.Contains(body, `"label":"beta"`) ||
		!strings.Contains(body, `"label":"lead"`) {
		t.Fatalf("m2m values missing: %s", body)
	}

	// 2. options picker on target with search
	w = do(s, "GET", "/api/data/customers/m2moptions/m2m_tags?search=vi", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"label":"vip"`) || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("m2moptions = %d %s", w.Code, w.Body)
	}

	// 3. links endpoint for row jo (key 1)
	w = do(s, "GET", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"})+"/m2m/m2m_tags", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"values":[1,2]`) {
		t.Fatalf("links = %d %s", w.Code, w.Body)
	}

	// 4. update: jo gets [vip, lead] → beta removed, lead added
	w = do(s, "PUT", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"}),
		`{"name":"jo","m2m_tags":[1,3]}`, c)
	if w.Code != 200 {
		t.Fatalf("update with m2m = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"})+"/m2m/m2m_tags", "", c)
	if !strings.Contains(w.Body.String(), `"values":[1,3]`) {
		t.Fatalf("links after update = %s", w.Body)
	}

	// 5. create with m2m: explicit key + selection
	w = do(s, "POST", "/api/data/customers/rows", `{"id":3,"name":"nia","m2m_tags":[2,3]}`, c)
	if w.Code != 200 {
		t.Fatalf("create with m2m = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"3"})+"/m2m/m2m_tags", "", c)
	if !strings.Contains(w.Body.String(), `"values":[2,3]`) {
		t.Fatalf("links after create = %s", w.Body)
	}

	// 6. junction audit entries (INSERT/DELETE on customer_tags)
	w = do(s, "GET", "/api/audit?tableDefId="+tdTok(s, 3), "", c)
	if !strings.Contains(w.Body.String(), `"INSERT"`) || !strings.Contains(w.Body.String(), `"DELETE"`) {
		t.Fatalf("junction audit = %s", w.Body)
	}

	// 7. delete protection: jo has links → delete blocked
	w = do(s, "DELETE", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"}), "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Customer Tags") {
		t.Fatalf("delete protection = %d %s", w.Code, w.Body)
	}

	// 8. bad selection shape → 400
	w = do(s, "PUT", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"}),
		`{"name":"jo","m2m_tags":"vip"}`, c)
	if w.Code != 400 {
		t.Fatalf("bad shape = %d %s", w.Code, w.Body)
	}
}

func TestM2MValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	custTok, _ := seedM2M(t, s)

	// junction must exist & differ from this table
	w := do(s, "PUT", "/api/tables/"+custTok, `{
		"datasourceId":"`+datasourceTokOf(t, s)+`","schemaName":"public","tableName":"customers",
		"label":"Customers","keyColumns":["id"],"pageSize":20,
		"columns":[
			{"name":"id","label":"ID","fieldType":"number","editable":false,"required":true,"visible":true,"searchable":true,"sortable":true,"position":0},
			{"name":"m2m_tags","label":"Tags","fieldType":"m2m","visible":true,"position":1,
			 "m2mJunctionDefId":"`+custTok+`","m2mJunctionSrcCol":"customer_id","m2mJunctionTgtCol":"tag_id",
			 "m2mDisplayColumns":["label"]}]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "junction cannot be this table") {
		t.Fatalf("self junction = %d %s", w.Code, w.Body)
	}

	// non-fk junction column rejected
	w = do(s, "PUT", "/api/tables/"+custTok, `{
		"datasourceId":"`+datasourceTokOf(t, s)+`","schemaName":"public","tableName":"customers",
		"label":"Customers","keyColumns":["id"],"pageSize":20,
		"columns":[
			{"name":"id","label":"ID","fieldType":"number","editable":false,"required":true,"visible":true,"searchable":true,"sortable":true,"position":0},
			{"name":"m2m_tags","label":"Tags","fieldType":"m2m","visible":true,"position":1,
			 "m2mJunctionDefId":"`+tdTok(s, 3)+`","m2mJunctionSrcCol":"id","m2mJunctionTgtCol":"tag_id",
			 "m2mDisplayColumns":["label"]}]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "must be defined fk columns") {
		t.Fatalf("non-fk src = %d %s", w.Code, w.Body)
	}

	// src column must reference this table
	w = do(s, "PUT", "/api/tables/"+custTok, `{
		"datasourceId":"`+datasourceTokOf(t, s)+`","schemaName":"public","tableName":"customers",
		"label":"Customers","keyColumns":["id"],"pageSize":20,
		"columns":[
			{"name":"id","label":"ID","fieldType":"number","editable":false,"required":true,"visible":true,"searchable":true,"sortable":true,"position":0},
			{"name":"m2m_tags","label":"Tags","fieldType":"m2m","visible":true,"position":1,
			 "m2mJunctionDefId":"`+tdTok(s, 3)+`","m2mJunctionSrcCol":"tag_id","m2mJunctionTgtCol":"customer_id",
			 "m2mDisplayColumns":["name"]}]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "source column must reference this table") {
		t.Fatalf("wrong src target = %d %s", w.Code, w.Body)
	}
}

func TestM2MJunctionGrants(t *testing.T) {
	s := newTestServer(t)
	admin := login(s)
	custTok, _ := seedM2M(t, s)
	addM2MColumn(t, s, admin, custTok, "")

	// reader on customers + tags but NOT junction → grid skips m2m, update rejected
	reader := loginAs(t, s, "m2mreader", &meta.Role{Name: "M2MReader"}, []meta.TableGrant{
		{TableDefID: 1, CanRead: true, CanUpdate: true},
		{TableDefID: 2, CanRead: true},
	})
	w := do(s, "GET", "/api/data/customers/rows", "", reader)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"m2mRels":{"m2m_tags"`) {
		t.Fatalf("m2mRels without junction read = %d %s", w.Code, w.Body)
	}
	w = do(s, "PUT", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"}),
		`{"name":"jo","m2m_tags":[1]}`, reader)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "Customer Tags") {
		t.Fatalf("junction grant = %d %s", w.Code, w.Body)
	}

	// m2moptions without junction read → 403
	w = do(s, "GET", "/api/data/customers/m2moptions/m2m_tags", "", reader)
	if w.Code != 403 {
		t.Fatalf("options grant = %d %s", w.Code, w.Body)
	}
}

func TestM2MExport(t *testing.T) {
	s := newTestServer(t)
	c := *login(s)
	custTok, _ := seedM2M(t, s)
	addM2MColumn(t, s, &c, custTok, "")

	body, _ := exportCSV(t, s, "customers", "", c)
	if !strings.Contains(body, "id,name,m2m_tags") {
		t.Fatalf("header = %q", body)
	}
	if !strings.Contains(body, "1,jo,\"vip, beta\"") {
		t.Fatalf("m2m export values = %q", body)
	}
}

// TestM2MDanglingTargetErrors pins the pre-extraction behavior when the m2m
// target definition is deleted after its dependents were saved: the picker
// never silently resolves to the junction — admins get the old def-lookup
// 404, grant-less users the old 403.
func TestM2MDanglingTargetErrors(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	num := meta.ColumnDef{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
		Visible: true, Searchable: true, Sortable: true, Position: 0}
	if err := s.store.SaveTableDef(&meta.TableDef{DatasourceID: 1, SchemaName: "public",
		TableName: "customers", Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20},
		[]meta.ColumnDef{num, {Name: "name", Label: "Name", FieldType: "text", Editable: true,
			Required: true, Visible: true, Searchable: true, Sortable: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveTableDef(&meta.TableDef{DatasourceID: 1, SchemaName: "public",
		TableName: "tags", Label: "Tags", KeyColumns: []string{"id"}, PageSize: 20},
		[]meta.ColumnDef{num, {Name: "label", Label: "Label", FieldType: "text", Editable: true,
			Required: true, Visible: true, Searchable: true, Sortable: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveTableDef(&meta.TableDef{DatasourceID: 1, SchemaName: "public",
		TableName: "customer_tags", Label: "Customer Tags",
		KeyColumns: []string{"customer_id", "tag_id"}, PageSize: 20},
		[]meta.ColumnDef{
			{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
				FKTableDefID: 1, FKRefColumn: "id", FKDisplayColumns: []string{"name"},
				Editable: true, Required: true, Visible: true, Sortable: true, Position: 0},
			{Name: "tag_id", Label: "Tag", FieldType: "fk", BaseType: "number",
				FKTableDefID: 2, FKRefColumn: "id", FKDisplayColumns: []string{"label"},
				Editable: true, Required: true, Visible: true, Sortable: true, Position: 1},
		}); err != nil {
		t.Fatal(err)
	}
	custTok, tagTok := tdTok(s, 1), tdTok(s, 2)
	addM2MColumn(t, s, c, custTok, tagTok)

	// delete the target definition through the api — nothing guards the
	// inbound junction fk, exactly the reachable drift sequence
	if w := do(s, "DELETE", "/api/tables/"+tagTok, "", c); w.Code != 200 {
		t.Fatalf("delete target def = %d %s", w.Code, w.Body)
	}

	// admin: old perm passed on the dangling id, then def lookup failed → 404
	w := do(s, "GET", "/api/data/customers/m2moptions/m2m_tags", "", c)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "table def not found") {
		t.Fatalf("admin m2moptions = %d %s", w.Code, w.Body)
	}

	// reader with grants on source + junction only: old GrantsFor on the
	// dangling id found nothing → 403
	reader := loginAs(t, s, "m2mdrift", &meta.Role{Name: "M2MDrift"}, []meta.TableGrant{
		{TableDefID: 1, CanRead: true},
		{TableDefID: 3, CanRead: true},
	})
	w = do(s, "GET", "/api/data/customers/m2moptions/m2m_tags", "", reader)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "no read access to the related tables") {
		t.Fatalf("reader m2moptions = %d %s", w.Code, w.Body)
	}
}
