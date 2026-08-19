package api

import (
	"os"
	"strings"
	"testing"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

// seedLive spins up a live-table fixture: 3 customers rows + a full table def.
func seedLive(t *testing.T, s *Server) {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	db, err := ds.Connect(ds.DSN{Raw: cs})
	if err != nil {
		t.Skipf("no PG: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;
		CREATE TYPE weather AS ENUM ('sunny','rainy');
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL,
			active bool DEFAULT true, balance numeric(10,2), born date, status weather);
		INSERT INTO customers(name,balance,status) VALUES
			('jo', 10.5, 'sunny'), ('joe', 2, 'rainy'), ('ana', 0, 'sunny')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err) // id 1 — keeps ids deterministic
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 2, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 2}
	cols := []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "active", Label: "Active", FieldType: "boolean", Editable: true,
			Visible: true, Position: 2},
		{Name: "balance", Label: "Balance", FieldType: "number", Editable: true,
			Visible: true, Sortable: true, Position: 3},
		{Name: "born", Label: "Born", FieldType: "datetime", Editable: true,
			Visible: true, Position: 4},
		{Name: "status", Label: "Status", FieldType: "enum", EnumOptions: []string{"sunny", "rainy"},
			Editable: true, Visible: true, Searchable: true, Position: 5},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func TestRowListAndGet(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)

	w := do(s, "GET", "/api/tables/1/rows", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"total":3`) ||
		!strings.Contains(w.Body.String(), `"pageSize":2`) {
		t.Fatalf("page1 = %s", w.Body)
	}

	w = do(s, "GET", "/api/tables/1/rows?search=jo", "", c)
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("search = %s", w.Body)
	}

	w = do(s, "GET", "/api/tables/1/rows?sort=name&dir=DESC", "", c)
	body := w.Body.String()
	// DESC: "joe" sorts before "jo" (prefix compares less); brief's original
	// `<` was inverted — it fired on correct DESC output.
	if strings.Index(body, `"joe"`) > strings.Index(body, `"jo"`) {
		t.Fatalf("sort desc wrong: %s", body)
	}

	// sort on non-sortable column (born) silently falls back to pk ASC
	w = do(s, "GET", "/api/tables/1/rows?sort=born&dir=DESC", "", c)
	if !strings.Contains(w.Body.String(), `"total":3`) {
		t.Fatalf("fallback sort = %s", w.Body)
	}

	// page 2 with pageSize 2 → last row only
	w = do(s, "GET", "/api/tables/1/rows?page=2", "", c)
	if !strings.Contains(w.Body.String(), `"total":3`) ||
		strings.Count(w.Body.String(), `"name":`) != 1 {
		t.Fatalf("page2 = %s", w.Body)
	}

	w = do(s, "GET", "/api/tables/1/rows/"+encodeRowKey([]string{"2"}), "", c)
	if !strings.Contains(w.Body.String(), `"name":"joe"`) {
		t.Fatalf("get row = %s", w.Body)
	}
	if w = do(s, "GET", "/api/tables/1/rows/"+encodeRowKey([]string{"999"}), "", c); w.Code != 404 {
		t.Fatalf("missing row = %d", w.Code)
	}
}
