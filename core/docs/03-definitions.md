# 03 — Definitions

A definition (`kucrud.Def` at declaration time, `defs.Table` at runtime) is the
contract between your declaration, the physical table and the served API. This page
covers the declaration surface completely: field types, introspection defaults, the
override merge rules, relations (fk / m2m), query views, computed columns, validation
rules and display formatting.

## The two shapes of a definition

```go
// Declaration (what you write) — kucrud.Def + kucrud.Override
kucrud.Def{
    Table:       "orders",
    Keys:        []string{"id"},
    Columns:     []kucrud.Override{ /* diffs against introspection */ },
    DefaultSort: kucrud.Sort("created_at", kucrud.Desc),
    Hooks:       map[kucrud.Event][]string{ /* see 06 */ },
    Actions:     `{"hidden":["delete"],"custom":[ /* see 06 */ ]}`,
    SourceType:  "table",       // or "query"
    QuerySQL:    "",            // required for query views
    PageSize:    20,            // 1..200, default 20
}

// Runtime (what the engine serves) — defs.Table + defs.Column
type defs.Table struct {
    Name, Label, Description          string
    Schema, PhysTab                   string // physical location
    Keys                              []string
    PageSize                          int
    DefaultSortCol, DefaultSortDir    string
    DefaultView, ViewConfig           string // host-side view config passthrough
    SourceType, QuerySQL              string
    Hooks, Actions                    string // assignment/action JSON
    Columns                           []defs.Column
}
```

## Field types

| FieldType | Values / notes | Filter ops | Sortable | Importable |
|---|---|---|---|---|
| `number` | JSON number (float64/int64) | `eq neq gt gte lt lte between in` | yes | yes |
| `boolean` | JSON bool | `eq` | yes | yes (`true/false/t/f/1/0/yes/no`) |
| `text` | string | `eq neq contains in` | yes | yes |
| `datetime` | RFC3339, `2006-01-02T15:04`, or `2006-01-02` | `eq gt lt between` | yes | yes (ISO-like) |
| `enum` | one of `EnumOptions` | `eq neq in` | yes | yes |
| `uuid` | UUID string | `eq neq contains in` | yes | yes |
| `json` | valid JSON (string or object/array; normalized to compact string) | **not filterable** | yes | yes (compacted) |
| `fk` | physical column upgraded with `FK` metadata; validates as its `BaseType` | `contains eq` (on target display text) | yes | yes (by ref value) |
| `m2m` | virtual column — no storage of its own | not filterable | no | not importable |

## Introspection defaults

For `SourceType: "table"` defs, `App.Resource` introspects the physical table
(Postgres: schema `public` first, then a full scan; MySQL: the connection's current
database) and produces these defaults per column:

- `Label` = humanized name: `[_-]` → space, Title Case (`created_at` → "Created At")
- `Required` = `true` when `NOT NULL` (and `Editable` follows for required columns)
- `Editable` = `true` for nullable non-PK columns; PK columns are not editable
- `Visible`, `Searchable`, `Sortable` = `true`
- `Position` = ordinal position

Type mapping (Postgres data type → field type): `boolean`→boolean;
`smallint/integer/bigint/numeric/real/double precision`→number;
`timestamp(z)/time(z)/date`→datetime; `text/varchar/char`→text; `uuid`→uuid;
`json/jsonb`→json. Everything else (arrays, bytea, unknown types) is **excluded** from
the definition. Postgres enum types introspect to `enum` columns carrying their option
list; MySQL enums likewise.

Keys default to the introspected primary key. **A table without a primary key must
declare `Def.Keys`** — at least one key column is required (keys drive the
update/delete `WHERE` predicate and the opaque row-key encoding; they don't have to be
the real database PK).

## Overrides and the merge rules

`Def.Columns` is a **diff, not a listing**. Each `Override` is matched by `Name`
against the introspected columns — an unknown name is an error (except `M2M`, which
appends a virtual column). Non-zero fields replace the default; zero fields keep it:

```go
type Override struct {
    Name       string   // must match an introspected column (except M2M)
    Label      string   // replaces when non-empty
    Hidden     bool     // true → Visible = false
    Format     string   // formatting JSON, replaces when non-empty (below)
    Validation []defs.ValidationRule
    FK         *defs.FK   // upgrades the column to an fk column
    M2M        *defs.M2M  // appends a virtual m2m column
    Editable   bool
    Required   bool
    Searchable *bool    // pointers: replace when non-nil (can switch a default OFF)
    Sortable   *bool    // pointers: replace when non-nil
}
```

Note the deliberate asymmetry: `Editable`/`Required` are plain bools that can only
*strengthen* a default (`true`), while `Searchable`/`Sortable` are pointers that can
switch a default off:

```go
noSearch, noSort := false, false
def := kucrud.Def{
    Table: "products",
    Columns: []kucrud.Override{
        {Name: "internal_note", Hidden: true},       // visible = false
        {Name: "slug", Required: true},              // force required
        {Name: "huge_text", Searchable: &noSearch},  // search off
        {Name: "trace_id", Sortable: &noSort},       // sort off
    },
}
```

### Formatting JSON (`Format`)

Display-only config carried on the column (never used server-side for values, only
forwarded to clients through the defs listing):

```json
{"number": {"thousands": true, "decimals": 2, "prefix": "Rp "}}
{"enumColors": {"open": "amber", "paid": "green"}}
```

Rules validated at registration: must be valid JSON; `number.decimals` requires a
number column and 0..6; `enumColors` requires an enum column and colors from
{gray, blue, green, amber, red, purple, cyan, orange}.

### Validation rules

Per-column rules applied on the string form of a value (empty/nil skipped —
required-ness is the separate `Required` flag), on every write and CSV import:

| Type | Check | Param |
|---|---|---|
| `email` | `^[^@\s]+@[^@\s]+\.[^@\s]+$` | — |
| `min_len` | rune count ≥ Param | 1..1000 |
| `max_len` | rune count ≤ Param | 1..1000 |
| `number` | `^-?[0-9]+(\.[0-9]+)?$` | — |
| `text` | letters (incl. accented) and spaces only | — |

```go
{Name: "email", Validation: []kucrud.ValidationRule{{Type: "email"}, {Type: "max_len", Param: 254}}}
```

## Foreign keys (fk)

An `FK` override upgrades a physical column into a relation column. The target is
**another registered definition, by name** (`""` = self-reference); `RefColumn` is the
target's key-ish column stored in this column; `DisplayColumns` are what pickers and
grid cells show.

```go
{Name: "customer_id", Label: "Customer",
    FK: &kucrud.FK{
        Table:          "customers",  // def name — must be registered
        RefColumn:      "id",
        DisplayColumns: []string{"name"},
    }},
```

What this unlocks:

- grid responses attach `rels` maps: `rels["customer_id"]["3"] = {"id":3,"name":"Acme"}`
- `GET fkoptions/customer_id` — a searchable, paginated picker over the target
- forms can filter rows by the *display text* of the fk (`contains`/`eq` filter ops,
  rendered as a LEFT JOIN against the target)
- inserts/updates verify the referenced row exists (batch `IN` lookups); a dangling
  reference is a 400, a database FK violation a 409 `CONFLICT`

## Many-to-many (m2m)

An `M2M` override appends a **virtual column** (nothing is stored on this table). The
junction is described by physical column roles:

```go
{Name: "tags", Label: "Tags",
    M2M: &kucrud.M2M{
        JunctionTable:  "product_tags", // physical junction table
        SrcCol:         "product_id",   // junction fk → this table
        TgtCol:         "tag_id",       // junction fk → target table
        DisplayColumns: []string{"label"},
    }},
```

At request time `engine.ResolveM2M` cross-checks the topology against the junction
table's own definition (the junction's `SrcCol` must be an fk referencing this table,
`TgtCol` an fk to the target, the two distinct, no other required junction columns) —
inconsistent configurations surface as clear errors, not wrong SQL. Reads attach
`m2mRels["tags"]["7"] = [{"id":1,"label":"red"}, ...]`; writes accept the column as an
array of target ref values in the JSON body and sync junction rows (insert/delete)
inside the write; junction writes can be additionally gated via `CanWrite`
([05 — Authorization](05-authorization.md)).

## Query views

`SourceType: "query"` turns a definition into a read-only view over arbitrary SQL:

```go
h, _ := app.Resource("open_orders", kucrud.Def{
    SourceType: "query",
    QuerySQL:   `SELECT o.id, o.amount, c.name AS customer
                 FROM orders o JOIN customers c ON c.id = o.customer_id
                 WHERE o.status = 'open'`,
})
```

Rules and guards:

- must start with `SELECT` or `WITH`, ≤ 20 000 chars, validated by `EXPLAIN`
  (single-statement SELECT only) and column-introspected at registration
- unaliased expression columns are dropped — every output column needs a stable alias
- **read-only by construction**: every write route returns `403 QUERY_READONLY` before
  gate logic; no fk/m2m columns or validation rules allowed
- execution wraps the stored SQL as `SELECT ... FROM (<query>) ku_q WHERE ... ORDER BY
  ...` inside a **read-only transaction** with `ds.QueryTimeout` (15 s default);
  timeouts map to `502 QUERY_TIMEOUT`
- search/sort/filters apply to the wrapped query exactly like a table

## Computed columns

A computed column is declared host-side (the platform stores them on the def; the
library evaluates them). It never touches the database — `engine.ApplyComputed`
evaluates in memory on every read:

```go
// defs.Column{Name: "total", FieldType: "number", IsComputed: true,
//            ComputedFormula: `price * qty`}
```

**Formula grammar** (whitespace-insensitive):

```
expr   := term (('+' | '-') term)*
term   := factor (('*' | '/') factor)*
factor := '-' factor
        | NUMBER
        | "double-quoted string"
        | columnIdent
        | '(' expr ')'
        | CONCAT '(' arg (',' arg)+ ')'
```

- arithmetic requires number operands (number columns or numeric literals); unary
  minus requires number
- identifiers must be real non-virtual columns; **fk columns cannot appear in
  formulas**
- `CONCAT` requires ≥ 2 text operands (text columns or string literals) and yields text
- `NULL` operand → `NULL` result; division by zero → `NULL` (never an error)
- invalid formulas compile to nil — reads still succeed, the column renders empty

## Sort defaults and keys

- `DefaultSort: kucrud.Sort(col, dir)` — the column must exist and be sortable;
  without one the engine falls back to: first key column ASC → first sortable
  non-virtual column → unsorted.
- `Keys` drive `GET/PUT/DELETE rows/{key}`, bulk-delete, exports of single rows and
  the audit row identity. Composite keys are first-class: the URL key is the opaque
  encoding of the JSON array of key values (see [04 — HTTP API](04-http-api.md)).

## Registration-time validation summary

`App.Resource` fails fast on: unknown driver/connection failure; table not found;
no primary key and no `Def.Keys`; unknown override names (non-M2M); an override with
both `FK` and `M2M`; `FK` without `RefColumn`; query views with fk/m2m/validation
columns; invalid formatting or validation-rule JSON; bad hook assignments or actions
JSON; `PageSize` outside 1..200; non-sortable explicit `DefaultSort` column.
