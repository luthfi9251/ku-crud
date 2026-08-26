package ds

import (
	"fmt"
	"strings"
	"testing"
)

func TestDialectQuoting(t *testing.T) {
	q, err := pgDialect.quoteIdent("col")
	if err != nil || q != `"col"` {
		t.Fatalf("pg quote: %q %v", q, err)
	}
	q, err = mysqlDialect.quoteIdent("col")
	if err != nil || q != "`col`" {
		t.Fatalf("mysql quote: %q %v", q, err)
	}
	if _, err := mysqlDialect.quoteIdent("bad col"); err == nil {
		t.Fatal("mysql allowlist must reject")
	}
}

func TestMysqlBuildList(t *testing.T) {
	p := ListParams{Schema: "app", Table: "users", Columns: []string{"id", "name"},
		Searchable: []string{"name"}, Search: "jo%", SortCol: "name", SortDir: "DESC",
		Limit: 20, Offset: 40}
	sql, args, err := mysqlDialect.buildList(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `id`,`name` FROM `app`.`users` WHERE (CAST(`name` AS CHAR) LIKE ? ESCAPE '\\\\') ORDER BY `name` DESC LIMIT ? OFFSET ?"
	if sql != want {
		t.Fatalf("got  %s\nwant %s", sql, want)
	}
	if len(args) != 3 || args[0] != "%jo\\%%" || args[1] != 20 || args[2] != 40 {
		t.Fatalf("args: %v", args)
	}
}

func TestPgBuildList(t *testing.T) {
	p := ListParams{Schema: "app", Table: "users", Columns: []string{"id"},
		Searchable: []string{"id"}, Search: "x", SortCol: "id", SortDir: "ASC",
		Limit: 1, Offset: 0}
	sql, args, err := pgDialect.buildList(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `"id"::text ILIKE $1`) || !strings.Contains(sql, "LIMIT $2 OFFSET $3") {
		t.Fatalf("pg list: %s", sql)
	}
	if len(args) != 3 {
		t.Fatalf("args: %v", args)
	}
}

func TestDialectBuilds(t *testing.T) {
	// insert (mysql: all ?)
	sql, err := mysqlDialect.buildInsert("s", "t", []string{"a", "b"}, []any{1, "x"})
	if err != nil || sql != "INSERT INTO `s`.`t` (`a`,`b`) VALUES (?,?)" {
		t.Fatalf("mysql insert: %s %v", sql, err)
	}
	// insert (pg: numbered)
	sql, err = pgDialect.buildInsert("s", "t", []string{"a", "b"}, []any{1, "x"})
	if err != nil || sql != `INSERT INTO "s"."t" ("a","b") VALUES ($1,$2)` {
		t.Fatalf("pg insert: %s %v", sql, err)
	}
	// update by key
	sql, args, err := mysqlDialect.buildUpdateByKey("s", "t", []string{"n"}, []any{"v"}, []string{"id"}, []any{7})
	if err != nil || sql != "UPDATE `s`.`t` SET `n`=? WHERE `id`=?" || len(args) != 2 || args[0] != "v" || args[1] != 7 {
		t.Fatalf("mysql update: %s %v", sql, args)
	}
	// delete by key (pg numbered)
	sql, args, err = pgDialect.buildDeleteByKey("s", "t", []string{"id"}, []any{9})
	if err != nil || sql != `DELETE FROM "s"."t" WHERE "id"=$1` || len(args) != 1 {
		t.Fatalf("pg delete: %s %v", sql, args)
	}
	// fetch by key (mysql)
	sql, args, err = mysqlDialect.buildFetchByKey("s", "t", []string{"a", "b"}, []any{1, 2}, []string{"a"})
	if err != nil || sql != "SELECT `a` FROM `s`.`t` WHERE `a`=? AND `b`=?" || len(args) != 2 {
		t.Fatalf("mysql fetch: %s %v", sql, args)
	}
	// fetch by ref values dedupes ref col out of display cols
	sql, args, err = pgDialect.buildFetchByRefValues("s", "t", "id", []string{"id", "name"}, []any{1, 2, 3})
	if err != nil || sql != `SELECT "id","name" FROM "s"."t" WHERE "id" IN ($1,$2,$3)` || len(args) != 3 {
		t.Fatalf("pg refvals: %s %v", sql, args)
	}
	// count by ref eq
	sql, args, err = mysqlDialect.buildCountByRefEq("s", "t", "c", 5)
	if err != nil || sql != "SELECT COUNT(*) FROM `s`.`t` WHERE `c`=?" || len(args) != 1 {
		t.Fatalf("mysql countref: %s %v", sql, args)
	}
}

func TestBuildValidationErrors(t *testing.T) {
	bad := ListParams{Schema: "s", Table: "t", Columns: []string{"id"}, SortCol: "nope",
		SortDir: "ASC", Limit: 1, Offset: 0}
	if _, _, err := pgDialect.buildList(bad); err == nil {
		t.Fatal("sort col not selectable must error")
	}
	if _, err := mysqlDialect.buildInsert("s", "t", nil, nil); err == nil {
		t.Fatal("empty insert must error")
	}
	if _, _, err := pgDialect.buildFetchByRefValues("s", "t", "id", nil, nil); err == nil {
		t.Fatal("empty refvals must error")
	}
}

func TestScanHelpers(t *testing.T) {
	scan := scanTargets(2)
	if len(scan) != 2 {
		t.Fatal("len")
	}
	vals := deref(scan)
	m := rowToMap([]string{"a", "b"}, vals)
	if len(m) != 2 {
		t.Fatal("map")
	}
}

func TestFilterBuildPG(t *testing.T) {
	p := ListParams{Schema: "app", Table: "users", Columns: []string{"id", "age", "name"},
		SortCol: "id", SortDir: "ASC", Limit: 20, Offset: 0,
		Filters: []ColumnFilter{
			{Column: "age", Op: "between", Values: []any{float64(18), float64(30)}},
			{Column: "name", Op: "contains", Values: []any{"jo"}},
			{Column: "id", Op: "in", Values: []any{float64(1), float64(2), float64(3)}},
		}}
	sql, args, err := pgDialect.buildList(p)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "id","age","name" FROM "app"."users" WHERE ("age" >= $1 AND "age" <= $2)` +
		` AND ("name"::text ILIKE $3 ESCAPE '\') AND ("id" IN ($4,$5,$6))` +
		` ORDER BY "id" ASC LIMIT $7 OFFSET $8`
	if sql != want {
		t.Fatalf("sql:\n%s\nwant:\n%s", sql, want)
	}
	if len(args) != 8 || args[0] != 18.0 || args[1] != 30.0 || args[2] != "%jo%" ||
		args[3] != 1.0 || args[4] != 2.0 || args[5] != 3.0 ||
		args[6] != 20 || args[7] != 0 {
		t.Fatalf("args = %#v", args)
	}
}

func TestFilterBuildFKJoin(t *testing.T) {
	p := ListParams{Schema: "app", Table: "orders", Columns: []string{"id", "customer_id"},
		SortCol: "id", SortDir: "ASC", Limit: 10, Offset: 0,
		Filters: []ColumnFilter{{
			Column: "customer_id", Op: "contains", Values: []any{"acme"},
			Join: &FKJoin{Schema: "app", Table: "customers", RefColumn: "id", DisplayColumns: []string{"name"}},
		}}}
	sql, args, err := pgDialect.buildList(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sql, `SELECT "orders"."id","orders"."customer_id" FROM "app"."orders"`) {
		t.Fatalf("base columns must be qualified under a join:\n%s", sql)
	}
	if !strings.Contains(sql, `ORDER BY "orders"."id" ASC`) {
		t.Fatalf("sort must be qualified under a join:\n%s", sql)
	}
	if !strings.Contains(sql, `LEFT JOIN "app"."customers" "f_customer_id" ON "f_customer_id"."id" = "orders"."customer_id"`) ||
		!strings.Contains(sql, `"f_customer_id"."name"::text ILIKE $1 ESCAPE '\'`) {
		t.Fatalf("fk join sql wrong:\n%s", sql)
	}
	if len(args) != 3 || args[0] != "%acme%" {
		t.Fatalf("args = %#v", args)
	}
	// eq variant matches the raw display column
	p.Filters[0].Op = "eq"
	p.Filters[0].Values = []any{"Acme Corp"}
	sql, _, _ = pgDialect.buildList(p)
	if !strings.Contains(sql, `"f_customer_id"."name" = $1`) {
		t.Fatalf("fk eq sql wrong:\n%s", sql)
	}
	// count joins too, so totals stay consistent
	csql, _, err := pgDialect.buildCount(p)
	if err != nil || !strings.Contains(csql, "LEFT JOIN") {
		t.Fatalf("count join missing: %s err=%v", csql, err)
	}
}

func TestFilterBuildSearchCombinedMySQL(t *testing.T) {
	p := ListParams{Schema: "app", Table: "users", Columns: []string{"id"}, Searchable: []string{"name"},
		Search: "x", SortCol: "id", SortDir: "ASC", Limit: 1, Offset: 0,
		Filters: []ColumnFilter{{Column: "id", Op: "eq", Values: []any{float64(5)}}}}
	sql, _, err := mysqlDialect.buildList(p)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "SELECT `id` FROM `app`.`users` WHERE (CAST(`name` AS CHAR) LIKE ? ESCAPE '\\\\') AND (`id` = ?)"
	if !strings.HasPrefix(sql, prefix) {
		t.Fatalf("combined where wrong:\n%s\nwant prefix:\n%s", sql, prefix)
	}
}

func TestFilterBadColumnRejected(t *testing.T) {
	p := ListParams{Schema: "app", Table: "users", Columns: []string{"id"}, SortCol: "id", SortDir: "ASC",
		Filters: []ColumnFilter{{Column: "id; DROP TABLE users", Op: "eq", Values: []any{float64(1)}}}}
	if _, _, err := pgDialect.buildList(p); err == nil {
		t.Fatal("injection-like column must be rejected")
	}
}

func TestBuildAggregate(t *testing.T) {
	cases := []struct {
		name string
		dt   sqlDialect
		p    AggregateParams
		sql  string
		args []any
	}{
		{"pg count", pgDialect, AggregateParams{Schema: "public", Table: "orders", Func: "count"},
			`SELECT COUNT(*),COUNT(*) FROM "public"."orders"`, nil},
		{"pg sum+filter", pgDialect, AggregateParams{Schema: "public", Table: "orders", Func: "sum", Column: "amount",
			Filters: []ColumnFilter{{Column: "status", Op: "eq", Values: []any{"paid"}}}},
			`SELECT SUM("amount"),COUNT(*) FROM "public"."orders" WHERE ("status" = $1)`, []any{"paid"}},
		{"pg min+fkjoin", pgDialect, AggregateParams{Schema: "public", Table: "orders", Func: "min", Column: "created",
			Filters: []ColumnFilter{{Column: "cust", Op: "contains", Values: []any{"acme"}, Join: &FKJoin{Schema: "public", Table: "customers", RefColumn: "id", DisplayColumns: []string{"name"}}}}},
			`SELECT MIN("orders"."created"),COUNT(*) FROM "public"."orders" LEFT JOIN "public"."customers" "f_cust" ON "f_cust"."id" = "orders"."cust" WHERE ("f_cust"."name"::text ILIKE $1 ESCAPE '\')`, []any{"%acme%"}},
		{"mysql avg", mysqlDialect, AggregateParams{Schema: "shop", Table: "orders", Func: "avg", Column: "amount"},
			"SELECT AVG(`amount`),COUNT(*) FROM `shop`.`orders`", nil},
	}
	for _, c := range cases {
		sqlText, args, err := c.dt.buildAggregate(c.p)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if sqlText != c.sql {
			t.Errorf("%s sql:\n got %s\nwant %s", c.name, sqlText, c.sql)
		}
		if fmt.Sprint(args) != fmt.Sprint(c.args) {
			t.Errorf("%s args: got %v want %v", c.name, args, c.args)
		}
	}
}

func TestBuildAggregateValidation(t *testing.T) {
	if _, _, err := pgDialect.buildAggregate(AggregateParams{Schema: "public", Table: "t", Func: "median", Column: "x"}); err == nil {
		t.Fatal("unknown func accepted")
	}
	if _, _, err := pgDialect.buildAggregate(AggregateParams{Schema: "public", Table: "t", Func: "sum", Column: ""}); err == nil {
		t.Fatal("sum without column accepted")
	}
	if _, _, err := pgDialect.buildAggregate(AggregateParams{Schema: "public", Table: "t", Func: "count", Column: "x"}); err == nil {
		t.Fatal("count with column accepted")
	}
	if _, _, err := pgDialect.buildAggregate(AggregateParams{Schema: "public", Table: "t", Func: "sum", Column: "a;b"}); err == nil {
		t.Fatal("bad identifier accepted")
	}
}

func TestBuildQueryAggregate(t *testing.T) {
	sqlText, args, err := pgDialect.buildQueryAggregate(AggregateParams{Query: "SELECT id, amount FROM orders", Func: "sum", Column: "amount",
		Filters: []ColumnFilter{{Column: "id", Op: "gt", Values: []any{float64(5)}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT SUM(ku_q."amount"),COUNT(*) FROM (SELECT id, amount FROM orders) ku_q WHERE (ku_q."id" > $1)`
	if sqlText != want {
		t.Errorf("sql:\n got %s\nwant %s", sqlText, want)
	}
	if fmt.Sprint(args) != fmt.Sprint([]any{float64(5)}) {
		t.Errorf("args: got %v", args)
	}
	if _, _, err := pgDialect.buildQueryAggregate(AggregateParams{Query: "SELECT 1", Func: "count",
		Filters: []ColumnFilter{{Column: "c", Op: "eq", Values: []any{1}, Join: &FKJoin{Schema: "s", Table: "t", RefColumn: "id", DisplayColumns: []string{"n"}}}}}); err == nil {
		t.Fatal("fk join on query view accepted")
	}
}
