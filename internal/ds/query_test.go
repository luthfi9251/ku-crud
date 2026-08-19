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
		`WHERE ("name"::text ILIKE $1 ESCAPE '\' OR "status"::text ILIKE $2 ESCAPE '\') ` +
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
	if sql != `SELECT COUNT(*) FROM "public"."t" WHERE ("id"::text ILIKE $1 ESCAPE '\')` ||
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
	if _, _, err := BuildInsert("public", "t", nil); err == nil {
		t.Fatal("empty cols accepted")
	}
}

func TestBuildUpdateByPK(t *testing.T) {
	sql, n, err := BuildUpdateByKey("public", "customers", []string{"name", "active"}, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `UPDATE "public"."customers" SET "name"=$1,"active"=$2 WHERE "id"=$3` || n != 2 {
		t.Fatalf("got %s n=%d", sql, n)
	}
	if _, _, err := BuildUpdateByKey("public", "t", nil, []string{"id"}); err == nil {
		t.Fatal("empty set cols accepted")
	}
}

func TestBuildUpdateByCompositeKey(t *testing.T) {
	sql, n, err := BuildUpdateByKey("public", "order_items", []string{"qty", "note"}, []string{"order_id", "item_id"})
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "public"."order_items" SET "qty"=$1,"note"=$2 WHERE "order_id"=$3 AND "item_id"=$4`
	if sql != want || n != 2 {
		t.Fatalf("\n got: %s n=%d\nwant: %s", sql, n, want)
	}
	// injection attempt in key column rejected
	if _, _, err := BuildUpdateByKey("public", "t", []string{"a"}, []string{"id; DROP TABLE x"}); err == nil {
		t.Fatal("key injection accepted")
	}
	if _, _, err := BuildUpdateByKey("public", "t", []string{"a"}, nil); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestBuildDeleteByPK(t *testing.T) {
	sql, err := BuildDeleteByKey("public", "customers", []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `DELETE FROM "public"."customers" WHERE "id"=$1` {
		t.Fatalf("got %s", sql)
	}
	sql, err = BuildDeleteByKey("public", "order_items", []string{"order_id", "item_id"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `DELETE FROM "public"."order_items" WHERE "order_id"=$1 AND "item_id"=$2` {
		t.Fatalf("got %s", sql)
	}
}

func TestBuildFetchByPK(t *testing.T) {
	sql, err := BuildFetchByKey("public", "customers", []string{"id"}, []string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `SELECT "id","name" FROM "public"."customers" WHERE "id"=$1` {
		t.Fatalf("got %s", sql)
	}
	sql, err = BuildFetchByKey("public", "order_items", []string{"a", "b"}, []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `SELECT "a","b","c" FROM "public"."order_items" WHERE "a"=$1 AND "b"=$2` {
		t.Fatalf("got %s", sql)
	}
}

func TestBuildFetchByRefValues(t *testing.T) {
	sql, err := BuildFetchByRefValues("public", "customers", "id", []string{"name", "city"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "id","name","city" FROM "public"."customers" WHERE "id" IN ($1,$2,$3)`
	if sql != want {
		t.Fatalf("got %s want %s", sql, want)
	}
	// display list containing the ref column is deduped
	sql, _ = BuildFetchByRefValues("public", "customers", "id", []string{"id"}, 1)
	if sql != `SELECT "id" FROM "public"."customers" WHERE "id" IN ($1)` {
		t.Fatalf("dedup: %s", sql)
	}
	if _, err := BuildFetchByRefValues("public", "customers", "bad col", nil, 1); err == nil {
		t.Fatal("want identifier error")
	}
	if _, err := BuildFetchByRefValues("public", "customers", "id", nil, 0); err == nil {
		t.Fatal("zero values must be rejected (empty IN () is invalid SQL)")
	}
}

func TestBuildCountByRefEq(t *testing.T) {
	sql, err := BuildCountByRefEq("public", "orders", "customer_id")
	if err != nil {
		t.Fatal(err)
	}
	if sql != `SELECT COUNT(*) FROM "public"."orders" WHERE "customer_id"=$1` {
		t.Fatalf("got %s", sql)
	}
}

func TestSearchLikeEscaping(t *testing.T) {
	p := ListParams{Schema: "public", Table: "t", Columns: []string{"id"},
		Searchable: []string{"id"}, Search: `50%_off\`, SortCol: "id", SortDir: "ASC"}
	sql, args, err := BuildList(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `ILIKE $1 ESCAPE '\'`) {
		t.Fatalf("missing ESCAPE clause: %s", sql)
	}
	if !reflect.DeepEqual(args[:1], []any{`%50\%\_off\\%`}) {
		t.Fatalf("args=%q", args[0])
	}
}
