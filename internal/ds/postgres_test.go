package ds

import (
	"database/sql"
	"os"
	"testing"
)

func pgTestDS() Conn {
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		return Conn{}
	}
	return Conn{Host: "x", Port: 1, DB: "x", User: "x",
		Password: "x", SSLMode: "disable", Raw: cs, Driver: "postgres"}
}

func TestPostgresAdapterCRUD(t *testing.T) {
	d := pgTestDS()
	if d.Raw == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	a, err := Open(d)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()
	if err := a.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	seed := `DROP SCHEMA public CASCADE; CREATE SCHEMA public;
		CREATE TABLE people(id serial PRIMARY KEY, name varchar(80) NOT NULL, balance numeric(10,2));
		INSERT INTO people(name,balance) VALUES ('jo',10.5),('joe',2),('ana',NULL);`
	db := rawSQLPG(t)
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tables, err := a.ListTables()
	if err != nil || len(tables) == 0 {
		t.Fatalf("listTables: %v %+v", err, tables)
	}
	cols, err := a.InspectTable("public", "people")
	if err != nil || len(cols) != 3 || !cols[0].IsPK || cols[0].FieldType != "number" {
		t.Fatalf("inspect: %v %+v", err, cols)
	}

	lp := ListParams{Schema: "public", Table: "people", Columns: []string{"id", "name"},
		Searchable: []string{"name"}, Search: "jo", SortCol: "id", SortDir: "ASC", Limit: 10, Offset: 0}
	rows, err := a.ListRows(lp)
	if err != nil || len(rows) != 2 {
		t.Fatalf("listRows: %v %+v", err, rows)
	}
	total, err := a.CountRows(lp)
	if err != nil || total != 2 {
		t.Fatalf("countRows: %v %d", err, total)
	}

	got, err := a.FetchByKey("public", "people", []string{"id"}, []any{1}, []string{"id", "name"})
	if err != nil || len(got) != 1 || got[0]["name"] != "jo" {
		t.Fatalf("fetchByKey: %v %+v", err, got)
	}

	if err := a.Insert("public", "people", []string{"name", "balance"}, []any{"zoe", 9}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := a.UpdateByKey("public", "people", []string{"name"}, []any{"zoe2"}, []string{"name"}, []any{"zoe"})
	if err != nil || n != 1 {
		t.Fatalf("update: %v %d", err, n)
	}
	rel, err := a.FetchByRefValues("public", "people", "name", []string{"id"}, []any{"jo", "joe"})
	if err != nil || len(rel) != 2 || rel["jo"]["name"] != "jo" {
		t.Fatalf("fetchByRefValues: %v %+v", err, rel)
	}
	cnt, err := a.CountByRefEq("public", "people", "name", "joe")
	if err != nil || cnt != 1 {
		t.Fatalf("countByRefEq: %v %d", err, cnt)
	}
	n, err = a.DeleteByKey("public", "people", []string{"name"}, []any{"zoe2"})
	if err != nil || n != 1 {
		t.Fatalf("delete: %v %d", err, n)
	}
}

// rawSQLPG opens a raw *sql.DB for seeding (same DSN the adapter uses).
func rawSQLPG(t *testing.T) *sql.DB {
	t.Helper()
	a, err := Open(pgTestDS())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a.(*pgAdapter).db
}
