package ds

import (
	"reflect"
	"strings"
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	q, err := QuoteIdent("user_name")
	if err != nil || q != `"user_name"` {
		t.Fatalf("got %q %v", q, err)
	}
	for _, bad := range []string{`name"; DROP TABLE x`, `has"quote`, "has space", "", "1abc", "col-x", "a b"} {
		if _, err := QuoteIdent(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestBuildList(t *testing.T) {
	p := ListParams{
		Schema:     "public",
		Table:      "customers",
		Columns:    []string{"id", "name", "status"},
		Searchable: []string{"name", "status"},
		Search:     "jo",
		SortCol:    "name", SortDir: "DESC",
		Limit: 20, Offset: 40,
	}
	sql, args, err := BuildList(p)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "id","name","status" FROM "public"."customers" ` +
		`WHERE ("name"::text ILIKE $1 OR "status"::text ILIKE $2) ` +
		`ORDER BY "name" DESC LIMIT $3 OFFSET $4`
	if sql != want {
		t.Fatalf("\n got: %s\nwant: %s", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"%jo%", "%jo%", 20, 40}) {
		t.Fatalf("args=%v", args)
	}

	// no search → no WHERE; only limit/offset args remain
	p.Search = ""
	sql, args, _ = BuildList(p)
	if !reflect.DeepEqual(args, []any{20, 40}) {
		t.Fatalf("expected only limit/offset args, got %v", args)
	}
	if strings.Contains(sql, "WHERE") {
		t.Fatalf("unexpected WHERE: %s", sql)
	}
	if !strings.Contains(sql, "LIMIT $1 OFFSET $2") {
		t.Fatalf("placeholder numbering wrong: %s", sql)
	}

	// sort col not in columns → error
	p.SortCol = "hax"
	if _, _, err := BuildList(p); err == nil {
		t.Fatal("sort injection accepted")
	}
	// bad dir → error
	p.SortCol, p.SortDir = "name", "ASC; DROP TABLE x"
	if _, _, err := BuildList(p); err == nil {
		t.Fatal("bad dir accepted")
	}
	// bad identifier in columns → error
	p.SortCol, p.SortDir = "name", "ASC"
	p.Columns = []string{"id", "na me"}
	if _, _, err := BuildList(p); err == nil {
		t.Fatal("bad column identifier accepted")
	}
}

func TestBuildCount(t *testing.T) {
	p := ListParams{Schema: "public", Table: "t", Columns: []string{"id"},
		Searchable: []string{"id"}, Search: "x", SortCol: "id", SortDir: "ASC"}
	sql, args, err := BuildCount(p)
	if err != nil {
		t.Fatal(err)
	}
	if sql != `SELECT COUNT(*) FROM "public"."t" WHERE ("id"::text ILIKE $1)` ||
		!reflect.DeepEqual(args, []any{"%x%"}) {
		t.Fatalf("got %s %v", sql, args)
	}
}

func TestBuildInsert(t *testing.T) {
	sql, n, err := BuildInsert("public", "customers", []string{"name", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `INSERT INTO "public"."customers" ("name","status") VALUES ($1,$2)` || n != 2 {
		t.Fatalf("got %s n=%d", sql, n)
	}
	if _, _, err := BuildInsert("public", "t", []string{"na me"}); err == nil {
		t.Fatal("bad identifier accepted")
	}
}

func TestBuildUpdateByPK(t *testing.T) {
	sql, n, err := BuildUpdateByPK("public", "customers", "id", []string{"name", "active"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `UPDATE "public"."customers" SET "name"=$1,"active"=$2 WHERE "id"=$3` || n != 2 {
		t.Fatalf("got %s n=%d", sql, n)
	}
}

func TestBuildDeleteByPK(t *testing.T) {
	sql, err := BuildDeleteByPK("public", "customers", "id")
	if err != nil {
		t.Fatal(err)
	}
	if sql != `DELETE FROM "public"."customers" WHERE "id"=$1` {
		t.Fatalf("got %s", sql)
	}
}

func TestBuildFetchByPK(t *testing.T) {
	sql, err := BuildFetchByPK("public", "customers", "id", []string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `SELECT "id","name" FROM "public"."customers" WHERE "id"=$1` {
		t.Fatalf("got %s", sql)
	}
}
