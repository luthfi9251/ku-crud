# 02 — Getting started

This guide takes you from zero to a running CRUD API over your own database, then
points at the deeper docs for everything else. The runnable reference for everything
described here is [`template/main.go`](../../template/main.go) in the monorepo.

## 1. Install

`kucrud-core` lives in `core/` of the Ku-CRUD monorepo. Consume it from your own
module with a `replace`:

```
go get github.com/luthfi9251/kucrud-core
```

```
// go.mod
require github.com/luthfi9251/kucrud-core v0.0.0

replace github.com/luthfi9251/kucrud-core => ../ku-crud/core   // or your fork
```

Dependencies pulled in: `github.com/jackc/pgx/v5` (Postgres) and
`github.com/go-sql-driver/mysql` (MySQL). Go 1.25+.

## 2. Connect

`kucrud.Conn` is the dialect-neutral connection description. `Raw`, when set, is a
verbatim DSN and overrides every other field; the driver is inferred from the DSN
scheme (`postgres://` / `postgresql://` → postgres, `mysql://` → mysql) when empty.

```go
conn := kucrud.Conn{
    Driver: "postgres",
    // either discrete fields …
    Host: "localhost", Port: 5432, DB: "app", User: "u", Password: "p", SSLMode: "disable",
    // … or one raw DSN (wins):
    Raw: "postgres://u:p@localhost:5432/app?sslmode=disable",
}
```

`kucrud.New(conn, opts...)` opens and validates the connection once; it is used for
introspection at registration time and as the shared adapter behind every request.

## 3. Create the App

```go
app, err := kucrud.New(conn)
if err != nil {
    log.Fatalf("connect: %v", err) // bad DSN, unreachable server, unknown driver …
}
defer app.Close()
```

Options (see [05 — Authorization](05-authorization.md) and
[06 — Hooks & actions](06-hooks-and-actions.md) for details):

```go
app, err := kucrud.New(conn,
    kucrud.WithGate(gate),            // the single auth/RBAC slot
    kucrud.WithHookRegistry(reg),     // custom registry; default hooks.Default
)
```

## 4. Register resources

A `kucrud.Def` declares one resource. For the default `SourceType: "table"` the
physical table **must exist** — Ku-CRUD never runs DDL; it introspects at registration
time and fails fast when the declaration doesn't match reality.

```go
app.CRUD("/api/data/products", kucrud.Def{
    Table: "products",
    Columns: []kucrud.Override{
        {Name: "price", Label: "Price", Format: `{"number":{"decimals":2}}`},
        {Name: "name", Validation: []kucrud.ValidationRule{{Type: "min_len", Param: 2}}},
        {Name: "category_id", Label: "Category",
            FK: &kucrud.FK{Table: "categories", RefColumn: "id",
                DisplayColumns: []string{"name"}}},
    },
    DefaultSort: kucrud.Sort("created_at", kucrud.Desc),
    PageSize:    50,
})

// fk targets must be registered too — relations resolve by definition name
app.CRUD("/api/data/categories", kucrud.Def{Table: "categories"})
```

Two registration forms:

- **`app.CRUD(path, def) *App`** — sugar: takes the resource name from the path's last
  segment, mounts it on the App's internal mux at `path+"/"`, panics on error
  (startup-config mistakes should crash loudly). Chainable.
- **`h, err := app.Resource(name, def)`** — the primary API: returns a plain
  `http.Handler` you mount yourself, anywhere. Re-registering a name replaces the def.

What introspection gives you for free (full merge rules in
[03 — Definitions](03-definitions.md)):

- `Label` = humanized column name (`created_at` → "Created At")
- `NOT NULL` → `Required` (+ `Editable`); nullable → `Editable` unless it's the PK
- `Visible` / `Searchable` / `Sortable` default to true
- keys default to the introspected primary key (tables without one must set
  `Def.Keys` explicitly)
- Postgres enums become `enum` columns with their option list

## 5. Mount and serve

You own the mux. Two choices — they compose:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("ok"))
})

// (a) mount bare resource handlers under your own prefixes
h, _ := app.Resource("products", kucrud.Def{Table: "products"})
mux.Handle("/api/v1/products/", http.StripPrefix("/api/v1", h))

// (b) hand the App mux everything under /api/ — serves /api/defs plus
//     every CRUD()-registered resource
app.CRUD("/api/data/orders", kucrud.Def{Table: "orders"})
mux.Handle("/api/", app)

log.Fatal(http.ListenAndServe(":8080", mux))
```

Routing is anchor-based: each resource handler finds its route root (`rows`,
`fkoptions`, `m2moptions`, `import`, `stats`) wherever it appears in the URL, so mount
prefixes are free-form — **except** that a prefix must not itself contain a segment
named like an anchor.

The full route surface one registration produces:

```
GET    /api/data/products/rows              list page (search/sort/filters/pagination)
POST   /api/data/products/rows              insert
GET    /api/data/products/rows/export       CSV export
POST   /api/data/products/rows/bulk-delete  bulk delete (≤1000 keys)
GET    /api/data/products/rows/{key}        single row (key is opaque, see 04)
PUT    /api/data/products/rows/{key}        update
DELETE /api/data/products/rows/{key}        delete
GET    /api/data/products/rows/{key}/m2m/{column}   m2m link values
GET    /api/data/products/fkoptions/{column}       fk picker page
GET    /api/data/products/m2moptions/{column}      m2m picker page
POST   /api/data/products/import/preview    CSV import preview
POST   /api/data/products/import/apply      CSV import apply
GET    /api/data/products/stats             single-value aggregate (cards)
GET    /api/defs                            registered defs + caller perms
```

## 6. First requests

```console
$ curl -s localhost:8080/api/defs | jq '.[] | {name, table, keys: .keyColumns}'

$ curl -s 'localhost:8080/api/data/products/rows?search=widget&sort=price&dir=DESC&page=1' \
    | jq '{total, page, first: .rows[0]}'

$ curl -s 'localhost:8080/api/data/products/rows?filters=[{"column":"price","op":"gt","values":["100"]}]' \
    | jq '.total'

$ curl -s 'localhost:8080/api/data/products/stats?func=sum&column=price' | jq
{
  "func": "sum",
  "column": "price",
  "value": 12345.5,
  "hasRows": true
}
```

Insert (only `editable` columns are accepted; unknown keys are rejected):

```console
$ curl -s -X POST localhost:8080/api/data/products/rows \
    -d '{"name":"Widget","price":9.99,"category_id":3}'
{"ok":true}
```

## 7. Auth: do not ship without a Gate

Without a `Gate`, **every op is allowed by the library**. Gate decisions are yours:

```go
func gate(r *http.Request, op kucrud.Op, table string) error {
    u, err := userFromSession(r) // your session/token machinery
    if err != nil {
        return err // rendered as 403 with err.Error() as message
    }
    if !userMay(u, op, table) {  // your RBAC tables
        return fmt.Errorf("%s may not %s %s", u.Name, op, table)
    }
    return nil
}

app, _ := kucrud.New(conn, kucrud.WithGate(gate))
```

The starter template deliberately ships a **deny-all stub** so nothing opens by
accident — replace `authstub.Gate` with your real check. More in
[05 — Authorization](05-authorization.md).

## 8. Hooks (optional, compile-time registered)

Assign registered hook functions to events per definition:

```go
app.CRUD("/api/data/products", kucrud.Def{
    Table: "products",
    Hooks: map[kucrud.Event][]string{
        kucrud.BeforeCreate: {"NormalizePrice"},
        kucrud.AfterCreate:  {"LogAfterCreate"},
    },
})
```

Hook functions live in your `hooks/` package and register into `hooks.Default`
— typically via the monorepo's `cmd/hookgen`, which generates the registration file.
The full contract (config payloads, `HookContext`, timeouts, synchronous vs worker
execution) is in [06 — Hooks & actions](06-hooks-and-actions.md).

## 9. Shutdown

```go
defer app.Close() // releases the shared pooled connection
```

Everything else is stateless: adapters are opened per use and closed by the engine;
defs are immutable snapshots.

## Troubleshooting quick hits

| Symptom | Cause |
|---|---|
| `New` fails with connection error | DSN wrong / server unreachable; `Raw` overrides discrete fields |
| `Resource` errors "table has no primary key" | Set `Def.Keys: []string{"col1", "col2"}` (composite keys allowed) |
| `Resource` errors "unknown column" in an override | Overrides must name introspected columns; only `M2M` appends a virtual one |
| 403 `QUERY_READONLY` | The def is a query view (`SourceType: "query"`) — read-only by construction |
| 403 on everything | Your `Gate` denies — or you mounted the template's deny-all `authstub.Gate` |
| 404 on a mounted prefix | The prefix contains a segment named `rows`/`fkoptions`/`m2moptions`/`import`/`stats` — rename it |
| 502 `QUERY_TIMEOUT` | Query-view SQL exceeded the 15 s `ds.QueryTimeout` |
