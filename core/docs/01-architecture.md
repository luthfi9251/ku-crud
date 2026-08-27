# 01 — Architecture

kucrud-core is a layered library. Each layer has one job and talks only to its
neighbors, so you can consume the whole stack (the `App`) or stop at any seam.

```
┌─────────────────────────────────────────────────────────────────┐
│                        YOUR PROCESS                             │
│   server · router · auth/sessions · logging · your handlers     │
└───────────────┬─────────────────────────────────────────────────┘
                │ mux.Handle("/api/", app)
┌───────────────▼───────────────┐   ┌──────────────────────────────┐
│  kucrud (root, app.go)        │   │  hooks                       │
│  App · New · Def · Override   │   │  Registry · HookFunc ·       │
│  introspection-backed build   │   │  assignments · actions       │
└───────┬───────────────────────┘   └──────────────▲───────────────┘
        │ App.Resource / App.CRUD                │ used by
┌───────▼───────────────────────┐   ┌────────────┴───────────────┐
│  httpapi                      │   │  defs (shared vocabulary)  │
│  anchor routing · Op · Gate   │   │  Table · Column · FK · M2M │
│  query-view write guard       │   │                            │
└───────┬───────────────────────┘   └────────────▲───────────────┘
        │ dispatch(w, r, op, write, fn)          │
┌───────▼───────────────────────────────────────────────────────┐
│  engine                                                        │
│  ReadService · WriteService · ImportService                    │
│  filters · validation · rowkeys · rels · computed · csvutil    │
└───────┬───────────────────────────────────────────────────────┘
        │ Resolver.Adapter(t)
┌───────▼───────────────────────────────────────────────────────┐
│  ds                                                            │
│  Conn · Open · Adapter (postgres / mysql) · sqlkit builders    │
│  query views · drift · aggregates                              │
└────────────────────────────────────────────────────────────────┘
                │ parameterized SQL, identifier allowlist
        ┌───────▼───────┐
        │ Postgres/MySQL│
        └───────────────┘
```

## Design principles

1. **The host owns everything cross-cutting.** No server, no router, no auth, no
   persistence inside the library. The `Gate func(r *http.Request, op Op, table string) error`
   is the single authorization slot ([05 — Authorization](05-authorization.md)).
2. **Definitions are data, not ids.** `defs.Table`/`defs.Column` reference related
   definitions *by name* — no integer ids, no foreign keys into metadata storage. The
   sentinel `defs.MissingTable` marks dangling references; `""` means self-reference.
3. **Introspection provides defaults; declarations override.** `App.Resource`
   introspects the physical table at registration time (labels, nullability, keys,
   enums) and merges your `Override` values on top — a declaration is a *diff*, not a
   full listing ([03 — Definitions](03-definitions.md)).
4. **Identifiers never reach SQL unchecked.** Every column/table name passes the strict
   allowlist `^[A-Za-z_][A-Za-z0-9_]*$` (`ds.QuoteIdent`) before quoting; values are
   always bind parameters; LIKE wildcards in user search input are escaped.
5. **Read paths degrade gracefully.** Relation enrichment (fk display rows, m2m links)
   is batched and *skipped* when a read-grant callback denies a target — the request
   still succeeds, minus the enrichment.
6. **Query views are read-only by construction.** `SourceType: "query"` defs reject
   every write route with `403 QUERY_READONLY` before any grant logic runs, and their
   SQL executes inside a read-only transaction with a statement timeout
   ([07 — Datasources](07-datasources.md)).

## The life of a request

A single `GET /api/data/orders/rows?search=acme&filters=...` through the platform-style
mount illustrates every layer:

1. **Host mux** — your router matches `/api/data/...` and hands the request to the
   `App` (or to a bare `httpapi.Resource` you mounted yourself).
2. **Anchor routing** (`httpapi`) — `Resource.ServeHTTP` scans the URL for the first
   *anchor segment* (`rows`, `fkoptions`, `m2moptions`, `import`, `stats`); everything
   before it is your mount prefix. This is why a resource mounts under any prefix
   (with the one caveat that a prefix must not itself contain an anchor word).
3. **Guard, then gate** — for write-marked ops on a `SourceType: "query"` def the
   `QUERY_READONLY` guard fires first; then the `Gate` is called with the `Op` class
   (`OpRead`, `OpCreate`, …) and the definition name. A non-nil error becomes
   `403 FORBIDDEN`.
4. **Service** (`engine`) — e.g. `ReadService.List` resolves sort + filters
   (`ResolveSort`, `ParseFilters`), opens an adapter through the `Resolver`, and runs
   list + count in one pass.
5. **Datasource** (`ds`) — `sqlkit` builders render dialect-correct SQL (pg `$n`
   placeholders + `ILIKE` + `::text` casts; mysql `?` + `LIKE` + `CAST(... AS CHAR)`),
   reusing the exact same `filterParts`/`searchWhere` machinery for lists, counts,
   exports and aggregates.
6. **Post-processing** — computed columns are applied in memory, fk/m2m relations are
   batch-resolved (chunked `IN` lists of 500) and attached as `rels`/`m2mRels` maps.
7. **Response** — one JSON envelope per route ([04 — HTTP API](04-http-api.md));
   errors are always `{"code", "message", "detail"}`.

Write requests additionally run the hook lifecycle: `Guard` (assignments usable?),
`RunBefore` (may rewrite the payload; the rewritten payload is re-validated),
the SQL write, then `RunSyncAfter` (optional, in-request, best-effort) and `RunAfter`
(post-commit, must not fail the request) — see
[06 — Hooks & actions](06-hooks-and-actions.md).

## The two consumption paths

**Standard path — the `App`.** One validated connection, registration-time
introspection, defaults merged with overrides, per-def `http.Handler`s plus an App-level
mux (`/api/defs` + everything registered via `CRUD`). The library wires default engine
services for you.

```go
h, err := app.Resource("orders", kucrud.Def{Table: "orders", Keys: []string{"id"}})
mux.Handle("/api/v1/orders/", h)
```

**Advanced path — inject your own services.** Hosts with multi-connection resolvers,
per-target read grants, junction write grants, metadata-derived delete protection or
outbox-backed hooks skip the App defaults and supply `httpapi.Options.Services`
themselves (this is exactly how the Ku-CRUD platform serves runtime-mutable
definitions). Covered in [05 — Authorization](05-authorization.md) and
[08 — Embedding the engine](08-embedding.md).

```go
h := httpapi.New(name, table, mySource, httpapi.Options{
    Gate: myGate,
    Services: func(r *http.Request, t *defs.Table) httpapi.ServiceSet {
        return httpapi.ServiceSet{Read: read, Write: write, Import: imp}
    },
})
```

## Invariants worth knowing

- **Connections are lazy and per-use.** `Resolver.Adapter(t)` opens on demand and the
  engine closes what it opens. The `App` shares one pooled adapter internally (its
  `Close` is a no-op wrapper) and you close it once at shutdown.
- **Defs are immutable snapshots per request.** Services receive a `*defs.Table`
  resolved for that request; hosts with runtime-mutable metadata simply rebuild the
  snapshot per request (the platform does).
- **Limits** live in one place and are enforced server-side: page size ≤ 200
  (default 20), ≤ 10 filters per request, `in` lists of 1..50, bulk-delete ≤ 1000,
  CSV export ≤ 100 000 rows, CSV import ≤ 5 MB / 10 000 rows, query-view statements
  ≤ 15 s, before-hooks ≤ 5 s, after-hooks ≤ 30 s.
