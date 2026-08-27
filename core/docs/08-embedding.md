# 08 — Embedding the engine

Most consumers take the `App` (or `httpapi.Resource` with injected services). This
page covers the seam *below* HTTP: driving the engine services directly, the CSV
import pipeline as reusable pieces, and how to test code built on the library.

## The Resolver seam

Everything in the engine starts from one interface:

```go
type Resolver interface {
    Adapter(t *defs.Table) (ds.Adapter, error)
    Resolve(name string) (*defs.Table, error) // "" = self, handled by the caller
}
```

Implement it over anything — a static map (what `App` does), runtime-mutable
metadata (what the platform does), a multi-connection catalog. Errors map to the
sentinels `engine.ErrDSNotFound` (→ 404) and `engine.ErrConn` (→ 502) when services
write HTTP responses.

## Services without HTTP

`ReadService`, `WriteService` and `ImportService` are plain structs whose methods
take `(w http.ResponseWriter, r *http.Request, t *defs.Table)` — handler-shaped for
`httpapi`, but usable from any handler you write (including non-`httpapi` routing, gRPC
gateways, background jobs that synthesize requests):

```go
svc := &engine.ReadService{
    R:       myResolver,
    FKJoin:  nil,                  // nil → fk display-value filters rejected
    CanRead: func(name string) bool { return true },
}

req := httptest.NewRequest("GET", "/rows?page=1&filters="+url.QueryEscape(
    `[{"column":"status","op":"eq","values":["paid"]}]`), nil)
req.SetPathValue("pk", engine.EncodeRowKey([]string{"12"})) // for Get
w := httptest.NewRecorder()
svc.List(w, req, table)
```

Full method surface: `ReadService.List/Get/ExportCSV/Stats/FKOptions/M2MOptions/M2MLinks`,
`WriteService.Insert/Update/Delete/BulkDelete`,
`ImportService.PreviewImport/ApplyImport` — semantics per route in
[04 — HTTP API](04-http-api.md).

When you want the *values* rather than HTTP responses — batch jobs, alternative
transports — go one layer down and call `ds.Adapter` through your resolver directly;
the `engine` helpers that make that safe are exported too:

```go
// validation + payload extraction exactly as the write path does
cols, vals, err := engine.EditablePayload(t, body, isInsert)

// row-key encoding shared with the HTTP surface
key := engine.EncodeRowKey([]string{"12", "x"})       // → WyIxMiIsIngiXQ (URL-safe)
raw, err := engine.DecodeKey(t, key)                   // → coerced []any
auditKey, err := engine.EncodeKey(t, rowMap)           // → `["12","x"]` JSON string

// computed columns in memory
engine.ApplyComputed(t.Columns, rows)

// m2m topology cross-check
cfg, msg := engine.ResolveM2M(resolver, t, col) // msg != "" ⇒ configuration problem
```

## Reusable pure pieces

The engine's building blocks are exported and independently useful:

- **`engine.ParseFilters(t, raw, fkJoin)`** — validate + coerce the wire filter JSON
  against a def (op matrix, 10-filter cap, `in` 1..50, date upper-bound expansion).
- **`engine.ResolveSort(t, col, dir)`** — explicit → def default → first key → first
  sortable; returns `("","")` when nothing sorts.
- **`engine.ValidateColumn(c, v)`** — per-type checks + validation rules; the same
  funnel used by writes and CSV import.
- **`engine.CompileComputed(c, cols)`** — parse + type-check a computed formula; returns
  the output type and an evaluator.
- **`csvutil.ParseCSV(data)`** — sniff delimiter (comma/semicolon/tab), parse, cap at
  10 000 data rows.
- **`csvutil.AutoMap(headers, cols)`** — propose header→column mapping (exact name →
  trimmed lowercase name → lowercase label; m2m not importable).
- **`csvutil.CoerceValidate(c, raw)`** — one CSV cell → typed value + rules (booleans
  accept `t/f/1/0/yes/no`, datetimes ISO-like, json compacted).
- **`engine.ImportMaxFile`** — 5 MB import cap constant.

The import endpoints are just orchestration over these: parse → auto-map →
per-row `CoerceValidate` (+ before-hooks, batch fk verification) → preview report or
insert loop with per-row failures.

## Testing patterns

The library's own tests model three approaches, all available to consumers:

**1. Fake resolver + fake adapter (unit).** Embed `ds.Adapter` in a struct and
override only what your test touches — any unexpected call panics through the nil
interface:

```go
type fakeAdapter struct {
    ds.Adapter                                  // panics on unexpected use
    listRows  func(ds.ListParams) ([]map[string]any, error)
    countRows func(ds.ListParams) (int, error)
}
func (f *fakeAdapter) ListRows(p ds.ListParams) ([]map[string]any, error) { return f.listRows(p) }
func (f *fakeAdapter) CountRows(p ds.ListParams) (int, error)             { return f.countRows(p) }
func (f *fakeAdapter) Close() error                                         { return nil }

type fakeResolver struct {
    tables  map[string]*defs.Table
    adapter func(*defs.Table) (ds.Adapter, error)
}
func (f *fakeResolver) Adapter(t *defs.Table) (ds.Adapter, error) { return f.adapter(t) }
func (f *fakeResolver) Resolve(name string) (*defs.Table, error)  { return f.tables[name], nil }
```

Drive services with `httptest.NewRequest`/`httptest.NewRecorder` and assert on the
recorded response — no database needed for routing, validation, gating or
enrichment logic.

**2. SQLite-free store tests.** Not applicable to the library itself (it has no
persistence) — but the pattern from the platform applies to your metadata layer.

**3. Live database tests (self-skipping).** Integration tests run against real
Postgres/MySQL when `KUCRUD_TEST_PG` / `KUCRUD_TEST_MYSQL` DSNs are set, and skip
otherwise:

```go
func openPG(t *testing.T) ds.Adapter {
    t.Helper()
    dsn := os.Getenv("KUCRUD_TEST_PG")
    if dsn == "" {
        t.Skip("KUCRUD_TEST_PG not set")
    }
    a, err := ds.Open(ds.Conn{Raw: dsn})
    if err != nil { t.Skipf("no PG: %v", err) }
    t.Cleanup(func() { a.Close() })
    return a
}
```

Seed over a raw `*sql.DB` (`sql.Open("pgx", dsn)` / `sql.Open("mysql", dsn)`), then
assert through the `Adapter`. In CI, point the env vars at throwaway databases
(`postgres:…-alpine` / `mysql:…` containers); locally they stay unset and the suite
is fast and hermetic.

**4. Full-stack smoke over the App.** Register a def against the live test DSN and
exercise the mounted handler with `httptest.NewServer(app)` — the monorepo's
`template/smoke_test.go` is a complete blueprint (boot schema, register, hit every
route, assert shapes).
