# Ku-CRUD — kucrud-core Extraction (Design Spec)

Date: 2026-08-25
Status: Approved (direction confirmed with owner; consultation phase)
Baseline: development @ a7ed0cb (v1.9 merged)

## 1. Goal

Reposition Ku-CRUD from a single runtime platform into a family of products
sharing one engine, with the long-term ambition of replacing GroceryCRUD
(the current tool of the owner's company — rejected for PHP unfamiliarity
and poor performance) with a Go equivalent.

Three faces over one engine:

```
kucrud-core    zero-persistence Go library: CRUD endpoint generator
               (ds adapters, sqlkit, validation, filter/sort/page,
                CSV, hooks as callbacks) — Def registered in code
     ↑
template       full-stack starter: Go app wiring core + React frontend
               that consumes the core API + auth stub (slot, host-owned)
     ↑
platform       ku-crud as it exists today: core + auth/RBAC + runtime
               definitions (SQLite) + embedded SPA
```

The platform becomes the first consumer of the core, not a separate
codebase. Nothing in the current feature set dies; stateful concerns
move down into platform-level hook implementations.

## 2. Owner decisions (confirmed)

| Question | Decision |
|---|---|
| Product direction | **Validate first.** No commitment to library-vs-platform as "the" product until dogfooded. |
| First consumer | The owner's own projects / company migration off GroceryCRUD (real pain, real dogfood). |
| Product shape | **CRUD endpoint generator + template.** Core ships API only; frontend is supplementary — a template that consumes the API. Not an embeddable UI library. |
| Auth in core | **None.** Template carries an auth stub (middleware slot) so naive users don't deploy open CRUD endpoints; the host owns real auth. |
| Persistence in core | **Zero persistence.** No SQLite, no runtime state. If a project needs audit/outbox, the developer implements it at definition time (hooks). |
| Definitions | **Code-first, grocery-style**: `app.CRUD("/products", Def{...})` — table, column overrides, sort, etc. registered in Go. Code is the source of truth. |
| Frontend ownership | Template frontend is user-owned for layout/pages; CRUD screens consume core's API contract. Wholesale rewrite allowed; API contract is the stability guarantee. |
| API ownership | **The developer owns the handler.** kucrud is a mounted `http.Handler`; the host keeps its server, router, middleware, and auth. `Resource(name, Def) http.Handler` is the primary API; `CRUD(path, Def)` is sugar over an internal mux. |
| Repository | **Separate repository (end state).** `kucrud-core` gets its own repo once the platform switchover suite is green — history preserved via path-aware split. Until then it lives as an in-repo module behind a `replace` directive. |
| Audit trail | Not a core feature. Actor injected via context (`WithActor`); audit persistence is a host-side hook recipe. |
| After-hooks / outbox | Plain callbacks in core. The durable outbox (v1.6) remains a platform feature / reference recipe. |
| MySQL | **First-class from day one** — GroceryCRUD shops are historically MySQL shops. Adapter + tests already exist; keep parity. |

## 3. Scope

### In core (extracted from today's `internal/`)

- `internal/ds` → adapters (postgres, mysql), introspection, drift compare
- sqlkit (dialect SQL builders, identifier allowlist, parameterization)
- column types & validation rules (email/min/max/number-only/text-only)
- search/sort/filter/pagination pipeline, computed columns
- CSV import (preview/validate) & export pipeline
- query-backed read-only definitions (v1.8, EXPLAIN guard)
- hooks as synchronous callbacks: before/after × create/update/delete,
  `onAction`; config + ordering per assignment
- fk / m2m relation resolution
- drift verify (`/verify` semantics)
- HTTP handlers **without** auth/session/RBAC gates
- `Def` registration API (Go builder) — introspection-backed defaults
  with per-column overrides (declaration = override, not full listing)

### Out of core (stays platform-side or host-side)

- auth, sessions, users, roles, per-table grants, platform grants
- audit persistence, durable outbox + worker, saved filters
- masked entity ids (Feistel tokens) — no shared metadata store to protect
- metadata SQLite store, migrations, import/export of definitions
- first-run setup, rate limiting (login-adjacent)
- embedded SPA as a *product* (platform keeps it; core ships no UI)

### Explicit non-goals

- `kucrud init` CLI scaffolder — `git clone` template + rename module is
  enough; CLI is deferred until it pays rent.
- GroceryCRUD→Go converter — company migration is per-app, pilot-first.
- MongoDB adapter (standing decision from v1.2 notes).
- DDL — never (standing project principle).
- Full template-override UI theming — theme tokens / CSS variables only,
  escalate if dogfood hits a wall.

## 4. Def API sketch (directional, not final)

The library lives *inside* the host application — the developer keeps the
server, router, and middleware. kucrud is a mounted `http.Handler`:

```go
products, err := app.Resource("products", kucrud.Def{
    Table: "products",          // columns auto-derived via introspection
    Columns: []kucrud.Override{
        {Name: "price", Format: kucrud.Currency("Rp")},
        {Name: "internal_note", Hidden: true},
    },
    DefaultSort: kucrud.Sort("created_at", kucrud.Desc),
})

mux := http.NewServeMux()                              // host-owned router
mux.Handle("/api/data/products/", withAuth(products))  // host-owned middleware
http.ListenAndServe(":8080", mux)                      // host-owned server
```

`CRUD(path, Def)` remains as sugar for the lazy path (registers the
resource on an internal mux the host mounts wholesale) — the template
uses it; embedded apps use `Resource`.

The `Def` struct is the JSON definition contract that already exists
between wizard and engine, surfaced as a public Go API. Freezing that
contract is a precondition for any external release.

## 5. Migration & validation plan (strategy, not tasks)

1. **Pilot one screen** of one company app on the core before committing
   to full extraction — cheapest honest validation.
2. Extract within one repo first (module boundary: `core/` with its own
   `go.mod` behind a `replace` directive; extraction must leave `internal/`
   — Go forbids cross-module imports of `internal/`). Once the platform
   switchover suite is green, promote `core/` to its own repository
   (history preserved by mapping the old `internal/` paths to their `core/`
   destinations in the split). That is the end state: clean identity,
   `go get`, semver tags. In-repo first avoids two-repo churn mid-refactor.
3. Platform consumes core via the same API the template would.
4. Company migration proceeds per-app, pilot-first, no converter.

## 6. Risks (accepted, with owners)

- **Migration is a rewrite** — each PHP+GroceryCRUD app becomes a Go app;
  not drop-in. Mitigated by pilot-first sequencing.
- **Reliability moves to developers** — audit/outbox quality becomes a
  per-project variable instead of a platform guarantee. Accepted: the
  company writes the recipe once, reuses everywhere.
- **`Def` becomes a public semver contract** — once external users exist,
  breaking changes need major versions. Mitigated by `core/v0.x` tagging
  discipline and freezing the definition JSON shape first.
- **Solo-maintainer surface area** — three faces, one maintainer. Mitigated
  by the shared-engine architecture (one engine, thin shells) and deferring
  CLI/converter/theming.

## 7. Open questions (non-blocking)

- Platform's long-term identity: product, internal tool, or merged into
  the template story — revisit after dogfood.
- Whether `@kucrud/ui` (shared React components) ever becomes a package —
  only if template users keep rebuilding the same screens.
