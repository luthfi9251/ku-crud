# Ku-CRUD Hooks — Developer Guide

> **Audience:** developers who write hooks and manage table definitions.
> **Status:** reflects the implementation on the `feature/v1.6` branch.
>
> Short, in-app reference: the **Hooks (automation)** section of the
> in-app Documentation page (Developer tab). This document is the full guide.

---

## 1. What a hook is

A hook is a **plain Go function** compiled into the Ku-CRUD binary. You write
it in the `hooks/` package, rebuild, and it becomes assignable to a CRUD
event on any table definition. When a table operation runs, Ku-CRUD calls
your function before and/or after the data change.

Hooks are the "run custom logic without building a separate service"
mechanism:

- **No network service, no extra binary** — the hook lives in the same
  process as the server.
- **Full type safety** — it is Go code compiled and unit-testable like the
  rest of your codebase.
- **Trusted code** — hooks run with the server's own privileges. They can
  open any registered datasource and read metadata. This is power, and it
  is why the guide below has a *Do / Don't* section.

### 1.1 Deployment model

Production still requires **rebuilding the binary** (same philosophy as the
rest of v1.6: "trusted compiled code"). There is no runtime plugin loading,
no hot reload of hook source in production.

Development loop:

```
write hooks/yourhook.go   →   make dev (or make build)   →   hook appears in
                             the table-definition editor dropdown
```

`make dev` and `make build` run `go generate ./hooks`, which regenerates
`hooks/registry_gen.go` via the AST scanner in `cmd/hookgen`. The generator
finds every top-level function whose signature matches `HookFunc`, so a new
hook appears automatically — **no manual registration step**.

If you run the binary directly with `go build ./cmd` (not via make), it
embeds whatever registry was generated last — run `go generate ./hooks`
first (or just use `make dev` / `make build`).

---

## 2. The hook contract

### 2.1 Signature

```go
func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event,
    row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error)
```

where `hooks` is the package `github.com/luthfi9251/ku-crud/core/hooks` (imported in
`hooks/example.go` as `kuhooks`).

Every hook must match this signature exactly. The `cmd/hookgen` scanner
only picks up functions with it — functions with different signatures are
ignored.

### 2.2 The six events

| Event | When it runs | Runs where |
|---|---|---|
| `beforeCreate` | before a row is inserted | in the request |
| `afterCreate` | after a row is committed | background worker |
| `beforeUpdate` | before a row is updated | in the request |
| `afterUpdate` | after a row is updated | background worker |
| `beforeDelete` | before a row is deleted | in the request |
| `afterDelete` | after a row is deleted | background worker |

`before*` hooks are **synchronous**: they run inside the HTTP request,
before the SQL statement. They can modify the payload or reject the write.
`after*` hooks are **asynchronous**: they are queued and run later on the
outbox worker; their result can never change the completed write.

### 2.3 The payload (`RowPayload`)

```go
type RowPayload struct {
    Values map[string]any // the new values (create/update). Empty for delete.
    Old    map[string]any // the pre-write row (update/delete). Nil on create.
}
```

| Event | `Values` | `Old` |
|---|---|---|
| `beforeCreate` / `afterCreate` | the new values | nil |
| `beforeUpdate` / `afterUpdate` | the new values | the row as it was before |
| `beforeDelete` / `afterDelete` | empty | the row being deleted |

Notes:

- Values are the raw JSON-decoded values (`float64` for numbers, `string`
  for text/datetime/enum/uuid, `bool`, nested `map[string]any` for JSON
  columns). This matches what `database/sql` yields — the same shape you
  see in grid rows.
- `after*` hooks receive a **snapshot** taken at enqueue time:
  - create → the payload that was inserted (after any `before*`
    mutations);
  - update → the old row merged with the new values (computed columns are
    *not* re-read);
  - delete → the deleted row.
  This snapshot is what the hook must work with — do not expect to be
  able to re-fetch a deleted row later.

### 2.4 The context (`HookContext`)

```go
type HookContext struct {
    Actor   string                                     // acting user's name; "" when anonymous
    Table   *defs.Table                                // the definition the hook is assigned to
    Columns []defs.Column                              // its columns
    Open    func(name string) (ds.Adapter, error)      // open a datasource by definition name
    Logger  *slog.Logger                               // server logger
    Host    any                                        // host-private payload (platform: *meta.Store)
}
```

> **Breaking change (core extraction):** the context previously carried
> `User ActingUser`, `TableDef *meta.TableDef`, `Columns []meta.ColumnDef`,
> `Open(datasourceID int64)` and `Store *meta.Store`. It now uses the
> ID-free `defs` contract types, opens datasources **by definition name**,
> and exposes the acting user as `Actor` plus the host store as `Host`.
> Update any hook reading `hc.User`, `hc.TableDef`, `hc.Store` or calling
> `hc.Open(id)`.

- **`Actor`** is the acting user's username (injected by the host via
  `hooks.WithActor`; `""` when anonymous). It is also readable from the
  context with `hooks.ActorFrom(ctx)`. For `after*` hooks the actor is
  the user who made the write — the outbox snapshots it at enqueue time,
  long after their session is gone.
- **`Open`** gives you a live connection to the datasource of any
  registered **table definition, by name** (e.g. `hc.Open("customers")`).
  **You must `Close()` the adapter when done.** The definition's own
  datasource is `hc.Open(hc.Table.Name)`.
- **`Table` / `Columns`** are the ID-free definition contract
  (`github.com/luthfi9251/ku-crud/core/defs`): names, types, validations, FK/M2M links by
  target *name* — never internal integer ids.
- **`Host`** is a host-private payload. On this platform it is the
  SQLite metadata store (`hc.Host.(*meta.Store)`); for library users it
  may be nil. Treat it as trusted, host-specific extension surface.
- **`Logger`** — use it instead of `fmt.Printf` so log lines carry
  structured fields and the server's log level.

### 2.5 The config (`cfg json.RawMessage`)

Each assignment on a table definition carries an **optional JSON config**
that is passed to the hook verbatim:

```json
{"beforeCreate": [{"hook": "NormalizePrice", "config": {"decimals": 2}, "order": 1}]}
```

Decode it inside the hook with `json.Unmarshal` into a struct; guard for
malformed/absent config:

```go
var c struct{ Decimals int }
if len(cfg) > 0 {
    _ = json.Unmarshal(cfg, &c) // never trust config
}
```

Config is **not validated** against the hook at assignment time beyond
"is valid JSON" — your hook is responsible for handling any shape. Prefer
a zero-value default (as `NormalizePrice` does: defaults to 2 decimals).

### 2.6 Execution order

Assignments for one event run **in `order` ascending** (ties keep list
order). Each `before*` hook sees the payload as the previous one left it —
a pipeline. Example: two `beforeCreate` hooks, `order` 1 then 2; hook 2
sees hook 1's modifications.

---

## 3. What you can do in a `before*` hook

`before*` hooks have two powers:

### 3.1 Modify values

Return a modified `RowPayload`. The changed `Values` become the payload for
the actual insert/update:

```go
func NormalizePrice(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
    row kuhooks.RowPayload, cfg json.RawMessage) (kuhooks.RowPayload, error) {
    // ...round row.Values["price"]...
    return row, nil
}
```

**Constraints:**

- You may change **existing** column values or add values for known
  editable columns. The modified payload goes back through the same
  column-validation path (`editablePayload`): unknown column names are
  rejected, and values are re-validated against the column type, enum
  options, and per-column validation rules. So a mutation must still
  produce a *valid* value for that column — that is the intended safety
  net.
- You **cannot** add a column that does not exist in the definition.
- On **delete** events there is nothing to modify — only reject.

### 3.2 Reject the write

Return a non-nil error. The request is aborted with:

- HTTP `400`, code `HOOK_REJECTED`, message = your error text.

The SQL statement never runs; nothing is written. The audit trail records
nothing for the rejected operation (the rejection happens before the write).

```go
func RejectClosedStatus(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
    row kuhooks.RowPayload, cfg json.RawMessage) (kuhooks.RowPayload, error) {
    if row.Values["status"] == "closed" {
        return row, errors.New("cannot create a row in closed status")
    }
    return row, nil
}
```

### 3.3 Side effects (with care)

A `before*` hook *can* perform side effects (open an adapter, write
somewhere, send a notification) — but a `before*` hook that fails *after*
doing a side effect leaves the side effect in place while the write is
rejected. **Prefer `after*` hooks for side effects** (Section 4), because
the outbox gives you retry. If you must do side effects in a `before*`
hook, make them idempotent and understand they are not rolled back when
you reject.

---

## 4. `after*` hooks and the outbox

`after*` hooks are **not** executed inside the request. The write path
enqueues one entry **per assignment** into a durable SQLite outbox
(`hook_outbox`); the background worker drains it.

### 4.1 Why asynchronous

- The request stays fast — a slow hook never blocks the user.
- The outbox is **durable** — an entry survives a crash between commit and
  hook execution.
- Failed hooks are **retried automatically** with backoff.

### 4.2 Retry & failure policy

| Attempt | Wait after failure |
|---|---|
| 1st | 30 s |
| 2nd | 2 min |
| 3rd | 10 min |
| 4th | 1 h |
| 5th | 4 h |
| 6th | **dead** |

- `MissingError` (hook not in this binary) → entry dies **immediately** —
  retrying can never succeed.
- The worker polls every 5 s, claiming the oldest due entries (batch of 10).
- A dead entry is only re-run via **manual retry** on the Hook Outbox page
  (or `POST /api/hooks/outbox/{id}/retry`).

### 4.3 What the outbox stores

Per entry: `table_def_id`, `event`, `hook_name`, `config`, `old_values`,
`new_values` snapshots, the **acting user** (id + username, snapshot at
enqueue time so the hook knows who triggered it even after the session is
gone — the hook context surfaces it as `Actor`), `status`
(`pending | done | dead`), `attempts`, `next_retry_at`, `last_error`.

### 4.4 Monitoring

The **Hook Outbox** page (sidebar → Hook Outbox, Platform Management grant)
lists entries with a status filter, shows last error and attempts, and lets
you **retry** failed/dead entries manually.

---

## 5. Which write paths fire hooks

**Every** write path fires the same hooks — that was a v1.6 design
requirement ("consistency of triggers"). Kanban drag is not a special case:
it calls the same row-update endpoint, so update hooks fire.

| Path | Events |
|---|---|
| Grid form: new row | `beforeCreate`, `afterCreate` |
| Grid form: edit row | `beforeUpdate`, `afterUpdate` |
| Grid form: delete row | `beforeDelete`, `afterDelete` |
| CSV import — **preview** | `beforeCreate` (validation only) |
| CSV import — **apply** | `beforeCreate` (per row), `afterCreate` (batch) |
| Bulk delete | `beforeDelete` (per row), `afterDelete` (batch) |
| Kanban drag | `beforeUpdate`, `afterUpdate` |
| m2m link add/remove (parent form) | junction definition's `beforeCreate`/`afterCreate` / `beforeDelete`/`afterDelete` |

### 5.1 CSV import specifics

- **Preview** runs `beforeCreate` hooks per row in *validation-only* mode:
  a hook rejection marks the row invalid in the preview (message prefixed
  `hook: `). Mutations in preview are **discarded** — preview is read-only.
- **Apply** runs `beforeCreate` exactly once per row, on the final typed
  payload, **before** validation. Mutations become part of the inserted
  values. A hook rejection records a per-row failure (the row is not
  inserted) and other rows continue. `afterCreate` hooks are enqueued after
  the whole batch completes, one entry per inserted row.
- **Consequence:** a hook with side effects in `beforeCreate` runs once per
  row during apply *and* once per row during preview. Keep `before*` hooks
  free of side effects (Section 3.3) or make them idempotent.

### 5.2 m2m specifics

- When a user adds/removes links on a parent form, the **junction table's
  own definition** hooks fire — not the parent's. So put m2m automation on
  the junction table definition.
- Link payloads are `{srcCol: srcVal, tgtCol: targetVal}`.
- m2m link sync runs hooks with `context.Background()` (no request
  context); each hook still gets its own 5 s timeout.
- A hook rejection on a link change aborts that link change with
  `400 HOOK_REJECTED` / `400 HOOK_MISSING`. Note the parent row write
  itself has already committed at that point (link sync happens after) —
  the *link* is rejected, not the row.

### 5.3 Update-path caveat (known limitation)

On the row-update path, `beforeUpdate`/`afterUpdate` hooks receive
`oldRows[0]` as `Old`. In practice key columns are unique and there is
exactly one old row; with **non-unique key columns** a single update may
touch several rows but the hook only sees/guards the first. Prefer key
columns that identify a single row.

---

## 6. The missing-hook policy

If a definition references a hook name that is **not** compiled into the
current binary (hook renamed, deleted, or the metadata was imported from
another environment):

- **Every write to that table is rejected** with HTTP `400`, code
  `HOOK_MISSING`, message `hook <name> is not registered in this binary`.
- In the outbox, an entry for a missing hook dies immediately.
- Definitions can still be **imported** (metadata transfer) even when the
  hook is absent — the drift only surfaces as a rejected write, never
  silently. Fix by adding/renaming the hook in `hooks/` and rebuilding, or
  removing the assignment.

**This is why rename breaks assignments**: the hook name *is* the Go
function name. Renaming `NormalizePrice` to `NormalizeProductPrice` is a
**new hook**; existing assignments still point at `NormalizePrice` and will
reject writes until reassigned.

---

## 7. Error codes

| Code | Status | Meaning |
|---|---|---|
| `HOOK_REJECTED` | 400 | a `before*` hook returned an error (write aborted) |
| `HOOK_MISSING` | 400 | assignment references a hook not in this binary |

Panics inside a hook are recovered and turned into `HOOK_REJECTED`
(`"hook panicked: ..."` for `before*`) or a retryable worker error
(`after*`) — a panicking hook **never crashes the server**.

---

## 8. Timeouts & execution guarantees

| Hook kind | Timeout |
|---|---|
| `before*` (synchronous) | **5 s** per hook |
| `after*` (worker) | **30 s** per hook |

- Timeout is enforced with `context.WithTimeout`. **Your hook must respect
  `ctx`**: long-running work should `select` on `ctx.Done()`, and network
  calls should use the context. A hook that blocks in a syscall/deadlock
  can stall the request (before) or tie up the single worker (after).
- A `before*` hook that hits its timeout is treated as a rejection
  (`HOOK_REJECTED`, message includes the context deadline error).
- `after*` hooks that time out are retried per the outbox policy.

---

## 9. Good use cases

**`before*` (validation / normalization / policy):**

- Cross-field validation that per-column rules can't express (e.g. "end
  date must be after start date").
- Normalization before storage: trim whitespace, round prices, uppercase
  codes, default a column from another.
- Business-rule guards: block status transitions, block writes on
  read-only records, enforce "no negative stock" at the application layer.
- Auto-set auditing columns from `hc.Actor` (e.g. `updated_by`).

**`after*` (side effects, via the durable outbox):**

- Notifications / webhooks / emails when a row changes.
- Writing derived data elsewhere (denormalized counters, search index
  entries) — the outbox retries make these reliable.
- Integration with external systems that may be briefly unavailable — the
  backoff+retry is on your side.
- Triggering a follow-up process (queue a job, write a log row).

**Pattern: validate in `before*`, act in `after*`.** A cross-field check
belongs in `beforeCreate` (fast, blocking); the notification belongs in
`afterCreate` (reliable, non-blocking).

---

## 10. Do's and Don'ts

### Do

- **Do** respect `ctx` and keep `before*` hooks under ~seconds — a slow
  hook is a slow request for every user.
- **Do** make side-effectful hooks **idempotent**. `after*` hooks can be
  retried; your hook may run more than once. Guard with a unique key /
  upsert / "already processed" check.
- **Do** `Close()` every adapter you open via `hc.Open`.
- **Do** use `hc.Logger` for diagnostics.
- **Do** use per-column validation rules + `before*` hooks as a layered
  defense (rules first, hooks for cross-field logic).
- **Do** default config values when `cfg` is empty or malformed.
- **Do** unit-test your hook functions like any Go code — they are plain
  functions.
- **Do** remember the deploy model: hook changes require rebuild + restart,
  and hook renames are breaking.

### Don't

- **Don't** write secrets or connection credentials into hook code that
  lives in a repo readable by non-operators — the hook runs with full
  privileges, so the code *is* a credential surface.
- **Don't** do one-off side effects in `before*` hooks that aren't
  idempotent — a later rejection leaves them behind.
- **Don't** run DDL from a hook. The product promise is "no DDL"; a hook
  doing `CREATE TABLE` bypasses every safeguard and assumption.
- **Don't** mutate `Old` and expect it to matter — only `Values` feeds the
  write.
- **Don't** make `before*` hooks depend on data you fetched inside the hook
  *after* the payload was validated; a race can slip an invalid value past.
- **Don't** swallow errors: returning `nil` from a `before*` hook after a
  failed check silently lets bad data through. Return the error to reject.
- **Don't** block forever. Honor the timeout.
- **Don't** assume `after*` hooks run exactly once or promptly — they are
  asynchronous with retries; the side effect may be delayed by the backoff
  schedule, or land after a manual retry.

---

## 11. Limitations (current implementation)

- **Rebuild to ship**: no hot-reload of hook code in production.
- **Config is free-form JSON**: no per-hook schema validation; the hook
  must defend against bad config.
- **`after*` snapshot, not live data**: computed columns are not re-read
  for after-hooks; for delete the row is gone at execution time.
- **m2m selections invisible to hooks**: the parent write's m2m *link
  selections* are stripped before hooks run — hooks cannot see or mutate
  which m2m links a request is setting. Junction *writes* still fire the
  junction definition's hooks.
- **Update with non-unique keys**: hooks see only `oldRows[0]` (Section 5.3).
- **Outbox enqueue is best-effort**: if the SQLite insert of the outbox
  entry itself fails, the write still commits and the side effect is lost
  (logged as a warning) — same policy as audit writes.
- **One worker per process**: `after*` hooks run sequentially on a single
  worker; a long hook delays the next entry. No horizontal scaling within
  one binary.
- **`after*` timeout is cooperative**: a hook that ignores `ctx` can still
  tie up the worker (Section 8).

---

## 12. API reference

| Endpoint | Gate | Purpose |
|---|---|---|
| `GET /api/hooks` | Platform | list registered hook names (editor dropdown) |
| `GET /api/hooks/outbox?status=&tableDefId=&page=` | Platform | outbox monitor (masked ids) |
| `POST /api/hooks/outbox/{id}/retry` | Platform | manual retry of a failed/dead entry |

A definition's hook assignments live in its `hooks` JSON field (object
keyed by event; each event is an ordered array of `{hook, config, order}`).
They are validated at definition save (unknown hook → 400) and every write
(hook not in binary → `HOOK_MISSING`).

---

## 13. Reference: writing your first hook

```go
// hooks/mynormalize.go
package hooks

import (
    "context"
    "encoding/json"
    "strings"

    kuhooks "github.com/luthfi9251/ku-crud/core/hooks"
)

// TrimName is a before-create hook: trims whitespace on the "name" column
// and rejects empty names.
func TrimName(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
    row kuhooks.RowPayload, cfg json.RawMessage) (kuhooks.RowPayload, error) {
    name, ok := row.Values["name"].(string)
    if !ok {
        return row, nil
    }
    name = strings.TrimSpace(name)
    if name == "" {
        return row, errors.New("name cannot be blank")
    }
    row.Values["name"] = name
    return row, nil
}
```

Then:

1. `make dev` (or `make build`) — regenerates `hooks/registry_gen.go`.
2. Restart the server — `GET /api/hooks` lists `TrimName`.
3. In the table definition editor → **Automation Hooks** → **Before create**
   → **Add hook** → select `TrimName`.
4. Save. New rows now pass through `TrimName`.

Check the built-in examples in `hooks/example.go` (`NormalizePrice`,
`LogAfterCreate`) for the exact import style and config handling.
