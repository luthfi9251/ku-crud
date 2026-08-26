package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func seedDS(t *testing.T, s *Server) {
	t.Helper()
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "d", Host: "h", Port: 1,
		DBName: "db", Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
}

// defBody returns a valid create payload; datasourceId is a masked token.
func defBody(s *Server) string {
	return `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"customers",
"label":"Customers","keyColumns":["id"],"pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"status","label":"Status","fieldType":"enum","enumOptions":["a","b"],
  "editable":true,"required":false,"visible":true,"searchable":false,"sortable":false,"position":1}]}`
}

func TestTableDefEndpoints(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)

	w := do(s, "POST", "/api/tables", defBody(s), c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}

	// PK not among columns → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody(s), `"keyColumns":["id"]`, `"keyColumns":["nope"]`, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "key column") {
		t.Fatalf("bad pk = %d %s", w.Code, w.Body)
	}

	// enum without options → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody(s), `"enumOptions":["a","b"],`, ``, 1), c)
	if w.Code != 400 {
		t.Fatalf("enum no options = %d %s", w.Code, w.Body)
	}

	// unknown fieldType → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody(s), `"fieldType":"enum"`, `"fieldType":"jsonb"`, 1), c)
	if w.Code != 400 {
		t.Fatalf("bad fieldType = %d %s", w.Code, w.Body)
	}

	// unquotable table name → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody(s), `"tableName":"customers"`, `"tableName":"bad table"`, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid identifier") {
		t.Fatalf("bad table name = %d %s", w.Code, w.Body)
	}

	// unquotable column name → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody(s), `"name":"id"`, `"name":"i d"`, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid identifier") {
		t.Fatalf("bad column name = %d %s", w.Code, w.Body)
	}

	w = do(s, "GET", "/api/tables", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Customers") {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/"+s.ids.Encode("td", 1), "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"fieldType":"enum"`) {
		t.Fatalf("get = %d %s", w.Code, w.Body)
	}
	w = do(s, "PUT", "/api/tables/"+s.ids.Encode("td", 1), strings.Replace(defBody(s),
		`"label":"Customers"`, `"label":"Cust2"`, 1), c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Cust2") {
		t.Fatalf("put = %d %s", w.Code, w.Body)
	}
	if w = do(s, "PUT", "/api/tables/999", defBody(s), c); w.Code != 404 {
		t.Fatalf("put missing = %d", w.Code)
	}
	w = do(s, "DELETE", "/api/tables/"+s.ids.Encode("td", 1), "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d", w.Code)
	}
	if w = do(s, "GET", "/api/tables/"+s.ids.Encode("td", 1), "", c); w.Code != 404 {
		t.Fatalf("get deleted = %d", w.Code)
	}
}

// defBodyDesc returns defBody with a table description injected before label.
func defBodyDesc(s *Server, desc string) string {
	return strings.Replace(defBody(s), `"label":`, fmt.Sprintf(`"description":"%s","label":`, desc), 1)
}

func TestTableDefDescriptionAPI(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)

	// create with description
	w := do(s, "POST", "/api/tables", defBodyDesc(s, "Customer orders"), c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(w.Body.String()), &created)

	// GET reflects it
	w = do(s, "GET", "/api/tables/"+created.ID, "", c)
	if !strings.Contains(w.Body.String(), `"description":"Customer orders"`) {
		t.Fatalf("get description: %s", w.Body)
	}

	// PUT with empty description clears it
	if w := do(s, "PUT", "/api/tables/"+created.ID, defBodyDesc(s, ""), c); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/"+created.ID, "", c)
	if strings.Contains(w.Body.String(), `"description":"Customer orders"`) {
		t.Fatalf("description not cleared: %s", w.Body)
	}
}

func TestTableDefDescriptionValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)

	// over 200 chars -> 400 VALIDATION
	if w := do(s, "POST", "/api/tables", defBodyDesc(s, strings.Repeat("x", 201)), c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "description too long") {
		t.Fatalf("long description = %d %s", w.Code, w.Body)
	}
	// exactly 200 is allowed (its own table name — table names are a
	// globally unique namespace, see TestTableDefDuplicateNameRejected)
	if w := do(s, "POST", "/api/tables", strings.Replace(defBodyDesc(s, strings.Repeat("x", 200)),
		`"tableName":"customers"`, `"tableName":"customers200"`, 1), c); w.Code != 200 {
		t.Fatalf("200-char description = %d %s", w.Code, w.Body)
	}

	// whitespace is trimmed on create
	if w := do(s, "POST", "/api/tables", defBodyDesc(s, "  Padded label  "), c); w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	w := do(s, "GET", "/api/tables/"+s.ids.Encode("td", 2), "", c)
	if !strings.Contains(w.Body.String(), `"description":"Padded label"`) {
		t.Fatalf("description not trimmed: %s", w.Body)
	}
	// and on update
	if w := do(s, "PUT", "/api/tables/"+s.ids.Encode("td", 2), defBodyDesc(s, "  Trimmed on update  "), c); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/"+s.ids.Encode("td", 2), "", c)
	if !strings.Contains(w.Body.String(), `"description":"Trimmed on update"`) {
		t.Fatalf("update description not trimmed: %s", w.Body)
	}
}

// seedCustomersPG makes the live fixture deterministic: reset public schema and
// create the same customers table Task 5's integration test seeds.
func seedCustomersPG(t *testing.T) {
	t.Helper()
	db, err := sql.Open("pgx", os.Getenv("KUCRUD_TEST_PG"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;
		CREATE TYPE weather AS ENUM ('sunny','rainy');
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL,
			active bool DEFAULT true, balance numeric(10,2), born date,
			status weather, meta jsonb);`); err != nil {
		t.Fatalf("seed live PG: %v", err)
	}
}

func TestIntrospectionEndpoints(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	seedCustomersPG(t)
	s := newTestServer(t)
	c := login(s)
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable",
		Raw: os.Getenv("KUCRUD_TEST_PG")}); err != nil {
		t.Fatal(err)
	}
	w := do(s, "GET", "/api/datasources/"+s.ids.Encode("ds", 1)+"/tables", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "customers") {
		t.Fatalf("tables = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/datasources/"+s.ids.Encode("ds", 1)+"/tables/public/customers/columns", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"fieldType":"enum"`) {
		t.Fatalf("columns = %d %s", w.Code, w.Body)
	}
}

// TestTableDefDuplicateNameRejected pins the global table-name namespace:
// /api/data/{name} is name-addressed, so a second def sharing a TableName
// (even on another schema or datasource) would serve one def's page with
// another def's rows — rejected at save with the group-taken error shape.
func TestTableDefDuplicateNameRejected(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	if w := do(s, "POST", "/api/tables", defBody(s), c); w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	id := tdTok(s, 1)

	// same table name in another schema → still taken (the namespace is global)
	w := do(s, "POST", "/api/tables", strings.Replace(defBody(s),
		`"schemaName":"public"`, `"schemaName":"sales"`, 1), c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "TABLE_NAME_TAKEN") {
		t.Fatalf("dup name = %d %s", w.Code, w.Body)
	}

	// update keeping its own name → OK
	if w = do(s, "PUT", "/api/tables/"+id, strings.Replace(defBody(s),
		`"label":"Customers"`, `"label":"Customers v2"`, 1), c); w.Code != 200 {
		t.Fatalf("own name on update = %d %s", w.Code, w.Body)
	}

	// rename to a free name → OK, and the freed name is claimable again
	if w = do(s, "PUT", "/api/tables/"+id, strings.Replace(defBody(s),
		`"tableName":"customers"`, `"tableName":"clients"`, 1), c); w.Code != 200 {
		t.Fatalf("rename = %d %s", w.Code, w.Body)
	}
	if w = do(s, "POST", "/api/tables", defBody(s), c); w.Code != 200 {
		t.Fatalf("reclaim freed name = %d %s", w.Code, w.Body)
	}

	// update onto another def's name → 409
	if w = do(s, "PUT", "/api/tables/"+id, defBody(s), c); w.Code != 409 ||
		!strings.Contains(w.Body.String(), "TABLE_NAME_TAKEN") {
		t.Fatalf("update to taken name = %d %s", w.Code, w.Body)
	}
}

// fkDefBody creates a customers def, then an orders def whose customer_id is
// an fk to it. Returns the orders create payload (fkTableDefId token).
func fkDefBody(s *Server) string {
	return `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"orders",
"label":"Orders","keyColumns":["id"],"pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":false,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"customer_id","label":"Customer","fieldType":"fk","baseType":"number",
  "fkTableDefId":"` + s.ids.Encode("td", 1) + `","fkRefColumn":"id","fkDisplayColumns":["id","name"],
  "editable":true,"required":false,"visible":true,"searchable":true,"sortable":true,"position":1}]}`
}

func seedParentDef(t *testing.T, s *Server) {
	t.Helper()
	parent := `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"customers",
"label":"Customers","keyColumns":["id"],"pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":false,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"name","label":"Name","fieldType":"text","editable":true,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":1}]}`
	if w := do(s, "POST", "/api/tables", parent, login(s)); w.Code != 200 {
		t.Fatalf("parent = %d %s", w.Code, w.Body)
	}
}

func TestTableDefFKValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	seedParentDef(t, s)

	// valid fk def → 200, DTO carries masked fk id + display columns
	w := do(s, "POST", "/api/tables", fkDefBody(s), c)
	if w.Code != 200 {
		t.Fatalf("create fk def = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"fkTableDefId":"`+s.ids.Encode("td", 1)+`"`) ||
		!strings.Contains(w.Body.String(), `"baseType":"number"`) ||
		!strings.Contains(w.Body.String(), `"fkDisplayColumns":["id","name"]`) {
		t.Fatalf("dto missing fk fields: %s", w.Body)
	}

	// fk to unknown def → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		s.ids.Encode("td", 1), s.ids.Encode("td", 99), 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "fk") {
		t.Fatalf("unknown target = %d %s", w.Code, w.Body)
	}

	// undecodable fk token → 400, not silently treated as self-ref
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		s.ids.Encode("td", 1), "zzz", 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "fk needs fkTableDefId") {
		t.Fatalf("garbage token = %d %s", w.Code, w.Body)
	}

	// missing fkTableDefId entirely → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		`"fkTableDefId":"`+s.ids.Encode("td", 1)+`",`, ``, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "fk needs fkTableDefId") {
		t.Fatalf("missing fkTableDefId = %d %s", w.Code, w.Body)
	}

	// duplicate fkDisplayColumns → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		`"fkDisplayColumns":["id","name"]`, `"fkDisplayColumns":["id","id"]`, 1), c)
	if w.Code != 400 {
		t.Fatalf("dup display col = %d %s", w.Code, w.Body)
	}

	// fkRefColumn not on target → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		`"fkRefColumn":"id"`, `"fkRefColumn":"nope"`, 1), c)
	if w.Code != 400 {
		t.Fatalf("bad ref col = %d %s", w.Code, w.Body)
	}

	// display column not on target → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		`"fkDisplayColumns":["id","name"]`, `"fkDisplayColumns":["zzz"]`, 1), c)
	if w.Code != 400 {
		t.Fatalf("bad display col = %d %s", w.Code, w.Body)
	}

	// fk without baseType → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		`"baseType":"number",`, ``, 1), c)
	if w.Code != 400 {
		t.Fatalf("no baseType = %d %s", w.Code, w.Body)
	}

	// non-fk column carrying fk fields → 400
	w = do(s, "POST", "/api/tables", strings.Replace(fkDefBody(s),
		`"fieldType":"fk"`, `"fieldType":"number"`, 1), c)
	if w.Code != 400 {
		t.Fatalf("fk fields on non-fk = %d %s", w.Code, w.Body)
	}

	// self reference via "self" sentinel → 200
	self := `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"cats",
"label":"Cats","keyColumns":["id"],"pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":false,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"parent_id","label":"Parent","fieldType":"fk","baseType":"number",
  "fkTableDefId":"self","fkRefColumn":"id","fkDisplayColumns":["id"],
  "editable":true,"visible":true,"position":1}]}`
	w = do(s, "POST", "/api/tables", self, c)
	if w.Code != 200 {
		t.Fatalf("self ref = %d %s", w.Code, w.Body)
	}
	// stored def reports its own token back
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 3), "", c); !strings.Contains(w.Body.String(),
		`"fkTableDefId":"`+tdTok(s, 3)+`"`) {
		t.Fatalf("self token round-trip: %s", w.Body)
	}
}

func TestTableValidationsRoundtripAndReject(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	var m map[string]any
	json.Unmarshal([]byte(defBody(s)), &m)
	col0 := m["columns"].([]any)[0].(map[string]any)

	col0["validations"] = []map[string]any{{"type": "email"}, {"type": "max_len", "param": 50}}
	w := do(s, "POST", "/api/tables", string(mustJSON(m)), c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"validations":[{"type":"email"},{"type":"max_len","param":50}]`) {
		t.Fatalf("validations not roundtripped: %s", w.Body)
	}

	col0["validations"] = []map[string]any{{"type": "nope"}}
	w = do(s, "POST", "/api/tables", string(mustJSON(m)), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid validation rule") {
		t.Fatalf("bad rule not rejected: %d %s", w.Code, w.Body)
	}
}
