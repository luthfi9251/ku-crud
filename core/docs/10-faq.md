# 10 — FAQ

Questions developers actually ask when adopting kucrud-core — from first contact to
production deployment. Written from the perspective of someone new to the library
shipping it in a cloud service. Deeper answers link into the guide pages.

## Getting oriented

### Is this an ORM?

No. An ORM maps your structs to tables row by row and you compose queries in code.
kucrud-core is a **CRUD service generator**: you declare a table once, it introspects
the real schema and serves a complete, validated data API (list/get/create/update/
delete, search/sort/filters/pagination, relations, CSV, aggregates) over HTTP. There
are no model structs and no query builder in your code. If you need arbitrary queries,
that's what query views are for ([03 — Definitions](03-definitions.md#query-views)).

### Does it run DDL / create tables?

Never. Ku-CRUD reads `information_schema` and introspects; all writes are DML with
bind parameters. Your schema is yours — the library fails fast at registration if the
declared table doesn't exist. The starter template ships a `schema.sql` it applies
itself **before** registration, which is the recommended split.

### Which databases are supported?

Postgres and MySQL, through one dialect-neutral `ds.Adapter`. See
[07 — Datasources](07-datasources.md#dialect-differences-worth-knowing) for the
dialect matrix.

### Does it store anything itself?

No — zero persistence, zero metadata storage in the library. Definitions are
in-memory snapshots registered at startup (or rebuilt per request by hosts with
runtime-mutable metadata). The full Ku-CRUD *platform* adds SQLite metadata, auth and
an admin UI on top of this library; the library itself is stateless.

### Can I use it with my framework (gin / chi / echo / fiber)?

Yes, anything that can host an `http.Handler`. Resources are plain handlers you mount
wherever you like — including behind your middleware chain:

```go
r := gin.New()
h, _ := app.Resource("products", kucrud.Def{Table: "products"})
r.Any("/api/v1/products/*any", gin.WrapH(http.StripPrefix("/api/v1", h)))
```

(With fiber you need the `fiber/adaptor`, same idea.) Routing is anchor-based: the
handler finds `rows`/`fkoptions`/`m2moptions`/`import`/`stats` wherever they appear —
so prefixes are free-form except they must not *contain* an anchor word.

## More than one database

### I have more than one database. How do I serve them all?

Three workable shapes, in increasing sophistication:

**1. One App per database.** `kucrud.New` takes exactly one `Conn` — spin up N apps
and mount each under its own prefix. Everything (relations, joins, hooks) works
because each app is internally consistent:

```go
salesApp, _ := kucrud.New(salesConn, kucrud.WithGate(gate))
crmApp, _   := kucrud.New(crmConn,   kucrud.WithGate(gate))

mux.Handle("/api/sales/", http.StripPrefix("/api/sales", salesApp))
mux.Handle("/api/crm/",   http.StripPrefix("/api/crm",   crmApp))
```

**2. One serving surface, custom resolver (the platform pattern).** Implement
`engine.Resolver` / `httpapi.DefSource` so each definition resolves to *its own*
datasource, and inject services via `httpapi.Options.Services`
([05 — Authorization](05-authorization.md#the-advanced-path-injecting-services)).
This is exactly how the Ku-CRUD platform serves runtime-registered defs across any
number of datasources: the def carries its datasource id, and
`Resolve(name)` / `Adapter(t)` open the right connection per table. List, count,
aggregate, write and import paths all run on the request's own def — so cross-database
is invisible to them.

```go
func (m *multiResolver) Adapter(t *defs.Table) (ds.Adapter, error) {
    d := metaByName[t.Name]          // each def knows its datasource
    return ds.Open(datasourceConn(d))
}
```

**3. Same Postgres cluster, different schemas.** Not even a special case: defs carry
per-table `Schema`, so multiple schemas in one connection are just multiple defs.

### Can a table in database A reference (fk) a table in database B?

Mostly yes, with one precise limitation. Relations resolve **by definition name**
through your resolver, not by SQL, so:

- **Works across databases**: fk display enrichment (`rels` maps), fk pickers
  (`fkoptions`), m2m links and pickers, write-time referential checks. Each of these
  runs as its own query against the *target's* adapter
  ([rels.go](../engine/rels.go) opens one adapter per target).
- **Does not work across databases**: filtering an fk column by the target's
  *display text* (`contains`/`eq` on an fk column). That renders a SQL `LEFT JOIN`
  inside the list query, which can only join tables inside one connection. The host's
  `FKJoin` callback decides — return an error for cross-server targets and the filter
  column is rejected cleanly.

The raw ref value itself (`WHERE customer_id = 3`) is always fine — it's just a
column on your table.

### One connection for everything, or a pool per request?

The `App` shares one pooled adapter internally (`pgx` pool with 5-minute
`ConnMaxLifetime`; MySQL DSN pooling). Custom resolvers choose their own strategy —
open-per-request with `defer Close()` (the platform's choice for isolation) or share
pools keyed by datasource. `Adapter` implementations must be safe to `Close` after
use; the engine always closes what it opens.

## Auth & security

### What's the default authorization?

**Everything is allowed.** A nil `Gate` allows every op on every def. The starter
template deliberately ships a deny-all stub so nothing opens by accident. Wire your
real check before exposing anything — [05 — Authorization](05-authorization.md).

### How do I plug in my JWT / sessions?

You don't hand tokens to the library. Authenticate in *your* middleware, put the user
on the request context, and close over it in the gate:

```go
mux.Use(authMiddleware)                 // validates JWT, stores User in ctx

gate := func(r *http.Request, op kucrud.Op, table string) error {
    u := userFromCtx(r)                 // your context helper
    if u == nil { return errors.New("unauthenticated") }
    return authorize(u, op, table)      // your RBAC
}
app, _ := kucrud.New(conn, kucrud.WithGate(gate))
```

### Is it SQL-injection safe?

Identifiers (table/column names) never interpolate: they pass the strict allowlist
`^[A-Za-z_][A-Za-z0-9_]*$` before dialect quoting. Values are always bind parameters.
LIKE wildcards (`%`/`_`) in user search input are escaped so they match literally.
Query-view SQL is admin-declared, `EXPLAIN`-validated as a single SELECT, and executed
read-only with a timeout. The remaining trust decision — *who may declare defs and
query views* — is yours.

### Can I hide rows per tenant / per user?

The gate filters *requests*, not *rows*. Row-level policies go in one of three places
(detailed in [05](05-authorization.md#row-level-authorization)):

- wrap the handler and inject a `filters` query param (filters AND-combine, so the
  client can't drop yours),
- a before-write hook that stamps/validates `tenant_id`,
- serve a scoped query view (`WHERE tenant_id = ...`) instead of the base table.

## Definitions & schema

### What happens when my schema changes after registration?

Registration-time introspection is a snapshot. If the live schema drifts (column
dropped/added/retyped), reads/writes will surface it as errors, and
`ds.CompareDrift` tells you exactly what moved:

```go
live, _ := adapter.InspectTable(def.Schema, def.PhysTab)
rep := ds.CompareDrift(table.Columns, live) // .Missing .Added .TypeChanged
```

The platform runs this on every page visit (`verify`) and offers a resync that
rebuilds the def from live introspection. With the plain `App`, restart to re-register
(or call `app.Resource` again — re-registering a name replaces the def).

### Can definitions change at runtime?

With the `App`, re-registering a name replaces the def — but consumers see a
consistent snapshot per request. Hosts needing fully dynamic defs (a table wizard,
multi-tenant catalogs) skip the App and build defs per request behind their own
`DefSource` — [05 — Authorization](05-authorization.md), [08 — Embedding](08-embedding.md).

### Why is my column missing from `/api/defs`?

Either its type maps to `""` (arrays, bytea, anything exotic — excluded by design),
or it's a query-view expression column without an alias (`SELECT count(*)` disappears;
`SELECT count(*) AS n` stays). Also check `Hidden: true` overrides.

### Can I expose a report / join over raw SQL?

Yes — query views: `SourceType: "query"` + `QuerySQL`. Read-only, searchable,
sortable, filterable, exportable like any table. Guards and limits:
[03 — Definitions](03-definitions.md#query-views).

## Hooks

### Where do hooks run — same process? Same request?

Before-hooks run synchronously inside the request (5 s budget). After-hooks run
post-commit: in the library's default wiring synchronously and best-effort (an error
is logged, never fails the request); hosts with durability needs implement
`engine.Hooks` with an outbox (the platform enqueues durable entries drained by a
retrying worker). Hooks are Go functions compiled into your binary and registered by
name — there is no plugin loading. Full contract:
[06 — Hooks & actions](06-hooks-and-actions.md).

### What does an after-hook failure do to my write?

Nothing — the write already committed. Default wiring: logged. Outbox wiring: retried
by the worker. That asymmetry (before can reject, after cannot) is the contract.

## Limits, performance, operations

### What limits are built in?

| Limit | Value |
|---|---|
| Page size | def-configured, 1..200 (default 20) |
| Filters per request | 10 |
| `in` list | 1..50 values |
| Bulk delete | 1000 keys |
| CSV export | 100 000 rows |
| CSV import | 5 MB file / 10 000 rows |
| Query-view statement | `ds.QueryTimeout`, default 15 s |
| Before / after / action hooks | 5 s / 30 s / 15 s |

All enforced server-side and surfaced as dedicated error codes (`BULK_TOO_LARGE`,
`EXPORT_TOO_LARGE`, `QUERY_TIMEOUT`, …) — see the table in
[04 — HTTP API](04-http-api.md#error-codes).

### Can I raise the query-view timeout?

It's a package variable: `ds.QueryTimeout = 30 * time.Second`. Apply it at startup;
it bounds every stored-query execution.

### Is it safe to run multiple replicas?

The library is stateless — defs are in-memory, no writes to local disk — so replicas
are only bounded by your databases. (Caveat for the *platform*, not core: its SQLite
metadata is single-writer; put replicas behind sticky routing or move metadata to
shared storage.)

### Health checks and graceful shutdown?

The library doesn't serve health endpoints — mount your own. The cheapest real check
is the connection you already hold:

```go
mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
    if err := pingDB(); err != nil {      // ds.Adapter.Ping or sql.DB.Ping
        http.Error(w, "db down", 503)
        return
    }
    w.Write([]byte("ok"))
})
```

Shutdown: stop accepting traffic, drain in-flight requests, then `app.Close()`.

### What should I alert on?

`502 CONN` (datasource unreachable/slow), `502 QUERY_TIMEOUT` (a stored query got
heavier than 15 s), and the `4xx VALIDATION`/`FILTER_INVALID` rate as a proxy for
clients fighting your definitions. All distinguishable by the error `code` in the
response body — log it, don't just the status.

### How do I test all this?

No database required: fake resolver + fake adapter structs + `httptest` for routing,
validation, gating, enrichment. Real-database integration tests self-skip without
`KUCRUD_TEST_PG` / `KUCRUD_TEST_MYSQL`. Patterns and code:
[08 — Embedding the engine](08-embedding.md#testing-patterns).

## Still stuck?

- Wire contract and shapes → [04 — HTTP API](04-http-api.md)
- Declaration semantics → [03 — Definitions](03-definitions.md)
- Platform-side hooks guide → [`docs/developer-hooks.md`](../../docs/developer-hooks.md)
- Runnable full-stack example → [`template/`](../../template/) in the monorepo
