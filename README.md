# Ku-CRUD

Runtime-defined CRUD over your Postgres and MySQL databases. Single binary, no DDL —
you own the schema, Ku-CRUD gives it a UI.

One Go process serves the JSON API (`/api/*`) and an embedded React SPA.
Metadata (users, datasources, table definitions, audit trail) lives in a
local SQLite file; your data never leaves your Postgres databases.

## Quick start

    go build -o ku-crud ./cmd
    ./ku-crud                                 # serves on :8080, metadata in ./ku-crud.db

Open http://localhost:8080 — a fresh instance (no users yet) automatically
redirects to the **first-run setup page** to create the admin user. After that,
log in, add a datasource (Postgres host/port/db/user/password), pick tables,
and CRUD away.

## How it works

1. **Datasources** — register Postgres or MySQL databases (driver + host/port/db/user/password/sslmode).
2. **Table definitions** — a 3-step wizard introspects a live table (columns, types,
   keys, enums) and lets you tune labels, editability, visibility, search/sort flags,
   and the page size. Keys can be a single column or a **composite key** (they are
   only used as the update/delete WHERE predicate, so they don't have to be the
   real Postgres PK — but at least one key column is required). Ku-CRUD never runs
   DDL — it only reads `information_schema`.
3. **CRUD** — each definition gets a data grid with search, sort, pagination, and
   row create/edit/delete. All SQL is fully parameterized; identifiers are validated
   against a strict allowlist (`^[A-Za-z_][A-Za-z0-9_]*$`) before quoting, and row
   keys travel in URLs as an opaque encoding.
4. **Relations (v1.2)** — a column can be typed `fk` pointing at another table
   definition (same or another datasource, self-reference allowed). The grid
   and forms show related display fields; forms pick related records via a
   searchable modal; referenced records are edited on their own table's page.
   Deleting a row that other defined tables reference is blocked with a clear
   conflict message; database FK violations surface the same way.
5. **Drift detection** — on page visit, the live schema is compared to the definition
   (`GET /api/tables/{id}/verify`). On drift the UI shows a red banner listing
   missing/added/type-changed columns with a one-click **Re-sync**.
6. **Audit trail** — every insert/update/delete writes best-effort audit rows
   (user, action, row key, old/new values) viewable at `/audit`.
7. **Roles & users** — the first user becomes the builtin **Admin**. Admins define
   custom roles: four independent platform grants (datasources, table definitions,
   audit trail, hook outbox) plus independent read/create/update/delete grants per table.
   User & role management is admin-only; the first user is immutable.
8. **CSV import/export (v1.3)** — export the grid as CSV (active search/sort
   applied, all pages, fk/m2m relations resolved to display values, BOM-prefixed
   UTF-8 format up to 100,000 rows via `GET /api/tables/{id}/rows/export`); import
   CSV with server-side parsing (comma/semicolon/tab auto-detected, 5MB / 10,000
   rows caps), editable column mapping, per-row validation preview (`POST
   /api/tables/{id}/rows/import/preview`) and batch insert (`POST
   /api/tables/{id}/rows/import` with `valid` or `all` mode), every insert audited.
9. **Bulk operations (v1.3)** — multi-select rows on the grid and delete them
   in one confirmed action (up to 1,000 keys per request via `POST
   /api/tables/{id}/rows/bulk-delete`); per-row conflict reporting and audit entries.
10. **Many-to-Many (v1.3)** — a virtual `m2m` column models a junction table
    (a defined table with two fk columns: one → this table, one → target).
    Grids show joined display values; forms manage links through a
    multi-select picker (requires create+delete grants on the junction;
    every link change is audited; deleting linked rows is blocked).
11. **Quick wins (v1.3)** — UUID and JSON column types (JSON pretty-printed in
    forms and grids, validated before submit), datasource passwords encrypted
    at rest (AES-256-GCM using `dsn_crypt_key`), login/setup rate limiting (5 failures / 15 min per
    username+IP), and default sort configuration (`defaultSortColumn`, `defaultSortDir`) per table definition.
12. **Exploration & portability (v1.4)** — advanced per-column filtering on the
    grid and CSV export (per-type operators for text/number/datetime/uuid/
    boolean/enum/fk, inclusive/exclusive ranges, `in` lists, filter chips);
    metadata **definition export/import** (password-free natural-key JSON, with
    a preview of new / duplicate / conflicting definitions before applying);
    per-column **validation rules** (email, min/max length, number-only,
    text-only) enforced on create/update and CSV import; sidebar table
    **grouping** (drag-to-group, nested menus) and a data-page shortcut to the
    definition editor.
13. **Views, formatting & personalization (v1.5)** — **computed columns**:
    definition-level virtual columns evaluated server-side from an allowlisted
    formula (no live DDL, never persisted); **multiple views** per table
    (grid / kanban / calendar, with the default view stored in the definition);
    **column formatting** (enum colors, number thousands/decimals/prefix,
    locale-aware datetime); **grid group-by**; **per-user saved filters**
    (name + filter JSON, private to the owning user); and **ID/EN
    localization** (user language preference, switchable in the UI).
14. **Automation via Go hooks (v1.6)** — write plain Go functions in `hooks/`
    with the `HookFunc` signature; `make dev` / `make build` regenerate the
    registry (AST codegen) so the functions appear in the table-definition
    editor. Assign hooks per event (before/after × create/update/delete)
    with per-assignment JSON config and execution order. Before-hooks run
    synchronously — they may modify values or reject the write (400
    `HOOK_REJECTED`); after-hooks run on a background worker backed by a
    durable SQLite outbox (5 retries: 30s/2m/10m/1h/4h, then dead — monitor
    and retry at `/hooks-outbox`). Hooks fire from every write path (form,
    CSV import preview/apply, bulk delete, kanban drag, m2m link sync) and
    receive full platform access (datasource adapters, metadata store,
    logger). A definition referencing a hook absent from the binary rejects
    writes with `HOOK_MISSING` — drift is never silent.
15. **Business UI polish (v1.7)** — the monolithic *Platform Management*
    grant is split into four independent grants (datasources, table
    definitions, audit trail, hook outbox), so a role can see exactly the
    management menus it needs — the sidebar follows the grants. Table
    definitions gain an optional **description** surfaced in the sidebar
    tooltip and under the data-page title, and the definition wizard makes
    the menu label prominent with a live preview. API errors render as
    friendly sentences (ID/EN) with the technical detail collapsed.
16. **Query views (v1.8)** — a table definition can be backed by a raw SQL
    SELECT (`sourceType: "query"`) instead of a physical table. Results are
    read-only grids with the full pipeline: search, sort, per-column filters,
    pagination, saved filters and CSV export. Execution is guarded in depth:
    EXPLAIN-validated single SELECT, read-only transaction, 15 s statement
    timeout and row caps; all write endpoints answer 403 `QUERY_READONLY`.

Supported column types: `boolean`, `number` (int/float/numeric), `text`,
`datetime` (date/time/timestamp), native Postgres `enum`, `uuid`, `json`
(json/jsonb; MySQL `json`), and `fk`/`m2m` relations. Arrays and bytea
columns are excluded. Computed columns are definition-level virtual columns
evaluated server-side — they are not live column types and never exist in
the underlying database.

## Configuration

The server has two flags and no config file:

| Flag    | Default      | Description                              |
|---------|--------------|------------------------------------------|
| `-addr` | `:8080`      | Listen address (`host:port` or `:port`)  |
| `-data` | `ku-crud.db` | Path to the SQLite metadata file         |

## API Endpoints

All API endpoints are under `/api` and return JSON. Authenticated endpoints require a valid session cookie (`ku_session`).

### Setup & Authentication
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/setup/status` | Check if initial setup is needed |
| POST | `/api/setup` | Create initial Admin user (only works when no users exist, rate limited) |
| POST | `/api/auth/login` | Log in with username/password (rate limited: 5 failures / 15 min per user+IP) |
| GET | `/api/auth/me` | Get current user profile and permissions |
| POST | `/api/auth/logout` | End session |

### Datasources & Table Definitions
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET / POST | `/api/datasources` | List or register datasources |
| GET / PUT / DELETE | `/api/datasources/{id}` | Manage a datasource |
| POST | `/api/datasources/{id}/test` | Test database connection |
| POST | `/api/datasources/{id}/introspect` | Inspect tables in a database |
| GET / POST | `/api/tables` | List or create table definitions |
| GET / PUT / DELETE | `/api/tables/{id}` | Manage a table definition |
| GET | `/api/tables/{id}/verify` | Verify definition against live database schema (drift detection) |
| POST | `/api/tables/{id}/resync` | Re-sync table definition columns with live schema |

### Data Rows & Bulk Operations (v1.3)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/tables/{id}/rows` | List rows with search (`search=`), sort (`sort=`, `dir=`), and pagination (`page=`, `limit=`) |
| POST | `/api/tables/{id}/rows` | Insert a new row |
| GET / PUT / DELETE | `/api/tables/{id}/rows/{rowKey}` | View, update, or delete a specific row by composite key |
| GET | `/api/tables/{id}/rows/export` | Export filtered/sorted rows to BOM UTF-8 CSV (up to 100,000 rows) |
| POST | `/api/tables/{id}/rows/import/preview` | Upload and preview CSV file with column auto-mapping and validation |
| POST | `/api/tables/{id}/rows/import` | Batch insert records from CSV (`mode=valid` or `mode=all`) |
| POST | `/api/tables/{id}/rows/bulk-delete` | Bulk delete rows by array of keys (up to 1,000 keys per request) |
| GET | `/api/tables/{id}/rows/m2m-options` | Fetch target table options for Many-to-Many relation field picker |
| GET | `/api/tables/{id}/rows/{rowKey}/m2m-links` | Get current linked keys for a row's Many-to-Many field |

### Administration & Audit
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET / POST | `/api/users` | List or create users (Admin only) |
| PUT / DELETE | `/api/users/{id}` | Manage users (Admin only; first user is immutable) |
| GET / POST | `/api/roles` | List or create custom roles (Admin only) |
| PUT / DELETE | `/api/roles/{id}` | Manage roles (Admin only) |
| GET | `/api/audit` | View audit trail records (Platform management grant) |

### Hooks (v1.6)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/hooks` | List hook functions compiled into the server |
| GET | `/api/hooks/outbox` | Monitor the after-hook outbox (pending / retrying / dead entries with last error) |
| POST | `/api/hooks/outbox/{id}/retry` | Requeue a dead outbox entry for immediate retry |


## First user

On a fresh instance the UI redirects to `/setup`, which calls two unauthenticated
endpoints that only work while no user exists:

- `GET /api/setup/status` → `{"needed":true|false}`
- `POST /api/setup` `{username,password}` — atomically creates the first user
  (the immutable, all-powerful **Admin**); permanently returns **403** once any
  user exists (no HTTP path to create a second user this way).

The CLI alternative is `cmd/seed-admin` (also the recovery tool if you ever
lock yourself out):

    go run ./cmd/seed-admin -data ku-crud.db -username admin

It prompts for the password on a TTY and reads stdin when piped, so it is
scriptable (`echo 'secret' | go run ./cmd/seed-admin ...`). An instance seeded
via CLI never offers the setup page.

Sessions: HMAC-SHA256 signed cookie `ku_session`, 24h expiry, HttpOnly +
SameSite=Lax. The signing secret is generated once and stored in the SQLite
metadata file.

## Upgrading from v1.0

Replace the binary and restart — schema migrations run automatically on
startup. The v1.1 migration assigns every pre-existing user the builtin
**Admin** role and flags the earliest user as the immutable first user;
single-PK definitions are migrated to the new `keyColumns` form (no change
in behavior). From v1.2 the datasource driver defaults to `postgres`.
v1.3 adds default-sort + relation columns and encrypts stored datasource
passwords in place (one-way: the metadata file then requires v1.3+).
v1.4 runs migration 7 (table_groups, table_defs.group_id, columns.validations)
on first start. v1.5 runs migration 8 (formatting/computed/default_view/
view_config on definitions, users.language, saved_filters) on first start.
v1.6 runs migration 9 (table_defs.hooks assignments, hook_outbox) on first
start. v1.7 runs migration 10 (split `platform_manage` into `manage_datasources`,
`manage_tables`, `view_audit`, `view_outbox`; `table_defs.description`) on
first start. Roles that had platform management get **all four** grants —
revisit the Roles editor after upgrading to remove what business users
should not see. The migration is one-way: a v1.7 metadata file cannot be
read by older binaries.
Metadata import files never contain datasource passwords — re-enter them in
the import wizard.

## Security notes

- Datasource passwords are stored **encrypted at rest** (AES-256-GCM, key kept in
  the metadata file's settings table). This removes plaintext credentials from
  casual reads or dumps of `ku-crud.db`; note the key lives in the same file,
  so this is hardening rather than a security boundary — still protect the file
  (file permissions). Upgrading from ≤ v1.2 encrypts existing passwords
  automatically on first start, and the DB then requires v1.3+ to read.
- **RBAC**: users hold exactly one role. The builtin Admin role implicitly
  has every permission (full platform access and full CRUD on all tables).
  Custom roles combine four independent platform grants — **manage
  datasources**, **manage table definitions**, **view audit trail**,
  **view hook outbox** (definition transfer needs datasource + definition;
  users/roles stay Admin-only) — with independent read/create/update/delete
  grants per table.
  User and role management is Admin-only. Disabled users are rejected at login
  and on every request. `fk` relations follow the same grants: related display
  values (and the fk record picker) resolve only for users with read access to
  the target table — everyone else sees raw column values.
- **Masked ids**: every entity id crossing the API boundary (datasources, table
  definitions, users, roles, audit entries) is an opaque 11-char token — a
  keyed Feistel permutation of the numeric id (HMAC-SHA256 round function,
  secret stored in the metadata file). Tokens are deterministic, unforgeable,
  and not decodable into a chosen id; raw numeric ids simply 404.
- Ku-CRUD never runs DDL. All changes go through parameterized SQL with strict
  identifier validation. Search input has LIKE wildcards escaped; sort columns
  are checked against the stored definition. CSV import mapping and values go
  through the same validation as the row write path.
- Login and first-run setup endpoints are rate limited (5 failed attempts per
  username+IP within 15 minutes → HTTP 429).
- The first user cannot be modified or deleted through the API.
  `cmd/seed-admin` remains the CLI recovery path.
- Bind to localhost or a private interface if the app is not behind an
  authenticated proxy (see Deployment).
- The session cookie is not marked `Secure` (targets plain HTTP on a trusted
  network). If you terminate TLS in front of Ku-CRUD, add the `Secure` attribute
  in `internal/api/auth.go` before exposing it publicly.

## Logging

The server logs JSON lines (Go `log/slog`) to stdout: one access line per
request with method, path, status and duration; 4xx responses are logged at
WARN and 5xx at ERROR including the application error code and message.
Under systemd, follow with `journalctl -u ku-crud -f`.

## Development

### Prerequisites

- Go 1.25+ (matches `go.mod`)
- Node 20+ / npm (frontend build only)
- Docker (optional — for the dev/test Postgres)

### Project layout

    cmd/main.go            server entry: flags, serves the API + embedded SPA
                           (go run ./cmd/main.go)
    cmd/seed-admin/        create an admin login user in the metadata store
    cmd/hookgen/           go:generate AST scanner: regenerates
                           hooks/registry_gen.go from the functions in hooks/
    hooks/                 developer-written hook functions (HookFunc
                           signature) + the generated registry_gen.go
    internal/hooks/        hook contract, executor (before-hooks), outbox
                           worker (after-hooks with retry/backoff)
    internal/meta/         SQLite metadata store: migrations, users, roles,
                           datasources, table defs, audit
    internal/ds/           Adapter layer: dialect-neutral `Adapter` interface +
                           `ds.Open` factory, sqlkit (dialect SQL builders),
                           postgres & mysql adapters (introspection, drift
                           compare inputs, fully parameterized SQL)
    internal/tokenid/      masked id codec (Feistel-HMAC, 11-char tokens)
    internal/api/          HTTP handlers: auth, RBAC gates, datasources,
                           table defs, rows, users, roles, audit, logging
                           middleware
    web/                   React + Vite + TypeScript + Tailwind v3 + shadcn/ui
    web/embed.go           go:embed of web/dist (lives next to the embedded dir —
                           embed cannot reach parent directories)
    web/dist/              embedded SPA (placeholder committed; real build output
                           is generated by `npm run build` / `make build`)

### Development mode (backend and frontend separately)

Run each side on its own — no rebuild of the SPA needed while coding:

    # terminal 1 — server (API + whatever SPA is in web/dist), on :8080
    make dev
    # or: go run ./cmd/main.go -addr :8080 -data /tmp/dev.db

    # terminal 2 — frontend with hot reload, on :5173 (proxies /api → :8080)
    cd web && npm run dev

Open http://localhost:5173 for live frontend work (terminal 1's own
http://localhost:8080 serves the last built SPA — a placeholder until you run
`npm run build`). Backend changes: restart the server. Frontend changes:
vite hot-reloads.

Seed a user for the dev metadata file (prompts on a TTY; reads stdin when
piped, so it works from scripts):

    go run ./cmd/seed-admin -data /tmp/dev.db -username admin

### Backend

    make dev-pg                       # docker compose up -d — starts both dev DBs:
                                      # Postgres on :5433 (ku/ku/ku)
                                      # MySQL on :3307 (ku/ku/ku)
    go run ./cmd/main.go -addr :8080 -data /tmp/dev.db

Tests (unit tests self-skip without a database):

    go test ./...                                   # unit only
    KUCRUD_TEST_PG=postgres://ku:ku@localhost:5433/ku go test ./... -count=1
    KUCRUD_TEST_MYSQL='ku:ku@tcp(localhost:3307)/ku?parseTime=true' \
      go test ./... -count=1 -p 1

Integration tests share one schema (`DROP SCHEMA public CASCADE`), so run
packages serially under load: `go test -p 1 ./...`.

Lint/format: `go vet ./...` and `gofmt -l .` must be clean.

### Hooks development

Add a plain Go function with the `HookFunc` signature in `hooks/`, then run
`make dev` (or `make build`) — `go generate ./hooks` scans the package with
cmd/hookgen and rewrites `hooks/registry_gen.go`, so the function appears in
the table-definition editor's Hooks section on the next load. Renaming a
function registers a **new** hook: assignments to the old name stay in the
definition and reject writes with `HOOK_MISSING` until you re-assign them.
Failed after-hooks land on the durable outbox and retry with backoff —
monitor and manually retry dead entries at `/hooks-outbox`.

### Frontend

    cd web
    npm install
    npm run dev        # Vite dev server, proxies /api → localhost:8080
    npm run build      # type-checks (strict) and writes web/dist

Run the backend on `:8080` for the dev-proxy setup to work as-is.

### Handy curl checks

    curl -s localhost:8080/api/health
    curl -s -c /tmp/cj -X POST localhost:8080/api/auth/login \
      -d '{"username":"admin","password":"..."}'
    curl -s -b /tmp/cj localhost:8080/api/datasources
    curl -s -b /tmp/cj localhost:8080/api/tables

## Building the binaries

    make build          # builds web/, embeds it into ./ku-crud, builds ./seed-admin

`go build ./cmd` alone embeds whatever is currently in web/dist.

## Deployment

Ku-CRUD is a single static binary with one writable dependency: the SQLite
metadata file. Any Linux host (or container) that can reach your Postgres
databases works.

### 1. Build and stage

On a machine with Go and Node (or build once in CI and copy the artifacts):

    make build
    scp ku-crud seed-admin user@server:/opt/ku-crud/

### 2. First user

Either use the built-in setup page — open the app in a browser and it redirects
to `/setup` on first run — or seed from the shell (prompts for a password,
min 4 chars, needs a TTY):

    cd /opt/ku-crud
    ./seed-admin -data /opt/ku-crud/ku-crud.db -username admin

If the server is exposed to untrusted networks, prefer the CLI (run it before
starting the service) so the setup page is never reachable.

### 3. Run under systemd

`/etc/systemd/system/ku-crud.service`:

    [Unit]
    Description=Ku-CRUD
    After=network-online.target

    [Service]
    User=ku-crud
    Group=ku-crud
    WorkingDirectory=/opt/ku-crud
    ExecStart=/opt/ku-crud/ku-crud -addr 127.0.0.1:8080 -data /opt/ku-crud/ku-crud.db
    Restart=on-failure

    # Hardening
    NoNewPrivileges=true
    ProtectSystem=strict
    ProtectHome=true
    ReadWritePaths=/opt/ku-crud

    [Install]
    WantedBy=multi-user.target

    systemctl daemon-reload && systemctl enable --now ku-crud

Binding to `127.0.0.1` keeps the port off the network; expose it via a reverse
proxy (recommended) or change to a private interface as needed.

### 4. Reverse proxy (recommended)

Terminate TLS and proxy to the app. nginx example:

    server {
        listen 443 ssl;
        server_name ku-crud.example.com;
        ssl_certificate     /etc/letsencrypt/live/ku-crud.example.com/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/ku-crud.example.com/privkey.pem;

        location / {
            proxy_pass http://127.0.0.1:8080;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        }
    }

If you expose Ku-CRUD over TLS, mark the session cookie `Secure` (see
Security notes).

### 5. Operate

- **Backups** — copy `ku-crud.db` while the service is stopped (or use
  `sqlite3 ku-crud.db ".backup backup.db"` online). This file holds users,
  session secret, datasource passwords, and table definitions. Losing it means
  re-registering datasources and definitions; your Postgres data is untouched.
- **Upgrades** — replace the binary and restart. Schema migrations run
  automatically and idempotently on startup. Keep a copy of the old binary and
  a metadata backup before upgrading.
- **Logs** — JSON via stdout and journald (`journalctl -u ku-crud -f`): request
  access lines with durations, errors for non-2xx responses. Audit write
  failures are logged here and never block a CRUD action.
- **Firewall** — allow the app outbound access to your Postgres ports; restrict
  inbound access to the proxy / trusted networks only.
