package ds

import (
	"strings"
	"testing"
)

func baseQP() QueryParams {
	return QueryParams{Query: "SELECT a, b FROM t WHERE x > 5",
		Columns: []string{"a", "b"}, Searchable: []string{"a"},
		SortCol: "a", SortDir: "ASC", Limit: 20, Offset: 0}
}

func TestBuildQueryListPG(t *testing.T) {
	sqlText, args, err := pgDialect.buildQueryList(baseQP())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sqlText, `SELECT ku_q."a",ku_q."b" FROM (SELECT a, b FROM t WHERE x > 5) ku_q`) {
		t.Fatalf("prefix = %s", sqlText)
	}
	if !strings.Contains(sqlText, "ORDER BY ku_q.\"a\" ASC LIMIT $") {
		t.Fatalf("order/limit = %s", sqlText)
	}
	if len(args) != 2 { // limit + offset only
		t.Fatalf("args = %v", args)
	}
}

func TestBuildQueryListFiltersAndSearch(t *testing.T) {
	p := baseQP()
	p.Search = `%" OR 1=1; DROP TABLE t; --`
	p.Filters = []ColumnFilter{{Column: "b", Op: "eq", Values: []any{`1' OR '1'='1`}}}
	sqlText, args, err := pgDialect.buildQueryList(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, hostile := range []string{"OR 1=1", "DROP TABLE", "OR '1'='1"} {
		if strings.Contains(sqlText, hostile) {
			t.Fatalf("hostile %q reached SQL text: %s", hostile, sqlText)
		}
	}
	if len(args) != 4 { // 1 search + 1 filter + limit + offset
		t.Fatalf("args = %v", args)
	}
	// MySQL dialect: same structure, ? placeholders
	msql, _, err := mysqlDialect.buildQueryList(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msql, "?)") || strings.Contains(msql, "$1") {
		t.Fatalf("mysql placeholders = %s", msql)
	}
}

func TestBuildQueryValidation(t *testing.T) {
	p := baseQP()
	p.SortCol = `a"; DROP TABLE t; --`
	if _, _, err := pgDialect.buildQueryList(p); err == nil {
		t.Fatal("hostile sort col accepted")
	}
	p = baseQP()
	p.SortDir = "ASC; DELETE FROM t"
	if _, _, err := mysqlDialect.buildQueryList(p); err == nil {
		t.Fatal("hostile sort dir accepted")
	}
	p = baseQP()
	p.SortCol = "notincols"
	if _, _, err := pgDialect.buildQueryList(p); err == nil {
		t.Fatal("sort col outside selectable set accepted")
	}
	p = baseQP()
	p.Columns = []string{`b"; evil`}
	if _, _, err := pgDialect.buildQueryList(p); err == nil {
		t.Fatal("hostile column accepted")
	}
}

func TestBuildQueryCount(t *testing.T) {
	p := baseQP()
	p.Filters = []ColumnFilter{{Column: "b", Op: "gt", Values: []any{float64(3)}}}
	sqlText, args, err := pgDialect.buildQueryCount(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sqlText, "SELECT COUNT(*) FROM (SELECT a, b FROM t WHERE x > 5) ku_q WHERE (ku_q.\"b\" > $1)") {
		t.Fatalf("count = %s", sqlText)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v", args)
	}
}
