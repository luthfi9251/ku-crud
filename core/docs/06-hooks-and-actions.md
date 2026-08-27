# 06 — Hooks & actions

Hooks are compile-time Go functions attached to table events by name, with optional
per-assignment config JSON. Custom actions (v1.9) extend the same registry into
row-level operations surfaced by host UIs. The contract is host-owned persistence:
core executes what is registered, and the *host* decides how (synchronously in-request
vs via an outbox worker).

## The hook function

```go
type HookFunc func(ctx context.Context, hc *HookContext, ev Event,
    row RowPayload, cfg json.RawMessage) (RowPayload, error)
```

- `ctx` — the request context on the request path (carrying the actor, below); a
  background context when a worker replays after-hooks.
- `hc` — host-level access: the def, its columns, a lazy datasource opener, a logger,
  and a host-private slot.
- `ev` — which event fired.
- `row` — the write payload; the **returned** payload continues the pipeline.
- `cfg` — the assignment's config JSON (raw; absent for no config).

A before-hook may rewrite `row.Values`; an error fails the write with
`400 HOOK_REJECTED` (message from the error).

```go
func NormalizePrice(ctx context.Context, hc *kucrud.HookContext,
    ev kucrud.Event, row kucrud.RowPayload, cfg json.RawMessage) (kucrud.RowPayload, error) {

    var c struct{ Decimals int `json:"decimals"` }
    json.Unmarshal(cfg, &c) // zero config → decimals 0
    if v, ok := row.Values["price"].(float64); ok {
        shift := math.Pow(10, float64(c.Decimals))
        row.Values["price"] = math.Round(v*shift) / shift
    }
    return row, nil
}
```

## Events & payloads

```go
const (
    BeforeCreate Event = "beforeCreate"
    AfterCreate  Event = "afterCreate"
    BeforeUpdate Event = "beforeUpdate"
    AfterUpdate  Event = "afterUpdate"
    BeforeDelete Event = "beforeDelete"
    AfterDelete  Event = "afterDelete"
    OnAction     Event = "onAction" // custom actions only; not assignable via Def.Hooks
)
```

```go
type RowPayload struct {
    Values  map[string]any // the new payload (empty map on delete)
    Old     map[string]any // the pre-write row; nil on create
    Message string         // set by onAction hooks; ignored by before/after
}
```

Lifecycle per op (order matters):

```
INSERT : Guard → BeforeCreate (may rewrite; result re-validated) → SQL insert
         → [RunSyncAfter] → AfterCreate
UPDATE : Guard → fetch old → BeforeUpdate(Values=new, Old=old) → SQL update
         → [RunSyncAfter] → AfterUpdate
DELETE : Guard → inbound-ref check → BeforeDelete(Old=old) → SQL delete
         → [RunSyncAfter] → AfterDelete(Old=old)
M2M    : each added link = BeforeCreate/AfterCreate on the JUNCTION def;
         each removed link = BeforeDelete/AfterDelete on the junction def
IMPORT : exactly one before/after pair per row, fired at insert time
```

Timeouts: before-hooks 5 s (in-request), after-hooks 30 s (worker), actions 15 s.

## HookContext

```go
type HookContext struct {
    Actor   string                              // hooks.ActorFrom(ctx), "" anonymous
    Table   *defs.Table                         // the def this event fires on
    Columns []defs.Column
    Open    func(name string) (ds.Adapter, error) // open another def's datasource; hook must Close
    Logger  *slog.Logger
    Host    any                                 // host-private (platform: *meta.Store); nil in the library
}
```

Thread the actor through contexts:

```go
ctx = hooks.WithActor(r.Context(), username) // middleware
actor := hooks.ActorFrom(ctx)                // inside hooks
```

`Open(name)` resolves a datasource *by definition name* — the sanctioned way for a
hook to touch the database beyond its own table. The adapter is per-use: `Close` it.

## Registry & hookgen

```go
var Default = hooks.NewRegistry() // package-level default
hooks.Register("NormalizePrice", NormalizePrice) // adds to Default

reg := hooks.NewRegistry()
reg.Register("Name", fn)   // duplicate name → error
reg.Get("Name")            // (HookFunc, bool)
reg.Names()                // sorted
```

In the monorepo, `cmd/hookgen` scans the host's `hooks/` package for functions with
the `HookFunc` signature and generates `hooks/registry_gen.go`:

```go
func init() {
    kuhooks.Register("NormalizePrice", NormalizePrice)
    kuhooks.Register("LogAfterCreate", LogAfterCreate)
}
```

…so the hook name is always the Go function name. Point the App at a different
registry with `kucrud.WithHookRegistry(reg)`.

## Assignments

`Def.Hooks` (and the platform's stored `hooks` JSON) maps events to ordered hook
names with optional config:

```go
app.CRUD("/api/data/products", kucrud.Def{
    Table: "products",
    Hooks: map[kucrud.Event][]string{
        kucrud.BeforeCreate: {"NormalizePrice"},
        kucrud.AfterCreate:  {"LogAfterCreate"},
    },
})
```

The wire/storage form is JSON (`Assignment.Hook`, `.Config`, `.Order`; lists sorted by
`Order`):

```json
{"beforeCreate": [{"hook": "NormalizePrice", "config": {"decimals": 2}, "order": 0}]}
```

`Registry.CheckMissing` verifies every assigned name exists in the binary —
assignments referencing hooks that aren't compiled in fail with
`hooks.MissingError` and requests reject with `400 HOOK_MISSING` (a host upgrading
without a hook compiled in surfaces a clear error, not silent skips).

Execution helpers (used by `httpapi`'s default adapter and hosts):

```go
reg.RunBefore(ctx, ev, assignments, hc, row) (RowPayload, error) // threads payload through, panic-safe
reg.RunOne(ctx, hc, ev, row, assignment) error                   // worker path for one after-hook
reg.RunAction(ctx, hc, row, assignment) (string, error)          // action hook; returns its message
```

## How core runs hooks vs how the platform does

The **library default** (`httpapi`'s internal hook adapter):

- `Guard` = `CheckMissing`
- `RunBefore` synchronous with the request context (actor threaded)
- `RunAfter` synchronous, post-commit, best-effort — an after-hook error is logged,
  never fails the request

The **platform** injects `Services` with its own `engine.Hooks` implementation:

- `RunBefore` executes compiled-in before-hooks in-request
- `RunSyncAfter` (`engine.SyncAfterHooks`) writes the audit trail synchronously
  inside the request — audit is a core hook implementation, assigned to every def by
  adapter construction, never persisted metadata
- `RunAfter` enqueues one durable outbox entry per after-hook assignment
  (post-commit, best-effort); `internal/hooks/worker.go` drains it with retries and
  executes via `RunOne`

The `SyncAfterHooks` seam exists precisely for "must complete inside the request,
must never fail it" side effects.

```go
type Hooks interface {
    Guard(t *defs.Table) error
    RunBefore(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error)
    RunAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload) error
}
type SyncAfterHooks interface {
    Hooks
    RunSyncAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload, rowKey string)
}
```

## Custom actions (v1.9)

Actions extend a def's UI with per-row operations backed by `OnAction` hooks. Core
parses and exposes them; **dispatch is host-owned** (`OpAction` is reserved — no core
route executes actions yet).

```go
app.CRUD("/api/data/invoices", kucrud.Def{
    Table:   "invoices",
    Actions: `{
      "hidden": ["copy", "import"],
      "custom": [
        {"id": "send", "label": "Send invoice", "confirm": "Email this invoice?",
         "grant": "update", "hook": "SendInvoice", "config": {"cc": "billing@x"},
         "order": 0, "style": "primary"},
        {"id": "void", "label": "Void", "grant": "update", "hook": "VoidInvoice", "style": "danger"}
      ]
    }`,
})
```

```go
type CustomAction struct {
    ID      string          // 1-64 chars [a-zA-Z0-9_-], unique per def
    Label   string          // required
    Confirm string          // optional confirmation prompt
    Grant   string          // read|create|update|delete — the grant execution requires
    Hook    string          // required, must be registered
    Config  json.RawMessage
    Order   int
    Style   string          // neutral|primary|danger (default neutral)
}
type ActionsConfig struct {
    Hidden []string       // subset of newRow,edit,delete,copy,import,export,refresh
    Custom []CustomAction // sorted by Order
}
```

Host dispatch pattern (the platform's `POST /api/data/{name}/rows/{key}/action`):
authenticate → verify the `Grant` class for the caller → look up the action by id →
load the row → `RunAction` (15 s) → return the hook's message. `RunAction` does **not**
write modified values back — an action that should persist changes does its own
writes (e.g. via `hc.Open`).

Full platform-side documentation: [`docs/developer-hooks.md`](../../docs/developer-hooks.md)
in the monorepo.
