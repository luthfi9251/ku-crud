# kucrud-core documentation

**kucrud-core** is the code-first CRUD library extracted from [Ku-CRUD](https://github.com/luthfi9251/ku-crud):
declare your Postgres/MySQL tables with introspection-backed defaults and per-column
overrides, and get a mount-anywhere `http.Handler` per definition — JSON rows CRUD,
relations (fk / many-to-many), CSV import/export, single-value aggregates, hooks, and
read-only SQL views.

The library owns **no server, no router, no persistence and no auth**. You own the
process; `kucrud-core` owns the data access, validation and HTTP semantics. The single
`Gate` function is the authorization slot.

```go
app, _ := kucrud.New(kucrud.Conn{Driver: "postgres", Raw: dsn},
    kucrud.WithGate(myGate))
h, _ := app.Resource("products", kucrud.Def{Table: "products"})
mux.Handle("/api/v1/products/", h)          // mount anywhere
app.CRUD("/api/data/orders", kucrud.Def{    // or register on the App mux
    Table: "orders",
    Columns: []kucrud.Override{{Name: "note", Required: true}},
})
mux.Handle("/api/", app)                    // + GET /api/defs
http.ListenAndServe(":8080", mux)
```

## Contents

| Doc | What it covers |
|---|---|
| [01 — Architecture](01-architecture.md) | Layering, design principles, the life of a request |
| [02 — Getting started](02-getting-started.md) | Install, connect, register resources, mount, ship |
| [03 — Definitions](03-definitions.md) | `Def` + `Override`: introspection defaults, merge rules, fk/m2m, query views, computed columns, validations, formatting |
| [04 — HTTP API](04-http-api.md) | Every route, request/response shapes, filters, pagination, row keys, error codes |
| [05 — Authorization](05-authorization.md) | The `Gate` slot, `Op` classes, and the advanced `Services` injection path |
| [06 — Hooks & actions](06-hooks-and-actions.md) | Hook contract, registry, `hookgen`, events, custom row actions |
| [07 — Datasources](07-datasources.md) | Connections, dialects, the `Adapter` contract, query-view guards, drift, aggregates |
| [08 — Embedding the engine](08-embedding.md) | Driving services without `httpapi`, the CSV import pipeline, testing patterns |
| [09 — API reference](09-api-reference.md) | Compact per-package reference of every exported identifier |
| [10 — FAQ](10-faq.md) | Questions developers actually ask — multi-database setups, auth, limits, ops |

## Five-minute quickstart

```go
package main

import (
    "log"
    "net/http"

    kucrud "github.com/luthfi9251/ku-crud/core"
)

func main() {
    app, err := kucrud.New(kucrud.Conn{
        Driver: "postgres",
        Raw:    "postgres://user:pass@localhost:5432/app?sslmode=disable",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer app.Close()

    // Registers /api/data/products/... on the App's internal mux.
    // Columns/keys are introspected at registration time; the overrides
    // below only tweak what introspection found.
    app.CRUD("/api/data/products", kucrud.Def{
        Table: "products",
        Columns: []kucrud.Override{
            {Name: "price", Label: "Price", Format: `{"number":{"decimals":2}}`},
        },
        DefaultSort: kucrud.Sort("created_at", kucrud.Desc),
    })

    log.Fatal(http.ListenAndServe(":8080", app))
}
```

Try it:

```console
$ curl -s localhost:8080/api/defs | jq '.[].name'
"products"

$ curl -s 'localhost:8080/api/data/products/rows?page=1' | jq '.total, .rows[0]'
```

## Installation

The module lives in the `core/` directory of the Ku-CRUD monorepo and is released as
the submodule `github.com/luthfi9251/ku-crud/core` with `core/vX.Y.Z` tags:

```
go get github.com/luthfi9251/ku-crud/core@v1.10.0
```

Inside the monorepo (platform + template) a `replace` pins it to the local directory:

```
require github.com/luthfi9251/ku-crud/core v0.0.0
replace github.com/luthfi9251/ku-crud/core => ./core
```

Dependencies: `github.com/jackc/pgx/v5` (Postgres) and
`github.com/go-sql-driver/mysql` (MySQL). Go 1.25+.

## Where is what

| Package | Role |
|---|---|
| `kucrud` (root) | `App`, `New`, `Def`, `Override`, `CRUD`, `Resource`, re-exported aliases |
| `kucrud/defs` | The ID-free definition vocabulary: `Table`, `Column`, `FK`, `M2M`, `ValidationRule` |
| `kucrud/ds` | Dialect-neutral datasource layer: `Conn`, `Open`, the `Adapter` interface, drift |
| `kucrud/engine` | Request services: read/write/import/stats + filters, validation, computed columns |
| `kucrud/httpapi` | Renders one definition as anchor-based data routes; `Gate` dispatch |
| `kucrud/hooks` | Hook registry, event contract, assignments, custom actions |

A complete, runnable consumer lives at [`template/`](../../template/) in the monorepo —
a full-stack starter (Go + embedded SPA) wired exactly the way this library intends.

## Stability notes

- The wire contract (routes, JSON shapes, error codes) mirrors the Ku-CRUD platform;
  changes there land here first.
- `OpAction` is reserved: custom row actions are parsed and exposed through the defs
  listing, but no core route executes them yet — hosts dispatch them (see
  [06 — Hooks & actions](06-hooks-and-actions.md)).
