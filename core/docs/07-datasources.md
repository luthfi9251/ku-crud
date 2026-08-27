# 07 — Datasources

The `ds` package is the dialect-neutral data access layer: one `Conn` description, one
`Open` factory, one `Adapter` interface — SQL generation and execution live inside the
adapters. Handlers and engine services import only the interface.

## Connections

```go
type Conn struct {
    Driver, Host                string // "" / "postgres" / "mysql"
    Port                        int
    DB, User, Password, SSLMode string
    Raw                         string // verbatim DSN; overrides every other field
}

func Open(c Conn) (Adapter, error)
```

- Driver inferred from the `Raw` DSN scheme when empty: `postgres://` /
  `postgresql://` → postgres, `mysql://` → mysql.
- Postgres via `jackc/pgx/v5/stdlib` (pooled, `ConnMaxLifetime` 5 min); MySQL via
  `go-sql-driver/mysql`.
- Connections are lazy and per-use: callers open on demand and `Close` when done. The
  `App` internally shares one pooled adapter (its close is a no-op) closed by
  `App.Close()`.

## Dialect differences worth knowing

| | postgres | mysql |
|---|---|---|
| Identifiers | `"col"` | `` `col` `` |
| Placeholders | `$1, $2, …` | `?` |
| Case-insensitive search | `col::text ILIKE '%x%'` | `CAST(col AS CHAR) LIKE '%x%'` |
| Booleans | native | int64 0/1 coerced to bool for boolean columns |
| Numeric aggregates | may arrive as numeric strings | often `[]byte` → string |

The coercion and string→number normalization happen inside `ds`/`engine` — consumers
see typed values. Search input escapes `%`/`_` (literal match semantics) and rides
bind parameters everywhere.

## The Adapter interface

The complete contract (23 methods). A new adapter — SQL or not — implements this and
registers in `Open`; nothing else in the codebase changes:

```go
type Adapter interface {
    Ping() error
    ListTables() ([]TableInfo, error)
    InspectTable(schema, table string) ([]LiveColumn, error)

    // Query views: read-only SQL-backed definitions. Execution runs inside
    // a read-only transaction with QueryTimeout applied.
    ExplainQuery(query string) error
    IntrospectQuery(query string) ([]LiveColumn, []string, error)
    ListQueryRows(p QueryParams) ([]map[string]any, error)
    CountQueryRows(p QueryParams) (int, error)

    ListRows(p ListParams) ([]map[string]any, error)
    CountRows(p ListParams) (int, error)
    // One single-value aggregate (dashboard cards): Query set = query-view
    // mode (read-only tx + timeout), else table mode.
    AggregateRows(p AggregateParams) (*AggregateResult, error)
    FetchByKey(schema, table string, keyCols []string, keyVals []any, cols []string) ([]map[string]any, error)

    Insert(schema, table string, cols []string, vals []any) error
    UpdateByKey(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (int64, error)
    DeleteByKey(schema, table string, keyCols []string, keyVals []any) (int64, error)

    FetchByRefValues(schema, table, refCol string, displayCols []string, vals []any) (map[string]map[string]any, error)
    CountByRefEq(schema, table, col string, val any) (int, error)

    // Junction link primitives for many-to-many relations.
    FetchPairsByRef(schema, table, col, retCol string, vals []any) ([]Pair, error)
    DeletePairs(schema, table, col1 string, val1 any, col2 string, val2 any) (int64, error)

    IsFKViolation(err error) bool
    Close() error
}
```

Parameter/result types:

```go
type ListParams struct {        // one page-list request
    Schema, Table    string
    Columns          []string
    Searchable       []string
    Search           string
    SortCol, SortDir string
    Limit, Offset    int
    Filters          []ColumnFilter
}

type QueryParams struct {       // same, over a stored query view
    Query      string
    Columns, Searchable []string
    Search     string
    SortCol, SortDir string
    Limit, Offset int
    Filters    []ColumnFilter
}

type AggregateParams struct {   // one single-value aggregate
    Schema, Table string   // table mode
    Query         string   // query-view mode (Schema/Table ignored)
    Func          string   // count|sum|avg|min|max
    Column        string   // required for sum/avg/min/max; empty for count
    Filters       []ColumnFilter
}

type AggregateResult struct {
    Value   any  // nil when the SQL aggregate returned NULL (empty set)
    HasRows bool // sidecar COUNT(*): was the filtered set non-empty
}

type ColumnFilter struct {
    Column string      // definition column name (validated upstream)
    Op     string      // eq|neq|gt|gte|lt|lte|between|in|contains
    Values []any       // coerced: float64 (number), bool (boolean), string otherwise
    Join   *FKJoin     // set only for fk display-value filters
}

type FKJoin struct {            // LEFT JOIN target for fk display filtering
    Schema, Table, RefColumn string
    DisplayColumns           []string
}

type LiveColumn struct {        // one introspected column
    Name        string   `json:"name"`
    FieldType   string   `json:"fieldType"`
    Nullable    bool     `json:"nullable"`
    IsPK        bool     `json:"isPk"`
    EnumOptions []string `json:"enumOptions"`
}

type TableInfo struct { Schema string `json:"schema"`; Name string `json:"name"` }
type Pair struct { Col any `json:"col"`; Ret any `json:"ret"` } // one junction link
```

### Generated SQL shapes

All builders live in `sqlkit.go` and share `filterParts`/`searchWhere` — the exact
same WHERE rendering (and injection posture) for lists, counts, exports and
aggregates:

```
list      : SELECT cols FROM schema.tbl [LEFT JOIN fk targets] WHERE search AND filters
            ORDER BY col DIR LIMIT $n OFFSET $n
count     : SELECT COUNT(*) FROM schema.tbl [joins] WHERE ...
aggregate : SELECT AGG(col), COUNT(*) FROM schema.tbl [joins] WHERE ...
query     : SELECT cols FROM (<stored sql>) ku_q WHERE ... ORDER BY ... LIMIT ...
fk filter : LEFT JOIN schema.target "f_<col>" ON "f_<col>"."ref" = base."col"
            AND ("f_<col>"."display" ILIKE $n ESCAPE '\')   -- fk ops: eq | contains
```

Aggregate function names come from an allowlist only (`count|sum|avg|min|max`);
`count` renders `COUNT(*)`; column identifiers re-pass the identifier regex at render
time (defense in depth). FK-join filters are rejected on query views.

## Query-view execution guards

Three layers around stored SQL, applied by the adapters:

1. **`ExplainQuery`** — registration/update time: the statement must `EXPLAIN` as a
   single SELECT (`SELECT 1; DROP TABLE x` and parameterized strings are rejected
   before anything runs).
2. **Read-only session** — Postgres: a read-only transaction with
   `SET LOCAL statement_timeout`; MySQL: a dedicated connection with
   `SET SESSION TRANSACTION READ ONLY` + `MAX_EXECUTION_TIME` (session-scoped — MySQL
   has no per-query read-only flag), both restored before the connection returns.
3. **`QueryTimeout`** — 15 s default (package var, adjustable):

```go
ds.QueryTimeout = 30 * time.Second
```

Driver-level timeouts are recognized by `ds.IsQueryTimeout(err)` (PG `57014`
query_canceled; MySQL `3024`/`1969`) which the engine maps to `502 QUERY_TIMEOUT`.

`IntrospectQuery` returns the output columns (Postgres/MySQL result-set introspection);
**unaliased expression columns are dropped** — `SELECT count(*)` disappears,
`SELECT count(*) AS n` survives. This is the registration-time gate that keeps query
views addressable by stable column names.

## Drift detection

Definitions can fall out of sync with the live schema (a column dropped, a type
changed). `ds.CompareDrift` reports how:

```go
type DriftReport struct {
    Missing     []string `json:"missing"`     // defined but dropped from the live table
    Added       []string `json:"added"`       // live but not defined
    TypeChanged []string `json:"typeChanged"` // same name, different field type
}
func (r DriftReport) Empty() bool
func CompareDrift(defined []defs.Column, live []ds.LiveColumn) DriftReport
func EffectiveType(c defs.Column) string // fk columns compare by their BaseType
```

Virtual columns (m2m, computed) never compare — they have no live counterpart.
Typical host flow (the platform's `verify`/`resync` endpoints): on page load,
`InspectTable` + `CompareDrift` → surface the report → offer a resync that rebuilds
the def from live introspection while preserving host-side config.

Type mapping for new columns comes from `ds.MapFieldType(dataType)` — the same
mapping introspection uses (unknown types map to `""` = excluded).

## Stats / aggregates

`AggregateRows` serves `GET /stats` (see [04 — HTTP API](04-http-api.md)). SQL NULL
semantics are preserved end-to-end: `SUM/AVG/MIN/MAX` over an empty set yields
`Value: nil, HasRows: false`; `COUNT` yields `0, false`. The sidecar `COUNT(*)` in the
same SELECT drives `HasRows` without a second round trip. Numeric aggregates that
drivers return as strings (Postgres `numeric`, MySQL decimals) are normalized —
`[]byte` → string inside `ds`, string → float64 in the engine when the column type is
number — so the JSON response carries real numbers.
