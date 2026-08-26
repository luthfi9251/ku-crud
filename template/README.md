# kucrud template

Full-stack starter consuming [kucrud-core](../core): declare tables in
`main.go`, get CRUD routes + a thin React UI. Auth ships deny-all
(`authstub/`) — nothing is exposed until you wire it.

## Quickstart

```bash
# 1. clone, then point the template at the in-repo core (already set):
#    template/go.mod:  replace github.com/luthfi9251/kucrud-core => ../core
# 2. rename the module to yours. The path kucrud-template is not only
#    in go.mod — main.go and smoke_test.go import kucrud-template/authstub —
#    so replace it in go.mod AND Go sources (from template/):
#    sed -i 's|kucrud-template|com.example.myapp|g' go.mod $(grep -rl --include='*.go' kucrud-template .)
# 3. set the database (PostgreSQL; example tables auto-create at boot)
export KUCRUD_DB_DSN='postgres://ku:ku@localhost:5432/ku'
# 4. build the web app and run the server (API on :8080, SPA served too)
cd web && npm install && npm run build && cd ..
KUCRUD_ADDR=:8080 go run .
# 5. open http://localhost:8080 — the UI loads, API answers 403 until
#    you wire auth: replace the deny-all Gate in authstub/middleware.go
```

Or Docker (from the repo root): `docker build -f template/Dockerfile -t kucrud-template .`

## Layout

| Path | What |
|---|---|
| `main.go` | host app: connection env, example resource, mux, SPA mount |
| `schema.sql` | example tables (applied idempotently at boot) |
| `authstub/middleware.go` | deny-all Gate stub — wire your auth here |
| `web/` | Vite + React: login placeholder + products CRUD page |
| `smoke_test.go` | end-to-end test (needs `KUCRUD_TEST_PG`, self-skips) |

## Routes (the core contract)

`GET /api/defs`; per resource `/api/data/{name}`: `GET|POST rows`,
`GET|PUT|DELETE rows/{key}`, `rows/export`, `rows/bulk-delete`,
`fkoptions/{col}`, `m2moptions/{col}`, `import/preview|apply`.
