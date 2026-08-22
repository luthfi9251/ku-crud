# Custom Query Views (v1.8) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Table definitions whose data source is a stored SQL SELECT (read-only grid with full search/sort/filter/pagination/export), executed under layered guards.

**Architecture:** `table_defs` gains `source_type`/`query_sql`; the ds layer wraps the stored query as a derived table (`SELECT ... FROM (query) AS ku_q ...`) so the existing filter/sort/search pipeline is reused; adapters execute inside read-only transactions with statement timeout; all write endpoints 403 for query defs.

**Tech Stack:** Go 1.25 stdlib `net/http`, `database/sql`, pgx v5, go-sql-driver/mysql, modernc.org/sqlite, React SPA (TypeScript).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-22-v1.8-query-views-design.md`
- Error codes verbatim: `QUERY_INVALID` (400), `QUERY_READONLY` (403), `QUERY_NO_KEY` (400), `QUERY_TIMEOUT` (502)
- Query cap: 20 000 chars; prefix must be `SELECT` or `WITH`; timeout 15 s (`ds.QueryTimeout` var)
- Derived-table alias: `ku_q`
- ku-crud never runs DDL; request-controlled values never enter SQL text (bind args only)
- All Go code follows existing patterns (see `internal/ds/sqlkit.go`, `internal/api/tables.go`); no comments unless matching house style
- Test commands: `go test ./internal/<pkg>/ -run <Test> -v` from repo root; live tests skip when `KUCRUD_TEST_PG` / `KUCRUD_TEST_MYSQL` unset
- Live DBs (docker-compose.yml): PG `host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable`; MySQL DSN `ku:ku@tcp(localhost:3307)/ku`
- One commit per task, message style `feat(scope): ...` matching `git log --oneline`

---

### Task 1: Meta layer — migration 11 + TableDef fields

**Files:**
- Modify: `internal/meta/meta.go` (migrations array, after the v1.7 entry)
- Modify: `internal/meta/tabledefs.go` (struct + 4 SQL statements)
- Modify: `internal/meta/metatransfer.go` (ApplyImport INSERT/UPDATE)
- Test: `internal/meta/migration11_test.go` (new)

**Interfaces:**
- Produces: `TableDef.SourceType string` (`"table"` default, `"query"`), `TableDef.QuerySQL string`; normalization on read: empty `SourceType` → `"table"`. Used by Tasks 3–9.

- [ ] **Step 1: Write the failing test**

Create `internal/meta/migration11_test.go`:

```go
package meta

import "testing"

func TestMigration11QueryDefs(t *testing.T) {
	st, err := Open(t.TempDir() + "/m11.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1,
		DBName: "b", Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SourceType: "query",
		QuerySQL: "SELECT name AS n, balance FROM customers",
		Label: "Q", KeyColumns: []string{}, PageSize: 20}
	cols := []ColumnDef{{Name: "n", Label: "N", FieldType: "text",
		Visible: true, Searchable: true, Sortable: true, Position: 0}}
	if err := st.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	got, gcols, err := st.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceType != "query" || got.QuerySQL != def.QuerySQL || len(gcols) != 1 {
		t.Fatalf("round-trip = %+v cols=%d", got, len(gcols))
	}
	list, err := st.ListTableDefs()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v %v", list, err)
	}
	if list[0].SourceType != "query" {
		t.Fatalf("list sourceType = %q", list[0].SourceType)
	}
	// legacy rows read as "table"
	if err := st.UpdateTableDef(&TableDef{ID: def.ID, DatasourceID: 1,
		SchemaName: "s", TableName: "t", Label: "L", KeyColumns: []string{"n"},
		PageSize: 20, SourceType: "table", QuerySQL: ""}, cols); err != nil {
		t.Fatal(err)
	}
	got, _, _ = st.GetTableDef(def.ID)
	if got.SourceType != "table" {
		t.Fatalf("table def sourceType = %q", got.SourceType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/meta/ -run TestMigration11QueryDefs -v`
Expected: FAIL — `SourceType`/`QuerySQL` undefined on `TableDef`.

- [ ] **Step 3: Implement**

`internal/meta/meta.go` — append migration 12 (index 11) to the `migrations` var, after the v1.7 entry:

```go
	// v1.8: query-backed table definitions (custom views).
	`ALTER TABLE table_defs ADD COLUMN source_type TEXT NOT NULL DEFAULT 'table';
ALTER TABLE table_defs ADD COLUMN query_sql TEXT NOT NULL DEFAULT '';`,
```

`internal/meta/tabledefs.go` — add to `TableDef` after `Hooks`:

```go
	SourceType string `json:"sourceType,omitempty"` // "table" (default) | "query"
	QuerySQL   string `json:"querySql,omitempty"`
```

Add helper below the struct:

```go
func normalizeSource(d *TableDef) {
	if d.SourceType != "query" {
		d.SourceType = "table"
		d.QuerySQL = ""
	}
}
```

In `SaveTableDef`: call `normalizeSource(def)` at the top; extend the INSERT column list and VALUES with `source_type,query_sql` → 15 columns total:

```go
	res, err := tx.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,description,key_columns,page_size,default_sort_col,default_sort_dir,default_view,view_config,group_id,hooks,source_type,query_sql)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, def.Description, string(kj), def.PageSize, def.DefaultSortCol, def.DefaultSortDir, def.DefaultView, def.ViewConfig, gid, def.Hooks, def.SourceType, def.QuerySQL)
```

In `UpdateTableDef`: same `normalizeSource(def)` at top; UPDATE statement becomes:

```go
	res, err := tx.Exec(`UPDATE table_defs SET datasource_id=?,schema_name=?,table_name=?,label=?,description=?,key_columns=?,page_size=?,default_sort_col=?,default_sort_dir=?,default_view=?,view_config=?,group_id=?,hooks=?,source_type=?,query_sql=?
		WHERE id=?`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, def.Description, string(kj), def.PageSize, def.DefaultSortCol, def.DefaultSortDir, def.DefaultView, def.ViewConfig, gid, def.Hooks, def.SourceType, def.QuerySQL, def.ID)
```

In `ListTableDefs` and `GetTableDef`: add `source_type,query_sql` to both SELECT lists (before `FROM`), add `&d.SourceType, &d.QuerySQL` to both Scan calls, and call `normalizeSource(d)` after scanning (before appending/returning).

`internal/meta/metatransfer.go` `ApplyImport`: in pass 1, add `source_type,query_sql` to both the UPDATE and INSERT statements with values `d.Def.SourceType, d.Def.QuerySQL` (before the `WHERE id=?` / matching placeholders).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/meta/ -run TestMigration11QueryDefs -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/meta/
git commit -m "feat(meta): source_type/query_sql on table defs (migration 11)"
```

---

### Task 2: ds layer — QueryParams + wrapper builders

**Files:**
- Create: `internal/ds/query.go`
- Test: `internal/ds/query_test.go` (new)

**Interfaces:**
- Produces: `QueryParams` struct (fields: `Query string; Columns []string; Searchable []string; Search string; SortCol, SortDir string; Limit, Offset int; Filters []ColumnFilter`); dialect methods `buildQueryList(p QueryParams) (string, []any, error)` / `buildQueryCount(p QueryParams) (string, []any, error)`; `QueryTimeout time.Duration` var (default 15s); `IsQueryTimeout(err error) bool`; const `queryAlias = "ku_q"`. Used by Tasks 3–4.

- [ ] **Step 1: Write the failing test**

Create `internal/ds/query_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ds/ -run TestBuildQuery -v`
Expected: FAIL — `QueryParams` undefined.

- [ ] **Step 3: Implement**

Create `internal/ds/query.go`:

```go
package ds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryAlias names the derived table wrapping a stored query view.
const queryAlias = "ku_q"

// QueryTimeout bounds one query-view execution (layer-3 guard).
var QueryTimeout = 15 * time.Second

// IsQueryTimeout reports whether err is a driver-level statement timeout.
func IsQueryTimeout(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "57014" // query_canceled (statement_timeout)
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 3024 || me.Number == 1969 // query / statement timeout
	}
	return false
}

// QueryParams carries one query-view list/count request. Columns, Searchable
// and SortCol resolve ONLY from stored metadata (validated by the api layer).
type QueryParams struct {
	Query      string
	Columns    []string
	Searchable []string
	Search     string
	SortCol    string
	SortDir    string
	Limit      int
	Offset     int
	Filters    []ColumnFilter
}

func (dt sqlDialect) buildQueryList(p QueryParams) (string, []any, error) {
	alias, err := dt.quoteIdent(queryAlias)
	if err != nil {
		return "", nil, err
	}
	cols := make([]string, len(p.Columns))
	for i, c := range p.Columns {
		q, err := dt.colRef(alias, c)
		if err != nil {
			return "", nil, err
		}
		cols[i] = q
	}
	valid := map[string]bool{}
	for _, c := range p.Columns {
		valid[c] = true
	}
	if !valid[p.SortCol] {
		return "", nil, fmt.Errorf("sort column %q not selectable", p.SortCol)
	}
	qsort, err := dt.colRef(alias, p.SortCol)
	if err != nil {
		return "", nil, err
	}
	if p.SortDir != "ASC" && p.SortDir != "DESC" {
		return "", nil, fmt.Errorf("invalid sort direction %q", p.SortDir)
	}
	sCond, sArgs, next := dt.searchWhere(p.Searchable, p.Search, 1, alias)
	joins, fCond, fArgs, next2, err := dt.filterParts(p.Filters, next, alias)
	if err != nil {
		return "", nil, err
	}
	var conds []string
	if sCond != "" {
		conds = append(conds, sCond)
	}
	if fCond != "" {
		conds = append(conds, fCond)
	}
	whereAll := ""
	if len(conds) > 0 {
		whereAll = " WHERE " + strings.Join(conds, " AND ")
	}
	args := append(sArgs, fArgs...)
	sql := "SELECT " + strings.Join(cols, ",") + " FROM (" + p.Query + ") " + alias +
		joins + whereAll + " ORDER BY " + qsort + " " + p.SortDir +
		fmt.Sprintf(" LIMIT %s OFFSET %s", dt.placeholder(next2), dt.placeholder(next2+1))
	args = append(args, p.Limit, p.Offset)
	return sql, args, nil
}

func (dt sqlDialect) buildQueryCount(p QueryParams) (string, []any, error) {
	alias, err := dt.quoteIdent(queryAlias)
	if err != nil {
		return "", nil, err
	}
	sCond, sArgs, next := dt.searchWhere(p.Searchable, p.Search, 1, alias)
	_, fCond, fArgs, _, err := dt.filterParts(p.Filters, next, alias)
	if err != nil {
		return "", nil, err
	}
	var conds []string
	if sCond != "" {
		conds = append(conds, sCond)
	}
	if fCond != "" {
		conds = append(conds, fCond)
	}
	whereAll := ""
	if len(conds) > 0 {
		whereAll = " WHERE " + strings.Join(conds, " AND ")
	}
	return "SELECT COUNT(*) FROM (" + p.Query + ") " + alias + whereAll, append(sArgs, fArgs...), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ds/ -run TestBuildQuery -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ds/query.go internal/ds/query_test.go
git commit -m "feat(ds): derived-table wrapper builders for query views"
```

---

### Task 3: Adapter contract + PG/MySQL query execution

**Files:**
- Modify: `internal/ds/adapter.go` (Adapter interface)
- Modify: `internal/ds/postgres.go`
- Modify: `internal/ds/mysql.go`

**Interfaces:**
- Consumes: Task 2 (`QueryParams`, `QueryTimeout`, `queryAlias`).
- Produces (added to `Adapter`): `ExplainQuery(query string) error`; `IntrospectQuery(query string) (cols []LiveColumn, dropped []string, err error)`; `ListQueryRows(p QueryParams) ([]map[string]any, error)`; `CountQueryRows(p QueryParams) (int, error)`. Used by Tasks 5–9.

- [ ] **Step 1: Extend the Adapter interface**

In `internal/ds/adapter.go`, add to the `Adapter` interface after `InspectTable`:

```go
	// Query views: read-only SQL-backed definitions (v1.8). Execution runs
	// inside a read-only transaction with QueryTimeout applied.
	ExplainQuery(query string) error
	IntrospectQuery(query string) ([]LiveColumn, []string, error)
	ListQueryRows(p QueryParams) ([]map[string]any, error)
	CountQueryRows(p QueryParams) (int, error)
```

Run: `go build ./...` — Expected: FAIL (pgAdapter/mysqlAdapter missing methods).

- [ ] **Step 2: Implement PG**

`internal/ds/postgres.go` — add `"context"` to imports, then append:

```go
// ---- query views (v1.8) ----

// pgTypeName maps driver-level type names (pgx DatabaseTypeName) to field
// types; "" = excluded (arrays, bytea, unknown).
func pgTypeName(n string) string {
	switch n {
	case "BOOL":
		return "boolean"
	case "INT2", "INT4", "INT8", "NUMERIC", "FLOAT4", "FLOAT8":
		return "number"
	case "DATE", "TIMESTAMP", "TIMESTAMPTZ", "TIME", "TIMETZ":
		return "datetime"
	case "TEXT", "VARCHAR", "BPCHAR":
		return "text"
	case "UUID":
		return "uuid"
	case "JSON", "JSONB":
		return "json"
	}
	return ""
}

// queryExec runs fn inside a read-only tx with the statement timeout set
// (layers 2-3). SET LOCAL auto-resets at tx end.
func (a *pgAdapter) queryExec(fn func(tx *sql.Tx) error) error {
	ctx := context.Background()
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", QueryTimeout.Milliseconds())); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func queryMapsTx(tx *sql.Tx, sqlText string, args ...any) ([]map[string]any, error) {
	rows, err := tx.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		scan := scanTargets(len(cols))
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		out = append(out, rowToMap(cols, deref(scan)))
	}
	return out, rows.Err()
}

func (a *pgAdapter) ExplainQuery(query string) error {
	rows, err := a.db.Query("EXPLAIN " + query)
	if err != nil {
		return err
	}
	rows.Close()
	return rows.Err()
}

func (a *pgAdapter) IntrospectQuery(query string) ([]LiveColumn, []string, error) {
	var cols []LiveColumn
	var dropped []string
	err := a.queryExec(func(tx *sql.Tx) error {
		rows, err := tx.Query(fmt.Sprintf("SELECT * FROM (%s) AS %s LIMIT 0", query, queryAlias))
		if err != nil {
			return err
		}
		defer rows.Close()
		ct, err := rows.ColumnTypes()
		if err != nil {
			return err
		}
		for _, c := range ct {
			ft := pgTypeName(c.DatabaseTypeName())
			if _, qerr := QuoteIdent(c.Name()); qerr != nil || ft == "" {
				dropped = append(dropped, c.Name())
				continue
			}
			nullable, _ := c.Nullable()
			cols = append(cols, LiveColumn{Name: c.Name(), FieldType: ft, Nullable: nullable})
		}
		return nil
	})
	return cols, dropped, err
}

func (a *pgAdapter) ListQueryRows(p QueryParams) ([]map[string]any, error) {
	sqlText, args, err := pgDialect.buildQueryList(p)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	err = a.queryExec(func(tx *sql.Tx) error {
		out, err = queryMapsTx(tx, sqlText, args...)
		return err
	})
	return out, err
}

func (a *pgAdapter) CountQueryRows(p QueryParams) (int, error) {
	sqlText, args, err := pgDialect.buildQueryCount(p)
	if err != nil {
		return 0, err
	}
	var n int
	err = a.queryExec(func(tx *sql.Tx) error {
		return tx.QueryRow(sqlText, args...).Scan(&n)
	})
	return n, err
}
```

- [ ] **Step 3: Implement MySQL**

`internal/ds/mysql.go` — add `"context"` to imports, then append:

```go
// ---- query views (v1.8) ----

type ctxQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// mysqlTypeName maps driver-level type names to field types. go-sql-driver
// reports TEXT as "BLOB", so text columns surface only via CHAR/VARCHAR
// result names — CAST(x AS CHAR) aliases them explicitly.
func mysqlTypeName(ct *sql.ColumnType) string {
	switch ct.DatabaseTypeName() {
	case "TINY":
		if l, ok := ct.Length(); ok && l <= 1 {
			return "boolean"
		}
		return "number"
	case "SHORT", "INT24", "LONG", "LONGLONG", "FLOAT", "DOUBLE", "NEWDECIMAL", "DECIMAL":
		return "number"
	case "DATE", "DATETIME", "TIMESTAMP", "TIME", "NEWDATE":
		return "datetime"
	case "VARCHAR", "VAR_STRING", "STRING":
		return "text"
	case "JSON":
		return "json"
	}
	return ""
}

// withQueryConn runs fn on a dedicated conn in a READ ONLY session with the
// execution-time cap set; both settings are restored before the conn returns
// to the pool (layers 2-3). MySQL has no per-query read-only flag, so the
// session-scoped switch is the available isolation.
func (a *mysqlAdapter) withQueryConn(fn func(ctx context.Context, q ctxQuerier) error) error {
	ctx := context.Background()
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "SET SESSION TRANSACTION READ WRITE")
	if _, err := conn.ExecContext(ctx,
		fmt.Sprintf("SET SESSION MAX_EXECUTION_TIME = %d", QueryTimeout.Milliseconds())); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "SET SESSION MAX_EXECUTION_TIME = 0")
	return fn(ctx, conn)
}

func (a *mysqlAdapter) ExplainQuery(query string) error {
	// EXPLAIN inside the read-only session: MySQL may execute uncorrelated
	// subqueries during planning, so the session guard applies here too.
	return a.withQueryConn(func(ctx context.Context, q ctxQuerier) error {
		rows, err := q.QueryContext(ctx, "EXPLAIN "+query)
		if err != nil {
			return err
		}
		rows.Close()
		return rows.Err()
	})
}

func (a *mysqlAdapter) IntrospectQuery(query string) ([]LiveColumn, []string, error) {
	var cols []LiveColumn
	var dropped []string
	err := a.withQueryConn(func(ctx context.Context, q ctxQuerier) error {
		rows, err := q.QueryContext(ctx,
			fmt.Sprintf("SELECT * FROM (%s) AS %s LIMIT 0", query, queryAlias))
		if err != nil {
			return err
		}
		defer rows.Close()
		ct, err := rows.ColumnTypes()
		if err != nil {
			return err
		}
		for _, c := range ct {
			ft := mysqlTypeName(c)
			if _, qerr := QuoteIdent(c.Name()); qerr != nil || ft == "" {
				dropped = append(dropped, c.Name())
				continue
			}
			nullable, _ := c.Nullable()
			cols = append(cols, LiveColumn{Name: c.Name(), FieldType: ft, Nullable: nullable})
		}
		return nil
	})
	return cols, dropped, err
}

func (a *mysqlAdapter) ListQueryRows(p QueryParams) ([]map[string]any, error) {
	sqlText, args, err := mysqlDialect.buildQueryList(p)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	err = a.withQueryConn(func(ctx context.Context, q ctxQuerier) error {
		rows, err := q.QueryContext(ctx, sqlText, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		names, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			scan := scanTargets(len(names))
			if err := rows.Scan(scan...); err != nil {
				return err
			}
			out = append(out, rowToMap(names, deref(scan)))
		}
		return rows.Err()
	})
	return out, err
}

func (a *mysqlAdapter) CountQueryRows(p QueryParams) (int, error) {
	sqlText, args, err := mysqlDialect.buildQueryCount(p)
	if err != nil {
		return 0, err
	}
	var n int
	err = a.withQueryConn(func(ctx context.Context, q ctxQuerier) error {
		return q.QueryContext(ctx, sqlText, args...).Scan
			// placeholder replaced below
	})
	return n, err
}
```

Fix `CountQueryRows` for MySQL (a `*sql.Rows` cannot Scan directly — use QueryRow semantics via a one-row helper):

```go
func (a *mysqlAdapter) CountQueryRows(p QueryParams) (int, error) {
	sqlText, args, err := mysqlDialect.buildQueryCount(p)
	if err != nil {
		return 0, err
	}
	var n int
	err = a.withQueryConn(func(ctx context.Context, q ctxQuerier) error {
		rows, err := q.QueryContext(ctx, sqlText, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(&n)
	})
	return n, err
}
```

(Use this second version; the first snippet's marked line is intentionally rejected by the compiler.)

- [ ] **Step 4: Build**

Run: `go build ./... && go vet ./internal/ds/`
Expected: clean.

- [ ] **Step 5: Run the full ds suite (regression)**

Run: `go test ./internal/ds/`
Expected: PASS (live tests skip without env vars).

- [ ] **Step 6: Commit**

```bash
git add internal/ds/adapter.go internal/ds/postgres.go internal/ds/mysql.go
git commit -m "feat(ds): query-view execution behind Adapter (read-only tx + timeout)"
```

---

### Task 4: Live adapter tests — guards proven per dialect

**Files:**
- Test: `internal/ds/query_live_test.go` (new)

**Interfaces:**
- Consumes: Task 3 adapter methods; Task 2 `QueryTimeout`/`IsQueryTimeout`.

- [ ] **Step 1: Write the tests**

Create `internal/ds/query_live_test.go`:

```go
package ds

import (
	"database/sql"
	"errors"
	"os"
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
		"UPDATE t SET a = 1",
		"SELECT $1 AS one",
		"SELECT FROM WHERE",
	} {
		if err := a.ExplainQuery(bad); err == nil {
			t.Fatalf("EXPLAIN accepted %q", bad)
		}
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
	_, _, err := a.IntrospectQuery("SELECT setval('qv_t_id_seq', 1) AS x")
	if err == nil || !errors.Is(err, sql.ErrNoRows) && err.Error() == "" {
		// setval is rejected by read-only transactions (25006); any error is
		// acceptable proof, no error is a failure.
		if err == nil {
			t.Fatal("side-effecting function succeeded in read-only tx")
		}
	}
	if err != nil && !containsAny(err.Error(), "read-only", "25006") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestQueryTimeoutPG(t *testing.T) {
	a := openPG(t)
	old := QueryTimeout
	QueryTimeout = 2 * time.Second
	t.Cleanup(func() { QueryTimeout = old })
	_, err := a.ListQueryRows(QueryParams{Query: "SELECT pg_sleep(30) AS s",
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
```

Note: simplify `TestQueryReadOnlyPG` — remove the first confusing `if` and keep only:

```go
	_, _, err := a.IntrospectQuery("SELECT setval('qv_t_id_seq', 1) AS x")
	if err == nil {
		t.Fatal("side-effecting function succeeded in read-only tx")
	}
	if !containsAny(err.Error(), "read-only", "25006") {
		t.Fatalf("unexpected error: %v", err)
	}
```

- [ ] **Step 2: Run with live DBs if available, else confirm skips**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/ds/ -run 'TestExplainQuery|TestQueryReadOnly|TestQueryTimeout|TestIntrospectQuery|TestListQueryRowsMySQL' -v` (omit env to see skips)
Expected: PASS, or SKIP lines when env unset.

- [ ] **Step 3: Commit**

```bash
git add internal/ds/query_live_test.go
git commit -m "test(ds): live guard proofs for query views (explain/readonly/timeout)"
```

---

### Task 5: API — input/DTO/validation, write guards, permission lock

**Files:**
- Modify: `internal/api/tables.go` (input structs, toDef, toTableDTO, tablePerms, validateDef, create/update handlers)
- Modify: `internal/api/rows.go` (write guards + QUERY_NO_KEY)
- Modify: `internal/api/bulk.go` (guard)
- Modify: `internal/api/import.go` (guard in importCtx)
- Modify: `internal/api/rels.go` (guards ×3)
- Test: `internal/api/queryviews_test.go` (new)

**Interfaces:**
- Consumes: Task 1 (`SourceType`/`QuerySQL`); Task 3 (`ExplainQuery`).
- Produces: `checkQuerySQL(q string) string`; `explainQueryDef(w, def) bool` (true = wrote error); `writeQueryReadOnly(w, def) bool`; `writeQueryErr(w, err)` (timeout-aware 502). Used by Tasks 6–9.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/queryviews_test.go`:

```go
package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

// seedQueryDef stores a query def directly (no live DB needed for guards).
func seedQueryDef(t *testing.T, s *Server, keys []string) {
	t.Helper()
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 1, SourceType: "query",
		QuerySQL: "SELECT name AS n FROM customers", Label: "Q",
		KeyColumns: keys, PageSize: 20}
	cols := []meta.ColumnDef{{Name: "n", Label: "N", FieldType: "text",
		Visible: true, Searchable: true, Sortable: true, Position: 0}}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func TestQueryDefWriteGuards(t *testing.T) {
	s := newTestServer(t)
	c := login(s) // alice = first user = Admin
	seedQueryDef(t, s, []string{"n"})
	tok := tdTok(s, 1)
	pk := rowKeyToken([]string{"jo"}) // helper from rows_composite_test.go
	endpoints := []struct{ method, path string }{
		{"POST", "/api/tables/" + tok + "/rows"},
		{"PUT", "/api/tables/" + tok + "/rows/" + pk},
		{"DELETE", "/api/tables/" + tok + "/rows/" + pk},
		{"POST", "/api/tables/" + tok + "/rows/bulk-delete"},
		{"GET", "/api/tables/" + tok + "/fkoptions/n"},
		{"GET", "/api/tables/" + tok + "/m2moptions/n"},
		{"GET", "/api/tables/" + tok + "/rows/" + pk + "/m2m/n"},
	}
	for _, e := range endpoints {
		w := do(s, e.method, e.path, "{}", c)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "QUERY_READONLY") {
			t.Fatalf("%s %s = %d %s", e.method, e.path, w.Code, w.Body)
		}
	}
	// admin included — query views have no write path at all
	w := do(s, "POST", "/api/tables/"+tok+"/rows", `{"n":"x"}`, c)
	if w.Code != 403 {
		t.Fatalf("admin write = %d %s", w.Code, w.Body)
	}
}

func TestQueryDefNoKeyRowGet(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedQueryDef(t, s, nil)
	w := do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows/anything", "", c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "QUERY_NO_KEY") {
		t.Fatalf("row get no-key = %d %s", w.Code, w.Body)
	}
}

func TestQueryDefValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	dsTok := s.ids.Encode("ds", 1)
	body := func(extra string) string {
		return `{"datasourceId":"` + dsTok + `","label":"Q","sourceType":"query",` + extra +
			`,"pageSize":20,"keyColumns":[],"columns":[` +
			`{"name":"n","label":"N","fieldType":"text","visible":true,"searchable":true,"sortable":true,"position":0}]}`
	}
	// dead datasource → EXPLAIN fails → 400 QUERY_INVALID
	w := do(s, "POST", "/api/tables", body(`"querySql":"SELECT 1 AS n"`), c)
	if w.Code != 400 {
		t.Fatalf("explain-on-save = %d %s", w.Code, w.Body)
	}
	// prefix check fires before any connection is attempted
	w = do(s, "POST", "/api/tables", body(`"querySql":"DELETE FROM x"`), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "SELECT or WITH") {
		t.Fatalf("prefix check = %d %s", w.Code, w.Body)
	}
	// fk columns rejected on query defs
	w = do(s, "POST", "/api/tables", `{"datasourceId":"`+dsTok+`","label":"Q","sourceType":"query",`+
		`"querySql":"SELECT 1 AS n","pageSize":20,"keyColumns":[],"columns":[`+
		`{"name":"n","label":"N","fieldType":"fk","baseType":"number","fkTableDefId":"self",`+
		`"fkRefColumn":"n","fkDisplayColumns":["n"],"position":0}]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "query view") {
		t.Fatalf("fk on query = %d %s", w.Code, w.Body)
	}
}
```

Check: if `rowKeyToken` does not exist in the test helpers, add to this file:

```go
func rowKeyToken(vals []string) string {
	b, _ := json.Marshal(vals)
	return base64.RawURLEncoding.EncodeToString(b)
}
```

with `"encoding/base64"` and `"encoding/json"` imports (drop it if a helper already exists — `rows_composite_test.go` may provide one under a different name; use whatever exists).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestQueryDef' -v`
Expected: FAIL — guards absent (writes hit adapter), fields not parsed.

- [ ] **Step 3: Implement**

`internal/api/tables.go`:

1. `tableDefInput` — add after `Hooks`:
```go
	SourceType string `json:"sourceType"`
	QuerySQL   string `json:"querySql"`
```
2. New helpers (place above `validateDef`):
```go
const querySQLMax = 20000

func checkQuerySQL(q string) string {
	if q == "" {
		return "querySql is required for query views"
	}
	if len(q) > querySQLMax {
		return "querySql exceeds 20000 characters"
	}
	head := strings.ToUpper(strings.TrimSpace(q))
	if !strings.HasPrefix(head, "SELECT") && !strings.HasPrefix(head, "WITH") {
		return "query must start with SELECT or WITH"
	}
	return ""
}

func isQueryDef(def *meta.TableDef) bool { return def.SourceType == "query" }
```
3. `toDef` — before building the struct: after `in.Description = strings.TrimSpace(...)`, add:
```go
	if in.SourceType != "query" {
		in.SourceType = "table"
		in.QuerySQL = ""
	}
	if in.SourceType == "query" {
		in.SchemaName, in.TableName = "", ""
	}
```
and add `SourceType: in.SourceType, QuerySQL: in.QuerySQL,` to the returned `&meta.TableDef{...}` literal.
4. `tableDefDTO` — add after `Hooks`:
```go
	SourceType string          `json:"sourceType,omitempty"`
	QuerySQL   string          `json:"querySql,omitempty"`
```
`toTableDTO` — fill `SourceType: def.SourceType, QuerySQL: def.QuerySQL`.
5. `tablePerms` — change signature to `(u CtxUser, def *meta.TableDef) permsDTO`; body:
```go
func (s *Server) tablePerms(u CtxUser, def *meta.TableDef) permsDTO {
	var p permsDTO
	if u.IsAdmin {
		p = permsDTO{true, true, true, true}
	} else {
		g, err := s.store.GrantsFor(u.RoleID, def.ID)
		if err != nil {
			return permsDTO{}
		}
		p = permsDTO{g.CanRead, g.CanCreate, g.CanUpdate, g.CanDelete}
	}
	if isQueryDef(def) {
		p.Create, p.Update, p.Delete = false, false, false
	}
	return p
}
```
Update both call sites: `handleTableList` → `s.tablePerms(u, &list[i])`; `handleTableGet` → `s.tablePerms(u, def)`.
6. `validateDef` — restructure the head and add per-column query rules:
```go
	query := isQueryDef(def)
	if def.DatasourceID == 0 || def.Label == "" {
		return "datasourceId and label are required"
	}
	if query {
		if msg := checkQuerySQL(def.QuerySQL); msg != "" {
			return msg
		}
	} else if def.SchemaName == "" || def.TableName == "" || len(def.KeyColumns) == 0 {
		return "datasourceId, schemaName, tableName, label, keyColumns are required"
	}
	if def.PageSize <= 0 || def.PageSize > 200 {
		return "pageSize must be 1..200"
	}
	if !query {
		for _, name := range append([]string{def.SchemaName, def.TableName}, def.KeyColumns...) {
			if _, err := ds.QuoteIdent(name); err != nil {
				return "invalid identifier: " + name
			}
		}
	}
```
Inside the column loop, after the `validFieldTypes` check add:
```go
		if query && (c.FieldType == "fk" || c.FieldType == "m2m") {
			return "column " + c.Name + ": query views cannot use fk or m2m columns"
		}
		if query && len(c.Validations) > 0 {
			return "column " + c.Name + ": query views cannot define validation rules"
		}
```
After the loop, replace the "at least one key column" enforcement so it only applies to table defs (the existing `keySeen` loop over `def.KeyColumns` already tolerates an empty slice — remove any early error that required non-empty keys for query defs). Add:
```go
	if query && def.Hooks != "" {
		return "query views cannot assign hooks"
	}
```
and skip `s.checkHooks(def)` when `query`.
7. EXPLAIN on save — new helper + two call sites:
```go
func (s *Server) explainQueryDef(w http.ResponseWriter, def *meta.TableDef) bool {
	if !isQueryDef(def) {
		return false
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return true
	}
	defer a.Close()
	if err := a.ExplainQuery(def.QuerySQL); err != nil {
		writeErr(w, 400, "QUERY_INVALID", "query failed validation", err.Error())
		return true
	}
	return false
}
```
In `handleTableCreate` and `handleTableUpdate`, immediately after `validateDef` passes: `if s.explainQueryDef(w, def) { return }`.
8. Write guard helper:
```go
func writeQueryReadOnly(w http.ResponseWriter, def *meta.TableDef) bool {
	if isQueryDef(def) {
		writeErr(w, 403, "QUERY_READONLY", "query views are read-only", nil)
		return true
	}
	return false
}
```

Insert `if writeQueryReadOnly(w, def) { return }` immediately after the `tableCtx` block in: `rows.go` `handleRowCreate`/`handleRowUpdate`/`handleRowDelete`; `bulk.go` `handleRowBulkDelete`; `rels.go` `handleFKOptions`/`handleM2MOptions`/`handleM2MLinks`.

`import.go` — in `importCtx`, after `tableCtx` succeeds and before the perm check:
```go
	if writeQueryReadOnly(w, def) {
		return nil, nil, false
	}
```

`rows.go` `handleRowGet` — after `tableCtx`:
```go
	if isQueryDef(def) && len(def.KeyColumns) == 0 {
		writeErr(w, 400, "QUERY_NO_KEY", "this query view has no key columns", nil)
		return
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestQueryDef' -v && go test ./internal/api/`
Expected: new tests PASS; existing suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat(api): query-view definitions, validation, write guards, perm lock"
```

---

### Task 6: API — read path (list/get/export) + resolveSort fix

**Files:**
- Modify: `internal/api/rows.go` (resolveSort, handleRowList, handleRowGet)
- Modify: `internal/api/export.go` (handleRowExport)
- Test: `internal/api/query_rows_pg_test.go` (new)

**Interfaces:**
- Consumes: Tasks 2–5 (`ListQueryRows`, `CountQueryRows`, `QueryParams`, `IsQueryTimeout`).
- Produces: `writeQueryErr(w, err)` in rows.go (also used by Task 7/8).

- [ ] **Step 1: Write the failing live test**

Create `internal/api/query_rows_pg_test.go`:

```go
package api

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func seedQueryLive(t *testing.T, s *Server) string {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	seedLive(t, s) // ds id 2 "live" + customers fixture (def 1)
	def := &meta.TableDef{DatasourceID: 2, SourceType: "query",
		QuerySQL: "SELECT name AS customer_name, balance FROM customers",
		Label: "Customer names", KeyColumns: []string{"customer_name"}, PageSize: 2}
	cols := []meta.ColumnDef{
		{Name: "customer_name", Label: "Name", FieldType: "text", Visible: true,
			Searchable: true, Sortable: true, Position: 0},
		{Name: "balance", Label: "Balance", FieldType: "number", Visible: true,
			Sortable: true, Position: 1},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	return tdTok(s, def.ID)
}

func TestQueryRowsListFilterSort(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedQueryLive(t, s)

	w := do(s, "GET", "/api/tables/"+tok+"/rows", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var res struct {
		Total int              `json:"total"`
		Rows  []map[string]any `json:"rows"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 3 || len(res.Rows) != 2 { // pageSize 2
		t.Fatalf("list total=%d rows=%d", res.Total, len(res.Rows))
	}

	f := url.QueryEscape(`[{"column":"customer_name","op":"contains","values":["jo"]}]`)
	w = do(s, "GET", "/api/tables/"+tok+"/rows?filters="+f+"&sort=balance&dir=DESC", "", c)
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 2 || res.Rows[0]["customer_name"] != "joe" {
		t.Fatalf("filter+sort = total %d rows %+v", res.Total, res.Rows)
	}

	w = do(s, "GET", "/api/tables/"+tok+"/rows?search=ana", "", c)
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 1 {
		t.Fatalf("search = %d", res.Total)
	}
}

func TestQueryRowsGetAndExport(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedQueryLive(t, s)

	pk := rowKeyToken([]string{"jo"})
	w := do(s, "GET", "/api/tables/"+tok+"/rows/"+pk, "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "customer_name") {
		t.Fatalf("row get = %d %s", w.Code, w.Body)
	}

	body, _ := exportCSV(t, s, tok, "", *c)
	if lines := strings.Split(strings.TrimSpace(body), "\n"); len(lines) != 4 {
		t.Fatalf("export lines = %d", len(lines))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/api/ -run TestQueryRows -v`
Expected: FAIL (list hits `InspectTable`-era table path against empty schema/table).

- [ ] **Step 3: Implement**

`rows.go`:

1. `resolveSort` — make keyless defs safe; replace the final `return def.KeyColumns[0], "ASC"` with:
```go
	if len(def.KeyColumns) > 0 {
		return def.KeyColumns[0], "ASC"
	}
	for _, c := range cols {
		if c.Sortable && c.FieldType != "m2m" && !c.IsComputed {
			return c.Name, "ASC"
		}
	}
	return "", ""
```
2. Error mapper (place near `writeDefErr`):
```go
func writeQueryErr(w http.ResponseWriter, err error) {
	if ds.IsQueryTimeout(err) {
		writeErr(w, 502, "QUERY_TIMEOUT", "query exceeded the execution time limit", nil)
		return
	}
	writeErr(w, 502, "CONN", "query failed", err.Error())
}
```
3. `handleRowList` — after `a, err := s.liveAdapter(...)` / `defer a.Close()`, wrap the existing query execution in a branch. Replace the block from `q := r.URL.Query()` through the `CountRows` error check with:
```go
	q := r.URL.Query()
	sortCol, sortDir := resolveSort(def, cols, q.Get("sort"), q.Get("dir"))
	filters, fmsg := s.parseFilters(def, cols, u, q.Get("filters"))
	if fmsg != "" {
		writeErr(w, 400, "FILTER_INVALID", fmsg, nil)
		return
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	names := realColNames(cols)
	var rows []map[string]any
	var total int
	if isQueryDef(def) {
		if sortCol == "" {
			writeErr(w, 400, "VALIDATION", "query view has no sortable column", nil)
			return
		}
		qp := ds.QueryParams{Query: def.QuerySQL, Columns: names,
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir, Filters: filters,
			Limit: def.PageSize, Offset: (page - 1) * def.PageSize}
		rows, err = a.ListQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
		total, err = a.CountQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
	} else {
		lp := ds.ListParams{Schema: def.SchemaName, Table: def.TableName, Columns: names,
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir,
			Filters: filters,
			Limit:   def.PageSize, Offset: (page - 1) * def.PageSize}
		rows, err = a.ListRows(lp)
		if err != nil {
			writeErr(w, 502, "CONN", "query failed", err.Error())
			return
		}
		total, err = a.CountRows(lp)
		if err != nil {
			writeErr(w, 502, "CONN", "count failed", err.Error())
			return
		}
	}
```
(keep the following `applyComputed`/`buildRels`/`buildM2MRels`/`writeJSON` lines unchanged — `rels.go` helpers already no-op without fk/m2m columns).
4. `handleRowGet` — after `rowKeyVals` succeeds, branch before `FetchByKey`:
```go
	var row map[string]any
	if isQueryDef(def) {
		kf := make([]ds.ColumnFilter, len(keyVals))
		for i, k := range def.KeyColumns {
			kf[i] = ds.ColumnFilter{Column: k, Op: "eq", Values: []any{keyVals[i]}}
		}
		qp := ds.QueryParams{Query: def.QuerySQL, Columns: names,
			SortCol: def.KeyColumns[0], SortDir: "ASC", Filters: kf, Limit: 1}
		rowsOut, err := a.ListQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
		if len(rowsOut) == 0 {
			writeErr(w, 404, "NOT_FOUND", "row not found", nil)
			return
		}
		row = rowsOut[0]
	} else {
		rowsOut, err := a.FetchByKey(def.SchemaName, def.TableName, def.KeyColumns, keyVals, names)
		if err != nil {
			writeErr(w, 502, "CONN", "query failed", err.Error())
			return
		}
		if len(rowsOut) == 0 {
			writeErr(w, 404, "NOT_FOUND", "row not found", nil)
			return
		}
		row = rowsOut[0]
	}
```
and replace the trailing uses of `rowsOut[0]`/`row` accordingly (existing code: `applyComputed(cols, []map[string]any{row})`, `buildRels`, `writeJSON`).
5. `export.go` `handleRowExport` — same pattern: after `resolveSort`/`parseFilters`, branch:
```go
	var total int
	var rows []map[string]any
	if isQueryDef(def) {
		if sortCol == "" {
			writeErr(w, 400, "VALIDATION", "query view has no sortable column", nil)
			return
		}
		qp := ds.QueryParams{Query: def.QuerySQL, Columns: realColNames(cols),
			Searchable: searchable, Search: q.Get("search"),
			SortCol: sortCol, SortDir: sortDir, Filters: filters,
			Limit: exportRowCap + 1, Offset: 0}
		total, err = a.CountQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
		if total > exportRowCap {
			writeErr(w, 400, "EXPORT_TOO_LARGE",
				fmt.Sprintf("export is limited to %d rows; this query matches %d — narrow the search", exportRowCap, total), nil)
			return
		}
		rows, err = a.ListQueryRows(qp)
		if err != nil {
			writeQueryErr(w, err)
			return
		}
	} else {
		lp := ds.ListParams{...existing...}
		...existing CountRows/ListRows with their error returns...
	}
```
(keep everything from `applyComputed(cols, rows)` down unchanged).

- [ ] **Step 4: Run tests**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/api/ -run TestQueryRows -v && go test ./internal/api/`
Expected: PASS + full suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/api/rows.go internal/api/export.go internal/api/query_rows_pg_test.go
git commit -m "feat(api): query-view read path (list/get/export) with timeout mapping"
```

---

### Task 7: API — verify/resync for query defs

**Files:**
- Modify: `internal/api/tables.go` (`handleVerify`, `handleResync`)
- Test: `internal/api/query_verify_pg_test.go` (new)

**Interfaces:**
- Consumes: `IntrospectQuery`, `ExplainQuery`, existing `CompareDrift`/`ReplaceColumns`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/query_verify_pg_test.go`:

```go
package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestQueryVerifyResync(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedQueryLive(t, s)

	// 1. verify passes on the seeded query
	w := do(s, "GET", "/api/tables/"+tok+"/verify", "", c)
	if w.Code != 200 {
		t.Fatalf("verify = %d %s", w.Code, w.Body)
	}

	// 2. break the stored query → verify 502 (EXPLAIN/plan fails)
	def, cols, _ := s.store.GetTableDef(2)
	def.QuerySQL = "SELECT nope FROM customers"
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	w = do(s, "GET", "/api/tables/"+tok+"/verify", "", c)
	if w.Code != 502 {
		t.Fatalf("broken verify = %d %s", w.Code, w.Body)
	}

	// 3. drift: query adds a column → resync appends it
	def.QuerySQL = "SELECT name AS customer_name, balance, status FROM customers"
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	w = do(s, "GET", "/api/tables/"+tok+"/verify", "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "DRIFT") {
		t.Fatalf("drift verify = %d %s", w.Code, w.Body)
	}
	w = do(s, "POST", "/api/tables/"+tok+"/resync", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "status") {
		t.Fatalf("resync = %d %s", w.Code, w.Body)
	}
	_, fresh, _ := s.store.GetTableDef(2)
	found := false
	for _, fc := range fresh {
		if fc.Name == "status" {
			found = true
		}
	}
	if !found {
		t.Fatal("resync did not append status column")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/api/ -run TestQueryVerifyResync -v`
Expected: FAIL (verify runs `InspectTable("", "")`).

- [ ] **Step 3: Implement**

`handleVerify` — replace the `live, err := db.InspectTable(...)` block:
```go
	var live []ds.LiveColumn
	if isQueryDef(def) {
		if err := db.ExplainQuery(def.QuerySQL); err != nil {
			writeErr(w, 502, "CONN", "query validation failed", err.Error())
			return
		}
		live, _, err = db.IntrospectQuery(def.QuerySQL)
	} else {
		live, err = db.InspectTable(def.SchemaName, def.TableName)
	}
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
```
`handleResync` — same replacement for its `InspectTable` block (without the ExplainQuery pre-step: `IntrospectQuery` alone covers it).

- [ ] **Step 4: Run tests**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/api/ -run TestQueryVerifyResync -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/tables.go internal/api/query_verify_pg_test.go
git commit -m "feat(api): drift verify/resync for query views"
```

---

### Task 8: API — query-introspect endpoint

**Files:**
- Modify: `internal/api/server.go` (route)
- Modify: `internal/api/datasources.go` (handler)
- Test: `internal/api/query_introspect_pg_test.go` (new)

**Interfaces:**
- Consumes: `ExplainQuery`, `IntrospectQuery`.
- Produces: `POST /api/datasources/{id}/query-introspect` → `{"columns": LiveColumn[], "dropped": string[]}`; also used by the frontend (Task 10).

- [ ] **Step 1: Write the failing test**

Create `internal/api/query_introspect_pg_test.go`:

```go
package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestQueryIntrospect(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s) // ds id 2 = live
	dsTok := s.ids.Encode("ds", 2)

	w := do(s, "POST", "/api/datasources/"+dsTok+"/query-introspect",
		`{"query":"SELECT name AS n, balance, 1+1 FROM customers"}`, c)
	if w.Code != 200 {
		t.Fatalf("introspect = %d %s", w.Code, w.Body)
	}
	var res struct {
		Columns []struct {
			Name     string `json:"name"`
			FieldType string `json:"fieldType"`
		} `json:"columns"`
		Dropped []string `json:"dropped"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Columns) != 2 || res.Columns[0].Name != "n" || len(res.Dropped) != 1 {
		t.Fatalf("res = %s", w.Body)
	}

	w = do(s, "POST", "/api/datasources/"+dsTok+"/query-introspect",
		`{"query":"DELETE FROM customers"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "QUERY_INVALID") {
		t.Fatalf("bad query = %d %s", w.Code, w.Body)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/api/ -run TestQueryIntrospect -v`
Expected: FAIL (404 — route absent).

- [ ] **Step 3: Implement**

`internal/api/server.go` — after the `handleDSColumns` route:
```go
	mux.HandleFunc("POST /api/datasources/{id}/query-introspect", s.RequireDSManage(s.handleDSQueryIntrospect))
```
`internal/api/datasources.go` — append:
```go
func (s *Server) handleDSQueryIntrospect(w http.ResponseWriter, r *http.Request) {
	id, err := s.dsCtx(r)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	var in struct {
		Query string `json:"query"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if msg := checkQuerySQL(in.Query); msg != "" {
		writeErr(w, 400, "QUERY_INVALID", msg, nil)
		return
	}
	d, err := s.store.GetDatasource(id)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	a, err := ds.Open(*d)
	if err != nil {
		writeErr(w, 502, "CONN", "connection failed", err.Error())
		return
	}
	defer a.Close()
	if err := a.ExplainQuery(in.Query); err != nil {
		writeErr(w, 400, "QUERY_INVALID", "query failed validation", err.Error())
		return
	}
	cols, dropped, err := a.IntrospectQuery(in.Query)
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	if cols == nil {
		cols = []ds.LiveColumn{}
	}
	if dropped == nil {
		dropped = []string{}
	}
	writeJSON(w, 200, map[string]any{"columns": cols, "dropped": dropped})
}
```

- [ ] **Step 4: Run tests**

Run: `KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" go test ./internal/api/ -run TestQueryIntrospect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/datasources.go internal/api/query_introspect_pg_test.go
git commit -m "feat(api): query-introspect endpoint for the view wizard"
```

---

### Task 9: Meta transfer — export/import of query defs

**Files:**
- Modify: `internal/api/metatransfer.go` (file format, export, tblEqual, buildImportPlan)
- Test: `internal/api/query_transfer_test.go` (new)

**Interfaces:**
- Consumes: Task 1 (`SourceType`/`QuerySQL` persisted), Task 5 (`checkQuerySQL`).

- [ ] **Step 1: Write the failing test**

Create `internal/api/query_transfer_test.go`:

```go
package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestMetaTransferQueryDef(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedQueryDef(t, []string{"n"}) // ds "dead" id 1 + query def id 1

	w := do(s, "GET", "/api/meta/export", "", c)
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	var file struct {
		Tables []struct {
			SourceType string `json:"sourceType"`
			QuerySQL   string `json:"querySql"`
		} `json:"tables"`
	}
	json.Unmarshal(w.Body.Bytes(), &file)
	if len(file.Tables) != 1 || file.Tables[0].SourceType != "query" ||
		file.Tables[0].QuerySQL != "SELECT name AS n FROM customers" {
		t.Fatalf("export tables = %s", w.Body)
	}

	// import into a second instance against the same local ds name
	s2 := newTestServer(t)
	c2 := login(s2)
	if err := s2.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	bb, _ := json.Marshal(map[string]any{
		"format": "ku-crud-meta", "version": 1,
		"groups": []string{}, "datasources": []map[string]any{},
		"tables": []map[string]any{{
			"datasourceRef": "dead", "schema": "", "table": "", "label": "Q",
			"keyColumns": []string{"n"}, "pageSize": 20, "sourceType": "query",
			"querySql": "SELECT name AS n FROM customers",
			"columns": []map[string]any{{"name": "n", "label": "N", "fieldType": "text",
				"editable": false, "required": false, "visible": true, "searchable": true,
				"sortable": true, "position": 0}},
		}}})
	w = do(s2, "POST", "/api/meta/import/apply?selections="+
		strings.NewReader("").String(), "", c2) // placeholder — replaced below
	// use multipart: build via buf
	w = postMultipart(s2, "/api/meta/import/apply", map[string]string{"file": string(bb)},
		`{"datasources":[],"tables":[{"ref":"dead//Q","mode":"skip"}],"groups":false}`, c2)
	if w.Code != 400 { // "skip" for a NEW table is invalid → plan rejection proves parsing worked
		t.Fatalf("apply = %d %s", w.Code, w.Body)
	}
	w = postMultipart(s2, "/api/meta/import/apply", map[string]string{"file": string(bb)},
		`{"datasources":[],"tables":[{"ref":"dead//Q","mode":"overwrite"}],"groups":false}`, c2)
	// ref of a query def uses empty schema/table — the natural key is
	// "ds/<schema>/<table>"; assert the stored def after a correct apply:
	if w.Code == 200 {
		def, _, _ := s2.store.GetTableDef(1)
		if def == nil || def.SourceType != "query" || def.QuerySQL == "" {
			t.Fatalf("imported def = %+v", def)
		}
	}
}
```

The ref-key problem: a query def has empty schema/table, so `tableRef` = `"dead//Q"`... in fact the ref is `ds + "/" + schema + "/" + table` = `"dead//"`. Two query defs on one datasource would collide. Fix in this task: give query defs a synthetic ref — extend `tableRef` usage with the label for query defs. Implement helper in `metatransfer.go`:

```go
func defRef(dsName string, ft metaFileTable) string {
	if ft.SourceType == "query" {
		return dsName + "/query/" + ft.Label
	}
	return tableRef(dsName, ft.Schema, ft.Table)
}
```

and use `defRef` wherever `tableRef(ft.DatasourceRef, ft.Schema, ft.Table)` is computed for file tables (diffMeta loop, buildImportPlan `fileTables` map + loop, dependency refs stay `tableRef` — fk/m2m are rejected on query defs anyway). Local-side refs: query defs resolve via `tableRef(dsName[d.DatasourceID], d.SchemaName, d.TableName)` = `"dead//"` — must become `defRef`-compatible too; add the same branch there using `d.Label`.

Simplify the test accordingly: the selection ref is `"dead/query/Q"` for both apply attempts, first with `"mode":"skip"` (rejected: skip on new table is invalid — verify current behavior; if skip on new is accepted silently, drop the first assertion and assert only the successful apply). Replace the multipart helper: if `postMultipart` doesn't exist in the test package, write it:

```go
func postMultipart(s *Server, path string, fields map[string]string, selections string, cookie *string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		fw, _ := mw.CreateFormFile(k, "f.json")
		fw.Write([]byte(v))
	}
	mw.WriteField("selections", selections)
	mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil && *cookie != "" {
		req.Header.Set("Cookie", *cookie)
	}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	return w
}
```

(Check `metatransfer_test.go` first — reuse its existing multipart helper if one exists.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestMetaTransferQueryDef -v`
Expected: FAIL — exported tables carry no `sourceType`/`querySql`.

- [ ] **Step 3: Implement**

`internal/api/metatransfer.go`:
1. `metaFileTable` — add after `GroupRef`:
```go
	SourceType string `json:"sourceType,omitempty"`
	QuerySQL   string `json:"querySql,omitempty"`
```
2. `buildMetaFile` — set `SourceType: d.SourceType, QuerySQL: d.QuerySQL` in the `metaFileTable{...}` literal, and use `defRef`-style refs: change the `ft := metaFileTable{DatasourceRef: ...}` construction site plus `refOf` to emit `ds/query/<label>` refs for query defs (same branch as the helper above).
3. `tblEqual` — add to the first comparison chain: `ft.SourceType != def.SourceType || ft.QuerySQL != def.QuerySQL ||` → `return false`.
4. `buildImportPlan` — in the per-table loop after `validateBundleTable(ft)`:
```go
		if ft.SourceType == "query" {
			if msg := checkQuerySQL(ft.QuerySQL); msg != "" {
				return nil, msg, nil
			}
			for _, fc := range ft.Columns {
				if fc.FieldType == "fk" || fc.FieldType == "m2m" {
					return nil, "query view " + ref + " cannot use fk or m2m columns", nil
				}
			}
		}
```
and carry the fields into the plan: `pd := meta.PlannedDef{DsName: ..., Def: meta.TableDef{..., SourceType: ft.SourceType, QuerySQL: ft.QuerySQL}}` (add both to the `meta.TableDef` literal). For table defs set `SourceType: "table"` explicitly.
5. Ref keys — apply the `defRef` branch (helper above) at: `fileTables` map construction, the `for _, ft := range f.Tables` loops in `diffMeta` and `buildImportPlan`, the local-index construction (`localTables`/`localRefs`) using the def's own fields, and `buildMetaFile`'s `refOf`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run TestMetaTransferQueryDef -v && go test ./internal/api/`
Expected: PASS + suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/api/metatransfer.go internal/api/query_transfer_test.go
git commit -m "feat(api): meta transfer carries query views with label-based refs"
```

---

### Task 10: Frontend — wizard source toggle, read-only grid via permissions, i18n

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/pages/TableForm.tsx`
- Modify: `web/src/lib/i18n/en.ts`, `web/src/lib/i18n/id.ts` (create id.ts if absent)

**Interfaces:**
- Consumes: Task 8 endpoint `POST /api/datasources/{id}/query-introspect`; Task 5 DTO fields `sourceType`/`querySql` and zeroed write permissions (the Data page already hides write actions from `permissions`, so `Data.tsx` needs no change).

- [ ] **Step 1: types + api client**

`web/src/lib/types.ts` — add to `TableDef` (after `description?: string;`):
```ts
  sourceType?: "table" | "query";
  querySql?: string;
```
Add near `LiveColumn` (or with the other shared types):
```ts
export interface QueryIntrospectResult {
  columns: { name: string; fieldType: FieldType; nullable: boolean; isPk?: boolean; enumOptions?: string[] | null }[];
  dropped: string[];
}
```

`web/src/lib/api.ts` — append:
```ts
export function introspectQuery(dsId: string, query: string) {
  return api<import("./types").QueryIntrospectResult>(
    `/datasources/${dsId}/query-introspect`,
    { method: "POST", body: JSON.stringify({ query }) },
  );
}
```

- [ ] **Step 2: TableForm — source toggle + query editor**

`web/src/pages/TableForm.tsx`:
1. Add state next to the existing `useState` block (after `const [tableName, setTableName] = useState("")`):
```tsx
  const [sourceType, setSourceType] = useState<"table" | "query">("table");
  const [querySql, setQuerySql] = useState("");
  const [droppedCols, setDroppedCols] = useState<string[]>([]);
```
2. Editing populate — inside the `useEffect` that reads `existingDef.data`, after `setTableName(d.tableName);`:
```tsx
      setSourceType(d.sourceType === "query" ? "query" : "table");
      setQuerySql(d.querySql ?? "");
```
3. Introspection — add after the `liveCols` query definition:
```tsx
  const queryIntrospect = useMutation({
    mutationFn: () => introspectQuery(dsId, querySql),
    onSuccess: (res) => {
      setDroppedCols(res.dropped);
      setKeys((prev) => (prev.length ? prev : []));
      setLabel((l) => l || "Query view");
      setCols(
        res.columns.map((c, i) => ({
          name: c.name,
          label: normalizeLabel(c.name),
          fieldType: c.fieldType,
          enumOptions: c.enumOptions ?? undefined,
          editable: false,
          required: false,
          visible: true,
          searchable: true,
          sortable: true,
          position: i,
        })),
      );
    },
  });
```
4. UI — in the datasource-selection step, above the schema/table pickers:
```tsx
            <div className="flex gap-2">
              {(["table", "query"] as const).map((st) => (
                <button key={st} type="button"
                  onClick={() => setSourceType(st)}
                  className={sourceType === st ? "btn btn-primary" : "btn"}>
                  {st === "table" ? t("tables.sourceTable") : t("tables.sourceQuery")}
                </button>
              ))}
            </div>
```
When `sourceType === "query"`, hide the schema/table pickers and render instead:
```tsx
            <textarea value={querySql} onChange={(e) => setQuerySql(e.target.value)}
              rows={8} className="w-full font-mono text-sm" placeholder="SELECT …" />
            <button type="button" className="btn"
              disabled={!dsId || !querySql.trim()}
              onClick={() => queryIntrospect.mutate()}>
              {t("tables.validateQuery")}
            </button>
            {queryIntrospect.isError && <ErrorBox error={queryIntrospect.error} />}
            {droppedCols.length > 0 && (
              <p className="text-amber-600 text-sm">
                {t("tables.droppedCols")}: {droppedCols.join(", ")}
              </p>
            )}
```
5. Submit payload — in the request body object sent to `POST/PUT /api/tables`, add:
```ts
        sourceType,
        querySql: sourceType === "query" ? querySql : "",
        ...(sourceType === "query" ? { schemaName: "", tableName: "" } : {}),
```
and keep `keyColumns: keys` (empty for query views is valid). Follow the house `className` conventions visible in the file (adjust `btn`/`btn-primary` names to what the file already uses).
6. Column editor step — when `sourceType === "query"`, hide the fk/m2m/hook/validation column affordances (leave name/label/visibility/search/sort/formatting and computed columns). Gate each relevant section with `{sourceType === "table" && (...)}`.

- [ ] **Step 3: i18n keys**

In `web/src/lib/i18n/en.ts` add (following the file's existing nesting under `tables:` and `errors:`):
```ts
  tables: {
    // …existing keys…
    sourceTable: "Physical table",
    sourceQuery: "SQL query",
    validateQuery: "Validate & preview columns",
    droppedCols: "Columns needing an alias (skipped)",
  },
  errors: {
    // …existing keys…
    QUERY_INVALID: "The SQL query is invalid: {{msg}}",
    QUERY_READONLY: "This view is read-only — it is built from a SQL query.",
    QUERY_NO_KEY: "This view has no key columns, so single rows cannot be opened.",
    QUERY_TIMEOUT: "The query took too long and was stopped. Try a simpler query.",
  },
```
Mirror the four error keys and the four `tables.*` labels in Indonesian in `id.ts`:
```ts
    sourceTable: "Tabel fisik",
    sourceQuery: "Query SQL",
    validateQuery: "Validasi & pratinjau kolom",
    droppedCols: "Kolom tanpa alias (dilewati)",
    QUERY_INVALID: "Query SQL tidak valid: {{msg}}",
    QUERY_READONLY: "View ini hanya-baca — dibangun dari query SQL.",
    QUERY_NO_KEY: "View ini tidak punya kolom kunci, baris tunggal tidak bisa dibuka.",
    QUERY_TIMEOUT: "Query terlalu lama dan dihentikan. Coba query yang lebih sederhana.",
```

- [ ] **Step 4: Build & typecheck**

Run: `cd web && npm run build` (or `npx tsc --noEmit` + the project's build script)
Expected: clean build, new bundle emitted.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -m "feat(web): query-view wizard mode, read-only grid, error i18n"
```

---

### Task 11: Final verification + docs

**Files:**
- Modify: `README.md` (feature list, v1.8 entry)
- Modify: `docs/superpowers/specs/2026-08-22-v1.8-query-views-design.md` (Status → Implemented)

- [ ] **Step 1: Full test suite**

Run: `go test ./... && go vet ./...`
Expected: all PASS (live tests skip without envs).

- [ ] **Step 2: Live suite (if DBs up)**

Run (docker compose up -d first):
```
KUCRUD_TEST_PG="host=localhost port=5433 user=ku password=ku dbname=ku sslmode=disable" \
KUCRUD_TEST_MYSQL="ku:ku@tcp(localhost:3307)/ku" \
go test ./internal/ds/ ./internal/api/ -v
```
Expected: no failures; the query-view live tests PASS.

- [ ] **Step 3: README**

Add item 16 to the numbered feature list in `README.md` (match the existing entry style):

```markdown
16. **Query views (v1.8)** — a table definition can be backed by a raw SQL
    SELECT (`sourceType: "query"`) instead of a physical table. Results are
    read-only grids with the full pipeline: search, sort, per-column filters,
    pagination, saved filters and CSV export. Execution is guarded in depth:
    EXPLAIN-validated single SELECT, read-only transaction, 15 s statement
    timeout and row caps; all write endpoints answer 403 `QUERY_READONLY`.
```

- [ ] **Step 4: Spec status**

In `docs/superpowers/specs/2026-08-22-v1.8-query-views-design.md` change the `Status:` line to `Status: Implemented (see docs/superpowers/plans/2026-08-22-query-views.md)`.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/superpowers/specs/2026-08-22-v1.8-query-views-design.md
git commit -m "docs: query views (v1.8) shipped"
```

---

## Self-Review Notes (resolved during planning)

- **Spec coverage:** §3 data model → Task 1; §5 guards → Tasks 2–4 (plus EXPLAIN-on-save in Task 5); §6 execution → Tasks 2–3; §7 API table (each endpoint row) → Tasks 5–8; §8 RBAC → Task 5 (`tablePerms` lock, `RequireDSManage` on introspect); §9 meta transfer → Task 9; §10 frontend → Task 10 (Data.tsx needs no change: write buttons already gate on the `permissions` object, which the backend now zeroes for query defs — and the 403s back-stop any missed button); §11 testing → Tasks 1–9 test steps; §12 rollout → Task 1 migration + Task 11 docs.
- **Known deliberate limitation (documented in Task 3 comments):** MySQL reports TEXT result columns as "BLOB" through `ColumnTypes`, so MySQL query views surface text only via CHAR/VARCHAR-typed expressions (`CAST(x AS CHAR) AS alias`); dropped columns are reported by name in the wizard.
- **Type consistency:** `IntrospectQuery` returns `(cols, dropped, err)` everywhere; `tablePerms(u, def)` signature change applied at both call sites; `QueryParams` field names match builders.
