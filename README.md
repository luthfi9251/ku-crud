# Ku-CRUD

Runtime-defined CRUD over your Postgres databases. Single binary, no DDL —
you own the schema, Ku-CRUD gives it a UI.

## Quick start

    go build -o ku-crud .
    ./ku-crud -create-user admin      # prompts for a password (min 4 chars)
    ./ku-crud                         # serves on :8080, metadata in ./ku-crud.db

Open http://localhost:8080, log in, add a datasource (Postgres host/port/db/user/password),
pick tables, and CRUD away.

## Security notes

- Datasource passwords are stored PLAINTEXT in the SQLite metadata file. Anyone who
  can read ku-crud.db can read those passwords — protect the file (file permissions).
- Auth is single-tier: every logged-in user can do everything. There are no roles.
- Ku-CRUD never runs DDL. All changes go through parameterized SQL with strict
  identifier validation.

## Development

    docker compose up -d              # dev Postgres on :5433 (ku/ku/ku)
    KUCRUD_TEST_PG=postgres://ku:ku@localhost:5433/ku go test ./...

Frontend lives in web/ (React + Vite + Tailwind + shadcn/ui); `npm run build`
in web/ refreshes web/dist which is embedded into the binary at compile time.
