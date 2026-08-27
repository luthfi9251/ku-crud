# 05 — Authorization

kucrud-core has exactly **one** built-in authorization concept: the `Gate`. Everything
else — sessions, tokens, RBAC tables, tenant scoping — is yours, and the library
provides well-defined seams to plug richer policies into the engine services
themselves.

## The Gate

```go
type Gate func(r *http.Request, op Op, table string) error
```

Called before every data-route op. Return `nil` to allow; any error becomes
`403 FORBIDDEN` with the error's message. `table` is the definition *name* (the
resource name you registered); `op` is the operation class:

```go
const (
    OpRead   Op = "read"    // GET rows, rows/{key}, stats, relation pickers
    OpCreate Op = "create"  // POST rows, import
    OpUpdate Op = "update"  // PUT rows/{key}
    OpDelete Op = "delete"  // DELETE rows/{key}, bulk-delete
    OpAction Op = "action"  // reserved: no core route dispatches it yet
    OpExport Op = "export"  // GET rows/export
    OpImport Op = "import"  // import preview/apply
)
```

Typical RBAC gate (the shape the Ku-CRUD platform uses):

```go
func gateFor(u User) kucrud.Gate {
    return func(r *http.Request, op kucrud.Op, table string) error {
        if u.IsAdmin {
            return nil
        }
        grants := lookupGrants(u.Role, table) // your tables
        switch op {
        case kucrud.OpRead, kucrud.OpExport:
            if !grants.Read { return fmt.Errorf("no read access to %s", table) }
        case kucrud.OpCreate, kucrud.OpImport:
            if !grants.Create { return fmt.Errorf("no create access to %s", table) }
        case kucrud.OpUpdate:
            if !grants.Update { return fmt.Errorf("no update access to %s", table) }
        case kucrud.OpDelete:
            if !grants.Delete { return fmt.Errorf("no delete access to %s", table) }
        }
        return nil
    }
}

app, _ := kucrud.New(conn, kucrud.WithGate(gateFor(currentUser)))
```

Notes:

- A **nil gate allows everything**. The starter template ships a deny-all stub on
  purpose — replace `authstub.Gate`.
- Ordering: on query-view defs the `QUERY_READONLY` write guard fires **before** the
  gate (so a read-only def never leaks write-op outcomes); otherwise the gate is the
  first check.
- `WithGate` applies to resources created after the option runs — pass it to `New`.
  The gate is read per request (so a `Resource` built later still observes a gate
  mutated through future options), and each `App.Resource` call snapshots gate +
  registry at registration time.
- `GET /api/defs` probes the gate per definition to compute the `permissions` object,
  so listings reflect caller access.

## What the gate does not cover

The gate is per *request against one definition*. Three relation-level policies need
finer control:

1. **FK filter joins** — filtering an fk column by the target's display text requires
   the caller to read the target. In the default wiring (`App`), `Resource.fkJoin`
   resolves joins without permission checks (core has no per-target grants); a
   misconfigured fk column is the only failure.
2. **Relation visibility** — `rels`/`m2mRels` enrichment and the picker endpoints
   leak target rows unless checked.
3. **Junction writes** — m2m link sync inserts/deletes rows in the junction table;
   some hosts need separate grants for that.

The engine services expose callbacks for exactly these; they are wired through the
advanced path.

## The advanced path: injecting services

`httpapi.Options.Services` replaces the default engine wiring wholesale — for hosts
whose resolver spans multiple connections, whose fk joins are permission-checked,
whose hook execution is asynchronous (outbox), or whose metadata is runtime-mutable
(the platform rebuilds every def per request):

```go
type Options struct {
    Gate     Gate
    Registry *hooks.Registry
    Services func(r *http.Request, t *defs.Table) ServiceSet
}

type ServiceSet struct {
    Read   *engine.ReadService
    Write  *engine.WriteService
    Import *engine.ImportService
}
```

A full platform-style wiring:

```go
// Resolver over runtime-mutable metadata (one def per request).
type metaResolver struct{ /* maps name → *meta.TableDef */ }

func (m *metaResolver) Adapter(t *defs.Table) (ds.Adapter, error) { /* open by def */ }
func (m *metaResolver) Resolve(name string) (*defs.Table, error)  { /* name → defs.Table */ }

func readService(u User, res engine.Resolver) *engine.ReadService {
    return &engine.ReadService{
        R:      res,
        FKJoin: func(column string) (*ds.FKJoin, error) {
            // resolve the join target AND check the caller's read grant on it:
            // returning an error rejects the filter column outright
            return resolveJoinWithGrant(column, u)
        },
        CanRead: func(name string) bool {
            // gates every relation read: rels enrichment, pickers, m2m links
            return userCanRead(u, name)
        },
    }
}

func writeService(u User, res engine.Resolver) *engine.WriteService {
    return &engine.WriteService{
        R:       res,
        H:       myHookAdapter(u), // engine.Hooks implementation (see 06)
        CanWrite: func(table, grant string) bool {
            // junction write gate: table = junction def name,
            // grant = "create" | "delete" for the link sync
            return userCan(u, table, grant)
        },
        RefSources: func(t *defs.Table) ([]engine.RefSource, error) {
            // inbound fk references for delete protection, derived from metadata
            return inboundRefs(t)
        },
    }
}

h := httpapi.New(def.TableName, coreTable, &defSource{res}, httpapi.Options{
    Gate: gateFor(u),
    Services: func(r *http.Request, t *defs.Table) httpapi.ServiceSet {
        return httpapi.ServiceSet{
            Read:   readService(u, res),
            Write:  writeService(u, res),
            Import: &engine.ImportService{R: res, H: myHookAdapter(u)},
        }
    },
})
```

### Callback semantics

- **`ReadService.FKJoin`** (`engine.FKJoinResolver`) — resolves one fk column's join
  target. `nil` rejects fk filter columns (a missing read grant behaves like
  "column not filterable"). It owns both the physical resolution and the permission.
- **`ReadService.CanRead`** (`func(name string) bool`) — consulted for relation
  enrichment (`rels`, `m2mRels`), the fk/m2m picker pages and m2m link reads. `nil`
  allows every target. Denials **skip enrichment** — the row data still serves.
- **`WriteService.CanWrite`** (`func(table, grant string) bool`) — the m2m junction
  write gate; the sync needs both `create` and `delete` on the junction def. `nil`
  allows everything.
- **`WriteService.RefSources`** — supplies inbound fk references for delete
  protection (`409 CONFLICT` with per-source counts). `nil` disables the check.
- **`WriteService.H` / `ImportService.H`** — the `engine.Hooks` implementation
  (guard/before/after, plus optional `SyncAfterHooks`). `nil` runs no hooks.

Without `Services`, `httpapi` wires defaults from the `DefSource`: one
`ReadService`/`WriteService`/`ImportService` over the source's own resolver, hook
execution through the registry (before-hooks synchronous with the request context,
after-hooks synchronous post-commit — no outbox in the library), fk joins resolved
from the registered defs, delete protection derived from registered defs' fk columns.

## Row-level authorization

The library does not prescribe row-level security. Two workable patterns:

- **Scope via hooks** — a `BeforeCreate`/`BeforeUpdate` hook (or a before-hook chain)
  that stamps/validates `tenant_id` on every write, plus list scoping in the host by
  wrapping the resource handler and rewriting the `filters` query param (append a
  tenant filter) before the request reaches the engine. Filters are AND-combined, so
  an injected filter cannot be dropped by the client.
- **Views** — expose a query view (`SourceType: "query"` with a `WHERE tenant = ...`)
  instead of the base table for restricted callers.

Remember `GET rows` serves whole definitions; the gate cannot filter *rows*, only
*requests*. Any row-level policy must live in filters, hooks or the SQL you expose.
