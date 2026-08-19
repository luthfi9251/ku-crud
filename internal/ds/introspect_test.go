package ds

import (
	"database/sql"
	"os"
	"testing"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set; run: docker compose up -d && KUCRUD_TEST_PG=postgres://ku:ku@localhost:5433/ku go test ./...")
	}
	db, err := sql.Open("pgx", cs)
	if err != nil {
		t.Skipf("connect failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMapFieldType(t *testing.T) {
	cases := map[string]string{
		"boolean":                     "boolean",
		"smallint":                    "number",
		"integer":                     "number",
		"bigint":                      "number",
		"numeric":                     "number",
		"real":                        "number",
		"double precision":            "number",
		"timestamp with time zone":    "datetime",
		"timestamp without time zone": "datetime",
		"date":                        "datetime",
		"time without time zone":      "datetime",
		"text":                        "text",
		"character varying":           "text",
		"character":                   "text",
		"json":                        "",
		"jsonb":                       "",
		"uuid":                        "",
		"bytea":                       "",
		"integer[]":                   "",
		"USER-DEFINED":                "", // enums handled by InspectTable, not the mapper
	}
	for in, want := range cases {
		if got := MapFieldType(in); got != want {
			t.Errorf("MapFieldType(%q)=%q want %q", in, got, want)
		}
	}
}

func TestInspectTableIntegration(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := db.Exec(`CREATE TYPE weather AS ENUM ('sunny','rainy');
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL,
			active bool DEFAULT true, balance numeric(10,2), born date,
			status weather, meta jsonb);`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tables, err := ListTables(db)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ti := range tables {
		if ti.Schema == "public" && ti.Name == "customers" {
			found = true
		}
	}
	if !found {
		t.Fatalf("customers not listed: %+v", tables)
	}

	cols, err := InspectTable(db, "public", "customers")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]LiveColumn{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if len(cols) != 6 { // 7 seeded columns, meta jsonb excluded
		t.Fatalf("cols=%d (%+v); jsonb must be excluded", len(cols), cols)
	}
	if !byName["id"].IsPK || byName["id"].FieldType != "number" {
		t.Fatalf("id: %+v", byName["id"])
	}
	if byName["status"].FieldType != "enum" || len(byName["status"].EnumOptions) != 2 {
		t.Fatalf("status: %+v", byName["status"])
	}
	if _, ok := byName["meta"]; ok {
		t.Fatal("jsonb column must be excluded")
	}
	if byName["name"].Nullable {
		t.Fatal("name is NOT NULL in schema")
	}
}
