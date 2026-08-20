# Ku-CRUD

Runtime-defined CRUD over your Postgres databases. Single binary, no DDL —
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

1. **Datasources** — register Postgres databases (host/port/db/user/password/sslmode).
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
   custom roles: a *Platform Management* bundle (datasources, table definitions,
   audit trail) plus independent read/create/update/delete grants per table.
   User & role management is admin-only; the first user is immutable.

Supported column types: `boolean`, `number` (int/float/numeric), `text`,
`datetime` (date/time/timestamp), and native Postgres `enum`. An `fk` column
relates a column to another table definition (the underlying column type is
preserved for drift checks). Arrays, JSON, UUID, and bytea columns are
excluded in v1.

## Configuration

The server has two flags and no config file:

| Flag    | Default      | Description                              |
|---------|--------------|------------------------------------------|
| `-addr` | `:8080`      | Listen address (`host:port` or `:port`)  |
| `-data` | `ku-crud.db` | Path to the SQLite metadata file         |

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
in behavior).

## Security notes

- Datasource passwords are stored PLAINTEXT in the SQLite metadata file. Anyone who
  can read ku-crud.db can read those passwords — protect the file (file permissions).
- **RBAC**: users hold exactly one role. The builtin Admin role implicitly has
  every permission (full platform access and full CRUD on all tables). Custom
  roles combine the Platform Management bundle (datasources + table definitions
  + audit trail) with independent read/create/update/delete grants per table.
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
  are checked against the stored definition.
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
    internal/meta/         SQLite metadata store: migrations, users, roles,
                           datasources, table defs, audit
    internal/ds/           Postgres: DSN/connect, introspection, drift compare,
                           query builders (QuoteIdent + fully parameterized SQL)
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

    make dev-pg                       # dev Postgres on :5433 (ku/ku/ku)
    go run ./cmd/main.go -addr :8080 -data /tmp/dev.db

Tests (unit tests self-skip without a database):

    go test ./...                                   # unit only
    KUCRUD_TEST_PG=postgres://ku:ku@localhost:5433/ku go test ./... -count=1

Integration tests share one schema (`DROP SCHEMA public CASCADE`), so run
packages serially under load: `go test -p 1 ./...`.

Lint/format: `go vet ./...` and `gofmt -l .` must be clean.

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
