-- Example schema for the template starter (PostgreSQL).
-- Applied idempotently at boot so `go run .` works on a fresh database;
-- real applications bring their own migrations.

CREATE TABLE IF NOT EXISTS categories (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

INSERT INTO categories (name)
SELECT 'General'
WHERE NOT EXISTS (SELECT 1 FROM categories);

CREATE TABLE IF NOT EXISTS products (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    price       NUMERIC(12,2),
    category_id BIGINT REFERENCES categories(id),
    -- Nullable + DEFAULT so inserts may omit it (introspection maps
    -- NOT NULL to client-required; the DB default fills the value).
    created_at  TIMESTAMPTZ DEFAULT now()
);
