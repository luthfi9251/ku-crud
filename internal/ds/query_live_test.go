package ds

import (
	"os"
	"strings"
	"testing"
	"time"

	"ku-crud/internal/meta"
)

func openPG(t *testing.T) Adapter {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	a, err := openPostgres(meta.Datasource{Raw: cs})
	if err != nil {
		t.Skipf("no PG: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func openMy(t *testing.T) Adapter {
	t.Helper()
	dsn := os.Getenv("KUCRUD_TEST_MYSQL")
	if dsn == "" {
		t.Skip("KUCRUD_TEST_MYSQL not set")
	}
	a, err := openMySQL(meta.Datasource{Raw: dsn})
	if err != nil {
		t.Skipf("no MySQL: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestExplainQueryRejectsPG(t *testing.T) {
	a := openPG(t)
	if err := a.ExplainQuery("SELECT 1 AS one"); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
	for _, bad := range []string{
		"SELECT 1 AS one; DROP TABLE x",
		"SELECT $1 AS one",
		"SELECT FROM WHERE",
	} {
		if err := a.ExplainQuery(bad); err == nil {
			t.Fatalf("EXPLAIN accepted %q", bad)
		}
	}
}

func TestQueryWrapRejectsDMLPG(t *testing.T) {
	a := openPG(t)
	db := a.(*pgAdapter).db
	if _, err := db.Exec(`DROP TABLE IF EXISTS qv_dml; CREATE TABLE qv_dml(id serial PRIMARY KEY, v text)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TABLE IF EXISTS qv_dml`)
	})
	const dml = "UPDATE qv_dml SET v = 'x'"
	// PG EXPLAIN plans DML without executing; rejection happens at the
	// wrap/tx layers, so layer 1 accepts this by design.
	if err := a.ExplainQuery(dml); err != nil {
		t.Fatalf("EXPLAIN rejected DML on existing table: %v", err)
	}
	if _, _, err := a.IntrospectQuery(dml); err == nil {
		t.Fatal("introspect accepted DML")
	}
	if _, err := a.ListQueryRows(QueryParams{Query: dml,
		Columns: []string{"v"}, SortCol: "v", SortDir: "ASC", Limit: 1}); err == nil {
		t.Fatal("list accepted DML")
	}
}

func TestExplainQueryRejectsMySQL(t *testing.T) {
	a := openMy(t)
	if err := a.ExplainQuery("SELECT 1 AS one"); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
	for _, bad := range []string{
		"SELECT 1 AS one; DROP TABLE x",
		"SELECT ? AS one",
		"SELECT FROM WHERE",
	} {
		if err := a.ExplainQuery(bad); err == nil {
			t.Fatalf("EXPLAIN accepted %q", bad)
		}
	}
}

func TestQueryReadOnlyPG(t *testing.T) {
	a := openPG(t)
	db := a.(*pgAdapter).db
	if _, err := db.Exec(`DROP TABLE IF EXISTS qv_t; CREATE TABLE qv_t(id serial PRIMARY KEY, v text)`); err != nil {
		t.Fatal(err)
	}
	setval := "SELECT setval('qv_t_id_seq', 1) AS x"
	// Introspection wraps the query in LIMIT 0: PG skips volatile function
	// evaluation there, so this path cannot fire a side effect (nor error).
	if _, _, err := a.IntrospectQuery(setval); err != nil {
		t.Fatalf("introspect setval: %v", err)
	}
	// The execution wrappers really run the function; the read-only tx
	// (layer 2) must reject it.
	_, err := a.CountQueryRows(QueryParams{Query: setval})
	if err == nil {
		t.Fatal("side-effecting function succeeded in read-only tx")
	}
	if !strings.Contains(err.Error(), "read-only") && !strings.Contains(err.Error(), "25006") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryTimeoutPG(t *testing.T) {
	a := openPG(t)
	old := QueryTimeout
	QueryTimeout = 2 * time.Second
	t.Cleanup(func() { QueryTimeout = old })
	// pg_sleep returns void, which PG cannot ORDER BY; "IS NULL" yields a
	// sortable boolean while still forcing the sleep to execute.
	_, err := a.ListQueryRows(QueryParams{Query: "SELECT pg_sleep(30) IS NULL AS s",
		Columns: []string{"s"}, SortCol: "s", SortDir: "ASC", Limit: 1})
	if !IsQueryTimeout(err) {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestIntrospectQueryPG(t *testing.T) {
	a := openPG(t)
	cols, dropped, err := a.IntrospectQuery(
		"SELECT name AS n, balance, 1 + 1 FROM (VALUES ('jo', 10.5)) AS v(name, balance)")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 || cols[0].Name != "n" || cols[0].FieldType != "text" ||
		cols[1].FieldType != "number" {
		t.Fatalf("cols = %+v", cols)
	}
	if len(dropped) != 1 || dropped[0] != "?column?" {
		t.Fatalf("dropped = %v", dropped)
	}
}

// Enum output columns must survive introspection with their options —
// verify/resync drift detection depends on them. The fixture lives in its
// own schema: the api package's tests drop schema public mid-run when the
// two packages test in parallel.
func TestIntrospectQueryEnumPG(t *testing.T) {
	a := openPG(t)
	db := a.(*pgAdapter).db
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS qv_enum_test CASCADE;
		CREATE SCHEMA qv_enum_test;
		CREATE TYPE qv_enum_test.weather AS ENUM ('sunny','rainy');
		CREATE TABLE qv_enum_test.forecast(id serial PRIMARY KEY, w qv_enum_test.weather)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DROP SCHEMA IF EXISTS qv_enum_test CASCADE`)
	})
	cols, dropped, err := a.IntrospectQuery("SELECT w FROM qv_enum_test.forecast")
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 0 {
		t.Fatalf("dropped = %v", dropped)
	}
	if len(cols) != 1 || cols[0].Name != "w" || cols[0].FieldType != "enum" ||
		strings.Join(cols[0].EnumOptions, ",") != "sunny,rainy" {
		t.Fatalf("cols = %+v", cols)
	}
}

func TestListQueryRowsMySQL(t *testing.T) {
	a := openMy(t)
	rows, err := a.ListQueryRows(QueryParams{Query: "SELECT 1 AS one, 'x' AS txt",
		Columns: []string{"one", "txt"}, SortCol: "one", SortDir: "ASC", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["txt"] != "x" {
		t.Fatalf("rows = %+v", rows)
	}
	n, err := a.CountQueryRows(QueryParams{Query: "SELECT 1 AS one FROM DUAL WHERE 1=1"})
	if err != nil || n != 1 {
		t.Fatalf("count = %d %v", n, err)
	}
}
