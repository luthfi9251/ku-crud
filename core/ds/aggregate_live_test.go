package ds

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
)

// seedAggData creates t_agg(id, n, s) with rows (1,10,'a'), (2,20,'b'),
// (3,NULL,'a') over a raw *sql.DB (adapters keep their pool private).
func seedAggData(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("DROP TABLE IF EXISTS t_agg"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t_agg (id INTEGER PRIMARY KEY, n INT, s TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		id int
		n  *int
		s  string
	}{
		{1, func() *int { v := 10; return &v }(), "a"},
		{2, func() *int { v := 20; return &v }(), "b"},
		{3, nil, "a"},
	} {
		if _, err := db.Exec("INSERT INTO t_agg (id, n, s) VALUES (?,?,?)", r.id, r.n, r.s); err != nil {
			t.Fatal(err)
		}
	}
}

func num(t *testing.T, v any) float64 {
	t.Helper()
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case string:
		var f float64
		if _, err := fmt.Sscanf(x, "%g", &f); err != nil {
			t.Fatalf("unparseable number %q", x)
		}
		return f
	}
	t.Fatalf("not a number: %#v", v)
	return -1
}

func TestAggregateRowsLivePG(t *testing.T) {
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	raw, err := sql.Open("pgx", cs)
	if err != nil {
		t.Fatal(err)
	}
	seedAggData(t, raw)
	raw.Close()

	a := openPG(t)
	r, err := a.AggregateRows(AggregateParams{Schema: "public", Table: "t_agg", Func: "count"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value == nil || num(t, r.Value) != 3 || !r.HasRows {
		t.Fatalf("count = %#v hasRows=%v", r.Value, r.HasRows)
	}

	r, err = a.AggregateRows(AggregateParams{Schema: "public", Table: "t_agg", Func: "sum", Column: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value == nil || num(t, r.Value) != 30 {
		t.Fatalf("sum = %#v", r.Value)
	}

	// zero-row set: aggregate NULL + hasRows false
	r, err = a.AggregateRows(AggregateParams{Schema: "public", Table: "t_agg", Func: "sum", Column: "n",
		Filters: []ColumnFilter{{Column: "s", Op: "eq", Values: []any{"zzz"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != nil || r.HasRows {
		t.Fatalf("empty sum = %#v hasRows=%v", r.Value, r.HasRows)
	}

	// query-view mode wraps stored SQL under the read-only tx
	r, err = a.AggregateRows(AggregateParams{Query: "SELECT n, s FROM t_agg", Func: "avg", Column: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value == nil || num(t, r.Value) != 15 {
		t.Fatalf("query avg = %#v", r.Value)
	}
}

func TestAggregateRowsLiveMySQL(t *testing.T) {
	dsn := os.Getenv("KUCRUD_TEST_MYSQL")
	if dsn == "" {
		t.Skip("KUCRUD_TEST_MYSQL not set")
	}
	raw, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	seedAggData(t, raw)
	raw.Close()

	a := openMy(t)
	r, err := a.AggregateRows(AggregateParams{Table: "t_agg", Func: "count"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value == nil || num(t, r.Value) != 3 || !r.HasRows {
		t.Fatalf("count = %#v hasRows=%v", r.Value, r.HasRows)
	}

	r, err = a.AggregateRows(AggregateParams{Table: "t_agg", Func: "sum", Column: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value == nil || num(t, r.Value) != 30 {
		t.Fatalf("sum = %#v", r.Value)
	}

	r, err = a.AggregateRows(AggregateParams{Query: "SELECT n, s FROM t_agg", Func: "avg", Column: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value == nil || num(t, r.Value) != 15 {
		t.Fatalf("query avg = %#v", r.Value)
	}
}
