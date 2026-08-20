package ds

import (
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
