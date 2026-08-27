# Aggregation Cards & Runtime JSON Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admin-defined dashboard cards showing one aggregate value (COUNT/SUM/AVG/MIN/MAX + grid-compatible filters) over tables/query views, plus JSON-aware display/editing for mixed-content text columns.

**Architecture:** Aggregate querying lives in `kucrud-core` (new `Adapter.AggregateRows` + sqlkit builders + `ReadService.Stats` + httpapi `stats` anchor, `OpRead`-gated). The platform persists cards in SQLite (migration #13) and serves CRUD under `/api/cards`; the web app renders cards on a new `/dashboard` page and as a strip on the Data grid. The JSON editor is frontend-only per-value detection (`looksLikeJSON`) in `Data.tsx`.

**Tech Stack:** Go (modules `ku-crud` root + `core/`), SQLite meta store, React 18 + TS strict + @tanstack/react-query + shadcn/ui + Tailwind, i18n en/id.

**Spec:** `docs/superpowers/specs/2026-08-26-aggregation-cards-json-editor-design.md`

## Global Constraints

- Go code: no comments unless explaining non-obvious decisions (repo convention: explanatory comments on exports are used throughout this codebase — follow neighboring file style).
- All UI strings through `useT()` with keys added to BOTH `web/src/lib/i18n/en.ts` and `web/src/lib/types.ts` id.ts sibling (`web/src/lib/i18n/id.ts`).
- Identifiers reach SQL only via `ds.QuoteIdent`/dialect `quoteIdent`; aggregate func names via allowlist only.
- Card filters are the exact grid filter JSON (`ActiveFilter[]`) validated by `engine.ParseFilters` / platform `parseFilters`.
- Masked ids only over the wire (`s.ids.Encode("card", id)`).
- Tests: `go test ./...` in `/opt/project/ku-crud` AND `/opt/project/ku-crud/core`; frontend `npm run build` in `web/` (no UI test runner). Live DB tests self-skip without `KUCRUD_TEST_PG` / `KUCRUD_TEST_MYSQL`.
- Commit style: `feat(core): ...`, `feat(platform): ...`, `feat(web): ...`, `test(...)`, matching `git log --oneline`.

---

### Task 1: core/ds — aggregate params/result types + sqlkit builders

**Files:**
- Modify: `core/ds/adapter.go` (add `AggregateParams`, `AggregateResult`)
- Modify: `core/ds/sqlkit.go` (add `validAgg`, `aggExpr`, `buildAggregate`)
- Modify: `core/ds/query.go` (add `buildQueryAggregate`)
- Test: `core/ds/sqlkit_test.go` (append)

**Interfaces:**
- Produces: `ds.AggregateParams{Schema, Table, Query, Func, Column string; Filters []ColumnFilter}`, `ds.AggregateResult{Value any; HasRows bool}`, `buildAggregate(p) (string, []any, error)`, `buildQueryAggregate(p) (string, []any, error)` — consumed by Task 2/3.

- [ ] **Step 1: Write failing tests**

Append to `core/ds/sqlkit_test.go` (reuse existing `pgDialect`/`mysqlDialect` vars and the file's table-driven style):

```go
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
			`SELECT MIN("orders"."created"),COUNT(*) FROM "public"."orders" LEFT JOIN "public"."customers" "f_cust" ON "f_cust"."id" = "orders"."cust" WHERE ("f_cust"."name" ILIKE $1 ESCAPE '\')`, []any{"%acme%"}},
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./ds/ -run TestBuildAggregate -v`
Expected: FAIL — `undefined: AggregateParams` (compile error).

- [ ] **Step 3: Implement**

In `core/ds/adapter.go`, after `ListParams`:

```go
// AggregateParams carries one single-value aggregate request (dashboard
// cards). Query set = query-view mode (Table/Schema ignored); otherwise
// the physical table is aggregated. Filters ride the same validated
// pipeline as list requests.
type AggregateParams struct {
	Schema, Table string
	Query         string
	Func          string // count|sum|avg|min|max
	Column        string // required for sum/avg/min/max; empty for count
	Filters       []ColumnFilter
}

// AggregateResult is one aggregate value. Value is nil when the SQL
// aggregate returned NULL (sum/avg/min/max over zero rows); HasRows
// reports whether the filtered set was non-empty (COUNT(*) sidecar).
type AggregateResult struct {
	Value   any
	HasRows bool
}

// scanAggregate scans the (agg, COUNT(*)) pair every aggregate query
// selects; the sidecar count drives HasRows. []byte values (numeric
// strings on some drivers) become plain strings.
func scanAggregate(sc scanner, out *AggregateResult) error {
	var n int64
	if err := sc.Scan(&out.Value, &n); err != nil {
		return err
	}
	out.HasRows = n > 0
	if b, ok := out.Value.([]byte); ok {
		out.Value = string(b)
	}
	return nil
}

// scanner is the shared shape of *sql.Row and rows returned from a tx.
type scanner interface{ Scan(dest ...any) error }
```

In `core/ds/sqlkit.go`, after `buildCount`:

```go
// validAgg enforces the aggregate allowlist and column presence. Values
// never reach SQL text; identifiers are checked at render time.
func validAgg(fn, col string) error {
	switch fn {
	case "count":
		if col != "" {
			return fmt.Errorf("count takes no column")
		}
	case "sum", "avg", "min", "max":
		if col == "" {
			return fmt.Errorf("%s requires a column", fn)
		}
	default:
		return fmt.Errorf("unsupported aggregate %q", fn)
	}
	return nil
}

// aggExpr renders the aggregate projection; count is column-free.
func (dt sqlDialect) aggExpr(fn, col, baseRef string) (string, error) {
	if fn == "count" {
		return "COUNT(*)", nil
	}
	qc, err := dt.colRef(baseRef, col)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(fn) + "(" + qc + ")", nil
}

// buildAggregate renders the single-value aggregate for a physical
// table: SELECT AGG(...),COUNT(*) FROM schema.tbl [fk joins] WHERE ...
// (filterParts reused — same injection posture as the grid).
func (dt sqlDialect) buildAggregate(p AggregateParams) (string, []any, error) {
	if err := validAgg(p.Func, p.Column); err != nil {
		return "", nil, err
	}
	tbl, err := dt.qualify(p.Schema, p.Table)
	if err != nil {
		return "", nil, err
	}
	baseRef := ""
	if hasFKJoin(p.Filters) {
		if baseRef, err = dt.quoteIdent(p.Table); err != nil {
			return "", nil, err
		}
	}
	agg, err := dt.aggExpr(p.Func, p.Column, baseRef)
	if err != nil {
		return "", nil, err
	}
	joins, fCond, fArgs, _, err := dt.filterParts(p.Filters, 1, baseRef)
	if err != nil {
		return "", nil, err
	}
	sqlText := "SELECT " + agg + ",COUNT(*) FROM " + tbl + joins
	if fCond != "" {
		sqlText += " WHERE " + fCond
	}
	return sqlText, fArgs, nil
}
```

In `core/ds/query.go`, after `buildQueryCount`:

```go
// buildQueryAggregate renders the single-value aggregate over a stored
// query view: SELECT AGG(...) FROM (<query>) ku_q WHERE ...
func (dt sqlDialect) buildQueryAggregate(p AggregateParams) (string, []any, error) {
	for _, f := range p.Filters {
		if f.Join != nil {
			return "", nil, fmt.Errorf("fk join filters are not supported on query views")
		}
	}
	if err := validAgg(p.Func, p.Column); err != nil {
		return "", nil, err
	}
	alias := queryAlias
	agg, err := dt.aggExpr(p.Func, p.Column, alias)
	if err != nil {
		return "", nil, err
	}
	_, fCond, fArgs, _, err := dt.filterParts(p.Filters, 1, alias)
	if err != nil {
		return "", nil, err
	}
	sqlText := "SELECT " + agg + ",COUNT(*) FROM (" + p.Query + ") " + alias
	if fCond != "" {
		sqlText += " WHERE " + fCond
	}
	return sqlText, fArgs, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test ./ds/ -run 'TestBuildAggregate|TestBuildQueryAggregate' -v`
Expected: PASS (all 3 tests).

- [ ] **Step 5: Commit**

```bash
git add core/ds/adapter.go core/ds/sqlkit.go core/ds/query.go core/ds/sqlkit_test.go
git commit -m "feat(core): sqlkit aggregate builders for stats cards"
```

---

### Task 2: core/ds — Adapter.AggregateRows (postgres + mysql) + live tests

**Files:**
- Modify: `core/ds/adapter.go` (interface method)
- Modify: `core/ds/postgres.go` (implementation)
- Modify: `core/ds/mysql.go` (implementation)
- Test: `core/ds/aggregate_live_test.go` (create)

**Interfaces:**
- Consumes: Task 1 builders.
- Produces: `Adapter.AggregateRows(AggregateParams) (*AggregateResult, error)` — consumed by engine (Task 3). Query-view mode runs under the existing read-only tx/session guards.

- [ ] **Step 1: Write failing live tests**

Create `core/ds/aggregate_live_test.go`, mirroring the env-guard harness in `core/ds/query_live_test.go` (reuse its helper names if present, else define locally):

```go
package ds

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// seedAggTable creates t_agg(n int, s text) with 3 rows (n = 10, 20, NULL).
func seedAggTable(t *testing.T, db *sql.DB, driver string) {
	t.Helper()
	drop := "DROP TABLE IF EXISTS t_agg"
	_, err := db.Exec(drop)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t_agg (id INTEGER PRIMARY KEY, n " + map[string]string{"postgres": "INT", "mysql": "INT"}[driver] + ", s TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id int
		n  *int
		s  string
	}{
		{1, ptr(10), "a"}, {2, ptr(20), "b"}, {3, nil, "a"},
	} {
		if _, err := db.Exec("INSERT INTO t_agg (id, n, s) VALUES (?,?,?)", row.id, row.n, row.s); err != nil {
			t.Fatal(err)
		}
	}
}

func ptr(i int) *int { return &i }

func TestAggregateRowsLive(t *testing.T) {
	for _, tc := range []struct {
		driver, env string
		open        func(string) (Adapter, error)
	}{
		{"postgres", "KUCRUD_TEST_PG", func(dsn string) (Adapter, error) { return Open(Conn{Driver: "postgres", Raw: dsn}) }},
		{"mysql", "KUCRUD_TEST_MYSQL", func(dsn string) (Adapter, error) { return Open(Conn{Driver: "mysql", Raw: dsn}) }},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip(tc.env + " not set")
			}
			a, err := tc.open(dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			db := a.(*sql.DB) // if adapters don't expose *sql.DB, adapt: see note below
			_ = db
		})
	}
}
```

**Note for implementer:** the adapters wrap `*sql.DB` privately. Do NOT reach into it — instead seed via `a.(interface{})` is wrong; add a tiny exported test hook is also wrong. Simplest correct approach: open the `*sql.DB` directly in the test with `sql.Open("pgx", dsn)` / `sql.Open("mysql", dsn)` for seeding, then use `ds.Open` for the assertions. Structure the test as: seed via raw `*sql.DB`, close it, then run `AggregateRows` assertions through the `Adapter`:

```go
func TestAggregateRowsLive(t *testing.T) {
	for _, tc := range []struct {
		driver, env, sqlDriver string
	}{
		{"postgres", "KUCRUD_TEST_PG", "pgx"},
		{"mysql", "KUCRUD_TEST_MYSQL", "mysql"},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip(tc.env + " not set")
			}
			raw, err := sql.Open(tc.sqlDriver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			seedAggTable(t, raw, tc.driver)
			raw.Close()

			a, err := Open(Conn{Driver: tc.driver, Raw: dsn})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()

			schema := map[string]string{"postgres": "public", "mysql": ""}[tc.driver]

			r, err := a.AggregateRows(AggregateParams{Schema: schema, Table: "t_agg", Func: "count"})
			if err != nil {
				t.Fatal(err)
			}
			if r.Value == nil || f64Of(r.Value) != 3 || !r.HasRows {
				t.Fatalf("count = %v hasRows=%v", r.Value, r.HasRows)
			}

			r, err = a.AggregateRows(AggregateParams{Schema: schema, Table: "t_agg", Func: "sum", Column: "n"})
			if err != nil {
				t.Fatal(err)
			}
			if r.Value == nil || f64Of(r.Value) != 30 {
				t.Fatalf("sum = %v", r.Value)
			}

			// zero-row set: sum NULL + hasRows false
			r, err = a.AggregateRows(AggregateParams{Schema: schema, Table: "t_agg", Func: "sum", Column: "n",
				Filters: []ColumnFilter{{Column: "s", Op: "eq", Values: []any{"zzz"}}}})
			if err != nil {
				t.Fatal(err)
			}
			if r.Value != nil || r.HasRows {
				t.Fatalf("empty sum = %v hasRows=%v", r.Value, r.HasRows)
			}

			// query-view mode wraps stored SQL
			r, err = a.AggregateRows(AggregateParams{Query: "SELECT n, s FROM t_agg", Func: "avg", Column: "n"})
			if err != nil {
				t.Fatal(err)
			}
			if r.Value == nil || f64Of(r.Value) != 15 {
				t.Fatalf("query avg = %v", r.Value)
			}
		})
	}
}

func f64Of(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case string:
		var f float64
		for _, c := range x {
			if c == '.' {
				fmt.Sscanf(x, "%f", &f)
				return f
			}
		}
		fmt.Sscanf(x, "%f", &f)
		return f
	}
	return -1
}
```

(Import `fmt` as needed; MySQL returns numeric aggregates as []byte→string after `scanAggregate`, which `f64Of` parses.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./ds/ -run TestAggregateRowsLive -v`
Expected: compile FAIL — `a.AggregateRows undefined (type ds.Adapter has no field or method AggregateRows)`.

- [ ] **Step 3: Implement**

In `core/ds/adapter.go`, add to the `Adapter` interface after `CountRows`:

```go
	ListRows(p ListParams) ([]map[string]any, error)
	CountRows(p ListParams) (int, error)
	// AggregateRows computes one single-value aggregate (dashboard cards):
	// Query set = query-view mode (read-only tx + timeout), else table mode.
	AggregateRows(p AggregateParams) (*AggregateResult, error)
```

In `core/ds/postgres.go`, after `CountRows`:

```go
func (a *pgAdapter) AggregateRows(p AggregateParams) (*AggregateResult, error) {
	if p.Query != "" {
		sqlText, args, err := pgDialect.buildQueryAggregate(p)
		if err != nil {
			return nil, err
		}
		out := &AggregateResult{}
		err = a.queryExec(func(tx *sql.Tx) error {
			return scanAggregate(tx.QueryRow(sqlText, args...), out)
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	sqlText, args, err := pgDialect.buildAggregate(p)
	if err != nil {
		return nil, err
	}
	out := &AggregateResult{}
	if err := scanAggregate(a.db.QueryRow(sqlText, args...), out); err != nil {
		return nil, err
	}
	return out, nil
}
```

In `core/ds/mysql.go`, after `CountRows` (mirror; query mode via `withQueryConn` + `QueryRowContext`):

```go
func (a *mysqlAdapter) AggregateRows(p AggregateParams) (*AggregateResult, error) {
	if p.Query != "" {
		sqlText, args, err := mysqlDialect.buildQueryAggregate(p)
		if err != nil {
			return nil, err
		}
		out := &AggregateResult{}
		err = a.withQueryConn(func(ctx context.Context, q ctxQuerier) error {
			return scanAggregate(q.QueryRowContext(ctx, sqlText, args...), out)
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	sqlText, args, err := mysqlDialect.buildAggregate(p)
	if err != nil {
		return nil, err
	}
	out := &AggregateResult{}
	if err := scanAggregate(a.db.QueryRow(sqlText, args...), out); err != nil {
		return nil, err
	}
	return out, nil
}
```

Check `ctxQuerier` in mysql.go — if it lacks `QueryRowContext`, add it to that interface (it's `QueryContext/ExecContext` today); `*sql.Conn` satisfies the extended shape.

- [ ] **Step 4: Build + run tests**

Run: `cd core && go build ./... && go vet ./... && go test ./ds/ -run TestAggregateRowsLive -v`
Expected: PASS (or SKIP when env vars unset — then verify compile only).

- [ ] **Step 5: Commit**

```bash
git add core/ds/
git commit -m "feat(core): Adapter.AggregateRows on postgres and mysql"
```

---

### Task 3: core/engine — ReadService.Stats

**Files:**
- Modify: `core/engine/rows_read.go` (add `Stats`, `aggFuncs`)
- Modify: `core/engine/rows_read_test.go` (extend `fakeAdapter`)
- Test: `core/engine/stats_test.go` (create)

**Interfaces:**
- Consumes: `ds.AggregateParams`, `ds.AggregateResult`, `ParseFilters`, `writeErr`/`writeJSON`/`writeQueryErr`/`writeLiveErr` (all in `rows_read.go`).
- Produces: `func (s *ReadService) Stats(w http.ResponseWriter, r *http.Request, t *defs.Table)` — consumed by httpapi (Task 4). Response `200`: `{"func":..., "column":..., "value":..., "hasRows":...}`; errors `400 STATS_INVALID` / `400 FILTER_INVALID` / `502 CONN` / `502 QUERY_TIMEOUT`.

- [ ] **Step 1: Write failing tests**

Create `core/engine/stats_test.go`:

```go
package engine

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/luthfi9251/kucrud-core/ds"
)

func statsDef() *defs.Table {
	return &defs.Table{Name: "orders", Schema: "public", PhysTab: "orders",
		Keys: []string{"id"}, PageSize: 20, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Position: 0},
			{Name: "amount", Label: "Amount", FieldType: "number", Visible: true, Position: 1},
			{Name: "created", Label: "Created", FieldType: "datetime", Visible: true, Position: 2},
			{Name: "note", Label: "Note", FieldType: "text", Visible: true, Position: 3},
			{Name: "total", Label: "Total", FieldType: "number", IsComputed: true, Visible: true, Position: 4},
			{Name: "tags", Label: "Tags", FieldType: "m2m", Visible: true, Position: 5},
		}}
}

func doStats(svc *ReadService, target string, t *defs.Table) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	svc.Stats(w, req, t)
	return w
}

func TestStatsValidation(t *testing.T) {
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) { return &fakeAdapter{}, nil }}}
	td := statsDef()
	for _, tc := range []struct{ q, code string }{
		{"", "STATS_INVALID"},
		{"?func=median&column=amount", "STATS_INVALID"},
		{"?func=count&column=amount", "STATS_INVALID"},
		{"?func=sum", "STATS_INVALID"},
		{"?func=sum&column=note", "STATS_INVALID"},
		{"?func=avg&column=created", "STATS_INVALID"},
		{"?func=sum&column=nope", "STATS_INVALID"},
		{"?func=sum&column=total", "STATS_INVALID"},   // computed
		{"?func=min&column=tags", "STATS_INVALID"},   // m2m
		{"?func=count&filters=bad", "FILTER_INVALID"},
	} {
		w := doStats(svc, "/stats"+tc.q, td)
		var e map[string]any
		json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != 400 || e["code"] != tc.code {
			t.Errorf("%q = %d %v, want 400 %s", tc.q, w.Code, e["code"], tc.code)
		}
	}
}

func TestStatsTableMode(t *testing.T) {
	var got ds.AggregateParams
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(p ds.AggregateParams) (*ds.AggregateResult, error) {
				got = p
				return &ds.AggregateResult{Value: "123.45", HasRows: true}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=sum&column=amount&filters=%5B%7B%22column%22%3A%22amount%22%2C%22op%22%3A%22gt%22%2C%22values%22%3A%5B10%5D%7D%5D", statsDef())
	if w.Code != 200 {
		t.Fatalf("stats = %d %s", w.Code, w.Body)
	}
	var out struct {
		Func    string `json:"func"`
		Column  string `json:"column"`
		Value   any    `json:"value"`
		HasRows bool   `json:"hasRows"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Func != "sum" || out.Column != "amount" || out.Value != 123.45 || !out.HasRows {
		t.Fatalf("out = %s", w.Body)
	}
	if got.Schema != "public" || got.Table != "orders" || got.Query != "" || got.Func != "sum" || got.Column != "amount" {
		t.Fatalf("params = %+v", got)
	}
	if len(got.Filters) != 1 || got.Filters[0].Column != "amount" || got.Filters[0].Op != "gt" {
		t.Fatalf("filters = %+v", got.Filters)
	}
}

func TestStatsCountPassthrough(t *testing.T) {
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(ds.AggregateParams) (*ds.AggregateResult, error) {
				return &ds.AggregateResult{Value: int64(7), HasRows: true}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=count", statsDef())
	if w.Code != 200 {
		t.Fatalf("count = %d %s", w.Code, w.Body)
	}
	var out struct {
		Value any `json:"value"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Value != float64(7) { // JSON number
		t.Fatalf("value = %#v", out.Value)
	}
}

func TestStatsQueryMode(t *testing.T) {
	var got ds.AggregateParams
	qd := statsDef()
	qd.SourceType = "query"
	qd.QuerySQL = "SELECT amount FROM orders"
	qd.PhysTab = ""
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": qd},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(p ds.AggregateParams) (*ds.AggregateResult, error) {
				got = p
				return &ds.AggregateResult{Value: nil, HasRows: false}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=avg&column=amount", qd)
	if w.Code != 200 {
		t.Fatalf("stats = %d %s", w.Code, w.Body)
	}
	if got.Query == "" || got.Table != "" {
		t.Fatalf("params = %+v", got)
	}
	var out struct {
		Value   any `json:"value"`
		HasRows bool `json:"hasRows"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Value != nil || out.HasRows {
		t.Fatalf("null agg = %s", w.Body)
	}
}
```

(`defs` import: `"github.com/luthfi9251/kucrud-core/defs"` — add to the import block.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./engine/ -run TestStats -v`
Expected: compile FAIL — `svc.Stats undefined` and `fakeAdapter` has no `aggregate` field.

- [ ] **Step 3: Implement**

Extend `fakeAdapter` in `core/engine/rows_read_test.go` (field + method, next to `countRows`):

```go
	aggregate func(ds.AggregateParams) (*ds.AggregateResult, error)
```

```go
func (f *fakeAdapter) AggregateRows(p ds.AggregateParams) (*ds.AggregateResult, error) {
	return f.aggregate(p)
}
```

In `core/engine/rows_read.go`, after `List`:

```go
// aggFuncs is the stats allowlist; SQL text only ever receives these
// lower-case names through the ds builder's own allowlist too.
var aggFuncs = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

// Stats renders GET stats: one aggregate value over the def with optional
// grid-format filters (dashboard cards).
func (s *ReadService) Stats(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	q := r.URL.Query()
	fn := q.Get("func")
	colName := q.Get("column")
	if !aggFuncs[fn] {
		writeErr(w, 400, "STATS_INVALID", "func must be one of count|sum|avg|min|max", nil)
		return
	}
	var col *defs.Column
	if colName != "" {
		for i := range t.Columns {
			if t.Columns[i].Name == colName {
				col = &t.Columns[i]
				break
			}
		}
		if col == nil || col.FieldType == "m2m" || col.IsComputed {
			writeErr(w, 400, "STATS_INVALID", "unknown or virtual column "+colName, nil)
			return
		}
	}
	switch fn {
	case "count":
		if colName != "" {
			writeErr(w, 400, "STATS_INVALID", "count takes no column", nil)
			return
		}
	case "sum", "avg":
		if col == nil || col.FieldType != "number" {
			writeErr(w, 400, "STATS_INVALID", fn+" requires a number column", nil)
			return
		}
	case "min", "max":
		if col == nil || (col.FieldType != "number" && col.FieldType != "datetime") {
			writeErr(w, 400, "STATS_INVALID", fn+" requires a number or datetime column", nil)
			return
		}
	}
	filters, ferr := ParseFilters(t, q.Get("filters"), s.FKJoin)
	if ferr != nil {
		writeErr(w, 400, "FILTER_INVALID", ferr.Error(), nil)
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()
	ap := ds.AggregateParams{Func: fn, Column: colName, Filters: filters}
	if t.SourceType == "query" {
		ap.Query = t.QuerySQL
	} else {
		ap.Schema, ap.Table = t.Schema, t.PhysTab
	}
	res, err := a.AggregateRows(ap)
	if err != nil {
		if t.SourceType == "query" {
			writeQueryErr(w, err)
			return
		}
		writeErr(w, 502, "CONN", "query failed", err.Error())
		return
	}
	v := res.Value
	if col != nil && col.FieldType == "number" {
		if sv, ok := v.(string); ok {
			if f, perr := strconv.ParseFloat(sv, 64); perr == nil {
				v = f
			}
		}
	}
	writeJSON(w, 200, map[string]any{"func": fn, "column": colName, "value": v, "hasRows": res.HasRows})
}
```

(`strconv` is already imported in rows_read.go.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test ./engine/ -v`
Expected: PASS — new stats tests plus all existing engine tests (the fakeAdapter embeds `ds.Adapter` so existing tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add core/engine/
git commit -m "feat(core): ReadService.Stats aggregate endpoint service"
```

---

### Task 4: core/httpapi — stats anchor

**Files:**
- Modify: `core/httpapi/httpapi.go` (anchor + route + doc comments)
- Test: `core/httpapi/httpapi_test.go` (append)

**Interfaces:**
- Consumes: `read.Stats` (Task 3).
- Produces: `GET {base}/stats?func=&column=&filters=` gated by `OpRead`. Anchor word `stats` joins the reserved-words set (mount prefixes must not contain it).

- [ ] **Step 1: Write failing tests**

Append to `core/httpapi/httpapi_test.go`:

```go
func TestStatsAnchor(t *testing.T) {
	src := &fakeSource{list: []*defs.Table{testTable()}}
	var gotOp httpapi.Op
	gate := func(r *http.Request, op httpapi.Op, table string) error {
		gotOp = op
		return nil
	}
	h := httpapi.New("things", src.list[0], src, httpapi.Options{Gate: gate})

	// wrong method proves the route resolved (405, not 404)
	if w := serve(t, h, http.MethodPost, "/stats"); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET" {
		t.Fatalf("stats POST = %d %s", w.Code, w.Body)
	}
	// trailing segments are not part of the route
	if w := serve(t, h, http.MethodGet, "/stats/extra"); w.Code != http.StatusNotFound {
		t.Fatalf("stats/extra = %d", w.Code)
	}
	// gate denial fires with OpRead before any datasource use
	h2 := httpapi.New("things", src.list[0], src, httpapi.Options{
		Gate: func(*http.Request, httpapi.Op, string) error { return errors.New("no") }})
	if w := serve(t, h2, http.MethodGet, "/stats?func=count"); w.Code != http.StatusForbidden {
		t.Fatalf("gated stats = %d %s", w.Code, w.Body)
	}
	if gotOp != httpapi.OpRead {
		t.Fatalf("op = %q", gotOp)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd core && go test ./httpapi/ -run TestStatsAnchor -v`
Expected: FAIL — `/stats` returns 404.

- [ ] **Step 3: Implement**

In `core/httpapi/httpapi.go`:

1. `anchors` map gains `"stats": true`.
2. Update the two doc comments that enumerate anchors (`Resource` doc: "the anchor segment (rows/fkoptions/m2moptions/import)" and "must not itself contain a segment named …") to include `stats`.
3. In `ServeHTTP`'s switch, after the `"import"` case:

```go
	case "stats":
		if len(segs) != 1 {
			writeErr(w, 404, "NOT_FOUND", "route not found", nil)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, "GET")
			return
		}
		read, _, _ := h.services(r)
		h.dispatch(w, r, OpRead, false, func() { read.Stats(w, r, h.t) })
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test ./httpapi/ -v && go test ./... `
Expected: PASS (whole core module).

- [ ] **Step 5: Commit**

```bash
git add core/httpapi/
git commit -m "feat(core): stats anchor on httpapi resources (OpRead-gated)"
```

---

### Task 5: internal/meta — migration #13 + stat_cards store

**Files:**
- Modify: `internal/meta/meta.go` (append migration)
- Create: `internal/meta/statcards.go`
- Test: `internal/meta/migration13_test.go` (create)

**Interfaces:**
- Produces (consumed by Task 6): 
  - `meta.StatCard{ID int64; TableDefID int64; Label, Func, Column, Filters string; Position int}`
  - `(s *Store) ListStatCards() ([]StatCard, error)` — ordered `position,id`
  - `(s *Store) GetStatCard(id int64) (*StatCard, error)` — `ErrNotFound`
  - `(s *Store) CreateStatCard(tableDefID int64, label, fn, column, filters string) (int64, error)`
  - `(s *Store) UpdateStatCard(id int64, label, fn, column, filters string) error`
  - `(s *Store) DeleteStatCard(id int64) error`
  - `(s *Store) MoveStatCard(id int64, up bool) error` — swaps `position` with the neighbor

- [ ] **Step 1: Write failing tests**

Create `internal/meta/migration13_test.go`:

```go
package meta

import (
	"testing"
)

func TestMigration13StatCards(t *testing.T) {
	s := openTest(t)
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 13 {
		t.Fatalf("user_version = %d, want 13", v)
	}
	var name string
	if err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='stat_cards'").Scan(&name); err != nil {
		t.Fatalf("stat_cards missing: %v", err)
	}
}

func TestStatCardsStore(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders", Label: "Orders",
		KeyColumns: []string{"id"}, PageSize: 20}
	cols := []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
	}
	if err := s.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	tdID := def.ID

	id1, err := s.CreateStatCard(tdID, "Revenue", "sum", "amount", "[]")
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := s.CreateStatCard(tdID, "Orders", "count", "", "[]")
	if id1 == id2 {
		t.Fatal("ids not distinct")
	}

	list, err := s.ListStatCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != id1 || list[1].ID != id2 {
		t.Fatalf("list order = %+v", list)
	}
	if list[0].Position >= list[1].Position {
		t.Fatalf("positions not increasing: %+v", list)
	}

	if err := s.MoveStatCard(id2, true); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListStatCards()
	if list[0].ID != id2 || list[1].ID != id1 {
		t.Fatalf("after move = %+v", list)
	}
	if err := s.MoveStatCard(id2, true); err == nil {
		t.Fatal("moving top card up should fail")
	}

	if err := s.UpdateStatCard(id1, "Revenue USD", "sum", "amount", `[]`); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetStatCard(id1)
	if err != nil || got.Label != "Revenue USD" {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := s.GetStatCard(999); err != ErrNotFound {
		t.Fatalf("missing = %v", err)
	}

	if err := s.DeleteStatCard(id1); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStatCard(id1); err != ErrNotFound {
		t.Fatal("double delete not ErrNotFound")
	}

	// cascade: deleting the def removes its cards
	s.CreateStatCard(tdID, "X", "count", "", "[]")
	if err := s.DeleteTableDef(tdID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListStatCards()
	if len(list) != 0 {
		t.Fatalf("cascade left cards: %+v", list)
	}
}
```

(Seed follows the exact `tabledefs_test.go` conventions: `CreateDatasource` returns only error, `SaveTableDef` mutates `def.ID`.)- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/meta/ -run 'TestMigration13|TestStatCards' -v`
Expected: FAIL — `user_version = 12, want 13` / undefined store methods.

- [ ] **Step 3: Implement**

Append to `migrations` in `internal/meta/meta.go` (verify the slice currently has 12 entries — `grep -c` backtick-terminated entries or temporarily print `len(migrations)` in a test; the last is the v1.9 `actions` ALTER):

```go
	// v1.10: admin-defined dashboard stat cards.
	`CREATE TABLE stat_cards(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  table_def_id INTEGER NOT NULL REFERENCES table_defs(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  func TEXT NOT NULL,
  column_name TEXT NOT NULL DEFAULT '',
  filters TEXT NOT NULL DEFAULT '[]',
  position INTEGER NOT NULL DEFAULT 0);`,
```

If `len(migrations)` != 13 after appending, renumber the test's expected `user_version` to `len(migrations)` and rename the test file accordingly.

Create `internal/meta/statcards.go`:

```go
package meta

import (
	"database/sql"
	"errors"
)

type StatCard struct {
	ID          int64  `json:"id"`
	TableDefID  int64  `json:"-"`
	Label       string `json:"label"`
	Func        string `json:"func"`
	Column      string `json:"column"`
	Filters     string `json:"filters"`
	Position    int    `json:"position"`
}

func (s *Store) ListStatCards() ([]StatCard, error) {
	rows, err := s.db.Query(`SELECT id,table_def_id,label,func,column_name,filters,position
		FROM stat_cards ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatCard
	for rows.Next() {
		var c StatCard
		if err := rows.Scan(&c.ID, &c.TableDefID, &c.Label, &c.Func, &c.Column, &c.Filters, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetStatCard(id int64) (*StatCard, error) {
	var c StatCard
	err := s.db.QueryRow(`SELECT id,table_def_id,label,func,column_name,filters,position
		FROM stat_cards WHERE id=?`, id).
		Scan(&c.ID, &c.TableDefID, &c.Label, &c.Func, &c.Column, &c.Filters, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) CreateStatCard(tableDefID int64, label, fn, column, filters string) (int64, error) {
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM stat_cards`).Scan(&maxPos); err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO stat_cards(table_def_id,label,func,column_name,filters,position)
		VALUES(?,?,?,?,?,?)`, tableDefID, label, fn, column, filters, maxPos.Int64+1)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (s *Store) UpdateStatCard(id int64, label, fn, column, filters string) error {
	res, err := s.db.Exec(`UPDATE stat_cards SET label=?,func=?,column_name=?,filters=? WHERE id=?`,
		label, fn, column, filters, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteStatCard(id int64) error {
	res, err := s.db.Exec(`DELETE FROM stat_cards WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveStatCard swaps positions with the neighboring card (up = smaller
// position). Moving past the end is an error.
func (s *Store) MoveStatCard(id int64, up bool) error {
	list, err := s.ListStatCards()
	if err != nil {
		return err
	}
	idx := -1
	for i := range list {
		if list[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	swap := idx - 1
	if !up {
		swap = idx + 1
	}
	if swap < 0 || swap >= len(list) {
		return errors.New("no neighbor to swap with")
	}
	a, b := list[idx], list[swap]
	if _, err := s.db.Exec(`UPDATE stat_cards SET position=? WHERE id=?`, b.Position, a.ID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE stat_cards SET position=? WHERE id=?`, a.Position, b.ID); err != nil {
		return err
	}
	return nil
}
```

(Verify `CreateDatasource`/`SaveTableDef` signatures against `internal/meta/datasources.go` / `tabledefs.go` and adjust the test seeding to the real signatures — mirror `internal/meta/tabledefs_test.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/meta/ -v`
Expected: PASS (all, existing included — the store migrates fresh DBs at 13).

- [ ] **Step 5: Commit**

```bash
git add internal/meta/
git commit -m "feat(platform): stat_cards store (migration 13)"
```

---

### Task 6: internal/api — cards handlers + routes

**Files:**
- Create: `internal/api/cards.go`
- Modify: `internal/api/server.go` (routes)
- Test: `internal/api/cards_test.go` (create)

**Interfaces:**
- Consumes: Task 5 store, `s.parseFilters(def, cols, u, filtersJSON)`, `s.hasTablePerm`, `s.ids.Encode/Decode("card", ...)`, `s.tableCtx(r)`.
- Produces (consumed by Task 7–9 frontend):
  - `GET /api/cards` → `[{id, tableDefId, tableName, tableLabel, label, func, column, filters, position}]` (read-grant filtered)
  - `POST /api/cards` `{tableDefId, label, func, column, filters}` → 200 card
  - `PUT /api/cards/{id}` same body → 200 card
  - `DELETE /api/cards/{id}` → `{ok:true}`
  - `POST /api/cards/{id}/move` `{"dir":"up"|"down"}` → `{ok:true}`

- [ ] **Step 1: Write failing tests**

Create `internal/api/cards_test.go`:

```go
package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func seedCardsDef(t *testing.T, s *Server) string {
	t.Helper()
	seedDS(t, s)
	body := `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"orders3",
"label":"Orders3","keyColumns":["id"],"pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"amount","label":"Amount","fieldType":"number","editable":true,
  "visible":true,"position":1},
 {"name":"status","label":"Status","fieldType":"enum","enumOptions":["open","paid"],
  "editable":true,"visible":true,"position":2}]}`
	if w := do(s, "POST", "/api/tables", body, login(s)); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}
	return tdTok(s, 1)
}

func TestCardsAdminCRUD(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedCardsDef(t, s)

	w := do(s, "POST", "/api/cards", `{"tableDefId":"`+tok+`","label":"Revenue","func":"sum","column":"amount","filters":"[]"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var card struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &card)
	if card.ID == "" || card.ID == "1" {
		t.Fatalf("card id not masked: %q", card.ID)
	}

	w = do(s, "GET", "/api/cards", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["tableName"] != "orders3" || list[0]["tableDefId"] != tok {
		t.Fatalf("list = %s", w.Body)
	}

	w = do(s, "PUT", "/api/cards/"+card.ID, `{"tableDefId":"`+tok+`","label":"Paid revenue","func":"sum","column":"amount",
"filters":"[{\"column\":\"status\",\"op\":\"eq\",\"values\":[\"paid\"]}]"}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Paid revenue") {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}

	w = do(s, "POST", "/api/cards/"+card.ID+"/move", `{"dir":"up"}`, c)
	if w.Code != 200 {
		t.Fatalf("move = %d %s", w.Code, w.Body)
	}

	w = do(s, "DELETE", "/api/cards/"+card.ID, "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
}

func TestCardsValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedCardsDef(t, s)
	for _, body := range []string{
		`{"tableDefId":"` + tok + `","label":"X","func":"median","column":"amount","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"sum","column":"status","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"count","column":"amount","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"sum","column":"","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"count","filters":"[{\"column\":\"nope\",\"op\":\"eq\",\"values\":[\"x\"]}]"}`,
		`{"tableDefId":"zzzzzzzzzzz","label":"X","func":"count","filters":"[]"}`,
	} {
		w := do(s, "POST", "/api/cards", body, c)
		if w.Code != 400 {
			t.Fatalf("accepted bad card: %d %s", w.Code, body)
		}
	}
}

func TestCardsRBAC(t *testing.T) {
	s := newTestServer(t)
	tok := seedCardsDef(t, s)
	admin := login(s)
	if w := do(s, "POST", "/api/cards", `{"tableDefId":"`+tok+`","label":"Revenue","func":"sum","column":"amount","filters":"[]"}`, admin); w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}

	reader := loginAs(t, s, "reader", &meta.Role{Name: "Reader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	stranger := loginAs(t, s, "stranger", &meta.Role{Name: "Stranger"}, nil)

	// reader (read grant) sees the card
	w := do(s, "GET", "/api/cards", "", reader)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Revenue") {
		t.Fatalf("reader list = %d %s", w.Code, w.Body)
	}
	// stranger (no grant) does not
	w = do(s, "GET", "/api/cards", "", stranger)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Revenue") {
		t.Fatalf("stranger list = %d %s", w.Code, w.Body)
	}
	// non-admin cannot manage
	for _, p := range []struct{ method, path string }{
		{"POST", "/api/cards"}, {"PUT", "/api/cards/x"}, {"DELETE", "/api/cards/x"}, {"POST", "/api/cards/x/move"},
	} {
		if w := do(s, p.method, p.path, `{}`, reader); w.Code != 403 {
			t.Fatalf("%s %s reader = %d %s", p.method, p.path, w.Code, w.Body)
		}
	}
}
```

(Verify `meta.TableGrant` field names against `internal/meta/roles.go` and adjust — mirror existing grant-seeding tests like `internal/api/rbac_test.go` / `grants_test.go`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestCards -v`
Expected: FAIL — 404s (routes missing).

- [ ] **Step 3: Implement**

Create `internal/api/cards.go`:

```go
package api

import (
	"net/http"

	"ku-crud/internal/meta"
)

type statCardDTO struct {
	ID         string `json:"id"`
	TableDefID string `json:"tableDefId"`
	TableName  string `json:"tableName"` // data address; empty for query views (use tableDefId token)
	TableLabel string `json:"tableLabel"`
	Label      string `json:"label"`
	Func       string `json:"func"`
	Column     string `json:"column"`
	Filters    string `json:"filters"`
	Position   int    `json:"position"`
}

// validateCard re-checks func/column/type rules against the CURRENT def
// (defs are runtime-mutable). Mirrors engine's Stats rules; serve-time
// validation remains the enforcement point.
func (s *Server) validateCard(def *meta.TableDef, cols []meta.ColumnDef, fn, column string) string {
	switch fn {
	case "count":
		if column != "" {
			return "count takes no column"
		}
	case "sum", "avg", "min", "max":
		var col *meta.ColumnDef
		for i := range cols {
			if cols[i].Name == column {
				col = &cols[i]
				break
			}
		}
		if col == nil || col.FieldType == "m2m" || col.IsComputed {
			return "unknown or virtual column " + column
		}
		if fn == "sum" || fn == "avg" {
			if col.FieldType != "number" {
				return fn + " requires a number column"
			}
		} else if col.FieldType != "number" && col.FieldType != "datetime" {
			return fn + " requires a number or datetime column"
		}
	default:
		return "func must be one of count|sum|avg|min|max"
	}
	return ""
}

func (s *Server) cardDTO(c meta.StatCard, def *meta.TableDef) statCardDTO {
	return statCardDTO{
		ID: s.ids.Encode("card", c.ID), TableDefID: s.ids.Encode("td", c.TableDefID),
		TableName: def.TableName, TableLabel: def.Label,
		Label: c.Label, Func: c.Func, Column: c.Column, Filters: c.Filters, Position: c.Position,
	}
}

func (s *Server) handleCardList(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cards, err := s.store.ListStatCards()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := []statCardDTO{}
	for _, c := range cards {
		def, _, err := s.store.GetTableDef(c.TableDefID)
		if err != nil {
			continue // def vanished between list and read (cascade races)
		}
		if !s.hasTablePerm(u, def.ID, "read") {
			continue
		}
		out = append(out, s.cardDTO(c, def))
	}
	writeJSON(w, 200, out)
}

func (s *Server) cardCtx(r *http.Request) (*meta.StatCard, error) {
	id, err := s.ids.Decode("card", r.PathValue("id"))
	if err != nil {
		return nil, meta.ErrNotFound
	}
	return s.store.GetStatCard(id)
}

type cardInput struct {
	TableDefID string `json:"tableDefId"`
	Label      string `json:"label"`
	Func       string `json:"func"`
	Column     string `json:"column"`
	Filters    string `json:"filters"`
}

func (s *Server) readCardInput(w http.ResponseWriter, r *http.Request, in *cardInput) (*meta.TableDef, []meta.ColumnDef, bool) {
	if err := readJSON(r, in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return nil, nil, false
	}
	if len(in.Label) < 1 || len(in.Label) > 60 {
		writeErr(w, 400, "VALIDATION", "card label must be 1..60 chars", nil)
		return nil, nil, false
	}
	tdID, err := s.ids.Decode("td", in.TableDefID)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "unknown table", nil)
		return nil, nil, false
	}
	def, cols, err := s.store.GetTableDef(tdID)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "unknown table", nil)
		return nil, nil, false
	}
	if msg := s.validateCard(def, cols, in.Func, in.Column); msg != "" {
		writeErr(w, 400, "STATS_INVALID", msg, nil)
		return nil, nil, false
	}
	if in.Filters == "" {
		in.Filters = "[]"
	}
	u := userFrom(r)
	if _, fmsg := s.parseFilters(def, cols, u, in.Filters); fmsg != "" {
		writeErr(w, 400, "FILTER_INVALID", fmsg, nil)
		return nil, nil, false
	}
	return def, cols, true
}

func (s *Server) handleCardCreate(w http.ResponseWriter, r *http.Request) {
	var in cardInput
	def, _, ok := s.readCardInput(w, r, &in)
	if !ok {
		return
	}
	id, err := s.store.CreateStatCard(def.ID, in.Label, in.Func, in.Column, in.Filters)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	card, _ := s.store.GetStatCard(id)
	writeJSON(w, 200, s.cardDTO(*card, def))
}

func (s *Server) handleCardUpdate(w http.ResponseWriter, r *http.Request) {
	card, err := s.cardCtx(r)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "card not found", nil)
		return
	}
	var in cardInput
	def, _, ok := s.readCardInput(w, r, &in)
	if !ok {
		return
	}
	if err := s.store.UpdateStatCard(card.ID, in.Label, in.Func, in.Column, in.Filters); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	updated, _ := s.store.GetStatCard(card.ID)
	writeJSON(w, 200, s.cardDTO(*updated, def))
}

func (s *Server) handleCardDelete(w http.ResponseWriter, r *http.Request) {
	card, err := s.cardCtx(r)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "card not found", nil)
		return
	}
	if err := s.store.DeleteStatCard(card.ID); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleCardMove(w http.ResponseWriter, r *http.Request) {
	card, err := s.cardCtx(r)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "card not found", nil)
		return
	}
	var in struct {
		Dir string `json:"dir"`
	}
	if err := readJSON(r, &in); err != nil || (in.Dir != "up" && in.Dir != "down") {
		writeErr(w, 400, "VALIDATION", "dir must be up or down", nil)
		return
	}
	if err := s.store.MoveStatCard(card.ID, in.Dir == "up"); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
```

In `internal/api/server.go`, next to the saved-filters block:

```go
	mux.HandleFunc("GET /api/cards", s.RequireAuth(s.handleCardList))
	mux.HandleFunc("POST /api/cards", s.RequireTablesManage(s.handleCardCreate))
	mux.HandleFunc("PUT /api/cards/{id}", s.RequireTablesManage(s.handleCardUpdate))
	mux.HandleFunc("DELETE /api/cards/{id}", s.RequireTablesManage(s.handleCardDelete))
	mux.HandleFunc("POST /api/cards/{id}/move", s.RequireTablesManage(s.handleCardMove))
```

(If `RequireTablesManage` has a different name, use the guard used by `POST /api/tables`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestCards -v && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat(platform): stat card CRUD api with read-grant filtered listing"
```

---

### Task 7: web — types, i18n, StatCardView

**Files:**
- Modify: `web/src/lib/types.ts`
- Create: `web/src/components/StatCardView.tsx`
- Modify: `web/src/lib/i18n/en.ts`, `web/src/lib/i18n/id.ts`

**Interfaces:**
- Consumes: `/api/cards` DTO (Task 6), `/api/data/{name}` def JSON, `/api/data/{name}/stats`.
- Produces: `StatCard`, `AggFunc`, `StatsResult` types; `<StatCardView card compact? onEdit? onDelete? onMove?/>` — consumed by Tasks 8–9.

- [ ] **Step 1: Types**

Append to `web/src/lib/types.ts`:

```ts
export type AggFunc = "count" | "sum" | "avg" | "min" | "max";

// StatCard is an admin-defined dashboard aggregate card.
export interface StatCard {
  id: Id; tableDefId: Id; tableName: string; tableLabel: string;
  label: string; func: AggFunc; column: string; filters: string; position: number;
}

export interface StatsResult { func: AggFunc; column: string; value: unknown; hasRows: boolean }
```

- [ ] **Step 2: i18n keys**

Append to `web/src/lib/i18n/en.ts`:

```ts
  "nav.dashboard": "Dashboard",
  "dash.title": "Dashboard",
  "dash.empty": "No stat cards yet.",
  "dash.emptyAdmin": "No stat cards yet — add one to show aggregate stats.",
  "dash.addCard": "Add Card",
  "card.label": "Label",
  "card.labelPh": "e.g. Paid revenue",
  "card.table": "Table",
  "card.func": "Function",
  "card.column": "Column",
  "card.filters": "Filters (optional)",
  "card.func.count": "Row count",
  "card.func.sum": "Sum",
  "card.func.avg": "Average",
  "card.func.min": "Minimum",
  "card.func.max": "Maximum",
  "card.save": "Save Card",
  "card.edit": "Edit Card",
  "card.delete": "Delete card?",
  "card.moveUp": "Move up",
  "card.moveDown": "Move down",
  "card.loading": "…",
  "card.noTables": "No tables available yet.",
  "card.of": "of {table}",
```

Append the matching block to `web/src/lib/i18n/id.ts` (Indonesian):

```ts
  "nav.dashboard": "Dasbor",
  "dash.title": "Dasbor",
  "dash.empty": "Belum ada card statistik.",
  "dash.emptyAdmin": "Belum ada card statistik — tambahkan untuk menampilkan agregat.",
  "dash.addCard": "Tambah Card",
  "card.label": "Label",
  "card.labelPh": "mis. Pendapatan lunas",
  "card.table": "Tabel",
  "card.func": "Fungsi",
  "card.column": "Kolom",
  "card.filters": "Filter (opsional)",
  "card.func.count": "Jumlah baris",
  "card.func.sum": "Jumlah",
  "card.func.avg": "Rata-rata",
  "card.func.min": "Minimum",
  "card.func.max": "Maksimum",
  "card.save": "Simpan Card",
  "card.edit": "Edit Card",
  "card.delete": "Hapus card ini?",
  "card.moveUp": "Naikkan",
  "card.moveDown": "Turunkan",
  "card.loading": "…",
  "card.noTables": "Belum ada tabel.",
  "card.of": "dari {table}",
```

- [ ] **Step 3: StatCardView component**

Create `web/src/components/StatCardView.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { Pencil, Trash2, ArrowDown, ArrowUp } from "lucide-react";
import { api } from "@/lib/api";
import { formatCell } from "@/lib/format";
import { useT, useI18nLang } from "@/lib/i18n";
import type { ColumnDef, StatCard, StatsResult, TableDefPayload } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

function statsPath(dataName: string, c: StatCard): string {
  const p = new URLSearchParams();
  p.set("func", c.func);
  if (c.column) p.set("column", c.column);
  if (c.filters && c.filters !== "[]") p.set("filters", c.filters);
  return `/data/${encodeURIComponent(dataName)}/stats?${p}`;
}

function renderCardValue(c: StatCard, res: StatsResult | undefined, col: ColumnDef | undefined, lang: string): string {
  if (!res) return "…";
  if (res.value === null || res.value === undefined) return "—";
  if (col && (typeof res.value === "number") && (col.fieldType === "number")) {
    return formatCell(col, res.value, lang);
  }
  if (col && col.fieldType === "datetime" && typeof res.value === "string") {
    const d = new Date(res.value);
    if (!isNaN(d.getTime())) {
      return new Intl.DateTimeFormat(lang === "id" ? "id-ID" : "en-US", { dateStyle: "medium", timeStyle: "short" }).format(d);
    }
  }
  return String(res.value);
}

export function StatCardView({ card, compact = false, onEdit, onDelete, onMove }: {
  card: StatCard;
  compact?: boolean;
  onEdit?: () => void;
  onDelete?: () => void;
  onMove?: (up: boolean) => void;
}) {
  const t = useT();
  const { lang } = useI18nLang();
  const dataName = card.tableName || card.tableDefId; // query views address by masked token
  const def = useQuery({
    queryKey: ["carddef", card.tableDefId],
    queryFn: () => api<TableDefPayload>(`/data/${encodeURIComponent(dataName)}`),
  });
  const stats = useQuery({
    queryKey: ["stats", card.id],
    queryFn: () => api<StatsResult>(statsPath(dataName, card)),
  });
  const col = def.data?.columns.find((c) => c.name === card.column);
  const value = renderCardValue(card, stats.data, col, lang);

  if (compact) {
    return (
      <div className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2">
        <div className="min-w-0">
          <div className="truncate text-[11px] text-muted-foreground">{card.label}</div>
          <div className="text-lg font-semibold tabular-nums leading-tight">{stats.isLoading ? t("card.loading") : value}</div>
        </div>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{card.label}</CardTitle>
        {(onEdit || onDelete || onMove) && (
          <div className="flex items-center gap-0.5">
            {onMove && (
              <>
                <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onMove(true)} title={t("card.moveUp")}><ArrowUp className="h-3.5 w-3.5" /></Button>
                <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onMove(false)} title={t("card.moveDown")}><ArrowDown className="h-3.5 w-3.5" /></Button>
              </>
            )}
            {onEdit && <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onEdit}><Pencil className="h-3.5 w-3.5" /></Button>}
            {onDelete && <Button variant="ghost" size="icon" className="h-6 w-6 text-destructive" onClick={onDelete}><Trash2 className="h-3.5 w-3.5" /></Button>}
          </div>
        )}
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-bold tabular-nums">{stats.isLoading ? t("card.loading") : value}</div>
        <div className="mt-1 text-xs text-muted-foreground">
          {t(`card.func.${card.func}`)}
          {card.column ? ` · ${col?.label ?? card.column}` : ""} · {t("card.of", { table: card.tableLabel })}
        </div>
      </CardContent>
    </Card>
  );
}
```

Notes: `t(key, vars)` interpolation is supported (`web/src/lib/i18n/index.tsx:63`). Check the `Card` component's export names in `web/src/components/ui/card.tsx`; adapt if different.

- [ ] **Step 4: Typecheck**

Run: `cd web && npm run build`
Expected: build succeeds (component not imported anywhere yet — ensure no unused-export errors; TS strict allows unused exports).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/types.ts web/src/lib/i18n/ web/src/components/StatCardView.tsx
git commit -m "feat(web): stat card view component, types and i18n"
```

---

### Task 8: web — Dashboard page + CardFormDialog + sidebar/route + Data strip

**Files:**
- Create: `web/src/pages/Dashboard.tsx`
- Create: `web/src/components/CardFormDialog.tsx`
- Modify: `web/src/main.tsx` (route)
- Modify: `web/src/components/Sidebar.tsx` (nav entry)
- Modify: `web/src/pages/Data.tsx` (card strip)

**Interfaces:**
- Consumes: Task 7 component/types, `FilterBar`, `serializeFilters`/`deserializeFilters`, `api`.

- [ ] **Step 1: CardFormDialog**

Create `web/src/components/CardFormDialog.tsx`:

```tsx
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useT } from "@/lib/i18n";
import type { AggFunc, ColumnDef, StatCard, TableDef, TableDefPayload } from "@/lib/types";
import { FilterBar, serializeFilters, deserializeFilters, type ActiveFilter } from "@/components/FilterBar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const FUNCS: AggFunc[] = ["count", "sum", "avg", "min", "max"];

function colsForFunc(cols: ColumnDef[], fn: AggFunc): ColumnDef[] {
  const real = cols.filter((c) => !c.isComputed && c.fieldType !== "m2m" && c.fieldType !== "fk");
  if (fn === "count") return [];
  if (fn === "sum" || fn === "avg") return real.filter((c) => c.fieldType === "number");
  return real.filter((c) => c.fieldType === "number" || c.fieldType === "datetime");
}

export function CardFormDialog({ open, onClose, card }: {
  open: boolean;
  onClose: () => void;
  card?: StatCard | null; // null/undefined = create
}) {
  const t = useT();
  const qc = useQueryClient();
  const tables = useQuery({ queryKey: ["defs"], queryFn: () => api<TableDef[]>("/tables") });
  const [tableId, setTableId] = useState("");
  const [label, setLabel] = useState("");
  const [fn, setFn] = useState<AggFunc>("count");
  const [column, setColumn] = useState("");
  const [filters, setFilters] = useState<ActiveFilter[]>([]);

  useEffect(() => {
    if (!open) return;
    if (card) {
      setTableId(card.tableDefId);
      setLabel(card.label);
      setFn(card.func);
      setColumn(card.column);
      setFilters(deserializeFilters(card.filters));
    } else {
      setTableId("");
      setLabel("");
      setFn("count");
      setColumn("");
      setFilters([]);
    }
  }, [open, card]);

  const def = useQuery({
    queryKey: ["cardformdef", tableId],
    enabled: !!tableId,
    queryFn: () => api<TableDefPayload>(`/tables/${tableId}`),
  });
  const allCols = def.data?.columns ?? [];
  const eligible = colsForFunc(allCols, fn);

  // drop column/filters that stop being valid when table or func changes
  useEffect(() => {
    if (column && !eligible.some((c) => c.name === column)) setColumn("");
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [tableId, fn, def.data]);
  useEffect(() => {
    const names = new Set(allCols.map((c) => c.name));
    setFilters((fs) => fs.filter((f) => names.has(f.column)));
    // eslint-disable-line react-hooks/exhaustive-deps
  }, [tableId]);

  const save = useMutation({
    mutationFn: () => {
      const body = {
        tableDefId: tableId,
        label: label.trim(),
        func: fn,
        column: fn === "count" ? "" : column,
        filters: serializeFilters(filters) || "[]",
      };
      return api(card ? `/cards/${card.id}` : "/cards", { method: card ? "PUT" : "POST", body: JSON.stringify(body) });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cards"] });
      onClose();
    },
  });

  const canSave = !!tableId && label.trim().length > 0 && (fn === "count" || !!column);

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{card ? t("card.edit") : t("dash.addCard")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <Label className="text-xs">{t("card.table")}</Label>
            <Select value={tableId || undefined} onValueChange={(v) => { setTableId(v); setColumn(""); setFilters([]); }}>
              <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {(tables.data ?? []).map((tb) => (
                  <SelectItem key={tb.id} value={tb.id} className="text-xs">{tb.label || tb.tableName}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">{t("card.label")}</Label>
            <Input className="h-9 text-xs" value={label} placeholder={t("card.labelPh")} onChange={(e) => setLabel(e.target.value)} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label className="text-xs">{t("card.func")}</Label>
              <Select value={fn} onValueChange={(v) => setFn(v as AggFunc)}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {FUNCS.map((f) => <SelectItem key={f} value={f} className="text-xs">{t(`card.func.${f}`)}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">{t("card.column")}</Label>
              <Select value={column || undefined} onValueChange={setColumn} disabled={fn === "count" || eligible.length === 0}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {eligible.map((c) => <SelectItem key={c.name} value={c.name} className="text-xs">{c.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">{t("card.filters")}</Label>
            {allCols.length > 0 ? (
              <FilterBar cols={allCols} filters={filters} onChange={setFilters} />
            ) : (
              <p className="text-xs text-muted-foreground">{t("card.noTables")}</p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button onClick={() => save.mutate()} disabled={!canSave || save.isPending} className="bg-blue-600 text-white hover:bg-blue-700">
            {save.isPending ? t("form.saving") : t("card.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

(`"form.saving"` exists already — see `Data.tsx`.)

- [ ] **Step 2: Dashboard page**

Create `web/src/pages/Dashboard.tsx`:

```tsx
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api } from "@/lib/api";
import { useT } from "@/lib/i18n";
import type { Me, StatCard } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { CardFormDialog } from "@/components/CardFormDialog";
import { StatCardView } from "@/components/StatCardView";

export default function Dashboard() {
  const t = useT();
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<Me>("/auth/me") });
  const cards = useQuery({ queryKey: ["cards"], queryFn: () => api<StatCard[]>("/cards") });
  const [editing, setEditing] = useState<StatCard | null>(null);
  const [open, setOpen] = useState(false);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["cards"] });
  const del = useMutation({
    mutationFn: (id: string) => api(`/cards/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
  const move = useMutation({
    mutationFn: ({ id, dir }: { id: string; dir: "up" | "down" }) =>
      api(`/cards/${id}/move`, { method: "POST", body: JSON.stringify({ dir }) }),
    onSuccess: invalidate,
  });

  const admin = !!me.data?.manageTables;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between border-b pb-4">
        <h1 className="text-xl font-semibold">{t("dash.title")}</h1>
        {admin && (
          <Button size="sm" className="gap-1" onClick={() => { setEditing(null); setOpen(true); }}>
            <Plus className="h-4 w-4" /> {t("dash.addCard")}
          </Button>
        )}
      </div>
      {(cards.data ?? []).length === 0 && !cards.isLoading ? (
        <p className="text-sm text-muted-foreground">{admin ? t("dash.emptyAdmin") : t("dash.empty")}</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {(cards.data ?? []).map((c) => (
            <StatCardView
              key={c.id}
              card={c}
              {...(admin ? {
                onEdit: () => { setEditing(c); setOpen(true); },
                onDelete: () => { if (confirm(t("card.delete"))) del.mutate(c.id); },
                onMove: (up: boolean) => move.mutate({ id: c.id, dir: up ? "up" : "down" }),
              } : {})}
            />
          ))}
        </div>
      )}
      <CardFormDialog open={open} onClose={() => setOpen(false)} card={editing} />
    </div>
  );
}
```

- [ ] **Step 3: Route + sidebar**

`web/src/main.tsx`: add `import Dashboard from "./pages/Dashboard";` and inside the `/` Route's children, FIRST (before `<Route index .../>`):

```tsx
            <Route path="dashboard" element={<Dashboard />} />
```

`web/src/components/Sidebar.tsx`: add `LayoutDashboard` to the lucide import list, then add an entry to the `navItems` array (defined at ~line 204) right before the `{ label: t("nav.tables"), path: "/", icon: Table2 }` entry so Dashboard sits above the table list for every authenticated user:

```tsx
    { label: t("nav.dashboard"), path: "/dashboard", icon: LayoutDashboard },
```

(The existing `navItems.map` rendering handles active state and collapsed mode — no other sidebar change needed.)

- [ ] **Step 4: Data.tsx card strip**

In `web/src/pages/Data.tsx`:

1. Imports: `import { StatCardView } from "../components/StatCardView";` and add `StatCard` to the types import.
2. Inside the `Data` component (after `const me = ...`):

```tsx
  const cards = useQuery({ queryKey: ["cards"], queryFn: () => api<StatCard[]>("/cards") });
  const tableCards = (cards.data ?? []).filter((c) => def.data && c.tableDefId === def.data.id);
```

3. Render the strip just ABOVE the `{/* Top Header & Action Toolbar */}` block:

```tsx
      {tableCards.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {tableCards.map((c) => (
            <StatCardView key={c.id} card={c} compact />
          ))}
        </div>
      )}
```

- [ ] **Step 5: Build + commit**

Run: `cd web && npm run build`
Expected: build succeeds, no TS errors.

```bash
git add web/src/
git commit -m "feat(web): dashboard page with stat cards and data-page strip"
```

---

### Task 9: web — runtime JSON editor for mixed text columns

**Files:**
- Create: `web/src/lib/jsondetect.ts`
- Modify: `web/src/pages/Data.tsx` (grid render, form prettify, textarea, save validation)

**Interfaces:**
- Produces: `looksLikeJSON(s: string): boolean`, `jsonModeColumns(cols, row): string[]` (internal helpers).

- [ ] **Step 1: Helper**

Create `web/src/lib/jsondetect.ts`:

```ts
// looksLikeJSON reports whether a string should be treated as a JSON
// document: it must start with { or [ and parse cleanly. Guarded prefix
// keeps "123", "true" and prose out (they are valid bare JSON values but
// never useful to pretty-print).
export function looksLikeJSON(s: string): boolean {
  const t = s.trimStart();
  if (!t.startsWith("{") && !t.startsWith("[")) return false;
  try {
    JSON.parse(t);
    return true;
  } catch {
    return false;
  }
}
```

- [ ] **Step 2: Grid rendering**

In `Data.tsx` `renderValue` (the function at ~line 1237), the `json` branch already renders a clamped `<pre>`. Change its condition from:

```tsx
  if (type === "json") {
```

to:

```tsx
  if (type === "json" || (type === "text" && typeof v === "string" && looksLikeJSON(v))) {
```

(the body — `prettyJSON(String(v))` + `<pre>` clamp — is reused unchanged; `prettyJSON` already falls back to the raw string).

- [ ] **Step 3: Form open prettify + JSON-mode tracking**

In `Data.tsx`:

1. Import `looksLikeJSON`.
2. Extend the form state type (find `useState<{ mode: "new" | "edit"; row: Row; initialKey?: string[] | null } | null>`) to carry the columns that started as JSON:

```tsx
  const [form, setForm] = useState<{ mode: "new" | "edit"; row: Row; initialKey?: string[] | null; jsonModes?: string[] } | null>(null);
```

3. Replace `prettifyFormRow` with a version that also returns the JSON-mode column names and covers text columns:

```tsx
  // pretty-print json column values (and JSON-looking text values) once,
  // when form state is created (keeps the textarea a plain controlled
  // input while editing); jsonModes records the text columns that started
  // as JSON — their edited content must stay valid JSON on save
  const prettifyFormRow = (row: Row): { row: Row; jsonModes: string[] } => {
    const out: Row = { ...row };
    const modes: string[] = [];
    for (const c of def.data?.columns ?? []) {
      if (typeof out[c.name] !== "string") continue;
      const s = out[c.name] as string;
      if (c.fieldType === "json") {
        out[c.name] = prettyJSON(s);
      } else if (c.fieldType === "text" && looksLikeJSON(s)) {
        out[c.name] = prettyJSON(s);
        modes.push(c.name);
      }
    }
    return { row: out, jsonModes: modes };
  };
```

4. Update every `setForm(...}` call site that used `prettifyFormRow(...)` (the copy handler and the auto-edit effect — and any edit-open handler that builds rows directly; grep `setForm({`): the edit/copy paths become

```tsx
    const p = prettifyFormRow(target);
    setForm({ mode: "edit", row: p.row, initialKey: k, jsonModes: p.jsonModes });
```

and for the row-copy/new path

```tsx
    const p = prettifyFormRow(row);
    // ...existing key deletion on p.row...
    setForm({ mode: "new", row: p.row, jsonModes: p.jsonModes });
```

The plain "new row" (empty) path sets `jsonModes: []` implicitly (undefined is fine — treat as empty).

5. In the save-submit validation block (where `badJson` is computed for `fieldType === "json"`), extend to text columns that started in JSON mode:

```tsx
                const jsonCols = new Set(form!.jsonModes ?? []);
                const badJson = modalFields.filter(
                  (c) =>
                    (c.fieldType === "json" || (c.fieldType === "text" && jsonCols.has(c.name))) &&
                    typeof form!.row[c.name] === "string" &&
                    (() => { try { JSON.parse(form!.row[c.name] as string); return false; } catch { return true; } })()
                );
```

6. In `FieldInput`, render the mono textarea for text columns currently holding JSON — change the final fallback branch chain by inserting before the default `<Input>`:

```tsx
      ) : col.fieldType === "text" && looksLikeJSON(val) ? (
        <Textarea
          disabled={disabled}
          className="min-h-[100px] font-mono text-xs"
          value={val}
          onChange={(e) => onChange(e.target.value === "" ? null : e.target.value)}
        />
      ) : (
```

- [ ] **Step 4: Build + commit**

Run: `cd web && npm run build`
Expected: build succeeds.

```bash
git add web/src/lib/jsondetect.ts web/src/pages/Data.tsx
git commit -m "feat(web): JSON-aware display and editing for mixed text columns"
```

---

### Task 10: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full Go test suites**

```bash
cd /opt/project/ku-crud && go vet ./... && go test ./...
cd /opt/project/ku-crud/core && go vet ./... && go test ./...
```
Expected: all PASS.

- [ ] **Step 2: Frontend build**

```bash
cd /opt/project/ku-crud/web && npm run build
```
Expected: success; `web/dist` regenerated (dist is committed in this repo — verify `git status` shows the expected rebuilt asset hash changes and stage them).

- [ ] **Step 3: Live smoke (optional, if env set)**

```bash
cd /opt/project/ku-crud/core && KUCRUD_TEST_PG=... go test ./ds/ -run TestAggregateRowsLive -v
```

- [ ] **Step 4: Commit any remaining artifacts**

```bash
git add -A
git commit -m "chore: rebuild web dist for stat cards release"
```

---

## Self-Review Notes

- Spec coverage: 1a→Tasks 1–2, 1b→Task 3, 1c→Task 4, 1d→Tasks 5–6, 1e→Tasks 7–8 (incl. Data strip + i18n), 1f error codes/tests embedded per task, Feature 2→Task 9. Out-of-scope items untouched.
- Template app: gains the core `stats` route automatically (no template change — matches spec).
- The `docs/superpowers/specs` file remains the requirements source; this plan's code is authoritative for implementation shape.
