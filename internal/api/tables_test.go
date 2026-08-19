package api

import (
	"database/sql"
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

const defBody = `{"datasourceId":1,"schemaName":"public","tableName":"customers",
"label":"Customers","pkColumn":"id","pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"status","label":"Status","fieldType":"enum","enumOptions":["a","b"],
  "editable":true,"required":false,"visible":true,"searchable":false,"sortable":false,"position":1}]}`

func TestTableDefEndpoints(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)

	w := do(s, "POST", "/api/tables", defBody, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}

	// PK not among columns → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody, `"pkColumn":"id"`, `"pkColumn":"nope"`, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "pk") {
		t.Fatalf("bad pk = %d %s", w.Code, w.Body)
	}

	// enum without options → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody, `"enumOptions":["a","b"],`, ``, 1), c)
	if w.Code != 400 {
		t.Fatalf("enum no options = %d %s", w.Code, w.Body)
	}

	// unknown fieldType → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody, `"fieldType":"enum"`, `"fieldType":"jsonb"`, 1), c)
	if w.Code != 400 {
		t.Fatalf("bad fieldType = %d %s", w.Code, w.Body)
	}

	// unquotable table name → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody, `"tableName":"customers"`, `"tableName":"bad table"`, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid identifier") {
		t.Fatalf("bad table name = %d %s", w.Code, w.Body)
	}

	// unquotable column name → VALIDATION
	w = do(s, "POST", "/api/tables", strings.Replace(defBody, `"name":"id"`, `"name":"i d"`, 1), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid identifier") {
		t.Fatalf("bad column name = %d %s", w.Code, w.Body)
	}

	w = do(s, "GET", "/api/tables", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Customers") {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/1", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"fieldType":"enum"`) {
		t.Fatalf("get = %d %s", w.Code, w.Body)
	}
	w = do(s, "PUT", "/api/tables/1", strings.Replace(defBody,
		`"label":"Customers"`, `"label":"Cust2"`, 1), c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Cust2") {
		t.Fatalf("put = %d %s", w.Code, w.Body)
	}
	if w = do(s, "PUT", "/api/tables/999", defBody, c); w.Code != 404 {
		t.Fatalf("put missing = %d", w.Code)
	}
	w = do(s, "DELETE", "/api/tables/1", "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d", w.Code)
	}
	if w = do(s, "GET", "/api/tables/1", "", c); w.Code != 404 {
		t.Fatalf("get deleted = %d", w.Code)
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
	w := do(s, "GET", "/api/datasources/1/tables", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "customers") {
		t.Fatalf("tables = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/datasources/1/tables/public/customers/columns", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"fieldType":"enum"`) {
		t.Fatalf("columns = %d %s", w.Code, w.Body)
	}
}
