# kucrud-core Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the CRUD engine from the ku-crud platform into a zero-persistence, auth-free Go module (`kucrud-core`) consumed by both the existing platform and a new full-stack template.

**Architecture:** Freeze the definition contract into ID-free types (`defs`), decouple `ds` from `meta`, lift the row pipeline out of `internal/api` into an `engine` package parameterized on (Adapter, defs, resolver, callbacks), then split into a `core/` module with name-based HTTP routes and a `Def` builder. The platform re-consumes core via a meta-backed resolver + RBAC gate; audit/outbox become platform hook implementations.

**Tech Stack:** Go 1.25, SQLite (platform only), Postgres/MySQL (adapters), React+Vite (template consumer).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-25-kucrud-core-extraction-design.md` (all owner decisions bind).
- Core is zero-persistence: no SQLite, no runtime state, no auth, no audit persistence.
- MySQL is first-class: every engine task runs both `KUCRUD_TEST_PG` and `KUCRUD_TEST_MYSQL` suites.
- `go vet ./...` and `gofmt -l .` clean after every task; `go test ./...` green after every task.
- No behavior change in platform endpoints until Task 9 (extraction is refactor-first, switchover last).
- Integration suites share one schema — run with `-p 1` under load.
- Core module path: `github.com/luthfi9251/kucrud-core` (root uses `replace` until first tag).

## Target layout

```
core/                        module github.com/luthfi9251/kucrud-core
  go.mod
  defs/defs.go               ID-free Table, Column, ValidationRule, Sort
  ds/                        adapter.go, postgres.go, mysql.go, sqlkit.go,
                             query.go, drift.go  (moved from internal/ds)
  engine/                    rows, filters, validate, rowkey, computed,
                             csv, hooks callback runner (from internal/api)
  httpapi/                   name-based routes + Gate slot + /api/defs
  app.go                     App, CRUD(path, Def), introspection defaults
internal/                    platform residue: meta (rows + conversion),
  api/                       auth, RBAC, users/roles, transfer, management
  hooks/                     outbox worker + platform hook adapter
```

---

### Task 1: `internal/defs` — the ID-free contract

**Files:**
- Create: `internal/defs/defs.go`
- Modify: `internal/meta/tabledefs.go` (add conversion only; row structs unchanged)
- Test: `internal/defs/defs_test.go`

**Interfaces:**
- Produces (later tasks build on these exact types):

```go
package defs

type SortDir string

const (
    Asc  SortDir = "asc"
    Desc SortDir = "desc"
)

type Table struct {
    Name            string   // route name, also the menu key (was Label)
    Label           string
    Description     string
    Schema, PhysTab string   // physical table (== Name for simple cases)
    Keys            []string
    PageSize        int
    DefaultSortCol  string
    DefaultSortDir  string
    DefaultView     string
    ViewConfig      string
    SourceType      string   // "table" | "query"
    QuerySQL        string
    Hooks           string   // assignments JSON (contract unchanged)
    Actions         string   // actions JSON (contract unchanged)
    Columns         []Column
}

type Column struct {
    Name, Label, FieldType string
    EnumOptions            []string
    Editable, Required, Visible, Searchable, Sortable bool
    Position               int
    Validations            []ValidationRule
    Formatting             string
    IsComputed             bool
    ComputedFormula        string
    BaseType               string
    FK                     *FK
    M2M                    *M2M
}

// FK references another *definition name*, not an int64 def id.
type FK struct {
    Table          string   // target definition name; "" = self
    RefColumn      string
    DisplayColumns []string
}

// M2M references the junction by table name, not junction def id.
type M2M struct {
    JunctionTable  string
    SrcCol, TgtCol string
    DisplayColumns []string
}

type ValidationRule struct {
    Type  string // email | min_len | max_len | number | text
    Param int
}
```

- [ ] **Step 1: Write the failing conversion test** — `internal/meta` must round-trip `TableDef`+`[]ColumnDef` (with FK/m2m columns and a self-FK resolved through a name→id map) into `defs.Table` and back losslessly for every field the engine reads.

```go
func TestTableDefToCoreRoundTrip(t *testing.T) {
    md := meta.TableDef{ID: 7, DatasourceID: 3, SchemaName: "public",
        TableName: "products", Label: "Products", KeyColumns: []string{"id"},
        PageSize: 25, DefaultSortCol: "created_at", DefaultSortDir: "desc"}
    mcols := []meta.ColumnDef{
        {Name: "category_id", FieldType: "fk", FKTableDefID: 9, FKRefColumn: "id",
            FKDisplayColumns: []string{"name"}, Editable: true},
        {Name: "parent_id", FieldType: "fk", FKTableDefID: 7, FKRefColumn: "id"},
        {Name: "tags", FieldType: "m2m", M2MJunctionDefID: 12, M2MJunctionSrcCol: "product_id",
            M2MJunctionTgtCol: "tag_id", M2MDisplayColumns: []string{"name"}},
    }
    ids := map[int64]string{7: "products", 9: "categories", 12: "product_tags"}

    got := meta.ToCoreDef(md, mcols, ids)

    if got.Name != "products" || len(got.Columns) != 3 {
        t.Fatalf("bad table: %+v", got)
    }
    fk := got.Columns[0].FK
    if fk == nil || fk.Table != "categories" || fk.RefColumn != "id" {
        t.Fatalf("fk not name-based: %+v", fk)
    }
    if got.Columns[1].FK.Table != "products" { // self-FK survives
        t.Fatalf("self fk: %+v", got.Columns[1].FK)
    }
    if got.Columns[2].M2M.JunctionTable != "product_tags" {
        t.Fatalf("m2m not name-based: %+v", got.Columns[2].M2M)
    }
}
```

- [ ] **Step 2: Run it — expect compile failure** (`meta.ToCoreDef` undefined).
  Run: `go test ./internal/defs/`
- [ ] **Step 3: Implement `defs` types + `meta.ToCoreDef(md meta.TableDef, cols []meta.ColumnDef, idToName map[int64]string) defs.Table`** in `internal/meta/tabledefs.go`. Self-FK (`FKTableDefID == md.ID`) maps to `Table: ""`.
- [ ] **Step 4: Full suite + vet clean.**
  Run: `go vet ./... && gofmt -l . && go test ./...`
- [ ] **Step 5: Commit** — `refactor(defs): ID-free definition contract + meta conversion`

### Task 2: `ds` decoupled from `meta`

**Files:**
- Modify: `internal/ds/adapter.go` (replace `meta.Datasource` with `ds.Conn`)
- Modify: `internal/meta/datasources.go` and every `ds.Open` caller (grep `ds.Open(` — auth-side datasource test endpoint, hooks context opener, engine handlers)
- Test: existing `internal/ds` suites (no new tests — signature swap)

**Interfaces:**
- Produces:

```go
package ds

type Conn struct {
    Driver, Host string
    Port         int
    DB, User, Password, SSLMode string
}

func Open(c Conn) (Adapter, error)
```

- [ ] **Step 1: Add `ds.Conn`, change `Open` signature;** every caller converts from `meta.Datasource` fields at the call site (`ds.Conn{Driver: d.Driver, Host: d.Host, ...}`).
- [ ] **Step 2: Build + run adapter suites both dialects.**
  Run: `go vet ./... && go test ./internal/ds/...` then with `KUCRUD_TEST_PG` and `KUCRUD_TEST_MYSQL` (`-p 1`)
- [ ] **Step 3: Commit** — `refactor(ds): Open takes ds.Conn, package no longer imports meta`

### Task 3: engine — pure helpers move

**Files:**
- Create: `internal/engine/` (one file per concern, moved from `internal/api`)
  - `filters.go` (from `api/filters.go` logic), `validate.go` (from `api/validate.go`),
    `rowkey.go` (from `api/rowkey.go`), `computed.go` (from `api/computed.go`),
    `csvutil.go` (from `api/csvutil.go`)
- Modify: `internal/api` callers import engine; delete moved code
- Test: move the matching `*_test.go` files alongside; adapt types `meta.ColumnDef` → `defs.Column`

**Interfaces:**
- Consumes: `defs.Table`, `defs.Column` (Task 1), `ds.Adapter` (unchanged)
- Produces (exact signatures; all pure — no store, no user):

```go
package engine

func ResolveSort(t *defs.Table, sortCol, sortDir string) (string, string)
func EditablePayload(t *defs.Table, body map[string]any, isInsert bool) ([]string, []any, error)
func ValidateColumn(c defs.Column, v any) error
func ParseFilters(t *defs.Table, q url.Values) ([]ds.ColumnFilter, error)
func EncodeKey(t *defs.Table, vals map[string]any) (string, error)
func DecodeKey(t *defs.Table, s string) ([]any, error)
```

- [ ] **Step 1: Move + adapt one file at a time**, running its moved test after each: rowkey → validate → filters → computed → csvutil. Type adaptation rule: `[]meta.ColumnDef`+`*meta.TableDef` params become `*defs.Table` (columns live inside); `s.store`/`CtxUser` references stay in `api` (those files are not moved yet if they contain any).
- [ ] **Step 2: Full gate.**
  Run: `go vet ./... && gofmt -l . && go test ./... -p 1` (plus PG/MYSQL env runs)
- [ ] **Step 3: Commit per file move** — `refactor(engine): move rowkey/validate/filters/computed/csvutil`

### Task 4: engine — read path (list/get/export)

**Files:**
- Create: `internal/engine/rows_read.go`
- Modify: `internal/api/rows.go`, `internal/api/export.go` (handlers shrink to: auth → resolve def → call engine → write JSON)
- Test: `internal/engine/rows_read_test.go` (moved/adapted from `api/rows_test.go` read cases)

**Interfaces:**
- Consumes: Task 3 helpers
- Produces:

```go
package engine

// Resolver hands the engine the physical datasource behind a definition,
// plus related definitions (fk/m2m targets) by name. The platform
// implements it over meta; the core App implements it over registered defs.
type Resolver interface {
    Adapter(t *defs.Table) (ds.Adapter, error)
    Resolve(name string) (*defs.Table, error) // "" = self handled by caller
}

type ReadService struct{ R Resolver }

func (s *ReadService) List(w http.ResponseWriter, r *http.Request, t *defs.Table)
func (s *ReadService) Get(w http.ResponseWriter, r *http.Request, t *defs.Table)
func (s *ReadService) ExportCSV(w http.ResponseWriter, r *http.Request, t *defs.Table)
```

- [ ] **Step 1: Characterization** — run existing `api/rows_test.go` read cases green before touching anything (baseline).
- [ ] **Step 2: Move list/get/export flow into `ReadService`** — mechanical: replace `s.store.GetTableDef` with resolver lookups passed in, replace `CtxUser` grant checks with nothing (they stay in the api handler before the call), keep response JSON byte-identical.
- [ ] **Step 3: Read-path tests green (unit + PG + MYSQL).**
- [ ] **Step 4: Commit** — `refactor(engine): read path on ReadService`

### Task 5: engine — write path (insert/update/delete/bulk)

**Files:**
- Create: `internal/engine/rows_write.go`
- Modify: `internal/api/rows.go` write handlers, `internal/api/bulk.go`
- Test: `internal/engine/rows_write_test.go` (adapted from `api/rows_write_test.go`, `api/bulk_test.go`)

**Interfaces:**
- Consumes: `Resolver`, Task 3 helpers
- Produces:

```go
package engine

// Hooks is the zero-persistence callback contract. All synchronous; the
// platform's outbox is one possible AfterX implementation.
type Hooks interface {
    RunBefore(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error)
    RunAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload) error
}

type WriteService struct {
    R Resolver
    H Hooks // may be nil
}

func (s *WriteService) Insert(w http.ResponseWriter, r *http.Request, t *defs.Table)
func (s *WriteService) Update(w http.ResponseWriter, r *http.Request, t *defs.Table)
func (s *WriteService) Delete(w http.ResponseWriter, r *http.Request, t *defs.Table)
func (s *WriteService) BulkDelete(w http.ResponseWriter, r *http.Request, t *defs.Table)
```

- [ ] **Step 1: Move insert/update/delete/bulk flows.** Audit calls (`s.store.InsertAudit`) are DELETED from the moved code — audit returns in Task 10 as an AfterX hook. FK conflict mapping (`IsFKViolation`) and m2m link sync stay inside the service (they are correctness, not policy).
- [ ] **Step 2: Write-path suites green (unit + both dialects, incl. `rows_hooks_test.go` adapted).**
- [ ] **Step 3: Commit** — `refactor(engine): write path on WriteService, audit decoupled`

### Task 6: engine — relations and import

**Files:**
- Create: `internal/engine/rels.go` (from `api/rels.go` + `api/rows.go` m2m helpers)
- Create: `internal/engine/importcsv.go` (from `api/import.go` pipeline)
- Test: adapted `rels_test.go`, `m2m_test.go`, `import_test.go`, `import_hooks_test.go`

**Interfaces:**
- Produces: `(s *ReadService) M2MOptions/M2MLinks`, `(s *WriteService) SyncM2MLinks`, `PreviewImport/ApplyImport(w, r, t, mode)` — same semantics, resolver-supplied junction defs.

- [ ] **Step 1: Move + adapt rels (fk value checks, m2m precheck/sync).**
- [ ] **Step 2: Move + adapt CSV import (preview/validate/apply) — hooks run through `Hooks` interface.**
- [ ] **Step 3: Rel+import suites green both dialects; commit** — `refactor(engine): relations + csv import`

### Task 7: hooks contract moves to defs-shaped context

**Files:**
- Create: `internal/engine/hookctx.go`
- Modify: `internal/hooks/hooks.go` (contract stays; context adapts), `internal/hooks/exec.go`, `internal/hooks/worker.go`
- Test: existing hooks suites

**Interfaces:**
- Produces (one contract, host-extendable):

```go
package hooks // after Task 8 this lives in core

type HookContext struct {
    Actor   string // injected by host via WithActor; "" when anonymous
    Table   *defs.Table
    Columns []defs.Column
    Open    func(name string) (ds.Adapter, error) // by definition name
    Logger  *slog.Logger
    Host    any // host-private payload (platform: *meta.Store); nil for library users
}
```

- [ ] **Step 1: Extend `HookContext` with `Actor`, `Host any`; replace `TableDef/Columns` with defs types and `Open(datasourceID)` with `Open(name)`.** Platform's executor builds the name→id resolution via its resolver. Update `docs/developer-hooks.md` — signature change is the one planned break for hook authors.
- [ ] **Step 2: hooks suites green; commit** — `refactor(hooks): defs-based HookContext with Actor/Host slots`

### Task 8: module split — `core/`

**Files:**
- Create: `core/go.mod` (`module github.com/luthfi9251/kucrud-core`, go 1.25)
- Move: `internal/defs` → `core/defs`, `internal/ds` → `core/ds`, `internal/engine` → `core/engine`, `internal/hooks` → `core/hooks` (outbox worker stays: `internal/hooks/worker.go` + `internal/meta/outbox.go` remain platform-side, importing core)
- Modify: root `go.mod` adds `require` + `replace github.com/luthfi9251/kucrud-core => ./core`; all imports rewired
- Test: entire suite (nothing new)

- [ ] **Step 1: `git mv` the four packages; fix imports (`ku-crud/internal/X` → `github.com/luthfi9251/kucrud-core/X`).**
- [ ] **Step 2: Full gate incl. integration.** Run: `go vet ./... && gofmt -l . && go test ./... -p 1` + both dialect env runs
- [ ] **Step 3: Commit** — `refactor(core): split engine into core module, platform consumes via replace`

### Task 9: `core/httpapi` + `core/app` — the library surface

**Files:**
- Create: `core/httpapi/httpapi.go`, `core/app.go`
- Test: `core/httpapi/httpapi_live_test.go` (self-skipping without `KUCRUD_TEST_PG`)

**Interfaces:**
- Produces (the public API from the spec §4):

```go
package kucrud // package kucrud at core root

type Def struct {
    Table       string
    Keys        []string
    Columns     []Override // declaration = override, not full listing
    DefaultSort SortSpec   // use Sort("created_at", core.Desc)
    Hooks       map[Event][]string // names in the registry
    Actions     string
    SourceType  string
    QuerySQL    string
    PageSize    int
}

type Override struct {
    Name                    string
    Label                   string
    Hidden                  bool
    Format                  string // formatting JSON, existing contract
    Validation              []defs.ValidationRule
    FK                      *defs.FK
    M2M                     *defs.M2M
    Editable, Required      bool
    Searchable, Sortable    *bool
}

type App struct{ /* mux, defs registry, resolver over registered defs */ }

func New(ds Conn, opts ...Option) (*App, error)
func (a *App) CRUD(path string, d Def) *App   // introspects defaults, applies overrides
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request)

// Gate is the single auth/RBAC slot: return non-nil to reject.
type Gate func(r *http.Request, op Op, table string) error
type Op string // read|create|update|delete|action|export|import

func WithGate(g Gate) Option

// Root-package aliases so users write core.FK / core.Sort, not core/defs.FK:
type (
    FK         = defs.FK
    M2M        = defs.M2M
    SortDir    = defs.SortDir
    Event      = hooks.Event
    HookFunc   = hooks.HookFunc
    RowPayload = hooks.RowPayload
)

func Sort(col string, dir defs.SortDir) SortSpec { return SortSpec{Col: col, Dir: dir} }
```

- [ ] **Step 1: Write the failing end-to-end test** — register one PG table with two overrides, drive full CRUD over `httptest`: list/sort/filter, insert (with before-hook mutation), update, delete, CSV export, 400 on validation failure, 403 through a Gate. Starts failing: `kucrud.New` undefined.
- [ ] **Step 2: Implement `httpapi` name-based routes** mounting engine services (`/api/data/{name}/rows...`, `GET /api/defs`) with the `Gate` called per op.
- [ ] **Step 3: Implement `App.CRUD`** — `InspectTable` fills default columns; overrides merge by `Name`; defs registry backs the engine `Resolver`.
- [ ] **Step 4: E2E test green (PG; MySQL variant reusing same harness with `KUCRUD_TEST_MYSQL`).**
- [ ] **Step 5: Commit** — `feat(core): public App/Def API with introspection defaults and Gate slot`

### Task 10: platform switchover — mount core, keep endpoints

**Files:**
- Modify: `internal/api/server.go` (mount `core/httpapi` under `/api/data/`), new `internal/api/defsource.go` (meta-backed `Resolver` + name lookup), `internal/api/rbac_gate.go` (maps `CtxUser` grants + `QUERY_READONLY` + v1.9 action grants to `core.Gate`)
- Modify: `web/src/lib/api.ts` (data-page base path `/api/tables/{id}` → `/api/data/{name}`; management pages untouched)
- Test: full `internal/api` suites unchanged-green (they exercise the platform endpoints end to end)

- [ ] **Step 1: Implement meta-backed Resolver + RBAC Gate.**
- [ ] **Step 2: Mount core routes; delete the now-dead platform row handlers** (`api/rows.go` bodies, `bulk.go`, `import.go`, `export.go` shrink to nothing — remove files).
- [ ] **Step 3: SPA path constant change; manual smoke via `make dev` (login, grid, form, kanban drag, csv, m2m picker).**
- [ ] **Step 4: FULL suite (unit + PG + MYSQL + `-p 1`).**
- [ ] **Step 5: Commit** — `refactor(platform): serves rows via core/httpapi, RBAC as Gate`

### Task 11: audit + outbox as hook implementations

**Files:**
- Create: `internal/hooks/platformhooks/audit.go` (AfterX → `meta.InsertAudit`, actor from `WithActor`)
- Modify: `internal/hooks/worker.go` (after-hooks enqueue to outbox as today; worker executes core `HookFunc` with Host-injected store)
- Test: adapted audit assertions from moved rows tests

- [ ] **Step 1: Audit-after-hook: re-add audit entries via assignment at platform bootstrap (every def gets the audit hook).**
- [ ] **Step 2: Outbox path green (`hooksapi_test.go`, `worker_test.go`, plus a rows write asserting audit row exists again).**
- [ ] **Step 3: Commit** — `feat(platform): audit + durable outbox as core hook implementations`

### Task 12: template starter

**Files:**
- Create: `template/` in-repo (split to its own repo post-validation):
  - `main.go` (see Appendix A), `go.mod` (requires core via replace for now),
  - `authstub/middleware.go` (deny-all with one TODO: wire host auth),
  - `web/` (thin React app: router + login placeholder + one CRUD page
    consuming `/api/data/{name}`; reuses core response shapes),
  - `Dockerfile`, `README.md` (10-line quickstart)
- Test: `template/smoke_test.go` — build + one httptest CRUD roundtrip with the stub gate returning 403.

- [ ] **Step 1: Author template files; smoke test green.**
- [ ] **Step 2: `git clone`-simulation check:** copy `template/` to `/tmp`, drop the replace for a vendored path build, confirm `go build ./...` works.
- [ ] **Step 3: Commit** — `feat(template): full-stack starter consuming core`

---

## Appendix A — end-state usage (target of Tasks 9+12)

Template `main.go`:

```go
package main

import (
    "log"
    "net/http"

    core "github.com/luthfi9251/kucrud-core"
)

func main() {
    app, err := core.New(core.Conn{
        Driver: "postgres", Host: "localhost", Port: 5432,
        DB: "shop", User: "ku", Password: "ku",
    })
    if err != nil {
        log.Fatal(err)
    }

    app.CRUD("/products", core.Def{
        Table: "products",
        Keys:  []string{"id"},
        Columns: []core.Override{
            {Name: "price", Label: "Harga", Format: `{"prefix":"Rp","thousands":true}`},
            {Name: "internal_note", Hidden: true},
            {Name: "category_id", FK: &core.FK{Table: "categories", RefColumn: "id",
                DisplayColumns: []string{"name"}}},
            {Name: "tags", M2M: &core.M2M{JunctionTable: "product_tags",
                SrcCol: "product_id", TgtCol: "tag_id",
                DisplayColumns: []string{"name"}}},
        },
        DefaultSort: core.Sort("created_at", core.Desc),
        Hooks:       map[core.Event][]string{"beforeCreate": {"normalizeSKU"}},
    })

    // Auth is the host's job — the template ships a deny-all stub.
    http.ListenAndServe(":8080", withAuthStub(app))
}
```

Platform-side audit recipe (what Task 11 ships):

```go
func AuditAfterWrite(ctx context.Context, hc *core.HookContext,
    ev core.Event, row core.RowPayload, cfg json.RawMessage) (core.RowPayload, error) {

    store := hc.Host.(*meta.Store) // platform injects; nil for library users
    return row, store.InsertAudit(&meta.AuditEntry{
        Username: core.ActorFrom(ctx),
        Action:   string(ev),
        Table:    hc.Table.Name,
        RowKey:   row.Key,
        NewJSON:  row.Values,
    })
}
```

## Appendix B — what is deliberately NOT in this plan

- `kucrud init` CLI (spec non-goal; `git clone template` suffices).
- GroceryCRUD converter (spec non-goal; pilot-first migration).
- Publishing/tagging `core/v0.x` — happens only after the company pilot (spec §5).
- Any SPA redesign — data pages keep today's components, only the fetch base path changes (Task 10).
